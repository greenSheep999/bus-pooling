package api

import (
	"net/http"
	"net/url"
	"sync"
	"testing"
)

// testJar 是最小的 cookie jar —— net/http/cookiejar 需要 publicsuffix，
// 测试里对 127.0.0.1 存一份就够。
type testJar struct {
	mu sync.Mutex
	c  []*http.Cookie
}

func newJar(t *testing.T) *testJar {
	t.Helper()
	return &testJar{}
}

func (j *testJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, in := range cookies {
		// MaxAge < 0 = 删除
		if in.MaxAge < 0 {
			for i, existing := range j.c {
				if existing.Name == in.Name {
					j.c = append(j.c[:i], j.c[i+1:]...)
					break
				}
			}
			continue
		}
		replaced := false
		for i, existing := range j.c {
			if existing.Name == in.Name {
				j.c[i] = in
				replaced = true
				break
			}
		}
		if !replaced {
			j.c = append(j.c, in)
		}
	}
}

func (j *testJar) Cookies(_ *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]*http.Cookie, len(j.c))
	copy(out, j.c)
	return out
}

func (j *testJar) cookies() []*http.Cookie { return j.Cookies(nil) }
