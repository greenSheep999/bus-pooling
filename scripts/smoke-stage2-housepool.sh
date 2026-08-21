#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
# Stage 2 smoke · housepool 真链路
#
# 覆盖 sprint-1-final.md Stage 2 · 只测号池承载链路:
#   ① housepool.base_url 配 kiro.aibbq.xyz · admin_key 装配
#   ② smoke-1f.sh update/check 端点走通(号池活着)
#   ③ 手动 BatchImport 一号进 bus-<id> group · verify=true 通
#   ④ 号池返回 usage / groups 挂对
#   ⑤ deathwatch 读号池探活 · 标 credential_ledger 死
#
# 前置:
#   - Stage 1 已完成(payment 通)· dev.env 有 BP_HOUSEPOOL_URL / _ADMIN_KEY
# ─────────────────────────────────────────────────────────────
set -euo pipefail

BP_ROOT="${BP_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8091}"

echo "==> Stage 2 smoke · housepool"

# 待补:完整 curl 序列 · 参照 smoke-1f.sh housepool 段
echo "TODO · issues-log I-11 · Stage 2 housepool smoke 待细化"
echo "现走 smoke-1f.sh 综合脚本"
exit 0
