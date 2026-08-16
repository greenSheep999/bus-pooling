package api

// admin_overview · v1-E · 运维单页 · 让上线后能看到 6 家 vendor 有没有钱 / fleet 在不在跑
//
// **仅运维**：跟 /api/admin/data-health 一样 · BP_ADMIN_KEY 头校验 · 绝不给前端。
// 这里可以直接暴露 vendor_id / vendor_ledger.balance_after / 我方在 vendor 侧的余额。
//
// **数据全从库读**（不实时打 vendor 端点 · 那些走探针 / balance poller / ledger backfiller）：
//   · 余额 → vendor_ledger 最新一条的 balance_after（vendor 侧账本）· 无就 vendor_probe 最新一条的 raw 里推
//   · fleet · dispatch · restock · 全用现有 vendor_dispatch / vendor_probe 表
//
// **为什么不引 vendorbalance.Cache 那个内存缓存**：那个是给 decider 拉号前预检用 · 数据 fresh
// 度只 5min · 而且是"最后一次 poll 成功值"（可能是几分钟前）· admin 页面看的是**趋势和最新
// 落库值** · 直接查表更实在。

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// vendorCurrency · 每家 vendor 的账户币种（vendor 侧账本 · 展示用）
// 大部分家用内部积分（credit · 我方 1:1 到 CNY）· 只有一家用 CNY（那家 USD 计价但账户 CNY）
func vendorCurrency(vendorID string) string {
	// 用 providers 常量避免文本匹配 vendor 真名（lint 白名单友好）
	if vendorID == string(providers.VendorKiroDrop) {
		return providers.CurrencyCNY
	}
	return providers.CurrencyCredit
}

// AdminOverviewRow · 单家 vendor 的运维视图
type AdminOverviewRow struct {
	VendorID       string `json:"vendor_id"` // 运维要看 · 真名不脱敏
	Alive          bool   `json:"alive"`     // 探针最后一次 vendor.Stock 是否成功
	LastProbeAt    string `json:"last_probe_at,omitempty"`
	LastProbeAgo   string `json:"last_probe_ago,omitempty"`
	ProbeErrorKind string `json:"probe_error_kind,omitempty"`

	// 我方在 vendor 侧的余额（vendor_ledger 最新一条 balance_after · 无则 nil）
	Balance          *AdminMoney `json:"balance,omitempty"`
	BalanceCheckedAt string      `json:"balance_checked_at,omitempty"`

	// fleet 状态（vendor_probe.ps_*）
	FleetGenerating *bool `json:"fleet_generating,omitempty"`
	FleetKeysActive *int  `json:"fleet_keys_active,omitempty"`
	FleetKeysStock  *int  `json:"fleet_keys_stock,omitempty"`
	FleetKeysDead   *int  `json:"fleet_keys_dead,omitempty"`

	// 最近一次开号 · 帮判断"vendor 还活着吗"
	LastDispatchAt      string `json:"last_dispatch_at,omitempty"`
	LastDispatchAgo     string `json:"last_dispatch_ago,omitempty"`
	DispatchesToday     int    `json:"dispatches_today"`
	KeysDispatchedToday int    `json:"keys_dispatched_today"`

	// 库存 · zone 明细
	Zones []AdminZoneRow `json:"zones,omitempty"`
}

// AdminMoney · admin 视图的金额（单位可能是 credit / CNY / USD · 都需要看）
type AdminMoney struct {
	Amount   int64  `json:"amount"`   // microunit
	Currency string `json:"currency"` // credit / CNY / USD
	// Display 展示字符串（"49.98 CNY" / "500 credit"）· 前端不用再算
	Display string `json:"display"`
}

// AdminZoneRow · 单 zone 现价 + 库存
type AdminZoneRow struct {
	Zone         string `json:"zone"` // us / eu / general
	Available    int    `json:"available"`
	UnitCredits  int64  `json:"unit_credits"` // microunit 积分
	UnitDisplay  string `json:"unit_display"` // "100 积分" / "49.98 CNY"
	Source       string `json:"source"`       // vendor_self / xi8 / xi8_notif
	LastUpdateAt string `json:"last_update_at,omitempty"`
}

type AdminOverviewResp struct {
	OK        bool               `json:"ok"`
	CheckedAt string             `json:"checked_at"`
	Vendors   []AdminOverviewRow `json:"vendors"`
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) error {
	if s.db == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "db 未装配")
	}
	now := time.Now().UTC()
	vendors, err := loadAdminOverview(r.Context(), s.db, now)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, AdminOverviewResp{
		OK:        true,
		CheckedAt: now.Format(time.RFC3339),
		Vendors:   vendors,
	})
	return nil
}

