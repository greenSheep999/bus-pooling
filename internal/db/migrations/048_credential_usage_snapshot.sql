-- +migrate up
--
-- 048 · 号用量快照 · 我方 housepool 侧数据（粗 · 展示用）
--
-- 数据源: housepool 后端 ListCredentials[].balance · 每 5min 一采（跟 deathwatch 复用一次调用）
-- 字段: 只有 balance 端点真正提供的 currentUsage / usageLimit / subscriptionTitle
--       请求日志 / RPM / TPM / concurrency 是**下游用户 pool** 才有 · 走独立表 §12.5b
--
-- 用途:
--   - 前端"号详情"进度条（currentUsage / usageLimit）
--   - deathwatch.markDead 读最近一条快照 · 拷贝到 credential_ledger.credits_used
--   - **不用于**分摊 · 不用于并发控制（数据太粗）

CREATE TABLE credential_usage_snapshot (
  id                    TEXT PRIMARY KEY,
  kiro_rs_credential_id INTEGER NOT NULL,
  current_usage_micro   INTEGER NOT NULL,   -- Balance.currentUsage × 1e6 (保浮点精度)
  usage_limit_micro     INTEGER NOT NULL,   -- Balance.usageLimit × 1e6 (月度上限)
  subscription_title    TEXT,               -- "KIRO PRO+" 原样 · 归一版本存 credential_ledger.subscription
  next_reset_at         TEXT,               -- Balance.nextResetAt · 下次配额重置
  observed_at           TEXT NOT NULL,
  UNIQUE (kiro_rs_credential_id, observed_at)
);

-- 前端号详情 24h 柱图 · 反查一号最近 N 条快照
CREATE INDEX idx_cred_usage_snap_by_cred
  ON credential_usage_snapshot(kiro_rs_credential_id, observed_at DESC);

-- +migrate down
DROP INDEX IF EXISTS idx_cred_usage_snap_by_cred;
DROP TABLE IF EXISTS credential_usage_snapshot;
