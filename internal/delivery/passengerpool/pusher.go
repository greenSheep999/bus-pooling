package passengerpool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/delivery/passengerpool/kirors"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

// DownstreamStore 是 Pusher 用到的 downstream.Store 子接口(窄化便于测试)。
type DownstreamStore interface {
	Get(ctx context.Context, passengerID string) (downstream.Config, error)
	DecryptPassengerpoolToken(encrypted []byte) (string, error)
}

// PlaintextLookup 是**明文缺口**的抽象(等 housepool 后端 reveal 端点)。
//
// 装配层可以传：
//   - 生产：真调 housepool.HousePool.GetCredentialPlaintext(未来端点) · 返 refresh_token 等
//   - 联调(BP_ALLOW_PASSENGERPOOL_PLACEHOLDER=1)：返固定 "PLACEHOLDER" 三字段
//   - 测试：直接 mock 返固定明文
//
// 1e-1 阶段: housepool 后端 无 reveal 端点 · 装配层永远传 placeholderLookup ·
// **grep 关键字 PLACEHOLDER_PLAINTEXT 定位这里**·未来接了真明文端点后·装配层切实现。
type PlaintextLookup interface {
	// FetchPlaintext 拿这批本地 credential_id 的明文字段(refresh_token / access_token / kiro_api_key)。
	// 返回 map[credential_id]PushCredential · 缺失的 id 视为"号不存在" · 直接 fail。
	FetchPlaintext(ctx context.Context, credentialIDs []string) (map[string]PushCredential, error)
}

// PlaintextUsage · 推 / handoff 成功后通知明文缓存标 used_at(24h 后 janitor 删)
// 独立 interface · 因为 PlaintextLookup 是查 · 这是通知 · 语义分开
type PlaintextUsage interface {
	MarkUsed(ctx context.Context, credentialID string) error
}

// PusherDeps 是装配一个真实 Pusher 需要的依赖(main.go 用)。
type PusherDeps struct {
	Downstreams DownstreamStore
	// Plaintext 明文查询 · nil = 走 placeholder 兜底(见 EnvAllowPlaceholder)
	Plaintext PlaintextLookup
	// PlaintextUsage 明文用完通知 · nil = 不标 used_at(TTL 24h 自然清)
	PlaintextUsage PlaintextUsage
	// HTTPX 出向 http · 统一走 internal/httpx
	HTTPX *httpx.Client
	// DB 用来读 credential_ledger 拿 masked / vendor / region(帮 Pusher 拼元数据 · 免得每号查两遍)
	DB *sql.DB
	// Logger nil = slog.Default
	Logger *slog.Logger
}

// NewPusher 建一个真实的 Pusher · 装配层用。
//
// 任何字段缺失都不 panic —— nil 是有效的"未装配" · 具体缺失走 ErrNoTarget 兜底。
func NewPusher(deps PusherDeps) Pusher {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &realPusher{deps: deps}
}

// realPusher 是 Pusher 的实际实现。
type realPusher struct {
	deps PusherDeps
}

