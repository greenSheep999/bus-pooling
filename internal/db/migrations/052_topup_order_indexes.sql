-- +migrate up

-- 052 · migration 051 rebuild topup_order 时 · DROP TABLE 把所有索引一并蒸发。
-- 复建 3 条索引 · 让 topup 查询回到 O(log N):
--   idx_topup_passenger_time  · /api/me/topups 分页 · WHERE passenger_id = ? ORDER BY created_at DESC
--   idx_topup_status          · janitor · WHERE status = 'pending' AND expires_at < ?
--   idx_topup_gateway_payment_id · gateway webhook 回调 · WHERE gateway_payment_id = ?
--
-- IF NOT EXISTS 幂等 · 生产可能已经手工建过·down 也 IF EXISTS。

CREATE INDEX IF NOT EXISTS idx_topup_passenger_time
    ON topup_order(passenger_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_topup_status
    ON topup_order(status, expires_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_topup_gateway_payment_id
    ON topup_order(gateway_payment_id)
    WHERE gateway_payment_id IS NOT NULL;

-- +migrate down

DROP INDEX IF EXISTS idx_topup_gateway_payment_id;
DROP INDEX IF EXISTS idx_topup_status;
DROP INDEX IF EXISTS idx_topup_passenger_time;
