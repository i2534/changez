#!/usr/bin/env bash
# setup.sh — Cursor Hooks 自动安装脚本
#
# 用法：
#   bash setup.sh --url http://127.0.0.1:8760 --token xxx
#   bash setup.sh --dry-run                   # 预览模式
#   bash setup.sh --uninstall                 # 卸载
set -euo pipefail

# ── 配置（命令行 > 环境变量 > 默认值）───────────────────────────
_ENV_URL="${CHANGEZ_URL:-}"
_ENV_TOKEN="${CHANGEZ_TOKEN:-}"
_ENV_SOURCE="${CHANGEZ_SOURCE:-}"
CHANGEZ_URL=""
CHANGEZ_TOKEN=""
CHANGEZ_SOURCE=""
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
    --dry-run)   DRY_RUN=true ;;
    --uninstall) UNINSTALL=true ;;
    --url)       CHANGEZ_URL="$2"; shift ;;
    --token)     CHANGEZ_TOKEN="$2"; shift ;;
    --source)    CHANGEZ_SOURCE="$2"; shift ;;
    --help|-h)
      echo "用法: bash setup.sh [选项]"
      echo ""
      echo "选项:"
      echo "  --url <地址>     Changez 服务器地址（默认 http://127.0.0.1:8760）"
      echo "  --token <令牌>   Bearer Token（可选）"
      echo "  --source <名称>  来源标识（默认 cursor）"
      echo "  --dry-run        预览模式（仅显示将执行的操作）"
      echo "  --uninstall      卸载模式"
      echo "  --help           显示此帮助"
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
  if [ -z "$CHANGEZ_SOURCE" ]; then CHANGEZ_SOURCE="cursor"; fi
fi

# ── Python 工具（统一入口）─────────────────────────────────────
run_python_tool() {
  local tool="$1"   # merge | remove | build-patch
  shift

  python3 -c "
import json, sys, os, shlex

def is_changez_hook(hook):
    text = json.dumps(hook).lower()
    return 'changez' in text

tool = sys.argv[1]
dry_run = sys.argv[2] == 'true'

if tool == 'merge':
    file_path = sys.argv[3]
    patch_str = sys.argv[4]
    patch = json.loads(patch_str)

    def deep_merge(base, override):
        result = base.copy()
        for k, v in override.items():
            if k == 'hooks' and isinstance(result.get(k), dict) and isinstance(v, dict):
                result[k] = merge_hooks(result[k], v)
            elif k in result and isinstance(result[k], dict) and isinstance(v, dict):
                result[k] = deep_merge(result[k], v)
            else:
                result[k] = v
        return result

    def merge_hooks(base_hooks, new_hooks):
        result = base_hooks.copy() if base_hooks else {}
        for event_name, new_items in new_hooks.items():
            base_items = result.get(event_name, [])
            filtered = [m for m in base_items if not is_changez_hook(m)]
            result[event_name] = filtered + new_items
        return result

    if os.path.exists(file_path):
        with open(file_path, 'r') as f:
            base = json.load(f)
        merged = deep_merge(base, patch)
    else:
        merged = patch

    if dry_run:
        print(json.dumps(merged, indent=2, ensure_ascii=False))
    else:
        os.makedirs(os.path.dirname(file_path) or '.', exist_ok=True)
        with open(file_path, 'w') as f:
            json.dump(merged, f, indent=2, ensure_ascii=False)
            f.write('\n')

elif tool == 'remove':
    file_path = sys.argv[3]
    if os.path.exists(file_path):
        with open(file_path, 'r') as f:
            base = json.load(f)
        result = base.copy()
        if 'hooks' in result:
            cleaned = {}
            for event_name, items in result['hooks'].items():
                kept = [m for m in items if not is_changez_hook(m)]
                if kept:
                    cleaned[event_name] = kept
            if cleaned:
                result['hooks'] = cleaned
            else:
                del result['hooks']
        if dry_run:
            print(json.dumps(result, indent=2, ensure_ascii=False))
        else:
            os.makedirs(os.path.dirname(file_path) or '.', exist_ok=True)
            with open(file_path, 'w') as f:
                json.dump(result, f, indent=2, ensure_ascii=False)
                f.write('\n')

elif tool == 'build-patch':
    url = sys.argv[3]
    token = sys.argv[4]
    source = sys.argv[5]

    env_parts = ['CHANGEZ_URL=' + shlex.quote(url), 'CHANGEZ_SOURCE=' + shlex.quote(source)]
    if token:
        env_parts.append('CHANGEZ_TOKEN=' + shlex.quote(token))
    env_prefix = ' '.join(env_parts)

    patch = {
        'version': 1,
        'hooks': {
            'afterFileEdit': [{
                'command': env_prefix + ' node hooks/changez/report.js',
                'timeout': 10
            }],
            'sessionStart': [{
                'command': env_prefix + ' node hooks/changez/session-init.js',
                'timeout': 10
            }]
        }
    }
    print(json.dumps(patch, ensure_ascii=False))
" "$tool" "$DRY_RUN" "$@"
}

