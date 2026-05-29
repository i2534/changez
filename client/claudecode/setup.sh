#!/usr/bin/env bash
# setup.sh — Claude Code Hook 自动安装脚本
#
# 用法：
#   bash setup.sh --url http://127.0.0.1:8760 --token xxx
#   bash setup.sh --daemon                    # 安装 daemon 模式
#   bash setup.sh --project                   # 项目级安装（.claude/ 下）
#   bash setup.sh --claude-dir /path/to/conf  # 自定义 Claude 配置目录
#   bash setup.sh --dry-run                   # 预览模式（不执行写入）
#   bash setup.sh --uninstall                 # 卸载
set -euo pipefail

# ── 配置（命令行 > 环境变量 > 默认值）───────────────────────────
DAEMON_MODE=false
PROJECT_MODE=false
DRY_RUN=false
UNINSTALL=false
CLAUDE_DIR=""

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

# ── 默认值辅助 ─────────────────────────────────────────────────
default() {
  local val="${1:-}"
  local fallback="${2:-}"
  if [ -z "$val" ]; then echo "$fallback"; else echo "$val"; fi
}

if [ $# -eq 0 ]; then
  set -- --help
fi

while [ $# -gt 0 ]; do
  case "$1" in
    --daemon)     DAEMON_MODE=true ;;
    --project)    PROJECT_MODE=true ;;
    --dry-run)    DRY_RUN=true ;;
    --uninstall)  UNINSTALL=true ;;
    --url)        CHANGEZ_URL="$2"; shift ;;
    --token)      CHANGEZ_TOKEN="$2"; shift ;;
    --source)     CHANGEZ_SOURCE="$2"; shift ;;
    --log-file)   CHANGEZ_LOG_FILE="$2"; shift ;;
    --claude-dir)
      CLAUDE_DIR="$2"
      shift
      ;;
    --help|-h)
      echo "用法: bash setup.sh [选项]"
      echo ""
      echo "选项:"
      echo "  --daemon         安装 daemon HTTP 模式（默认 stdin hook 模式）"
      echo "  --project        项目级安装（.claude/ 而非 ~/.claude/）"
      echo "  --claude-dir <路径>  自定义 Claude 配置目录（默认 ~/.claude）"
      echo "  --url <地址>     Changez 服务器地址（默认 http://127.0.0.1:8760）"
      echo "  --token <令牌>   Bearer Token（可选）"
      echo "  --source <名称>  来源标识（默认 claudecode）"
      echo "  --log-file <路径>  日志文件路径（默认 <claude-dir>/changez/changez.log）"
      echo "  --dry-run        预览模式（仅显示将执行的操作）"
      echo "  --uninstall      卸载模式"
      echo "  --help           显示此帮助"
      echo ""
      echo "环境变量（命令行参数优先级更高）:"
      echo "  CHANGEZ_URL      Changez 服务器地址"
      echo "  CHANGEZ_TOKEN    Bearer Token"
      echo "  CHANGEZ_SOURCE   来源标识"
      echo "  CHANGEZ_LOG_FILE 日志文件路径"
      exit 0
      ;;
    *) error "未知参数: $1"; exit 1 ;;
  esac
  shift
done

# ── 解析配置（CLI > 环境变量 > 默认值）─────────────────────────
_ENV_URL="${CHANGEZ_URL:-}"
_ENV_TOKEN="${CHANGEZ_TOKEN:-}"
_ENV_SOURCE="${CHANGEZ_SOURCE:-}"
_ENV_LOG="${CHANGEZ_LOG_FILE:-}"
CHANGEZ_URL=""
CHANGEZ_TOKEN=""
CHANGEZ_SOURCE=""
CHANGEZ_LOG_FILE=""

if [ "$UNINSTALL" = false ]; then
  CHANGEZ_URL="$(default "${_ENV_URL}" "http://127.0.0.1:8760")"
  CHANGEZ_TOKEN="$(default "${_ENV_TOKEN}" "")"
  CHANGEZ_SOURCE="$(default "${_ENV_SOURCE}" "claudecode")"
  CHANGEZ_LOG_FILE="$(default "${_ENV_LOG}" "")"