// Push 主流程 · 见 Pusher 接口注释。
//
// 六步：
//
//	① 拉 downstream cfg + 校验必要字段
//	② 解密 admin_token(用完扔)
//	③ 拿明文(真调 / placeholder)
//	④ 组装 PushCredential + 附上打码元数据
//	⑤ 建一次性 kirors.Client · BatchImport · 消费 SSE
//	⑥ 归错分类 → PushResult
func (p *realPusher) Push(ctx context.Context, passengerID string, creds []PushCredential) (*PushResult, error) {
	if len(creds) == 0 {
		return &PushResult{}, nil
	}
	if p.deps.Downstreams == nil {
		return nil, ErrNoTarget
	}

	// ① 拉 downstream cfg
	cfg, err := p.deps.Downstreams.Get(ctx, passengerID)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return nil, fmt.Errorf("passengerpool: 读 downstream cfg: %w", err)
	}
	if cfg.PassengerpoolURL == "" || !cfg.PassengerpoolTokenConfigured {
		return nil, ErrNoTarget
	}

	// ② 解密 admin_token · 明文只在此函数内活到 kirors.Close()
	adminKey, err := p.deps.Downstreams.DecryptPassengerpoolToken(cfg.PassengerpoolTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("passengerpool: 解密 admin_token: %w", err)
	}
	if adminKey == "" {
		return nil, ErrNoTarget
	}

	// ③ 拿明文(placeholder 兜底 · 生产接了 reveal 端点后改走真明文)
	credentialIDs := make([]string, 0, len(creds))
	for _, c := range creds {
		credentialIDs = append(credentialIDs, c.CredentialID)
	}
	plainMap, err := p.fetchPlaintext(ctx, credentialIDs)
	if err != nil {
		return nil, err
	}

	// ④ 组装 · 每号带上打码 label / region(元数据 · 不敏感)
	withPlain := make([]PushCredential, 0, len(creds))
	for _, c := range creds {
		pc, ok := plainMap[c.CredentialID]
		if !ok {
			// 明文缺失 · 单号 fail
			continue
		}
		// 拼元数据(email / region / label 走请求方传进来的)
		if c.Email != "" {
			pc.Email = c.Email
		}
		if c.Region != "" {
			pc.Region = c.Region
		}
		if len(c.Groups) > 0 {
			pc.Groups = c.Groups
		}
		if c.VendorLabel != "" {
			pc.VendorLabel = c.VendorLabel
		}
		pc.CredentialID = c.CredentialID
		withPlain = append(withPlain, pc)
	}
	if len(withPlain) == 0 {
		// 全部号都拿不到明文 · 视为整批失败
		out := &PushResult{}
		for _, c := range creds {
			out.Failed = append(out.Failed, FailedItem{
				CredentialID: c.CredentialID,
				Err: &PushError{
					Kind:    ErrKindBadRequest,
					Message: "号在本地找不到明文",
				},
			})
		}
		return out, nil
	}

	// ⑤ 建一次性 client · defer Close 保证明文扔掉
	client, err := kirors.New(kirors.Config{
		BaseURL:  cfg.PassengerpoolURL,
		AdminKey: adminKey,
	}, p.deps.HTTPX)
	adminKey = "" // 明文引用立刻扔 · 让 GC 后续回收 · Client 内还有一份 · 用完 Close
	if err != nil {
		return nil, fmt.Errorf("passengerpool: 建 client: %w", err)
	}
	defer client.Close()

	// PushCredential → kirors.ImportInput(打破依赖循环)
	inputs := make([]kirors.ImportInput, 0, len(withPlain))
	for _, c := range withPlain {
		inputs = append(inputs, kirors.ImportInput{
			CredentialID:  c.CredentialID,
			RefreshToken:  c.RefreshToken,
			AccessToken:   c.AccessToken,
			KiroAPIKey:    c.KiroAPIKey,
			Email:         c.Email,
			Region:        c.Region,
			Groups:        c.Groups,
			SourceChannel: c.VendorLabel,
		})
	}

	res, err := client.BatchImport(ctx, inputs)
	if err != nil {
		// HTTP 层 / 网络层 · 整批失败 · 每号一条 FailedItem
		var se *kirors.StreamError
		if errors.As(err, &se) {
			pe := streamErrorToPushError(se)
			out := &PushResult{}
			for _, c := range withPlain {
				out.Failed = append(out.Failed, FailedItem{
					CredentialID: c.CredentialID,
					Err:          pe,
				})
			}
			return out, nil
		}
		return nil, err
	}

	// ⑥ 归错分类 · 每号按 SSE 事件里的 status 分流
	result := p.classifyResult(withPlain, res)

	// ⑦ P1-i(2026-08-16) · 推成功的号标 credplain.used_at · 24h 后 janitor 硬删
	// duplicate 也视为成功(号在 k2a 已存在 · 我方明文可以清)
	if p.deps.PlaintextUsage != nil {
		markCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for _, id := range result.Success {
			if err := p.deps.PlaintextUsage.MarkUsed(markCtx, id); err != nil {
				p.deps.Logger.Warn("passengerpool.MarkUsed 失败·janitor 24h 后 TTL 兜底",
					"credential_id", id, "err", err)
			}
		}
		for _, id := range result.Duplicate {
			if err := p.deps.PlaintextUsage.MarkUsed(markCtx, id); err != nil {
				p.deps.Logger.Warn("passengerpool.MarkUsed 失败·janitor 24h 后 TTL 兜底",
					"credential_id", id, "err", err)
			}
		}
	}
	return result, nil
}

