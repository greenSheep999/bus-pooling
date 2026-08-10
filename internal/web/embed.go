// Package web 内嵌前端 Vite 产物 · Docker 里由 web-build stage 塞到 dist/。
//
// 作用：Go 二进制单独就能 serve SPA · 不再需要 nginx / caddy 挂 web/dist。
// 运行时布局：`internal/web/dist/index.html` + assets · Docker COPY 时打包进去。
//
// 本地开发：`web/dist` 目录空是 OK 的 —— 未 build 时 Handler() 返 404，
// 让开发者用 Vite dev server（5173）走反代。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// 内嵌 dist 目录。Docker 三阶段构建里，web-build stage 会把 Vite 产物
// 拷到这里再让 go-build stage 编译。
//
//go:embed all:dist
var dist embed.FS

// distSub 解开 `dist/` 前缀·让 http.FS 从根目录开始查
func distSub() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

// Handler 返 SPA 静态服务 handler。
//
//   - 请求 `/api/*`、`/healthz` 不该走到这里（在 mux 里已有专门 handler·mux 优先命中）
//   - 请求 `/assets/*`、`/*.svg`、`/*.png` 等：直接查内嵌 FS
//   - 请求 `/` 或找不到文件的路径：返回 index.html（SPA 路由兜底）
//   - 内嵌 FS 空（本地未 build）：返 404 · 前端提示到 5173 dev 起
func Handler() http.Handler {
	sub, err := distSub()
	if err != nil {
		return http.NotFoundHandler()
	}

	// 检查 index.html 是否真的 embed 了 · 判断 dist 是否有内容
	indexBytes, indexErr := fs.ReadFile(sub, "index.html")
	if indexErr != nil {
		// 本地开发 · web 还没 build · 静静 404 · 别炸日志
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "web dist not embedded · run `npm run build` in web/", http.StatusNotFound)
		})
	}

	fsHandler := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API 前缀不该走静态 · 让 mux 前面的路由先命中·这里做双保险
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}

		// 尝试直接从 FS 读文件 · 不存在则回退 index.html（SPA 客户端路由）
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			serveIndex(w, r, indexBytes)
			return
		}
		if _, err := fs.Stat(sub, clean); err != nil {
			// 文件不存在 · SPA fallback
			serveIndex(w, r, indexBytes)
			return
		}
		fsHandler.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}
