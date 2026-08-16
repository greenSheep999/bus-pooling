package api

// admin_market · 我方第 7 家 Kiro Vendor Market 手工上架 · 只运维用
//
// 运营的日常操作:
//   1) 手工去 vendor 那边买号（拿到 refresh_token / api_key / account 等）
//   2) POST /api/admin/market/offers · 上架货架 · 定分档价 · 开关
//   3) POST /api/admin/market/stock · 一批号 → housepool BatchImport → market_stock_item
//        导入走跟其他 6 家**同一条 BatchImport 链**（车主要求 · docs/24 §3）
//        BatchImport 成功后号已在 housepool prebuy-pool group · 用户提取时才转 group
//
// 全部走 X-Admin-Key 头校验（跟 admin/data-health 一样）· 别泄漏到乘客前端（§0.1）。

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/marketstock"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// GET /api/admin/market/offers · 列所有货架 + 每档当前 available 数
//
// 前端后台展示"当前架上有什么" · 用来核对是否要补货 / 改价。
func (s *Server) handleAdminMarketListOffers(w http.ResponseWriter, r *http.Request) error {
	if s.marketStock == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "marketstock 未装配")
	}
	offers, err := s.marketStock.ListOffers(r.Context())
	if err != nil {
		return err
	}
	// 逐 offer 数 available（少查一次也可 · offer 数量小 · 单个 COUNT 便宜）
	type offerRow struct {
		ID           string                   `json:"id"`
		VendorID     string                   `json:"vendor_id"`
		AccountKind  string                   `json:"account_kind"`
		Subscription string                   `json:"subscription"`
		PriceBands   []providers.QtyPriceBand `json:"price_bands"`
		Enabled      bool                     `json:"enabled"`
		Source       string                   `json:"source,omitempty"`
		Available    int                      `json:"available"`
	}
	out := make([]offerRow, 0, len(offers))
	for _, o := range offers {
		n, err := s.marketStock.AvailableCount(r.Context(), o.ID)
		if err != nil {
			return err
		}
		out = append(out, offerRow{
			ID: o.ID, VendorID: o.VendorID,
			AccountKind: string(o.AccountKind), Subscription: string(o.Subscription),
			PriceBands: o.PriceBands, Enabled: o.Enabled, Source: o.Source,
			Available: n,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"offers": out})
	return nil
}

// POST /api/admin/market/offers · 上架 / 改价 / 开关 · 幂等（同 kind+plan → UPSERT）
type adminMarketUpsertOfferReq struct {
	VendorID     string                   `json:"vendor_id"`    // 目前只 "kiro_market"（未来第 8 家再放开）
	AccountKind  string                   `json:"account_kind"` // enterprise | personal
	Subscription string                   `json:"subscription"` // power | pro | pro_plus | pro_max
	PriceBands   []providers.QtyPriceBand `json:"price_bands"`  // 分档 · Upper=0 = 及以上
	Enabled      bool                     `json:"enabled"`
	Source       string                   `json:"source"` // 落 credential_ledger.source · 用户视角"这号谁提的"
}

func (s *Server) handleAdminMarketUpsertOffer(w http.ResponseWriter, r *http.Request) error {
	if s.marketStock == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "marketstock 未装配")
	}
	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req adminMarketUpsertOfferReq
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	// 白名单校验 · 拒 pool_id / 未来 vendor_id 乱填的错值
	if req.VendorID != string(providers.VendorKiroMarket) {
		return ErrBadRequest(fmt.Sprintf(
			"vendor_id 只支持 %q · 未来加第 8 家再放开", providers.VendorKiroMarket))
	}
	kind := providers.AccountKind(req.AccountKind).Normalize()
	if kind != providers.AccountEnterprise && kind != providers.AccountPersonal {
		return ErrBadRequest("account_kind 必须是 enterprise | personal")
	}
	plan := providers.SubscriptionPlan(req.Subscription)
	if !plan.Valid() {
		return ErrBadRequest(fmt.Sprintf(
			"subscription 必须在 %v", providers.AllSubscriptionPlans))
	}

	id, err := s.marketStock.UpsertOffer(r.Context(), marketstock.UpsertOfferInput{
		VendorID:     req.VendorID,
		AccountKind:  kind,
		Subscription: plan,
		PriceBands:   req.PriceBands,
		Enabled:      req.Enabled,
		Source:       req.Source,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"offer_id": id})
	return nil
}

