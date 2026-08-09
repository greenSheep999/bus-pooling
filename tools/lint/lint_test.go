// Package lint · 用 go test 的方式集成 CI 术语检查 · CI 只跑 `go test ./...` 就够。
//
// 这个 test 调 tools/lint/no_internal_terms.py · 扫全库注释 + 字符串字面量。
// 出违规 → test fail · CI 直接红。
package lint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoInternalTermsInSource · CLAUDE.md 硬约束 CI · 扫全库禁止内部术语泄漏。
func TestNoInternalTermsInSource(t *testing.T) {
	// 找 repo 根（往上找到含 go.mod 的目录）
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("找不到 go.mod · 无法定位 repo 根")
		}
		root = parent
	}

	script := filepath.Join(root, "tools", "lint", "no_internal_terms.py")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("lint script 不在预期位置·跳过: %v", err)
	}

	cmd := exec.Command("python3", script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CI 内部术语检查失败·违规见下方输出：\n\n%s", string(out))
	}
}

// TestLintCoversFrontend · 防回退：lint 必须覆盖前端源码。
//
// **为什么单独测这条**：这个 lint 原来只扫 .go / .sql · 结果顶部 promo 里的
// 通道商真名溜了很久没人发现。而 CLAUDE.md §12.6 管的**恰恰是对外文案** ——
// 那些字全在前端。如果哪天有人把 web/src 从扫描范围里拿掉，这条会红。
func TestLintCoversFrontend(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "tools", "lint", "no_internal_terms.py")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("lint script 不在预期位置·跳过: %v", err)
	}
	src, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, need := range []string{`"web/src"`, `".tsx"`, `".ts"`} {
		if !strings.Contains(body, need) {
			t.Errorf("lint 脚本里找不到 %s —— 前端可能被从扫描范围里移除了"+
				"（CLAUDE.md §12.6 的对外文案全在前端·必须扫）", need)
		}
	}
}

// TestLintCatchesReasoningComments · 防回退：lint 必须能抓"思考日志式注释"（§0.2）。
//
// 这类注释反复溜进代码（产品论述 / 对话记录 / 方案论证写在 // 里）·
// 之前 lint 只查术语泄漏不查这个。这条测试保证规则不被摘掉。
func TestLintCatchesReasoningComments(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "tools", "lint", "no_internal_terms.py")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("lint script 不在预期位置·跳过: %v", err)
	}
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	// §0.2 那组规则的特征串 —— 少一个说明规则被删了
	for _, need := range []string{"§0.2", "消除焦虑", "营销支出", "对话记录"} {
		if !strings.Contains(string(body), need) {
			t.Errorf("lint 缺少 §0.2 注释检查的模式 %q —— "+
				"思考日志式注释会重新溜进代码", need)
		}
	}
}

// repoRoot 往上找到含 go.mod 的目录。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("找不到 go.mod · 无法定位 repo 根")
		}
		root = parent
	}
}
