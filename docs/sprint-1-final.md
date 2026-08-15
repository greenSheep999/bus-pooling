# sprint-1-final · 阶段 1 收官上线路线

> **本文只写"阶段 1 全做完之后 · 怎么切真链路 → 上线"**。
>
> 阶段 1 每个 milestone 的交付清单在各自 `sprint-1a/1b/1c/1d/1e-backend.md`。
> 长期契约(架构 / API / DB)在 `00-14` A 契约。
>
> 本文是阶段 1 的**上线执行手册** —— 从 code-complete 到真流量的过渡。

## 收官定义

**术语双档**（1f 收口后固化 · 别再混）：
- **code-complete** = 1a-1e 全绿 · 主线代码路径都在，但**策略层收口未做**
- **feature-complete** = code-complete + 1f 全绿（策略优先级铁律 + 全局默认字段对齐 + `Effective()` 唯一入口）· 阶段 1 **代码不再需要新增**
- **阶段 1 收官上线** = feature-complete + mock/live 分阶段切换 + 真链路小流量 24h 无 P0/P1

**当前状态**(2026-08-15 · sprint-1e 收官后期):
- ✅ **Stage 0-1 完成** · 支付真链路全自动到账验证通过(3 笔 0.15 USD)
- ⚠️ **Stage 2 部分** · housepool 存活探活通 · 拉号进 housepool 未验(依赖 Stage 3)
- ⚠️ **Stage 3+ 阻塞** · **上游 vendor 暂只支持个人号 · 不支持外部拉号** · 需协议兼容后才能开
- 6 P0 sprint-1e 收官 issue 全修:refill FK 787 · channel_fee 隐藏 · webhook 域名修 · 优惠码全链路等
- **前进条件**:上游 vendor 侧支持"外部拉号"协议后 · Stage 3 单家 vendor smoke → Stage 4-6 逐个开 → Stage 7 归档

**不是一个总开关** —— 是多把开关分阶段打开。上线路线上任一 stage 卡住 = 未上线。

## code-complete + feature-complete 前提(1a-1f 全绿)

- [x] **1a** · 账号 / 钱包 / 手动拉号 / 1 人 bus / handoff / 状态机(生产在跑)
- [x] **1b** · 6 家 vendor / 兑换码 / payment-gateway(生产在跑)
- [x] **1c** · anon 撮合 + team 邀请码 + 分摊(生产在跑)
- [x] **1d** · 自动补车 / webhook 唤醒 / 比价 fallback / 号死补车(codex 六刀收敛完成 · 30 commit 已 push)
- [x] **1e** · 推 passengerpool 双写 + 对外 webhook(8 commit 已 push · code-complete)
- [x] **1f** · 策略优先级铁律 + `internal/strategy.Effective()` 唯一入口 + 全局默认三字段对齐 + `/docs` 对接文档扩(见 `archive/sprint-1f-scope.md` · **落码完成 = feature-complete**)

## Stage 序列(分阶段切真链路)

### Stage 0 · code-complete 验收 ✅ 完成

- [x] 1a-1f 全绿(1f 落码完成 · phase-1-acceptance-v2.md 4 层验证通过)
- [x] 全套 CI 通过 · 本地 go test ./... 37/37 packages · npm run build 通 · CI lint 有 pre-existing 违规不阻塞
- [x] 全部 mock 模式跑通端到端测试(smoke-1f.sh)

### Stage 1 · 只切 payment gateway 真链路 ✅ 完成(2026-08-15)

- [x] `BP_GW_BASE / BP_GW_TOKEN / BP_GW_SETTLEMENT_SECRET` 配真值(vps22 `/opt/bus-pooling/.env`)
- [x] `BP_GW_SUCCESS_URL` = `https://kirobus.com/wallet`
- [x] vendor 仍 mock · housepool 仍 mock(阶段 1 收官 gate 剥离)
- [x] 走通:充值 → settlement webhook 验签 → wallet_ledger 双条 recharge + channel_fee(3 笔 0.15 USD 实付验证)
- [x] `GATEWAY_PUBLIC_BASE_URL` 由错误 `vendor.kirobus.com/gateway` 修正为 `https://kirobus.com/gateway`(Caddyfile 加反代 · 撤 vendor 子域污染)
- [x] waffo store webhook 通过 SDK Add() 注册 · **不需人工去 waffo 后台配**
- [x] 全自动到账验证:`webhook applied` + `settlement delivered attempts=1` · 无手动 settle
- [x] 钱包流水 §12.6 清理:channel_fee 隐藏 · recharge 显净额(避免 "-0" 困惑)
- [x] refund / reversed / pending_topup janitor 恢复 · 单元测试覆盖
- [x] **目的**:"钱进钱包" 的链路安全 · **达成**

### Stage 2 · 切 housepool 真链路 ✅ 我方部分完成(2026-08-16)

