-- +migrate up

-- 017_pull_cost_split.sql
--
-- 1c · 多人拼车按成员分摊扣款（decisions §8.18 / §8.23 / §8.26）。
--
-- 之前 settle.go 把整轮的钱全扣发起人（注释写"1a 只做 single bus，全归本人"），
-- 多人车落地后这条不成立了：车里 2 人各 50%，拉号的人付 100%，另一个白蹭。
--
-- reserve_split_json 记**这轮谁冻了多少**（microunit）：
--   {"<passenger_id>": 12000000, "<passenger_id2>": 12000000}
--
-- 为什么要落库：冻结发生在调 vendor **之前**，settle / 崩溃恢复都要知道
-- "该退给谁多少"。只存 reserved_amount 总额的话，janitor 恢复时没法按人释放。
--
-- 兼容：老行为 NULL · 恢复路径读到 NULL 时退回"全退发起人"的老语义
-- （那些行本来就是单人车 · 语义等价）。

ALTER TABLE pending_purchase ADD COLUMN reserve_split_json TEXT;

-- +migrate down

ALTER TABLE pending_purchase DROP COLUMN reserve_split_json;
