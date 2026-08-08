package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// Bus 相关端点。响应形状对齐 web/src/types/index.ts 的 Bus / BusMember。

type busResponse struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Status      string       `json:"status"`
	MemberCount int          `json:"member_count"`
	InviteCode  *string      `json:"invite_code"`
	CreatedAt   string       `json:"created_at"`
	Members     []memberResp `json:"members"`
	// 号池汇总 · 1a 先给 0（Iss #10 只做元数据，号统计在 §credentials 端点单独查）
	AliveCount         int   `json:"alive_count"`
	DeadCount          int   `json:"dead_count"`
	SpendToday         int64 `json:"spend_today"`
	AvgLifespanSeconds int64 `json:"avg_lifespan_seconds"`
}

type memberResp struct {
	PassengerID   string  `json:"passenger_id"`
	Username      string  `json:"username"`
	Role          string  `json:"role"`
	JoinedAt      string  `json:"joined_at"`
	SharePct      int     `json:"share_pct"`
	Balance       int64   `json:"balance"`
	Status        string  `json:"status"`
	SkippedCount  int     `json:"skipped_count"`
	LastSkippedAt *string `json:"last_skipped_at"`
}

type createBusReq struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (s *Server) handleCreateBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	var req createBusReq
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	kind := bus.Kind(req.Kind)
	if kind == "" {
		kind = bus.KindSingle
	}

	b, err := s.buses.Create(r.Context(), bus.CreateInput{
		Name:      req.Name,
		Kind:      kind,
		CreatorID: p.ID,
	})
	switch {
	case errors.Is(err, bus.ErrBadKind):
		return ErrBadRequest("阶段 1a 只支持 single 车")
	case err != nil:
		return err
	}

	resp, err := s.buildBusResponse(r, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, resp)
	return nil
}

func (s *Server) handleListBuses(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	list, err := s.buses.ListForPassenger(r.Context(), p.ID)
	if err != nil {
		return err
	}
	items := make([]busResponse, 0, len(list))
	for i := range list {
		resp, err := s.buildBusResponse(r, &list[i])
		if err != nil {
			return err
		}
		items = append(items, resp)
	}
	// 分页信封（1a 单人车不会太多，一页装下；契约上限 500）
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": len(items),
		"page": 1, "page_size": len(items), "pages": 1,
	})
	return nil
}

