#!/usr/bin/env bash
# setup.sh — OpenCode 插件自动安装脚本
#
# 用法：
#   bash setup.sh                              # 自动安装
#   bash setup.sh --url http://127.0.0.1:8760 --token xxx
#   bash setup.sh --server-only                # 仅安装 Server 插件
#   bash setup.sh --dry-run                    # 预览模式
#   bash setup.sh --uninstall                  # 卸载
#   bash setup.sh --uninstall --server-only    # 仅卸载 Server 插件
#
# 插件安装到 ~/.opencode/changez/（独立目录，不干扰 OpenCode 自动扫描）
set -euo pipefail

# ── 配置（命令行 > 环境变量 > 默认值）───────────────────────────
_ENV_URL="${CHANGEZ_URL:-}"
_ENV_TOKEN="${CHANGEZ_TOKEN:-}"
_ENV_SOURCE="${CHANGEZ_SOURCE:-}"
CHANGEZ_URL=""
CHANGEZ_TOKEN=""
CHANGEZ_SOURCE=""
SERVER_ONLY=false
DRY_RUN=false
UNINSTALL=false

# ── 颜色 ───────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

info()  { printf "${CYAN}→${NC}  %s\n" "$1"; }
ok()    { printf "${GREEN}✓${NC}    %s\n" "$1"; }
warn()  { printf "${YELLOW}⚠${NC}    %s\n" "$1"; }
error() { printf "${RED}✗${NC}    %s\n" "$1"; }
dry()   { printf "${MAGENTA}[dry-run]${NC}  %s\n" "$1"; }

if [ $# -eq 0 ]; then
  set -- --help
fi

while [ $# -gt 0 ]; do
  case "$1" in
    --server-only) SERVER_ONLY=true ;;
    --dry-run)     DRY_RUN=true ;;
    --uninstall)   UNINSTALL=true ;;
    --url)         CHANGEZ_URL="$2"; shift ;;
    --token)       CHANGEZ_TOKEN="$2"; shift ;;
    --source)      CHANGEZ_SOURCE="$2"; shift ;;
    --help|-h)
      echo "用法: bash setup.sh [选项]"
      echo ""
      echo "选项:"
      echo "  --server-only  仅安装/卸载 Server 插件（不安装/卸载 TUI 插件）"
      echo "  --url <地址>   Changez 服务器地址（默认 http://127.0.0.1:8760）"
      echo "  --token <令牌> Bearer Token（可选）"
      echo "  --source <名称> 来源标识（默认 opencode）"
      echo "  --dry-run      预览模式（仅显示将执行的操作）"
      echo "  --uninstall    卸载模式"
      echo "  --help         显示此帮助"
      echo ""
      echo "环境变量（命令行参数优先级更高）:"
      echo "  CHANGEZ_URL    Changez 服务器地址"
      echo "  CHANGEZ_TOKEN  Bearer Token"
      echo "  CHANGEZ_SOURCE 来源标识"
      exit 0
      ;;
    *) error "未知参数: $1"; exit 1 ;;
  esac
  shift
done

if [ "$UNINSTALL" = false ]; then
  if [ -z "$CHANGEZ_URL" ]; then CHANGEZ_URL="$_ENV_URL"; fi
  if [ -z "$CHANGEZ_URL" ]; then CHANGEZ_URL="http://127.0.0.1:8760"; fi
  if [ -z "$CHANGEZ_TOKEN" ]; then CHANGEZ_TOKEN="$_ENV_TOKEN"; fi
  if [ -z "$CHANGEZ_SOURCE" ]; then CHANGEZ_SOURCE="$_ENV_SOURCE"; fi
  if [ -z "$CHANGEZ_SOURCE" ]; then CHANGEZ_SOURCE="opencode"; fi
fi

# ── JSON 合并工具（python3）────────────────────────────────────
# mode: merge（添加/更新）| remove（卸载）
json_merge() {
  local file="$1"
  local patch="$2"
  local mode="$3"
  python3 -c "
import json, sys, os

file_path = sys.argv[1]
patch_str = sys.argv[2]
dry_run = sys.argv[3] == 'true'
mode = sys.argv[4]

patch = json.loads(patch_str)

def is_changez_entry(item):
    if isinstance(item, list) and len(item) >= 1:
        path = item[0]
        if isinstance(path, str) and 'changez' in path:
            return True
    return False

def deep_merge(base, override):
    result = base.copy()
    for k, v in override.items():
        if k == 'plugin' and isinstance(result.get(k), list) and isinstance(v, list):
            # Remove existing changez entries first, then add new ones
            existing = [item for item in result[k] if not is_changez_entry(item)]
            existing_paths = set()
            for item in existing:
                if isinstance(item, list) and len(item) >= 1:
                    existing_paths.add(item[0])
                elif isinstance(item, str):
                    existing_paths.add(item)
            for new_item in v:
                new_path = new_item[0] if isinstance(new_item, list) else new_item
                if new_path not in existing_paths:
                    existing.append(new_item)
                    existing_paths.add(new_path)
            result[k] = existing
        elif k in result and isinstance(result[k], dict) and isinstance(v, dict):
            result[k] = deep_merge(result[k], v)
        else:
            result[k] = v
    return result

def deep_remove(base):
    result = base.copy()
    for k in list(result.keys()):
        if k == 'plugin' and isinstance(result[k], list):
            result[k] = [item for item in result[k] if not is_changez_entry(item)]
        elif isinstance(result[k], dict):
            result[k] = deep_remove(result[k])
    return result

if os.path.exists(file_path):
    with open(file_path, 'r') as f:
        base = json.load(f)
    if mode == 'remove':
        merged = deep_remove(base)
    else:
        merged = deep_merge(base, patch)
else:
    merged = patch if mode != 'remove' else {}

if dry_run:
    print(json.dumps(merged, indent=2, ensure_ascii=False))
else:
    if merged:
        os.makedirs(os.path.dirname(file_path) or '.', exist_ok=True)
        with open(file_path, 'w') as f:
            json.dump(merged, f, indent=2, ensure_ascii=False)
            f.write('\n')
" "$file" "$patch" "$DRY_RUN" "$mode"
}

