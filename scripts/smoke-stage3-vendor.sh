#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────
# Stage 3 smoke · 单家 vendor 真链路
#
# 覆盖 sprint-1-final.md Stage 3 · 单家 vendor 真拉号:
#   ① DRY_RUN=0 + BP_ALLOW_LIVE_PULL=1 · 双锁开
#   ② vendor api_key 装 vendor_account 表 · seed-vendor 命令
#   ③ 起服务 · vendor 探针跑 60s · vendor_probe 落库
#   ④ 手动 POST /me/pull count=1 · 走真 vendor 扣款
#   ⑤ 号进 housepool prebuy-pool group → assign into_bus 迁 bus-<id>
#   ⑥ credential_ledger 落 status=alive · key_masked 非空
#
# 前置:
#   - Stage 1&2 完成 · vendor api_key seed 过
#   - **当前 blocking**:上游 vendor 只支持个人号 · 不支持外部拉号
#     (sprint-1-final Stage 3 记 · 需 vendor 侧协议兼容)
# ─────────────────────────────────────────────────────────────
set -euo pipefail

echo "==> Stage 3 smoke · 单家 vendor · **当前上游 blocking**"
echo "TODO · issues-log I-11 · Stage 3 vendor smoke 待细化"
echo "现走 smoke-1f.sh 综合脚本(mock vendor 走通)"
exit 0