func (s *Server) handleGetBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	id := r.PathValue("bus_id")
	b, err := s.buses.GetForPassenger(r.Context(), id, p.ID)
	switch {
	case errors.Is(err, bus.ErrNotFound), errors.Is(err, bus.ErrNotMember):
		// 404 而不是 403 —— 不泄漏"车存在但你不是成员"
		return ErrNotFound("找不到这辆车")
	case err != nil:
		return err
	}
	resp, err := s.buildBusResponse(r, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) handleLeaveBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	id := r.PathValue("bus_id")
	err = s.buses.Leave(r.Context(), id, p.ID)
	switch {
	case errors.Is(err, bus.ErrNotFound), errors.Is(err, bus.ErrNotMember):
		return ErrNotFound("找不到这辆车")
	case errors.Is(err, bus.ErrOwnerCantLeave):
		return ErrConflict(CodeConflict, "你是车主，只能解散车不能退出")
	case errors.Is(err, bus.ErrDissolved):
		return ErrConflict(CodeConflict, "车已解散")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (s *Server) handleDissolveBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	id := r.PathValue("bus_id")
	err = s.buses.Dissolve(r.Context(), id, p.ID)
	switch {
	case errors.Is(err, bus.ErrNotFound), errors.Is(err, bus.ErrNotMember):
		// 非 creator 或不存在，都返 404（不泄漏权限细节）
		return ErrNotFound("找不到这辆车")
	case errors.Is(err, bus.ErrDissolved):
		return ErrConflict(CodeConflict, "车已解散")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// handleBusPull 拉号入车 —— 跟 POST /me/pull 一致，只是 target_group = bus-<id>。
func (s *Server) handleBusPull(w http.ResponseWriter, r *http.Request) error {
	if s.decider == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal,
			"拉号服务暂未装配")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	// 校验乘客在这辆车里（防越权拉到别人车里）
	if _, err := s.buses.GetForPassenger(r.Context(), busID, p.ID); err != nil {
		return ErrNotFound("找不到这辆车")
	}

	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req pullRequest
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	if req.Count < 1 {
		return ErrBadRequest("count 必须 ≥ 1")
	}

	key := r.Header.Get("X-Idempotency-Key")
	if key == "" {
		return newFail(http.StatusBadRequest, CodeBadIdempotencyKey,
			"拉号必须带 X-Idempotency-Key（32 位十六进制）")
	}

	hit, err := ensureIdempotencyRecord(r.Context(), s.db, p.ID, r.Method, r.URL.Path, key, body)
	if err != nil {
		return err
	}
	switch hit.status {
	case idemReplay:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(hit.responseStatus)
		_, _ = w.Write(hit.responseBody)
		return nil
	case idemConflict:
		return ErrIdempotencyConflict()
	}

	// strategy 校验（读余额 + 用量）· bus 级上限跟全局取更严的（AND · §8.27）
	bal, err := s.wallets.Get(r.Context(), p.ID)
	if err != nil {
		return err
	}
	used, err := s.wallets.TodayUsage(r.Context(), p.ID)
	if err != nil {
		return err
	}
	// 1a 阶段暂不接车级 max_unit_price（表里有列，还没端点配置它） · Iss #12 补
	if _, err := s.strategies.CanPull(r.Context(), p.ID, strategy.CheckInput{
		BusID:   busID,
		Count:   req.Count,
		Balance: bal.Balance,
		Used:    strategy.Usage{Rounds: used.Rounds, Spend: used.Spend},
	}); err != nil {
		if fail := translateStrategyErr(err); fail != nil {
			return fail
		}
		return err
	}

	result, err := s.decider.Pull(r.Context(), decider.PullInput{
		PassengerID:         p.ID,
		BusID:               busID,
		Count:               req.Count,
		Zone:                providers.Zone(req.Zone),
		IdempotencyRecordID: hit.recordID,
	})
	if err != nil {
		if fail := translateDeciderErr(err); fail != nil {
			return fail
		}
		return err
	}

	resp := pullResponse{
		PullRoundID:      result.PullRoundID,
		VendorID:         result.VendorID,
		Purchased:        result.Purchased,
		CredentialIDs:    result.CredentialIDs,
		UnitPrice:        result.UnitPrice,
		ServiceFee:       result.ServiceFee,
		TotalDebit:       result.TotalDebit,
		BalanceRemaining: result.BalanceRemaining,
	}
	respBody, _ := json.Marshal(resp)
	_ = saveIdempotentResponse(r.Context(), s.db, hit.recordID, http.StatusOK, respBody)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
	return nil
}

// buildBusResponse 把 bus.Bus 拼成对外响应，含成员列表。
func (s *Server) buildBusResponse(r *http.Request, b *bus.Bus) (busResponse, error) {
	members, err := s.buses.Members(r.Context(), b.ID)
	if err != nil {
		return busResponse{}, err
	}
	mResp := make([]memberResp, 0, len(members))
	for _, m := range members {
		username, balance, err := s.passengerBriefFor(r, m.PassengerID)
		if err != nil {
			return busResponse{}, err
		}
		var lastSkipped *string
		mResp = append(mResp, memberResp{
			PassengerID: m.PassengerID, Username: username,
			Role:     m.Role,
			JoinedAt: m.JoinedAt.Format("2006-01-02T15:04:05.000Z"),
			SharePct: m.SharePct, Balance: balance,
			Status: m.Status, SkippedCount: 0, LastSkippedAt: lastSkipped,
		})
	}
	var invite *string
	if b.InviteCode != "" {
		c := b.InviteCode
		invite = &c
	}
	return busResponse{
		ID: b.ID, Name: b.Name, Kind: string(b.Kind), Status: string(b.Status),
		MemberCount: len(members), InviteCode: invite,
		CreatedAt: b.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
		Members:   mResp,
	}, nil
}

// passengerBriefFor 拿一个乘客的 username + 钱包余额（拼进 BusMember）。
func (s *Server) passengerBriefFor(r *http.Request, passengerID string) (string, int64, error) {
	p, err := s.passengers.ByID(r.Context(), passengerID)
	if err != nil {
		return "", 0, err
	}
	bal, err := s.wallets.Get(r.Context(), passengerID)
	if err != nil {
		return "", 0, err
	}
	return p.Username, bal.Balance, nil
}

// 兜底：io / json / errors 全用到了
var _ = io.EOF
var _ = json.Marshal
var _ = errors.New
