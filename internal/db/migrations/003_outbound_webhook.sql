-- 阶段 1e · webhook 出向投递台账
-- 权威定义 docs/06-db-schema §14
--
-- 阶段 1a 装配 webhook 测试端点时先建这张表 —— 让 GET /me/downstream/webhook/deliveries
-- 有真实数据可返（在没这张表的时候只能返空数组 · 不容易发现回归）

-- +migrate up

CREATE TABLE outbound_webhook_delivery (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  event_id               TEXT NOT NULL,                  -- 我方生成 · 一次事件多次重试共用一个
  event_type             TEXT NOT NULL,
  target_url             TEXT NOT NULL,
  payload                TEXT NOT NULL,
  attempt                INTEGER NOT NULL DEFAULT 1,
  status                 TEXT NOT NULL,                  -- pending | delivered | failed | dropped
  response_status        INTEGER,                        -- HTTP status · 未收到响应时 NULL
  response_body_snippet  TEXT,                            -- 前 512 字节 · 用来排查
  latency_ms             INTEGER,                        -- 从发到收响应的耗时 · 未收到时 NULL
  next_retry_at          TEXT,
  delivered_at           TEXT,
  created_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (status IN ('pending','delivered','failed','dropped'))
);
CREATE INDEX idx_owd_passenger ON outbound_webhook_delivery(passenger_id, created_at DESC);
CREATE INDEX idx_owd_scan ON outbound_webhook_delivery(status, next_retry_at);

-- +migrate down

DROP TABLE IF EXISTS outbound_webhook_delivery;
