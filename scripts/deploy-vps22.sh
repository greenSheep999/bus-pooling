#!/usr/bin/env bash
# vps22 一键部署 · 替换旧 kiro-auto container · 上新 bus-pooling 镜像 (kirobus tag)
#
# 前置：
#   - GHCR 镜像已经 build 完（GitHub Actions ghcr-build.yml 跑绿）
#   - vps22 已经 docker login ghcr.io（.docker/config.json 里 ghcr.io 项已在）
#   - vps22 上 /opt/bus-pooling 由本脚本创建·docker-compose.yml + .env + config.yaml 由脚本 scp 过去
#   - 本机 BP_MASTER_KEY 已在 .dev.env（或生成新的，脚本会引导）
#
# 使用：本机 `bash scripts/deploy-vps22.sh`

set -euo pipefail

REMOTE=vps22
REMOTE_DIR=/opt/bus-pooling
IMAGE=ghcr.io/greensheep999/bus-pooling:kirobus

echo "===== step 0 · 本地检查 ====="
if [ ! -f .dev.env ]; then
  echo "!! .dev.env 缺失·脚本需要它来 seed vendor 凭证"
  exit 1
fi

echo ""
echo "===== step 1 · 确认镜像已经在 GHCR ====="
if ! docker manifest inspect "$IMAGE" >/dev/null 2>&1; then
  echo "!! 镜像 $IMAGE 还没上 GHCR"
  echo "   看 https://github.com/greenSheep999/bus-pooling/actions"
  echo "   等 Build Docker image workflow 绿了再跑本脚本"
  exit 1
fi
echo "OK · $IMAGE 存在"

echo ""
echo "===== step 2 · 准备 /opt/bus-pooling 部署目录 ====="
ssh "$REMOTE" "mkdir -p ${REMOTE_DIR}/data"

echo ""
echo "===== step 3 · scp docker-compose.yml + config.yaml ====="
# 生产 config.yaml 由本地 config.example.yaml 生成 · 只留业务参数（不敏感）
scp docker-compose.yml "${REMOTE}:${REMOTE_DIR}/docker-compose.yml"

# config.yaml · 优先用本地 config.yaml · 没就用 example
if [ -f config.yaml ]; then
  scp config.yaml "${REMOTE}:${REMOTE_DIR}/config.yaml"
else
  echo "!! 本地 config.yaml 不存在 · 用 config.example.yaml 兜底（生产可能要改）"
  scp config.example.yaml "${REMOTE}:${REMOTE_DIR}/config.yaml"
fi

echo ""
echo "===== step 4 · 准备 .env（只放 BP_MASTER_KEY / BP_ADMIN_KEY · 敏感凭证走 seed-vendor CLI）====="
ssh "$REMOTE" bash <<REMOTE_SH
set -euo pipefail
ENV_FILE=${REMOTE_DIR}/.env
if [ ! -f "\$ENV_FILE" ]; then
  echo "-- 生成新 .env --"
  # BP_MASTER_KEY 从本地传过来 · BP_ADMIN_KEY 本地 seed(admin/* 端点鉴权)
  ADMIN_KEY=\$(openssl rand -hex 32)
  cat > "\$ENV_FILE" <<ENV_END
BP_MASTER_KEY=${BP_MASTER_KEY:-CHANGE_ME_RUN_GENKEY}
BP_ADMIN_KEY=\$ADMIN_KEY
BP_ADDR=:8080
BP_DB_PATH=/app/data/bus-pooling.db
BP_CONFIG=/app/config.yaml
DRY_RUN=0
ENV_END
  chmod 600 "\$ENV_FILE"
  echo "-- .env 写入 · chmod 600 · BP_ADMIN_KEY 已 seed(cat 一下取 X-Admin-Key 值) --"
