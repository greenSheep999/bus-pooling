package deathwatch

// usage.go · 我方 housepool 侧号用量数据 → 落 credential_usage_snapshot + 兜底填 subscription
//
// **语义边界**（docs/06-db-schema §12.5a/b/c）：
//
//	§12.5a credential_usage_snapshot         · 周期采样（本文件）· 前端号详情进度条 · 5min 一采
//	§12.5b passenger_usage_log(⏸ 未来)       · 下游 pool 请求日志 · RPM/TPM/calls · 分摊/并发用
//	§12.5c credential_ledger.credits_used    · 死号那一刻的最后快照 · 死后不再变（markDead 写）
//
// 本文件只负责 §12.5a · 顺手兜底 credential_ledger.subscription（BatchImport 路径没落时）·
// **不写 credits_used**（那是 markDead 一次性写的）· **不采下游请求日志**（那要接 passengerpool）。

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// refreshUsageSnapshot · 一号一次采样 · 落 credential_usage_snapshot
//
// 幂等靠 UNIQUE(kiro_rs_credential_id, observed_at) —— 同一秒两次采样会因唯一约束跳过
// （用 INSERT OR IGNORE 静默 · 不当错）。
//
// 顺手做兜底 subscription 补漏（不覆盖已有值 · 手工池 offer.Subscription 是权威）。
func (w *Watcher) refreshUsageSnapshot(ctx context.Context, cred *housepool.Credential) {
	if cred == nil || cred.Balance == nil {
		return // Balance nil 就是号池没探过 · 跳过
	}
	// 转 microunit · 跟 credential_ledger.credits_used 单位一致（都是 CNY 积分 × 1e6）
	usedMicro := int64(cred.Balance.CurrentUsage * 1_000_000)
	limitMicro := int64(cred.Balance.UsageLimit * 1_000_000)
	title := cred.Balance.SubscriptionTitle
	var nextReset any
	if cred.Balance.NextResetAt != nil {
		nextReset = cred.Balance.NextResetAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}

	// 落快照 · INSERT OR IGNORE 幂等
	_, err := w.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO credential_usage_snapshot
		  (id, kiro_rs_credential_id, current_usage_micro, usage_limit_micro,
		   subscription_title, next_reset_at, observed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), uint64(cred.ID), usedMicro, limitMicro,
		nullIfEmpty(title), nextReset, formatTime(w.now().UTC()))
	if err != nil {
		w.log.Warn("deathwatch: 落 usage snapshot 失败",
			"credential_id", cred.ID, "err", err)
	}

	// 兜底填 credential_ledger.subscription（NULL 才写 · 不覆盖手工池 offer 权威值）
	plan := normalizeSubscriptionTitle(title)
	if plan == "" {
		return
	}
	_, err = w.db.ExecContext(ctx, `
		UPDATE credential_ledger
		   SET subscription = ?
		 WHERE kiro_rs_credential_id = ? AND subscription IS NULL`,
		string(plan), uint64(cred.ID))
	if err != nil {
		w.log.Warn("deathwatch: 补 subscription 失败", "credential_id", cred.ID, "err", err)
	}
}

// normalizeSubscriptionTitle · "KIRO PRO+" → "pro_plus" 等
// 跟 decider/settle.normalizePlan 同一套映射 · 独立复制一份避免 import 环。
func normalizeSubscriptionTitle(raw string) providers.SubscriptionPlan {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "kiro")
	s = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)

	switch s {
	case "power":
		return providers.PlanPower
	case "pro":
		return providers.PlanPro
	case "pro+", "proplus":
		return providers.PlanProPlus
	case "promax":
		return providers.PlanProMax
	}
	return ""
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
