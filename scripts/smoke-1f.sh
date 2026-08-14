#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
# 1f live 链路 smoke test
#
# 覆盖端到端主路径（阶段 1a-1e）：
#   ① 起服务（DRY_RUN=0 · live vendor + live housepool）
#   ② 注册测试乘客 + session cookie + API key
#   ③ 走 dev-topup 给钱包充值（BP_ENABLE_DEV_TOPUP=1 mock 路径）
#   ④ 建 single bus + 配 downstream passengerpool（k2a）
#   ⑤ 触发拉号（默认 vendor kiro91 · count=1）
#   ⑥ 查 credential_ledger 看号有没有落库
#   ⑦ 查 outbound_webhook_delivery 看有没有 boarded 事件
#   ⑧ 查 k2a 侧看双写是否命中
#
# 前置：
#   - .dev.env 里必须有 BP_HOUSEPOOL_ADMIN_KEY（vps22 config.json）
#   - 环境变量 K2A_ADMIN_KEY 是 vps196 kiro-rs 的 adminApiKey（用于下游校验）
#     不写进 .dev.env —— .dev.env 是我方系统池的·k2a 是"乘客的" passengerpool
#
# 铁律：
#   - set -euo pipefail · 每一步 assert · 失败立刻 exit + 行号
#   - 每条 curl 打 status + body 前 200 字 · 便于事后排错
#   - 跑完清理：kill 服务·删测试用户（数据库不删·历史保留）
# ─────────────────────────────────────────────────────────────
set -euo pipefail

# ── 顶部参数（改这里就行，别改下面） ──────────────────────
BP_ROOT="${BP_ROOT:-/Users/danlio/Repositories/daniel/bus-pooling}"
# 默认 8091 避 8080 冲突（主 workflow / dev serve 常占 8080）
# 想在 8080 起自己起 · 或 export BASE_URL / SMOKE_PORT
SMOKE_PORT="${SMOKE_PORT:-8091}"
BASE_URL="${BASE_URL:-http://127.0.0.1:${SMOKE_PORT}}"
SMOKE_EMAIL="${SMOKE_EMAIL:-smoke-1f-$(date +%s)@example.com}"
SMOKE_USERNAME="${SMOKE_USERNAME:-smoke1f$(date +%s)}"
SMOKE_PASSWORD="${SMOKE_PASSWORD:-smoke-1f-password-please-change}"
K2A_URL="${K2A_URL:-https://k2a.muxpay.xyz}"
K2A_ADMIN_KEY="${K2A_ADMIN_KEY:-}"    # 必填 · 从 vps196 /data/kiro-rs-ultra/config/config.json 拿
DEFAULT_VENDOR="${DEFAULT_VENDOR:-kiro91}"
PULL_COUNT="${PULL_COUNT:-1}"
# 充 100 积分 = 100_000_000 microunit（1 积分 = 1 CNY = 1_000_000 microunit · CLAUDE §7.2）
TOPUP_CREDITS="${TOPUP_CREDITS:-100000000}"

# ── 输出与失败陷阱 ────────────────────────────────────────
COOKIE_JAR="$(mktemp)"
STDOUT_LOG="$(mktemp)"
SERVE_LOG="$(mktemp)"
SERVE_PID=""

red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow(){ printf "\033[33m%s\033[0m\n" "$*"; }

trap 'ec=$?; on_exit $ec $LINENO' EXIT
on_exit() {
    local code=$1 line=$2
    if [[ $code -ne 0 ]]; then
        red "❌ smoke test FAIL (exit=$code · line=$line)"
        if [[ -n "$SERVE_PID" ]]; then
            yellow "── 服务日志尾 60 行 ──"
            tail -60 "$SERVE_LOG" || true
        fi
    fi
    if [[ -n "$SERVE_PID" ]] && kill -0 "$SERVE_PID" 2>/dev/null; then
        yellow "stopping serve pid=$SERVE_PID"
        kill "$SERVE_PID" 2>/dev/null || true
        wait "$SERVE_PID" 2>/dev/null || true
    fi
    rm -f "$COOKIE_JAR" "$STDOUT_LOG"
    # SERVE_LOG 保留 · 便于事后调查
    if [[ $code -eq 0 ]]; then
        rm -f "$SERVE_LOG"
    else
        yellow "serve log 保留在: $SERVE_LOG"
    fi
}

# ── 步骤辅助 ──────────────────────────────────────────────
STEP=0
step() { STEP=$((STEP+1)); yellow "▶ [step $STEP] $*"; }