# ── 安全构建 JSON patch（python3，防注入）───────────────────────
build_plugin_patch() {
  local url="$1"
  local token="$2"
  local source="$3"
  local plugin_path="$4"

  python3 -c "
import json, sys
patch = {'plugin': [['file://' + sys.argv[4], {'url': sys.argv[1], 'token': sys.argv[2], 'source': sys.argv[3], 'logLevel': 'info'}]]}
print(json.dumps(patch, ensure_ascii=False))
" "$url" "$token" "$source" "$plugin_path"
}

build_tui_patch() {
  local tui_path="$1"
  local url="$2"
  local token="$3"

  python3 -c "
import json, sys
patch = {'plugin': [['file://' + sys.argv[1], {'url': sys.argv[2], 'token': sys.argv[3]}]]}
print(json.dumps(patch, ensure_ascii=False))
" "$tui_path" "$url" "$token"
}

# ── 前置检查 ───────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

command -v python3 &>/dev/null || { error "未找到 python3，JSON 合并需要 python3"; exit 1; }

if [ "$DRY_RUN" = true ]; then
  warn ""
  warn "========== 预览模式（不会写入任何文件）=========="
  warn ""
fi

# ── 安装路径（独立目录，不干扰 OpenCode 自动扫描）─────────────
CHANGEZ_DIR="$HOME/.opencode/changez"
OPENCODE_DIR="$HOME/.config/opencode"

# ── 卸载模式 ───────────────────────────────────────────────────
if [ "$UNINSTALL" = true ]; then
  if [ -f "${OPENCODE_DIR}/opencode.jsonc" ]; then
    CONFIG_FILE="${OPENCODE_DIR}/opencode.jsonc"
  elif [ -f "${OPENCODE_DIR}/opencode.json" ]; then
    CONFIG_FILE="${OPENCODE_DIR}/opencode.json"
  else
    CONFIG_FILE="${OPENCODE_DIR}/opencode.json"
  fi

  # Server 配置（始终移除）
  if [ "$DRY_RUN" = true ]; then
    dry "从 ${CONFIG_FILE} 移除 changez 条目（合并后）:"
    json_merge "$CONFIG_FILE" "{}" "remove"
  else
    if [ -f "$CONFIG_FILE" ]; then
      json_merge "$CONFIG_FILE" "{}" "remove" >/dev/null
      ok "已从 ${CONFIG_FILE} 移除 changez 条目"
    else
      warn "${CONFIG_FILE} 不存在，无需移除"
    fi
  fi

  # TUI 配置（--server-only 时保留）
  if [ "$SERVER_ONLY" = false ]; then
    if [ "$DRY_RUN" = true ]; then
      dry "从 ${OPENCODE_DIR}/tui.json 移除 changez 条目（合并后）:"
      json_merge "${OPENCODE_DIR}/tui.json" "{}" "remove"
    else
      if [ -f "${OPENCODE_DIR}/tui.json" ]; then
        json_merge "${OPENCODE_DIR}/tui.json" "{}" "remove" >/dev/null
        ok "已从 tui.json 移除 changez 条目"
      else
        warn "tui.json 不存在，无需移除"
      fi
    fi
  fi

  # 文件清理
  if [ "$DRY_RUN" = true ]; then
    if [ "$SERVER_ONLY" = true ] && [ -f "${CHANGEZ_DIR}/changez.tui.tsx" ]; then
      dry "rm -f ${CHANGEZ_DIR}/changez.server.ts"
    else
      dry "rm -rf ${CHANGEZ_DIR}"
    fi
  else
    if [ "$SERVER_ONLY" = true ] && [ -f "${CHANGEZ_DIR}/changez.tui.tsx" ]; then
      rm -f "${CHANGEZ_DIR}/changez.server.ts"
      ok "已删除 ${CHANGEZ_DIR}/changez.server.ts（保留 TUI 插件）"
    elif [ -d "$CHANGEZ_DIR" ]; then
      rm -rf "$CHANGEZ_DIR"
      ok "已删除 ${CHANGEZ_DIR}"
    else
      warn "${CHANGEZ_DIR} 不存在，无需删除"
    fi
  fi

  info ""
  if [ "$DRY_RUN" = true ]; then
    ok "预览完成。移除 --dry-run 以执行卸载。"
  else
    ok "卸载完成！重启 OpenCode 使变更生效。"
  fi
  exit 0
