-- +migrate up
--
-- 043_credential_plaintext.sql · 手上号的明文加密缓存 · decisions §12.5 扩展
--
-- 上游 kiro.rs 后端**未提供 reveal 端点** · bus-pooling 拉号成功那一刻拿到明文后
-- 必须自己存一份(加密) · 否则 push_pool / handoff 都没明文可导出。
--
-- 独立表 · 不落 credential_ledger(那个表明文永不落 · CLAUDE §12.5 铁律)
-- TTL 24h + used_at 后 24h 双条件 · janitor 定时清

CREATE TABLE credential_plaintext (
  credential_id             TEXT PRIMARY KEY,
  auth_method               TEXT NOT NULL,           -- refresh_token | api_key | bearer
  refresh_token_encrypted   BLOB,                     -- AES-GCM · null = 该方法不用此字段
  access_token_encrypted    BLOB,
  kiro_api_key_encrypted    BLOB,
  email                     TEXT,                     -- 明文 · 不敏感(BatchImport 里就带)
  created_at                TEXT NOT NULL,
  expires_at                TEXT NOT NULL,            -- 24h TTL · janitor 到期硬删
  used_at                   TEXT,                     -- handoff/push 成功后立标 · 24h 后 janitor 删
  FOREIGN KEY (credential_id) REFERENCES credential_ledger(id) ON DELETE CASCADE,
  CHECK (auth_method IN ('refresh_token','api_key','bearer'))
);

CREATE INDEX idx_credential_plaintext_expires ON credential_plaintext(expires_at);
CREATE INDEX idx_credential_plaintext_used ON credential_plaintext(used_at) WHERE used_at IS NOT NULL;

-- +migrate down

-- SQLite 3.35+ 支持 DROP · 保守回滚不删(空表不影响读写)
