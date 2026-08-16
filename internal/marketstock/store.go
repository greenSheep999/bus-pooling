// Package marketstock · 我方手工上架货架的库存 store（docs/24 §3 · migration 047）。
//
// 前 6 家：Stock() 打 vendor API · Purchase() 打 vendor API · 号从上游落地
// 第 7 家（本包）：Stock() 数 market_stock_item.available · Purchase() 从预导入号里
//
//	挑一个 · reserve → sell · 号已经在 housepool 里 · 只是转 owner
//
// 关键不变式（防超卖 · 审计 P0-4）：
//
//	同一个 stock_item 一次只被一个 pending 占用 —— 靠条件 UPDATE 实现（不是 SELECT
//	FOR UPDATE · SQLite 不支持行锁 · 用 UPDATE ... WHERE status='available' 竞争）。
//	抢到的那条 tx 拿到 id · 抢不到的 tx affected=0 直接返 ErrNoStock。
package marketstock

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ReserveTTL · reserved 状态最长持有时间 · sweeper 扫过期释放回 available。
//
// 5 分钟对齐 pending_purchase 里 purchasing 的兜底时间（09-transactions §2）·
// 上游没这么久 · 但 decider 可能崩在 purchasing 前 · 留足恢复窗口。
const ReserveTTL = 5 * time.Minute

var (
	ErrNoStock      = errors.New("marketstock: 无可用库存")
	ErrOfferMissing = errors.New("marketstock: 找不到货架")
)

// Offer · 货架定义（market_offer 表一行）· 由后台配置
type Offer struct {
	ID           string
	VendorID     string
	AccountKind  providers.AccountKind
	Subscription providers.SubscriptionPlan
	// PriceBands 数量分档 · 按 lower 升序 · 最高档 Upper=0 = 及以上
	PriceBands []providers.QtyPriceBand
	Enabled    bool
	Source     string // 落到 credential_ledger.source
}

// UnitPriceFor · 按购买数量落到哪一档 · 返 microunit / 个。
// 分档必须连续无空洞（Store.upsertOffer 落库时校验）· 找不到给最后一档兜底。
func (o Offer) UnitPriceFor(count int) int64 {
	if count < 1 || len(o.PriceBands) == 0 {
		return 0
	}
	for _, b := range o.PriceBands {
		if count >= b.Lower && (b.Upper == 0 || count <= b.Upper) {
			return b.UnitPriceCredits
		}
	}
	// 兜底：超过所有档 → 用最高档
	return o.PriceBands[len(o.PriceBands)-1].UnitPriceCredits
}

// ReservedItem · Reserve 返回的一条占用记录 · Sell/Release 都用它
type ReservedItem struct {
	StockItemID        string
	KiroRSCredentialID uint64
	OfferID            string
	Source             string
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// AvailableCount · 某货架当前可卖数（前端展示 tab 数字 / vendor Stock 快照用）
func (s *Store) AvailableCount(ctx context.Context, offerID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM market_stock_item
		 WHERE offer_id = ? AND status = 'available'`, offerID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("marketstock: count available: %w", err)
	}
	return n, nil
}

// AvailableCountByKind · 某 vendor × kind 的总可卖数（跨订阅档聚合 · Stock 快照用）
func (s *Store) AvailableCountByKind(
	ctx context.Context, vendorID string, kind providers.AccountKind,
) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM market_stock_item i
		  JOIN market_offer o ON o.id = i.offer_id
		 WHERE o.vendor_id = ? AND o.account_kind = ? AND o.enabled = 1
		   AND i.status = 'available'`,
		vendorID, string(kind.Normalize())).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("marketstock: count by kind: %w", err)
	}
	return n, nil
}

