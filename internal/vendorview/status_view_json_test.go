package vendorview

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVendorStatusRow_QualityInJSON · row 的 JSON 必须含 quality 字段
//
// 上一版实测 · 生产 curl 出来没 quality 键 · 靠这个测试卡住
func TestVendorStatusRow_QualityInJSON(t *testing.T) {
	row := VendorStatusRow{
		AnonID:      "abc123",
		AnonLabel:   "Vendor Test",
		Alive:       true,
		StockBucket: "many",
		Quality: VendorQuality{
			Score: 87,
			Tags: []QualityTag{
				{Kind: "stable"},
				{Kind: "high-volume"},
			},
		},
	}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	// quality 键必须在
	if !strings.Contains(s, `"quality"`) {
		t.Fatalf("JSON 缺 quality 键: %s", s)
	}
	// tags 数组必须有内容
	if !strings.Contains(s, `"kind":"stable"`) || !strings.Contains(s, `"kind":"high-volume"`) {
		t.Errorf("tags 内容丢失: %s", s)
	}
	// Score 不应出现（json:"-"）
	if strings.Contains(s, `"score"`) || strings.Contains(s, `"Score"`) {
		t.Errorf("Score 应内部字段 · 不该出 JSON: %s", s)
	}
}

// TestVendorStatusRow_EmptyTagsStillEmits · 数据不足时 tags 只有 watching · 但仍要出 quality 键
func TestVendorStatusRow_EmptyTagsStillEmits(t *testing.T) {
	row := VendorStatusRow{
		AnonID:  "abc",
		Quality: VendorQuality{Tags: []QualityTag{{Kind: "watching"}}},
	}
	raw, _ := json.Marshal(row)
	if !strings.Contains(string(raw), `"quality"`) {
		t.Errorf("watching 态也应有 quality: %s", raw)
	}
}
