package strategy

// Effective · 策略优先级铁律的**唯一入口**(15-scheduling §4.3.4)。
//
// 所有决策路径(手动拉号 / 建车向导 / bus.Scheduler / deathwatch.RefillTick /
// webhook 扫号 / stockwatch fire / decider 内部兜底 / record 单独拉)都必须调
// 这个函数拿 EffectiveStrategy · **禁止**在外面自己拼字段 · 见 §4.3.4 底部规则。
//
// **两类字段规则不同**(§4.3.2):
//
//	类① 硬上限(取 min · request 不能放宽) · MaxUnitPrice / DailyRoundLimit / DailySpendLimit
//	类② 覆盖  (后者盖前者)             · PerRoundCount / PreferredVendor / Zone /
//	                                     AutoRefillEnabled / RefillWatermark / RefillMinCount
//
// **优先级链**(§4.3.1):
//
//	request override > 车级 > 全局 > 系统默认
//
// **依赖注入**(EffectiveDeps 接口)避免 strategy 包 import bus 造成循环 ·
// 装配层实现 · 传乘客全局策略 / 车级策略 / 系统默认几个数字。

import (
	"context"
	"fmt"
)

// EffectiveStrategy · Effective() 输出 · 已经算好优先级的最终值。
//
// **调用方不能再二次拼** —— 拿到这份直接用。
type EffectiveStrategy struct {
	// MaxUnitPrice · 硬上限 · min(request, 车级, 全局) · 0 = 不限
	MaxUnitPrice int64

	// DailyRoundLimit · 硬上限 · 只在全局(车级 deprecated · §4.1) · 0 = 不限
	DailyRoundLimit int
	// DailySpendLimit · 硬上限 · 只在全局(车级 deprecated · §4.1) · 0 = 不限
	DailySpendLimit int64

	// PerRoundCount · 覆盖字段 · request → 车级 → 全局 → config.pull.DefaultCount ·
	// 至少 1(zero 不合法 · 上游 vendor 也拒 count<1)
	PerRoundCount int

	// PreferredVendor · 覆盖字段 · request → 车级 → 全局 · 空 = AutoPick
	PreferredVendor string

	// Zone · 覆盖字段 · request → 车级 → 全局 · 默认 "auto"
	Zone string

	// AutoRefillEnabled · 覆盖字段 · 车级 → 全局(§4.3.2b 方案 A 落地后)
	AutoRefillEnabled bool

	// RefillWatermark · 覆盖字段 · 车级 → 全局(§4.3.2b 方案 A 落地后)
	RefillWatermark int

	// RefillMinCount · 车级 · nil = 按 gap 补齐差额(RefillWatermark - alive_total · Step 5 语义)
	RefillMinCount *int

	// ── 全局跨车调度护栏(migration 040 · CLAUDE §1.5)· 只对自动补车链路生效 ──
	// **注意**:这三字段只在**自动触发路径**判(webhook / probe / scheduler /
	// deathwatch.RefillTick / stockwatch.Notify)· 手动拉号(RequestOverride!=nil)**不受此约束**。
	//
	// AutoRefillDailyBudget · 所有 auto 车加起来一天最多花 N microunit · 0 = 不限
	AutoRefillDailyBudget int64
	// AutoRefillMinWalletReserve · 钱包低于此值时所有 auto 车暂停 · 0 = 不设保护线
	AutoRefillMinWalletReserve int64
	// AutoRefillVendorAllowlist · 自动补车只允许的 vendor id 列表 · 空 = 不限
	AutoRefillVendorAllowlist []string
}

// RequestOverride · 一次性动作参数 · 手动拉号 / 建车向导首次拉号带的字段。
//
// **规则**(§4.3.2 类①类②):
//   - 硬上限字段(MaxUnitPrice)· request 只能**收紧** · 不能放宽 · 传比全局宽的值会被忽略
//   - 覆盖字段(Count/Vendor/Zone)· request 有值就用 · 0/false 也算合法覆盖
//
// **别混 `Count` 和 `PerRoundCount`**(§4.3.2d):
//   - request.Count 是"这一次拉几个" · 最高优先级 · 有值就用 · 不用降级
//   - PerRoundCount 是"默认批量偏好" · 落库 · 只在 request.Count 为 nil 时才走三层链
//
// **自动触发传 nil**(webhook/scheduler/deathwatch/probe 等 · 5 触发源均无 request)。
type RequestOverride struct {
	// Count · 本次拉号数量(手动动作 · §4.3.2d) · nil = 无一次性数量 · 走 PerRoundCount 链
	Count *int
	// Vendor · 本次指定 vendor · nil = 无一次性偏好 · 走 PreferredVendor 链
	Vendor *string
	// Zone · 本次指定 zone · nil = 无一次性偏好 · 走 Zone 链
	Zone *string
	// MaxUnitPrice · 本次硬上限(microunit) · nil = 无一次性硬上限 · 走全局∧车级
	// 只能**收紧** · 传比全局宽的值会被静默忽略(见 §4.3.2 类①"请求约束更严"节)
	MaxUnitPrice *int64
}