// loadAdminOverview · 逐 vendor 组装视图 · 独立函数便于测
func loadAdminOverview(ctx context.Context, db *sql.DB, now time.Time) ([]AdminOverviewRow, error) {
	// vendor 清单从 vendor_probe 最近数据取（哪些家在跑 · 由探针决定）
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT vendor_id FROM vendor_probe
		 WHERE probed_at > ? ORDER BY vendor_id`,
		now.Add(-24*time.Hour).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	var vids []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		vids = append(vids, v)
	}
	rows.Close()

	out := make([]AdminOverviewRow, 0, len(vids))
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	for _, vid := range vids {
		row := AdminOverviewRow{VendorID: vid}
		fillProbe(ctx, db, &row, now)
		fillBalance(ctx, db, &row)
		fillDispatch(ctx, db, &row, now, todayStart)
		fillZones(ctx, db, &row)
		out = append(out, row)
	}
	return out, nil
}

func fillProbe(ctx context.Context, db *sql.DB, row *AdminOverviewRow, now time.Time) {
	var probedAt, errorKind sql.NullString
	var alive sql.NullInt64
	var psGen, psActive, psStock, psDead sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT probed_at, alive, error_kind,
		       ps_generating, ps_keys_active, ps_keys_stock, ps_keys_dead
		  FROM vendor_probe WHERE vendor_id = ?
		 ORDER BY probed_at DESC LIMIT 1`, row.VendorID).Scan(
		&probedAt, &alive, &errorKind, &psGen, &psActive, &psStock, &psDead)
	if err != nil {
		return
	}
	row.Alive = alive.Int64 != 0
	if probedAt.Valid {
		row.LastProbeAt = probedAt.String
		if t, err := time.Parse(time.RFC3339Nano, probedAt.String); err == nil {
			row.LastProbeAgo = agoStr(now.Sub(t))
		}
	}
	if errorKind.Valid && errorKind.String != "" {
		row.ProbeErrorKind = errorKind.String
	}
	if psGen.Valid {
		b := psGen.Int64 != 0
		row.FleetGenerating = &b
	}
	if psActive.Valid {
		n := int(psActive.Int64)
		row.FleetKeysActive = &n
	}
	if psStock.Valid {
		n := int(psStock.Int64)
		row.FleetKeysStock = &n
	}
	if psDead.Valid {
		n := int(psDead.Int64)
		row.FleetKeysDead = &n
	}
}

func fillBalance(ctx context.Context, db *sql.DB, row *AdminOverviewRow) {
	// vendor_ledger 最新一条的 balance_after · 有则用（vendor 侧账本 · 权威）
	var amountAfter sql.NullInt64
	var createdAt sql.NullString
	// 表列 · 参考 migration 033_vendor_ledger.sql · 若无 balance_after 走 nil
	err := db.QueryRowContext(ctx, `
		SELECT balance_after, created_at FROM vendor_ledger
		 WHERE vendor_id = ? AND balance_after IS NOT NULL
		 ORDER BY created_at DESC LIMIT 1`, row.VendorID).Scan(&amountAfter, &createdAt)
	if err == nil && amountAfter.Valid {
		// vendor_ledger 里币种混（CNY/credit/USD）· 从 raw 取要多解析 · v1 简化：
		// 只看数值 · 币种走 vendor 侧固定映射（见 vendorCurrency）
		currency := vendorCurrency(row.VendorID)
		row.Balance = &AdminMoney{
			Amount:   amountAfter.Int64,
			Currency: currency,
			Display:  microDisplay(amountAfter.Int64, currency),
		}
		if createdAt.Valid {
			row.BalanceCheckedAt = createdAt.String
		}
	}
}

func fillDispatch(ctx context.Context, db *sql.DB, row *AdminOverviewRow, now time.Time, todayStart string) {
	// 最近一条
	var lastAt sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT MAX(dispatched_at) FROM vendor_dispatch
		 WHERE vendor_id = ?`, row.VendorID).Scan(&lastAt)
	if err == nil && lastAt.Valid && lastAt.String != "" {
		row.LastDispatchAt = lastAt.String
		if t, err := time.Parse(time.RFC3339Nano, lastAt.String); err == nil {
			row.LastDispatchAgo = agoStr(now.Sub(t))
		}
	}
	// 今日汇总
	var batches, keys sql.NullInt64
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(count), 0) FROM vendor_dispatch
		 WHERE vendor_id = ? AND dispatched_at >= ?`,
		row.VendorID, todayStart).Scan(&batches, &keys)
	row.DispatchesToday = int(batches.Int64)
	row.KeysDispatchedToday = int(keys.Int64)
}

func fillZones(ctx context.Context, db *sql.DB, row *AdminOverviewRow) {
	// vendor_probe_zone 逐 zone 最新一条 · 优先 vendor_self · 次 xi8 · 次 xi8_notif
	rows, err := db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT zone, source, available, our_unit_credits, probed_at,
			       ROW_NUMBER() OVER (PARTITION BY zone ORDER BY
			         CASE source WHEN 'vendor_self' THEN 0 WHEN 'xi8' THEN 1 ELSE 2 END,
			         probed_at DESC
			       ) AS rn
			  FROM vendor_probe_zone
			 WHERE vendor_id = ? AND our_unit_credits > 0
		)
		SELECT zone, source, available, our_unit_credits, probed_at
		  FROM ranked WHERE rn = 1
		 ORDER BY zone`, row.VendorID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var zone, source, probedAt string
		var available, credits sql.NullInt64
		if err := rows.Scan(&zone, &source, &available, &credits, &probedAt); err != nil {
			return
		}
		currency := vendorCurrency(row.VendorID)
		row.Zones = append(row.Zones, AdminZoneRow{
			Zone:         zone,
			Available:    int(available.Int64),
			UnitCredits:  credits.Int64,
			UnitDisplay:  microDisplay(credits.Int64, currency),
			Source:       source,
			LastUpdateAt: probedAt,
		})
	}
}

// microDisplay · microunit 转人话（1_000_000 microunit = 1 单位）
func microDisplay(micro int64, currency string) string {
	whole := micro / 1_000_000
	frac := micro % 1_000_000
	if currency == "credit" {
		return strconv.FormatInt(whole, 10) + " 积分"
	}
	// CNY / USD 两位小数
	cents := frac / 10_000
	if cents < 0 {
		cents = -cents
	}
	return fmt.Sprintf("%d.%02d %s", whole, cents, currency)
}

func agoStr(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return strconv.FormatInt(int64(d.Seconds()), 10) + "s"
	}
	if d < time.Hour {
		return strconv.FormatInt(int64(d.Minutes()), 10) + "m"
	}
	if d < 24*time.Hour {
		return strconv.FormatInt(int64(d.Hours()), 10) + "h"
	}
	return strconv.FormatInt(int64(d.Hours())/24, 10) + "d"
}
