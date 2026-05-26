#!/usr/bin/env bash
set -euo pipefail

CHANGEZ_URL="${CHANGEZ_URL:-http://127.0.0.1:8760}"
CHANGEZ_TOKEN="${CHANGEZ_TOKEN:-}"
HOOK_SCRIPT="${HOOK_SCRIPT:-client/claude-code/changez-hook.js}"
TEST_LOG="/tmp/changez-hook-test-$$.log"
TEST_PROJECT="/tmp/changez-hook-test-$$"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); printf "${GREEN}✓ PASS${NC}  %s\n" "$1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); printf "${RED}✗ FAIL${NC}  %s\n" "$1"; [ -n "${2:-}" ] && printf "       %s\n" "$2"; }
warn() { printf "${YELLOW}⚠ WARN${NC}  %s\n" "$1"; }

run_hook() {
    local input="$1"
    local extra_url="${2:-}"
    local env_prefix=""
    
    [ -n "$CHANGEZ_TOKEN" ] && env_prefix="CHANGEZ_TOKEN=$CHANGEZ_TOKEN"
    [ -n "$extra_url" ] && env_prefix="${env_prefix:+$env_prefix }CHANGEZ_URL=$extra_url"
    env_prefix="${env_prefix:+$env_prefix }CHANGEZ_LOG_FILE=$TEST_LOG"
    
    if [ -n "$env_prefix" ]; then
        env $env_prefix node "$HOOK_SCRIPT" <<< "$input" 2>&1
    else
        node "$HOOK_SCRIPT" <<< "$input" 2>&1
    fi
}

run_hook_exit() {
    local input="$1"
    local extra_url="${2:-}"
    local env_prefix=""
    
    [ -n "$CHANGEZ_TOKEN" ] && env_prefix="CHANGEZ_TOKEN=$CHANGEZ_TOKEN"
    [ -n "$extra_url" ] && env_prefix="${env_prefix:+$env_prefix }CHANGEZ_URL=$extra_url"
    
    if [ -n "$env_prefix" ]; then
        env $env_prefix node "$HOOK_SCRIPT" <<< "$input" >/dev/null 2>&1
    else
        node "$HOOK_SCRIPT" <<< "$input" >/dev/null 2>&1
    fi
    return $?
}

api_get() {
    if [ -n "$CHANGEZ_TOKEN" ]; then
        curl -sf -H "Authorization: Bearer $CHANGEZ_TOKEN" "${CHANGEZ_URL}$1" 2>/dev/null || echo ""
    else
        curl -sf "${CHANGEZ_URL}$1" 2>/dev/null || echo ""
    fi
}

json_val() {
    python3 -c "import sys,json; print(json.load(sys.stdin)$1)" 2>/dev/null || echo "$2"
}

rm -f "$TEST_LOG"
trap 'rm -f "$TEST_LOG"' EXIT

echo "╔══════════════════════════════════════════════════════╗"
echo "║     changez-hook 自动化测试                         ║"
echo "╠══════════════════════════════════════════════════════╣"
echo "║  URL:    $CHANGEZ_URL"
echo "║  Token:  ${CHANGEZ_TOKEN:-(empty)}"
echo "║  Script: $HOOK_SCRIPT"
echo "╚══════════════════════════════════════════════════════╝"

# Prereqs
command -v node &>/dev/null || { echo "ERROR: node not found"; exit 1; }
[ -f "$HOOK_SCRIPT" ] || { echo "ERROR: hook script not found: $HOOK_SCRIPT"; exit 1; }
HEALTH=$(curl -sf "${CHANGEZ_URL}/health" 2>/dev/null || echo "")
[ "$HEALTH" = '{"status":"ok"}' ] || { echo "ERROR: changez not reachable at $CHANGEZ_URL"; exit 1; }
if [ -n "$CHANGEZ_TOKEN" ]; then
    STATS_CHECK=$(api_get "/api/stats")
    [ -n "$STATS_CHECK" ] || { echo "ERROR: token auth failed"; exit 1; }
fi
echo ""
echo "✓ 前置检查通过"

# Clean log before tests
rm -f "$TEST_LOG"

# Baseline
STATS=$(api_get "/api/stats")
BASELINE_CLAUDE=0
BASELINE_VERSIONS=0
if [ -n "$STATS" ]; then
    BASELINE_CLAUDE=$(echo "$STATS" | json_val "['sources'].get('claude-code',0)" "0")
    BASELINE_VERSIONS=$(echo "$STATS" | json_val "['versions']" "0")
fi

echo ""
echo "=== 层 1: 核心功能 ==="

# T1: SessionStart
echo ""
echo "--- T1: SessionStart 项目注册 ---"
OUTPUT=$(run_hook "{\"hook_event_name\":\"SessionStart\",\"cwd\":\"$TEST_PROJECT\"}")
sleep 0.5
PROJECTS=$(api_get "/api/projects")
if echo "$PROJECTS" | grep -q "$TEST_PROJECT" 2>/dev/null; then
    pass "SessionStart → 项目注册成功"
elif [ -z "$CHANGEZ_TOKEN" ]; then
    warn "SessionStart → 无 token，跳过验证"
else
    fail "SessionStart → 项目未找到"
fi

