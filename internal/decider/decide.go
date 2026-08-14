package decider

// 统一系统调度决策入口 · docs/15-scheduling.md §5
//
// **纯决策 · 不做副作用**:输入拉号触发 · 输出 {拒 / 下单 / 挂单} · 副作用由调用方触发。
//
// 谁调它:
//   - bus.Scheduler(水位巡检 5min)
//   - deathwatch.RefillTick(号死后是否补 1min)
//   - stockwatch.Notify(webhook/probe 唤醒挂单)
//   - 未来:usage(用量见底) / prebuy(付费抢号 · 待决策能力)
//
// 谁不调它:
//   - 手动拉号(api/bus.go:handleBusPull → strategy.CanPull → decider.Pull) — 现链路完整 · 不动
//
// 六步串行判据 · 详见 docs/15-scheduling.md §5.2。

import (
	"context"
	"errors"
	"time"
)

// Source · 决策器触发源
type Source string

const (
	SourceDeathRefill Source = "death_refill" // 号死后是否补
	SourceScheduler   Source = "scheduler"    // 5min 兜底巡检
	SourceWebhook     Source = "webhook"      // vendor 新号推送(第五刀)
	SourceProbe       Source = "probe"        // 探针推算(第五刀)
	SourceUsage       Source = "usage"        // 用量见底(未来)
)

// ErrSourceUnimplemented · 触发源未接入决策器时返
var ErrSourceUnimplemented = errors.New("decider: 触发源未接入 Decide")

// DecideInput · 决策器输入
//
// 调用方(bus.Scheduler / deathwatch)负责提前查好这些字段 · Decide 不读 DB。
type DecideInput struct {
	Source Source
	BusID  string

	// AliveByVendor · 车里活号数 按 vendor 分组
	//   key=vendor_id · value=alive count
	// 空 map 表示整车 alive=0
	AliveByVendor map[string]int

	// 用户策略(从 bus.Strategy + passenger_strategy_default 读)
	AutoRefillEnabled bool
	RefillWatermark   int   // 补到几个 · 0 = 未启用自动补
	RefillMinCount    int   // 每次至少补几个 · 0 视为按差额补
	BusMaxUnitPrice   int64 // microunit · 0 = 不限
	PassengerMaxPrice int64 // microunit · 0 = 不限
	PreferredVendor   string

	// 系统运营态
	//   Cool / Balance / Tight · 从 stockwatch.ModeMgr 读
	//   KillPulls · KILL_PULLS 哨兵是否激活
	//   Turbo · TURBO_ON 哨兵是否激活
	Mode      string
	KillPulls bool
	Turbo     bool

	// PricesByVendor · 各 vendor 当前单价 · 用于备胎判据的价格过滤
	//   key=vendor_id · value=(unit_price_microunit, freshness)
	// 若 vendor 单价未知或过期 · 键存在但价格为 0 表示"数据不新鲜"
	PricesByVendor map[string]VendorPriceSnapshot
}

// VendorPriceSnapshot · 单个 vendor 的价格快照
type VendorPriceSnapshot struct {
	UnitPriceMicro int64
	ObservedAt     time.Time
	// Stale · 数据是否已老于新鲜度窗口 · 调用方判定
	Stale bool
}

// DecideOutput · 决策器输出
//
// 三种输出:
//   - Verdict=VerdictReject · 附 RejectReason · 调用方 log 后不动作
//   - Verdict=VerdictPull   · 调用方调 decider.Pull(带 PullInput)
//   - Verdict=VerdictEnqueue · 调用方调 stockwatch.Enqueue(带 EnqueueParams)
type DecideOutput struct {
	Verdict      Verdict
	RejectReason string     // Verdict=Reject 时填
	PullInput    *PullInput // Verdict=Pull 时填
	// EnqueueParams · Verdict=Enqueue 时填(用 stockwatch.EnqueueParams)
	// 类型改成 any 避免 decider → stockwatch 硬依赖(装配层判类型)
	EnqueueParams any
}

// Verdict · 决策器输出类型
type Verdict int

const (
	VerdictReject  Verdict = iota // 拒
	VerdictPull                   // 下单 · 调 decider.Pull
	VerdictEnqueue                // 挂单 · 调 stockwatch.Enqueue
)

