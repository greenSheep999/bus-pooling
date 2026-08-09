#!/usr/bin/env bash
# tests/e2e/p0-regressions.sh · 三条 P0 的**端到端**定向复现
#
# 覆盖三条审计 P0：
#   P0-1 · assign 并发跨系统分叉（两 idem key 同 cred 派不同 bus）
#   P0-2 · early settlement 状态滞留（pending_topup 卡 initial · order 已 credited）
#   P1-1 · janitor expire 双表不一致（pending=expired · order=pending）
#
# 每条断言：修前会红 · 修后必绿。跑之前先编译最新 · rm -f 旧 DB。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BIN=${BP_P0_BIN:-/tmp/bp-p0}
DB=${BP_P0_DB:-/tmp/bp-p0.db}
PORT=${BP_P0_PORT:-18097}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-p0.log
CA=/tmp/bp-p0-cookies.txt

pass=0
fail=0
banner() { printf "\n== %s ==\n" "$1"; }
ok()     { printf "  ✅ %s\n" "$1"; pass=$((pass+1)); }
ko()     { printf "  ❌ %s\n" "$1"; fail=$((fail+1)); }

cleanup() {
  local p
  p=$(lsof -i ":$PORT" -sTCP:LISTEN -P 2>/dev/null | awk 'NR==2 {print $2}' || true)
  if [ -n "${p:-}" ]; then kill "$p" 2>/dev/null || true; sleep 0.3; fi
  rm -f "$DB" "${DB}-wal" "${DB}-shm" "$CA"
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

banner "P0-1 复现 · assign 并发派单 UNIQUE 挡住"
curl -sSf -c "$CA" -X POST "$BASE/api/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"p0@e.local","username":"p0user","password":"12345678"}' >/dev/null

# 建两辆车 A / B
BUS_A=$(curl -sSf -b "$CA" -X POST "$BASE/api/me/buses" \
  -H "Content-Type: application/json" \
  -d '{"name":"A","kind":"anon","max_members":3}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
BUS_B=$(curl -sSf -b "$CA" -X POST "$BASE/api/me/buses" \
  -H "Content-Type: application/json" \
  -d '{"name":"B","kind":"anon","max_members":3}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')

# 直接造一个 alive record credential（省充值 + 拉号）· owner_record_passenger_id 是当前 passenger
PID=$(sqlite3 "$DB" "SELECT id FROM passenger LIMIT 1;")
sqlite3 "$DB" <<SQL
INSERT INTO pull_round (id, vendor_id, client_order_id, count_requested, count_purchased,
                       key_cost_total, service_fee_total, participants_split_json, status, created_at)
VALUES ('r-p0', 'kiro91', 'co-p0', 1, 1, 100, 10, '{}', 'completed', '2026-01-01');
INSERT INTO credential_ledger (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
                              current_group, vendor_id, source_pull_round_id, status, disabled, pulled_at, credits_used)
VALUES ('c-p0', 999, NULL, '$PID', 'record-$PID', 'kiro91', 'r-p0', 'alive', 0, '2026-01-01', 0);
SQL

# 并发两个 assign · 不同 idem key · 同 credential · 不同 bus
key1=$(gen_key); key2=$(gen_key)
(curl -sS -o /tmp/p0-r1.txt -w "%{http_code}\n" -b "$CA" -X POST "$BASE/api/me/pull-records/assign" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $key1" \
  -d "{\"credential_ids\":[\"c-p0\"],\"destination\":\"into_bus\",\"bus_id\":\"$BUS_A\"}" > /tmp/p0-r1-sc.txt) &
P1=$!
(curl -sS -o /tmp/p0-r2.txt -w "%{http_code}\n" -b "$CA" -X POST "$BASE/api/me/pull-records/assign" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $key2" \
  -d "{\"credential_ids\":[\"c-p0\"],\"destination\":\"into_bus\",\"bus_id\":\"$BUS_B\"}" > /tmp/p0-r2-sc.txt) &
P2=$!
wait $P1 $P2

sc1=$(cat /tmp/p0-r1-sc.txt | tr -d '[:space:]')
sc2=$(cat /tmp/p0-r2-sc.txt | tr -d '[:space:]')
# 期望：一 200 · 一 409
if { [ "$sc1" = "200" ] && [ "$sc2" = "409" ]; } || { [ "$sc1" = "409" ] && [ "$sc2" = "200" ]; }; then
  ok "并发 assign · 一 200 一 409（UNIQUE 挡住 R2）"
else
  ko "并发 assign 状态码异常 sc1=$sc1 sc2=$sc2"
fi

# 核心断言：ledger.owner_bus_id 必须跟真正胜出的 pending_assignment 一致（无分叉）
winner=$(sqlite3 "$DB" "SELECT target_bus_id FROM pending_assignment WHERE credential_id='c-p0' AND status='completed' LIMIT 1;")
ledger=$(sqlite3 "$DB" "SELECT COALESCE(owner_bus_id,'') FROM credential_ledger WHERE id='c-p0';")
if [ -n "$winner" ] && [ "$ledger" = "$winner" ]; then
  ok "credential_ledger.owner_bus_id 跟胜出的 pending 一致（无跨系统分叉）"
else
  ko "分叉：winner=$winner ledger=$ledger"
fi

banner "P0-2 复现 · early settlement pending 状态推进"
# 场景：起单不 attach gateway · pending_topup 手工留 initial · webhook 到时应一路推 completed
# （单测 TestSettlement_EarlyPendingRecovers 已覆盖 · 这里跑 e2e 版）
key3=$(gen_key)
ORDER=$(curl -sSf -b "$CA" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $key3" \
  -d '{"credits":50000000,"channel":"waffo"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["order_id"])')

# 手工把 pending_topup 回到 initial（模拟 AttachGateway 还没跑）
sqlite3 "$DB" "UPDATE pending_topup SET status='initial', updated_at=datetime('now') WHERE topup_order_id='$ORDER';"

# dev-mark-paid（模拟 webhook 到）· pending_topup 应一路推到 completed
curl -sSf -b "$CA" -X POST "$BASE/api/internal/topup/$ORDER/paid" >/dev/null
st=$(sqlite3 "$DB" "SELECT status FROM pending_topup WHERE topup_order_id='$ORDER';")
if [ "$st" = "completed" ]; then
  ok "early settlement · pending_topup 从 initial 一路推 completed（EnsureAtLeast 修）"
else
  ko "pending_topup 卡在 $st · P0-2 未修复"
fi

banner "P1-1 复现 · janitor expire 同步双表"
# 造个 order + pending initial · updated_at 早得离谱 · janitor 应双表 expire
key4=$(gen_key)
ORDER2=$(curl -sSf -b "$CA" -X POST "$BASE/api/me/topup" \
  -H "Content-Type: application/json" -H "X-Idempotency-Key: $key4" \
  -d '{"credits":10000000,"channel":"waffo"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["order_id"])')
sqlite3 "$DB" "UPDATE pending_topup SET status='initial', updated_at='2020-01-01T00:00:00.000Z' WHERE topup_order_id='$ORDER2';"
sqlite3 "$DB" "UPDATE topup_order SET updated_at='2020-01-01T00:00:00.000Z' WHERE id='$ORDER2';"

# 等 janitor 一轮（默认 30s）
sleep 35
p=$(sqlite3 "$DB" "SELECT status FROM pending_topup WHERE topup_order_id='$ORDER2';")
o=$(sqlite3 "$DB" "SELECT status FROM topup_order WHERE id='$ORDER2';")
if [ "$p" = "expired" ] && [ "$o" = "expired" ]; then
  ok "双表同步 expired · 无分叉（P1-1 修）"
else
  ko "双表分叉 · pending=$p · order=$o（want expired/expired）"
fi

banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：$LOG"
  tail -30 "$LOG"
  exit 1
fi
echo "  ✅ 三条 P0 定向复现全绿"
