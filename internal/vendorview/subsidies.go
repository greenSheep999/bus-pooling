package vendorview

// 减免栈（docs/10-pricing §3）· 跟 tier 正交 · 有时效额度 · 用完/到期自动恢复
//
// 4 种减免（跟静态计费栈的 4 层一一对应）：
//   channel_fee    · 充值时的通道费 · 走独立科目 · 不在本文处理（充值流水那侧）
//   service_fee    · 减服务费层
//   single_pull    · 减单拉分项层
//   total_discount · 减组合价整体（最后）

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// applySubsidies · PricedFor 调用 · 返本次可减多少积分
//
// 只处理三个 kind：service_fee / single_pull / total_discount
// channel_fee 不管（那是充值流水那边的事）
//
// 每次消费一次 remaining_uses · 用完自动切 exhausted 状态。expires_at 到期视为无效跳过。
func (s *Service) applySubsidies(ctx context.Context, passengerID string, bd Breakdown) int64 {
	if s == nil || s.probeStore == nil || passengerID == "" {
		return 0
	}
	rows, err := s.probeStore.db.QueryContext(ctx, `
		SELECT id, kind, amount_rule
		  FROM user_subsidy
		 WHERE passenger_id = ?
		   AND (remaining_uses IS NULL OR remaining_uses > used_count)
		   AND (expires_at IS NULL OR expires_at > ?)
		   AND kind IN ('service_fee','single_pull','total_discount')
		 ORDER BY created_at ASC`,
		passengerID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0
	}
	defer rows.Close()

	type row struct {
		id     string
		kind   string
		amount string
	}
	var applied []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.kind, &r.amount); err != nil {
			return 0
		}
		applied = append(applied, r)
	}

	var total int64
	for _, r := range applied {
		delta := computeSubsidyDelta(r.kind, r.amount, bd)
		if delta <= 0 {
			continue
		}
		total += delta
		// 消费一次
		_, _ = s.probeStore.db.ExecContext(ctx,
			`UPDATE user_subsidy SET used_count = used_count + 1 WHERE id = ?`,
			r.id)
	}
	return total
}

// computeSubsidyDelta · 单个减免可减多少
//
// amount_rule JSON 两种形态：
//   {"kind":"waive"}          · 全免（返对应层的完整金额）
//   {"kind":"pct","pct":10}   · 按百分比减
type amountRule struct {
	Kind string  `json:"kind"` // waive / pct
	Pct  float64 `json:"pct"`  // 0-100
}

func computeSubsidyDelta(kind, amountJSON string, bd Breakdown) int64 {
	var rule amountRule
	if err := json.Unmarshal([]byte(amountJSON), &rule); err != nil {
		return 0
	}

	// 每种 kind 对应到 Breakdown 的哪一层
	var base int64
	switch kind {
	case "service_fee":
		base = bd.ServiceFee
	case "single_pull":
		base = bd.SinglePullExtra
	case "total_discount":
		// 组合价整体（含 base + 所有层）
		base = bd.Base + bd.VendorMarkup + bd.RegionMarkup + bd.SinglePullExtra + bd.ServiceFee
	default:
		return 0
	}

	switch rule.Kind {
	case "waive":
		return base
	case "pct":
		if rule.Pct <= 0 || rule.Pct > 100 {
			return 0
		}
		return int64(float64(base) * rule.Pct / 100)
	default:
		return 0
	}
}

// GrantSubsidy · 发一份减免给用户 · 邀请奖励 / 促销码兑换 / 优惠码使用 时调
//
// 三种 kind + 4 种 source（docs/10-pricing §3）· amount_rule 传 JSON
func (s *Service) GrantSubsidy(ctx context.Context, in GrantSubsidyInput) (string, error) {
	if s == nil || s.probeStore == nil {
		return "", ErrPriceMissing
	}
	id := in.ID
	if id == "" {
		id = generateID()
	}
	amountJSON, err := json.Marshal(in.AmountRule)
	if err != nil {
		return "", err
	}
	_, err = s.probeStore.db.ExecContext(ctx, `
		INSERT INTO user_subsidy
			(id, passenger_id, kind, source, source_ref,
			 amount_rule, remaining_uses, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.PassengerID, in.Kind, in.Source,
		nullIfEmptyStr(in.SourceRef), string(amountJSON),
		nullIfNilIntPtr(in.RemainingUses), nullIfEmptyStr(in.ExpiresAt),
		time.Now().UTC().Format(time.RFC3339))
	return id, err
}

type GrantSubsidyInput struct {
	ID            string    // 空则自动生成
	PassengerID   string
	Kind          string    // channel_fee / service_fee / single_pull / total_discount
	Source        string    // personal_invite / promo / invite_reward / coupon
	SourceRef     string    // 码 id / 奖励 id · 可空
	AmountRule    amountRule
	RemainingUses *int      // nil = 不限次
	ExpiresAt     string    // 空 = 不限时 · RFC3339
}

// ── 小工具 ──

func generateID() string {
	return "sub_" + time.Now().UTC().Format("20060102150405.000000")
}

func nullIfEmptyStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullIfNilIntPtr(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}