# curl_json <METHOD> <PATH> [--data <json>] [--cookie] [--header "K: V"] ...
#   写状态码到 CURL_STATUS · 响应体到 CURL_BODY
curl_json() {
    local method=$1 path=$2; shift 2
    local args=(-sS --max-time 30 -X "$method" -o "$STDOUT_LOG" -w "%{http_code}")
    # 默认 JSON header
    args+=(-H "Content-Type: application/json")
    local use_cookie=0
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --data)   args+=(--data "$2"); shift 2 ;;
            --cookie) use_cookie=1; shift ;;
            --header) args+=(-H "$2"); shift 2 ;;
            *) red "curl_json: 不认识参数 $1"; exit 42 ;;
        esac
    done
    if [[ $use_cookie -eq 1 ]]; then
        args+=(-b "$COOKIE_JAR" -c "$COOKIE_JAR")
    fi
    local url="$BASE_URL$path"
    CURL_STATUS=$(curl "${args[@]}" "$url" 2>/dev/null || echo "000")
    CURL_STATUS="${CURL_STATUS:-000}"
    CURL_BODY="$(cat "$STDOUT_LOG" 2>/dev/null || true)"
    CURL_BODY="${CURL_BODY:-}"
    printf "    %s %s → %s | %s\n" "$method" "$path" "$CURL_STATUS" "${CURL_BODY:0:200}"
}

require_status() {
    local want=$1 line=${2:-?}
    if [[ "$CURL_STATUS" != "$want" ]]; then
        red "  ✗ 期望 HTTP $want · 实际 $CURL_STATUS (line $line)"
        red "    body: $CURL_BODY"
        exit 1
    fi
}

# ── 预检查 ────────────────────────────────────────────────
cd "$BP_ROOT"

if [[ ! -f .dev.env ]]; then
    red "缺 .dev.env · 请先按 docs/1f-live-test.md 配好"
    exit 2
fi

if ! grep -q "^BP_HOUSEPOOL_ADMIN_KEY=." .dev.env; then
    red "缺 BP_HOUSEPOOL_ADMIN_KEY · 请先填 .dev.env"
    yellow "填法：从 vps22 /opt/kiro-aibbq/data/config.json 里 adminApiKey 字段拿明文"
    exit 3
fi

if [[ -z "$K2A_ADMIN_KEY" ]]; then
    yellow "⚠️ K2A_ADMIN_KEY 未设置 · 跳过 k2a 双写校验步骤"
    yellow "  取法：ssh vps196 'docker exec kiro-rs cat /app/config/config.json | grep adminApiKey'"
fi

# ── 起服务 ────────────────────────────────────────────────
step "起服务（DRY_RUN=0 · live vendor + live housepool）"

# shellcheck source=/dev/null
set -a; source .dev.env; set +a
export DRY_RUN=0
export BP_ENABLE_DEV_TOPUP=1
# BP_ADDR 覆盖 config.yaml 的 :8080
export BP_ADDR=":${SMOKE_PORT}"
# smoke 用独立 db · 别糊到本地正在用的 data/bus-pooling.db
SMOKE_DB="${SMOKE_DB:-$BP_ROOT/data/smoke-1f.db}"
export BP_DB_PATH="$SMOKE_DB"
yellow "  BP_ADDR=$BP_ADDR · BP_DB_PATH=$BP_DB_PATH"

# 端口冲突预检
if lsof -iTCP:$SMOKE_PORT -sTCP:LISTEN -Pn 2>/dev/null | grep -q LISTEN; then
    red "$SMOKE_PORT 端口已被占用 · 先 kill 或 export SMOKE_PORT=<别的> 重试"
    lsof -iTCP:$SMOKE_PORT -sTCP:LISTEN -Pn
    exit 4
fi

# 迁移（新 DB 必须 · 老 DB 幂等）
yellow "  迁移 DB..."
if ! go run ./cmd/bus-pooling migrate up >>"$SERVE_LOG" 2>&1; then
    red "  ✗ 迁移失败 · 日志尾："
    tail -40 "$SERVE_LOG"
    exit 5
fi
green "  ✓ 迁移完成"

go run ./cmd/bus-pooling serve >>"$SERVE_LOG" 2>&1 &
SERVE_PID=$!
yellow "  serve pid=$SERVE_PID · 日志: $SERVE_LOG"

# 等 60s 健康
for i in $(seq 1 60); do
    if curl -sf --max-time 2 "$BASE_URL/healthz" >/dev/null 2>&1; then
        green "  ✓ /healthz 通 · 用了 ${i}s"
        break
    fi
    if ! kill -0 "$SERVE_PID" 2>/dev/null; then
        red "  ✗ serve 挂了 · 日志尾："
        tail -80 "$SERVE_LOG"
        exit 5
    fi
    sleep 1
