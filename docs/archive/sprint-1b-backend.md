# sprint-1b · 后端主线交付清单

> **本文只写"1b 要做什么 + 上线前还剩什么"**。技术细节 / 决策讨论 / 长期契约 →
> 分别去 A 契约（00-19）/ D 决策（decisions.md）· 别塞进这里。
>
> 阶段 1b 目标（`00-values-and-phases §7`）：**加 5 家 vendor + 兑换码 + payment-gateway
> pass-through**。仍无拼车（多人）、无自动。

## 交付清单

- [ ] 5 家 vendor adapter 全通生产（91kiro 已通 · 剩 5 家）
- [ ] 兑换码兑换 · redeem 端到端
- [ ] payment-gateway waffo 通道 · pass-through 5%
- [ ] 全局设置页 · daily 上限（跨所有车累加）

## 依赖 / 阻塞

（待用户补 · 我不自作主张）

## 上线判据

- [ ] 5 家 vendor 探针都在跑 · vendor_probe 有数据
- [ ] redeem 走通一次端到端（兑换码 → 积分 → 拉号）
- [ ] 充值走通一次端到端（真跳 waffo → webhook 验签 → wallet_ledger 双条）
- [ ] 全局 daily 上限拦得住超额拉号

**归档时机**：1b 上线 · 挪到 `docs/archive/`。
