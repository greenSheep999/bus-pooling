package kiroappcc

// **回归哨兵 · 2026-08-13 · 生产实测发现**
//
// 本家 webhook 从接通起 100% 丢失（实测一天 21+ 条全废）。链路：
//
//	vendor 推 → Caddy 200 → HMAC 验签过 → Parse() → **字段全落空** →
//	dispatcher 判 "缺 vendor_id 或 event_id" → 丢弃
//
// 根因：老 webhookPayload 是照 6 家共性字段猜的骨架（`event_id` / `new_keys` /
// `order_id`）· 而本家实际发的是 `id` / `count` / `time` · 一个都对不上。
//
// 这组测试钉死真实载荷形状 —— 抓自生产日志原文。

import (
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 生产日志原文（2026-08-13T23:30:44 收到）
const realRestockBody = `{"available":50,"count":50,"event":"stock",` +
	`"id":"evt_BsawZMiNERBGITaBl5DcGNwV","price":100,"time":"2026-08-13T15:30:39Z"}`

func TestParse_RealRestockPayload(t *testing.T) {
	a := &Adapter{}
	evt, err := a.Parse([]byte(realRestockBody), nil)
	if err != nil {
		t.Fatal(err)
	}

	if evt.EventType != providers.EventNewKeysAvailable {
		t.Errorf("event=stock 应归一成 new_keys_available · 得 %q", evt.EventType)
	}
	// EventID 非空是 dispatcher 的准入条件 —— 空就整条丢弃
	if evt.EventID != "evt_BsawZMiNERBGITaBl5DcGNwV" {
		t.Errorf("EventID 应取 `id` · 得 %q", evt.EventID)
	}
	if evt.NewKeys != 50 {
		t.Errorf("NewKeys 应取 `count` · 得 %d", evt.NewKeys)
	}
	// 无区 vendor · 定价链靠 general 占位（见 mapper_test.go）
	if evt.Zone != providers.ZoneGeneral {
		t.Errorf("Zone 应 general · 得 %q", evt.Zone)
	}
	// 用 vendor 侧时刻 · 不是我方接收时刻
	want := time.Date(2026, 8, 13, 15, 30, 39, 0, time.UTC)
	if !evt.ReceivedAt.Equal(want) {
		t.Errorf("时刻应取载荷 `time` · want %v got %v", want, evt.ReceivedAt)
	}
	if evt.VendorID != providers.VendorKiroAppCC {
		t.Errorf("VendorID 错 · 得 %q", evt.VendorID)
	}
}

// 共性别名仍要认 —— vendor 改版回标准字段名时不能又静默
func TestParse_CommonAliasesStillWork(t *testing.T) {
	body := `{"event":"new_keys_available","event_id":"e-1","new_keys":7,` +
		`"purchase_order_id":"poid-1","timestamp":1786000000}`
	a := &Adapter{}
	evt, err := a.Parse([]byte(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if evt.EventID != "e-1" || evt.NewKeys != 7 || evt.PurchaseOrderID != "poid-1" {
		t.Errorf("共性别名解析错 · %+v", evt)
	}
	if evt.EventType != providers.EventNewKeysAvailable {
		t.Errorf("EventType 错 · 得 %q", evt.EventType)
	}
	if !evt.ReceivedAt.Equal(time.Unix(1786000000, 0).UTC()) {
		t.Errorf("应回落 unix timestamp · 得 %v", evt.ReceivedAt)
	}
}

func TestParse_TestEventNotTreatedAsRestock(t *testing.T) {
	a := &Adapter{}
	evt, err := a.Parse([]byte(`{"event":"test","id":"evt_t"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if evt.EventType != providers.EventTest {
		t.Errorf("test 事件不该当开号 · 得 %q", evt.EventType)
	}
}

// 时刻字段全缺时回落当下 · 不能留零值（零值会让 dispatch 落到 1970 · 图表全歪）
func TestParse_NoTimeFallsBackToNow(t *testing.T) {
	a := &Adapter{}
	evt, err := a.Parse([]byte(`{"event":"stock","id":"evt_x","count":3}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if evt.ReceivedAt.IsZero() || time.Since(evt.ReceivedAt) > time.Minute {
		t.Errorf("应回落当下时刻 · 得 %v", evt.ReceivedAt)
	}
}

// v4.4 · webhook 载荷带的 price / available 要暴露给 dispatcher 落 probe_zone
func TestParse_ExposesPriceAndAvailable(t *testing.T) {
	a := &Adapter{}
	evt, err := a.Parse([]byte(realRestockBody), nil)
	if err != nil {
		t.Fatal(err)
	}
	if evt.UnitPrice == nil {
		t.Fatal("UnitPrice 应非空（webhook 带了 price=100）")
	}
	// price=100 积分 → 100 * 1_000_000 microunit
	if evt.UnitPrice.Amount != 100_000_000 {
		t.Errorf("UnitPrice.Amount = %d · want 100_000_000（100 积分 microunit）", evt.UnitPrice.Amount)
	}
	if evt.UnitPrice.Currency != providers.CurrencyCredit {
		t.Errorf("UnitPrice.Currency 应 credit · 得 %v", evt.UnitPrice.Currency)
	}
	if evt.Available == nil {
		t.Fatal("Available 应非空（webhook 带了 available=50）")
	}
	if *evt.Available != 50 {
		t.Errorf("Available = %d · want 50", *evt.Available)
	}
}

// price=0 / available=0 · 不该往 evt 里填（避免 dispatcher 落 0-价一行）
func TestParse_ZeroPriceOmitted(t *testing.T) {
	a := &Adapter{}
	body := `{"event":"stock","id":"evt_y","count":3}`
	evt, err := a.Parse([]byte(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if evt.UnitPrice != nil {
		t.Errorf("price 缺失时 UnitPrice 应 nil · 得 %+v", evt.UnitPrice)
	}
	if evt.Available != nil {
		t.Errorf("available 缺失时 Available 应 nil · 得 %v", *evt.Available)
	}
}
