#!/usr/bin/env bash
# tests/e2e/anon-bus.sh · 1c-1 · 匿名撮合多人 bus 骨架 e2e
#
# 断言：
#   1. 乘客 A 建 anon bus（zone=us · max_unit_price=30M · max_members=3）
#   2. 乘客 B POST /api/me/buses/anon/match 匹配到 A 建的 bus（matched=true）
#   3. 乘客 B POST /api/me/buses/{id}/join 加入成功·成员数=2
#   4. 乘客 C match + auto_join=true 一次调用完成加入·成员数=3
#   5. 乘客 D match 应看到"车满"或匹配不到（max_members=3 已达）
#   6. 乘客 D 尝试直接 join → 409 bus_full
#   7. bus.share_pct 均分（3 人各 33/33/34）
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BIN=${BP_AB_BIN:-/tmp/bp-ab}
DB=${BP_AB_DB:-/tmp/bp-ab.db}
PORT=${BP_AB_PORT:-18096}
BASE="http://127.0.0.1:${PORT}"
LOG=/tmp/bp-ab.log
CA=/tmp/bp-ab-A.txt
CB=/tmp/bp-ab-B.txt
CC=/tmp/bp-ab-C.txt
CD=/tmp/bp-ab-D.txt

pass=0
fail=0
banner() { printf "\n== %s ==\n" "$1"; }
ok()     { printf "  ✅ %s\n" "$1"; pass=$((pass+1)); }
ko()     { printf "  ❌ %s\n" "$1"; fail=$((fail+1)); }

cleanup() {
  local p
  p=$(lsof -i ":$PORT" -sTCP:LISTEN -P 2>/dev/null | awk 'NR==2 {print $2}' || true)
  if [ -n "${p:-}" ]; then kill "$p" 2>/dev/null || true; sleep 0.3; fi
  rm -f "$DB" "${DB}-wal" "${DB}-shm" "$CA" "$CB" "$CC" "$CD"
}
trap cleanup EXIT

register() {
  local cookies="$1" email="$2" username="$3"
  curl -sSf -c "$cookies" -X POST "$BASE/api/register" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$email\",\"username\":\"$username\",\"password\":\"12345678\"}" >/dev/null
}

banner "step 0 · 起服务"
go build -o "$BIN" ./cmd/bus-pooling
export BP_MASTER_KEY=${BP_MASTER_KEY:-18791c18de60833ca343712a98adf7cc2822bdd0d4f878aceed8bf9e96e277e9}
export BP_DB_PATH="$DB"
export BP_ADDR=":$PORT"
export BP_INSECURE_COOKIE=1
export DRY_RUN=1

rm -f "$DB" "${DB}-wal" "${DB}-shm" "$LOG"
"$BIN" migrate up >/dev/null
"$BIN" serve >"$LOG" 2>&1 &
BP_PID=$!
sleep 1.2
curl -sSf -o /dev/null "$BASE/healthz" || { echo "!! 服务没起来"; tail -30 "$LOG"; exit 1; }
ok "服务起来 pid=$BP_PID"

banner "step 1 · A 注册并建 anon bus"
register "$CA" "a@e.local" "alice"
resp=$(curl -sSf -b "$CA" -X POST "$BASE/api/me/buses" \
  -H "Content-Type: application/json" \
  -d '{"name":"拼车 US","kind":"anon","max_members":3,"anon_zone":"us","anon_max_unit_price":30000000}')
BUS_ID=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])' <<<"$resp")
if [ -n "$BUS_ID" ]; then ok "A 建 anon bus id=$BUS_ID"; else ko "A 建车失败: $resp"; exit 1; fi

kind=$(sqlite3 "$DB" "SELECT kind||'|'||COALESCE(anon_zone,'')||'|'||COALESCE(anon_max_unit_price,0)||'|'||COALESCE(max_members,0) FROM bus WHERE id='$BUS_ID';")
if [ "$kind" = "anon|us|30000000|3" ]; then
  ok "DB 三维属性落对：$kind"
else
  ko "DB 属性异常：$kind"
fi

banner "step 2 · B 匹配 anon bus（不 auto_join）"
register "$CB" "b@e.local" "bob"
resp=$(curl -sSf -b "$CB" -X POST "$BASE/api/me/buses/anon/match" \
  -H "Content-Type: application/json" \
  -d '{"zone":"us","max_unit_price":30000000}')
