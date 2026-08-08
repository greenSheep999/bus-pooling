# internal/config

读 yaml 配置 + env 覆盖。

**主要类型**：`Config`（`Server` / `DB` / `HTTPX` / `Housepool` / `Secrets` 五段）。

**铁律**：敏感值（主密钥 / admin key / vendor 凭证）**只从 env 读**，不进 yaml —— yaml 会进 git。
`Validate()` 校验普通字段；`RequireSecrets()` 单独校验密钥，这样 `migrate` 子命令不用配密钥也能跑。
