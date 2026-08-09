-- +migrate up

-- 012_assign_credential_lock.sql
--
-- P0-1 修：assign 并发跨系统分叉。
--
-- 场景：两个**不同** idempotency key 的 assign 请求·**同一** credential_id·
--       不同 target_bus_id · 并发到达：
--
--   tx1(A)  BEGIN → 落 pending_assignment A(cid, bus-X) initial → COMMIT
--   tx1(B)  BEGIN → 落 pending_assignment B(cid, bus-Y) initial → COMMIT
--   pool(A) UpdateCredential(cid, groups=[bus-X])
--   pool(B) UpdateCredential(cid, groups=[bus-Y])                  ← 后到覆盖 A
--   tx2(A)  AssignToBusTx 成功·台账 owner_bus_id=bus-X · pending A completed
--   tx2(B)  AssignToBusTx WHERE owner_bus_id IS NULL 命中 0 行 · 409
--
-- 结果：credential_ledger.owner_bus_id = bus-X · 但 housepool 实际在 bus-Y。**分叉**。
--
-- 修：**同一 credential_id 只能有一个 initial 状态的 pending_assignment**。
-- 第二个并发请求 INSERT 就被约束挡住 · 早失败 · 不落 initial · 不走 pool。
--
-- SQLite partial UNIQUE index：只对 status='initial' 生效·completed / need_manual 不占索引。

CREATE UNIQUE INDEX idx_pa_credential_initial_unique
  ON pending_assignment(credential_id)
 WHERE status = 'initial';

-- +migrate down

DROP INDEX IF EXISTS idx_pa_credential_initial_unique;
