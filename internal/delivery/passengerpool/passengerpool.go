// Package passengerpool 是"推乘客自己号池"(去向 ②)的抽象层。
//
// 上层(pullrecord / api handler)只 import 这个包·不 import 具体协议实现(kirors)。
// 换协议只需加一个新子包。
//
// **跟 housepool 的关键差别**：admin_token 是**一次性**的——
// 每次 Push 时从 downstream.DecryptPassengerpoolToken 取一次明文·构造 client·用完扔·
// Client struct 不留 adminKey 字段。这是因为：
//   - 乘客的 admin_token 可能随时轮换·长住会拿旧 token
//   - 用完就 GC 减少明文在内存里的时间窗口
package passengerpool

import (
	"context"
	"errors"
	"fmt"
)

// ErrorKind 是归一化后的错误分类·**只出六种**(对外脱敏 · CLAUDE.md §0.1)：
//
//   - unauthorized 鉴权失败(401/403)·不可重试
//   - not_found    对方端点不存在(404)·不可重试
//   - conflict     号已存在或状态冲突(409/duplicate)·特殊·会被 Pusher 归成成功
//   - timeout      网络超时 / 5xx / DNS 挂·可重试
//   - duplicate    对家幂等命中·refresh_token 已导入·**视为成功**(见 Pusher)
//   - bad_request  4xx 其它·配置错·不可重试
//   - stream_broken SSE 流中途断·可重试
type ErrorKind string

const (
	ErrKindUnauthorized ErrorKind = "unauthorized"
	ErrKindNotFound     ErrorKind = "not_found"
	ErrKindConflict     ErrorKind = "conflict"
	ErrKindTimeout      ErrorKind = "timeout"
	ErrKindDuplicate    ErrorKind = "duplicate"
	ErrKindBadRequest   ErrorKind = "bad_request"
	ErrKindStreamBroken ErrorKind = "stream_broken"
)

// Retriable 决定 push_error_retriable 落库值(decisions §8.24)。
// 只有 timeout / stream_broken 判可重试·其他都不重试。
func (k ErrorKind) Retriable() bool {
	switch k {
	case ErrKindTimeout, ErrKindStreamBroken:
		return true
	}
	return false
}

// PushError 是**对外可见**的推送错误(pullrecord.PushError 的孪生)。
//
// 字段跟 credential_ledger 里的 push_error_* 五字段对齐(migration 001)。
// **不带**对家协议名 / 内部术语——只有六种通用错误码 + 人话 message。
type PushError struct {
	Kind ErrorKind
	// Status HTTP 状态码 · 0 = 没连上(超时 / DNS)
	Status int
	// Message 给用户看的人话(可以带对家域名·但**不能带**协议名 / 内部字段)
	Message string
}

