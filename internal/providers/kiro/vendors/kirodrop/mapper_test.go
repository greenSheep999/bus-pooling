package kirodrop

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// **回归哨兵 · 2026-08-12**
//
// vendor 侧 stock 响应新形状带 `price:"7.35"` 字符串（USD）· 老 mapper 完全忽略 ·
// 导致 UnitPrice.Amount=0 · Prices 页显示为 0。修复：解析成 microunit USD Money。
//
// docs/18 §1.3 · Prober 落库时会经 vendor_pricing.credits_per_unit 换算成积分。
func TestParseUSDStringToMoney(t *testing.T) {
	cases := []struct {
		in      string
		wantAmt int64  // microunit
		wantCur string // Currency
	}{
		// 生产实测（kirodrop UI 2026-08-12 · $7.35）
		{"7.35", 7350000, string(providers.CurrencyUSD)},
		// 阶梯降价示例
		{"5.88", 5880000, string(providers.CurrencyUSD)},
		// 整数
		{"10", 10000000, string(providers.CurrencyUSD)},
		// 小数很短
		{"0.5", 500000, string(providers.CurrencyUSD)},
		// 6 位精确
		{"1.234567", 1234567, string(providers.CurrencyUSD)},
		// 超过 6 位 · 截断
		{"1.2345678", 1234567, string(providers.CurrencyUSD)},
		// 空 · 返零 Money（Currency 空）
		{"", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parseUSDStringToMoney(tc.in)
			if got.Amount != tc.wantAmt {
				t.Errorf("parseUSDStringToMoney(%q).Amount = %d · 期 %d", tc.in, got.Amount, tc.wantAmt)
			}
			if string(got.Currency) != tc.wantCur {
				t.Errorf("parseUSDStringToMoney(%q).Currency = %q · 期 %q", tc.in, got.Currency, tc.wantCur)
			}
		})
	}
}

// **回归哨兵 · 2026-08-13**
//
// 本 vendor 只给 `region:"us-east-1"` · **不给 zone 短名** · 老 mapper 在
// "无 zones 数组" 的兜底分支里完全没填 Zone → 侧表 zone 列落空 →
// PricedFor 按 zone 查匹配不到（docs/19-fields.md §3）。
//
// 修复：兜底分支和 zones[] 分支都过 providers.ZoneOf 归一。
func TestToStockSnapshot_ZoneNormalized(t *testing.T) {
	t.Run("无 zones 数组 · 从 region 归一", func(t *testing.T) {
		sr := &stockResp{
			Stock:  []byte("5"),
			Region: "us-east-1",
			Price:  "7.35",
		}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("应补一个默认 zone · got %d", len(snap.Zones))
		}
		z := snap.Zones[0]
		if z.Zone != providers.ZoneUS {
			t.Errorf("Zone = %q · want %q（老 bug 是落空）", z.Zone, providers.ZoneUS)
		}
		if z.Region != "us-east-1" {
			t.Errorf("Region = %q · 原文该保留", z.Region)
		}
		if z.UnitPrice.Amount != 7_350_000 || z.UnitPrice.Currency != providers.CurrencyUSD {
			t.Errorf("UnitPrice = %+v · want {7350000 USD}", z.UnitPrice)
		}
	})

	t.Run("eu region 也归一", func(t *testing.T) {
		sr := &stockResp{Stock: []byte("3"), Region: "eu-central-1", Price: "5.20"}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 || snap.Zones[0].Zone != providers.ZoneEU {
			t.Errorf("eu-central-1 应归一到 %q · got %+v", providers.ZoneEU, snap.Zones)
		}
	})

	t.Run("有 zones 数组 · 只给 region 时也归一", func(t *testing.T) {
		sr := &stockResp{
			Stock: []byte("0"),
			Zones: []zoneItem{{Region: "us-east-1", Available: 2, UnitPrice: 50}},
		}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("got %d zones", len(snap.Zones))
		}
		if snap.Zones[0].Zone != providers.ZoneUS {
			t.Errorf("zones[] 分支 Zone = %q · want %q", snap.Zones[0].Zone, providers.ZoneUS)
		}
	})
}

