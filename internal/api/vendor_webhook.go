package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// vendor webhook 接收端 · 阶段 1a 骨架：
//
// 目标：**vendor 侧不再刷我方 404**。签名验证 + 记录事件 · 但**不真处理**
// credential-death / credential-alive 事件（那是 1d）。
//
// 6 家 vendor 的路径统一：POST /api/webhooks/vendor/{vendor_id}
// 其中 {vendor_id} 是 术语铁律 §1.1 的 vendor slug：
//   - 91kiro   · X-KM-Signature: sha256=<hex>  · mac(secret, ts + "." + body)
//   - kirodrop · X-Kiro-Signature: v1=<hex>    · mac(secret, ts + "." + body)（drop.kiro.ss）
//   - 其余 4 家（kiroceo / kirooo / kiroappio / kiroappcc）· 文档明示或空缺 · 无 HMAC
//     · 靠 URL 保密（1b/1d 再补）
//
// 密钥从环境变量拿·同 vendor secrets 规范：
//   BP_VENDOR_KIRO91_WEBHOOK_SECRET
//   BP_VENDOR_KIRODROP_WEBHOOK_SECRET
// 缺失 · 该 vendor 的 webhook 会**拒绝所有请求 401**（防未配置就上线）。

// vendor slug 白名单·防随便打路径进来触发 log
var knownVendorSlugs = map[string]bool{
	"91kiro":    true,
	"kiroceo":   true,
	"kirooo":    true,
	"kiroappio": true,
	"kiroappcc": true,
	"kirodrop":  true,
}

// hmacSpec 描述有 HMAC 那几家的签名协议
type hmacSpec struct {
	Header    string // 请求 header 名·不区分大小写
	Prefix    string // 值前缀 · sha256= 或 v1=
	TimeHdr   string // 时间戳 header 名·空 = 不带 timestamp
	SecretEnv string // 环境变量名
}

var hmacSpecs = map[string]*hmacSpec{
	"91kiro": {
		Header: "X-KM-Signature", Prefix: "sha256=", TimeHdr: "X-KM-Timestamp",
		SecretEnv: "BP_VENDOR_KIRO91_WEBHOOK_SECRET",
	},
	"kirodrop": {
		Header: "X-Kiro-Signature", Prefix: "v1=", TimeHdr: "X-Kiro-Timestamp",
		SecretEnv: "BP_VENDOR_KIRODROP_WEBHOOK_SECRET",
	},
}

// handleVendorWebhook · POST /api/webhooks/vendor/{vendor_id}
//
// **1a 只**：验签（有 HMAC 那两家）+ 白名单校验 + 记 log + 返 200。
// 真处理 credential 状态事件是 1d（webhookin 包）。
func (s *Server) handleVendorWebhook(w http.ResponseWriter, r *http.Request) error {
	slug := r.PathValue("vendor_id")
	if !knownVendorSlugs[slug] {
		// 不是我方 6 家 · 拒 · 别让 vendor 误以为接住了
		return newFail(http.StatusNotFound, "unknown_vendor",
			"未知的 vendor slug")
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return ErrBadJSON("回调 body 读取失败")
	}

	spec, wantsHMAC := hmacSpecs[slug]
	if wantsHMAC {
		secret := os.Getenv(spec.SecretEnv)
		if secret == "" {
			// 密钥没配·**拒**（返 401 让 vendor 知道要重发或联系我方）
			slog.Warn("vendor webhook 密钥未配 · 拒收", "vendor", slug, "env", spec.SecretEnv)
			return &Fail{Status: http.StatusUnauthorized,
				Err: &Error{Code: "webhook_not_configured", Message: "webhook 未配置密钥"}}
		}
		sig := r.Header.Get(spec.Header)
		ts := ""
		if spec.TimeHdr != "" {
			ts = r.Header.Get(spec.TimeHdr)
		}
		if err := verifyVendorHMAC(secret, spec.Prefix, ts, sig, raw); err != nil {
			slog.Warn("vendor webhook 验签失败", "vendor", slug, "err", err)
			return &Fail{Status: http.StatusUnauthorized,
				Err: &Error{Code: "bad_signature", Message: "签名验证不通过"}}
		}
	}

	// 阶段 1a 只 log · 事件类型 / body 前 256 字节
	// 不解析·不写状态·不触发号死处理（那是 1d 的 webhookin 包）
	preview := string(raw)
	if len(preview) > 256 {
		preview = preview[:256] + "…"
	}
	slog.Info("vendor webhook 收到（1a 只 log·不处理）",
		"vendor", slug,
		"event_type", r.Header.Get("X-Event-Type"),
		"content_type", r.Header.Get("Content-Type"),
		"body_bytes", len(raw),
		"body_preview", preview)

	writeJSON(w, http.StatusOK, map[string]any{
		"received":  true,
		"vendor":    slug,
		"processed": false,
		"note":      "阶段 1a 仅接收 · 事件在 1d 处理",
	})
	return nil
}

// verifyVendorHMAC · 91kiro + kirodrop 通用格式：
//
//	mac = HMAC-SHA256(secret, timestamp + "." + body)  · 91kiro 用 sha256= 前缀
//	mac = HMAC-SHA256(secret, timestamp + "." + body)  · kirodrop 用 v1= 前缀
//
// timestamp 缺失（有些实现不带）· 直接 sign(body)
func verifyVendorHMAC(secret, prefix, ts, sigHeader string, body []byte) error {
	if sigHeader == "" {
		return errBadSig
	}
	if !strings.HasPrefix(sigHeader, prefix) {
		return errBadSig
	}
	gotHex := sigHeader[len(prefix):]
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return errBadSig
	}
	// 时间戳窗口（有 header 就查·30 分钟宽·跟 payment gateway 一致 §12.6）
	if ts != "" {
		if !within(ts, 30*time.Minute) {
			return errStaleSig
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	if ts != "" {
		mac.Write([]byte(ts))
		mac.Write([]byte("."))
	}
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return errBadSig
	}
	return nil
}

// within 判断 unix 秒时间戳在 ±d 窗口内
func within(ts string, d time.Duration) bool {
	sec, err := parseUnixSeconds(ts)
	if err != nil {
		return false
	}
	drift := time.Now().Unix() - sec
	if drift < 0 {
		drift = -drift
	}
	return time.Duration(drift)*time.Second <= d
}

func parseUnixSeconds(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadSig
		}
		n = n*10 + int64(c-'0')
	}
	if n == 0 {
		return 0, errBadSig
	}
	return n, nil
}

var (
	errBadSig   = &webhookErr{"bad signature"}
	errStaleSig = &webhookErr{"timestamp out of window"}
)

type webhookErr struct{ msg string }

func (e *webhookErr) Error() string { return e.msg }
