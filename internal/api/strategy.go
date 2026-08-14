package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// 全局策略 · GET/PUT /api/me/strategy（05-api-contract §7）。
//
// 前端入口是「设置 › 拉号偏好」。两块字段语义不同（decisions §8.27）：
// 上限**会真的拦下操作**，默认值只预填新车 —— 文案上也分开摆，
// 免得乘客以为改默认值就改了上限。

// strategyResponse 形状对齐 web/src/types/index.ts 的 GlobalStrategy。
// 那份 TS 是可执行契约，有出入以它为准（契约 §625）。
//
// 1f-B(15-scheduling §4.3.2b) · 加 default_auto_refill_enabled /
// default_refill_watermark / default_refill_min_count 三字段 · 作为：
//  1. 新车 seed(建车向导预填)
//  2. 车级 auto/refill 为 NULL 时的运行时 fallback(strategy.Effective)
//
// 三字段命名带 default_ 前缀 · 跟车级字段(auto_refill_enabled / refill_watermark /
// refill_min_count)分开 · 免得前端把全局值当成车级值用。
type strategyResponse struct {
	MaxUnitPrice             *int64            `json:"max_unit_price"`
	DailyRoundLimit          *int              `json:"daily_round_limit"`
	DailySpendLimit          *int64            `json:"daily_spend_limit"`
	PerRoundCount            int               `json:"per_round_count"`
	PreferredVendor          *string           `json:"preferred_vendor"`
	DefaultZone              string            `json:"default_zone"`
	DefaultAutoRefillEnabled bool              `json:"default_auto_refill_enabled"`
	DefaultRefillWatermark   int               `json:"default_refill_watermark"`
	DefaultRefillMinCount    *int              `json:"default_refill_min_count"`
	UsedToday                usedTodayResponse `json:"used_today"`
}

type usedTodayResponse struct {
	Rounds int   `json:"rounds"`
	Spend  int64 `json:"spend"`
}

func (s *Server) handleGetStrategy(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}

	st, err := s.strategies.Get(r.Context(), p.ID)
	if err != nil {
		return err
	}
	// used_today 从钱包的日计数来 —— 策略表不存用量，
	// 否则同一个数字有两处来源，迟早不一致
	used, err := s.wallets.TodayUsage(r.Context(), p.ID)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, buildStrategyResponse(st, used.Rounds, used.Spend))
	return nil
}

// buildStrategyResponse · 一处装填 strategyResponse · Get / Put 共用
// 避免 1f-B 加了 3 字段后 Get / Put 两处漏改。
func buildStrategyResponse(st strategy.Strategy, roundsToday int, spendToday int64) strategyResponse {
	return strategyResponse{
		MaxUnitPrice:             st.MaxUnitPrice,
		DailyRoundLimit:          st.DailyRoundLimit,
		DailySpendLimit:          st.DailySpendLimit,
		PerRoundCount:            st.PerRoundCount,
		PreferredVendor:          st.PreferredVendor,
		DefaultZone:              st.DefaultZone,
		DefaultAutoRefillEnabled: st.DefaultAutoRefillEnabled,
		DefaultRefillWatermark:   st.DefaultRefillWatermark,
		DefaultRefillMinCount:    st.DefaultRefillMinCount,
		UsedToday:                usedTodayResponse{Rounds: roundsToday, Spend: spendToday},
	}
}

// strategyPutRequest 用 json.RawMessage 而不是 *T，是为了分清三种情况：
//
//	字段没出现        → raw == nil        → 不动这个字段
//	字段是 null       → raw == "null"     → 显式设成"不限"
//	字段有值          → raw == "30000000" → 设成这个值
//
// 用 *T 的话前两种都是 nil，乘客设了上限就再也清不掉。
type strategyPutRequest struct {
	MaxUnitPrice    json.RawMessage `json:"max_unit_price"`
	DailyRoundLimit json.RawMessage `json:"daily_round_limit"`
	DailySpendLimit json.RawMessage `json:"daily_spend_limit"`
	PerRoundCount   json.RawMessage `json:"per_round_count"`
	PreferredVendor json.RawMessage `json:"preferred_vendor"`
	DefaultZone     json.RawMessage `json:"default_zone"`
	// 1f-B · auto/refill 全局默认 · auto/watermark 非空值(required-like) ·
	// min_count 允许 null(表"按 gap 补差额" · §4.3.2c 选项 X)
	DefaultAutoRefillEnabled json.RawMessage `json:"default_auto_refill_enabled"`
	DefaultRefillWatermark   json.RawMessage `json:"default_refill_watermark"`
	DefaultRefillMinCount    json.RawMessage `json:"default_refill_min_count"`
	// UsedToday 是只读的 —— 允许它出现（前端常把整个对象 PUT 回来）但忽略。
	// 不允许的话前端得先删字段，那是没必要的麻烦。
	UsedToday json.RawMessage `json:"used_today"`
}

