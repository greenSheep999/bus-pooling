#!/usr/bin/env bash
# tests/e2e/run-e2e.sh · 阶段 1a DoD 的一键 E2E
#
# 三组：
#   1. 幂等重放 · 同 X-Idempotency-Key 拉 5 次 · 只扣 1 次
#   2. 5 并发拉号 · 5 个不同幂等键并发 · 余额扣 5 次不超扣
#   3. Kill 恢复 · 拉号中途 SIGKILL 后端 · 重启后 janitor 应能把
#      pending_purchase 推进到 completed 或 need_manual（不能一直卡 pending）
#
# 默认 DRY_RUN=1（走 mock vendor + mock pool · 不花真钱）。
# 通过标准：全部 3 组 ✅ · exit 0；任一失败 exit 1
#
# 用法：
#   bash tests/e2e/run-e2e.sh                # 用 /tmp/bp-e2e.db
#   BP_E2E_DB=/tmp/x.db bash tests/e2e/run-e2e.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# ── 变量 ──────────────────────────────────────
BIN=${BP_E2E_BIN:-/tmp/bp-e2e}
DB=${BP_E2E_DB:-/tmp/bp-e2e.db}
PORT=${BP_E2E_PORT:-18081}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-e2e.log
COOKIES=/tmp/bp-e2e-cookies.txt
# 32 位 hex 幂等键
IDEM_1="e2e00000000000000000000000000001"
IDEM_2="e2e00000000000000000000000000002"

pass=0
fail=0

banner() { printf "\n== %s ==\n" "$1"; }
ok()     { printf "  ✅ %s\n" "$1"; pass=$((pass+1)); }
ko()     { printf "  ❌ %s\n" "$1"; fail=$((fail+1)); }

cleanup() {
  local pid
  pid=$(lsof -i ":$PORT" -sTCP:LISTEN -P 2>/dev/null | awk 'NR==2 {print $2}' || true)
  if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; sleep 0.3; fi
  rm -f "$DB" "${DB}-wal" "${DB}-shm" "$COOKIES"
}
trap cleanup EXIT

# 生成 hex idempotency key
gen_key() {
  python3 -c "import secrets;print(secrets.token_hex(16))"
}

# ── 步骤 0 · 编译 + 起服务 ────────────────────
banner "step 0 · 编译 + 起服务"
go build -o "$BIN" ./cmd/bus-pooling
export BP_MASTER_KEY=${BP_MASTER_KEY:-18791c18de60833ca343712a98adf7cc2822bdd0d4f878aceed8bf9e96e277e9}
export BP_DB_PATH="$DB"
export BP_ADDR=":$PORT"
export BP_INSECURE_COOKIE=1
export DRY_RUN=1
export BP_ENABLE_DEV_TOPUP=1

rm -f "$DB" "${DB}-wal" "${DB}-shm" "$LOG"
"$BIN" migrate up >/dev/null

# 后台起 · $! 抓 pid
"$BIN" serve >"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
if ! curl -sSf -o /dev/null "$BASE/healthz"; then
  echo "!! 服务没起来"
  tail -30 "$LOG"
  exit 1
fi
ok "服务起来了 pid=$BP_PID"

# ── 步骤 1 · 注册 + 充值 · 走 dev endpoint 真跑完整链路 ──
banner "step 1 · 注册 + 充值 500 积分"

curl -sSf -c "$COOKIES" -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"e2e@x.local","username":"e2euser","password":"12345678"}' >/dev/null
ok "注册 + 登录"