// Decide · 六步串行判据 · 见 docs/15-scheduling.md §5.2
//
// **纯函数**:所有依赖走 in · 不读 DB / 不调 vendor / 不改状态。副作用由调用方触发。
func Decide(ctx context.Context, in DecideInput) (DecideOutput, error) {
	// Step 1 · 系统闸门
	if in.KillPulls {
		return reject("kill_pulls · 全停"), nil
	}

	// Step 2 · 用户 auto 开关
	// manual 不进决策器(api/bus.go 直发 · 见文件顶注释)
	// death_refill / scheduler / webhook / probe / usage 都要过 auto 检查
	switch in.Source {
	case SourceDeathRefill, SourceScheduler, SourceWebhook, SourceProbe, SourceUsage:
		if !in.AutoRefillEnabled {
			return reject("auto_off · 用户关了自动补车"), nil
		}
	default:
		return reject("source_unknown · 未识别触发源"), ErrSourceUnimplemented
	}

	// Step 3 · 目标 + 多 vendor 备胎判据
	if in.RefillWatermark <= 0 {
		return reject("no_watermark · 未设水位(RefillWatermark<=0)"), nil
	}

	aliveTotal := 0
	for _, n := range in.AliveByVendor {
		aliveTotal += n
	}

	// 已达水位
	if aliveTotal >= in.RefillWatermark {
		return reject("at_watermark · 已达目标水位"), nil
	}

	// 备胎判据 · 有任一 vendor 撑得住就不动(数量够 AND 价格没超)
	maxPrice := effectiveMaxPrice(in.BusMaxUnitPrice, in.PassengerMaxPrice)
	batchMin := in.RefillMinCount
	if batchMin <= 0 {
		batchMin = 1
	}

	// 整车挂(Case A) · 直跳 Step 4 · Case A 强制规则
	if aliveTotal > 0 {
		for vid, alive := range in.AliveByVendor {
			if alive < batchMin {
				continue
			}
			// 数量够 · 判价格
			if !vendorPriceOK(in.PricesByVendor, vid, maxPrice) {
				continue
			}
			return reject("has_backup · 备胎 vendor 撑得住 · 等它也见底"), nil
		}
	}

	// Step 4 · mode × source → 决定 output
	verdict, reason := decideOutputByMode(in, aliveTotal == 0)
	if verdict == VerdictReject {
		return reject(reason), nil
	}

	// Step 5-6 · 参数解析 + 限额判定 · 交给调用方组装
	// 骨架版本:只返 Verdict · PullInput / EnqueueParams 后续补
	return DecideOutput{Verdict: verdict}, nil
}

// effectiveMaxPrice · 类① 硬上限取最严 · 0 视为不限
func effectiveMaxPrice(busMax, passengerMax int64) int64 {
	if busMax <= 0 && passengerMax <= 0 {
		return 0 // 都不限
	}
	if busMax <= 0 {
		return passengerMax
	}
	if passengerMax <= 0 {
		return busMax
	}
	if busMax < passengerMax {
		return busMax
	}
	return passengerMax
}

// vendorPriceOK · 该 vendor 当前单价 ≤ maxPrice(0=不限) · 数据 stale 视为不 OK(保守撑不住)
func vendorPriceOK(prices map[string]VendorPriceSnapshot, vendorID string, maxPrice int64) bool {
	if maxPrice <= 0 {
		// 用户没设上限 · 只要数据新鲜就 OK
		snap, ok := prices[vendorID]
		if !ok {
			return true // 没价格数据 · 保守放行(等 Pull 时兜底)
		}
		return !snap.Stale && snap.UnitPriceMicro > 0
	}
	snap, ok := prices[vendorID]
	if !ok {
		return false // 有价格上限但无数据 · 判撑不住
	}
	if snap.Stale {
		return false // 数据老 · 判撑不住(codex P0-A 修正)
	}
	if snap.UnitPriceMicro <= 0 || snap.UnitPriceMicro > maxPrice {
		return false
	}
	return true
}

// decideOutputByMode · Step 4 表 · 返 (verdict, reason if reject)
//
// Case A 整车挂 · 强制规则 · 不看 source
//
//	Cool → Pull(有货直拉)
//	Balance / Tight → Enqueue(紧俏时下单必 ErrNoStock)
//
// Case C 常规(alive>0 但撑不住) · 看 source × mode
func decideOutputByMode(in DecideInput, allDead bool) (Verdict, string) {
	// TURBO_ON · 无视 mode · 一律激进
	mode := in.Mode
	if in.Turbo {
		mode = "tight"
	}

	if allDead {
		// Case A 强制
		switch mode {
		case "cool":
			return VerdictPull, ""
		default: // balance / tight / 空
			return VerdictEnqueue, ""
		}
	}

	// Case C · 常规
	switch in.Source {
	case SourceDeathRefill, SourceScheduler:
		switch mode {
		case "cool", "balance":
			return VerdictPull, ""
		case "tight":
			return VerdictEnqueue, ""
		default:
			return VerdictPull, "" // 未知 mode 保守下单
		}
	case SourceWebhook:
		switch mode {
		case "cool":
			return VerdictReject, "mode_cool_no_webhook · cool 时不响应 webhook"
		case "balance", "tight":
			return VerdictPull, ""
		}
	case SourceProbe:
		switch mode {
		case "cool", "balance":
			return VerdictReject, "mode_no_probe · 只 tight 时响应探针"
		case "tight":
			return VerdictPull, ""
		}
	}

	return VerdictReject, "source_mode_unhandled · 未定义的 source×mode 组合"
}

func reject(reason string) DecideOutput {
	return DecideOutput{Verdict: VerdictReject, RejectReason: reason}
}
