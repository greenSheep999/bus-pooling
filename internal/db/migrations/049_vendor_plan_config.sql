-- +migrate up
--
-- 049 · vendor 档位开关配置(P0 · sprint-1e 部署后紧修)
--
-- 起因:前 6 家 vendor 的档位之前写死在 Capability.SelectablePlans 里 · 上游今天开
-- Power 明天开 PRO Max 时代码要跟着改 · 违反"跟随上游变化"原则。
--
-- 分工:
--   vendor_plan_config · 每家 vendor × 每种 kind × 每档 subscription 的**上架开关**
--   已有的 vendor_probe_zone · 存实时**库存 + 价** · 不改
--   已有的 credential_ledger · 存**号本身档位属性**(号观察值) · 不改
--   已有的 market_offer     · 仅 kiro_market vendor 用 · 存实物号货架
--
-- Offers API 组装(docs/24 §3):
--   for each 启用 vendor:
--     for each (kind ∈ AccountKinds) × (plan ∈ 该 kind 下 enabled=1 的档):
--       从 vendor_probe_zone 拿 available/unit_price 快照
--       emit OfferItem{kind, plan, zone, available, unit_price}
--
-- **不写死** —— 每家 vendor × 每种 kind × 4 档默认都 enabled=1 · 后台可关。
-- **不造重复表** —— 库存 / 价格 / 号档位属性 已经有观察表 · 只补开关维度。

CREATE TABLE vendor_plan_config (
  vendor_id      TEXT NOT NULL,
  account_kind   TEXT NOT NULL,     -- enterprise | personal
  subscription   TEXT NOT NULL,     -- power | pro | pro_plus | pro_max
  enabled        INTEGER NOT NULL DEFAULT 1,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  PRIMARY KEY (vendor_id, account_kind, subscription),
  CHECK (account_kind IN ('enterprise', 'personal')),
  CHECK (subscription IN ('power', 'pro', 'pro_plus', 'pro_max')),
  CHECK (enabled IN (0, 1))
);

CREATE INDEX idx_vendor_plan_config_enabled
  ON vendor_plan_config(vendor_id, account_kind, enabled)
  WHERE enabled = 1;

-- Seed · 每家 vendor 建满 4 档 × 2 kind = 8 行(方便后台后续 toggle)
-- **当前实况**·2026-08-16:
--   企业池:只 Power 上架    → enterprise/power=1 · 其他 3 档=0
--   个人池:Pro + Pro+ 上架  → personal/pro=1, personal/pro_plus=1 · 其他 2 档=0
-- 上游哪天开新档·后台把对应行 UPDATE enabled=1 即可 · 不改代码不改 schema
INSERT INTO vendor_plan_config (vendor_id, account_kind, subscription, enabled, created_at, updated_at)
SELECT v.vid, k.kind, p.plan,
       CASE
         WHEN k.kind = 'enterprise' AND p.plan = 'power'    THEN 1
         WHEN k.kind = 'personal'   AND p.plan = 'pro'      THEN 1
         WHEN k.kind = 'personal'   AND p.plan = 'pro_plus' THEN 1
         ELSE 0
       END AS enabled,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  FROM (SELECT 'kiro91' AS vid UNION ALL SELECT 'kiroceo'
        UNION ALL SELECT 'kirooo' UNION ALL SELECT 'kiroappio'
        UNION ALL SELECT 'kiroappcc' UNION ALL SELECT 'kirodrop'
        UNION ALL SELECT 'kiro_market') AS v
  CROSS JOIN (SELECT 'enterprise' AS kind UNION ALL SELECT 'personal') AS k
  CROSS JOIN (SELECT 'power' AS plan UNION ALL SELECT 'pro'
              UNION ALL SELECT 'pro_plus' UNION ALL SELECT 'pro_max') AS p;

-- +migrate down
DROP INDEX IF EXISTS idx_vendor_plan_config_enabled;
DROP TABLE IF EXISTS vendor_plan_config;
