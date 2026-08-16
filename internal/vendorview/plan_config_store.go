package vendorview

// plan_config_store · vendor_plan_config 读写(migration 049)
//
// **为什么在 vendorview 包内建**:offers.go 是唯一消费者·分包只会让"档位来源"跳两层。
// **只读**·后台改档位走 admin API(admin_vendor_plans.go)·不在这里给写方法。

import (
	"context"
	"database/sql"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// PlanConfigStore 读 vendor_plan_config 的启用项。
type PlanConfigStore struct{ db *sql.DB }

func NewPlanConfigStore(db *sql.DB) *PlanConfigStore {
	return &PlanConfigStore{db: db}
}

// EnabledPlans · 拿一家 vendor + kind 下的所有启用档位(enabled=1)
//
// 返回可能为空 · 空 = 后台关掉了该 vendor 该 kind 的所有档 · offers 端点应视为"不上架"。
// 老数据(未建表 or 未 seed)时 s 为 nil · 返 [4 档全开]兜底 · 避免生产回归。
func (s *PlanConfigStore) EnabledPlans(
	ctx context.Context, vendorID string, kind providers.AccountKind,
) ([]providers.SubscriptionPlan, error) {
	if s == nil || s.db == nil {
		return defaultEnabledPlans(kind), nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT subscription FROM vendor_plan_config
		 WHERE vendor_id = ? AND account_kind = ? AND enabled = 1
		 ORDER BY
		   CASE subscription
		     WHEN 'power'    THEN 1
		     WHEN 'pro'      THEN 2
		     WHEN 'pro_plus' THEN 3
		     WHEN 'pro_max'  THEN 4
		     ELSE 99
		   END`, vendorID, string(kind.Normalize()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]providers.SubscriptionPlan, 0, 4)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, providers.SubscriptionPlan(p))
	}
	return out, rows.Err()
}

// defaultEnabledPlans 兜底 · store 未装配时按 2026-08-16 实况:
//   企业 → power 一档 · 个人 → pro + pro_plus 两档
// 生产建表 seed 后走 DB · 兜底只在异常路径生效
func defaultEnabledPlans(kind providers.AccountKind) []providers.SubscriptionPlan {
	if kind.Normalize() == providers.AccountPersonal {
		return []providers.SubscriptionPlan{providers.PlanPro, providers.PlanProPlus}
	}
	return []providers.SubscriptionPlan{providers.PlanPower}
}

// UpsertPlan · 后台开关档位 · admin API 用
//
// enabled=true → 上架该组合 · enabled=false → 下架
// 幂等 · 重复调用只更新时间戳
func (s *PlanConfigStore) UpsertPlan(
	ctx context.Context, vendorID string, kind providers.AccountKind,
	plan providers.SubscriptionPlan, enabled bool,
) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vendor_plan_config (vendor_id, account_kind, subscription, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, account_kind, subscription) DO UPDATE SET
		  enabled    = excluded.enabled,
		  updated_at = excluded.updated_at`,
		vendorID, string(kind.Normalize()), string(plan), en, now, now)
	return err
}

// ListAll · 后台查全表 · 返回所有配置行 · 用于渲染管理 UI
func (s *PlanConfigStore) ListAll(ctx context.Context) ([]PlanConfigRow, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT vendor_id, account_kind, subscription, enabled, updated_at
		  FROM vendor_plan_config
		 ORDER BY vendor_id, account_kind, subscription`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlanConfigRow, 0, 48)
	for rows.Next() {
		var r PlanConfigRow
		var en int
		var updated string
		if err := rows.Scan(&r.VendorID, &r.AccountKind, &r.Subscription, &en, &updated); err != nil {
			return nil, err
		}
		r.Enabled = en != 0
		r.UpdatedAt = updated
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlanConfigRow · 后台列表行
type PlanConfigRow struct {
	VendorID     string
	AccountKind  string
	Subscription string
	Enabled      bool
	UpdatedAt    string
}
