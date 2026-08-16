package pricing

// surcharge 规则引擎（decisions §8.30 B）。
//
// 目标：不写费率到代码 · 全从 DB 读 · 按命中条件累加 rate_bp。
// 加新计费项 = INSERT 一行 · 改一条规则 = UPDATE · **绝不改代码里的费率**。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// SurchargeKind · 一条规则的类型。
type SurchargeKind string

const (
	KindVendor     SurchargeKind = "vendor"      // vendor 层附加（跟 vendor_pricing.vendor_surcharge_bp 二选一·这里更灵活）
	KindZone       SurchargeKind = "zone"        // 区域分项（zone=eu 单独计费之类）
	KindRetail     SurchargeKind = "retail"      // 零售分项（未 invited 用户按此分项）
	KindCapability SurchargeKind = "capability"  // 附加能力槽（1c+ 用户可选·1b 只放规则）
	KindAdhoc      SurchargeKind = "adhoc"       // 临时分项（活得特别长的车之类）
	KindService    SurchargeKind = "service"     // 服务费（原 Rates.Service · 迁到表）
	KindSinglePull SurchargeKind = "single_pull" // 单次分项（count==1 时·原 Rates.SinglePull）
)

// Rule · surcharge_rule 表一行。
type Rule struct {
	ID             string
	Kind           SurchargeKind
	Name           string
	RateBp         int64
	Base           string // key_cost | subtotal
	Active         bool
	AppliesWhen    Predicate
	WaivedWhen     Predicate
	UserSelectable bool
	Priority       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Predicate · 简化的 JSON 谓词（1b 只支持相等 / 数值比较）。
//
// 支持的 key：
//   - "vendor_id"       · string 相等
//   - "zone"            · string 相等
//   - "count"           · int 相等 · 或 {">":N} / {"<":N}
//   - "passenger.invited" · bool 相等
//   - "bus.avg_lifespan_h" · float · {">":N} / {"<":N}（1d 数据齐后才 meaningful）
//
// nil / 空 map = 无条件（applies_when 里 = 总命中·waived_when 里 = 从不减免）。
type Predicate map[string]any

// EvalContext · 谓词求值上下文·由调用方在拉号时填。
type EvalContext struct {
	VendorID         string
	Zone             string
	Count            int
	PassengerInvited bool
	BusAvgLifespanH  float64 // 0 = 无数据 · 该谓词命中不了 >0 条件
}

// Match · Predicate 对 ctx 求值。空 Predicate 视为"总命中"。
// 语义：所有 key 都需要匹配（AND）· 任一不匹配返 false。
func (p Predicate) Match(ctx EvalContext) bool {
	if len(p) == 0 {
		return true
	}
	for k, v := range p {
		if !matchOne(k, v, ctx) {
			return false
		}
	}
	return true
}

func matchOne(key string, want any, ctx EvalContext) bool {
	switch key {
	case "vendor_id":
		return asString(want) == ctx.VendorID
	case "zone":
		return asString(want) == ctx.Zone
	case "count":
		return matchNumeric(want, float64(ctx.Count))
	case "passenger.invited":
		if b, ok := want.(bool); ok {
			return b == ctx.PassengerInvited
		}
		return false
	case "bus.avg_lifespan_h":
		return matchNumeric(want, ctx.BusAvgLifespanH)
	default:
		// 未知 key · 保守返 false（不误命中）
		return false
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// matchNumeric · 支持 3 种写法：
//   - 数字直接量 · 相等
//   - {">":N}   · got > N
//   - {"<":N}   · got < N
func matchNumeric(want any, got float64) bool {
	switch v := want.(type) {
	case float64:
		return v == got
	case int:
		return float64(v) == got
	case int64:
		return float64(v) == got
	case map[string]any:
		if gt, ok := v[">"]; ok {
			return got > toFloat(gt)
		}
		if lt, ok := v["<"]; ok {
			return got < toFloat(lt)
		}
		if eq, ok := v["="]; ok {
			return got == toFloat(eq)
		}
	}
	return false
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

// ── Store ────────────────────────────────────────────

// SurchargeStore · surcharge_rule CRUD。
type SurchargeStore struct{ db *sql.DB }

func NewSurchargeStore(db *sql.DB) *SurchargeStore { return &SurchargeStore{db: db} }

// ListActive · 拿所有 active=1 的规则 · 按 priority 升序（低优先级先应用）。
// **不做过滤**：谓词求值让 Engine 做（同一次拉号复用规则集 · 避免每次都查 DB）。
func (s *SurchargeStore) ListActive(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, name, rate_bp, base, active,
		       applies_when_json, waived_when_json, user_selectable,
		       priority, created_at, updated_at
		  FROM surcharge_rule
		 WHERE active = 1
		 ORDER BY priority ASC, kind ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("pricing: 查 surcharge_rule: %w", err)
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var (
			r         Rule
			active    int
			userSel   int
			applies   sql.NullString
			waived    sql.NullString
			createdAt string
			updatedAt string
		)
		if err := rows.Scan(&r.ID, &r.Kind, &r.Name, &r.RateBp, &r.Base,
			&active, &applies, &waived, &userSel,
			&r.Priority, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		r.Active = active == 1
		r.UserSelectable = userSel == 1
		r.CreatedAt = parseTime(createdAt)
		r.UpdatedAt = parseTime(updatedAt)
		if applies.Valid && applies.String != "" {
			if err := json.Unmarshal([]byte(applies.String), &r.AppliesWhen); err != nil {
				return nil, fmt.Errorf("pricing: 解析 applies_when_json rule=%s: %w", r.Name, err)
			}
		}
		if waived.Valid && waived.String != "" {
			if err := json.Unmarshal([]byte(waived.String), &r.WaivedWhen); err != nil {
				return nil, fmt.Errorf("pricing: 解析 waived_when_json rule=%s: %w", r.Name, err)
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Upsert · 后台配置 / migration hook 用。
func (s *SurchargeStore) Upsert(ctx context.Context, r Rule) error {
	if r.ID == "" {
		return errors.New("pricing: Rule.ID 必填")
	}
	if r.Name == "" {
		return errors.New("pricing: Rule.Name 必填")
	}
	if r.Base == "" {
		r.Base = "key_cost"
	}
	if r.Priority == 0 {
		r.Priority = 100
	}
	appliesJSON, err := marshalPred(r.AppliesWhen)
	if err != nil {
		return err
	}
	waivedJSON, err := marshalPred(r.WaivedWhen)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO surcharge_rule
		  (id, kind, name, rate_bp, base, active, applies_when_json,
		   waived_when_json, user_selectable, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  kind              = excluded.kind,
		  name              = excluded.name,
		  rate_bp           = excluded.rate_bp,
		  base              = excluded.base,
		  active            = excluded.active,
		  applies_when_json = excluded.applies_when_json,
		  waived_when_json  = excluded.waived_when_json,
		  user_selectable   = excluded.user_selectable,
		  priority          = excluded.priority,
		  updated_at        = excluded.updated_at`,
		r.ID, string(r.Kind), r.Name, r.RateBp, r.Base,
		boolToInt(r.Active), appliesJSON, waivedJSON,
		boolToInt(r.UserSelectable), r.Priority, now, now)
	if err != nil {
		return fmt.Errorf("pricing: upsert surcharge_rule: %w", err)
	}
	return nil
}

func marshalPred(p Predicate) (any, error) {
	if len(p) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("pricing: 编码 predicate: %w", err)
	}
	return string(b), nil
}

// ── Engine ───────────────────────────────────────────

// Engine · 对一次拉号求值命中的规则集合 · 返回按 kind 汇总的 basis point。
//
// **1b 语义**：所有 active 规则并行求值 · 按 kind 桶汇总（同 kind 多条规则 rate_bp 相加）。
// 未来（1c+）想让某条规则"独占"某 kind · 加 exclusive flag。
type Engine struct {
	rules []Rule
}

// NewEngine · 从 store 拉规则并预处理 · 一次拉号复用一个 Engine。
func NewEngine(rules []Rule) *Engine {
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
	return &Engine{rules: rules}
}

// EvalResult · Engine 求值结果 · 按 kind 分组的 rate_bp 之和。
type EvalResult struct {
	Vendor     int64
	Zone       int64
	Retail     int64
	Capability int64
	Adhoc      int64
	Service    int64
	SinglePull int64
	// Hits · 具体命中的规则（供 pull_round_surcharge 落库）
	Hits []Hit
}

// Hit · 一条命中规则的快照。
type Hit struct {
	RuleID   string
	RuleName string
	Kind     SurchargeKind
	RateBp   int64
}

// Eval · 对给定 context 求值·返回按 kind 汇总的费率。
func (e *Engine) Eval(ctx EvalContext) EvalResult {
	var out EvalResult
	for _, r := range e.rules {
		if !r.Active {
			continue
		}
		if !r.AppliesWhen.Match(ctx) {
			continue
		}
		if len(r.WaivedWhen) > 0 && r.WaivedWhen.Match(ctx) {
			continue // 减免
		}
		out.Hits = append(out.Hits, Hit{
			RuleID: r.ID, RuleName: r.Name, Kind: r.Kind, RateBp: r.RateBp,
		})
		switch r.Kind {
		case KindVendor:
			out.Vendor += r.RateBp
		case KindZone:
			out.Zone += r.RateBp
		case KindRetail:
			out.Retail += r.RateBp
		case KindCapability:
			out.Capability += r.RateBp
		case KindAdhoc:
			out.Adhoc += r.RateBp
		case KindService:
			out.Service += r.RateBp
		case KindSinglePull:
			out.SinglePull += r.RateBp
		}
	}
	return out
}
