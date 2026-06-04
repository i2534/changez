# 统一配置与二进制文件过滤方案

## 背景

文件列表中出现 `.delta` 等二进制文件记录，根因是 hook 层缺少二进制文件过滤。

### 根因链路

```
changez 写入 data/deltas/*.delta
  → opencode hook 拦截文件变更
  → 读到 delta 文件内容上报到 changez
  → changez 又写新 delta
  → 首次追踪后后续标记为 unchanged（性能浪费，非无限循环）
```

三个 hook（opencode、claudecode、cursor）目前只过滤 >10MB 的文件，**没有任何二进制文件过滤**。

## Git 的做法（参考）

Git 使用两层机制：`.gitattributes` 配置驱动 + 内容启发式检测。核心思路是**配置优先，内容检测兜底**——这与我们的方案一致。

## 统一配置系统

### 配置层级（优先级从高到低）

```
项目级配置 (.changez/config.json)
    ↓ 逐字段覆盖
环境变量 (CHANGEZ_*)
    ↓ 逐字段覆盖
全局配置 (~/.changez/config.json)
    ↓ 逐字段覆盖
硬编码默认值
```

**注意**：opencode 插件通过 `opencode.jsonc` 的 `env` 字段注入环境变量，claudecode 和 cursor 直接读取 `CHANGEZ_*` 环境变量。三个 hook 统一从环境变量读取配置。

### 配置文件查找策略

**项目级配置**：从当前工作目录向上查找 `.changez/config.json`，以 `.git` 目录为边界。

```
项目根目录
├── .git/           ← 查找边界
├── .changez/
│   └── config.json ← 项目级配置
└── src/
    └── hooks/
        └── changez-hook.js  ← 从这里向上查找
```

**全局配置**：`~/.changez/config.json`（用户主目录下）

### 配置文件结构

```json
{
  "url": "http://127.0.0.1:8760",
  "token": "your-token",
  "source": "opencode",
  "max_file_size": 10485760,
  "log_level": "info",
  "gitignore": true,
  "excludes": [
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
    "*.eot"
  ]
}
```

### 配置字段说明

| 字段 | 类型 | 说明 | 默认值 |
|---|---|---|---|
| `url` | string | Changez 服务器地址 | `http://127.0.0.1:8760` |
| `token` | string | Bearer Token | `""` |
| `source` | string | 来源标识（opencode / claudecode / cursor） | 各 hook 自有默认值 |
| `max_file_size` | number | 单文件最大字节数 | `10485760`（10MB） |
| `log_level` | string | 日志级别（debug / info / warn / error） | `"info"` |
| `gitignore` | boolean | 是否包含 `.gitignore` 的排除模式 | `true` |
| `excludes` | string[] | 排除模式列表，使用 `.gitignore` 语法 | 见下方默认列表 |

**`excludes` 默认列表**（精简版，常见项目覆盖已足够，生僻扩展名用户可按需添加）：
```
data/, node_modules/, .git/, vendor/, dist/, build/,
*.delta, *.db, *.sqlite, *.blob,
*.png, *.jpg, *.jpeg, *.gif, *.ico, *.bmp, *.webp,
*.zip, *.tar, *.gz, *.bz2, *.xz,
*.pdf, *.exe, *.dll, *.so, *.dylib, *.bin,
*.class, *.pyc, *.wasm,
*.woff, *.woff2, *.ttf, *.otf, *.eot
```

### 环境变量映射

| 环境变量 | 配置字段 |
|---|---|
| `CHANGEZ_URL` | `url` |
| `CHANGEZ_TOKEN` | `token` |
| `CHANGEZ_SOURCE` | `source` |
| `CHANGEZ_MAX_FILE_SIZE` | `max_file_size` |
| `CHANGEZ_LOG_LEVEL` | `log_level` |
| `CHANGEZ_GITIGNORE` | `gitignore` |
| `CHANGEZ_EXCLUDES` | `excludes`（逗号分隔） |

### `excludes` 规则

使用 `.gitignore` 语法：

```json
{
  "excludes": [
    "data/",
    "node_modules/",
    "*.delta",
    "*.db",
    "vendor/**/*.log"
  ]
}
```

- `data/` — 排除 `data/` 目录及其所有子目录
- `*.delta` — 排除所有 `.delta` 文件
- `vendor/**/*.log` — 排除 `vendor` 下任意层级的 `.log` 文件

### `gitignore` 字段

当 `gitignore: true` 时（默认值），hook 会读取项目根目录的 `.gitignore` 文件，将其中的排除模式合并到 `excludes` 中。

```
最终 excludes = 合并后的 excludes + .gitignore 正向模式
```

**注意**：
- 只取 `.gitignore` 中的正向排除模式（忽略 `#` 注释和 `!` 否定模式）
- `.gitignore` 模式在配置合并之后追加，始终有效

### 合并策略

- 逐字段覆盖，高优先级配置覆盖低优先级配置的**对应字段**
- 数组字段（`excludes`）也是覆盖（不是追加）
- **`gitignore` 合并发生在配置合并之后**：先完成 4 层配置合并，再将 `.gitignore` 模式追加到合并后的 `excludes`