- [x] `housepool.base_url` = `https://kiro.aibbq.xyz`(config.yaml 已配)
- [x] `housepool.admin_key` 通过 kiro.rs config.json adminApiKey 装配 · 阶段 1a 已实装
- [x] smoke-1f 验证 housepool 存活(kiro.aibbq.xyz update/check 返 running)
- [x] **手动 BatchImport 一号(2026-08-16 · ksk_...w4DV)进 `bus-<bus_id>` group · verify=true 通 · 返 KIRO PRO+ / usage 62/2000 · groups 挂对** —— 证 housepool 承载 API 全部工作
- [ ] **bus-pooling 侧走真拉号流程写 credential_ledger** 未验(依赖 Stage 3 上游支持外部拉号)
- [ ] deathwatch 读 housepool 探活标 credential_ledger 死 · 未跑(依赖上一条)

**Stage 2 结论**: 号池 API 层全通 · 差 bus-pooling 侧走拉号链路把号从 vendor 拉进 housepool(依赖 Stage 3)

### Stage 3 · 切单家 vendor 真链路 ⚠️ **阻塞:上游只支持个人号 · 暂不支持外部拉号**

- [x] `DRY_RUN=0`(vps22 生产已切)
- [ ] `BP_ALLOW_LIVE_PULL` = 1 · 阶段 1 收官 gate 未开
- [x] 6 家 vendor `api_key` 全部通过 `seed-vendor` 落 `vendor_account` 表 · active
- [ ] **上游 vendor 暂不支持外部拉号 · 只有个人号** · 需先兼容个人号协议才能拉
- [ ] Stage 3 具体验收(拉 1 个号 · 扣款 · 进 housepool)推迟到上游支持后

### Stage 4 · 逐家 vendor 打开 ⚠️ 依赖 Stage 3

- [ ] 上游支持外部拉号后 · 6 家单独开 · 每家 24h 观察

### Stage 5 · 打开自动模式(1d 生效) ⚠️ 依赖 Stage 3

- [x] 用户策略默认 `auto_refill_enabled=false`(保 UI 契约)
- [x] bus.Scheduler / stockwatch / janitor 全装配 · 已经在跑(FK 787 修复后无崩溃)
- [ ] 单车灰度打开 · 需 Stage 3 通了才有真号进车才能验

### Stage 6 · 打开 1e 外发(最后) ⚠️ 依赖 Stage 3

- [x] passengerpool 双写代码路径全通 · Pusher 装配 · smoke-1f 有单次手动触发(k2a `latency_ms=602 ok:true`)
- [x] outbound webhook 分派器已装配(webhookout.Dispatcher · 3-attempts retrier · retry 队列)
- [x] passenger_downstream 表结构就位(passengerpool_url + webhook_url + 加密 token)
- [ ] **生产真流量验证**未做:leedx2011 账号未在设置里配下游 URL · 无从触发
- [ ] 依赖 Stage 3 通了 · 真号拉进车才有 push_pool 事件驱动 outbound

**Stage 6 结论**: 装配 + 单元测试全过 · 需 Stage 3 有真号 + 用户配下游 URL 才能真验

### Stage 7 · 打 tag · 部署 · 归档

- [ ] Stage 1-6 观察 24h 无 P0/P1
- [ ] git tag 打标记(具体命名规则跟运维对齐)
- [x] `sprint-1a-backend.md` / `sprint-1a-frontend.md` 归档到 `docs/archive/`（2026-08-15 · 1f 落码完成后归档）
- [x] `sprint-1b-backend.md` / `sprint-1c-backend.md` / `sprint-1d-backend.md` / `sprint-1e-backend.md` / `sprint-1f-scope.md` 归档到 `docs/archive/`
- [ ] `sprint-1-final.md`(本文) 归档到 `docs/archive/`（Stage 7 结尾操作 · 阶段 1 完全收官后）
- [ ] 进入阶段 2 · 新建 `sprint-2a-backend.md`

## 环境变量填充清单 · 明早用户看这个

**参考**：`docs/1f-audit.json` —— 结构化列出所有 1f 审计到位后的字段/API/DTS 对齐现状 + 差距。用户 stage 切换时按这份决定填哪些 env、灰度哪几辆车。

**关键 env 三档**：

| 档 | 环境变量 | Stage |
|---|---|---|
| **money live** | `BP_GW_BASE` / `BP_GW_TOKEN` / `BP_GW_SETTLEMENT_SECRET` / `BP_GW_SUCCESS_URL` | Stage 1 |
| **housepool live** | `housepool.base_url` / `housepool.admin_key` / `housepool.expected_version` | Stage 2 |
| **vendor live** | `DRY_RUN=0` / `BP_ALLOW_LIVE_PULL=1` / `decider.default_vendor` + 单家 vendor `api_key` + `webhook_secret` | Stage 3-4 |

**不需要再写代码** —— Stage 1-6 全部只需要 env + 数据库配置 + UI 灰度切换。阶段 1 feature-complete 意味着代码层面阶段 1 已经收工。

## 阶段 1 收官后 · 进阶段 2

阶段 2 目标见 `docs/00-values-and-phases.md §7`:
- **2a** · 列队策略(多 bus 抢同一 vendor 时排队;bus 内集单窗口调优)
- **2b** · 压车治理(bus 内噪邻探测 + 限速降级)

新的 sprint 主线文档在**阶段 2 各 milestone 开工时才建**。**别提前造** —— 保持 sprint 文档跟当前工作面对齐。
