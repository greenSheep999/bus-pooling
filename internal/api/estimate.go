package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// POST /api/me/pull/estimate · 提取确认窗的费用预估（**不下单**）。
//
// 对外**只**返单价 / 总额 / 服务费（CLAUDE.md §0.1）· 加价链分层字段绝不出。

type estimateReq struct {
	VendorID   string `json:"vendor_id"`
	Zone       string `json:"zone,omitempty"`
	Count      int    `json:"count"`
	CouponCode string `json:"coupon_code,omitempty"` // 1a 阶段 mock：无实际减免
}

type estimateResp struct {
	UnitPrice  int64 `json:"unit_price"`  // 加价链算完的单价
	ServiceFee int64 `json:"service_fee"` // 服务费一项
	Total      int64 `json:"total"`       // = unit_price × count
}

func (s *Server) handleEstimate(w http.ResponseWriter, r *http.Request) error {
	if s.decider == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拉号服务暂未装配")
	}
	if _, err := mustCaller(r); err != nil {
		return err
	}

	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req estimateReq
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	if req.Count < 1 {
		return ErrBadRequest("count 必须 ≥ 1")
	}

	unit, err := s.estimateUnitCost(r.Context(), req.VendorID, req.Zone)
	if err != nil {
		if errors.Is(err, decider.ErrNoStock) {
			return ErrConflict("no_stock", "暂无可拉的号，稍后再试")
		}
		return err
	}
	bd := s.decider.PriceEstimate(unit, req.Count)
	writeJSON(w, http.StatusOK, estimateResp{
		UnitPrice:  bd.UnitPrice,
		ServiceFee: bd.ServiceFee,
		Total:      bd.Total,
	})
	return nil
}

// estimateUnitCost 从 vendor 库存快照拿单价（作为估价用；实扣以 purchase 为准）。
// 1a 只支持 kiro91（DRY_RUN 或 live）· vendor_id 请求参数被服务端裁定。
func (s *Server) estimateUnitCost(ctx context.Context, _ string, zone string) (int64, error) {
	// 直接问 decider 用的那个 vendor · 别自己 registry 查一遍
	stock, err := s.decider.VendorStock(ctx, providers.Zone(zone))
	if err != nil {
		return 0, err
	}
	if stock.Available <= 0 {
		return 0, decider.ErrNoStock
	}
	for _, z := range stock.Zones {
		if zone == "" || string(z.Zone) == zone {
			if z.Available > 0 {
				return z.UnitPrice.Amount, nil
			}
		}
	}
	return 0, decider.ErrNoStock
}
