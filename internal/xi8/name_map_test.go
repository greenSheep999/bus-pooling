package xi8

// 昵称 → slug 反查的回归测试
//
// **为什么需要昵称反查**：聚合源的 `/api/my/notifications`（唯一带历史价格的端点）
// 每条通知**只给昵称不给 vendor_id** · 其他端点都给 id。要把历史价落到对应 vendor
// 只能靠昵称反查（docs/vendors/xi8.md §3.7）。
//
// 昵称是上游自己起的 · 上游改了这里要跟着改。反查不到时**跳过不猜** ——
// 宁可少一条历史价 · 不要把价归错家（归错家 = 用错的价给用户报价）。

import "testing"

// 两张映射表必须一致 · 不能一张有一张没有
func TestNameMapConsistentWithIDMap(t *testing.T) {
	if len(vendorNameToXi8ID) != len(vendorSlugByXi8ID) {
		t.Errorf("两张表条数不一致 · name→id 有 %d 条 · id→slug 有 %d 条",
			len(vendorNameToXi8ID), len(vendorSlugByXi8ID))
	}
	// 每个昵称都能一路查到 slug
	for name, id := range vendorNameToXi8ID {
		slug := vendorSlugByXi8ID[id]
		if slug == "" {
			t.Errorf("昵称 %q → id %d · 但 id→slug 查不到", name, id)
		}
		if got := VendorSlugForXi8Name(name); got != slug {
			t.Errorf("VendorSlugForXi8Name(%q) = %q · want %q", name, got, slug)
		}
	}
	// id 不重复（两个昵称指同一家会让统计错乱）
	seen := make(map[int]string, len(vendorNameToXi8ID))
	for name, id := range vendorNameToXi8ID {
		if prev, dup := seen[id]; dup {
			t.Errorf("id %d 被两个昵称占用：%q 和 %q", id, prev, name)
		}
		seen[id] = name
	}
}

// 未映射的昵称返空串 · 调用方据此跳过
func TestVendorSlugForXi8Name_UnknownReturnsEmpty(t *testing.T) {
	for _, in := range []string{"", "没见过的名字", "unknown-vendor"} {
		if got := VendorSlugForXi8Name(in); got != "" {
			t.Errorf("VendorSlugForXi8Name(%q) = %q · 未映射该返空串（调用方靠它跳过）", in, got)
		}
	}
}

// id 反查也得对（其他端点走这条）
func TestVendorSlugForXi8ID(t *testing.T) {
	if got := VendorSlugForXi8ID(0); got != "" {
		t.Errorf("id 0（测试推送用的假 id）该返空串 · got %q", got)
	}
	if got := VendorSlugForXi8ID(999); got != "" {
		t.Errorf("未映射 id 该返空串 · got %q", got)
	}
	// 5 家都能查到
	for id := 1; id <= 5; id++ {
		if got := VendorSlugForXi8ID(id); got == "" {
			t.Errorf("id %d 查不到 slug", id)
		}
	}
}
