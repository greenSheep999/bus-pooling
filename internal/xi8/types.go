package xi8

// xi8 匿名 vendor 昵称到我方 vendor slug 的映射（实证 2026-08-11）·
// 详见 docs/decisions.md §11.11 · 5 家实锤 · **kiroappcc 不在 xi8**：
//
//	xi8 vendor_id  昵称    我方 slug
//	1              脆脆    kiroceo    · 价 101/70.70 匹配 · 无质保
//	2              羽毛    kiroappio  · 价 80.80/40.40 · warranty 10min
//	3              南南    kirodrop   · USD 计价 · 无质保
//	4              小鸡    kirooo     · 价 101/70.70 · 时间戳吻合
//	5              饭饭    kiro91     · warranty 10min · 时间戳吻合
//
// 未来 xi8 加新 vendor · 需在 vendorSlugByXi8ID 里补映射并说明证据。
var vendorSlugByXi8ID = map[int]string{
	1: "kiroceo",
	2: "kiroappio",
	3: "kirodrop",
	4: "kirooo",
	5: "kiro91",
}

// VendorSlugForXi8ID · 从 xi8.vendor_id 查我方 vendor slug · 未映射返 ""
func VendorSlugForXi8ID(id int) string {
	return vendorSlugByXi8ID[id]
}

// timeField · xi8 时间字段的三段式（`at` / `iso` / `ago_secs`）
type timeField struct {
	At      string `json:"at"`       // "2026-08-11 23:53:11"（北京墙钟 · 无 tz）
	ISO     string `json:"iso"`      // "2026-08-11T23:53:11+08:00"（用这个 · 有 tz）
	AgoSecs int    `json:"ago_secs"` // 相对现在的秒数（对账时用不上）
}

// RestockLogResp · /api/restock-log
type RestockLogResp struct {
	OK    bool          `json:"ok"`
	Count int           `json:"count"`
	Rows  []RestockRow  `json:"rows"`
}

type RestockRow struct {
	VendorID    int       `json:"vendor_id"`
	Name        string    `json:"name"`
	Region      string    `json:"region"`       // "us" / "eu"（xi8 简称 · 跟我方 us-east-1 / eu-central-1 差异注意）
	RegionLabel string    `json:"region_label"` // "美区" / "欧区"
	Stock       int       `json:"stock"`
	PrevStock   int       `json:"prev_stock"`
	Added       int       `json:"added"`
	At          timeField `json:"at"`
}

// VendorsResp · /api/vendors
type VendorsResp struct {
	OK      bool         `json:"ok"`
	Count   int          `json:"count"`
	Vendors []VendorInfo `json:"vendors"`
}

type VendorInfo struct {
	VendorID         int             `json:"vendor_id"`
	Name             string          `json:"name"`
	TotalStock       int             `json:"total_stock"`
	MaxPerOrder      int             `json:"max_per_order"`
	WarrantyMinutes  *int            `json:"warranty_minutes"`
	LastRestock      *timeField      `json:"last_restock"`
	RestockSource    string          `json:"restock_source"` // "webhook" / "推算"
	StockSynced      *timeField      `json:"stock_synced"`
	Quality          VendorQuality   `json:"quality"`
	Regions          []VendorRegion  `json:"regions"`
}

type VendorQuality struct {
	Survival string `json:"survival"` // 例如 "40 分钟-2 小时"
	Risk     string `json:"risk"`     // "低" / "中" / "高"
	Grade    string `json:"grade"`    // "推荐" / "观察" / "警告"
	Verdict  string `json:"verdict"`  // 一句总结
	Note     string `json:"note"`
}

type VendorRegion struct {
	Region        string     `json:"region"`
	RegionLabel   string     `json:"region_label"`
	Stock         int        `json:"stock"`
	PriceFen      int        `json:"price_fen"`  // 单价（分 · 人民币）
	Price         string     `json:"price"`      // "101.00"
	Buyable       bool       `json:"buyable"`
	Floating      bool       `json:"floating"`
	Blocked       bool       `json:"blocked"`
	BlockReason   string     `json:"block_reason"`
	LastRestock   *timeField `json:"last_restock"`
	RestockSource string     `json:"restock_source"`
	StockSynced   *timeField `json:"stock_synced"`
}

// SignalsResp · /api/signals
type SignalsResp struct {
	OK      bool     `json:"ok"`
	Count   int      `json:"count"`
	Signals []Signal `json:"signals"`
}

type Signal struct {
	VendorID       int       `json:"vendor_id"`
	Name           string    `json:"name"`
	Event          string    `json:"event"`
	VendorOrderID  string    `json:"vendor_order_id"`
	Regions        []string  `json:"regions"`
	RegionLabels   []string  `json:"region_labels"`
	Count          int       `json:"count"`
	At             timeField `json:"at"`
}
