package kirooo

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 真实响应 fixture（2026-08-14 · vendor-probe 抓的原文 · 脱敏后）
const realCreditsBody = `{"credits":70,"ledger":[` +
	`{"id":686,"kind":"claim_key","amount":-30,"balance_after":70,"ref_id":"88234229e8de904444c36d9ec949f973","note":"欧洲区自助提货 Key #4415","created_at":"2026-08-09 17:14:05"},` +
	`{"id":131,"kind":"recharge","amount":100,"balance_after":100,"ref_id":"KA135T1786006789327","note":"支付宝充值 100.00 元到账","created_at":"2026-08-06 17:00:28"}],` +
	`"master_price":500,"risk_flag":0}`

func parseCredits(t *testing.T, body string) []providers.VendorLedgerEntry {
	t.Helper()
	var wrap creditsResp
	if err := json.Unmarshal([]byte(body), &wrap); err != nil {
		t.Fatalf("解析: %v", err)
	}
	out := make([]providers.VendorLedgerEntry, 0, len(wrap.Ledger))
	for _, r := range wrap.Ledger {
		reason := normalizeKiroooKind(r.Kind)
		mag := r.Amount
		if mag < 0 {
			mag = -mag
		}
		micro := int64(mag) * 1_000_000
		if reason == providers.LedgerPurchase {
			micro = -micro
		}
		out = append(out, providers.VendorLedgerEntry{
			EntryID:   itoa(r.ID),
			OrderID:   r.RefID,
			Reason:    reason,
			RawReason: r.Kind,
			Amount:    providers.Money{Amount: micro, Currency: providers.CurrencyCredit},
			CreatedAt: parseKiroooTime(r.CreatedAt),
		})
	}
	return out
}

func itoa(n int64) string {
	// 跟 ledger.go 的 strconv.FormatInt 一致 · 测试里省依赖
	return strconvFormat(n)
}
func strconvFormat(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// 真实响应逐字段核对
func TestKiroooLedger_RealShape(t *testing.T) {
	entries := parseCredits(t, realCreditsBody)
	if len(entries) != 2 {
		t.Fatalf("应 2 笔 · 得 %d", len(entries))
	}

	// claim_key → purchase · 扣费为负 · 30 积分 = -30_000_000 microunit
	claim := entries[0]
	if claim.Reason != providers.LedgerPurchase {
		t.Errorf("claim_key 应 purchase · 得 %s", claim.Reason)
	}
	if claim.Amount.Amount != -30_000_000 {
		t.Errorf("claim 应 -30_000_000 microunit · 得 %d", claim.Amount.Amount)
	}
	if claim.OrderID != "88234229e8de904444c36d9ec949f973" {
		t.Errorf("OrderID 应取 ref_id · 得 %q", claim.OrderID)
	}
	if claim.EntryID != "686" {
		t.Errorf("EntryID 应取 id · 得 %q", claim.EntryID)
	}

	// recharge → recharge · 入账为正
	rc := entries[1]
	if rc.Reason != providers.LedgerRecharge {
		t.Errorf("recharge 应 recharge · 得 %s", rc.Reason)
	}
	if rc.Amount.Amount != 100_000_000 {
		t.Errorf("recharge 应 +100_000_000 · 得 %d", rc.Amount.Amount)
	}
}

// created_at 是北京墙钟 · 必须 -8h 转 UTC（时区坑回归哨兵）
func TestKiroooLedger_BeijingWallClock(t *testing.T) {
	entries := parseCredits(t, realCreditsBody)
	// "2026-08-09 17:14:05" 北京 = 09:14:05 UTC
	want := time.Date(2026, 8, 9, 9, 14, 5, 0, time.UTC)
	if !entries[0].CreatedAt.Equal(want) {
		t.Errorf("北京墙钟应 -8h 转 UTC · want %v got %v", want, entries[0].CreatedAt)
	}
}

// kind 归一
func TestNormalizeKiroooKind(t *testing.T) {
	cases := map[string]string{
		"claim_key": providers.LedgerPurchase,
		"recharge":  providers.LedgerRecharge,
		"refund":    providers.LedgerRefund,
		"adjust":    providers.LedgerAdjust,
		"没见过的":      providers.LedgerOther,
	}
	for kind, want := range cases {
		if got := normalizeKiroooKind(kind); got != want {
			t.Errorf("%s → %s · want %s", kind, got, want)
		}
	}
}
