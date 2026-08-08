package downstream

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// ValidateTargetURL 校验一个"我方要主动发请求过去"的 URL。
//
// 拒绝内网 / 回环 / 云元数据地址 —— 防 SSRF：用户配一个 http://169.254.169.254/latest/…
// 之后我方后端拉起 webhook 或 passengerpool 测试，就会拿到我们云主机的 IAM 凭证。
//
// **不拒绝**未解析的域名 —— 有些自建 kiro.rs 走内网 DNS，公网解析失败但服务能通。
// SSRF 的关键防线在 IP 层（IP literal 直接拉黑，DNS 解析结果拉黑），域名合法性由
// httpx 层的 timeout + DNS 失败自然处理。
func ValidateTargetURL(raw string) error {
	if raw == "" {
		return errBadURL("地址不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errBadURL("地址格式不对")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errBadURL("只支持 http/https")
	}
	host := u.Hostname()
	if host == "" {
		return errBadURL("地址缺少主机名")
	}

	// IP literal 场景：直接判段
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return errBadURL("不能配内网 / 回环 / 云元数据地址")
		}
		return nil
	}

	// 域名场景：拉黑关键字（内网 / 元数据服务）· 域名 → IP 的实际验证放到调用发起时做
	lowered := strings.ToLower(host)
	for _, blocked := range blockedHostSuffixes {
		if lowered == blocked || strings.HasSuffix(lowered, "."+blocked) {
			return errBadURL("不能配内网 / 回环地址")
		}
	}
	return nil
}

// blockedHostSuffixes 是明确不能配的域名。这里只挡"user 手抄地址笔误"级别的，
// 深层 SSRF 防护（DNS rebinding / CIDR 匹配所有解析结果）在 httpx 层做。
var blockedHostSuffixes = []string{
	"localhost",
	"local",
	"internal",
	"metadata.google.internal", // GCP
	"metadata.goog",
}

// isBlockedIP 拉黑的 IP 段。
//
// - 回环：127.0.0.0/8 · ::1
// - 私网：10/8 · 172.16/12 · 192.168/16 · 169.254/16（含 AWS/Azure 元数据 169.254.169.254）
// - IPv6 内网：fc00::/7 · fe80::/10
// - 未指定 / 多播 / 保留
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// AWS 元数据（IPv6 变体 fd00:ec2::254）
	if ip.To4() == nil {
		lowered := strings.ToLower(ip.String())
		if strings.HasPrefix(lowered, "fd00:ec2:") {
			return true
		}
	}
	return false
}

// MaskToken 生成对外展示用 mask。
//
// 规则：前缀（如 "kiro_admin_"）+ 16 个 • + 最后 4 位真值。
// 参考 web/src/mocks/handlers.ts:252 和 fixtures.ts:784 —— 前端已用这种形状渲染。
func MaskToken(plaintext string) string {
	return maskWithPrefix("kiro_admin_", plaintext)
}

// MaskSecret 给 webhook secret 用。前缀 "whsec_"。
func MaskSecret(plaintext string) string {
	return maskWithPrefix("whsec_", plaintext)
}

func maskWithPrefix(prefix, plaintext string) string {
	if plaintext == "" {
		return ""
	}
	tail := plaintext
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return prefix + strings.Repeat("•", 16) + tail
}

// ── 错误 ────────────────────────────────────────────

// ErrBadURL URL 校验失败 —— message 是人话，可直接落对外错误响应。
var ErrBadURL = errors.New("downstream: 地址不合法")

type badURLError struct{ msg string }

func (e *badURLError) Error() string       { return e.msg }
func (e *badURLError) Unwrap() error       { return ErrBadURL }
func (e *badURLError) UserMessage() string { return e.msg }

func errBadURL(msg string) error { return &badURLError{msg: msg} }

// UserMessage 让 handler 拿到"能直接给用户看"的中文 message。
func UserMessage(err error) string {
	var u interface{ UserMessage() string }
	if errors.As(err, &u) {
		return u.UserMessage()
	}
	return ""
}
