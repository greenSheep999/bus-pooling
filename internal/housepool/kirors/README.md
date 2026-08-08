# internal/housepool/kirors

`housepool.HousePool` 针对 kiro.rs 的实现。**上层不 import 这个包。**

**文件分工**：`client.go` HTTP 调用 · `types.go` wire 类型（不外暴）· `mapper.go` wire → 归一化。

**几条核对过源码的实况**（`docs/08-housepool-contract.md §10b`）：
- 端点都在 **`/api/admin`** 前缀下 · `Config.BaseURL` 只填域名，前缀由 client 内部拼
- 鉴权 header **`x-api-key`**
- JSON 是 **camelCase**。这个包是 camelCase 和我方 snake_case 的唯一交界处，别让它漏到上层
- 列表端点返回**包着的对象**（`{total, credentials:[...]}`），且顺带给池子聚合值 →
  `ListCredentials` 一并返回 `PoolSnapshot`，不用另打 stats
- `SetDisabled` 的 body **只有 `{disabled}`** —— 传不进自定义 reason
- **没有单号 GET** —— `GetCredential` 走列表后过滤

**BatchImport 是 SSE**：kiro.rs 返回**一个**流，summary 是流里 `status=="summary"` 的
最后一个事件。我方拆成 `Events` / `Summary` 两个 channel 让上层用起来清楚。

SSE **不走 `httpx.Do`**（那个会把 body 读完才返回，流式下等于白等），走 `httpx.Stream`。
`Stream` 故意**不重试** —— 流可能已经导进去几个号了，重放会重复导入。
