#!/usr/bin/env bash
# kirodrop 会话 token 定期刷新（approach B · 写 .env + 重启）
#
# cron 频繁跑（如每小时）· 但**只在当前 token 真失效时才刷新+重启** —— 先拿当前 token 打
# 一下 /api/v1/reservation · 200 就跳过（免无谓重启）· 401 才走 ddddocr 登录换新 token。
#
# 装法（vps22）：
#   1. 本目录（solve.py + Dockerfile + refresh.sh）放 /opt/bus-pooling/kirodrop-refresh/
#   2. docker build -t kdrop-refresh /opt/bus-pooling/kirodrop-refresh
#   3. 建 /opt/bus-pooling/.kdrop-creds（KDROP_EMAIL=.. / KDROP_PASSWORD=.. · chmod 600）
#   4. crontab: 0 * * * * /opt/bus-pooling/kirodrop-refresh/refresh.sh
set -euo pipefail

DIR=/opt/bus-pooling
ENV_FILE="$DIR/.env"
CREDS="$DIR/.kdrop-creds"
LOG="$DIR/kdrop-refresh.log"
IMAGE=kdrop-refresh
BASE="https://drop.kiro.ss"
KEY=BP_VENDOR_KIRODROP_SESSION_TOKEN

log() { echo "$(date -Is) $*" >> "$LOG"; }

cur_token() { grep "^${KEY}=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2-; }

# 0. 当前 token 仍有效 → 跳过（免无谓重启）
CUR="$(cur_token || true)"
if [ -n "${CUR:-}" ]; then
  code=$(curl -sS -m 12 -o /dev/null -w '%{http_code}' \
    "$BASE/api/v1/reservation?quantity=1&region=us" -H "Authorization: Bearer $CUR" || echo 000)
  if [ "$code" = "200" ]; then
    log "当前 token 仍有效($code) · 跳过"
    exit 0
  fi
  log "当前 token 失效($code) · 走刷新"
else
  log "无当前 token · 走刷新"
fi

# 1. 跑 ddddocr 容器登录拿新 token（凭证从受保护文件读 · 不写死脚本）
[ -f "$CREDS" ] || { log "❌ 缺 $CREDS"; exit 1; }
set -a; . "$CREDS"; set +a
NEW=$(docker run --rm \
  -e KDROP_BASE="$BASE" -e KDROP_EMAIL="$KDROP_EMAIL" -e KDROP_PASSWORD="$KDROP_PASSWORD" \
  "$IMAGE" 2>>"$LOG") || { log "❌ 登录容器失败(见上)"; exit 1; }

if [ -z "$NEW" ] || ! printf '%s' "$NEW" | grep -q '^kd_session_'; then
  log "❌ 没拿到合法 token · 不动 .env"
  exit 1
fi

# 2. 更新 .env（有则替换 · 无则追加）
cd "$DIR"
if grep -q "^${KEY}=" "$ENV_FILE"; then
  # 用 | 作分隔避免 token 里的 / 冲突（token 是 [A-Za-z0-9_-] · 安全）
  sed -i "s|^${KEY}=.*|${KEY}=${NEW}|" "$ENV_FILE"
else
  echo "${KEY}=${NEW}" >> "$ENV_FILE"
fi
chmod 600 "$ENV_FILE"

# 3. 重启 app 加载新 token
docker compose up -d --force-recreate >>"$LOG" 2>&1
log "✅ token 已刷新 + app 重启"
