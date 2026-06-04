#!/usr/bin/env node
"use strict";

/**
 * changez 配置加载器
 *
 * 配置层级（优先级从高到低）：
 *   项目级配置 (.changez/config.json)
 *     ↓ 逐字段覆盖
 *   环境变量 (CHANGEZ_*)
 *     ↓ 逐字段覆盖
 *   全局配置 (~/.changez/config.json)
 *     ↓ 逐字段覆盖
 *   硬编码默认值
 */

const fs = require("fs");
const path = require("path");

// 缓存 minimatch 避免每次 matchPattern 都 require
let minimatch;
try {
  minimatch = require("minimatch");
} catch {
  minimatch = null;
}

// ── 默认值 ──────────────────────────────────────────────────────

const DEFAULTS = {
  url: "http://127.0.0.1:8760",
  token: "",
  source: "",
  max_file_size: 10485760,
  log_level: "info",
  gitignore: true,
  excludes: [
    "data/",
    "node_modules/",
    ".git/",
    "vendor/",
    "dist/",
    "build/",
    "*.delta",
    "*.db",
    "*.sqlite",
    "*.blob",
    "*.png",
    "*.jpg",
    "*.jpeg",
    "*.gif",
    "*.ico",
    "*.bmp",
    "*.webp",
    "*.zip",
    "*.tar",
    "*.gz",
    "*.bz2",
    "*.xz",
    "*.pdf",
    "*.exe",
    "*.dll",
    "*.so",
    "*.dylib",
    "*.bin",
    "*.class",
    "*.pyc",
    "*.wasm",
    "*.woff",
    "*.woff2",
    "*.ttf",
    "*.otf",
    "*.eot",
  ],
};

// ── 工具函数 ────────────────────────────────────────────────────

function expandTilde(p) {
  if (p.startsWith("~/")) {
    const home = process.env.HOME || process.env.USERPROFILE || "";
    return path.join(home, p.slice(2));
  }
  if (p === "~") {
    return process.env.HOME || process.env.USERPROFILE || "";
  }
  return p;
}

function loadJSON(filePath) {
  try {
    const absPath = path.isAbsolute(filePath) ? filePath : expandTilde(filePath);
    const content = fs.readFileSync(absPath, "utf8");
    return JSON.parse(content);
  } catch (e) {
    if (e.code === "ENOENT" || e.name === "SyntaxError") {
      // ENOENT: file not found — silently ignore, fallback to defaults.
      // SyntaxError: JSON parse error — warn but don't crash so the hook keeps working.
      console.warn(`changez: 配置加载失败 ${filePath}: ${e.message}`);
    } else if (e.code === "EACCES") {
      console.error(`changez: 无权读取配置文件 ${filePath}: ${e.message}`);
    } else {
      console.warn(`changez: 配置加载异常 ${filePath}: [${e.name}] ${e.message}`);
    }
    return null;
  }
}

function loadEnvConfig() {
  const config = {};

  if (process.env.CHANGEZ_URL !== undefined) config.url = process.env.CHANGEZ_URL;
  if (process.env.CHANGEZ_TOKEN !== undefined) config.token = process.env.CHANGEZ_TOKEN;
  if (process.env.CHANGEZ_SOURCE !== undefined) config.source = process.env.CHANGEZ_SOURCE;
  if (process.env.CHANGEZ_MAX_FILE_SIZE !== undefined) {
    const v = parseInt(process.env.CHANGEZ_MAX_FILE_SIZE, 10);
    if (!isNaN(v)) config.max_file_size = v;
  }
  if (process.env.CHANGEZ_LOG_LEVEL !== undefined) config.log_level = process.env.CHANGEZ_LOG_LEVEL;
  if (process.env.CHANGEZ_GITIGNORE !== undefined) {
    config.gitignore = process.env.CHANGEZ_GITIGNORE.toLowerCase() === "true";
  }
  if (process.env.CHANGEZ_EXCLUDES !== undefined) {
    config.excludes = process.env.CHANGEZ_EXCLUDES.split(",").map((s) => s.trim()).filter(Boolean);
  }

  return config;
}

// ── 配置合并 ────────────────────────────────────────────────────

function mergeConfigs(...configs) {
  const result = {};
  for (const config of configs) {
    if (config) {
      for (const key of Object.keys(config)) {
        if (config[key] !== undefined) {
          result[key] = config[key];
        }
      }
    }
  }
  return result;
}

// ── 配置查找 ────────────────────────────────────────────────────

/* 向上查找 .changez/config.json，以 .git 目录为边界。
   如果目录树中没有 .git 目录，会一直查找到文件系统根目录。
   返回 { config, dir } 或 null。 */
function findProjectConfig(startDir) {
  let current = path.resolve(startDir);
  const root = path.parse(current).root;

  while (current !== root) {
    // 检查 .git 边界
    if (fs.existsSync(path.join(current, ".git"))) {
      const configPath = path.join(current, ".changez", "config.json");
      if (fs.existsSync(configPath)) {
        const config = loadJSON(configPath);
        if (config) return { config, dir: current };
      }
      return null;
    }

    // 检查 .changez/config.json
    const configPath = path.join(current, ".changez", "config.json");
    if (fs.existsSync(configPath)) {
      const config = loadJSON(configPath);
      if (config) return { config, dir: current };
    }

    current = path.dirname(current);
  }

  return null;
}

