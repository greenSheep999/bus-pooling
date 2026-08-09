#!/usr/bin/env bash
# tests/e2e/real-crash-window.sh · 真崩溃窗口 e2e
#
# 跟 run-e2e.sh step 4 的区别（审计准确指出的那个差异）：
#   - run-e2e.sh step 4：手工造 4 种 pending 状态·SIGKILL·看 janitor 兜。
#     证明的是"janitor 扫到卡在 X 状态时能做对"·**不证明**"业务真跑到 X 时崩溃后能恢复"。
#   - 本脚本：DryRunVendor 加 3 秒延迟·业务真发起拉号·1 秒后 SIGKILL·
#     进程死在 purchasing 状态中间（vendor 请求已发·响应未回·台账未落）。
#     重启后·看 janitor 能否恢复到 completed 或 need_manual。
#
# 这才是"业务真跑到某一步时精确崩溃"的窗口测试。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BIN=${BP_CRASH_BIN:-/tmp/bp-crash}
DB=${BP_CRASH_DB:-/tmp/bp-crash.db}
PORT=${BP_CRASH_PORT:-18092}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-crash.log
COOKIES=/tmp/bp-crash-cookies.txt

pass=0
fail=0
banner() { printf "\n== %s ==\n" "$1"; }
ok()     { printf "  ✅ %s\n" "$1"; pass=$((pass+1)); }
ko()     { printf "  ❌ %s\n" "$1"; fail=$((fail+1)); }

cleanup() {
  local p
  p=$(lsof -i ":$PORT" -sTCP:LISTEN -P 2>/dev/null | awk 'NR==2 {print $2}' || true)
  if [ -n "${p:-}" ]; then kill "$p" 2>/dev/null || true; sleep 0.3; fi
  rm -f "$DB" "${DB}-wal" "${DB}-shm" "$COOKIES"
}
trap cleanup EXIT

gen_key() { python3 -c "import secrets;print(secrets.token_hex(16))"; }

# ── 起服务 · DryRunVendor Purchase 前 sleep 3s ──
banner "step 0 · 起服务（BP_DRY_RUN_PURCHASE_DELAY_MS=3000）"
go build -o "$BIN" ./cmd/bus-pooling
export BP_MASTER_KEY=${BP_MASTER_KEY:-18791c18de60833ca343712a98adf7cc2822bdd0d4f878aceed8bf9e96e277e9}
export BP_DB_PATH="$DB"
export BP_ADDR=":$PORT"
export BP_INSECURE_COOKIE=1
export DRY_RUN=1
export BP_ENABLE_DEV_TOPUP=1
export BP_DRY_RUN_PURCHASE_DELAY_MS=3000

rm -f "$DB" "${DB}-wal" "${DB}-shm" "$LOG"
"$BIN" migrate up >/dev/null
"$BIN" serve >"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
if ! curl -sSf -o /dev/null "$BASE/healthz"; then
  echo "!! 服务没起来"; tail -30 "$LOG"; exit 1
fi
ok "服务起来 pid=$BP_PID (Purchase 会 sleep 3s)"

# ── 注册 + 充值 ──
banner "step 1 · 注册 + 充值 500 积分"
curl -sSf -c "$COOKIES" -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"crash@e.local","username":"crashuser","password":"12345678"}' >/dev/null