fi

# ── 安装模式 ───────────────────────────────────────────────────
info "安装路径: ${CHANGEZ_DIR}/"
info "配置文件: ${OPENCODE_DIR}/"

# ── 复制 Server 插件 ───────────────────────────────────────────
if [ "$DRY_RUN" = true ]; then
  dry "mkdir -p ${CHANGEZ_DIR}"
  dry "cp ${SCRIPT_DIR}/changez.server.ts ${CHANGEZ_DIR}/changez.server.ts"
else
  mkdir -p "$CHANGEZ_DIR"
  cp "${SCRIPT_DIR}/changez.server.ts" "${CHANGEZ_DIR}/changez.server.ts"
  ok "安装 Server 插件: ${CHANGEZ_DIR}/changez.server.ts"
fi

# ── 复制 TUI 插件 ──────────────────────────────────────────────
if [ "$SERVER_ONLY" = false ]; then
  if [ "$DRY_RUN" = true ]; then
    dry "cp ${SCRIPT_DIR}/changez.tui.tsx ${CHANGEZ_DIR}/changez.tui.tsx"
  else
    cp "${SCRIPT_DIR}/changez.tui.tsx" "${CHANGEZ_DIR}/changez.tui.tsx"
    ok "安装 TUI 插件: ${CHANGEZ_DIR}/changez.tui.tsx"
  fi
fi

# ── 确定配置文件 ───────────────────────────────────────────────
if [ -f "${OPENCODE_DIR}/opencode.jsonc" ]; then
  CONFIG_FILE="${OPENCODE_DIR}/opencode.jsonc"
elif [ -f "${OPENCODE_DIR}/opencode.json" ]; then
  CONFIG_FILE="${OPENCODE_DIR}/opencode.json"
else
  CONFIG_FILE="${OPENCODE_DIR}/opencode.json"
fi

# ── 写入 Server 插件配置 ───────────────────────────────────────
ABS_SERVER_PATH="${CHANGEZ_DIR}/changez.server.ts"
PLUGIN_PATCH="$(build_plugin_patch "$CHANGEZ_URL" "$CHANGEZ_TOKEN" "$CHANGEZ_SOURCE" "$ABS_SERVER_PATH")"

info "生成 Server 插件配置..."
if [ "$DRY_RUN" = true ]; then
  dry "写入 ${CONFIG_FILE}（合并后）:"
  json_merge "$CONFIG_FILE" "$PLUGIN_PATCH" "merge"
else
  if [ -f "$CONFIG_FILE" ]; then
    warn "发现现有配置，已自动合并 changez 插件"
  fi
  json_merge "$CONFIG_FILE" "$PLUGIN_PATCH" "merge" >/dev/null
  ok "配置已写入: ${CONFIG_FILE}"
fi

# ── 写入 TUI 插件配置 ──────────────────────────────────────────
if [ "$SERVER_ONLY" = false ]; then
  TUI_CONFIG_FILE="${OPENCODE_DIR}/tui.json"
  ABS_TUI_PATH="${CHANGEZ_DIR}/changez.tui.tsx"

  TUI_PATCH="$(build_tui_patch "$ABS_TUI_PATH" "$CHANGEZ_URL" "$CHANGEZ_TOKEN")"

  info "生成 TUI 插件配置..."
  if [ "$DRY_RUN" = true ]; then
    dry "写入 ${TUI_CONFIG_FILE}（合并后）:"
    json_merge "$TUI_CONFIG_FILE" "$TUI_PATCH" "merge"
  else
    if [ -f "$TUI_CONFIG_FILE" ]; then
      warn "发现现有 tui.json，已自动合并 changez TUI 插件"
    fi
    json_merge "$TUI_CONFIG_FILE" "$TUI_PATCH" "merge" >/dev/null
    ok "TUI 配置已写入: ${TUI_CONFIG_FILE}"
  fi
fi

# ── 完成 ───────────────────────────────────────────────────────
info ""
if [ "$DRY_RUN" = true ]; then
  ok "预览完成。移除 --dry-run 以执行安装。"
else
  ok "安装完成！"
  info ""
  info "下一步："
  info "  1. 重启 OpenCode 使插件生效"
  info "  2. 查看日志确认插件加载: \"changez loaded (${CHANGEZ_URL}, ...)\""
  info "  3. 执行一次文件编辑，验证 Changez 是否收到快照"
fi