// ── .gitignore 加载 ────────────────────────────────────────────

function loadGitignore(projectRoot) {
  const gitignorePath = path.join(projectRoot, ".gitignore");
  if (!fs.existsSync(gitignorePath)) return [];

  const patterns = [];
  const lines = fs.readFileSync(gitignorePath, "utf8").split("\n");

  for (const line of lines) {
    const trimmed = line.trim();
    // 跳过空行、注释、否定模式
    if (!trimmed || trimmed.startsWith("#") || trimmed.startsWith("!")) continue;
    patterns.push(trimmed);
  }

  return patterns;
}

// ── 主接口 ─────────────────────────────────────────────────────

/* 加载完整配置
 * @param {string} startDir - 起始目录（通常是当前工作目录）
 * @param {object} pluginConfig - 插件参数（opencode），优先级同环境变量
 * @returns {{ config: object, excludes: string[], projectRoot: string }}
 */
function loadConfig(startDir, pluginConfig) {
  // 1. 加载全局配置
  const globalConfig = loadJSON("~/.changez/config.json");

  // 2. 加载项目级配置
  const projectResult = findProjectConfig(startDir);

  // 3. 合并配置（从左到右优先级递增）
  //    优先级: 默认值 < 全局配置 < 环境变量 < 插件参数 < 项目级配置
  //    pluginConfig（opencode 插件参数）优先级高于环境变量
  const envConfig = loadEnvConfig();
  let config = mergeConfigs(
    DEFAULTS,
    globalConfig,
    envConfig,
    pluginConfig,
    projectResult ? projectResult.config : null
  );

  // 4. 确定项目根目录
  let projectRoot = startDir;
  if (projectResult) {
    projectRoot = projectResult.dir;
  } else {
    // 向上查找 .git 作为项目根目录
    let current = path.resolve(startDir);
    const root = path.parse(current).root;
    while (current !== root) {
      if (fs.existsSync(path.join(current, ".git"))) {
        projectRoot = current;
        break;
      }
      current = path.dirname(current);
    }
  }

  // 5. 合并 excludes
  const excludes = new Set(config.excludes || []);

  if (config.gitignore) {
    const gitignorePatterns = loadGitignore(projectRoot);
    for (const pattern of gitignorePatterns) {
      excludes.add(pattern);
    }
  }

  return {
    config,
    excludes: [...excludes],
    projectRoot,
  };
}

// ── 文件过滤 ────────────────────────────────────────────────────

/* 检查文件是否应该跳过
 * @param {string} filePath - 文件路径
 * @param {string[]} excludes - 排除模式列表
 * @param {string} [content] - 文件内容（用于 null byte 检测）
 * @returns {boolean}
 */
function shouldSkipFile(filePath, excludes, content) {
  // Layer 1: 检查是否匹配 excludes 模式
  for (const pattern of excludes) {
    if (matchPattern(pattern, filePath)) return true;
  }

  // Layer 2: Null Byte 兜底检测（仅当 content 可用时）
  if (content) {
    const checkLen = Math.min(content.length, 8192);
    for (let i = 0; i < checkLen; i++) {
      if (content.charCodeAt(i) === 0) return true;
    }
  }

  return false;
}

/* .gitignore 模式匹配（简化版）
 * 支持：data/ 目录排除, *.ext 通配符, vendor/** 递归通配符
 * 不支持：! 否定模式, # 注释
 */
function matchPattern(pattern, filePath) {
  // 目录排除（以 / 结尾）
  if (pattern.endsWith("/")) {
    const dir = pattern.slice(0, -1);
    // 精确匹配或子目录匹配
    if (filePath === dir || filePath.startsWith(dir + "/")) return true;
    // 也支持路径中包含该目录
    const normalized = filePath.replace(/\\/g, "/");
    if (normalized.includes("/" + dir + "/") || normalized.startsWith(dir + "/")) return true;
    return false;
  }

  // 通配符模式
  if (pattern.includes("*")) {
    if (minimatch) {
      return minimatch(filePath, pattern, { dot: true });
    }
    return simpleGlobMatch(pattern, filePath);
  }

  // 精确匹配
  return filePath === pattern || filePath.includes("/" + pattern + "/");
}

/* 简化的 glob 匹配（minimatch 不可用时的降级方案） */
function simpleGlobMatch(pattern, filePath) {
   // 将 glob 模式转换为正则，支持 *、**、? 通配符
   const regex = pattern
     .replace(/[.+^${}()|\\]/g, "\\$&")      // escape special chars (not ? or *)
     .replace(/\*\*/g, "___DOUBLE_STAR___")    // protect ** before single * handling
     .replace(/\\\?/g, "QMARK_ESCAPED")        // temporarily mark literal \? if escaped by user
     .replace(/\?/g, "[^/]")                   // ? matches any char except /
     .replace(/QMARK_ESCAPED/g, "\\\?\\" )      // restore literal ? (rare case)
     .replace(/\*/g, "[^/]*")                  // * matches anything in same directory level
     .replace(/___DOUBLE_STAR___/g, ".*");    // ** crosses directories

  return new RegExp("^" + regex + "$").test(filePath.replace(/\\/g, "/"));
}

// ── 导出 ────────────────────────────────────────────────────────

module.exports = {
  loadConfig,
  shouldSkipFile,
  DEFAULTS,
  matchPattern,
};