ORDER=$(curl -sSf -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":500000000,"channel":"waffo"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["order_id"])')
curl -sSf -b "$COOKIES" -X POST "$BASE/api/internal/topup/$ORDER/paid" >/dev/null
ok "余额 500 积分"

# ── 真崩溃窗口 · POST /me/pull 发起 · 1s 后 SIGKILL ──
banner "step 2 · 真崩溃窗口 · pull 中间 SIGKILL"
IDEM=$(gen_key)
# 后台发拉号 · 会 sleep 3s · 拿不到响应
(curl -sS -o /tmp/bp-crash-pull.txt -w "%{http_code}" -b "$COOKIES" -X POST "$BASE/api/me/pull" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: $IDEM" \
  -d '{"count":2}' > /tmp/bp-crash-pull-status.txt 2>/dev/null || true) &
PULL_PID=$!

# 轮询 · 等到 pending_purchase 出现·最多 2s
set +e
pp_before=""
for i in 1 2 3 4 5 6 7 8; do
  sleep 0.25
  pp_before=$(sqlite3 "$DB" "SELECT status FROM pending_purchase LIMIT 1;" 2>/dev/null)
  [ -n "$pp_before" ] && break
done
set -e
if [ -n "${pp_before:-}" ]; then
  ok "崩溃前 pending_purchase 存在·status=${pp_before}（业务真跑到中间）"
else
  ko "崩溃前 pending_purchase 不存在·可能 sleep 太短或状态机异常"
  tail -20 "$LOG"
  exit 1
fi

# **精确 SIGKILL** · 业务正在 Purchase sleep 中
kill -9 "$BP_PID" 2>/dev/null || true
sleep 0.3
if kill -0 "$BP_PID" 2>/dev/null; then ko "bp 没死"; else ok "bp 已 SIGKILL（业务卡在 $pp_before 状态）"; fi

# 等 curl 也失败（连接被 reset）
wait "$PULL_PID" 2>/dev/null || true
set +e
curl_status=$(cat /tmp/bp-crash-pull-status.txt 2>/dev/null)
set -e
echo "  curl HTTP=${curl_status:-connection-reset}（预期非 200·连接被 reset）"

# ── 重启 · 看 janitor 兜 ──
banner "step 3 · 重启 · 等 janitor 恢复"
"$BIN" serve >>"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
if ! curl -sSf -o /dev/null "$BASE/healthz"; then
  ko "重启后服务未就绪"; tail -20 "$LOG"; exit 1
fi

# 该状态可能是 reserved 或 purchasing · 各自超时不同
# reserved 60s · purchasing 30s · 我方造 crash 时状态在 <1s · 需 UPDATE 让它超时
# 手工把 updated_at 推早·免得等 30-60 秒
sqlite3 "$DB" "UPDATE pending_purchase SET updated_at='2020-01-01T00:00:00.000Z';"

# 等 janitor 一轮（15s + 处理时间）
sleep 20

# 断言：pending_purchase 已不再卡在中间态
pp_after_cnt=$(sqlite3 "$DB" "SELECT count(1) FROM pending_purchase;" 2>/dev/null || echo 0)
if [ "${pp_after_cnt:-0}" = "0" ]; then
  ok "janitor delete 行（initial 分支或 reserved 释放）"
else
  pp_after=$(sqlite3 "$DB" "SELECT status FROM pending_purchase LIMIT 1;" 2>/dev/null || echo "unknown")
  case "${pp_after:-unknown}" in
    completed|need_manual|need_recover_vendor|cancelled_reserve)
      ok "janitor 把业务真崩溃行推到 $pp_after"
      ;;
    initial|reserved|purchasing|purchased|imported)
      ko "janitor 未处理·仍卡 ${pp_after:-?}"
      tail -30 "$LOG" | grep -E "janitor|recover" | head -10
      ;;
    *)
      ok "janitor 推到 ${pp_after:-?}"
      ;;
  esac
fi

# 断言：钱包也没超扣（reserved 释放 or 拉号成功一次·两种都可接受）
bal=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])' 2>/dev/null || echo 0)
if [ "${bal:-0}" -le "500000000" ] && [ "${bal:-0}" -ge "440000000" ]; then
  ok "余额 ${bal:-0} 微单位（440M-500M 之间·未超扣）"
else
  ko "余额 ${bal:-0} 异常"
fi

# ── 汇总 ────
banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：$LOG"
  tail -20 "$LOG"
  exit 1
fi
echo "  ✅ 真崩溃窗口 · 业务在 pull 中间 SIGKILL · janitor 恢复"
