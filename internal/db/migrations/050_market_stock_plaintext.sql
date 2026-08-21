-- +migrate up
--
-- 050_market_stock_plaintext.sql · 手工池号 · admin 导入到 sold 之间的明文暂存
--
-- **为什么单独一张表**:issues-log I-01 · 手工池路径是"号先进 housepool prebuy-pool ·
-- 后由用户拉号触发 sold" —— sold 时才产生 credential_ledger.id。但**明文只在 admin
-- POST /api/admin/market/stock 那一刻进来一次** · 到 sold 时已经不在内存了。
--
-- credential_plaintext 的主键是 credential_id(ledger.id) · admin 塞号时 ledger.id 还没有 ·
-- 所以塞不进 credplain。这张表用 kiro_rs_credential_id 作主键做**中转** ——
-- decider/settle 手工池 sold 分支拿到 ledger.id 后:读这里 → 写 credplain → 删这行。
--
-- TTL 长一些(7d) · 因为号在 available 状态没上限 · 但也不能永不过期(防明文残留)。
-- 号一直没卖出 · janitor 到期删 → 该号 push_pool 会走 placeholder 兜底(跟当前行为一致)。

CREATE TABLE market_stock_plaintext (
  kiro_rs_credential_id     INTEGER PRIMARY KEY,      -- 号池 credential id · 跟 market_stock_item 关联
  auth_method               TEXT NOT NULL,
  refresh_token_encrypted   BLOB,
  access_token_encrypted    BLOB,
  kiro_api_key_encrypted    BLOB,
  email                     TEXT,
  created_at                TEXT NOT NULL,
  expires_at                TEXT NOT NULL,            -- 默认 7d · janitor 清
  CHECK (auth_method IN ('refresh_token','api_key','bearer'))
);

CREATE INDEX idx_market_stock_plaintext_expires ON market_stock_plaintext(expires_at);

-- +migrate down

DROP INDEX IF EXISTS idx_market_stock_plaintext_expires;
DROP TABLE IF EXISTS market_stock_plaintext;
