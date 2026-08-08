# internal/httpx

统一所有**出向** HTTP（CLAUDE.md §7.1：proxy / timeout / no_proxy 统一）。

**主要类型**：`Client` · `Response`（body 已读完，调用方不用管 Close）。

**重试策略**：网络错误 / 超时 / 429（听 `Retry-After`）/ 5xx 才重试；**4xx 不重试**（重试改变不了结果）。
重试用尽后返回**最后那个响应**而不是错误 —— 调用方要看到 503 才能决定怎么办。

vendor 和 housepool 的 client 都必须走这里，别各自 `new http.Client`。