done
if ! curl -sf --max-time 2 "$BASE_URL/healthz" >/dev/null 2>&1; then
    red "  ✗ /healthz 60s 内没起 · 检查日志"
    tail -80 "$SERVE_LOG"
    exit 6
fi

# ── 注册 + session ───────────────────────────────────────
step "注册 $SMOKE_EMAIL"
curl_json POST /api/register --cookie --data "$(cat <<EOF
{"email":"$SMOKE_EMAIL","username":"$SMOKE_USERNAME","password":"$SMOKE_PASSWORD"}
EOF
)"
require_status 201 $LINENO
PID=$(echo "$CURL_BODY" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
green "  ✓ passenger_id=$PID"

step "查 /api/me · 确认 session 好使"
curl_json GET /api/me --cookie
require_status 200 $LINENO

step "查 /api/me/wallet · 应 0"
curl_json GET /api/me/wallet --cookie
require_status 200 $LINENO

# ── 充值（走 dev mock）────────────────────────────────────
step "创建 topup order · $TOPUP_CREDITS 积分"
# 先看有没有可用 channel
curl_json GET /api/topup/channels
require_status 200 $LINENO
CHANNEL_ID=$(echo "$CURL_BODY" | python3 -c '
import json,sys
d = json.load(sys.stdin)
items = d.get("items") or d.get("channels") or (d if isinstance(d, list) else [])
if not items:
    print("", end="")
else:
    # 优先挑 hosted 的（不需要 payer_reference）
    hosted = [c for c in items if c.get("rail") == "hosted"]
    print((hosted or items)[0].get("id", ""), end="")
' 2>/dev/null || true)
if [[ -z "$CHANNEL_ID" ]]; then
    yellow "  ⚠️ 没找到可用 topup channel · 跳过充值 · 拉号会因余额不足失败"
    yellow "    (config.yaml 没配 topup channels 或者字段名不对 · 见 internal/topupchannel/)"
else
    green "  ✓ channel=$CHANNEL_ID"
    IDEM=$(openssl rand -hex 16)
    curl_json POST /api/me/topup --cookie \
        --header "X-Idempotency-Key: $IDEM" \
        --data "$(cat <<EOF
{"channel":"$CHANNEL_ID","credits":$TOPUP_CREDITS}
EOF
)"
    if [[ "$CURL_STATUS" != "201" && "$CURL_STATUS" != "200" ]]; then
        yellow "  ⚠️ 起单失败（$CURL_STATUS）· 跳过充值"
    else
        ORDER_ID=$(echo "$CURL_BODY" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("order_id",""))')
        if [[ -n "$ORDER_ID" ]]; then
            step "mock 标 paid（BP_ENABLE_DEV_TOPUP 路径）"
            curl_json POST "/api/internal/topup/$ORDER_ID/paid" --cookie
            if [[ "$CURL_STATUS" == "200" ]]; then
                green "  ✓ topup 到账"
            else
                yellow "  ⚠️ dev-topup 标 paid 失败（$CURL_STATUS）"
            fi
        fi
    fi
fi

step "查 wallet 余额"
curl_json GET /api/me/wallet --cookie
require_status 200 $LINENO
BALANCE=$(echo "$CURL_BODY" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("balance",0))')
yellow "  余额 = $BALANCE microunit"

# ── 建 bus ────────────────────────────────────────────────
step "建 single bus"
curl_json POST /api/me/buses --cookie --data '{"name":"smoke-1f-bus","kind":"single"}'
require_status 201 $LINENO
BUS_ID=$(echo "$CURL_BODY" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
green "  ✓ bus_id=$BUS_ID"

# ── 配 downstream passengerpool（k2a）───────────────────
if [[ -n "$K2A_ADMIN_KEY" ]]; then
    step "配 downstream passengerpool → k2a"
    curl_json PUT /api/me/downstream/passengerpool --cookie \
        --data "$(cat <<EOF
{"passengerpool_url":"$K2A_URL","token":"$K2A_ADMIN_KEY","rules":{"push_on_pull":true,"resync_on_dead":false,"retry_on_failure":true,"bus_only":false}}
EOF
)"
    if [[ "$CURL_STATUS" == "200" ]]; then
        green "  ✓ passengerpool 配上"
    else
        yellow "  ⚠️ passengerpool 配置失败（$CURL_STATUS）· body: $CURL_BODY"
    fi

    step "测通 passengerpool"
    curl_json POST /api/me/downstream/passengerpool/test --cookie
    yellow "  test 结果: $CURL_STATUS | $CURL_BODY"
else
    yellow "跳过 downstream 配置（没 K2A_ADMIN_KEY）"
fi

# ── 触发拉号 ──────────────────────────────────────────────
step "触发拉号 · vendor=$DEFAULT_VENDOR · count=$PULL_COUNT"
IDEM=$(openssl rand -hex 16)
curl_json POST "/api/me/buses/$BUS_ID/pull" --cookie \
    --header "X-Idempotency-Key: $IDEM" \
    --data "$(cat <<EOF
{"count":$PULL_COUNT,"vendor_id":"$DEFAULT_VENDOR"}
EOF
)"
if [[ "$CURL_STATUS" != "200" && "$CURL_STATUS" != "201" ]]; then
    red "  ✗ 拉号失败（$CURL_STATUS）"
    red "    body: $CURL_BODY"
    yellow "    可能原因：余额不足 / vendor 未启用 / housepool 不通 / config.yaml 里 default_vendor 空"
    exit 7