// EffectiveDeps · Effective() 需要读的四类数据。
//
// **抽成接口是为了避免 strategy → bus 硬依赖**(bus 反过来不 import strategy · 但
// 让 strategy 里出现 bus.Strategy 类型也不合理 · 保 strategy 是底层数据包语义)。
// 装配层(main.go / api.Server)提供实现。
type EffectiveDeps interface {
	// GlobalGet · 读乘客全局策略默认(passenger_strategy_default 表) ·
	// 没存过返 Defaults(pid) 不是错(strategy.Store.Get 已实现)。
	GlobalGet(ctx context.Context, passengerID string) (Strategy, error)
	// BusGet · 读一辆车的车级策略 · nil = busID 为空 / 车不存在 / 无权访问 ·
	// 装配层判断 · 别在 Effective() 里再校验。
	BusGet(ctx context.Context, busID string) (*BusStrategy, error)
	// SystemDefaults · 系统默认值(config.pull.*) · 每次调用便宜返 · 不用缓存。
	SystemDefaults() SystemDefaults
}

// BusStrategy · strategy 包视角的车级策略字段(镜像 bus.Strategy · 只含 Effective
// 需要的字段)。装配层从 bus.Strategy 手工翻译过来 · 让 strategy 包不必 import bus。
//
// **指针语义严格对齐** bus.Strategy(§4.3.2b 方案 A):
//
//	nil  = 跟随全局默认
//	非 nil = 覆盖本车(包括显式 0 / false)
type BusStrategy struct {
	// AutoRefillEnabled / RefillWatermark · **纯车级** · NOT NULL DEFAULT 0
	// (migration 040 撤回 nullable · 见 docs/15-scheduling.md §4.3.2b)
	AutoRefillEnabled bool
	RefillWatermark   int
	// RefillMinCount 保持可空 · nil = 按 gap 补齐差额
	RefillMinCount *int
	// 以下覆盖字段仍走 nullable(nil = 跟随全局)
	PerRoundCount   *int
	MaxUnitPrice    *int64
	PreferredVendor *string
	// Zone · bus.Strategy 目前不存 zone(anon_zone 是撮合用)· 保留字段占位 · 常年 nil
	Zone *string
}

// SystemDefaults · config.pull.* 快照 · Effective() 只读几个字段。
type SystemDefaults struct {
	// PerRoundCount · config.pull.default_count · 建议 3
	PerRoundCount int
	// DefaultZone · 系统默认 zone · 恒 "auto"(乘客没配 · 车没配 · 全局没配时兜底)
	DefaultZone string
}

