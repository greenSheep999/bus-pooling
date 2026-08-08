# internal/api

HTTP 层：路由 / 中间件 / 请求响应编解码。

**主要类型**：`Server` · `handler`（返回 error 的 handler 形状）· `Fail` / `Error`（错误响应）。

**两条铁律**（`CLAUDE.md §12`）：
- 返回体和 message **绝不出现内部术语**（housepool / record group / provider / 内部状态枚举）
  —— 有测试扫这个
- 状态对外收敛（§12.5）· 内部多态在这一层映射成用户能看懂的少数几个

**handler 返回 error 而不是各自 writeFail**：统一在一处转成响应，避免漏写 `return`
导致写两次响应体。

**鉴权三档**：
- 公开：注册 / 登录 / 登出
- `RequireAuth`：会话 cookie 或 API key
- `RequireSession`：**只**会话 —— 改密码 / 建 API key 用这个（见 passenger README）

**decodeJSON 拒绝未知字段** —— 客户端拼错字段名时早报错，而不是静默忽略然后行为跟预期不符。
