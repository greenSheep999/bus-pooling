#!/usr/bin/env bash
# tests/e2e/topup-channels.sh · 1b · topup 多渠道注册表 + POST 校验
#
# 断言：
#   1. GET /api/topup/channels 返 4 家（waffo 启·其他 3 家关）
#   2. 4 家都暴露三维属性（region / rail / provider_kind 内部不暴露 · enabled / requires_payer_reference）
#   3. POST /api/me/topup channel=bybit 返 503 channel_disabled（暂关）
#   4. POST /api/me/topup channel=notreal 返 400
#   5. env override BP_TOPUP_BYBIT_ENABLED=1 后·bybit 可下单 · direct rail 必须带 payer_reference
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BIN=${BP_TC_BIN:-/tmp/bp-tc}
DB=${BP_TC_DB:-/tmp/bp-tc.db}
PORT=${BP_TC_PORT:-18094}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-tc.log
COOKIES=/tmp/bp-tc-cookies.txt

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

banner "step 0 · 起服务（默认只 waffo 启）"
go build -o "$BIN" ./cmd/bus-pooling
export BP_MASTER_KEY=${BP_MASTER_KEY:-18791c18de60833ca343712a98adf7cc2822bdd0d4f878aceed8bf9e96e277e9}
export BP_DB_PATH="$DB"
export BP_ADDR=":$PORT"
export BP_INSECURE_COOKIE=1
export DRY_RUN=1
export BP_ENABLE_DEV_TOPUP=1
unset BP_TOPUP_BYBIT_ENABLED BP_TOPUP_BINANCE_ENABLED BP_TOPUP_EPUSDT_ENABLED

rm -f "$DB" "${DB}-wal" "${DB}-shm" "$LOG"
"$BIN" migrate up >/dev/null
"$BIN" serve >"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
curl -sSf -o /dev/null "$BASE/healthz" || { echo "!! 服务没起来"; tail -30 "$LOG"; exit 1; }
ok "服务起来 pid=$BP_PID"

banner "step 1 · GET /api/topup/channels 返 4 家"
resp=$(curl -sSf "$BASE/api/topup/channels")
count=$(python3 -c 'import json,sys;print(len(json.load(sys.stdin)["channels"]))' <<<"$resp")
if [ "$count" = "4" ]; then ok "4 家渠道全在（含 disabled）"; else ko "channels count=$count · want 4"; fi

# 检查暴露的字段（不能出 provider_kind）
if python3 -c 'import json,sys;c=json.load(sys.stdin)["channels"][0];assert "provider_kind" not in c,c' <<<"$resp" 2>/dev/null; then
  ok "响应不暴露 provider_kind（术语铁律 §12.6）"
else
  ko "响应泄漏内部字段 provider_kind"
fi

# 三维属性都在
if python3 -c 'import json,sys;c=json.load(sys.stdin)["channels"];assert all(k in c[0] for k in ["region","rail","enabled","asset"]),c[0]' <<<"$resp" 2>/dev/null; then
  ok "三维属性都在（region / rail / asset / enabled）"
else
  ko "响应缺三维属性"
fi

# waffo enabled=true
waffo_en=$(python3 -c 'import json,sys;print([c["enabled"] for c in json.load(sys.stdin)["channels"] if c["id"]=="waffo"][0])' <<<"$resp")
if [ "$waffo_en" = "True" ]; then ok "waffo enabled=true"; else ko "waffo enabled=$waffo_en · want True"; fi

# bybit disabled
bybit_en=$(python3 -c 'import json,sys;print([c["enabled"] for c in json.load(sys.stdin)["channels"] if c["id"]=="bybit"][0])' <<<"$resp")
if [ "$bybit_en" = "False" ]; then ok "bybit enabled=false（暂关）"; else ko "bybit enabled=$bybit_en · want False"; fi

banner "step 2 · 注册 + POST bybit 应 503"
curl -sSf -c "$COOKIES" -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"tc@e.local","username":"tcuser","password":"12345678"}' >/dev/null

sc=$(curl -sS -o /dev/null -w "%{http_code}" -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":100000000,"channel":"bybit","payer_reference":"12345"}')
if [ "$sc" = "503" ]; then ok "关闭渠道 bybit 返 503（暂未开放）"; else ko "关闭渠道返 $sc · want 503"; fi

banner "step 3 · POST notreal 应 400"
sc=$(curl -sS -o /dev/null -w "%{http_code}" -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":100000000,"channel":"notreal"}')
if [ "$sc" = "400" ]; then ok "未知渠道 notreal 返 400"; else ko "未知渠道返 $sc · want 400"; fi

banner "step 4 · env override 打开 bybit · direct rail 要 payer_reference"
kill "$BP_PID" 2>/dev/null || true
sleep 0.5
export BP_TOPUP_BYBIT_ENABLED=1
"$BIN" serve >>"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
curl -sSf -o /dev/null "$BASE/healthz" || { ko "重启失败"; exit 1; }

# 无 payer_reference · direct rail 应 400
sc=$(curl -sS -o /dev/null -w "%{http_code}" -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":100000000,"channel":"bybit"}')
if [ "$sc" = "400" ]; then ok "direct rail 缺 payer_reference 返 400"; else ko "缺 payer_reference 返 $sc · want 400"; fi

# 带 payer_reference · 应能建单（gateway 未装配·会走 mock 路径）
resp=$(curl -sS -b "$COOKIES" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $(gen_key)" \
  -d '{"credits":100000000,"channel":"bybit","payer_reference":"118027304"}')
oid=$(python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("order_id",""))' <<<"$resp" 2>/dev/null || echo "")
if [ -n "$oid" ]; then
  ok "bybit 带 payer_reference 建单成功 order_id=$oid"
  # 检查 DB channel/rail/provider_kind 落对
  ch=$(sqlite3 "$DB" "SELECT channel||'|'||rail||'|'||provider_kind FROM topup_order WHERE id='$oid';")
  if [ "$ch" = "bybit|direct|bybit_internal" ]; then
    ok "DB 三维属性落对：$ch"
  else
    ko "DB 三维属性异常：$ch (want bybit|direct|bybit_internal)"
  fi
else
  ko "bybit 建单失败：$resp"
fi

banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：$LOG"
  tail -20 "$LOG"
  exit 1
fi
echo "  ✅ 多渠道注册表 · POST 校验 · env override 全通"