// FindOffer · 找符合条件的**已启用**货架 · plan 空 = 匹配任意档。
// 用于 Purchase 前定位货架（决定单价 + 后续 Reserve 从哪个 offer 挑号）。
func (s *Store) FindOffer(
	ctx context.Context, vendorID string,
	kind providers.AccountKind, plan providers.SubscriptionPlan,
) (*Offer, error) {
	q := `SELECT id, vendor_id, account_kind, subscription, price_bands_json,
	             enabled, COALESCE(source, '')
	        FROM market_offer
	       WHERE vendor_id = ? AND account_kind = ? AND enabled = 1`
	args := []any{vendorID, string(kind.Normalize())}
	if plan != "" {
		q += ` AND subscription = ?`
		args = append(args, string(plan))
	}
	q += ` LIMIT 1`

	var (
		o        Offer
		kindStr  string
		planStr  string
		bandsStr string
		enabled  int
	)
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&o.ID, &o.VendorID, &kindStr, &planStr, &bandsStr, &enabled, &o.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOfferMissing
	}
	if err != nil {
		return nil, fmt.Errorf("marketstock: find offer: %w", err)
	}
	o.AccountKind = providers.AccountKind(kindStr)
	o.Subscription = providers.SubscriptionPlan(planStr)
	o.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(bandsStr), &o.PriceBands); err != nil {
		return nil, fmt.Errorf("marketstock: 解析 price_bands: %w", err)
	}
	return &o, nil
}