ORDER=$(curl -sSf -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":500000000,"channel":"waffo"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["order_id"])')

# dev endpoint mark paid · 走完 wallet_ledger 写两条（recharge + channel_fee）
curl -sSf -b "$COOKIES" -X POST "$BASE/api/internal/topup/$ORDER/paid" >/dev/null

bal=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')
if [ "$bal" = "500000000" ]; then ok "余额 500 积分"; else ko "余额期望 500000000·实际 $bal"; fi

# ── 步骤 2 · 幂等重放 · 同 key 拉 5 次 ────────
banner "step 2 · 幂等重放 (同 key × 5 · 只扣 1 次)"
before=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')

first_body=""
for i in 1 2 3 4 5; do
  body=$(curl -sS -b "$COOKIES" -X POST "$BASE/api/me/pull" \
    -H "Content-Type: application/json" \
    -H "X-Idempotency-Key: $IDEM_1" \
    -d '{"count":2}')
  if [ -z "$first_body" ]; then
    first_body="$body"
    continue
  fi
  if [ "$body" != "$first_body" ]; then
    ko "第 $i 次响应跟首次不一致"
    echo "first: $first_body"; echo "now:   $body"
    break
  fi
done
[ "$fail" -eq 0 ] && ok "5 次响应字节一致"

after=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')
delta=$((before - after))
# DRY_RUN 下 unit_price=30_000_000·2 号 = 60_000_000
expected=60000000
if [ "$delta" -eq "$expected" ]; then ok "只扣一次 · $delta 微单位"; else ko "delta 期望 $expected·实际 $delta"; fi

# ── 步骤 3 · 5 并发拉号 · 5 个不同 key（余额充足场景）────────
banner "step 3 · 5 并发拉号 (不同 key · 应各扣一次)"
before=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')

pids=()
for i in 1 2 3 4 5; do
  key=$(gen_key)
  curl -sS -b "$COOKIES" -X POST "$BASE/api/me/pull" \
    -H "Content-Type: application/json" \
    -H "X-Idempotency-Key: $key" \
    -d '{"count":1}' >/tmp/bp-e2e-c$i.json &
  pids+=($!)
done
for p in "${pids[@]}"; do wait "$p" || true; done

after=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')
delta=$((before - after))
# 5 × 30_000_000 = 150_000_000（单价 · 1 号）· DRY_RUN vendor
expected=150000000
if [ "$delta" -eq "$expected" ]; then ok "并发 5 次各扣一次 · $delta"; else ko "delta 期望 $expected·实际 $delta（超扣或漏扣）"; fi

# ── 步骤 3b · 余额不足并发 · 真资金竞争（不能超扣）──────────
banner "step 3b · 余额不足并发 (资金竞争·不能超扣不能漏扣)"
# 场景：wallet 剩余不够抢·10 goroutine 每个要 30_000_000·wallet 只够 3 次成功
# 造场景：先把 wallet SQL 直接压到 90_000_000（= 3 次单价）
sqlite3 "$DB" "UPDATE wallet SET balance=90000000 WHERE passenger_id IN (SELECT id FROM passenger LIMIT 1);"

before=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')
if [ "$before" != "90000000" ]; then
  ko "预置余额失败 · got=$before want=90000000"
else
  # 10 goroutine 各拉 1 号（30_000_000）· 只能 3 个成功
  rm -f /tmp/bp-e2e-r*.txt
  pids=()
  for i in $(seq 1 10); do
    key=$(gen_key)
    (curl -sS -o /dev/null -w "%{http_code}\n" -b "$COOKIES" -X POST "$BASE/api/me/pull" \
       -H "Content-Type: application/json" \
       -H "X-Idempotency-Key: $key" \
       -d '{"count":1}' > /tmp/bp-e2e-r$i.txt) &
    pids+=($!)
  done
  for p in "${pids[@]}"; do wait "$p" || true; done

  # 统计成功数（HTTP 200）· 失败数（HTTP 402 insufficient_balance）
  success=$(cat /tmp/bp-e2e-r*.txt 2>/dev/null | grep -c "^200$" || echo 0)
  failed=$(cat /tmp/bp-e2e-r*.txt 2>/dev/null | grep -c "^402$" || echo 0)
  after=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')

  # 断言：成功数 = 3 · 失败数 = 7 · 余额 = 0（3 × 30_000_000 恰好扣光）
  if [ "$success" = "3" ] && [ "$failed" = "7" ] && [ "$after" = "0" ]; then
    ok "资金竞争无超扣 · 成功 3 · 失败 7 · 余额 0"
  else
    ko "资金竞争异常 · 成功=$success (期望 3) · 失败=$failed (期望 7) · 余额=$after (期望 0)"
  fi
fi

# ── 步骤 4 · Kill 恢复 ────────────────────────
banner "step 4 · Kill 恢复 (SIGKILL → 重启 → janitor 兜)"
# 手工插 idempotency_record + pending_purchase(initial) · 模拟 crash 前刚 create
# 用真实经过的拉号 · 会有 idempotency_record 已存在·挑一条不重要的当锚
pid=$(sqlite3 "$DB" "SELECT id FROM passenger LIMIT 1;")
sqlite3 "$DB" <<SQL
# 用 2020 年·远比 janitor 的任何超时都久·必被扫
INSERT INTO idempotency_record (id, passenger_id, method, path, idempotency_key,
  request_fingerprint, created_at)
VALUES ('e2e-idem-crash', '$pid', 'POST', '/api/me/pull', 'e2ecrash000000000000000000000000',
  'e2ecrash-fingerprint', '2020-01-01T00:00:00Z');
INSERT INTO pending_purchase (id, passenger_id, idempotency_record_id, target_group, vendor_id,
  count_requested, reserved_amount, client_order_id, status, created_at, updated_at)
VALUES ('e2e-crash-1', '$pid', 'e2e-idem-crash', 'record-$pid', 'kiro91', 1, 30000000,
  'e2ecrash00000000000000000000e2ec', 'initial',
  '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z');
SQL

# SIGKILL bp
kill -9 "$BP_PID" 2>/dev/null || true
sleep 0.3
if kill -0 "$BP_PID" 2>/dev/null; then ko "bp 没死"; else ok "bp 已 SIGKILL"; fi

# 重启
"$BIN" serve >>"$LOG" 2>&1 &
BP_PID=$!
sleep 1.5
if ! curl -sSf -o /dev/null "$BASE/healthz"; then
  ko "重启后服务未就绪"
  tail -20 "$LOG"; exit 1
fi

# 等 janitor 扫（默认周期 15s · 给 25s 保守）
sleep 25
# initial 状态的 janitor 恢复 = 直接 DELETE（未做任何外部动作·安全释放）·
# 所以合格结果是：行已被删（COUNT=0）· 或推进到 completed / need_manual。
# 卡在 initial/purchasing/purchased/imported = fail。
pp_count=$(sqlite3 "$DB" "SELECT count(1) FROM pending_purchase WHERE id='e2e-crash-1';")
if [ "$pp_count" = "0" ]; then
  ok "janitor 已回收 initial 卡单 (行被 delete)"
else
  pp_status=$(sqlite3 "$DB" "SELECT status FROM pending_purchase WHERE id='e2e-crash-1';")
  case "$pp_status" in
    completed|need_manual)
      ok "janitor 推进到 $pp_status"
      ;;
    *)
      ko "janitor 未处理 · status=$pp_status (期望 delete / completed / need_manual)"
      ;;
  esac
fi

# ── 汇总 ──────────────────────────────────────
banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：/tmp/bp-e2e.log"
  tail -30 "$LOG"
  exit 1
fi
echo "  ✅ 全绿"