matched=$(python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("matched",False))' <<<"$resp")
match_id=$(python3 -c 'import json,sys;d=json.load(sys.stdin);print((d.get("bus") or {}).get("id",""))' <<<"$resp")
if [ "$matched" = "True" ] && [ "$match_id" = "$BUS_ID" ]; then
  ok "B 匹配到 A 的 bus id=$match_id"
else
  ko "B 撮合失败: matched=$matched match_id=$match_id resp=$resp"
fi

banner "step 3 · B 显式 join"
resp=$(curl -sS -o /dev/null -w "%{http_code}" -b "$CB" -X POST "$BASE/api/me/buses/$BUS_ID/join")
if [ "$resp" = "200" ]; then ok "B join → 200"; else ko "B join = $resp"; fi

cnt=$(sqlite3 "$DB" "SELECT count(1) FROM bus_member WHERE bus_id='$BUS_ID' AND left_at IS NULL;")
if [ "$cnt" = "2" ]; then ok "成员数 = 2"; else ko "成员数 = $cnt · want 2"; fi

banner "step 4 · C match + auto_join"
register "$CC" "c@e.local" "carol"
resp=$(curl -sSf -b "$CC" -X POST "$BASE/api/me/buses/anon/match" \
  -H "Content-Type: application/json" \
  -d '{"zone":"us","max_unit_price":30000000,"auto_join":true}')
matched=$(python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("matched",False))' <<<"$resp")
if [ "$matched" = "True" ]; then ok "C match + auto_join 成功"; else ko "C 撮合失败: $resp"; fi

cnt=$(sqlite3 "$DB" "SELECT count(1) FROM bus_member WHERE bus_id='$BUS_ID' AND left_at IS NULL;")
if [ "$cnt" = "3" ]; then ok "成员数 = 3（车满）"; else ko "成员数 = $cnt · want 3"; fi

# share_pct 检查 · 均分 = 33 33 34（余给 owner）
shares=$(sqlite3 "$DB" "SELECT share_pct FROM bus_member WHERE bus_id='$BUS_ID' AND left_at IS NULL ORDER BY role DESC, joined_at ASC;")
# 期望：owner (alice) = 34 · member (bob, carol) = 33
expected="34
33
33"
if [ "$shares" = "$expected" ]; then
  ok "share_pct 均分正确（34/33/33）"
else
  ko "share_pct 分布异常"
  echo "得到:"; echo "$shares"
fi

banner "step 5 · D 撮合 · 车满不该匹配"
register "$CD" "d@e.local" "dan"
resp=$(curl -sSf -b "$CD" -X POST "$BASE/api/me/buses/anon/match" \
  -H "Content-Type: application/json" \
  -d '{"zone":"us","max_unit_price":30000000}')
matched=$(python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("matched",False))' <<<"$resp")
if [ "$matched" = "False" ]; then ok "D 撮合返 matched=false（车满已过滤）"; else ko "D 撮合应 miss: $resp"; fi

banner "step 6 · D 直接 join 满车 → 409"
sc=$(curl -sS -o /dev/null -w "%{http_code}" -b "$CD" -X POST "$BASE/api/me/buses/$BUS_ID/join")
if [ "$sc" = "409" ]; then ok "D join 满车 → 409"; else ko "D join 满车 = $sc · want 409"; fi

banner "step 7 · anon 撮合幂等（B 已加入·再 match 不重复匹配同车）"
resp=$(curl -sSf -b "$CB" -X POST "$BASE/api/me/buses/anon/match" \
  -H "Content-Type: application/json" \
  -d '{"zone":"us","max_unit_price":30000000}')
matched=$(python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("matched",False))' <<<"$resp")
if [ "$matched" = "False" ]; then ok "B 已加入 · match 返 false（不重复匹配）"; else ko "B 应 miss: $resp"; fi

banner "汇总"
echo "  pass: $pass  fail: $fail"
if [ "$fail" -gt 0 ]; then
  echo "  ⚠️  日志尾：$LOG"
  tail -20 "$LOG"
  exit 1
fi
echo "  ✅ 匿名撮合骨架 · 建/匹配/join/满车/幂等全通"