```typescript
// 步骤 1：4 层配置合并（从左到右优先级递增）
const config = mergeConfigs(
  defaults,           // 最低优先级
  globalConfig,       // 覆盖默认值
  envVars,            // 覆盖全局
  projectConfig       // 最高优先级
);

// 步骤 2：gitignore 模式追加（始终在最后）
const finalExcludes = config.gitignore
  ? [...config.excludes, ...parseGitignore(projectRoot)]
  : config.excludes;
```

**示例**：全局 `excludes: ["*.delta"]` + 项目级 `excludes: ["*.db"]` → 合并后 `excludes: ["*.db"]`（项目级覆盖）→ 追加 `.gitignore` 模式 → 最终 `["*.db", ...gitignorePatterns]`。全局的 `*.delta` 丢失，因为高优先级覆盖了低优先级。

## 过滤方案

### 两层过滤

```
文件路径
  → Layer 1: 检查是否匹配 excludes 模式（零 I/O）
    → 匹配 → 跳过
    → 不匹配 → Layer 2: 读取前 8KB 检测 null byte
      → 有 null byte → 跳过
      → 无 null byte → 正常上报
```

原方案中的"扩展名黑名单 + 目录排除"已合并为 `excludes` 字段，使用 `.gitignore` 规则统一处理。

### 过滤函数

```typescript
function shouldSkipFile(filePath: string, content?: string): boolean {
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
```

### `.gitignore` 模式匹配

需要实现 `.gitignore` 模式匹配，支持：

- `data/` — 目录排除
- `*.delta` — 通配符扩展名排除
- `vendor/**/*.log` — 递归通配符

**不需要的功能**：
- `!` 否定模式（changez 场景下无意义）
- `#` 注释（JSON 中无法写注释，配置说明文档中解释即可）

**实现方案**：使用 `minimatch` 库（Node.js 生态中 `.gitignore` 匹配的标准库）。

> **注意**：`minimatch` 是本项目唯一的必要外部依赖。它 v9+ 版本无外部依赖，总大小 ~30KB，用于 `.gitignore` 模式匹配，是合理的依赖引入。
> 
> **兼容性**：需要验证 `minimatch` v9+ 与 Node.js 18+（hook 的运行时）的兼容性。claudecode 没有 `package.json`，需要通过内联或 CDN 方式引入。
> 
> 如果 `minimatch` 不可用（如环境限制），降级为简单的 glob 匹配：`*` 匹配任意字符，`**` 匹配任意路径，`?` 匹配单个字符。

## 实现位置

在三个 hook 的各自文件读取环节添加过滤逻辑，**不需要**改服务端。

| Hook | 文件 | 过滤位置 |
|---|---|---|
| opencode | `client/opencode/changez.server.ts` | L372-393 文件读取循环 |
| claudecode | `client/claudecode/changez-hook.js` | L128-140 `handlePostToolUse` |
| cursor | `client/cursor/hooks/report.js` | L44-55 `main` 读取文件 |

### 配置加载流程

每个 hook 启动时按优先级加载配置：

```typescript
// 1. 加载全局配置（如不存在则跳过）
const globalConfig = loadJSON("~/.changez/config.json");

// 2. 向上查找项目级配置（以 .git 为边界）
const projectConfig = findProjectConfig(".changez/config.json");

// 3. 合并配置（从左到右优先级递增）
const config = mergeConfigs(
  defaults,
  globalConfig,
  envVars,
  projectConfig
);

// 4. 合并 .gitignore 模式（始终在最后）
const finalExcludes = config.gitignore
  ? [...config.excludes, ...parseGitignore(projectRoot)]
  : config.excludes;
```

**注意**：第 4 步在配置合并之后执行。如果 `gitignore: true`，`.gitignore` 中的模式会追加到合并后的 `excludes` 末尾。这意味着高优先级配置的 `excludes` 会完全覆盖低优先级，但 `.gitignore` 模式始终会被追加。

## 迁移说明

### 现有配置迁移

**环境变量**：继续支持，无需迁移。

**全局配置**：如果 `~/.changez/config.json` 不存在，使用默认值。

**项目级配置**：如果 `.changez/config.json` 不存在，使用默认值。

### 迁移示例

```bash
# 1. 创建项目级配置
mkdir -p .changez
cat > .changez/config.json << 'EOF'
{
  "url": "http://127.0.0.1:8760",
  "token": "your-token",
  "source": "opencode",
  "gitignore": true,
  "excludes": [
    "data/",
    "node_modules/",
    "*.delta",
    "*.db"
  ]
}
EOF

# 2. 验证配置
# 检查 .changez/config.json 是否生效

# 3. 清理历史数据（可选）
# 如果之前已经创建了 .delta 文件记录，可以手动清理
```

### 注意事项

1. **配置优先级**：项目级配置 > 环境变量 > 全局配置 > 默认值
2. **数组覆盖**：高优先级的 `excludes` 会完全覆盖低优先级的 `excludes`，不会合并
3. **`gitignore` 合并**：如果 `gitignore: true`，`.gitignore` 中的模式会在配置合并之后追加到 `excludes` 末尾，始终有效
4. **降级方案**：如果 `minimatch` 不可用，使用简单的 glob 匹配
