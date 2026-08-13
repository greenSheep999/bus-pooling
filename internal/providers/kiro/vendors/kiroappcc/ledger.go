package kiroappcc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ListLedger · GET /api/user/txns · vendor 侧积分流水（交叉对账 · docs/20 §1）。
//
// **鉴权特殊**（2026-08-14 实测）：`/api/user/*` 不认 API key（API key 只管 /openapi/*）·
// 要**网页登录 session token**。本 vendor 登录**无验证码**（`POST /api/user/login
// {username,password}` → `{token,user}`）· 所以可自动重登 · 适合 backfiller（不像 kirodrop 要验证码）。
//
// **响应形状实测确认**（bare array）：
//
//	[{"id":2434,"delta":-25,"reason":"claim","refType":"inventory","refId":"2948",
//	  "balanceAfter":785,"createdAt":"2026-08-01T11:38:39...+00:00"}, ...]
//
// delta 已带符号（claim -25 = 扣费）· reason "claim"=提货扣费 · refId 是对账 join 键 ·
// createdAt RFC3339（带纳秒+tz · time.Parse 认）· 积分单位（1:1 CNY · ×1_000_000 到 microunit）。
func (a *Adapter) ListLedger(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorLedgerEntry], error) {
	if a.cfg.LoginUser == "" || a.cfg.LoginPass == "" {
		return nil, fmt.Errorf("kiroappcc: ledger 需网页账密（LoginUser/Pass 未配）")
	}
	body, err := a.userGet(ctx, "/api/user/txns?limit=200")
	if err != nil {
		return nil, err
	}
	var rows []txnRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("kiroappcc: txns 解析: %w", err)
	}
	out := make([]providers.VendorLedgerEntry, 0, len(rows))
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		reason := normalizeCCReason(r.Reason)
		mag := r.Delta
		if mag < 0 {
			mag = -mag
		}
		micro := int64(mag) * 1_000_000
		if reason == providers.LedgerPurchase {
			micro = -micro
		}
		out = append(out, providers.VendorLedgerEntry{
			EntryID:      strconv.FormatInt(r.ID, 10),
			OrderID:      r.RefID,
			Reason:       reason,
			RawReason:    r.Reason,
			Amount:       providers.Money{Amount: micro, Currency: providers.CurrencyCredit},
			BalanceAfter: providers.Money{Amount: int64(r.BalanceAfter) * 1_000_000, Currency: providers.CurrencyCredit},
			CreatedAt:    parseTime(r.CreatedAt),
			Raw:          raw,
		})
	}
	return &providers.HistoryPage[providers.VendorLedgerEntry]{Items: out}, nil
}

type txnRow struct {
	ID           int64  `json:"id"`
	Delta        int    `json:"delta"` // 带符号 · 积分
	Reason       string `json:"reason"`
	RefType      string `json:"refType"`
	RefID        string `json:"refId"`
	BalanceAfter int    `json:"balanceAfter"`
	CreatedAt    string `json:"createdAt"`
}

// normalizeCCReason · kiroappcc txns reason → 我方 6 类（claim 实测 · 其余按语义 · 未知归 other）
func normalizeCCReason(r string) string {
	switch r {
	case "claim":
		return providers.LedgerPurchase
	case "recharge", "topup", "redeem":
		return providers.LedgerRecharge
	case "refund", "warranty", "warranty_refund":
		return providers.LedgerRefund
	case "adjust", "admin_adjust":
		return providers.LedgerAdjust
	default:
		return providers.LedgerOther
	}
}

// userGet · 打 /api/user/* 端点 · 自动带 session token · 401 时重登一次再试。
func (a *Adapter) userGet(ctx context.Context, path string) ([]byte, error) {
	tok, err := a.ensureSession(ctx, false)
	if err != nil {
		return nil, err
	}
	body, status, err := a.doUserGet(ctx, path, tok)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		// token 过期 · 强制重登一次
		tok, err = a.ensureSession(ctx, true)
		if err != nil {
			return nil, err
		}
		body, status, err = a.doUserGet(ctx, path, tok)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kiroappcc: %s http %d", path, status)
	}
	return body, nil
}

func (a *Adapter) doUserGet(ctx context.Context, path, tok string) ([]byte, int, error) {
	req, err := a.newReq(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, err
	}
	// 覆盖 newReq 设的 API key 鉴权 · /api/user/* 用 session token
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

// ensureSession · 返当前 session token · 无缓存或 force 时登录换新。
func (a *Adapter) ensureSession(ctx context.Context, force bool) (string, error) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if a.sessionToken != "" && !force {
		return a.sessionToken, nil
	}
	payload, _ := json.Marshal(map[string]string{
		"username": a.cfg.LoginUser,
		"password": a.cfg.LoginPass,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(a.cfg.BaseURL, "/")+"/api/user/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("kiroappcc: 登录: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kiroappcc: 登录 http %d", resp.StatusCode)
	}
	var lr struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body, &lr); err != nil {
		return "", fmt.Errorf("kiroappcc: 登录响应解析: %w", err)
	}
	if lr.Token == "" {
		return "", fmt.Errorf("kiroappcc: 登录未返 token")
	}
	a.sessionToken = lr.Token
	return lr.Token, nil
}