# T2: Write
echo ""
echo "--- T2: Write 工具创建 snapshot ---"
OUTPUT=$(run_hook "{\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"$TEST_PROJECT/hello.txt\",\"content\":\"Hello World from Changez\"},\"tool_response\":{\"type\":\"create\"},\"session_id\":\"test-auto-001\"}")
if echo "$OUTPUT" | grep -qi "error\|fail"; then
    fail "Write → 错误输出: $OUTPUT"
else
    pass "Write → exit 0, 无错误"
fi

# T3: Edit
echo ""
echo "--- T3: Edit 工具更新 snapshot ---"
OUTPUT=$(run_hook "{\"hook_event_name\":\"PostToolUse\",\"tool_name\":\"Edit\",\"tool_input\":{\"file_path\":\"$TEST_PROJECT/hello.txt\",\"old_string\":\"Hello\",\"new_string\":\"Hello Changez\"},\"tool_response\":{\"originalFile\":\"Hello Changez World from Changez\",\"type\":\"update\"},\"session_id\":\"test-auto-001\"}")
if echo "$OUTPUT" | grep -qi "error\|fail"; then
    fail "Edit → 错误输出: $OUTPUT"
else
    pass "Edit → exit 0, 无错误"
fi

# T4: Data verification
echo ""
echo "--- T4: Snapshot 数据验证 ---"
sleep 0.5
STATS_AFTER=$(api_get "/api/stats")
if [ -n "$STATS_AFTER" ]; then
    CLAUDE_AFTER=$(echo "$STATS_AFTER" | json_val "['sources'].get('claude-code',0)" "0")
    DIFF=$((CLAUDE_AFTER - BASELINE_CLAUDE))
    if [ "$DIFF" -ge 2 ]; then
        pass "Snapshot 数据 → claude-code 新增 $DIFF 条"
    else
        fail "Snapshot 数据 → 预期 ≥2 条新增，实际 $DIFF 条"
    fi
else
    warn "无法查询 stats，跳过数据验证"
fi

echo ""
echo "=== 层 2: 边界场景 ==="

# T5: Non-Write/Edit tool
echo ""
echo "--- T5: 非 Write/Edit 工具跳过 ---"
OUTPUT=$(run_hook '{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"session_id":"test"}')
if [ -z "$OUTPUT" ]; then
    pass "Bash 工具 → 静默跳过"
else
    fail "Bash 工具 → 应有静默输出: $OUTPUT"
fi

# T6: Empty stdin
echo ""
echo "--- T6: 空 stdin ---"
EXIT_CODE=0
echo "" | node "$HOOK_SCRIPT" >/dev/null 2>&1 || EXIT_CODE=$?
if [ "$EXIT_CODE" -eq 0 ]; then
    pass "空 stdin → exit 0"
else
    fail "空 stdin → exit $EXIT_CODE"
fi

# T7: Invalid JSON
echo ""
echo "--- T7: 无效 JSON ---"
EXIT_CODE=0
echo "not-json{{{" | node "$HOOK_SCRIPT" >/dev/null 2>&1 || EXIT_CODE=$?
if [ "$EXIT_CODE" -eq 0 ]; then
    pass "无效 JSON → exit 0"
else
    fail "无效 JSON → exit $EXIT_CODE"
fi

# T8: Service unreachable
echo ""
echo "--- T8: 服务不可达（重试后 exit 0）---"
EXIT_CODE=0
(
  export CHANGEZ_TOKEN
  timeout 15 env CHANGEZ_URL=http://127.0.0.1:59999 CHANGEZ_TOKEN="$CHANGEZ_TOKEN" node "$HOOK_SCRIPT" <<< '{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"/tmp/x.txt","content":"x"},"tool_response":{"type":"create"},"session_id":"s"}' >/dev/null 2>&1
) || EXIT_CODE=$?
if [ "$EXIT_CODE" -eq 0 ]; then
    pass "服务不可达 → exit 0（fire-and-forget）"
else
    fail "服务不可达 → exit $EXIT_CODE"
fi

# T9: No file_path
echo ""
echo "--- T9: 无 file_path ---"
OUTPUT=$(run_hook '{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"content":"no path"},"tool_response":{"type":"create"},"session_id":"s"}')
if [ -z "$OUTPUT" ]; then
    pass "无 file_path → 静默跳过"
else
    fail "无 file_path → 应有静默输出: $OUTPUT"
fi

# T10: Log file verification
echo ""
echo "--- T10: 日志文件验证 ---"
if [ -f "$TEST_LOG" ]; then
    HAS_SKIP=$(grep -c "skip tool" "$TEST_LOG" || true)
    HAS_REPORTING=$(grep -c "reporting:" "$TEST_LOG" || true)
    if [ "$HAS_SKIP" -gt 0 ] 2>/dev/null && [ "$HAS_REPORTING" -gt 0 ] 2>/dev/null; then
        pass "日志文件 → 包含 skip 和 reporting 记录"
    else
        fail "日志文件 → skip=$HAS_SKIP, reporting=$HAS_REPORTING"
    fi
else
    warn "日志文件不存在，跳过验证"
fi

# Summary
echo ""
echo "═══════════════════════════════════════════════════════"
if [ "$FAIL" -eq 0 ]; then
    printf "${GREEN}全部通过${NC} — $TOTAL/$TOTAL 测试通过\n"
else
    printf "测试完成 — $PASS/$TOTAL 通过, ${RED}$FAIL 失败${NC}\n"
fi
echo "═══════════════════════════════════════════════════════"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
