-- +migrate up

-- ── 038 · webhook 开关 + 订阅事件白名单落库 ────────────────────
--
-- **背景**：1a 阶段前端 UI 允许用户切换启用开关 / 勾选订阅事件 · 但后端 PUT /webhook
-- 只接受 URL · 其它字段扔掉；events 走硬编码 defaultWebhookEvents 全量。
-- 1e-2 webhookout 上线后 · 需要真根据用户配置决定"发不发 · 发哪些" · 补两列落库。
--
-- **不动 4 条推送策略**(push_on_pull / resync_on_dead / retry_on_failure / bus_only) ·
-- 那些是 passengerpool 双写用的 · 跟这里的 webhook 出向没关系。

-- 启用开关 · 默认 1(有 URL + secret 就自动启用 · 用户能显式关)
ALTER TABLE passenger_downstream ADD COLUMN webhook_enabled INTEGER NOT NULL DEFAULT 1;

-- 订阅事件白名单 · JSON 数组 · NULL = 未配置(视为全订阅 4 事件)
-- 用户在 UI 勾选后落 ["new_keys_available","boarded"] 这类
--
-- **为什么用 JSON 不建关联表**：只有 4 个事件 · 每用户最多 4 条 · 单列 JSON 够用 ·
-- 加关联表要多 3 个 SQL + join · 得不偿失。dispatcher 读时 json_extract 或
-- 应用层 Unmarshal 都可。
ALTER TABLE passenger_downstream ADD COLUMN webhook_events_json TEXT;

-- +migrate down
-- SQLite ALTER TABLE DROP COLUMN 3.35+ 支持 · 但为兼容旧版走"新表+复制"更保险
-- 这里选 DROP COLUMN(项目 CI/生产都用 3.35+ · docs/07-testing §db-version)
ALTER TABLE passenger_downstream DROP COLUMN webhook_events_json;
ALTER TABLE passenger_downstream DROP COLUMN webhook_enabled;