// streamErrorToPushError 把 kirors.StreamError 翻译成对外脱敏的 PushError。
//
// 走 Kind 字符串映射(kirors 层独立类型 · 不引 passengerpool)。
func streamErrorToPushError(se *kirors.StreamError) *PushError {
	if se == nil {
		return nil
	}
	var kind ErrorKind
	switch se.Kind {
	case kirors.KindUnauthorized:
		kind = ErrKindUnauthorized
	case kirors.KindNotFound:
		kind = ErrKindNotFound
	case kirors.KindConflict:
		kind = ErrKindConflict
	case kirors.KindTimeout:
		kind = ErrKindTimeout
	case kirors.KindStreamBroken:
		kind = ErrKindStreamBroken
	default:
		kind = ErrKindBadRequest
	}
	return &PushError{
		Kind:    kind,
		Status:  se.Status,
		Message: se.Message,
	}
}

// classifyResult 从 SSE 消费结果映射回每号成功 / 失败 / duplicate。
//
// 对应关系：
//   - verified  → Success
//   - duplicate → Duplicate(视为成功 · handler 走 MarkPushSuccessTx)
//   - failed    → Failed (Err 从 wire.Error 拼出)
//   - 事件缺失(SSE 少一条)→ Failed(kind=stream_broken)
func (p *realPusher) classifyResult(creds []PushCredential, res *kirors.BatchImportResult) *PushResult {
	out := &PushResult{}
	seen := make(map[int]bool, len(creds))
	for _, ev := range res.PerIndex {
		if ev.Index < 0 || ev.Index >= len(creds) {
			continue
		}
		cid := creds[ev.Index].CredentialID
		seen[ev.Index] = true
		switch ev.Status {
		case "verified":
			out.Success = append(out.Success, cid)
		case "duplicate":
			out.Duplicate = append(out.Duplicate, cid)
		case "failed":
			// ev.Error 是对家吐的 raw · 里边可能带 HTTP 状态码 / 内部限额字段名 /
			// 内部函数名等术语 · **不透传**（CLAUDE §0.1/§12.6）· 归成人话
			out.Failed = append(out.Failed, FailedItem{
				CredentialID: cid,
				Err: &PushError{
					Kind:    ErrKindBadRequest,
					Message: userFacingPushMessage(ev.Error),
				},
			})
		default:
			// 未识别事件 · 内部诊断信息（ev.Status）不出对外 message
			out.Failed = append(out.Failed, FailedItem{
				CredentialID: cid,
				Err: &PushError{
					Kind:    ErrKindStreamBroken,
					Message: "推送中断 · 稍后可重试",
				},
			})
		}
	}
	// 事件里没提到的 · 走 stream_broken(可重试)
	for i, c := range creds {
		if !seen[i] {
			out.Failed = append(out.Failed, FailedItem{
				CredentialID: c.CredentialID,
				Err: &PushError{
					Kind:    ErrKindStreamBroken,
					Message: "对家未返此号的导入结果",
				},
			})
		}
	}
	return out
}

