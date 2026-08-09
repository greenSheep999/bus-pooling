-- +migrate up

-- 014_pending_topup_creating.sql
--
-- P0 修：CreatePayment 崩溃窗口丢单。
--
-- 场景：handleCreateTopup 顺序：
--   ① CreateOrderWithPending → topup_order + pending_topup(initial) 一起 commit
--   ② paymentGW.CreatePayment  ← 外部 · gateway 侧建单 · 可能已收款
--   ③ AttachGateway            ← 回填 gateway_payment_id + checkout_url
--   ④ EnsureAtLeast(gateway_ordered)
--
-- 崩溃窗口：② 后 ③ 前进程死 · 本地 pending=initial · 无 gateway_payment_id。
-- 原 janitor 走 initialTimeout → 双表 expire · 但 gateway 侧已有单 · webhook 一到
-- 会走 unmatched fallback（按 client_order_id · P0-A 已修）。可是**如果 webhook 也丢**·
-- 我方永远不知道 gateway 侧有单 · 用户付了钱不到账。
--
-- 修：加中间态 `gateway_creating` · **调 CreatePayment 前**就推进。崩后 janitor 看到
-- pending=gateway_creating · 不直接 expire · 走"用 client_order_id (= order.ID) 反查
-- gateway 是否已有单"分支 · 已建 → 回填 · 未建 → 才 expire。

CREATE TABLE pending_topup_new (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  topup_order_id         TEXT NOT NULL,
  status                 TEXT NOT NULL,
  error                  TEXT,
  -- 1b P0 修：poll gateway 失败计数 · 累到上限才 pending_manual · **不 expire**
  poll_fail_count        INTEGER NOT NULL DEFAULT 0,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (topup_order_id) REFERENCES topup_order(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN (
    'initial',
    'gateway_creating',    -- 1b P0 修：CreatePayment 调用中·崩后 janitor 用 client_order_id 反查
    'gateway_ordered',
    'gateway_paid',
    'credited',
    'completed',
    'expired',
    'cancelled',
    'refunded',
    'pending_manual'
  ))
);

INSERT INTO pending_topup_new
  (id, idempotency_record_id, passenger_id, topup_order_id, status,
   error, poll_fail_count, created_at, updated_at)
SELECT id, idempotency_record_id, passenger_id, topup_order_id, status,
       error, 0, created_at, updated_at
FROM pending_topup;

DROP TABLE pending_topup;
ALTER TABLE pending_topup_new RENAME TO pending_topup;

CREATE INDEX idx_pt_scan ON pending_topup(status, updated_at);
CREATE INDEX idx_pt_order ON pending_topup(topup_order_id);
CREATE INDEX idx_pt_passenger ON pending_topup(passenger_id, created_at DESC);

-- +migrate down

CREATE TABLE pending_topup_old (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  topup_order_id         TEXT NOT NULL,
  status                 TEXT NOT NULL,
  error                  TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (topup_order_id) REFERENCES topup_order(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN (
    'initial', 'gateway_ordered', 'gateway_paid', 'credited',
    'completed', 'expired', 'cancelled', 'refunded', 'pending_manual'
  ))
);

INSERT INTO pending_topup_old
  (id, idempotency_record_id, passenger_id, topup_order_id, status,
   error, created_at, updated_at)
SELECT id, idempotency_record_id, passenger_id, topup_order_id,
       -- gateway_creating 回滚成 initial（能被 down 之后的 janitor 兜到）
       CASE WHEN status = 'gateway_creating' THEN 'initial' ELSE status END,
       error, created_at, updated_at
FROM pending_topup;

DROP TABLE pending_topup;
ALTER TABLE pending_topup_old RENAME TO pending_topup;

CREATE INDEX idx_pt_scan ON pending_topup(status, updated_at);
CREATE INDEX idx_pt_order ON pending_topup(topup_order_id);
CREATE INDEX idx_pt_passenger ON pending_topup(passenger_id, created_at DESC);
