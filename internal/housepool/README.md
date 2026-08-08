# internal/housepool

我方号池的**抽象层**。上层（decider / pullrecord / bus / deathwatch）**只 import 这个包**，
不 import 具体实现（`kirors`）—— 换号池实现只需加一个新子包。

**主要类型**：`HousePool`（interface）· `Credential` · `Group` · `BatchImportResult` · `Error`。

**这里的类型是归一化后的形状**。号池实际的 wire 格式不一样（kiro.rs 是 camelCase 且列表
端点包了一层），翻译在 `kirors/mapper.go`。

**group 名别手拼** —— 用 `BusGroup(id)` / `RecordGroup(pid)` / `MarketGroup`。拼错一个字
号就进错组，而且很难查。

**`DisabledReason` 判据**（deathwatch 依赖 · 见 `docs/08-housepool-contract.md`）：
- `IsDeadReason(r)` → 可直接判死（`Suspended` / `QuotaExceeded` / `InvalidRefreshToken`）
- `NeedsProbe(r)` → 号池侧可能自愈，要用 `TestCredential` 复核
- **`Manual` 两个都返 false** —— 那是我方主动 disable 的（拉号记录待派 / handoff 待确认 /
  成员挂起）。**当成死号会把全部待派号误标**

**1a 实现范围**：Credential 增删改查 + Group + BatchImport + Ping。
ClientKey（1e）和 Stats（1d）留在接口里但返 `ErrNotSupported` —— 明确报错好过静默返空。
