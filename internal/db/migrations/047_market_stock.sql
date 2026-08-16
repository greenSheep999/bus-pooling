-- +migrate up
--
-- 047 · 我方第 7 家 vendor "Kiro Vendor Market - various sources" · 手工上架库存
--
-- 跟前 6 家的唯一差别：**库存不是 API 查来的 · 号不是 API 买来的** ——
-- 运营买好后走后台 admin 导入 · 之后完全同构（进 housepool / 落 credential_ledger /
-- deathwatch 监控 / assign 派发 / 推 passengerpool / handoff）。
--
-- 表分工（docs/24 §3）：
--   market_offer      · 货架**定义**（vendor × kind × plan → 价格 · 售卖开关）
--                       行数固定（配置表 · 上架多少档就多少行 · 目前 2 行）
--   market_stock_item · 每一把预上架**号**（1:1 对应 housepool credential）
--                       状态 available / reserved / sold  →  卖出后归入 credential_ledger
--
-- 为什么不复用 credential_ledger 存"未卖出"的号（decisions §11.15 抢号缓冲的老提案）：
--   现有 CHECK 强制 owner_bus_id / owner_record_passenger_id **必选其一**。改这个
--   CHECK 要重建表（3 张 FK 依赖）· 风险大。独立表更干净：卖出时直接从 market_stock_item
--   INSERT 到 credential_ledger（同一 kiro_rs_credential_id）· 生命周期分明。
--
-- 卖号原子性（防超卖 · 审计 P0-4）：
--   Reserve → Sell 两步 · Reserve 用条件 UPDATE 竞争 · 只有一个 tx 能把 available 抢到 reserved。
--   崩溃恢复：reserved 超时（5min）没落 sold 自动 release 回 available。

CREATE TABLE market_offer (
  id                  TEXT PRIMARY KEY,
  vendor_id           TEXT NOT NULL,       -- 'kiro_market' 或未来的第 8 家
  account_kind        TEXT NOT NULL,       -- enterprise | personal
  subscription        TEXT NOT NULL,       -- power | pro | pro_plus | pro_max
  -- 数量分档 · JSON: [{"lower":1,"upper":9,"unit_price":50},{"lower":10,"upper":0,"unit_price":40}]
  -- Upper=0 = 及以上（跟 providers.QtyPriceBand 一致 · docs/vendors/kiro-ooo.md §2.3b）
  price_bands_json    TEXT NOT NULL,
  enabled             INTEGER NOT NULL DEFAULT 1,
  source              TEXT,                -- 运营渠道标识（示例值 "SuperMan" · 落到 credential_ledger.source）
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (vendor_id, account_kind, subscription),
  CHECK (account_kind IN ('enterprise', 'personal')),
  CHECK (subscription IN ('power', 'pro', 'pro_plus', 'pro_max')),
  CHECK (enabled IN (0, 1))
);

CREATE INDEX idx_market_offer_enabled ON market_offer(enabled, vendor_id);

CREATE TABLE market_stock_item (
  id                    TEXT PRIMARY KEY,
  offer_id              TEXT NOT NULL,
  -- 号池 credential id · 运营导入时已经进 housepool（prebuy-pool group）
  kiro_rs_credential_id INTEGER NOT NULL UNIQUE,
  status                TEXT NOT NULL,     -- available | reserved | sold
  -- reserved 时的临时占用信息 · sold 后清空
  reserved_by_pending   TEXT,              -- pending_purchase.id · 释放/回滚要
  reserved_at           TEXT,              -- 5min 未 sold → sweeper 释放回 available
  -- sold 时的对账信息（落地 credential_ledger 后·反向索引）
  sold_ledger_id        TEXT,              -- credential_ledger.id
  sold_at               TEXT,
  imported_by           TEXT NOT NULL,     -- 运营导入人（后台账号）
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (offer_id) REFERENCES market_offer(id),
  CHECK (status IN ('available', 'reserved', 'sold')),
  CHECK (
    (status = 'available' AND reserved_by_pending IS NULL AND sold_ledger_id IS NULL) OR
    (status = 'reserved'  AND reserved_by_pending IS NOT NULL AND sold_ledger_id IS NULL) OR
    (status = 'sold'      AND sold_ledger_id IS NOT NULL)
  )
);

-- 竞争 available 号的核心查询 · 按 offer 找可用号
CREATE INDEX idx_market_stock_available ON market_stock_item(offer_id, status)
  WHERE status = 'available';
-- 崩溃恢复 sweeper 扫超时 reserved
CREATE INDEX idx_market_stock_reserved ON market_stock_item(status, reserved_at)
  WHERE status = 'reserved';

-- +migrate down
DROP INDEX IF EXISTS idx_market_stock_reserved;
DROP INDEX IF EXISTS idx_market_stock_available;
DROP TABLE IF EXISTS market_stock_item;
DROP INDEX IF EXISTS idx_market_offer_enabled;
DROP TABLE IF EXISTS market_offer;
