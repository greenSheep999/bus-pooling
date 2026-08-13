package kiroappcc

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 真实 txns 响应 fixture（2026-08-14 · 登录 session 抓的原文 · bare array）
const realTxnsBody = `[` +
	`{"id":2434,"delta":-25,"reason":"claim","refType":"inventory","refId":"2948","balanceAfter":785,"createdAt":"2026-08-01T11:38:39.291815435+00:00"},` +
	`{"id":131,"delta":100,"reason":"recharge","refType":"topup","refId":"KA1","balanceAfter":885,"createdAt":"2026-07-30T10:00:00+00:00"}]`

func TestParseTxns_RealShape(t *testing.T) {
	var rows []txnRow
	if err := json.Unmarshal([]byte(realTxnsBody), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("应 2 笔 · 得 %d", len(rows))
	}
	// claim -25 → purchase · 负
	if normalizeCCReason(rows[0].Reason) != providers.LedgerPurchase {
		t.Errorf("claim 应 purchase · 得 %s", normalizeCCReason(rows[0].Reason))
	}
	if rows[0].RefID != "2948" || rows[0].BalanceAfter != 785 {
		t.Errorf("字段解析错 · %+v", rows[0])
	}
	// createdAt 带纳秒+tz · parseTime 认
	if parseTime(rows[0].CreatedAt).IsZero() {
		t.Error("createdAt 应解析出来")
	}
	// recharge → recharge
	if normalizeCCReason(rows[1].Reason) != providers.LedgerRecharge {
		t.Errorf("recharge 归一错 · 得 %s", normalizeCCReason(rows[1].Reason))
	}
}

// live 测试 · 有网页账密才跑（验证 login-session 全链路：登录→拿 token→打 txns→解析）
func TestListLedger_Live(t *testing.T) {
	user := os.Getenv("BP_VENDOR_KIROAPPCC_LOGIN_USER")
	pass := os.Getenv("BP_VENDOR_KIROAPPCC_LOGIN_PASS")
	if user == "" || pass == "" {
		t.Skip("无 BP_VENDOR_KIROAPPCC_LOGIN_USER/PASS · 跳过 live 测试")
	}
	a, err := New(Config{
		BaseURL: "https://kiroapp.cc", LoginUser: user, LoginPass: pass,
		Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := a.ListLedger(ctx, "")
	if err != nil {
		t.Fatalf("live ListLedger: %v", err)
	}
	t.Logf("live 拉到 %d 笔流水", len(page.Items))
	for i, e := range page.Items {
		if i >= 3 {
			break
		}
		t.Logf("  %s reason=%s amount=%d order=%s", e.EntryID, e.Reason, e.Amount.Amount, e.OrderID)
	}
}
