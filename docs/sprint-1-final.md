# sprint-1-final · 阶段 1 收官上线路线

> **本文只写"阶段 1 全做完之后 · 怎么切真链路 → 上线"**。
>
> 阶段 1 每个 milestone 的交付清单在各自 `sprint-1a/1b/1c/1d/1e-backend.md`。
> 长期契约(架构 / API / DB)在 `00-14` A 契约。
>
> 本文是阶段 1 的**上线执行手册** —— 从 code-complete 到真流量的过渡。

## 收官定义

**阶段 1 收官上线** = 阶段 1 code-complete(1a-1e 全绿) + mock/live 分阶段切换验收 + 真链路小流量运行 24h 无 P0/P1。

**不是一个总开关** —— 是多把开关分阶段打开。上线路线上任一 stage 卡住 = 未上线。

## code-complete 前提(1a-1e 全绿)

- [x] **1a** · 账号 / 钱包 / 手动拉号 / 1 人 bus / handoff / 状态机(生产在跑)
- [x] **1b** · 6 家 vendor / 兑换码 / payment-gateway(生产在跑)
- [x] **1c** · anon 撮合 + team 邀请码 + 分摊(生产在跑)
- [x] **1d** · 自动补车 / webhook 唤醒 / 比价 fallback / 号死补车(codex 六刀收敛完成 · 30 commit 已 push)
- [ ] **1e** · 推 passengerpool 双写 + 对外 webhook(未开始 · 见 `sprint-1e-backend.md`)

## Stage 序列(分阶段切真链路)

### Stage 0 · code-complete 验收

- [ ] 1a-1e 全绿
- [ ] 全套 CI 通过(`go build + vet + test -race + 敏感字扫 + npm ci/build/lint + 内部术语 lint`)
- [ ] 全部 mock 模式跑通端到端测试

### Stage 1 · 只切 payment gateway 真链路

- [ ] `BP_GW_BASE / BP_GW_TOKEN / BP_GW_SETTLEMENT_SECRET` 配真值
- [ ] `BP_GW_SUCCESS_URL` 指向线上前端
- [ ] vendor 仍 `DRY_RUN=true` · housepool 仍 mock
- [ ] 走通:充值 → settlement webhook 验签 → wallet_ledger 双条 recharge + channel_fee
- [ ] 走通:refund / reversed / pending_topup janitor 恢复
- [ ] **目的**:"钱进钱包" 的链路安全

### Stage 2 · 切 housepool 真链路

- [ ] `housepool.base_url` 配线上 kiro.rs(kiro.aibbq.xyz)
- [ ] `housepool.admin_key` 配真值
- [ ] `housepool.expected_version` 配一致语义版本
- [ ] vendor 仍 mock(DRY_RUN=true)
- [ ] 走通:DryRunVendor Purchase 返假号 → BatchImport 进真 housepool → 分组 / 客户端 key / handoff / delete
- [ ] 走通:deathwatch 读 housepool 探活状态 · 标死 credential_ledger
- [ ] **目的**:"号池承载" 安全

### Stage 3 · 切单家 vendor 真链路

- [ ] `DRY_RUN=0`
- [ ] `BP_ALLOW_LIVE_PULL=1`
- [ ] 只启用一家 vendor(`config.vendors` enabled=true 保留一家)
- [ ] `decider.default_vendor` = 该 vendor 的 id
- [ ] 该 vendor `api_key` + `webhook_secret` 走 `seed-vendor` 落 `vendor_account` 表
- [ ] vendor 侧充值一小笔余额(几十积分)
- [ ] 走通:用户手动拉 1 个号 → 真扣 vendor 侧钱 → 号进 housepool → 我方钱包扣积分
- [ ] 走通:vendor 侧 webhook 到 · 归一化验签落 `vendor_dispatch`
- [ ] **目的**:"真钱链路 · 单家 vendor" 闭环

### Stage 4 · 逐家 vendor 打开

- [ ] 每家单独开 · stock / purchase / no_stock / webhook 逐个验
- [ ] **不要六家一起开** · 每家单独观察 vendorbalance / vendor_dispatch / 探针数据 24h
- [ ] **目的**:逐家边界暴露

### Stage 5 · 打开自动模式(1d 生效)

- [ ] 用户策略默认 `auto_refill_enabled=false`(保 UI 契约 · 见 `decisions §12.已定 2026-08-15`)
- [ ] 单车灰度打开 · 观察 bus.Scheduler / deathwatch.RefillTick / stockwatch / janitor
- [ ] 观察 30 分钟 · 无异常再放开更多车
- [ ] **目的**:自动路径与真钱链路联动安全

### Stage 6 · 打开 1e 外发(最后)

- [ ] passengerpool 双写:先内部账号验证 · 再灰度用户
- [ ] outbound webhook:先只推 `boarded` 一种事件 · 观察 3-attempts 重试逻辑
- [ ] **目的**:外部系统影响面 · 回滚成本最高 · 放最后

### Stage 7 · 打 tag · 部署 · 归档

- [ ] Stage 1-6 观察 24h 无 P0/P1
- [ ] git tag 打标记(具体命名规则跟运维对齐)
- [ ] `sprint-1a-backend.md` / `sprint-1a-frontend.md` 归档到 `docs/archive/`
- [ ] `sprint-1b-backend.md` / `sprint-1c-backend.md` / `sprint-1d-backend.md` / `sprint-1e-backend.md` 归档到 `docs/archive/`
- [ ] `sprint-1-final.md`(本文) 归档到 `docs/archive/`
- [ ] 进入阶段 2 · 新建 `sprint-2a-backend.md`

## 阶段 1 收官后 · 进阶段 2

阶段 2 目标见 `docs/00-values-and-phases.md §7`:
- **2a** · 列队策略(多 bus 抢同一 vendor 时排队;bus 内集单窗口调优)
- **2b** · 压车治理(bus 内噪邻探测 + 限速降级)

新的 sprint 主线文档在**阶段 2 各 milestone 开工时才建**。**别提前造** —— 保持 sprint 文档跟当前工作面对齐。
