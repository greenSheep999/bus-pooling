package kirodrop

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 编译期保证 *Adapter 满足 TimeDecayLister · backfiller 靠 runtime 断言接线。
var _ providers.TimeDecayLister = (*Adapter)(nil)

// decayRegions · reservation 逐区查 · 本 vendor 两区（goods_id 1=us / 2=eu）。
var decayRegions = []string{"us", "eu"}

// reservationResp · GET /api/v1/reservation 真实响应（2026-08-14 浏览器 session 实测）。
// 价格是字符串（"49.980000"）· schedule 时间无 tz（北京墙钟 · 同本 provider 其他 vendor）。
type reservationResp struct {
	Region       string `json:"region"`
	ExchangeRate string `json:"exchange_rate"`
	// ⚠️ 数值字段实测返浮点（interval_minutes=30.0）· 用 float64 收再转 int（int 直接解会炸）。
	TimedPricing struct {
		Enabled       bool    `json:"enabled"`
		Active        bool    `json:"active"`
		IntervalMin   float64 `json:"interval_minutes"`
		MaxReductions float64 `json:"max_reductions"`
		Applied       float64 `json:"reductions_applied"`
		StartTime     string  `json:"start_time"`
		Schedule      []struct {
			ReductionNumber float64 `json:"reduction_number"`
			EffectiveAt     string  `json:"effective_at"`
			UnitPriceCNY    string  `json:"unit_price_cny"`
			UnitPriceUSD    string  `json:"unit_price_usd"`
		} `json:"schedule"`
	} `json:"timed_pricing"`
}

// ListTimeDecay · 逐区拉 /api/v1/reservation 的时间降价 schedule → TieredPricing。
//
// 鉴权：只认 Authorization: Bearer <SessionToken>（网页登录 · 带图形验证码 · 人工 seed）。
//   - SessionToken 空 → 返 (nil, nil)：未配置 · backfiller 静默跳过 · 不清旧值。
//   - token 过期（401）→ 返 error：backfiller 记 WARN 提示重新 seed · 保留上次落库值。
//
// 现价链（/api/me/stock · api_key）不依赖本方法 · token 挂了不影响探活/现价。
func (a *Adapter) ListTimeDecay(ctx context.Context) ([]providers.TieredPricing, error) {
	if strings.TrimSpace(a.cfg.SessionToken) == "" {
		return nil, nil // 未配置 token · 跳过
	}
	out := make([]providers.TieredPricing, 0, len(decayRegions))
	for _, zone := range decayRegions {
		tp, err := a.reservationTiers(ctx, zone)
		if err != nil {
			return nil, err
		}
		if tp != nil {
			out = append(out, *tp)
		}
	}
	return out, nil
}

func (a *Adapter) reservationTiers(ctx context.Context, zone string) (*providers.TieredPricing, error) {
	path := "/api/v1/reservation?quantity=1&region=" + zone
	req, err := a.newBearerReq(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("kirodrop: reservation %s: 401 · session token 过期或无效 · 需重新 seed", zone)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirodrop: reservation %s: http %d", zone, resp.StatusCode)
	}
	var r reservationResp
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return nil, fmt.Errorf("kirodrop: reservation %s 解析: %w", zone, err)
	}
	if !r.TimedPricing.Enabled || len(r.TimedPricing.Schedule) == 0 {
		return nil, nil // 该区没开降价 · 无 schedule
	}

	tp := &providers.TieredPricing{
		Region:        zone,
		Enabled:       r.TimedPricing.Enabled,
		Active:        r.TimedPricing.Active,
		IntervalMin:   int(r.TimedPricing.IntervalMin),
		MaxReductions: int(r.TimedPricing.MaxReductions),
		Applied:       int(r.TimedPricing.Applied),
		StartAt:       parseDecayTime(r.TimedPricing.StartTime),
	}
	for _, sc := range r.TimedPricing.Schedule {
		tp.Schedule = append(tp.Schedule, providers.TierSchedule{
			Index:            int(sc.ReductionNumber),
			EffectiveAt:      parseDecayTime(sc.EffectiveAt),
			UnitPriceCredits: priceToMicro(sc.UnitPriceCNY), // 1 积分 ≡ 1 CNY
			UnitPriceUSDRaw:  priceToMicro(sc.UnitPriceUSD),
		})
	}
	return tp, nil
}

// newBearerReq · /api/v1/* 独家端点专用 · 用 Bearer session token（不带 X-API-Key）。
func (a *Adapter) newBearerReq(ctx context.Context, method, path string) (*http.Request, error) {
	u := strings.TrimRight(a.cfg.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, fmt.Errorf("kirodrop: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.SessionToken)
	return req, nil
}

// decayTimeLayout · schedule 时间无 tz（"2026-08-12T15:20:27.584046"）· 按北京墙钟解释。
const decayTimeLayout = "2006-01-02T15:04:05.999999"

var decayTZ = time.FixedZone("CST", 8*3600)

func parseDecayTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// 无 tz · 北京墙钟 → UTC（同本 provider 其他 vendor 的时区处理）
	if t, err := time.ParseInLocation(decayTimeLayout, s, decayTZ); err == nil {
		return t.UTC()
	}
	// 带 tz 的 RFC3339 兜底
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// priceToMicro · vendor 价格字符串（"49.980000" / "7.35"）→ microunit（× 1e6 · 四舍五入）。
func priceToMicro(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 1_000_000))
}