fi

# ── Python 工具（统一入口，避免重复代码）────────────────────────
run_python_tool() {
  local tool="$1"   # merge | remove-hooks | build-hook-patch
  shift

  python3 -c "
import json, sys, os

def is_changez_hook(hook):
    text = json.dumps(hook).lower()
    return 'changez' in text

def merge_hooks(base_hooks, new_hooks):
    result = base_hooks.copy() if base_hooks else {}
    for event_name, new_matchers in new_hooks.items():
        base_matchers = result.get(event_name, [])
        filtered = [m for m in base_matchers if not is_changez_hook(m)]
        result[event_name] = filtered + new_matchers
    return result

def remove_changez_hooks(hooks):
    result = {}
    for event_name, matchers in hooks.items():
        new_matchers = []
        for matcher in matchers:
            cleaned = {k: v for k, v in matcher.items() if k != 'hooks'}
            if 'hooks' in matcher:
                kept = [h for h in matcher['hooks'] if not is_changez_hook(h)]
                if kept:
                    cleaned['hooks'] = kept
                    new_matchers.append(cleaned)
            elif not is_changez_hook(matcher):
                new_matchers.append(matcher)
        if new_matchers:
            result[event_name] = new_matchers
    return result

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

elif tool == 'remove-hooks':
    file_path = sys.argv[3]
    if os.path.exists(file_path):
        with open(file_path, 'r') as f:
            base = json.load(f)
        result = base.copy()
        if 'hooks' in result:
            cleaned = remove_changez_hooks(result['hooks'])
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

elif tool == 'build-hook-patch':
    import shlex
    hook_path = sys.argv[3]
    url = sys.argv[4]
    token = sys.argv[5]
    source = sys.argv[6]
    daemon = sys.argv[7] == 'true'
    log_file = sys.argv[8]

    env_parts = ['CHANGEZ_URL=' + shlex.quote(url), 'CHANGEZ_SOURCE=' + shlex.quote(source)]
    if token:
        env_parts.append('CHANGEZ_TOKEN=' + shlex.quote(token))
    if log_file:
        env_parts.append('CHANGEZ_LOG_FILE=' + shlex.quote(log_file))
    env_prefix = ' '.join(env_parts)

    if daemon:
        post_cmd = 'curl -sf -X POST -d @- -H \"Content-Type: application/json\" http://127.0.0.1:8761/hook'
    else:
        post_cmd = env_prefix + ' node ' + shlex.quote(hook_path)

    patch = {
        'hooks': {
            'PostToolUse': [{
                'matcher': 'Edit|Write',
                'hooks': [{
                    'type': 'command',
                    'command': post_cmd,
                    'async': True
                }]
            }],
            'SessionStart': [{
                'hooks': [{
                    'type': 'command',
                    'command': post_cmd,
                    'async': True
                }]
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

# ── 确定安装路径 ───────────────────────────────────────────────
if [ -n "$CLAUDE_DIR" ]; then
  CLAUDE_DIR="$(cd "$CLAUDE_DIR" 2>/dev/null && pwd || realpath -m "$CLAUDE_DIR")"
  info "自定义配置目录: ${CLAUDE_DIR}/"
elif [ "$PROJECT_MODE" = true ]; then
  PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
  CLAUDE_DIR="${PROJECT_ROOT}/.claude"
  info "项目级安装: ${CLAUDE_DIR}/"
else
  CLAUDE_DIR="$HOME/.claude"
  info "全局安装: ${CLAUDE_DIR}/"
fi

CHANGEZ_DIR="${CLAUDE_DIR}/changez"
SETTINGS_FILE="${CLAUDE_DIR}/settings.json"

# ── 卸载模式 ───────────────────────────────────────────────────
if [ "$UNINSTALL" = true ]; then
  if [ "$DRY_RUN" = true ]; then
    dry "从 ${SETTINGS_FILE} 移除 changez hooks（合并后）:"
    if [ -f "$SETTINGS_FILE" ]; then
      run_python_tool remove-hooks "$SETTINGS_FILE"
    else
      warn "${SETTINGS_FILE} 不存在，无需移除"
    fi
  else
    if [ -f "$SETTINGS_FILE" ]; then
      run_python_tool remove-hooks "$SETTINGS_FILE" >/dev/null
      ok "已从 ${SETTINGS_FILE} 移除 changez hooks"
    else
      warn "${SETTINGS_FILE} 不存在，无需移除"
    fi
  fi

  if [ "$DRY_RUN" = true ]; then
    dry "rm -rf ${CHANGEZ_DIR}"
  else
    if [ -d "$CHANGEZ_DIR" ]; then
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
    ok "卸载完成！"
  fi
  exit 0
fi

# ── 安装模式 ───────────────────────────────────────────────────
if [ -z "$CHANGEZ_LOG_FILE" ]; then
  CHANGEZ_LOG_FILE="${CHANGEZ_DIR}/changez.log"
fi

HOOK_SRC="${SCRIPT_DIR}/changez-hook.js"
DAEMON_SRC="${SCRIPT_DIR}/changez-daemon.js"

# ── 复制文件 ───────────────────────────────────────────────────
if [ "$DRY_RUN" = true ]; then
  dry "mkdir -p ${CHANGEZ_DIR}"
  dry "cp ${HOOK_SRC} ${CHANGEZ_DIR}/changez-hook.js"
  if [ "$DAEMON_MODE" = true ]; then
    dry "cp ${DAEMON_SRC} ${CHANGEZ_DIR}/changez-daemon.js"
  fi
else
  mkdir -p "$CHANGEZ_DIR"
  cp "$HOOK_SRC" "$CHANGEZ_DIR/changez-hook.js"
  ok "安装 hook 脚本: ${CHANGEZ_DIR}/changez-hook.js"

  if [ "$DAEMON_MODE" = true ]; then
    cp "$DAEMON_SRC" "$CHANGEZ_DIR/changez-daemon.js"
    ok "安装 daemon 脚本: ${CHANGEZ_DIR}/changez-daemon.js"
  fi
fi

# ── 构建并写入 hook 配置 ───────────────────────────────────────
HOOK_PATH="${CHANGEZ_DIR}/changez-hook.js"
HOOK_PATCH="$(run_python_tool build-hook-patch "$HOOK_PATH" "$CHANGEZ_URL" "$CHANGEZ_TOKEN" "$CHANGEZ_SOURCE" "$DAEMON_MODE" "$CHANGEZ_LOG_FILE")"

info "生成 Claude Code hook 配置..."

if [ "$DRY_RUN" = true ]; then
  dry "写入 ${SETTINGS_FILE}（合并后）:"
  run_python_tool merge "$SETTINGS_FILE" "$HOOK_PATCH"
else
  if [ -f "$SETTINGS_FILE" ]; then
    warn "发现现有配置，已自动合并 hooks"
  fi
  run_python_tool merge "$SETTINGS_FILE" "$HOOK_PATCH" >/dev/null
  ok "配置已写入: ${SETTINGS_FILE}"
fi

# ── 完成 ───────────────────────────────────────────────────────
info ""
if [ "$DRY_RUN" = true ]; then
  ok "预览完成。移除 --dry-run 以执行安装。"
else
  ok "安装完成！"
  info ""
  info "验证安装："
  if [ "$DAEMON_MODE" = true ]; then
    info "  1. 启动 daemon: node ${CHANGEZ_DIR}/changez-daemon.js"
    info "  2. 测试: curl http://127.0.0.1:8761/health"
  else
    info "  1. 测试 hook: echo '{\"hook_event_name\":\"SessionStart\",\"cwd\":\"/tmp/test\"}' | node ${CHANGEZ_DIR}/changez-hook.js"
  fi
  info "  2. 运行自动化测试: bash ${SCRIPT_DIR}/test-hook.sh"
fi
