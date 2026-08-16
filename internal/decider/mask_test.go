package decider

import (
	"strings"
	"testing"
)

// maskKey 的铁律：**输出绝不包含完整明文** —— 这函数的结果会进 DB 和前端。
func TestMaskKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plain string
		want  string
	}{
		{"标准 ksk", "ksk_5NInKiurSdK8wjrDZmHBtjlt101UySK2", "ksk_...ySK2"},
		{"别的前缀", "usr_abcdefghijklmnop", "usr_...mnop"},
		{"空串返空", "", ""},
		{"短到没末 4 位", "ksk_a", "ksk_…"},
		{"无下划线", "abcdefghij", "ksk_...ghij"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := maskKey(tc.plain)
			if got != tc.want {
				t.Errorf("maskKey(%q) = %q · 期望 %q", tc.plain, got, tc.want)
			}
		})
	}
}

// 长明文绝不整段出现在输出里（防手滑改成返原文）
func TestMaskKey_NeverLeaksPlaintext(t *testing.T) {
	plain := "ksk_5NInKiurSdK8wjrDZmHBtjlt101UySK2"
	got := maskKey(plain)
	if got == plain {
		t.Fatal("maskKey 返了原文")
	}
	// 只允许末 4 位泄漏 · 去掉末 4 位后的主体不该出现在结果里
	body := plain[4 : len(plain)-4]
	if len(body) > 0 && strings.Contains(got, body) {
		t.Errorf("maskKey 输出含明文主体: %q", got)
	}
}
