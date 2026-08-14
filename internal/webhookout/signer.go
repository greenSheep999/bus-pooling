package webhookout

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// SignatureVersion · X-Bus-Signature 头的版本前缀(sha256=<hex>)。
//
// **从一开始就用版本前缀**·未来换 SHA512 或别的算法时前端能同时验多版本 ·
// 别用裸 hex(那样 v1/v2 切换时会有兼容期)。
const SignatureVersion = "sha256"

// SignPayload 计算 HMAC-SHA256 签名 · 返回 hex 编码。
//
// 输入: secret + timestamp + "." + body(所有字节)
// 输出: sha256=<64 位 hex>
//
// **timestamp 是 unix 秒**(不是毫秒·跟 docs/05 §11 X-Bus-Timestamp 一致) ·
// body 是**原始 JSON 字节**(不 pretty-print / 不重排 · 乘客侧验签也要拿原字节)。
//
// 秘密防重放:乘客侧应校验 timestamp 在当前 ±5min · 我方 payload 里冗余 timestamp 字段。
func SignPayload(secret string, timestamp time.Time, body []byte) string {
	ts := strconv.FormatInt(timestamp.UTC().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte{'.'})
	mac.Write(body)
	sum := mac.Sum(nil)
	return SignatureVersion + "=" + hex.EncodeToString(sum)
}