// POST /api/admin/market/stock · 一批号 → BatchImport → market_stock_item
//
// **关键**：走跟其他 vendor 拉号**同一条** BatchImport 链
// （housepool.Pool.BatchImport → verified/duplicate → market_stock_item.available）·
// 不做手工 UPSERT credential_ledger · 号必须先真进 housepool 再落 store。
type adminMarketImportStockReq struct {
	OfferID string `json:"offer_id"` // 必填 · 归到哪个货架
	// Credentials 每把号一份 · 支持 refresh_token 形态（跟 6 家 vendor 相同）
	// KiroAPIKey / RefreshToken 至少一个 · 号池按 vendor 支持的形态取
	Credentials []adminImportCred `json:"credentials"`
	// ImportedBy 谁导入的（后台账号名 · 落 market_stock_item.imported_by · 审计用）
	ImportedBy string `json:"imported_by"`
}

type adminImportCred struct {
	KiroAPIKey    string `json:"kiro_api_key,omitempty"`
	RefreshToken  string `json:"refresh_token,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	Email         string `json:"email,omitempty"`
	IssuerURL     string `json:"issuer_url,omitempty"`
	StartURL      string `json:"start_url,omitempty"`
	TokenEndpoint string `json:"token_endpoint,omitempty"`
	Scopes        string `json:"scopes,omitempty"`
	Region        string `json:"region,omitempty"`
}

// prebuyPoolGroup · 未售号池 group（跟 decisions §11.15 抢号缓冲的 group 名一致）·
// 卖出时 orchestrator 把号从这里搬到 bus-<id> / record-<pid>
const prebuyPoolGroup = "prebuy-pool"

func (s *Server) handleAdminMarketImportStock(w http.ResponseWriter, r *http.Request) error {
	if s.marketStock == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "marketstock 未装配")
	}
	if s.pool == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "housepool 未装配")
	}
	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req adminMarketImportStockReq
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	if req.OfferID == "" {
		return ErrBadRequest("offer_id 必填")
	}
	if len(req.Credentials) == 0 {
		return ErrBadRequest("credentials 至少一条")
	}
	if req.ImportedBy == "" {
		return ErrBadRequest("imported_by 必填(审计需要)")
	}

	// 走跟 vendor 拉号**同一条** BatchImport · 号进 prebuy-pool group
	creds := make([]housepool.ImportCredential, 0, len(req.Credentials))
	for _, c := range req.Credentials {
		if c.KiroAPIKey == "" && c.RefreshToken == "" {
			return ErrBadRequest("每把号 kiro_api_key 或 refresh_token 至少一个")
		}
		creds = append(creds, housepool.ImportCredential{
			KiroAPIKey:    c.KiroAPIKey,
			RefreshToken:  c.RefreshToken,
			AccessToken:   c.AccessToken,
			Email:         c.Email,
			IssuerURL:     c.IssuerURL,
			StartURL:      c.StartURL,
			TokenEndpoint: c.TokenEndpoint,
			Scopes:        c.Scopes,
			Region:        c.Region,
			Groups:        []string{prebuyPoolGroup},
			SourceChannel: "market_admin",
		})
	}

	// 先查 offer 拿到上架档位 · 后面校验号真实档跟它一致（告警不阻塞）
	offer, err := s.marketStock.FindOfferByID(r.Context(), req.OfferID)
	if err != nil {
		return ErrBadRequest(fmt.Sprintf("offer_id 不存在或已禁用: %s", req.OfferID))
	}

	result, err := s.pool.BatchImport(r.Context(), housepool.BatchImportRequest{
		Credentials: creds,
		Verify:      true, // 不验活的号一上线就死 · 见 decider/import.go 同样约定
	})
	if err != nil {
		return fmt.Errorf("BatchImport 启动: %w", err)
	}

	// 收 SSE 流 · verified/duplicate 落 market_stock_item · failed 计数报错 · 顺手做档位一致性告警
	sum := collectImportEvents(r.Context(), s.marketStock, req.OfferID, req.ImportedBy, offer.Subscription, result)
	// 排空 summary 让流关闭
	for range result.Summary {
	}
	if err := result.Err(); err != nil {
		return fmt.Errorf("BatchImport 流中断: %w", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imported":              sum.Imported,
		"duplicate":             sum.Duplicate,
		"failed":                sum.Failed,
		"offer_id":              req.OfferID,
		"offer_subscription":    string(offer.Subscription),
		"subscription_mismatch": sum.SubMismatch, // 空数组表示全对 · 非空运营应关注
	})
	return nil
}

// importSummary · 导入结果汇总（含档位不一致警告）
type importSummary struct {
	Imported     int
	Failed       int
	Duplicate    int
	SubMismatch  []subMismatchWarn // 档位不一致告警 · 运营应对齐 offer 或换 offer
	ImportedSubs []string          // 每把号号池回报的原始 subscription（对账用）
}

// subMismatchWarn · 一把号的档位跟 offer 上架档位不一致
type subMismatchWarn struct {
	KiroRSCredentialID uint64 `json:"kiro_rs_credential_id"`
	OfferPlan          string `json:"offer_plan"`     // offer 上架时选的（我们卖成这档）
	UpstreamTitle      string `json:"upstream_title"` // housepool 返的号真实档
}

// collectImportEvents · 消费 BatchImport 事件流 · verified/duplicate → market_stock_item.available
//
// **duplicate 也当成功**（跟 decider/import.go 同一条规则）· housepool BatchImport
// 幂等 · 同 refresh_token 重复导入返 duplicate + 原 credential_id · 忽略它会让
// admin 无法重跑失败批。
//
// **档位一致性告警**（本次加）：号池 SSE 事件里 evt.Subscription 是号真实档位（"KIRO PRO+"）·
// 跟 offer.Subscription（运营上架时选的档）比 · 不一致就加进 SubMismatch · 告诉运营:
//   - 号真实档比 offer 高 → 用户占便宜（我们卖亏了）
//   - 号真实档比 offer 低 → 用户吃亏（我们卖贵了 · 有客诉风险）
//
// 只是**告警不阻塞** —— 运营可能就是想按 offer 定价卖出去（比如清仓、促销）· 不能自动拒。
func collectImportEvents(
	ctx context.Context,
	store *marketstock.Store,
	offerID, importedBy string,
	offerPlan providers.SubscriptionPlan,
	result *housepool.BatchImportResult,
) importSummary {
	var sum importSummary
	for evt := range result.Events {
		switch evt.Status {
		case housepool.ImportStatusVerified, housepool.ImportStatusDuplicate:
			if evt.CredentialID == nil {
				sum.Failed++
				continue
			}
			// 档位一致性检查（不阻塞 · 只告警）· 见函数注释
			if evt.Subscription != "" && offerPlan != "" {
				sum.ImportedSubs = append(sum.ImportedSubs, evt.Subscription)
				upstreamPlan := normalizePlanForImport(evt.Subscription)
				if upstreamPlan != "" && upstreamPlan != offerPlan {
					sum.SubMismatch = append(sum.SubMismatch, subMismatchWarn{
						KiroRSCredentialID: uint64(*evt.CredentialID),
						OfferPlan:          string(offerPlan),
						UpstreamTitle:      evt.Subscription,
					})
				}
			}
			// duplicate 时号池返原 credential_id · 但 stock_item 可能已经存在（UNIQUE 约束）·
			// AddItem 会拒重复 · 那就当成功不新增（保证 admin 重跑幂等）
			_, addErr := store.AddItem(ctx, marketstock.AddItemInput{
				OfferID:            offerID,
				KiroRSCredentialID: uint64(*evt.CredentialID),
				ImportedBy:         importedBy,
			})
			if addErr != nil {
				// 大概率是 UNIQUE 冲突（重跑幂等）· 计入 dup 而不是 failed
				if evt.Status == housepool.ImportStatusDuplicate {
					sum.Duplicate++
				} else {
					// verified 但落 market_stock_item 失败 = 号已进池但账没记 · 报警
					sum.Failed++
				}
				continue
			}
			if evt.Status == housepool.ImportStatusDuplicate {
				sum.Duplicate++
			} else {
				sum.Imported++
			}
		case housepool.ImportStatusFailed:
			sum.Failed++
		}
	}
	return sum
}

// normalizePlanForImport · "KIRO PRO+" → "pro_plus" 归一
// 跟 decider/settle.normalizePlan / deathwatch.normalizeSubscriptionTitle 同一套规则 ·
// 独立复制一份避免 import 环（api 层不该 import decider/deathwatch 内部工具）
func normalizePlanForImport(raw string) providers.SubscriptionPlan {
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