// Reserve · 从货架 available 里抢 n 个进 reserved · 原子（防超卖）。
//
// 抢不到 n 个（并发或超卖）返 ErrNoStock · 已抢到的会**自动释放**（tx rollback）。
// pendingID 是占用凭据 · 崩溃恢复 sweeper 靠它决定是释放还是继续 sell。
func (s *Store) Reserve(
	ctx context.Context, offerID, pendingID string, n int,
) ([]ReservedItem, error) {
	if n <= 0 {
		return nil, fmt.Errorf("marketstock: reserve n=%d 必须>0", n)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	out := make([]ReservedItem, 0, n)

	for i := 0; i < n; i++ {
		// 挑一条 available（按 created_at FIFO · 老号先卖 · 减少 quota 损耗）
		var (
			id     string
			kiroID uint64
			source sql.NullString
		)
		err := tx.QueryRowContext(ctx, `
			SELECT i.id, i.kiro_rs_credential_id, o.source
			  FROM market_stock_item i
			  JOIN market_offer o ON o.id = i.offer_id
			 WHERE i.offer_id = ? AND i.status = 'available'
			 ORDER BY i.created_at
			 LIMIT 1`, offerID).Scan(&id, &kiroID, &source)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoStock
		}
		if err != nil {
			return nil, fmt.Errorf("marketstock: pick available: %w", err)
		}

		// 条件 UPDATE 抢占 · WHERE status='available' 是并发保护
		// 抢不到（affected=0）= 别的 tx 刚拿走 · 重试或返 ErrNoStock
		res, err := tx.ExecContext(ctx, `
			UPDATE market_stock_item
			   SET status = 'reserved',
			       reserved_by_pending = ?,
			       reserved_at = ?,
			       updated_at = ?
			 WHERE id = ? AND status = 'available'`,
			pendingID, now, now, id)
		if err != nil {
			return nil, fmt.Errorf("marketstock: reserve claim: %w", err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			// 被别人抢先 · 重试挑下一个
			i--
			continue
		}
		out = append(out, ReservedItem{
			StockItemID:        id,
			KiroRSCredentialID: kiroID,
			OfferID:            offerID,
			Source:             source.String,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// Sell · reserved → sold · 绑定 credential_ledger.id · **必须在 settle 同一个 tx 里做**。
//
// 传 tx 是为了跟 credential_ledger 的 INSERT 原子提交 · 避免"号已经落 ledger
// 但 stock_item 还是 reserved" → 崩溃后 sweeper 会误释放。
func (s *Store) SellTx(
	ctx context.Context, tx *sql.Tx,
	stockItemID, ledgerID string,
) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
		UPDATE market_stock_item
		   SET status = 'sold',
		       sold_ledger_id = ?,
		       sold_at = ?,
		       reserved_by_pending = NULL,
		       reserved_at = NULL,
		       updated_at = ?
		 WHERE id = ? AND status = 'reserved'`,
		ledgerID, now, now, stockItemID)
	if err != nil {
		return fmt.Errorf("marketstock: sell: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("marketstock: sell 失败(不是 reserved 或 id 错): %s", stockItemID)
	}
	return nil
}

// Release · reserved → available · 崩溃恢复 / cancel 用。幂等（已 available 也返 nil）。
func (s *Store) Release(ctx context.Context, stockItemID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE market_stock_item
		   SET status = 'available',
		       reserved_by_pending = NULL,
		       reserved_at = NULL,
		       updated_at = ?
		 WHERE id = ? AND status = 'reserved'`, now, stockItemID)
	if err != nil {
		return fmt.Errorf("marketstock: release: %w", err)
	}
	return nil
}

// ReleaseByPending · 按 pending_id 批量释放（崩溃恢复取消一整个 pending 时用）
func (s *Store) ReleaseByPending(ctx context.Context, pendingID string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		UPDATE market_stock_item
		   SET status = 'available',
		       reserved_by_pending = NULL,
		       reserved_at = NULL,
		       updated_at = ?
		 WHERE reserved_by_pending = ? AND status = 'reserved'`, now, pendingID)
	if err != nil {
		return 0, fmt.Errorf("marketstock: release by pending: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// FindReservedByPending · 反查一个 pending（或 client_order_id）当前占的 reserved 号。
// 崩溃恢复 · OrderKeys 补拉 · 都靠这个 —— 上层拿到 client_order_id · 靠它找回原来那批号。
func (s *Store) FindReservedByPending(ctx context.Context, pendingID string) ([]ReservedItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.kiro_rs_credential_id, i.offer_id, COALESCE(o.source, '')
		  FROM market_stock_item i
		  JOIN market_offer o ON o.id = i.offer_id
		 WHERE i.reserved_by_pending = ? AND i.status = 'reserved'
		 ORDER BY i.created_at`, pendingID)
	if err != nil {
		return nil, fmt.Errorf("marketstock: find by pending: %w", err)
	}
	defer rows.Close()

	var out []ReservedItem
	for rows.Next() {
		var it ReservedItem
		if err := rows.Scan(&it.StockItemID, &it.KiroRSCredentialID, &it.OfferID, &it.Source); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SweepExpired · 扫超时 reserved（> ReserveTTL）· 释放回 available。
// janitor 定时调 · 防"decider 崩在 purchasing 前 · reserved 永久占位"。
func (s *Store) SweepExpired(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-ReserveTTL).Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		UPDATE market_stock_item
		   SET status = 'available',
		       reserved_by_pending = NULL,
		       reserved_at = NULL,
		       updated_at = ?
		 WHERE status = 'reserved' AND reserved_at < ?`, now, cutoff)
	if err != nil {
		return 0, fmt.Errorf("marketstock: sweep: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AddItem · 后台导入一把号 · 走 admin 导入 API 调用（不给运行时业务链用）
type AddItemInput struct {
	OfferID            string
	KiroRSCredentialID uint64
	ImportedBy         string
}

func (s *Store) AddItem(ctx context.Context, in AddItemInput) (string, error) {
	if in.OfferID == "" || in.KiroRSCredentialID == 0 {
		return "", fmt.Errorf("marketstock: AddItem 缺参数")
	}
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO market_stock_item
		  (id, offer_id, kiro_rs_credential_id, status, imported_by, created_at, updated_at)
		VALUES (?, ?, ?, 'available', ?, ?, ?)`,
		id, in.OfferID, in.KiroRSCredentialID, in.ImportedBy, now, now)
	if err != nil {
		return "", fmt.Errorf("marketstock: add item: %w", err)
	}
	return id, nil
}

// FindOfferByID · 按 offer id 精确查（含 disabled）· admin 校验用
func (s *Store) FindOfferByID(ctx context.Context, offerID string) (*Offer, error) {
	var (
		o        Offer
		kindStr  string
		planStr  string
		bandsStr string
		enabled  int
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, vendor_id, account_kind, subscription, price_bands_json,
		       enabled, COALESCE(source, '')
		  FROM market_offer WHERE id = ?`, offerID).Scan(
		&o.ID, &o.VendorID, &kindStr, &planStr, &bandsStr, &enabled, &o.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOfferMissing
	}
	if err != nil {
		return nil, fmt.Errorf("marketstock: find offer by id: %w", err)
	}
	o.AccountKind = providers.AccountKind(kindStr)
	o.Subscription = providers.SubscriptionPlan(planStr)
	o.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(bandsStr), &o.PriceBands); err != nil {
		return nil, fmt.Errorf("marketstock: 解析 price_bands: %w", err)
	}
	return &o, nil
}

// ListOffers · 列所有货架（含 disabled · admin 视图用 · 业务链走 FindOffer）
func (s *Store) ListOffers(ctx context.Context) ([]Offer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, vendor_id, account_kind, subscription, price_bands_json,
		       enabled, COALESCE(source, '')
		  FROM market_offer
		 ORDER BY vendor_id, account_kind, subscription`)
	if err != nil {
		return nil, fmt.Errorf("marketstock: list offers: %w", err)
	}
	defer rows.Close()

	var out []Offer
	for rows.Next() {
		var (
			o        Offer
			kindStr  string
			planStr  string
			bandsStr string
			enabled  int
		)
		if err := rows.Scan(&o.ID, &o.VendorID, &kindStr, &planStr,
			&bandsStr, &enabled, &o.Source); err != nil {
			return nil, err
		}
		o.AccountKind = providers.AccountKind(kindStr)
		o.Subscription = providers.SubscriptionPlan(planStr)
		o.Enabled = enabled == 1
		if err := json.Unmarshal([]byte(bandsStr), &o.PriceBands); err != nil {
			return nil, fmt.Errorf("marketstock: 解析 price_bands: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpsertOffer · 后台配置货架（价格 / 开关 / 分档）· 幂等
type UpsertOfferInput struct {
	VendorID     string
	AccountKind  providers.AccountKind
	Subscription providers.SubscriptionPlan
	PriceBands   []providers.QtyPriceBand
	Enabled      bool
	Source       string
}

func (s *Store) UpsertOffer(ctx context.Context, in UpsertOfferInput) (string, error) {
	if err := validateBands(in.PriceBands); err != nil {
		return "", err
	}
	bandsJSON, err := json.Marshal(in.PriceBands)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	enabled := 0
	if in.Enabled {
		enabled = 1
	}

	// 已存在 → 更新（保留原 id）· 不存在 → 插入新 id
	var existingID string
	err = s.db.QueryRowContext(ctx, `
		SELECT id FROM market_offer
		 WHERE vendor_id = ? AND account_kind = ? AND subscription = ?`,
		in.VendorID, string(in.AccountKind.Normalize()), string(in.Subscription),
	).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		id := uuid.NewString()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO market_offer
			  (id, vendor_id, account_kind, subscription, price_bands_json,
			   enabled, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.VendorID, string(in.AccountKind.Normalize()), string(in.Subscription),
			string(bandsJSON), enabled, nullIfEmpty(in.Source), now, now)
		if err != nil {
			return "", fmt.Errorf("marketstock: insert offer: %w", err)
		}
		return id, nil
	case err != nil:
		return "", fmt.Errorf("marketstock: lookup offer: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE market_offer
		   SET price_bands_json = ?, enabled = ?, source = ?, updated_at = ?
		 WHERE id = ?`,
		string(bandsJSON), enabled, nullIfEmpty(in.Source), now, existingID)
	if err != nil {
		return "", fmt.Errorf("marketstock: update offer: %w", err)
	}
	return existingID, nil
}

// validateBands · 分档必须连续无空洞 · 最高档 Upper=0（及以上）
func validateBands(bands []providers.QtyPriceBand) error {
	if len(bands) == 0 {
		return fmt.Errorf("marketstock: 至少一档价")
	}
	prev := 0
	for i, b := range bands {
		if b.Lower <= prev {
			return fmt.Errorf("marketstock: 档 %d Lower=%d 未升序（上一档到 %d）", i, b.Lower, prev)
		}
		if b.UnitPriceCredits <= 0 {
			return fmt.Errorf("marketstock: 档 %d 单价必须>0", i)
		}
		if b.Upper != 0 && b.Upper < b.Lower {
			return fmt.Errorf("marketstock: 档 %d Upper=%d < Lower=%d", i, b.Upper, b.Lower)
		}
		if i < len(bands)-1 && b.Upper == 0 {
			return fmt.Errorf("marketstock: Upper=0(及以上)只能是最后一档")
		}
		if b.Upper > 0 {
			prev = b.Upper
		} else {
			prev = 1 << 30
		}
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
