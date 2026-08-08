package strategy

import (
	"context"
	"errors"
	"fmt"
)

// Intent 是「可以拉」的结论 + 约束，交给 decider 去真的拉（03-modules §7）。
//
// 本包**不选 vendor、不算价、不动号池** —— Intent 里的 Vendor 只是乘客的偏好，
// decider 仍然自己比价（偏好为 nil 时更是完全由它决定）。
type Intent struct {
	PassengerID string
	// BusID 为空 = 单独拉号（进 record group）· 非空 = 拉进那辆车
	BusID string
	// WantCount 本轮想拉几个号
	WantCount int
	// MaxUnitPrice 生效的单价上限（microunit）· nil = 不限。
	// 车级同名字段跟全局取**更严**的（AND · decisions §8.27），
	// 这里已经是取严之后的结果。
	MaxUnitPrice *int64
	// Zone us | eu | auto
	Zone string
	// Vendor 乘客指定的 vendor · nil = 让系统比价
	Vendor *string
}

// LimitError 是被硬上限拦下时的错误。
//
// 带上 Limit / Used / Cap / Current 是因为契约要求前端能提示「超了多少」
// （05-api-contract §7：`daily_limit_reached` 带 limit/used，
// `price_over_cap` 带 cap/current）。只给一句"超了"前端没法画进度条。
type LimitError struct {
	Kind LimitKind
	// Limit / Used 用于 daily_round / daily_spend
	Limit int64
	Used  int64
	// Cap / Current 用于 unit_price
	Cap     int64
	Current int64
}

type LimitKind string

const (
	LimitDailyRound LimitKind = "daily_round"
	LimitDailySpend LimitKind = "daily_spend"
	LimitUnitPrice  LimitKind = "unit_price"
)

func (e *LimitError) Error() string {
	switch e.Kind {
	case LimitUnitPrice:
		return fmt.Sprintf("strategy: 单价超上限（上限 %d，当前 %d）", e.Cap, e.Current)
	default:
		return fmt.Sprintf("strategy: 今日已达上限（%s：上限 %d，已用 %d）", e.Kind, e.Limit, e.Used)
	}
}

// ErrLimitReached 让上层可以只用 errors.Is 粗判「是被上限拦的」，
// 要细节再 errors.As 取 LimitError。
var ErrLimitReached = errors.New("strategy: 触发上限")

func (e *LimitError) Is(target error) bool { return target == ErrLimitReached }

// ErrBadCount 请求的数量不合法。
var ErrBadCount = errors.New("strategy: 数量非法")

// Usage 是今日已用量。由调用方从 wallet 取后传进来 ——
// **本包不 import wallet**，那会形成 strategy→wallet→? 的耦合，
// 而 CanPull 想要的只是两个数字。这样也让测试不用搭钱包。
type Usage struct {
	Rounds int
	Spend  int64
}

// CheckInput 是一次拉号前的校验输入。
type CheckInput struct {
	// BusID 为空 = 单独拉号（提取 key）。
	// **提取只受全局限额管** —— record group 没有车级限额（decisions §8.27）
	BusID string
	// Count 本轮想拉几个
	Count int
	// UnitPriceHint 预估单价（microunit）· 0 = 还不知道价（比价前）。
	// **判折后价**（用了优惠码就传折后的 · §8.27）
	UnitPriceHint int64
	// Balance 当前可用余额（microunit）
	Balance int64
	// Used 今日已用
	Used Usage
	// BusMaxUnitPrice 车级单价上限 · nil = 车没设。
	// 跟全局取**更严**的（AND）
	BusMaxUnitPrice *int64
}

// CanPull 判「当下能不能拉」，能就给出 Intent。
//
// 校验顺序是刻意的：**先判不花钱就能知道的**（数量 / 轮数 / 单价），
// 最后才判余额。这样乘客拿到的是最根本的那个原因 —— 比如同时余额不足又超轮数，
// 告诉他"今天拉满了"比"钱不够"有用（充了钱还是拉不动）。
func (s *Store) CanPull(ctx context.Context, passengerID string, in CheckInput) (*Intent, error) {
	if in.Count < 1 {
		return nil, fmt.Errorf("%w: count=%d", ErrBadCount, in.Count)
	}

	st, err := s.Get(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	return decide(st, passengerID, in)
}

// decide 是纯函数版的 CanPull —— 不碰 DB，方便直接测各种上限组合。
func decide(st Strategy, passengerID string, in CheckInput) (*Intent, error) {
	// ① 每日轮数 —— **1 轮 = 1 次拉号动作**，不管这轮拉几个号（CLAUDE.md §2）
	if st.DailyRoundLimit != nil {
		if in.Used.Rounds+1 > *st.DailyRoundLimit {
			return nil, &LimitError{
				Kind:  LimitDailyRound,
				Limit: int64(*st.DailyRoundLimit),
				Used:  int64(in.Used.Rounds),
			}
		}
	}

	// ② 单价上限 —— 全局跟车级取更严的（AND）。
	//
	// **提取（BusID 空）只受全局管** —— record group 没有车级限额（decisions §8.27）。
	// 这里主动忽略车级上限而不是信任调用方：调用方多传一个字段就会让提取被
	// 一个本不该管它的上限拦住，而那种 bug 从现象上看像"上限算错了"，极难查。
	busCap := in.BusMaxUnitPrice
	if in.BusID == "" {
		busCap = nil
	}
	priceCap := stricter(st.MaxUnitPrice, busCap)
	if priceCap != nil && in.UnitPriceHint > 0 && in.UnitPriceHint > *priceCap {
		return nil, &LimitError{
			Kind:    LimitUnitPrice,
			Cap:     *priceCap,
			Current: in.UnitPriceHint,
		}
	}

	// ③ 每日消费 —— 用预估总额判。比价前（hint=0）判不了，
	// 那时 estTotal=0 恒不超；decider 拿到真价后会再判一次。
	estTotal := in.UnitPriceHint * int64(in.Count)
	if st.DailySpendLimit != nil && estTotal > 0 {
		if in.Used.Spend+estTotal > *st.DailySpendLimit {
			return nil, &LimitError{
				Kind:  LimitDailySpend,
				Limit: *st.DailySpendLimit,
				Used:  in.Used.Spend,
			}
		}
	}

	// ④ 余额 —— 放最后，理由见 CanPull 的注释
	if estTotal > 0 && in.Balance < estTotal {
		return nil, ErrInsufficientBalance
	}

	zone := st.DefaultZone
	if zone == "" {
		zone = ZoneAuto
	}
	return &Intent{
		PassengerID:  passengerID,
		BusID:        in.BusID,
		WantCount:    in.Count,
		MaxUnitPrice: priceCap,
		Zone:         zone,
		Vendor:       st.PreferredVendor,
	}, nil
}

// ErrInsufficientBalance 余额不够这一轮的预估花费。
var ErrInsufficientBalance = errors.New("strategy: 余额不足")

// stricter 取两个上限里更严（更小）的那个。nil = 不限。
//
// 注意 **nil 不是 0** —— 一边没设时结果是另一边，不是"取 0 最严"。
func stricter(a, b *int64) *int64 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case *b < *a:
		return b
	default:
		return a
	}
}