fi
PURCHASED=$(echo "$CURL_BODY" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("purchased",0))')
if [[ "$PURCHASED" -lt 1 ]]; then
    red "  ✗ 拉到 0 个号 · 详情: $CURL_BODY"
    exit 8
fi
green "  ✓ 拉到 $PURCHASED 个号"

# ── 等异步落库 ────────────────────────────────────────────
step "等 5s · 让 webhookout / passengerpool pusher 跑完"
sleep 5

# ── 直接查数据库 ──────────────────────────────────────────
DB_PATH="${SMOKE_DB:-$BP_ROOT/data/smoke-1f.db}"
if [[ -f "$DB_PATH" ]]; then
    step "查 credential_ledger · 看号是否落库"
    LEDGER_COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM credential_ledger WHERE passenger_id='$PID';" 2>/dev/null || echo 0)
    yellow "  credential_ledger 里 $LEDGER_COUNT 条记录"

    step "查 outbound_webhook_delivery · 看有没有 boarded"
    HAS_TABLE=$(sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table' AND name='outbound_webhook_delivery';" 2>/dev/null || echo "")
    if [[ -n "$HAS_TABLE" ]]; then
        WEBHOOK_ROWS=$(sqlite3 "$DB_PATH" "SELECT event,status FROM outbound_webhook_delivery WHERE passenger_id='$PID' ORDER BY created_at DESC LIMIT 5;" 2>/dev/null || echo "")
        yellow "  outbound_webhook_delivery: ${WEBHOOK_ROWS:-<空>}"
    else
        yellow "  outbound_webhook_delivery 表还没建（阶段 1a-1e 迁移里没有？）"
    fi
else
    yellow "  ⚠️ 数据库 $DB_PATH 不在 · 跳 SQLite 检查"
fi

# ── k2a 侧对账 ────────────────────────────────────────────
if [[ -n "$K2A_ADMIN_KEY" ]]; then
    step "查 k2a 侧 credentials 看双写是否命中"
    K2A_TOTAL_BEFORE_UNKNOWN="?"  # 没做增量对比 · 只看当前总数
    RESP=$(curl -sS --max-time 10 "$K2A_URL/api/admin/credentials" \
        -H "x-api-key: $K2A_ADMIN_KEY" 2>&1 || true)
    K2A_TOTAL=$(echo "$RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("total","?"))' 2>/dev/null || echo "?")
    yellow "  k2a total = $K2A_TOTAL（人工对比 smoke 前后差 · 应 +$PURCHASED）"
fi

# ── 清理测试用户 ──────────────────────────────────────────
step "清理测试 passenger（软删）"
if [[ -f "$DB_PATH" ]]; then
    # 只清 session · 不删 passenger 行（保留台账）
    sqlite3 "$DB_PATH" "DELETE FROM passenger_session WHERE passenger_id='$PID';" 2>/dev/null || true
    green "  ✓ session 清理完"
fi

green "════════════════════════════════════════════════"
green "✅ smoke test 全通"
green "════════════════════════════════════════════════"
echo
echo "关键值："
echo "  passenger_id = $PID"
echo "  bus_id       = $BUS_ID"
echo "  purchased    = $PURCHASED"
echo "  余额         = $BALANCE microunit"
echo
echo "手动追查："
echo "  sqlite3 $DB_PATH \"SELECT * FROM credential_ledger WHERE passenger_id='$PID';\""
echo "  serve 日志: $SERVE_LOG（跑通后已删）"
