package kirors

import (
	"encoding/json"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// wire → 归一化的翻译。这里和 types.go 是 camelCase 的唯一交界处。

func toCredential(w wireCredential) housepool.Credential {
	c := housepool.Credential{
		ID:                housepool.CredentialID(w.ID),
		Email:             deref(w.Email),
		Priority:          w.Priority,
		Disabled:          w.Disabled,
		DisabledReason:    deref(w.DisabledReason),
		Subscription:      deref(w.Subscription),
		Provider:          deref(w.Provider),
		AuthMethod:        deref(w.AuthMethod),
		Endpoint:          w.Endpoint,
		SourceChannel:     deref(w.SourceChannel),
		Groups:            w.Groups,
		SuccessCount:      w.SuccessCount,
		FailureCount:      w.FailureCount,
		TotalFailureCount: w.TotalFailureCount,
	}
	if w.AccruedCost != nil {
		c.AccruedCost = *w.AccruedCost
	}
	if w.BilledRequests != nil {
		c.BilledRequests = *w.BilledRequests
	}
	c.ExpiresAt = parseTimePtr(w.ExpiresAt)
	c.LastUsedAt = parseTimePtr(w.LastUsedAt)
	if t := parseTimePtr(w.CreatedAt); t != nil {
		c.CreatedAt = *t
	}
	return c
}

func toSnapshot(w wireCredentialList) housepool.PoolSnapshot {
	return housepool.PoolSnapshot{
		Total:         w.Total,
		Available:     w.Available,
		DisabledCount: w.DisabledCount,
		CoolingCount:  w.CoolingCount,
		InFlightTotal: w.InFlightTotal,
		RPMTotal:      w.RPMTotal,
		TPMTotal:      w.TPMTotal,
	}
}

func toBalance(w wireBalance) housepool.Balance {
	return housepool.Balance{
		ID:                housepool.CredentialID(w.ID),
		SubscriptionTitle: deref(w.SubscriptionTitle),
		CurrentUsage:      w.CurrentUsage,
		UsageLimit:        w.UsageLimit,
		Remaining:         w.Remaining,
		UsagePercentage:   w.UsagePercentage,
		NextResetAt:       parseTimeFromNumber(w.NextResetAt),
		OverageEnabled:    w.OverageEnabled,
		OverageCapable:    w.OverageCapable,
	}
}

func toGroup(w wireGroup) housepool.Group {
	g := housepool.Group{
		Name:            w.Name,
		Description:     deref(w.Description),
		CacheMode:       deref(w.CacheMode),
		CacheMetering:   deref(w.CacheMetering),
		CredentialCount: w.CredentialCount,
		ClientKeyCount:  w.ClientKeyCount,
	}
	if w.CompactThreshold != nil {
		g.CompactThreshold = *w.CompactThreshold
	}
	if t := parseTimePtr(&w.CreatedAt); t != nil {
		g.CreatedAt = *t
	}
	return g
}

func toImportCredential(c housepool.ImportCredential) wireImportCredential {
	return wireImportCredential{
		RefreshToken:     c.RefreshToken,
		AccessToken:      c.AccessToken,
		KiroAPIKey:       c.KiroAPIKey,
		Email:            c.Email,
		IssuerURL:        c.IssuerURL,
		StartURL:         c.StartURL,
		TokenEndpoint:    c.TokenEndpoint,
		Scopes:           c.Scopes,
		Region:           c.Region,
		Groups:           c.Groups,
		Priority:         c.Priority,
		SourceChannel:    c.SourceChannel,
		ConcurrencyLimit: c.ConcurrencyLimit,
	}
}

func toBatchImportEvent(w wireBatchImportEvent) housepool.BatchImportEvent {
	e := housepool.BatchImportEvent{
		Index:        w.Index,
		Status:       housepool.BatchImportStatus(w.Status),
		Email:        deref(w.Email),
		Usage:        deref(w.Usage),
		Subscription: deref(w.Subscription),
		Error:        deref(w.Error),
		RolledBack:   w.RolledBack,
	}
	if w.CredentialID != nil {
		id := housepool.CredentialID(*w.CredentialID)
		e.CredentialID = &id
	}
	return e
}

func toBatchImportSummary(w wireBatchImportSummary) housepool.BatchImportSummary {
	return housepool.BatchImportSummary{
		Total:      w.Total,
		Imported:   w.Imported,
		Verified:   w.Verified,
		Duplicate:  w.Duplicate,
		Failed:     w.Failed,
		RolledBack: w.RolledBack,
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// parseTimePtr 容忍几种时间写法。housepool 主要给 RFC3339，但不同字段来源不完全一致。
func parseTimePtr(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, *s); err == nil {
			u := t.UTC()
			return &u
		}
	}
	// 解不出来就当没有 —— 时间字段解析失败不该让整个请求失败
	return nil
}

// parseTimeFromNumber · json.Number(unix epoch)→ *time.Time
// kiro.rs 1.8.3 balance.nextResetAt 返 unix epoch number(小数)· 不是 string
// 用 json.Number 兼容 · Float64() 解 · 无效返 nil
func parseTimeFromNumber(n *json.Number) *time.Time {
	if n == nil {
		return nil
	}
	f, err := n.Float64()
	if err != nil || f <= 0 {
		return nil
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}
