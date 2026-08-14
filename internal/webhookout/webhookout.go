// Package webhookout · 我方 → 乘客的对外 webhook 出向。
//
// 位置：所有触发源(decider.settle 成功 / deathwatch.markDead / deathwatch.RefundOnce /
// handoff.Complete / pullrecord.Assign push_pool 成功)成功后调 Dispatcher.Dispatch。
//
// **职责边界**：
//   - Dispatch 是**非阻塞**入队 · 主链路不等 · 失败静默 · 不回滚主 tx
//   - Sender 一次 HTTP · HMAC-SHA256 签名 · 8s timeout · 落 outbound_webhook_delivery 一行
//   - Retrier 后台每 5s 扫 status=pending + next_retry_at 到期 · 3 次后置 failed
//   - 事件枚举 4 种(new_keys_available / all_keys_dead / warranty_refund / boarded)
//
// **不做**：
//   - 不解析业务数据(payload 由调用方组好传进来)
//   - 不管重试策略字段(push_on_pull / retry_on_failure)· 那些是拉号 / 补车用的
//   - 不做 SSRF 校验(URL 校验在 downstream.ValidateTargetURL · 存的时候校验过了)
package webhookout

import (
	"context"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"time"

	"crypto/rand"
)

// EventType · 我方 → 乘客的四种事件类型(docs/05-api-contract §11)。
//
// **独立于 providers.EventType** —— 那是 vendor → 我方入向的事件枚举 ·
// 名字冲突但方向相反。别搞混。
type EventType string

const (
	// EventNewKeysAvailable · 拉号成功 · 号进车或进 record group
	EventNewKeysAvailable EventType = "new_keys_available"
	// EventAllKeysDead · 车里所有号都死了(bus-level · 只针对 bus)
	EventAllKeysDead EventType = "all_keys_dead"
	// EventWarrantyRefund · 质保退款到账
	EventWarrantyRefund EventType = "warranty_refund"
	// EventBoarded · 号已交付(handoff Complete / push_pool 成功)
	EventBoarded EventType = "boarded"
	// EventTest · 用户点"测试 webhook"按钮时发的
	EventTest EventType = "test"
)

// AllEventTypes 是白名单(前端复选框展示 · api handler 返给 GET /downstream/webhook)。
//
// **对齐 docs/05 §11** —— 别加 · 别改顺序(前端按下标存)。
var AllEventTypes = []EventType{
	EventNewKeysAvailable,
	EventAllKeysDead,
	EventWarrantyRefund,
	EventBoarded,
}

// Config · 装配 Dispatcher 需要的东西。零值走默认。
type Config struct {
	DB *sql.DB
	// Store · downstream.Store · 拉 URL + 解密 secret · Dispatcher 只从这里取
	Store DownstreamStore
	// HTTPX · 出向 http · 不 nil
	HTTPX HTTPDoer
	// Logger · nil = slog.Default
	Logger *slog.Logger
	// Timeout · 单次 POST 超时 · 默认 8s(docs/05 §11)
	Timeout time.Duration
	// MaxRetries · 3 次(docs/05 §11)
	MaxRetries int
	// Backoffs · 每次重试的等待 · 长度必须 = MaxRetries · 默认 3s/8s/20s
	Backoffs []time.Duration
	// QueueSize · buffered chan 长度 · 满时新入直接 dropped 落台账
	// 默认 1024 · 生产环境 · 4 触发源合流不会持续超过这个数
	QueueSize int
	// Now · 时钟 · 测试注入 · 默认 time.Now().UTC()
	Now func() time.Time
	// NewEventID · 事件 id 生成 · 测试注入 · 默认 UUID v7 兜底(见 newDefaultEventID)
	NewEventID func() string
}

// DownstreamStore · Dispatcher 只用的 downstream 接口(窄化便于测试)。
type DownstreamStore interface {
	Get(ctx context.Context, passengerID string) (DownstreamConfig, error)
	DecryptWebhookSecret(encrypted []byte) (string, error)
	InsertDelivery(ctx context.Context, a DeliveryAttempt) (DeliveryRow, error)
}

