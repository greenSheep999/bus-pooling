package api

import (
	"net/http"
	"time"
)

// 顶部跑马灯活动位（GET /api/promos）。
//
// **公开端点·不要求登录** —— 跑马灯在 landing / 登录页也要显示。
//
// 只下发已启用 + 未过期的条目。过期判定在**服务端**做：
// 客户端时钟不可信（用户改系统时间就能看过期活动），而且过期条目根本不该出网。

type promoItemDTO struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// To 空 = 不可点（纯公告）
	To string `json:"to,omitempty"`
	// CountdownUntil 空 = 不显示倒计时 · 非空是 RFC3339
	CountdownUntil string `json:"countdown_until,omitempty"`
}

type promosResp struct {
	Items []promoItemDTO `json:"items"`
	// ServerNow 服务端当前时间 · 前端拿它算倒计时的**基准**
	//
	// 为什么必须给：客户端时钟可能偏几分钟甚至几天。用 (serverNow - clientNow) 算出
	// 偏移量，倒计时才准。不给的话时钟快的用户会提前看到"已结束"。
	ServerNow string `json:"server_now"`
}

// handleListPromos · GET /api/promos · 公开。
func (s *Server) handleListPromos(w http.ResponseWriter, r *http.Request) error {
	now := time.Now().UTC()
	items := make([]promoItemDTO, 0, len(s.promos))
	for _, p := range s.promos {
		if !p.Enabled {
			continue
		}
		// 倒计时到点 → 自动下线（运营忘了关也不会挂过期活动）
		if p.CountdownUntil != "" {
			until, err := time.Parse(time.RFC3339, p.CountdownUntil)
			if err == nil && !now.Before(until) {
				continue
			}
		}
		items = append(items, promoItemDTO{
			ID:             p.ID,
			Text:           p.Text,
			To:             p.To,
			CountdownUntil: p.CountdownUntil,
		})
	}
	writeJSON(w, http.StatusOK, promosResp{
		Items:     items,
		ServerNow: now.Format(time.RFC3339),
	})
	return nil
}