else
  echo "-- .env 已存在·保留 --"
  # I-29 admin toggle 部署后 · 补 BP_ADMIN_KEY(老 .env 没这行)
  if ! grep -q "^BP_ADMIN_KEY=" "\$ENV_FILE"; then
    ADMIN_KEY=\$(openssl rand -hex 32)
    echo "BP_ADMIN_KEY=\$ADMIN_KEY" >> "\$ENV_FILE"
    echo "-- 补 BP_ADMIN_KEY 到 .env(老 .env 没这行 · 现在补上让 admin/* 端点可用) --"
  fi
fi
REMOTE_SH

echo ""
echo "===== step 5 · 停旧 kiro-auto container（保留数据卷·可回滚）====="
ssh "$REMOTE" bash <<'REMOTE_SH'
set -euo pipefail
echo "-- 当前 9917 上跑的：--"
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Ports}}' | grep -E "9917|kiro-auto" || echo "(无)"
echo ""
docker stop kiro-auto kiro-auto-supertokens-1 kiro-auto-supertokens-db-1 2>/dev/null || true
docker rm kiro-auto kiro-auto-supertokens-1 kiro-auto-supertokens-db-1 2>/dev/null || true
echo "-- 旧 container 已停 + 删 · SuperTokens PostgreSQL 数据卷保留 --"
REMOTE_SH

echo ""
echo "===== step 6 · 拉新镜像 + 迁移 + 起容器 ====="
ssh "$REMOTE" bash <<REMOTE_SH
set -euo pipefail
cd ${REMOTE_DIR}
docker compose pull

# I-36 · migrate 竞态修复:app 拒启动("有未应用的迁移") → docker exec 就 exec 不进去 →
# 死锁。所以先 docker run --rm 跑 migrate up · 再 compose up · 让 app 起来时表已就绪。
echo "-- 迁移 up(compose up 之前 · 避免 pending migration 让 app crashloop 死锁) --"
docker run --rm \
  --env-file ${REMOTE_DIR}/.env \
  -v ${REMOTE_DIR}/data:/app/data \
  -v ${REMOTE_DIR}/config.yaml:/app/config.yaml \
  $IMAGE migrate up

docker compose up -d
sleep 8
echo ""
docker ps --filter name=kirobus --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
REMOTE_SH

echo ""
echo "===== step 7 · seed 6 家 vendor 凭证 · 从本地 .dev.env 读明文加密写表 ====="
# 从本地 .dev.env 加载
set -a; . ./.dev.env; set +a

seed_one() {
  local slug="$1"; local key_var="$2"
  local key="${!key_var:-}"
  if [ -z "$key" ]; then
    echo "-- SKIP $slug · $key_var 空 --"
    return
  fi
  ssh "$REMOTE" "docker exec kirobus /app/bus-pooling seed-vendor $slug --api-key='$key'" \
    && echo "OK · seeded $slug" \
    || echo "!! FAIL · seed $slug"
}

seed_one kiro91    BP_VENDOR_KIRO91_API_KEY
seed_one kiroceo   BP_VENDOR_KIROCEO_API_KEY
seed_one kirooo    BP_VENDOR_KIROOOO_API_KEY
seed_one kiroappio BP_VENDOR_KIROAPPIO_API_KEY
seed_one kiroappcc BP_VENDOR_KIROAPPCC_API_KEY
seed_one kirodrop  BP_VENDOR_KIRODROP_API_KEY

echo ""
echo "===== step 8 · 触发重启让 adapter 读表凭证 ====="
ssh "$REMOTE" "cd ${REMOTE_DIR} && docker compose restart"

sleep 8
ssh "$REMOTE" "docker ps --filter name=kirobus --format '{{.Status}}'"

echo ""
echo "===== step 9 · 外部验证 ====="
echo "curl https://kirobus.com/healthz"
curl -sS -I https://kirobus.com/healthz | head -3
echo ""
echo "如需回滚："
echo "  ssh $REMOTE 'docker compose -f ${REMOTE_DIR}/docker-compose.yml down && docker start kiro-auto'"