// DownstreamConfig 是 Dispatcher 需要的 downstream 字段子集(避免包依赖循环)。
// 装配层用 adapter 把 downstream.Config 转成这个。
type DownstreamConfig struct {
	PassengerID             string
	WebhookURL              string
	WebhookSecretEncrypted  []byte
	WebhookSecretConfigured bool
	// 1e-2 P0-1/2 · 用户显式启用开关 + 订阅事件白名单
	//   Enabled=false → 不发(不管其它条件)
	//   Events==nil → 全订阅(兜底) · 有值 → 只发列在里面的
	Enabled bool
	Events  []string
	// PushOnPull / BusOnly · 事件白名单过滤用
	PushOnPull   bool
	ResyncOnDead bool
	BusOnly      bool
}

// DeliveryAttempt · 一次投递台账入参(避免包依赖 downstream · adapter 转)
type DeliveryAttempt struct {
	PassengerID    string
	EventID        string
	EventType      string
	TargetURL      string
	Payload        string
	Attempt        int
	Status         string
	ResponseStatus *int
	ResponseSnip   string
	LatencyMs      *int
}

// DeliveryRow · InsertDelivery 返 · 只用 ID(重试时按 ID 找)
type DeliveryRow struct {
	ID string
}

// HTTPDoer · 出向 http · 只用 Do · 便于 mock(装配层传 internal/httpx.Client)
type HTTPDoer interface {
	// Do 走一次请求 · 返 status/body/latency · 上层不管重试
	// (retrier 自己按 next_retry_at 扫库)
	Do(ctx context.Context, req *HTTPReq) (*HTTPResp, error)
}

// HTTPReq / HTTPResp · 内部 http 抽象(不用 net/http 直接暴露给测试)
type HTTPReq struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

type HTTPResp struct {
	StatusCode int
	Body       []byte
	Header     map[string]string
}

// Dispatcher 主类型 · 内部 buffered chan 异步投递。
//
// **不阻塞主链** — Dispatch 立即返回 · 内部 goroutine 消费入队事件。
// 主链走完自己的事务 · 通知 Dispatcher · 不等结果。
type Dispatcher struct {
	cfg   Config
	queue chan queueItem
	// stopCh · 优雅退出信号
	stopCh chan struct{}
	// done · 内部 goroutine 退出确认
	done chan struct{}
	// retrierDone · retrier goroutine 退出确认
	retrierDone chan struct{}
	// logger · 默认 slog.Default
	logger *slog.Logger
}

// queueItem · Dispatch 入队的一条
type queueItem struct {
	passengerID string
	eventType   EventType
	payload     any
	// eventID 可以外部传(测试用) · 空的话 Dispatcher 自己生成
	eventID string
}

// New 建 Dispatcher · 默认值填齐。
//
// **不 Start** · 装配层显式调 Start(ctx) · 优雅退出走 Stop。
func New(cfg Config) *Dispatcher {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if len(cfg.Backoffs) == 0 {
		cfg.Backoffs = []time.Duration{
			3 * time.Second,
			8 * time.Second,
			20 * time.Second,
		}
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewEventID == nil {
		cfg.NewEventID = newDefaultEventID
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		cfg:         cfg,
		queue:       make(chan queueItem, cfg.QueueSize),
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
		retrierDone: make(chan struct{}),
		logger:      logger,
	}
}

// NewEventID · 外部装配层可以调这个生成 event_id(payload 顶层 冗余用)。
// Dispatcher 内部主流程也用它 · 保持一致。
func (d *Dispatcher) NewEventID() string {
	if d == nil || d.cfg.NewEventID == nil {
		return newDefaultEventID()
	}
	return d.cfg.NewEventID()
}

// Now · 外部装配层可能需要访问时钟(测试注入)。
func (d *Dispatcher) Now() time.Time {
	if d == nil || d.cfg.Now == nil {
		return time.Now().UTC()
	}
	return d.cfg.Now()
}

// newDefaultEventID · 32 位十六进制 · 跟 X-Idempotency-Key 一致的格式。
//
// 不用 UUID v7 · 减少包依赖(uuid.NewString 在 hostname 拉不到时会兜底 · 但仍然
// 有一次 syscall)。16 字节 CSPRNG + hex 足够唯一 · 对家侧 (event_id, attempt) 幂等。
func newDefaultEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
