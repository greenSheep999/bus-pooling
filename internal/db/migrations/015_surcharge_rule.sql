-- +migrate up

-- 015_surcharge_rule.sql
--
-- 1b P1-2B · surcharge 规则表（decisions §8.30 B）。
--
-- 把 vendor / zone / retail / capability / adhoc 五类分项统一到一张表·
-- 引擎按 `active=1 AND applies_when 命中 AND waived_when 未命中` 遍历 · 按 priority
-- 累加 rate_bp。加新计费项 = INSERT 一行·**不改代码**（CLAUDE.md §7.3 铁律：
-- 具体费率不进代码 · 只进表 / 后台 / 文档）。

CREATE TABLE surcharge_rule (
  id                  TEXT PRIMARY KEY,
  kind                TEXT NOT NULL,          -- vendor | zone | retail | capability | adhoc | service | single_pull
  name                TEXT NOT NULL UNIQUE,   -- 例：'retail_markup' / 'zone_eu' / 'service_fee'
  rate_bp             INTEGER NOT NULL,       -- basis point · 2000 = 20%
  base                TEXT NOT NULL DEFAULT 'key_cost',  -- 一律加在号价上（跟 00 §3「计费项加在号价上」一致）
  active              INTEGER NOT NULL DEFAULT 0,        -- 默认关·避免误开
  -- 什么条件开启（JSON 谓词 · 1b 支持相等和数值比较）·空 = 无条件
  applies_when_json   TEXT,
  -- 什么条件减免（同 applies_when 谓词语法）· 优先级高于 applies_when
  waived_when_json    TEXT,
  -- capability 类可能让乘客主动勾选（1c+ 才用 · 现在忽略）
  user_selectable     INTEGER NOT NULL DEFAULT 0,
  priority            INTEGER NOT NULL DEFAULT 100,   -- 多条命中时的应用顺序
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  CHECK (kind IN ('vendor', 'zone', 'retail', 'capability', 'adhoc', 'service', 'single_pull')),
  CHECK (rate_bp >= 0),
  CHECK (base IN ('key_cost', 'subtotal'))
);

CREATE INDEX idx_sr_active ON surcharge_rule(active, priority);
CREATE INDEX idx_sr_kind ON surcharge_rule(kind, active);

-- pull_round_surcharge · 记每次拉号真命中了哪些规则 · 收了多少（对账 / 申诉用）
-- 1b 加骨架 · 1c/1d 实际写入
CREATE TABLE pull_round_surcharge (
  pull_round_id   TEXT NOT NULL,
  rule_id         TEXT NOT NULL,
  rule_name       TEXT NOT NULL,          -- 快照·rule 后来改名不影响历史
  rule_kind       TEXT NOT NULL,
  rate_bp         INTEGER NOT NULL,       -- 快照
  amount          INTEGER NOT NULL,       -- microunit · 实际收了多少（batch 层·非单号）
  created_at      TEXT NOT NULL,
  PRIMARY KEY (pull_round_id, rule_id),
  FOREIGN KEY (pull_round_id) REFERENCES pull_round(id),
  FOREIGN KEY (rule_id) REFERENCES surcharge_rule(id)
);

CREATE INDEX idx_prs_round ON pull_round_surcharge(pull_round_id);

-- 不 seed 具体规则 · 装配层看 env 决定要不要 upsert 兼容规则（1a 兼容路径）。

-- +migrate down

DROP INDEX IF EXISTS idx_prs_round;
DROP TABLE IF EXISTS pull_round_surcharge;
DROP INDEX IF EXISTS idx_sr_kind;
DROP INDEX IF EXISTS idx_sr_active;
DROP TABLE IF EXISTS surcharge_rule;
