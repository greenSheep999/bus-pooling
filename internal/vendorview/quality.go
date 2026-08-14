package vendorview

import (
	"math"
	"time"
)

// VendorQuality · 单家 vendor 综合质量评估（对外输出 + 内部排序）
//
// 作用面（3 处消费者）：
//  1. Status 页排序（Score 降序）+ 卡片展示 Tags
//  2. AutoPick 后端打分（未来接入 · 现在保留原公式不动）
//  3. Buses / Extract 页 VendorTag 边上显示等级色（未来 sprint）
//
// **为什么标签制而不是 A/B/C 或分数**（`decisions §12.6`）：
//   - 数字看着技术 · 用户不知道 87 分对不对
//   - A/B/C 太笼统 · 一个字母吞掉所有维度
//   - 标签**多维叠加** · 一家能同时挂"高产 · 稳定 · 活跃" · 直观
//   - 用户视角只关心"这家能不能用" · 标签是决策线索 · 不是打分
//
// **Score 只用于内部排序 · 前端不显示这个数字**（`CLAUDE.md §0.1`）
type VendorQuality struct {
	// Score 综合分（0-100）· 内部用于排序 · **不透给前端**
	Score int `json:"-"`

	// Tags 用户可见的多标签 · 排序：正向标签在前 · 观察态在后
	Tags []QualityTag `json:"tags"`
}

// QualityTag 单个质量标签 · 前端按 kind 决定颜色和图标
type QualityTag struct {
	// Kind 标签种类 · 前端映射到色调（ok / brand / info / neutral / warn / danger）
	// - "stable"     · 24h uptime ≥ 95%              · 稳定（ok · 绿）
	// - "high-volume" · 窗口内批次 ≥ 20              · 高产（brand · 紫）
	// - "active"     · 最新一次 ≤ 24h 内             · 活跃（info · 蓝）
	// - "in-stock"   · 当下 stock=many               · 有货（ok · 绿）
	// - "out-of-stock" · 当下 stock=out              · 缺货（danger · 红）
	// - "warranty"   · has_warranty=true             · 保质（brand · 紫）
	// - "watching"   · 数据不足 / uptime<50% / 长期没开号 · 观察中（warn · 黄）
	Kind string `json:"kind"`
}

// qualityInput 算分需要的原始数据 · 从 VendorStatusRow 抽出来（不改 Row 内部字段）
type qualityInput struct {
	alive           bool
	uptime24hPct    *int
	stockBucket     string
	hasWarranty     bool
	dispatchBatches int       // 窗口内批次数
	lastDispatch    time.Time // 零值 = 从未
	dataSufficient  bool      // 有足够数据算分（uptime 样本 ≥10 或 dispatchBatches>0）
	now             time.Time
}

// computeQuality 综合分 + 多标签 · 权重定稿（`decisions §11.7`）：
//
//	Uptime    30% · 稳（挂了拉不到号）
//	Volume    35% · 有货（长期没开号名义存在但用不上）
//	Freshness 20% · 活跃（长期不发货 = 这家凉了）
//	Stock     15% · 当下能拉
//
// 各维度归一到 0-100 · 加权得 Score · 再据阈值挂标签。
//
// **数据不足**（`dataSufficient=false`）· 直接返回 `watching` 标签 · Score=0
// 排最后 · 不给正向暗示。
func computeQuality(in qualityInput) VendorQuality {
	// 数据不足 · 只挂 watching · 不打分
	if !in.dataSufficient || !in.alive {
		return VendorQuality{
			Score: 0,
			Tags:  []QualityTag{{Kind: "watching"}},
		}
	}

	// 1. Uptime 分（0-100）· 直接用 pct · 缺失当 50 中性值
	uptimeScore := 50.0
	if in.uptime24hPct != nil {
		uptimeScore = float64(*in.uptime24hPct)
	}

	// 2. Volume 分（0-100）· log 归一避免"批次多 10 倍分就爆炸"
	//    50 批 ≈ 100 分 · 20 批 ≈ 77 分 · 10 批 ≈ 60 分 · 5 批 ≈ 40 分
	volumeScore := 0.0
	if in.dispatchBatches > 0 {
		volumeScore = math.Min(100, math.Log2(float64(in.dispatchBatches)+1)*17.5)
	}

	// 3. Freshness 分（0-100）· 指数衰减 · 越久越低
	//    刚发 = 100 · 6h 前 = 87 · 24h 前 = 60 · 3d 前 = 22 · 7d 前 = 5
	freshScore := 0.0
	if !in.lastDispatch.IsZero() {
		hoursAgo := in.now.Sub(in.lastDispatch).Hours()
		if hoursAgo < 0 {
			hoursAgo = 0
		}
		freshScore = 100 * math.Exp(-hoursAgo/48)
	}

	// 4. Stock 分 · many=100 · low=50 · out=20 · unknown=30
	stockScore := 30.0
	switch in.stockBucket {
	case "many":
		stockScore = 100
	case "low":
		stockScore = 50
	case "out":
		stockScore = 20
	}

	score := uptimeScore*0.30 + volumeScore*0.35 + freshScore*0.20 + stockScore*0.15

	// 挂标签 · 阈值定稿（决策附在字段注释里）
	tags := make([]QualityTag, 0, 5)

	// 稳定 · uptime ≥ 95%
	if in.uptime24hPct != nil && *in.uptime24hPct >= 95 {
		tags = append(tags, QualityTag{Kind: "stable"})
	}

	// 高产 · 窗口内批次 ≥ 20
	if in.dispatchBatches >= 20 {
		tags = append(tags, QualityTag{Kind: "high-volume"})
	}

	// 活跃 · 最新一次 ≤ 24h
	if !in.lastDispatch.IsZero() && in.now.Sub(in.lastDispatch) <= 24*time.Hour {
		tags = append(tags, QualityTag{Kind: "active"})
	}

	// 有货 · 当下 stock=many
	if in.stockBucket == "many" {
		tags = append(tags, QualityTag{Kind: "in-stock"})
	}
	// 缺货 · 当下 stock=out · 用户视角一眼看到"这家买不到号"
	if in.stockBucket == "out" {
		tags = append(tags, QualityTag{Kind: "out-of-stock"})
	}

	// 保质
	if in.hasWarranty {
		tags = append(tags, QualityTag{Kind: "warranty"})
	}

	// 观察态兜底 · 挂不出任何正向标 或 uptime 极低
	if len(tags) == 0 ||
		(in.uptime24hPct != nil && *in.uptime24hPct < 50) {
		// 已有 uptime<50% 的话 · 观察态和已有标签共存（"高产但不稳"是重要信号）
		if len(tags) == 0 {
			tags = append(tags, QualityTag{Kind: "watching"})
		} else if in.uptime24hPct != nil && *in.uptime24hPct < 50 {
			tags = append(tags, QualityTag{Kind: "watching"})
		}
	}

	return VendorQuality{
		Score: int(score + 0.5),
		Tags:  tags,
	}
}
