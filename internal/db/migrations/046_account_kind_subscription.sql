-- +migrate up
--
-- 046 · Offer 维度落库 · account_kind + subscription + source
--
-- 背景（docs/24-category-subscription.md）：
--   Offer = vendor × account_kind × subscription × zone · 四者**平级**
--   上游实测（2026-08-16）：有的 vendor 企业池/个人池是两套端点、两套价、两套库存
--
-- 为什么 pending_purchase 也要存：
--   崩溃恢复重放 Purchase 时必须用**同一个 account_kind** —— 两个池价和库存都不同，
--   用错池会去错的端点下单（docs/09-transactions §2 重放语义）。
--
-- subscription 的值从哪来：
--   多数 vendor **买前不给档位**（实测某家个人池 ?plan= 参数被忽略）·
--   所以 credential_ledger.subscription 来自号池 BatchImport 事件回报，
--   而 pending_purchase.plan 只是"请求意图"。两者可能不同，各存各的。
--
-- CHECK 约束按 providers.AllSubscriptionPlans · **上游加档位时要同步改这里**。

ALTER TABLE credential_ledger ADD COLUMN account_kind TEXT;
ALTER TABLE credential_ledger ADD COLUMN subscription TEXT;
ALTER TABLE credential_ledger ADD COLUMN source       TEXT;

ALTER TABLE pending_purchase ADD COLUMN account_kind TEXT;
ALTER TABLE pending_purchase ADD COLUMN plan         TEXT;
ALTER TABLE pending_purchase ADD COLUMN source       TEXT;

-- 索引：按类型/档位筛号（提取页 tab 过滤 · quota 统计 · 对账）
CREATE INDEX idx_cred_ledger_kind ON credential_ledger(account_kind, status);
CREATE INDEX idx_cred_ledger_plan ON credential_ledger(subscription);

-- ⚠️ SQLite 的 ALTER TABLE ADD COLUMN **不支持加 CHECK** · 值域校验在写入层做：
--   account_kind → providers.AccountKind
--   subscription → providers.SubscriptionPlan.Valid()（认不出存 NULL · 宁缺勿错）
-- 认不出的档位串**不落库** —— 存错档位会让 quota 判断错（PRO 1000 当成 Power 10000）。

-- +migrate down
DROP INDEX IF EXISTS idx_cred_ledger_kind;
DROP INDEX IF EXISTS idx_cred_ledger_plan;
-- SQLite 3.35+ 支持 DROP COLUMN · 但生产库上删列有重建表风险 ·
-- 这几列全 NULL 时不影响任何读写 · down 保留列（跟 044 同策略）。