# ── 前置检查 ───────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

command -v python3 &>/dev/null || { error "未找到 python3，JSON 合并需要 python3"; exit 1; }

if [ "$UNINSTALL" = false ]; then
  command -v node &>/dev/null || { error "未找到 node，请先安装 Node.js 18+"; exit 1; }
  ok "Node.js $(node --version)"
fi

if [ "$DRY_RUN" = true ]; then
  warn ""
  warn "========== 预览模式（不会写入任何文件）=========="
  warn ""
fi

# ── 确定 Cursor 配置目录 ───────────────────────────────────────
CURSOR_DIR="$HOME/.cursor"
CURSOR_HOOKS_DIR="${CURSOR_DIR}/hooks/changez"
CURSOR_HOOKS_JSON="${CURSOR_DIR}/hooks.json"

info "Cursor 配置目录: ${CURSOR_DIR}/"

# ── 卸载模式 ───────────────────────────────────────────────────
if [ "$UNINSTALL" = true ]; then
  if [ "$DRY_RUN" = true ]; then
    dry "从 ${CURSOR_HOOKS_JSON} 移除 changez hooks（合并后）:"
    if [ -f "$CURSOR_HOOKS_JSON" ]; then
      run_python_tool remove "$CURSOR_HOOKS_JSON"
    else
      warn "${CURSOR_HOOKS_JSON} 不存在，无需移除"
    fi
  else
    if [ -f "$CURSOR_HOOKS_JSON" ]; then
      run_python_tool remove "$CURSOR_HOOKS_JSON" >/dev/null
      ok "已从 ${CURSOR_HOOKS_JSON} 移除 changez hooks"
    else
      warn "${CURSOR_HOOKS_JSON} 不存在，无需移除"
    fi
  fi

  if [ "$DRY_RUN" = true ]; then
    dry "rm -f ${CURSOR_HOOKS_DIR}/report.js ${CURSOR_HOOKS_DIR}/session-init.js"
  else
    removed=false
    for f in report.js session-init.js; do
      if [ -f "${CURSOR_HOOKS_DIR}/$f" ]; then
        rm -f "${CURSOR_HOOKS_DIR}/$f"
        removed=true
      fi
    done
    if [ "$removed" = true ]; then
      ok "已删除 hook 脚本"
    else
      warn "hook 脚本不存在，无需删除"
    fi
  fi

  info ""
  if [ "$DRY_RUN" = true ]; then
    ok "预览完成。移除 --dry-run 以执行卸载。"
  else
    ok "卸载完成！重启 Cursor 使变更生效。"
  fi
  exit 0
fi

# ── 安装模式 ───────────────────────────────────────────────────
# ── 复制文件 ───────────────────────────────────────────────────
if [ "$DRY_RUN" = true ]; then
  dry "mkdir -p ${CURSOR_HOOKS_DIR}"
  dry "cp ${SCRIPT_DIR}/hooks/report.js ${CURSOR_HOOKS_DIR}/report.js"
  dry "cp ${SCRIPT_DIR}/hooks/session-init.js ${CURSOR_HOOKS_DIR}/session-init.js"
else
  mkdir -p "$CURSOR_HOOKS_DIR"
  cp "${SCRIPT_DIR}/hooks/report.js" "${CURSOR_HOOKS_DIR}/report.js"
  cp "${SCRIPT_DIR}/hooks/session-init.js" "${CURSOR_HOOKS_DIR}/session-init.js"
  ok "安装 hook 脚本: ${CURSOR_HOOKS_DIR}/"
fi

# ── 生成 hooks.json ────────────────────────────────────────────
HOOKS_PATCH="$(run_python_tool build-patch "$CHANGEZ_URL" "$CHANGEZ_TOKEN" "$CHANGEZ_SOURCE")"

info "生成 hooks.json..."

if [ "$DRY_RUN" = true ]; then
  dry "写入 ${CURSOR_HOOKS_JSON}（合并后）:"
  run_python_tool merge "$CURSOR_HOOKS_JSON" "$HOOKS_PATCH"
else
  if [ -f "$CURSOR_HOOKS_JSON" ]; then
    warn "发现现有 hooks.json，已自动合并 changez hooks"
  fi
  run_python_tool merge "$CURSOR_HOOKS_JSON" "$HOOKS_PATCH" >/dev/null
  ok "配置已写入: ${CURSOR_HOOKS_JSON}"
fi

# ── 完成 ───────────────────────────────────────────────────────
info ""
if [ "$DRY_RUN" = true ]; then
  ok "预览完成。移除 --dry-run 以执行安装。"
else
  ok "安装完成！"
  info ""
  info "下一步："
  info "  1. 重启 Cursor 使 hooks 生效"
  info "  2. 在 Cursor 中编辑文件，验证 Changez 是否收到快照"
fi
