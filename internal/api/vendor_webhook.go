package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// vendor webhook 接收端 · 阶段 1a 骨架：
//
// 目标：**vendor 侧不再刷我方 404**。签名验证 + 记录事件 · 但**不真处理**
// credential-death / credential-alive 事件（那是 1d）。
//
// 路径：POST /api/webhooks/vendor/{vendor_id}
//   - {vendor_id} 是 术语铁律 §1.1 的 vendor slug
//   - 白名单 knownVendorSlugs 校验（防随便打路径触发 log）
//   - hmacSpecs 里注册的 slug 会做 HMAC 验签 · 未注册的靠 URL 保密（1b/1d 再补）
//
// 密钥全从环境变量拿：`BP_VENDOR_<slug>_WEBHOOK_SECRET`（大写）·
// 有 HMAC spec 但密钥缺失 = 该 vendor 拒收 401（防未配就上线）。

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
	Prefix    string // 值前缀 · sha256= / v1= / 空（纯 hex）
	TimeHdr   string // 时间戳 header 名·空 = 不带 timestamp
	VendorID  string // vendor_account 表里的 internal vendor_id（生产从表读 secret）
	SecretEnv string // 环境变量名（dev 兜底 · 或表未 seed 时降级）
}

// hmacSpecs · vendor webhook 签名协议 · 一家一条。
//
// **VendorID** = vendor_account 表里的 internal vendor_id（跟白名单 slug 不完全一样：
// 白名单 slug 用 vendor 的品牌写法 · vendor_account 里用内部 id · 两者对少数家不一致）。
// 装两个 key 才能在 receiver（用白名单 slug）里查表（用 vendor_account 里的 id）。
var hmacSpecs = map[string]*hmacSpec{
	"91kiro": {
		Header: "X-KM-Signature", Prefix: "sha256=", TimeHdr: "X-KM-Timestamp",
		VendorID:  "kiro91",
		SecretEnv: "BP_VENDOR_KIRO91_WEBHOOK_SECRET",
	},
	"kirodrop": {
		Header: "X-Kiro-Signature", Prefix: "v1=", TimeHdr: "X-Kiro-Timestamp",
		VendorID:  "kirodrop",
		SecretEnv: "BP_VENDOR_KIRODROP_WEBHOOK_SECRET",
	},
	// 第三家 · X-Kiro-Signature 头 · **纯 hex** · 无 v1= 前缀 · 无 timestamp ·
	// 签名原文 = 请求体（不含时间戳前缀）· vendor 后台文案：
	//   "每次推送带 X-Kiro-Signature 头 · 值为 HMAC-SHA256(密钥, 请求体)"
	"kiroappcc": {
		Header: "X-Kiro-Signature", Prefix: "", TimeHdr: "",
		VendorID:  "kiroappcc",
		SecretEnv: "BP_VENDOR_KIROAPPCC_WEBHOOK_SECRET",
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
		secret := s.resolveWebhookSecret(r.Context(), spec)
		if secret == "" {
			// 密钥没配·**拒**（返 401 让 vendor 知道要重发或联系我方）
			slog.Warn("vendor webhook 密钥未配 · 拒收",
				"vendor", slug, "vendor_id", spec.VendorID, "env", spec.SecretEnv)
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

	// log 一条 · body 前 256 字节（原始 body 只在 log 里保留 · inbound_webhook_event
	// 表存归一化字段 · 明文 key payload 不落库）
	preview := string(raw)
	if len(preview) > 256 {
		preview = preview[:256] + "…"
	}
	slog.Info("vendor webhook 收到",
		"vendor", slug,
		"event_type", r.Header.Get("X-Event-Type"),
		"content_type", r.Header.Get("Content-Type"),
		"body_bytes", len(raw),
		"body_preview", preview)

	// 分派到 webhookin.Dispatcher · 用 vendor adapter 的 Parse() 归一化事件类型 ·
	// 然后按类型走 vendor_dispatch / deathwatch / refund 三条链路。
	//
	// dispatcher 未装配（老部署 / 测试）· 或 registry 里查不到 vendor · 都直接 200
	// 不阻塞 vendor 侧（vendor 只关心 2xx · 我方内部错误自己重试或人工排查）
	processed := false
	if s.webhookDispatcher != nil {
		internalID := slugToInternalVendorID(slug)
		if v, ok := s.lookupVendorByID(internalID); ok {
			if wp, ok := v.(providers.WebhookParser); ok {
				if evt, err := wp.Parse(raw, r.Header); err == nil && evt != nil {
					if evt.VendorID == "" {
						evt.VendorID = providers.VendorID(internalID)
					}
					if err := s.webhookDispatcher.Handle(r.Context(), evt); err != nil {
						slog.Warn("webhookin 分派出错 · vendor 侧仍返 200",
							"vendor", slug, "event_id", evt.EventID, "err", err)
					} else {
						processed = true
					}
				} else if err != nil {
					slog.Warn("vendor adapter Parse 失败", "vendor", slug, "err", err)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"received":  true,
		"vendor":    slug,
		"processed": processed,
	})
	return nil
}

// slugToInternalVendorID · webhook 路径的 slug（vendor 品牌用）→ 内部 vendor_id。
//
// 只有一家的品牌写法跟内部 id 不同 · 其他 5 家一致 · 所以这里只做单条映射。
func slugToInternalVendorID(slug string) string {
	if slug == "91kiro" {
		return "kiro91"
	}
	return slug
}

// lookupVendorByID · 从 vendorView 里查 vendor · 让 receiver 拿到 WebhookParser。
// 没装配 vendorView 时返 nil, false（老部署路径）。
func (s *Server) lookupVendorByID(vendorID string) (providers.Vendor, bool) {
	if s.vendorView == nil {
		return nil, false
	}
	return s.vendorView.LookupVendor(vendorID)
}

// resolveWebhookSecret · 从 vendor_account 表读 webhook_secret 明文（AES 解密后）·
// 表空 fallback env·两个都空返 ""。
//
// **决策**（decisions §11.6）：生产环境走表 · env 只是 dev 兼容通道。
// 表查错误不 log 到高位（会有 401 后续告警 · 别重复噪音）。
func (s *Server) resolveWebhookSecret(ctx context.Context, spec *hmacSpec) string {
	if s.vaStore != nil && spec.VendorID != "" {
		cred, err := s.vaStore.LoadActive(ctx, spec.VendorID)
		if err == nil && cred != nil && cred.WebhookSecret != "" {
			return cred.WebhookSecret
		}
	}
	return os.Getenv(spec.SecretEnv)
}

// verifyVendorHMAC · 有 HMAC 的 vendor 通用格式：
//
//	mac = HMAC-SHA256(secret, timestamp + "." + body)  · 部分 vendor 用 sha256= 前缀
//	mac = HMAC-SHA256(secret, timestamp + "." + body)  · 另一些用 v1= 前缀
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
