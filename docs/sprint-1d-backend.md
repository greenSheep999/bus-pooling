# sprint-1d · 后端主线交付清单

> **本文只写"1d 要做什么 + 上线前还剩什么"**。技术细节 / 决策讨论 / 长期契约 →
> 分别去 A 契约（00-19）/ D 决策（decisions.md）· 别塞进这里。
>
> 阶段 1d 目标（`00-values-and-phases §7`）：**自动模式（时钟 / 剩号少 / vendor
> webhook 触发）+ 号死自动补车 + 决策模型（比价 + fallback）**。无压车治理。

## 交付清单

- [x] 号死自动补车 · `deathwatch.RefillTick` 1min 扫队列 · 真调 `decider.Pull`
- [x] vendor 余额自动切换 · `BalanceChecker` + `PickBestVendorExcluding` · 没钱切下一家
- [x] 号寿命 + 存活率喂 AutoPick · `QualityStore` 真数据（不再是 50% 常数）
- [x] webhook 带的 price/available 落 vendor_probe_zone · source='webhook'
- [x] 陈旧管线告警外发 · `StalenessChecker` + `AlertNotifier`
- [ ] 自动模式 scheduler · `bus.Scheduler` 已 commit 但需按 `decisions §12` 六条位置改造：
    - [ ] 位置 1 · vendor 新号 webhook 唤醒范围（谁被喂）
    - [ ] 位置 2 · prebuy-pool 抢到无主号的分配路径
    - [ ] 位置 3 · 多 vendor 同车判据（"另一家撑得住就不拉"）
    - [ ] 位置 4 · 建拼车后第一次一律手动 · **由 UI 保证**（建车向导 + AutoRefillEnabled 默认 false · 见 decisions §12.已定 2026-08-15）
    - [ ] 位置 5 · 保底触发挂 stockwatch 不是硬下单
    - [ ] 位置 6 · 用户字段命名对齐（RefillWatermark 是补到几个还是紧急线）

## 依赖 / 阻塞

- 1c 完成（拼车分摊已通）
- 六条位置**等用户拍板** —— 拍完更新 `decisions.md §12` · 再照做

## 上线判据

- [x] 号死后 1min 内自动补车 · 生产验证过
- [x] vendor 余额不够自动切下一家 · 单测覆盖
- [ ] 自动模式 scheduler 六条位置全部拍板 + 落码 + 验证
- [ ] 陈旧管线告警真外发到 BP_ALERT_WEBHOOK（生产验证）

**归档时机**：1d 六条位置全通生产 · 挪到 `docs/archive/`。
