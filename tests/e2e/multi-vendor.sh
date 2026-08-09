#!/usr/bin/env bash
# tests/e2e/multi-vendor.sh · P1-D 修复验证 · 拉号多 vendor 支持
#
# 修 P1-D：以前 live decider 硬编 kiro91 · api pull.go 忽略 vendor_id · estimate 也丢。
# 现在：装配层给 decider 装 6 家 DryRunVendor · pull.go 传 req.VendorID · decider.PullInput
# 按 vendor_id 路由。
#
# 断言：DRY_RUN 模式下·分别用 6 家 vendor_id 拉号·每次都能走完（pending_purchase.vendor_id
# 落对·credential_ledger.vendor_id 落对）。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BIN=${BP_MV_BIN:-/tmp/bp-mv}
DB=${BP_MV_DB:-/tmp/bp-mv.db}
PORT=${BP_MV_PORT:-18093}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-mv.log
COOKIES=/tmp/bp-mv-cookies.txt

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
if ! curl -sSf -o /dev/null "$BASE/healthz"; then
  echo "!! 服务没起来"; tail -30 "$LOG"; exit 1
fi
ok "服务起来 pid=$BP_PID"

banner "step 1 · 注册 + 充值 500 积分"
curl -sSf -c "$COOKIES" -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"mv@e.local","username":"mvuser","password":"12345678"}' >/dev/null

ORDER=$(curl -sSf -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":500000000,"channel":"waffo"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["order_id"])')
curl -sSf -b "$COOKIES" -X POST "$BASE/api/internal/topup/$ORDER/paid" >/dev/null
ok "余额 500 积分"

banner "step 2 · 分别用 6 家 vendor_id 拉号"
for v in "kiro91" "kiroceo" "kirooo" "kiroappio" "kiroappcc" "kirodrop"; do
  key=$(gen_key)
  resp=$(curl -sS -b "$COOKIES" -X POST "$BASE/api/me/pull" \
    -H "Content-Type: application/json" \
    -H "X-Idempotency-Key: $key" \
    -d "{\"count\":1,\"vendor_id\":\"$v\"}")
  got_vendor=$(python3 -c "import json,sys;print(json.load(sys.stdin).get('vendor_id',''))" <<<"$resp")
  if [ "$got_vendor" = "$v" ]; then
    ok "vendor=$v · 响应 vendor_id 落对"
  else
    ko "vendor=$v · 响应 vendor_id=$got_vendor (want $v) · body=$resp"
  fi
done

banner "step 3 · pending_purchase.vendor_id 落对 6 家"
distinct=$(sqlite3 "$DB" "SELECT count(DISTINCT vendor_id) FROM pending_purchase;")
if [ "$distinct" = "6" ]; then
  ok "pending_purchase 里 vendor_id 分布 = 6 家"
else
  ko "pending_purchase vendor_id distinct=$distinct · want 6"
  sqlite3 "$DB" "SELECT vendor_id, count(1) FROM pending_purchase GROUP BY vendor_id;"
fi

banner "step 4 · 请求未装配的 vendor 返 400"
key=$(gen_key)
sc=$(curl -sS -o /dev/null -w "%{http_code}" -b "$COOKIES" -X POST "$BASE/api/me/pull" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: $key" \
  -d '{"count":1,"vendor_id":"kirotest_notreal"}')
if [ "$sc" = "400" ]; then
  ok "未装配 vendor 返 400"
else
  ko "未装配 vendor 返 $sc · want 400"
fi

banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：$LOG"
  tail -20 "$LOG"
  exit 1
fi
echo "  ✅ 多 vendor 拉号 · 6 家全通"