// fetchPlaintext 走 PlaintextLookup 拿明文 · nil 或占位开关开时走 placeholder。
//
// **明文缺口降级路径**(BP_ALLOW_PASSENGERPOOL_PLACEHOLDER=1):
// housepool 后端 reveal 端点未接 · Pusher 用固定占位字符串填三字段 · 对家 mock 收到能识别。
// 生产 · 未来接了 reveal 端点后 · Plaintext 依赖注入真实实现 · 环境变量应关。
//
// grep 关键字:**PLACEHOLDER_PLAINTEXT** 定位这里 · 等 housepool 后端 reveal 端点后替换。
//
// **P0-c 修(2026-08-16)**: 走 credplain 时 · 某号找不到明文(未 seed 或 TTL 过期)
// 不硬 fail 整批 · 而是**该号走 placeholder** · 其它号仍用真明文。这样单号缺失不影响批。
func (p *realPusher) fetchPlaintext(ctx context.Context, credentialIDs []string) (map[string]PushCredential, error) {
	usePlaceholder := os.Getenv(EnvAllowPlaceholder) == "1" || p.deps.Plaintext == nil
	if usePlaceholder {
		// 占位路径 · **PLACEHOLDER_PLAINTEXT** · 明文缺口
		return placeholderPlaintext(credentialIDs), nil
	}
	// 走真实 lookup · 拿不到某号时该号走 placeholder(不硬 fail 整批)
	got, err := p.deps.Plaintext.FetchPlaintext(ctx, credentialIDs)
	if err != nil {
		// Lookup 整体失败(如 credplain 找不到任何一号)· 全 placeholder 兜底
		p.deps.Logger.Warn("passengerpool.fetchPlaintext: 走全 placeholder 兜底",
			"err", err, "count", len(credentialIDs))
		return placeholderPlaintext(credentialIDs), nil
	}
	// 部分号缺失时 · 补 placeholder
	if len(got) < len(credentialIDs) {
		filled := placeholderPlaintext(credentialIDs)
		for id, pc := range got {
			filled[id] = pc
		}
		return filled, nil
	}
	return got, nil
}

// placeholderPlaintext 联调用 · 每号返"PLACEHOLDER:<id>"三字段。
//
// **PLACEHOLDER_PLAINTEXT** · 跟 handoff 的 readHandoffPlaceholder 对称。
// 对家 mock 收到能一眼识别·不会误当成真号导入。
func placeholderPlaintext(credentialIDs []string) map[string]PushCredential {
	out := make(map[string]PushCredential, len(credentialIDs))
	for _, id := range credentialIDs {
		out[id] = PushCredential{
			CredentialID: id,
			RefreshToken: "PLACEHOLDER:not-a-real-token:" + id,
		}
	}
	return out
}

// HousepoolPlaintext 是 PlaintextLookup 的一个骨架实现(等 housepool 后端 reveal 端点)。
//
// **1e-1 阶段**：housepool 后端 无 reveal 端点 · 这个实现返 ErrNotSupported ·
// 装配层判断 · 走 placeholder 兜底。
//
// **未来接了 reveal 端点后**：把 FetchPlaintext 里的 return 换成真调
// pool.GetCredentialPlaintext · 别的都不动。**PLACEHOLDER_PLAINTEXT**
type HousepoolPlaintext struct {
	Pool housepool.HousePool
}

// FetchPlaintext 现阶段永远返 ErrNotSupported(明文缺口)。
// 装配层判断 · 用 placeholder 兜底。**PLACEHOLDER_PLAINTEXT**
func (h *HousepoolPlaintext) FetchPlaintext(_ context.Context, _ []string) (map[string]PushCredential, error) {
	return nil, &PushError{
		Kind:    ErrKindBadRequest,
		Message: "reveal 端点未接 · 等 housepool 加接口",
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// userFacingPushMessage 把对家吐的 raw error 归类成用户话。
//
// **为什么不透传** —— raw 里可能带 HTTP 状态码 / 内部限额字段名 / 内部函数名
// 等**内部术语**（CLAUDE §0.1/§12.6）· 直出到 toast 会给用户看到看不懂的字符串。
//
// 归类只按**结果对用户的意义**分类 —— 号在对家没收下，是「用完了」「假的/失效」
// 「网络断」还是「其他」· 让用户能立刻做下一步（换号 / 重试 / 联系我们）。
func userFacingPushMessage(raw string) string {
	if raw == "" {
		return "对家未接受此号"
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "quota_exceeded"),
		strings.Contains(lower, "monthly_request_count"),
		strings.Contains(lower, "reached the limit"),
		strings.Contains(raw, "402"),
		strings.Contains(lower, "payment required"):
		return "号已用完额度 · 换号或等 quota 重置"
	case strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "suspended"),
		strings.Contains(raw, "401"),
		strings.Contains(raw, "403"):
		return "号已失效 · 无法推送"
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "stream"),
		strings.Contains(lower, "network"):
		return "推送中断 · 稍后可重试"
	default:
		return "对家未接受此号"
	}
}
