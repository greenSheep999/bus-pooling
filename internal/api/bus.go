package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// Bus 相关端点。响应形状对齐 web/src/types/index.ts 的 Bus / BusMember。

type busResponse struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Kind        string        `json:"kind"`
	Status      string        `json:"status"`
	MemberCount int           `json:"member_count"`
	InviteCode  *string       `json:"invite_code"`
	CreatedAt   string        `json:"created_at"`
	Members     []memberResp  `json:"members"`
	Strategy    busStrategyDT `json:"strategy"`
	// 号池汇总 · 1a 先给 0（Iss #10 只做元数据，号统计在 §credentials 端点单独查）
	AliveCount         int   `json:"alive_count"`
	DeadCount          int   `json:"dead_count"`
	SpendToday         int64 `json:"spend_today"`
	AvgLifespanSeconds int64 `json:"avg_lifespan_seconds"`
}

// busStrategyDT 对齐前端 BusStrategy（TS 是权威 · CLAUDE.md §0）
//
// 1f-B(15-scheduling §4.3.2b 方案 A) · auto/refill 三字段前端 TS 已改成 `| null` ·
// null = 跟随全局默认 · 值(含 0 / false) = 覆盖本车。用指针接住"字段存在但值是 null"
// 的差别 —— json.Unmarshal 会把 `"auto_refill_enabled": null` 解成 *bool = nil ·
// 把 `"auto_refill_enabled": false` 解成 *bool = 指向 false。
type busStrategyDT struct {
	AutoRefillEnabled *bool   `json:"auto_refill_enabled"`
	RefillWatermark   *int    `json:"refill_watermark"`
	RefillMinCount    *int    `json:"refill_min_count"`
	PerRoundCount     *int    `json:"per_round_count"`
	MaxUnitPrice      *int64  `json:"max_unit_price"`
	DailyRoundLimit   *int    `json:"daily_round_limit"`
	DailySpendLimit   *int64  `json:"daily_spend_limit"`
	PreferredVendor   *string `json:"preferred_vendor"`
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
	// Kind 可选 · 空 = single（1a: single · 1c: anon · 2a: team）
	Kind string `json:"kind"`
	// Strategy 建车时的初始策略（前端已收集 · TS BusStrategy 形状）
	Strategy *busStrategyDT `json:"strategy"`
	// InviteCodeHint team 车专用（1a 忽略）
	InviteCodeHint string `json:"invite_code_hint"`
	// anon 专用（1c-1）
	MaxMembers       int    `json:"max_members,omitempty"`
	AnonZone         string `json:"anon_zone,omitempty"`
	AnonMaxUnitPrice int64  `json:"anon_max_unit_price,omitempty"` // microunit
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

	in := bus.CreateInput{
		Name:             req.Name,
		Kind:             kind,
		CreatorID:        p.ID,
		MaxMembers:       req.MaxMembers,
		AnonZone:         req.AnonZone,
		AnonMaxUnitPrice: req.AnonMaxUnitPrice,
	}
	if req.Strategy != nil {
		in.Strategy = &bus.Strategy{
			AutoRefillEnabled: req.Strategy.AutoRefillEnabled,
			RefillWatermark:   req.Strategy.RefillWatermark,
			RefillMinCount:    req.Strategy.RefillMinCount,
			PerRoundCount:     req.Strategy.PerRoundCount,
			MaxUnitPrice:      req.Strategy.MaxUnitPrice,
			DailyRoundLimit:   req.Strategy.DailyRoundLimit,
			DailySpendLimit:   req.Strategy.DailySpendLimit,
			PreferredVendor:   req.Strategy.PreferredVendor,
		}
	}
	b, err := s.buses.Create(r.Context(), in)
	switch {
	case errors.Is(err, bus.ErrBadKind):
		return ErrBadRequest(err.Error())
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

// PUT /api/me/buses/{bus_id} · 改车名（前端 useRenameBus 只发 {name}）
type renameBusReq struct {
	Name string `json:"name"`
}

func (s *Server) handleUpdateBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	var req renameBusReq
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	err = s.buses.Rename(r.Context(), busID, p.ID, req.Name)
	switch {
	case errors.Is(err, bus.ErrNotFound), errors.Is(err, bus.ErrNotMember):
		return ErrNotFound("找不到这辆车")
	case errors.Is(err, bus.ErrDissolved):
		return ErrConflict(CodeConflict, "车已解散")
	case err != nil && err.Error() == "bus: 车名不能为空":
		return ErrBadRequest("车名不能为空")
	case err != nil && err.Error() == "bus: 车名不能超过 40 字":
		return ErrBadRequest("车名不能超过 40 字")
	case err != nil:
		return err
	}
	// 返回改名后的完整对象（跟前端 useRenameBus onSuccess 一致）
	b, err := s.buses.Get(r.Context(), busID)
	if err != nil {
		return err
	}
	resp, err := s.buildBusResponse(r, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// PUT /api/me/buses/{bus_id}/strategy · 改车级策略
func (s *Server) handleUpdateBusStrategy(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	var st busStrategyDT
	if err := decodeJSON(r, &st); err != nil {
		return err
	}
	err = s.buses.UpdateStrategy(r.Context(), busID, p.ID, bus.Strategy{
		AutoRefillEnabled: st.AutoRefillEnabled,
		RefillWatermark:   st.RefillWatermark,
		RefillMinCount:    st.RefillMinCount,
		PerRoundCount:     st.PerRoundCount,
		MaxUnitPrice:      st.MaxUnitPrice,
		DailyRoundLimit:   st.DailyRoundLimit,
		DailySpendLimit:   st.DailySpendLimit,
		PreferredVendor:   st.PreferredVendor,
	})
	switch {
	case errors.Is(err, bus.ErrNotFound), errors.Is(err, bus.ErrNotMember):
		return ErrNotFound("找不到这辆车")
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
	// 校验乘客在这辆车里(防越权拉到别人车里) · 车级策略走 Effective 里的 BusGet ·
	// 这里只判归属 · 车级字段读取由装配层完成(§4.3.4)。
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
	if req.VendorID == "auto" {
		req.VendorID = ""
	}
	if req.Zone == "auto" {
		req.Zone = ""
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

	// 1f-C · 策略优先级铁律 · 所有策略读取走 strategy.Effective(§4.3.4) ·
	// 别自己拼字段(preferred_vendor 三层降级 / max_unit_price 取严等)。
	//
	// request override · 手动拉号带的一次性字段(§4.3.2d):
	//   - count · 手动动作参数 · 有值就用(最高优先级 · 不走 PerRoundCount 链)
	//   - vendor · req.VendorID 有值(空串已在上面清成 "")
	//   - zone · req.Zone 有值(空串已在上面清成 "")
	//   - 手动拉号不带 max_unit_price · 收紧场景要显式带 · 见 §4.3.2 类①
	reqOverride := buildManualPullOverride(req)
	eff, err := s.effective(r.Context(), p.ID, busID, reqOverride)
	if err != nil {
		return err
	}
	// 保留 canpull 硬护栏(余额 / daily_round / daily_spend / 单价上限)校验 ·
	// **护栏值来自 EffectiveStrategy** · 别再从 bus.Strategy 抽字段。
	bal, err := s.wallets.Get(r.Context(), p.ID)
	if err != nil {
		return err
	}
	used, err := s.wallets.TodayUsage(r.Context(), p.ID)
	if err != nil {
		return err
	}
	_, err = s.strategies.CanPull(r.Context(), p.ID, strategy.CheckInput{
		BusID:           busID,
		Count:           req.Count,
		Balance:         bal.Balance,
		Used:            strategy.Usage{Rounds: used.Rounds, Spend: used.Spend},
		BusMaxUnitPrice: nilIfZeroInt64(eff.MaxUnitPrice),
	})
	if err != nil {
		if fail := translateStrategyErr(err); fail != nil {
			return fail
		}
		return err
	}

	// **P3 · preferred_vendor 进下单** · vendor 已由 Effective 按优先级挑好 ·
	// eff.PreferredVendor 空 = AutoPick(decider 内 picker 兜底)。
	vendorID := eff.PreferredVendor
	// zone · request 已在 Effective 里处理 · 但注意 "auto" 在这里等价空(见上面 322-323)
	zoneOut := eff.Zone
	if zoneOut == strategy.ZoneAuto {
		zoneOut = ""
	}
	result, err := s.decider.Pull(r.Context(), decider.PullInput{
		PassengerID:         p.ID,
		BusID:               busID,
		Count:               req.Count,
		Zone:                providers.Zone(zoneOut),
		VendorID:            providers.VendorID(vendorID),
		IdempotencyRecordID: hit.recordID,
		// 生效上限 · 由 Effective 取严得到 · 0 = 不限
		MaxUnitPrice: eff.MaxUnitPrice,
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

// matchAnonReq · POST /api/me/buses/anon/match 请求
type matchAnonReq struct {
	Zone         string `json:"zone,omitempty"`
	MaxUnitPrice int64  `json:"max_unit_price,omitempty"` // microunit
	// AutoJoin=true 时·撮合到就直接加成员（一次调用完事）· 默认 false 让前端确认
	AutoJoin bool `json:"auto_join,omitempty"`
}

// matchAnonResp · 撮合成功返 matched=true + bus 详情 · 未找到 matched=false
type matchAnonResp struct {
	Matched bool         `json:"matched"`
	Bus     *busResponse `json:"bus,omitempty"`
	// Reason · 未匹配时说明（no_match / already_member / …）
	Reason string `json:"reason,omitempty"`
}

// handleMatchAnonBus · POST /api/me/buses/anon/match
//
// 1c-1 · 匿名撮合骨架：找一辆已存在的活跃 anon bus 匹配·成功可选自动 join。
//
// **未启用集单窗口**：真意图池 + 定时合流是 1c-2 · 现阶段前端拿到 bus_id 后
// 走原有的 POST /api/me/buses/{id}/pull 拉号（多人同 bus 各自触发·decider 各自算账）。
func (s *Server) handleMatchAnonBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	var req matchAnonReq
	body, err := readBody(r)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		if err := decodeStrict(body, &req); err != nil {
			return err
		}
	}

	b, err := s.buses.FindMatchable(r.Context(), bus.MatchOptions{
		PassengerID:  p.ID,
		Zone:         req.Zone,
		MaxUnitPrice: req.MaxUnitPrice,
	})
	switch {
	case errors.Is(err, bus.ErrNoMatch):
		writeJSON(w, http.StatusOK, matchAnonResp{Matched: false, Reason: "no_match"})
		return nil
	case err != nil:
		return err
	}

	// 可选自动 join
	if req.AutoJoin {
		if err := s.buses.Join(r.Context(), b.ID, p.ID); err != nil {
			switch {
			case errors.Is(err, bus.ErrAlreadyMember):
				// 幂等：已加入等价成功
			case errors.Is(err, bus.ErrBusFull):
				writeJSON(w, http.StatusOK, matchAnonResp{Matched: false, Reason: "bus_full"})
				return nil
			case errors.Is(err, bus.ErrDissolved):
				writeJSON(w, http.StatusOK, matchAnonResp{Matched: false, Reason: "dissolved"})
				return nil
			default:
				return err
			}
		}
		// 加入后重读（member list 变了）
		b, err = s.buses.Get(r.Context(), b.ID)
		if err != nil {
			return err
		}
	}

	resp, err := s.buildBusResponse(r, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, matchAnonResp{Matched: true, Bus: &resp})
	return nil
}

// handleJoinBus · POST /api/me/buses/{bus_id}/join · 显式加入一辆 anon bus
//
// 幂等：已成员返 200 + 现状（不算错·前端可重试）· 车满 409 · 已解散 410 · 车不是 anon 400。
func (s *Server) handleJoinBus(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if busID == "" {
		return ErrBadRequest("缺 bus_id")
	}

	if err := s.buses.Join(r.Context(), busID, p.ID); err != nil {
		switch {
		case errors.Is(err, bus.ErrAlreadyMember):
			// 幂等·下面走返 200 + 现状
		case errors.Is(err, bus.ErrNotFound):
			return ErrNotFound("找不到这辆车")
		case errors.Is(err, bus.ErrBadKind):
			return ErrBadRequest("这辆车不允许加入")
		case errors.Is(err, bus.ErrBusFull):
			return newFail(http.StatusConflict, "bus_full", "车已满")
		case errors.Is(err, bus.ErrDissolved):
			return newFail(http.StatusGone, "bus_dissolved", "车已解散")
		default:
			return err
		}
	}
	b, err := s.buses.Get(r.Context(), busID)
	if err != nil {
		return err
	}
	resp, err := s.buildBusResponse(r, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// buildBusResponse 把 bus.Bus 拼成对外响应，含成员列表。
func (s *Server) buildBusResponse(r *http.Request, b *bus.Bus) (busResponse, error) {
	// 1c 之前建的车没邀请码 · 读到就补（老数据自愈 · 不做 migration）
	// 补失败不阻塞响应 —— 邀请码不是车能不能用的前提
	if err := s.buses.EnsureInviteCode(r.Context(), b); err != nil {
		slog.Warn("补邀请码失败·不阻塞", "bus_id", b.ID, "err", err)
	}
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
		Strategy: busStrategyDT{
			AutoRefillEnabled: b.Strategy.AutoRefillEnabled,
			RefillWatermark:   b.Strategy.RefillWatermark,
			RefillMinCount:    b.Strategy.RefillMinCount,
			PerRoundCount:     b.Strategy.PerRoundCount,
			MaxUnitPrice:      b.Strategy.MaxUnitPrice,
			DailyRoundLimit:   b.Strategy.DailyRoundLimit,
			DailySpendLimit:   b.Strategy.DailySpendLimit,
			PreferredVendor:   b.Strategy.PreferredVendor,
		},
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

// joinByInviteReq · POST /api/me/buses/join-by-invite 请求体
type joinByInviteReq struct {
	InviteCode string `json:"invite_code"`
}

// handleJoinByInvite · POST /api/me/buses/join-by-invite · 用邀请码加入 team bus。
//
// 语义：
//   - 邀请码无效 / 车已解散 → 404（不区分·避免枚举）
//   - 车满 409 · 已成员 200 幂等
func (s *Server) handleJoinByInvite(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	var req joinByInviteReq
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.InviteCode == "" {
		return ErrBadRequest("缺拼车码")
	}
	b, err := s.buses.JoinByInviteCode(r.Context(), req.InviteCode, p.ID)
	switch {
	case errors.Is(err, bus.ErrInvalidInvite):
		return ErrNotFound("拼车码无效或车已解散")
	case errors.Is(err, bus.ErrAlreadyMember):
		// 幂等 · 返当前车状态
		found, ferr := s.buses.FindByInviteCode(r.Context(), req.InviteCode)
		if ferr != nil {
			return ferr
		}
		b = found
	case errors.Is(err, bus.ErrBusFull):
		return newFail(http.StatusConflict, "bus_full", "车已满")
	case errors.Is(err, bus.ErrDissolved):
		return ErrNotFound("拼车码无效或车已解散")
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

// handleRemoveMember · DELETE /api/me/buses/{bus_id}/members/{pid}
//
// 车主移除成员 · 剩下的人 share_pct 重算（decisions §8.18）。
// **不退**被移除者已花的钱（提前下车不退）· 他的历史轮次质保退款照旧（§8.35 #19）。
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) error {
	caller, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	targetID := r.PathValue("pid")
	if busID == "" || targetID == "" {
		return ErrBadRequest("缺 bus_id 或成员 id")
	}

	switch err := s.buses.RemoveMember(r.Context(), busID, caller.ID, targetID); {
	case errors.Is(err, bus.ErrNotFound):
		return ErrNotFound("找不到这辆车")
	case errors.Is(err, bus.ErrNotMember):
		return ErrNotFound("这个人不在车里")
	case errors.Is(err, bus.ErrNotOwner):
		return newFail(http.StatusForbidden, "not_owner", err.Error())
	case errors.Is(err, bus.ErrDissolved):
		return newFail(http.StatusGone, "bus_dissolved", "车已解散")
	case err != nil:
		return err
	}

	// 返回移除后的车（前端要刷新成员列表和新的分摊比例）
	b, err := s.buses.Get(r.Context(), busID)
	if err != nil {
		return err
	}
	resp, err := s.buildBusResponse(r, b)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// handleRegenInviteCode · POST /api/me/buses/{bus_id}/invite-code · owner 换邀请码。
//
// 权限：只 owner 可换 · 非 owner 返 403。
// 效果：旧邀请码立即失效（DB UPDATE 覆盖）。
func (s *Server) handleRegenInviteCode(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if busID == "" {
		return ErrBadRequest("缺 bus_id")
	}
	code, err := s.buses.RegenerateInviteCode(r.Context(), busID, p.ID)
	switch {
	case errors.Is(err, bus.ErrNotFound):
		return ErrNotFound("找不到这辆车")
	case errors.Is(err, bus.ErrNotOwner):
		return newFail(http.StatusForbidden, "not_owner", "只有车主能换拼车码")
	case errors.Is(err, bus.ErrBadKind):
		return ErrBadRequest("系统撮合的搭车池没有拼车码")
	case errors.Is(err, bus.ErrDissolved):
		return newFail(http.StatusGone, "bus_dissolved", "车已解散")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"invite_code": code})
	return nil
}

// 兜底：io / json / errors 全用到了
var _ = io.EOF
var _ = json.Marshal
var _ = errors.New