// **回归哨兵 · 2026-08-13**
//
// 本 vendor 的 new_keys_available 是**双区合并通知**（6 家里唯一）· 一次到货只推 1 条 ·
// 但 body 带两区完整信息 · 幂等键**按区分开**（purchase_order_ids_by_region）。
//
// 老 webhookPayload struct 是从别家抄的 · 这 7 个双区字段一个都没定义 ·
// 解出来 PerZone 恒 nil · 上层只能按顶级字段当一条处理 → 漏一个区。
func TestParse_DualZoneNotification(t *testing.T) {
	body := []byte(`{
	  "event": "new_keys_available",
	  "event_id": "evt-1",
	  "order_id": "ord-1",
	  "dispatch_id": "disp-1",
	  "purchase_order_id": "poid-top",
	  "notification_scope": "dual",
	  "region": "us-east-1",
	  "new_keys": 5,
	  "regions": ["us-east-1", "eu-central-1"],
	  "new_keys_by_region": {"us-east-1": 3, "eu-central-1": 2},
	  "purchase_order_ids_by_region": {"us-east-1": "poid-us", "eu-central-1": "poid-eu"},
	  "batch_ids_by_region": {"us-east-1": ["b1","b2"], "eu-central-1": ["b3"]},
	  "created_at": "2026-08-13T10:00:00Z"
	}`)

	a := &Adapter{}
	evt, err := a.Parse(body, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if evt.EventType != providers.EventNewKeysAvailable {
		t.Errorf("EventType = %q", evt.EventType)
	}
	if evt.NewKeys != 5 {
		t.Errorf("顶级 NewKeys = %d · want 5（合计）", evt.NewKeys)
	}
	// 顶级 zone 从 region 归一
	if evt.Zone != providers.ZoneUS {
		t.Errorf("Zone = %q · want us（从 region 归一）", evt.Zone)
	}

	// ★ PerZone 必须拆出两个区
	if len(evt.PerZone) != 2 {
		t.Fatalf("PerZone 应有 2 区 · 得 %d（老 bug 是 nil · 上层只处理一个区）", len(evt.PerZone))
	}
	// regions[] 顺序为准
	us, eu := evt.PerZone[0], evt.PerZone[1]
	if us.Zone != providers.ZoneUS || us.NewKeys != 3 || us.PurchaseOrderID != "poid-us" {
		t.Errorf("us 区解错 · got %+v", us)
	}
	if len(us.BatchIDs) != 2 {
		t.Errorf("us 区 BatchIDs = %v · want 2 个", us.BatchIDs)
	}
	if eu.Zone != providers.ZoneEU || eu.NewKeys != 2 || eu.PurchaseOrderID != "poid-eu" {
		t.Errorf("eu 区解错 · got %+v", eu)
	}
}

// 单区通知（无 *_by_region 字段）· PerZone 必须是 nil · 让上层走老路径
func TestParse_SingleZone_NoPerZone(t *testing.T) {
	body := []byte(`{
	  "event": "new_keys_available",
	  "event_id": "evt-2",
	  "purchase_order_id": "poid-single",
	  "region": "us-east-1",
	  "new_keys": 4
	}`)
	a := &Adapter{}
	evt, err := a.Parse(body, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if evt.PerZone != nil {
		t.Errorf("单区通知 PerZone 该是 nil · got %+v", evt.PerZone)
	}
	if evt.PurchaseOrderID != "poid-single" {
		t.Errorf("PurchaseOrderID = %q", evt.PurchaseOrderID)
	}
}

// 某区缺幂等键 · 跳过那区（宁可少一区也别用错的键去 Purchase）
func TestParse_DualZone_MissingKeySkipsZone(t *testing.T) {
	body := []byte(`{
	  "event": "new_keys_available",
	  "regions": ["us-east-1", "eu-central-1"],
	  "new_keys_by_region": {"us-east-1": 3, "eu-central-1": 2},
	  "purchase_order_ids_by_region": {"us-east-1": "poid-us"}
	}`)
	a := &Adapter{}
	evt, err := a.Parse(body, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(evt.PerZone) != 1 {
		t.Fatalf("缺幂等键的区该跳过 · 得 %d 区", len(evt.PerZone))
	}
	if evt.PerZone[0].Zone != providers.ZoneUS {
		t.Errorf("留下的该是 us · got %q", evt.PerZone[0].Zone)
	}
}

// 订单状态 partially_refunded · 要标出来（老代码完全没解析 status）
func TestToPurchaseResult_PartiallyRefunded(t *testing.T) {
	cases := []struct {
		name   string
		status string
		refund string
		want   bool
	}{
		{"完全成交", "completed", "", false},
		{"部分退款 · 靠 status", "partially_refunded", "", true},
		{"部分退款 · 靠退款金额", "completed", "12.50", true},
		{"全额退", "refunded", "50.00", true},
		{"老响应无 status 字段", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := &purchaseResp{
				Purchased:         3,
				TotalCredits:      30,
				Status:            c.status,
				RefundedAmountCNY: c.refund,
			}
			got := toPurchaseResult(pr, 5, false, nil)
			if got.PartiallyRefunded != c.want {
				t.Errorf("PartiallyRefunded = %v · want %v（status=%q refund=%q）",
					got.PartiallyRefunded, c.want, c.status, c.refund)
			}
		})
	}
}
