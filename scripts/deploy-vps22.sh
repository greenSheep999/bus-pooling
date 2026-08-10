#!/usr/bin/env bash
# vps22 一键部署 · 替换旧 kiro-auto container · 上新 bus-pooling (kirobus 镜像)
#
# 前置：
#   - GHCR 镜像已经 build 完（GitHub Actions ghcr-build.yml 跑绿）
#   - vps22 已 `docker login ghcr.io`
#   - .env + config.yaml 已经在 /opt/bus-pooling/ 下准备好
#
# 使用：本机 `bash scripts/deploy-vps22.sh` · 通过 SSH 到 vps22 执行

set -euo pipefail

REMOTE=vps22
REMOTE_DIR=/opt/bus-pooling
IMAGE=ghcr.io/greensheep999/bus-pooling:kirobus

echo "===== step 1 · 确认镜像已经在 GHCR ====="
docker manifest inspect "$IMAGE" >/dev/null 2>&1 \
  || { echo "!! 镜像 $IMAGE 还没上 GHCR · 等 Actions build 完再跑本脚本"; exit 1; }
echo "OK · $IMAGE 存在"

echo ""
echo "===== step 2 · SSH 到 vps22 · 拉镜像 ====="
ssh "$REMOTE" docker pull "$IMAGE"

echo ""
echo "===== step 3 · 备份旧 container + 停 kiro-auto 相关 ====="
ssh "$REMOTE" bash <<REMOTE_SH
set -euo pipefail
echo "-- 当前占 9917 的 =="
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Ports}}' | grep -E "9917|kiro-auto" || echo "(无)"
echo ""
echo "-- 停 kiro-auto 相关（保留数据卷·可回滚） --"
docker stop kiro-auto kiro-auto-supertokens-1 kiro-auto-supertokens-db-1 2>/dev/null || true
docker rm kiro-auto 2>/dev/null || true
echo "-- 旧 container 已停 · SuperTokens 数据卷保留 · 需要回滚可 docker start --"
REMOTE_SH

echo ""
echo "===== step 4 · 准备 /opt/bus-pooling 部署目录 ====="
ssh "$REMOTE" bash <<REMOTE_SH
set -euo pipefail
mkdir -p ${REMOTE_DIR}/data
cd ${REMOTE_DIR}
if [ ! -f docker-compose.yml ]; then
  echo "!! ${REMOTE_DIR}/docker-compose.yml 缺失 · scp 一份过来："
  echo "   scp docker-compose.yml ${REMOTE}:${REMOTE_DIR}/"
fi
if [ ! -f .env ]; then
  echo "!! ${REMOTE_DIR}/.env 缺失 · 需要手工创建（BP_MASTER_KEY + 6 家 vendor API key）"
fi
if [ ! -f config.yaml ]; then
  echo "!! ${REMOTE_DIR}/config.yaml 缺失 · 需要手工创建"
fi
REMOTE_SH

echo ""
echo "===== step 5 · 起新容器 ====="
ssh "$REMOTE" bash <<REMOTE_SH
set -euo pipefail
cd ${REMOTE_DIR}
docker compose pull
docker compose up -d
sleep 5
echo ""
echo "-- 起后状态 --"
docker ps --filter name=kirobus --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
echo ""
echo "-- 健康检查（等 15s） --"
sleep 10
docker ps --filter name=kirobus --format '{{.Status}}'
REMOTE_SH

echo ""
echo "===== step 6 · 外部验证 ====="
echo "本机测："
echo "  curl -I https://kirobus.com/healthz"
echo "  curl https://kirobus.com/api/vendors/status"
echo ""
echo "如需回滚 kiro-auto："
echo "  ssh $REMOTE 'docker compose -f ${REMOTE_DIR}/docker-compose.yml down && docker start kiro-auto kiro-auto-supertokens-1 kiro-auto-supertokens-db-1'"
