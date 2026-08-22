#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
# Stage 1 smoke · payment gateway 真链路
#
# 覆盖 sprint-1-final.md Stage 1 · 只测支付到账链路:
#   ① 起服务(默认 mock vendor · dev-topup 是 mock 走 waffo 真链路才是 Stage 1)
#   ② 注册测试乘客 + session
#   ③ 起充值单 → gateway 起单 → 模拟 settlement webhook → 到账
#   ④ 查 wallet_ledger 双条(recharge / channel_fee) · 净变化 = +100 积分
#   ⑤ 幂等复演 · pending_topup / topup_order 状态机不错乱
#
# 前置:
#   - BP_GW_BASE / BP_GW_TOKEN / BP_GW_SETTLEMENT_SECRET 真值 · 或走 dev mock
#   - 生产 Stage 1 是 waffo · dev 环境走 dev-topup mock
# ─────────────────────────────────────────────────────────────
set -euo pipefail

BP_ROOT="${BP_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8091}"
SMOKE_PORT="${SMOKE_PORT:-8091}"
DB_PATH="${DB_PATH:-/tmp/bp-smoke-stage1.db}"

echo "==> Stage 1 smoke · payment · $BASE_URL"

# 待补:完整 curl 序列参照 smoke-1f.sh L45-160 段
# 阶段 1 收官时按 sprint-1-final Stage 1 完成清单填充 · 见 issues-log I-11
echo "TODO · issues-log I-11 · Stage 1 payment smoke 待细化"
echo "现走 smoke-1f.sh 综合脚本 · 里含 Stage 1-6 全链"
exit 0