func (e *PushError) Error() string {
	if e == nil {
		return ""
	}
	if e.Status > 0 {
		return fmt.Sprintf("passengerpool: %s(HTTP %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("passengerpool: %s: %s", e.Kind, e.Message)
}

// Retriable 是重试判据(pullrecord 侧写库时用)。
func (e *PushError) Retriable() bool {
	if e == nil {
		return false
	}
	return e.Kind.Retriable()
}

// Sentinel 错误·让上层 errors.Is 判特定 Kind(测试用得多)。
var (
	ErrUnauthorized = errors.New("passengerpool: 对家 admin_token 无效")
	ErrNotFound     = errors.New("passengerpool: 对家 URL 无此端点")
	ErrConflict     = errors.New("passengerpool: 对家已存在冲突")
	ErrTimeout      = errors.New("passengerpool: 对家超时或不可达")
	ErrBadRequest   = errors.New("passengerpool: 请求被对家拒绝")
	ErrStreamBroken = errors.New("passengerpool: 导入流中断")
)

// Unwrap 让 errors.Is 认 Kind → sentinel 的映射。
func (e *PushError) Unwrap() error {
	switch e.Kind {
	case ErrKindUnauthorized:
		return ErrUnauthorized
	case ErrKindNotFound:
		return ErrNotFound
	case ErrKindConflict:
		return ErrConflict
	case ErrKindTimeout:
		return ErrTimeout
	case ErrKindBadRequest:
		return ErrBadRequest
	case ErrKindStreamBroken:
		return ErrStreamBroken
	}
	return nil
}

// PushCredential 是一条要推给乘客号池的号。
//
// **明文字段**只在这个 struct 里流转·用完就扔·
// 绝不落库·绝不 log·绝不进 slog 结构化字段。
type PushCredential struct {
	// CredentialID 我方 credential_ledger.id(UUID v7)·对外派发用这个
	CredentialID string
	// RefreshToken / AccessToken / KiroAPIKey 三个明文字段任一非空
	//
	// 明文缺口降级路径(BP_ALLOW_PASSENGERPOOL_PLACEHOLDER=1)：
	// 三字段可以是占位字符串·对家 mock 收到"PLACEHOLDER"就算联调通了。
	RefreshToken string
	AccessToken  string
	KiroAPIKey   string
	// Email / Region / Groups 是可选元数据·对家可能按 group 分类
	Email  string
	Region string
	Groups []string
	// VendorLabel 对家收到的 sourceChannel 字段·用打码 label 不用真名
	VendorLabel string
}

// PushResult 是 Push 的返回·跟 credential_ledger 六字段对齐。
//
// **每号一个结果**：Success[i] / Failed[i] 互斥。
// Duplicate 视为 Success(refresh_token 幂等命中·对家已有·不重推)。
type PushResult struct {
	// Success 成功推送的 credential_id
	Success []string
	// Failed 每个失败的 credential 一条 · Err 非 nil
	Failed []FailedItem
	// Duplicate 对家幂等命中的 · 也视为成功但独立统计给 log
	Duplicate []string
}

// FailedItem 一个号推送失败的详情。
type FailedItem struct {
	CredentialID string
	Err          *PushError
}

// Pusher 是"推乘客号池"的抽象接口。
//
// **窄接口**·装配层实现·上层(handler)只见这个 · 不见 housepool 后端 协议名。
// **不依赖** pullrecord / bus · Layer 5 只被 Layer 3 调。
type Pusher interface {
	// Push 把一批号推给该乘客的 passengerpool。
	//
	// **必须调用方保证归属校验**(handler 已用 pullrecord.GetOwnershipsTx 校验过) ·
	// Pusher 只负责调对家 + 归错分类。
	//
	// 返回 PushResult 拆开成功 / 失败 / 幂等三类 · handler 按各自事务写库。
	// 顶层 error 只在"根本没能开始"时非 nil(拉 downstream 配置失败 / 拿明文失败) ·
	// 对家协议错走 PushResult.Failed。
	Push(ctx context.Context, passengerID string, creds []PushCredential) (*PushResult, error)
}

// UsePlaceholder 是明文缺口降级开关的判据·跟 handoff 的 BP_ALLOW_HANDOFF_PLACEHOLDER 对称。
//
// 装配层用这个环境变量决定 pushCredentialFromLedger 里三字段填 placeholder 还是真明文 ·
// **上线**：接了 housepool reveal 端点后 · 环境变量应该关掉(默认关) · 走真明文分支。
const EnvAllowPlaceholder = "BP_ALLOW_PASSENGERPOOL_PLACEHOLDER"

// ErrNoTarget 乘客没配 passengerpool_url / 没配 token · Pusher 走 dry-run 兜底。
//
// **不算错**：handler 收到这个应该走 MarkPushed(只标时间戳不真推) · 跟 1a 一致。
// 生产 live 模式应该在装配层强制要求配 · 但 mock 环境不能因此炸。
var ErrNoTarget = errors.New("passengerpool: 乘客未配 passengerpool 或 cipher 未装")
