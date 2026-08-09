#!/usr/bin/env bash
# tests/e2e/pending-topup.sh · 1b P1-C · pending_topup 状态机 e2e
#
# 断言：
#   1. POST /api/me/topup 起单后·pending_topup 行 status=gateway_ordered
#   2. dev-mark-paid 后·pending_topup status=completed
#   3. 手工造 initial 卡单 → janitor 推 expired
#   4. 手工造 gateway_paid 卡单 → janitor 重试 → credited
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BIN=${BP_PT_BIN:-/tmp/bp-pt}
DB=${BP_PT_DB:-/tmp/bp-pt.db}
PORT=${BP_PT_PORT:-18095}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-pt.log
COOKIES=/tmp/bp-pt-cookies.txt

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

banner "step 0 · 起服务"
go build -o "$BIN" ./cmd/bus-pooling
export BP_MASTER_KEY=${BP_MASTER_KEY:-18791c18de60833ca343712a98adf7cc2822bdd0d4f878aceed8bf9e96e277e9}
export BP_DB_PATH="$DB"
export BP_ADDR=":$PORT"
export BP_INSECURE_COOKIE=1
export DRY_RUN=1
export BP_ENABLE_DEV_TOPUP=1

rm -f "$DB" "${DB}-wal" "${DB}-shm" "$LOG"
"$BIN" migrate up >/dev/null
"$BIN" serve >"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
curl -sSf -o /dev/null "$BASE/healthz" || { echo "!! 服务没起来"; tail -30 "$LOG"; exit 1; }
ok "服务起来 pid=$BP_PID"

banner "step 1 · 注册 + 起充值单"
curl -sSf -c "$COOKIES" -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"pt@e.local","username":"ptuser","password":"12345678"}' >/dev/null

ORDER=$(curl -sSf -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":100000000,"channel":"waffo"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["order_id"])')
ok "起单 order_id=$ORDER"

# pending_topup 应该在 gateway_ordered
st=$(sqlite3 "$DB" "SELECT status FROM pending_topup WHERE topup_order_id='$ORDER';")
if [ "$st" = "gateway_ordered" ]; then
  ok "pending_topup status=gateway_ordered"
else
  ko "pending_topup status=$st · want gateway_ordered"
fi

banner "step 2 · dev-mark-paid → 完整闭环"
curl -sSf -b "$COOKIES" -X POST "$BASE/api/internal/topup/$ORDER/paid" >/dev/null
st=$(sqlite3 "$DB" "SELECT status FROM pending_topup WHERE topup_order_id='$ORDER';")
if [ "$st" = "completed" ]; then
  ok "dev-mark-paid 后 pending_topup=completed（完整闭环）"
else
  ko "dev-mark-paid 后 pending_topup=$st · want completed"
fi

# 钱包应到账
bal=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')
if [ "$bal" = "100000000" ]; then
  ok "钱包到账 100 积分"
else
  ko "钱包到账 = $bal · want 100000000"
fi

banner "step 3 · 手工造 initial 卡单 → janitor 推 expired"
# 造一行 initial 且 updated_at 早得离谱（超过 5min initial timeout）
sqlite3 "$DB" <<SQL
INSERT INTO idempotency_record (id,passenger_id,method,path,idempotency_key,request_fingerprint,created_at)
  VALUES ('idem-stuck-init', (SELECT id FROM passenger LIMIT 1), 'POST','/api/me/topup','k1234567890123456789012345678901','fp-init','2020-01-01');
INSERT INTO topup_order (id,passenger_id,channel,region,rail,credits,channel_fee,paid,pay_url,status,expires_at,provider_kind,created_at,updated_at)
  VALUES ('order-stuck-init', (SELECT id FROM passenger LIMIT 1),'waffo','overseas','hosted',100,5,105,'x','pending','2027-01-01','waffo_checkout','2020-01-01','2020-01-01');
INSERT INTO pending_topup (id,idempotency_record_id,passenger_id,topup_order_id,status,created_at,updated_at)
  VALUES ('pt-stuck-init','idem-stuck-init',(SELECT id FROM passenger LIMIT 1),'order-stuck-init','initial','2020-01-01','2020-01-01');
SQL

# 等 janitor 一轮（默认 30s · 拉长为 45s 保守）
sleep 35
st=$(sqlite3 "$DB" "SELECT status FROM pending_topup WHERE id='pt-stuck-init';")
if [ "$st" = "expired" ]; then
  ok "initial 卡单 → janitor 推 expired"
else
  ko "initial 卡单未处理 · status=$st · want expired"
  tail -30 "$LOG" | grep -i "topup janitor\|pending_topup" | head -5
fi

banner "step 4 · 手工造 gateway_paid 卡单 → janitor 重试 MarkPaid → credited"
sqlite3 "$DB" <<SQL
INSERT INTO idempotency_record (id,passenger_id,method,path,idempotency_key,request_fingerprint,created_at)
  VALUES ('idem-stuck-paid', (SELECT id FROM passenger LIMIT 1), 'POST','/api/me/topup','k2234567890123456789012345678901','fp-paid','2020-01-01');
INSERT INTO topup_order (id,passenger_id,channel,region,rail,credits,channel_fee,paid,pay_url,status,expires_at,provider_kind,created_at,updated_at)
  VALUES ('order-stuck-paid', (SELECT id FROM passenger LIMIT 1),'waffo','overseas','hosted',50000000,2500000,52500000,'x','pending','2027-01-01','waffo_checkout','2020-01-01','2020-01-01');
INSERT INTO pending_topup (id,idempotency_record_id,passenger_id,topup_order_id,status,created_at,updated_at)
  VALUES ('pt-stuck-paid','idem-stuck-paid',(SELECT id FROM passenger LIMIT 1),'order-stuck-paid','gateway_paid','2020-01-01','2020-01-01');
SQL

# 再等一轮 janitor
sleep 35
st=$(sqlite3 "$DB" "SELECT status FROM pending_topup WHERE id='pt-stuck-paid';")
case "$st" in
  credited|completed)
    ok "gateway_paid 卡单 → janitor 推到 $st"
    ;;
  *)
    ko "gateway_paid 卡单 status=$st · want credited/completed"
    tail -30 "$LOG" | grep -i "topup janitor\|pending_topup\|MarkPaid" | head -10
    ;;
esac

# 钱包应又加了 50 积分（第二次 MarkPaid）
bal2=$(curl -sSf -b "$COOKIES" "$BASE/api/me/wallet" | python3 -c 'import json,sys;print(json.load(sys.stdin)["balance"])')
if [ "$bal2" = "150000000" ]; then
  ok "第二笔到账 50 积分 · 总余额 150"
else
  ko "第二笔未到账 · 余额=$bal2 · want 150000000"
fi

banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：$LOG"
  tail -20 "$LOG"
  exit 1
fi
echo "  ✅ pending_topup 状态机 · webhook 主推进 + janitor 兜底全通"
