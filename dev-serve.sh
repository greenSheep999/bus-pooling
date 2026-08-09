#!/usr/bin/env bash
# dev-serve.sh · 本地开发起后端 · Vite 会代理 /api 到这个端口
#
# 用法：bash dev-serve.sh
#
# 第一次跑：生成新 master key + 建库 + 拉 migration + 起服务（DRY_RUN 默认开）
# 之后跑：读复用 .dev.env 里存的 key + 直接起服务
#
# 想重来：rm .dev.env data/dev.db 再跑一次

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

ENV_FILE=".dev.env"
DB_PATH="${BP_DB_PATH:-$REPO_ROOT/data/dev.db}"

if [ ! -f "$ENV_FILE" ]; then
  echo "==> 首次跑 · 生成 master key + 迁移库"
  KEY=$(go run ./cmd/bus-pooling genkey | tr -d '\n' | sed -E 's/^BP_MASTER_KEY=//')
  cat > "$ENV_FILE" <<EOF
BP_MASTER_KEY=$KEY
BP_DB_PATH=$DB_PATH
BP_ADDR=127.0.0.1:8080
BP_INSECURE_COOKIE=1
BP_RATE_SERVICE_BP=500
DRY_RUN=1
BP_ENABLE_DEV_TOPUP=1
BP_CONFIG=$REPO_ROOT/config.yaml
EOF
  chmod 600 "$ENV_FILE"
  echo "==> $ENV_FILE 已写（key 不进 git · 已在 .gitignore）"
fi

# config.yaml 不在 git 里（.gitignore 屏蔽）· 首次从 example 复制一份
# 跑马灯文案 / 拉号参数 / max_members 都在里面·没这个文件那些功能就是默认值
if [ ! -f "$REPO_ROOT/config.yaml" ]; then
  cp "$REPO_ROOT/config.example.yaml" "$REPO_ROOT/config.yaml"
  echo "==> config.yaml 已从 example 复制（改它可调跑马灯 / 拉号参数）"
fi

# 载入
set -a; . "./$ENV_FILE"; set +a

mkdir -p "$(dirname "$DB_PATH")"

echo "==> 应用 migration"
go run ./cmd/bus-pooling migrate up

echo "==> 起服务 http://$BP_ADDR"
echo "    前端 dev server 通过 vite proxy 转发 /api"
echo "    Ctrl-C 停"
exec go run ./cmd/bus-pooling serve