// Effective · 按 §4.3.1 优先级链算出 EffectiveStrategy。
//
// **参数说明**(§4.3.4):
//   - passengerID · 必填
//   - busID · 空 = record 单独拉 · 不查车级 · 直接走全局(§4.3.3 底部允许项)
//   - req · nil = 自动触发(无 request override · webhook/scheduler/deathwatch 等)
//
// **不做校验** —— busID 是否属于 passenger / 车是否已解散等由调用方保证(装配层
// 已经在 EffectiveDeps.BusGet 里判过 · 返 nil 表示"不适用车级")。
func Effective(ctx context.Context, deps EffectiveDeps, passengerID, busID string, req *RequestOverride) (EffectiveStrategy, error) {
	if deps == nil {
		return EffectiveStrategy{}, fmt.Errorf("strategy.Effective: deps 未装配")
	}
	if passengerID == "" {
		return EffectiveStrategy{}, fmt.Errorf("strategy.Effective: passengerID 必填")
	}

	global, err := deps.GlobalGet(ctx, passengerID)
	if err != nil {
		return EffectiveStrategy{}, fmt.Errorf("strategy.Effective: 读全局: %w", err)
	}

	var busSt *BusStrategy
	if busID != "" {
		busSt, err = deps.BusGet(ctx, busID)
		if err != nil {
			return EffectiveStrategy{}, fmt.Errorf("strategy.Effective: 读车级: %w", err)
		}
	}

	sys := deps.SystemDefaults()

	out := EffectiveStrategy{}

	// ── 类① 硬上限 · 取 min(全局, 车级, request) · 0 视为不限 ──

	// MaxUnitPrice · 三层取严
	globalMax := int64(0)
	if global.MaxUnitPrice != nil {
		globalMax = *global.MaxUnitPrice
	}
	var busMax int64
	if busSt != nil && busSt.MaxUnitPrice != nil {
		busMax = *busSt.MaxUnitPrice
	}
	var reqMax int64
	if req != nil && req.MaxUnitPrice != nil {
		reqMax = *req.MaxUnitPrice
	}
	out.MaxUnitPrice = stricter3(globalMax, busMax, reqMax)

	// DailyRoundLimit / DailySpendLimit · 只在全局(车级 deprecated · §4.1)
	if global.DailyRoundLimit != nil {
		out.DailyRoundLimit = *global.DailyRoundLimit
	}
	if global.DailySpendLimit != nil {
		out.DailySpendLimit = *global.DailySpendLimit
	}

	// ── 类② 覆盖字段 · 后者盖前者 ──

	// PerRoundCount · request → 车级 → 全局 → 系统默认
	out.PerRoundCount = sys.PerRoundCount
	if out.PerRoundCount < 1 {
		out.PerRoundCount = 1
	}
	if global.PerRoundCount >= 1 {
		out.PerRoundCount = global.PerRoundCount
	}
	if busSt != nil && busSt.PerRoundCount != nil && *busSt.PerRoundCount >= 1 {
		out.PerRoundCount = *busSt.PerRoundCount
	}
	if req != nil && req.Count != nil && *req.Count >= 1 {
		out.PerRoundCount = *req.Count
	}

	// PreferredVendor · request → 车级 → 全局 → 空(AutoPick)
	if global.PreferredVendor != nil {
		out.PreferredVendor = *global.PreferredVendor
	}
	if busSt != nil && busSt.PreferredVendor != nil {
		out.PreferredVendor = *busSt.PreferredVendor
	}
	if req != nil && req.Vendor != nil {
		out.PreferredVendor = *req.Vendor
	}

	// Zone · request → 车级 → 全局 → 系统默认 "auto"
	out.Zone = sys.DefaultZone
	if out.Zone == "" {
		out.Zone = ZoneAuto
	}
	if global.DefaultZone != "" {
		out.Zone = global.DefaultZone
	}
	if busSt != nil && busSt.Zone != nil && *busSt.Zone != "" {
		out.Zone = *busSt.Zone
	}
	if req != nil && req.Zone != nil && *req.Zone != "" {
		out.Zone = *req.Zone
	}

	// AutoRefillEnabled / RefillWatermark / RefillMinCount · **纯车级** · 无全局 fallback
	//
	// **1f-refactor(migration 040)撤回镜像模型**:auto/refill 三字段之前在 §4.3.2b
	// 走 "车级 → 全局" fallback · 造成两层字段镜像 · 用户误触。用户拍板改回:
	//   · 车级就是车级 · NOT NULL DEFAULT 0(migration 040)· 全局层的 default_* 只做
	//     建车 seed(handleCreateBus 里预填一次) · 不做运行时 fallback。
	//   · 全局层的调度护栏走 daily_budget / min_wallet_reserve / vendor_allowlist ·
	//     那是"跨车" · 不是"车级镜像"。
	//
	// 因此这里直接从车级取值 · 车级无表示或 busID 空则用零值(等价"关闭 / 阈值 0 / 按 gap")。
	if busSt != nil {
		out.AutoRefillEnabled = busSt.AutoRefillEnabled
		out.RefillWatermark = busSt.RefillWatermark
		if busSt.RefillMinCount != nil {
			v := *busSt.RefillMinCount
			out.RefillMinCount = &v
		}
	}

	// 全局跨车调度护栏(migration 040)· 3 字段透传给调用方 · 由 decideOutputByMode/桥判
	if global.AutoRefillDailyBudget != nil {
		out.AutoRefillDailyBudget = *global.AutoRefillDailyBudget
	}
	if global.AutoRefillMinWalletReserve != nil {
		out.AutoRefillMinWalletReserve = *global.AutoRefillMinWalletReserve
	}
	if len(global.AutoRefillVendorAllowlist) > 0 {
		out.AutoRefillVendorAllowlist = append([]string(nil), global.AutoRefillVendorAllowlist...)
	}

	return out, nil
}

// stricter3 · 三个上限取更严(min · §4.3.2 类①) · 0 视为不限 · 全 0 返 0(全不限)。
//
// **别用 stricter(canpull.go) 三次** —— 那版是指针语义(nil = 不限) · 这版是值
// 语义(0 = 不限)。命名不同避免混用。
//
// 语义:
//
//	stricter3(0, 0, 0) = 0        // 全不限
//	stricter3(20, 0, 0) = 20      // 只有一个 · 就是它
//	stricter3(20, 30, 0) = 20     // 两个非零 · 取小(严)
//	stricter3(20, 30, 10) = 10    // request(第三个)最严 · 用它
//	stricter3(20, 30, 40) = 20    // request 想放宽 · 无效 · 仍取 min(全局, 车级)
func stricter3(a, b, c int64) int64 {
	var out int64
	for _, v := range [3]int64{a, b, c} {
		if v <= 0 {
			continue
		}
		if out == 0 || v < out {
			out = v
		}
	}
	return out
}
