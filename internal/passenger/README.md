# internal/passenger

账号：注册 / 登录 / session / API key。

**主要类型**：`Store`（所有操作的入口）· `Passenger` · `APIKey`。

**密码**：Argon2id（`m=64MB, t=3, p=2` · 参数来自 `docs/03-modules.md §334`）。
参数编码在 PHC 串里，将来调参数不会让老用户登不进来。

**几条安全约束，改代码前先看**：
- **token 只存 SHA-256**（session token / API key 都是）· 库被读走也拿不到能用的凭证
- **登录不区分「账号不存在」和「密码错」** —— 都返回 `ErrWrongPassword`，否则接口成了账号枚举器。
  账号不存在时还会跑一次假 hash 校验，让两条路径耗时相近
- **API key 明文只在创建时返回一次**，之后任何端点都拿不到
- **吊销 API key 不删行** —— 台账留痕（售后要能查这把 key 什么时候建的、用过没）
- **停用账号要挡住两条鉴权路径**（session + API key），只挡一条等于没挡

**API key 权限收窄**在 api 层（`RequireSession`）：不能改密码、不能建新 key ——
防「泄露的 key 换成新 key 把主人锁在门外」。