func (s *Server) handlePutStrategy(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}

	var req strategyPutRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	patch, err := buildPatch(req)
	if err != nil {
		return err
	}

	st, err := s.strategies.Put(r.Context(), p.ID, patch)
	switch {
	case errors.Is(err, strategy.ErrBadZone):
		return ErrBadRequest("区域只能填 us、eu 或 auto")
	case errors.Is(err, strategy.ErrBadPerRoundCount):
		return ErrBadRequest("每轮数量要在 1-200 之间")
	case errors.Is(err, strategy.ErrNegativeLimit):
		// 想"不限"就传 null，别传负数 —— 负上限会让每次拉号都被拦且很难查
		return ErrBadRequest("上限不能是负数，想不限就留空")
	case err != nil:
		return err
	}

	used, err := s.wallets.TodayUsage(r.Context(), p.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, buildStrategyResponse(st, used.Rounds, used.Spend))
	return nil
}

func buildPatch(req strategyPutRequest) (strategy.Patch, error) {
	var p strategy.Patch

	maxUnitPrice, err := nullableField[int64]("max_unit_price", req.MaxUnitPrice)
	if err != nil {
		return p, err
	}
	p.MaxUnitPrice = maxUnitPrice

	roundLimit, err := nullableField[int]("daily_round_limit", req.DailyRoundLimit)
	if err != nil {
		return p, err
	}
	p.DailyRoundLimit = roundLimit

	spendLimit, err := nullableField[int64]("daily_spend_limit", req.DailySpendLimit)
	if err != nil {
		return p, err
	}
	p.DailySpendLimit = spendLimit

	vendor, err := nullableField[string]("preferred_vendor", req.PreferredVendor)
	if err != nil {
		return p, err
	}
	p.PreferredVendor = vendor

	perRound, err := requiredField[int]("per_round_count", req.PerRoundCount)
	if err != nil {
		return p, err
	}
	p.PerRoundCount = perRound

	zone, err := requiredField[string]("default_zone", req.DefaultZone)
	if err != nil {
		return p, err
	}
	p.DefaultZone = zone

	// 1f-B · auto / watermark 是 bool/int(不接受 null · 想"关"就传 false / 0)
	// min_count 允许 null(§4.3.2c 选项 X · null = 按 gap 补差额)
	autoEnabled, err := requiredField[bool]("default_auto_refill_enabled", req.DefaultAutoRefillEnabled)
	if err != nil {
		return p, err
	}
	p.DefaultAutoRefillEnabled = autoEnabled

	watermark, err := requiredField[int]("default_refill_watermark", req.DefaultRefillWatermark)
	if err != nil {
		return p, err
	}
	p.DefaultRefillWatermark = watermark

	minCount, err := nullableField[int]("default_refill_min_count", req.DefaultRefillMinCount)
	if err != nil {
		return p, err
	}
	p.DefaultRefillMinCount = minCount

	return p, nil
}

// nullableField 解析一个「可以是 null」的字段。
//
// 返回 **T：nil = 字段没出现（别动）· 非 nil 且 *ret == nil = 显式 null（设成不限）
func nullableField[T any](name string, raw json.RawMessage) (**T, error) {
	if raw == nil {
		return nil, nil
	}
	if isJSONNull(raw) {
		var explicitNil *T
		return &explicitNil, nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, ErrBadRequest(fmt.Sprintf("%s 的值格式不对", name))
	}
	return ptrTo(&v), nil
}

// requiredField 解析一个**不接受 null** 的字段（TS 里是非空类型）。
//
// 显式传 null 当成请求错误而不是静默忽略 —— 静默忽略会让客户端以为改成功了。
func requiredField[T any](name string, raw json.RawMessage) (*T, error) {
	if raw == nil {
		return nil, nil
	}
	if isJSONNull(raw) {
		return nil, ErrBadRequest(fmt.Sprintf("%s 不能留空", name))
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, ErrBadRequest(fmt.Sprintf("%s 的值格式不对", name))
	}
	return &v, nil
}

func isJSONNull(raw json.RawMessage) bool {
	// RawMessage 保留原始字节，可能带空白
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case 'n':
			return string(trimJSONSpace(raw)) == "null"
		default:
			return false
		}
	}
	return false
}

func trimJSONSpace(raw json.RawMessage) json.RawMessage {
	start, end := 0, len(raw)
	for start < end && isSpace(raw[start]) {
		start++
	}
	for end > start && isSpace(raw[end-1]) {
		end--
	}
	return raw[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func ptrTo[T any](v *T) **T { return &v }

// failFromLimitError 把 strategy 的上限错误翻译成契约里的错误码。
//
// **这层必须做翻译**：strategy 的 LimitKind 是内部枚举，
// 直接透给用户就违反 CLAUDE.md §12.6（对外不出现内部术语）。
func failFromLimitError(err error) *Fail {
	var le *strategy.LimitError
	if !errors.As(err, &le) {
		return nil
	}
	switch le.Kind {
	case strategy.LimitUnitPrice:
		return ErrPriceOverCap(le.Cap, le.Current)
	case strategy.LimitDailyRound:
		return ErrDailyLimitReached("今天的拉号次数已经用完了，明天再来或去「拉号偏好」调高上限", le.Limit, le.Used)
	case strategy.LimitDailySpend:
		return ErrDailyLimitReached("今天的消费额度已经用完了，明天再来或去「拉号偏好」调高上限", le.Limit, le.Used)
	}
	return nil
}
