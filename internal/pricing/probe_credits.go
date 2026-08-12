package pricing

// probe_credits · 读 vendor_probe.our_unit_credits（docs/18 §1.4 机制 A 的出口）
//
// **为什么读库而不是现算**（docs/18 §6）：换算在**入库那一刻**做完（Prober 落 our_unit_credits）·
// 下游一律读结果列 · 不再各自拿汇率反推。这样只有一处换算规则 · 对账时库里就是权威值。
//
// **schema 限制**：`vendor_probe` 没有 region 列 · `our_unit_credits` 是**首个 zone 的采样**
// （`sample_price_region` 记了是哪个 zone）· 所以这里只能按 vendor 查 · 拿不到"某 region 的价"。
// 多 region 差价大的 vendor 要精确到区 · 得先给 vendor_probe 加 region 维度（当前不值当）。

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ProbeCredits · vendor_probe 的积分读取器
type ProbeCredits struct{ db *sql.DB }

func NewProbeCredits(db *sql.DB) *ProbeCredits { return &ProbeCredits{db: db} }

// LatestCredits · 该 vendor 最近一条有效积分单价（microunit）+ 探测时刻。
//
// found=false 的两种情况（调用方自己决定怎么兜）：
//   - 从没探到过（刚接入 / 长期断线）
//   - 探到的都是 0（vendor 一直缺货 · 没单价可采）
func (p *ProbeCredits) LatestCredits(ctx context.Context, vendorID string) (credits int64, probedAt time.Time, found bool) {
	if p == nil || p.db == nil {
		return 0, time.Time{}, false
	}
	var c sql.NullInt64
	var at string
	err := p.db.QueryRowContext(ctx,
		`SELECT our_unit_credits, probed_at FROM vendor_probe
		  WHERE vendor_id = ? AND our_unit_credits IS NOT NULL AND our_unit_credits > 0
		  ORDER BY probed_at DESC LIMIT 1`, vendorID).Scan(&c, &at)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			// 查库出错跟"没数据"对调用方是同一件事（都得走兜底）· 不往上抛
			return 0, time.Time{}, false
		}
		return 0, time.Time{}, false
	}
	if !c.Valid || c.Int64 <= 0 {
		return 0, time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, at)
	return c.Int64, t, true
}
