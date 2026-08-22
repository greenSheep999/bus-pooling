package decider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/credplain"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// settle 在**一个事务**里做完落账 + 落库 + 完成，这几件事必须原子：
//   - 把冻结转成消费（各分层各一笔 ledger）
//   - 差额释放回 balance（估价高于实扣时）
//   - 写 pull_round
//   - 写 credential_ledger 每号一行
//   - 累加当日计数
//   - 推进 pending_purchase → completed
//
// 一条也不能少 —— 少一条对账就废（比如少了 credential_ledger 那批号就成了孤号）。
func (o *Orchestrator) settle(
	ctx context.Context,
	pendingID string,
	pending Pending,
	purchase *providers.PurchaseResult,
	credIDs []housepool.CredentialID,
	reservePlan SplitPlan,
) (*PullResult, error) {
	// 无号池元数据的老调用路径（崩溃恢复）· 包成 ImportedCred 走同一实现
	cs := make([]ImportedCred, 0, len(credIDs))
	for _, id := range credIDs {
		cs = append(cs, ImportedCred{ID: id})
	}
	return o.settleWithMeta(ctx, pendingID, pending, purchase, cs, reservePlan)
}

// settleWithMeta · settle 的真实现 · 带号池回报的 subscription 元数据。
func (o *Orchestrator) settleWithMeta(
	ctx context.Context,
	pendingID string,
	pending Pending,
	purchase *providers.PurchaseResult,
	imported []ImportedCred,
	reservePlan SplitPlan,
) (*PullResult, error) {

	// 按 vendor 实扣的**实际单价**重算分项（估价可能偏低或偏高）
	actual := int64(purchase.Purchased)
	if actual <= 0 {
		return nil, fmt.Errorf("decider: 意外的 0 成交")
	}
	unitCost := purchase.TotalCost.Amount / actual
	// 1b P1-2B · 用 settle 上下文求 Rates（同 Pull 时）
	rates := o.resolveRates(ctx, RateContext{
		VendorID: pending.VendorID,
		Count:    purchase.Purchased,
	})
	bd := Price(unitCost, purchase.Purchased, rates)
	if !bd.Verify() {
		return nil, fmt.Errorf("decider: 分项之和 != 总额（不该发生）")
	}

	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 实扣总额按冻结方案的比例重新摊到各成员（1c 多人分摊）。
	//
	// 为什么按冻结比例摊而不是重新查 share_pct：冻结那一刻已经决定了"谁参与这轮"
	// （含谁被跳过），中间余额可能变过。用冻结比例保证"冻了多少的人扣多少"，
	// 释放差额也退到原冻结的人头上·不会张冠李戴。
	settlePlan, err := rescalePlan(reservePlan, pending, bd.Total, purchase.Purchased)
	if err != nil {
		return nil, err
	}

	pullRoundID := o.newID()

	// 逐人：差额退回 / 补冻 → 按分项 commit → 每日计数
	for _, part := range settlePlan.Participants {
		reservedForHim := reservePlan.AmountFor(part.PassengerID)
		if reservedForHim == 0 && len(reservePlan.Participants) == 0 {
			// reserve_split 缺失（老数据 / 单人快路径）· 退回按总额算
			reservedForHim = pending.ReservedAmount
		}
		if diff := reservedForHim - part.Amount; diff > 0 {
			if err := wallet.ReleaseReservedTx(ctx, tx, part.PassengerID, diff); err != nil {
				return nil, fmt.Errorf("decider: 释放差额(%s): %w", part.PassengerID, err)
			}
		} else if diff < 0 {
			// 实扣 > 冻结（混价单）· 差额从他余额再冻一份并直接消费
			if err := wallet.ReserveTx(ctx, tx, part.PassengerID, -diff); err != nil {
				return nil, fmt.Errorf("decider: 补冻结差额(%s): %w", part.PassengerID, err)
			}
		}

		// 该成员那份按分项落 ledger（每层一笔 · 对外映射成 spend）
		memberBD := scaleBreakdown(bd, part.Amount, bd.Total)
		if err := o.commitBreakdown(ctx, tx, part.PassengerID, pullRoundID, memberBD); err != nil {
			return nil, err
		}
		// 每日计数按各人实扣算（日花费上限是个人维度的）
		if err := wallet.BumpDailyTx(ctx, tx, part.PassengerID, 1, part.Amount); err != nil {
			return nil, err
		}
	}

	// pull_round（participants_split_json 记这轮谁分了几个号）
	if err := o.insertPullRound(
		ctx, tx, pullRoundID, pending, purchase, bd, settlePlan); err != nil {
		return nil, err
	}

	// I-27 · 落 pull_round_surcharge 每条 hit 一行(对账 / 申诉拆规则贡献用)。
	// hitsResolver nil / 表空 / env fallback 时返 nil · 跳过落库(兼容老部署)。
	if o.hitsResolver != nil {
		hits := o.hitsResolver.ResolveHits(ctx, RateContext{
			VendorID: pending.VendorID,
			Count:    purchase.Purchased,
		})
		if err := insertSurchargeHits(ctx, tx, pullRoundID, hits, bd, o.now()); err != nil {
			return nil, fmt.Errorf("decider: 落 pull_round_surcharge: %w", err)
		}
	}

	// credential_ledger 每号一行
	ledgerIDs, err := o.insertCredentials(ctx, tx, pending, pullRoundID, purchase, imported)
	if err != nil {
		return nil, err
	}

	// pending_purchase → completed（同事务的条件 UPDATE）
	if err := advancePendingTx(ctx, tx, pendingID, StatusImported, StatusCompleted,
		Fields{PullRoundID: pullRoundID}, o.now()); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 取扣后余额（读一次库，不放事务里避免拿到旧值）
	var balance int64
	if err := o.db.QueryRowContext(ctx,
		`SELECT balance FROM wallet WHERE passenger_id = ?`,
		pending.PassengerID).Scan(&balance); err != nil {
		return nil, err
	}

	return &PullResult{
		PullRoundID:      pullRoundID,
		VendorID:         pending.VendorID,
		Purchased:        purchase.Purchased,
		CredentialIDs:    ledgerIDs,
		UnitPrice:        bd.UnitPrice,
		ServiceFee:       bd.ServiceFee,
		TotalDebit:       bd.Total,
		BalanceRemaining: balance,
	}, nil
}

// rescalePlan 把冻结方案按实扣总额等比缩放 · 号数按新金额占比重分。
//
// 冻结用估价（可能偏高或偏低），实扣按 vendor 真实单价。参与人不变，
// 只是每人的金额等比例变化。余数给出钱最多的人（差几 microunit 不引小数）。
//
// reservePlan 为空（老数据 / 单人快路径没落 split）时退回"全归发起人"。
func rescalePlan(
	reservePlan SplitPlan, pending Pending, actualTotal int64, keys int,
) (SplitPlan, error) {
	if actualTotal <= 0 {
		return SplitPlan{}, fmt.Errorf("decider: 实扣总额必须 > 0")
	}
	// 没有分摊信息 → 单人语义
	if len(reservePlan.Participants) == 0 {
		return SplitPlan{
			Participants: []Participant{
				{PassengerID: pending.PassengerID, Amount: actualTotal, Keys: keys},
			},
			Total: actualTotal,
		}, nil
	}
	// 单人：直接给全额（避免等比缩放的整数误差）
	if len(reservePlan.Participants) == 1 {
		p := reservePlan.Participants[0]
		return SplitPlan{
			Participants: []Participant{
				{PassengerID: p.PassengerID, Amount: actualTotal, Keys: keys},
			},
			Skipped: reservePlan.Skipped,
			Total:   actualTotal,
		}, nil
	}

	reservedTotal := reservePlan.Total
	if reservedTotal <= 0 {
		return SplitPlan{}, fmt.Errorf("decider: 冻结总额异常: %d", reservedTotal)
	}

	parts := make([]Participant, len(reservePlan.Participants))
	var allocated int64
	for i, p := range reservePlan.Participants {
		a := actualTotal * p.Amount / reservedTotal
		parts[i] = Participant{PassengerID: p.PassengerID, Amount: a}
		allocated += a
	}
	// 余数给冻得最多的人
	if rem := actualTotal - allocated; rem != 0 {
		maxIdx := 0
		for i := range parts {
			if parts[i].Amount > parts[maxIdx].Amount {
				maxIdx = i
			}
		}
		parts[maxIdx].Amount += rem
	}

	// 号数按金额占比分 · 余数给出钱最多的
	if keys > 0 {
		assigned := 0
		for i := range parts {
			n := int(int64(keys) * parts[i].Amount / actualTotal)
			parts[i].Keys = n
			assigned += n
		}
		if rem := keys - assigned; rem > 0 {
			maxIdx := 0
			for i := range parts {
				if parts[i].Amount > parts[maxIdx].Amount {
					maxIdx = i
				}
			}
			parts[maxIdx].Keys += rem
		}
	}

	// 自检：钱一分不多不少
	var sum int64
	for _, p := range parts {
		sum += p.Amount
	}
	if sum != actualTotal {
		return SplitPlan{}, fmt.Errorf(
			"decider: 缩放后分摊之和 %d != 实扣 %d（不该发生）", sum, actualTotal)
	}

	return SplitPlan{
		Participants: parts,
		Skipped:      reservePlan.Skipped,
		Total:        actualTotal,
	}, nil
}

// scaleBreakdown 把整轮分项按某成员应付占比缩放成他那份的分项。
//
// 保证：缩放后各层之和 == 该成员应付额（余数补到 keyCost —— 它是最大项·
// 相对误差最小；补到 service_fee 会让费率看起来不对）。
func scaleBreakdown(bd Breakdown, memberAmount, roundTotal int64) Breakdown {
	if roundTotal <= 0 || memberAmount == roundTotal {
		return bd
	}
	scale := func(v int64) int64 { return v * memberAmount / roundTotal }
	out := Breakdown{
		UnitPrice:     bd.UnitPrice,
		keyCost:       scale(bd.keyCost),
		vendorFee:     scale(bd.vendorFee),
		regionFee:     scale(bd.regionFee),
		singlePullFee: scale(bd.singlePullFee),
		capabilityFee: scale(bd.capabilityFee),
		ServiceFee:    scale(bd.ServiceFee),
	}
	sum := out.keyCost + out.vendorFee + out.regionFee +
		out.singlePullFee + out.capabilityFee + out.ServiceFee
	// 余数补到 keyCost（最大项）· 保证分项之和 == 该成员应付
	out.keyCost += memberAmount - sum
	out.Total = memberAmount
	return out
}

// commitBreakdown 按分项落 wallet_ledger。每层非 0 才落一笔 —— 全 0 也没意义。
//
// 对外前端只看到 spend；对内每层单独一笔是**对账所需**（哪层收多少要能追）。
func (o *Orchestrator) commitBreakdown(
	ctx context.Context, tx *sql.Tx,
	passengerID, pullRoundID string,
	bd Breakdown,
) error {
	moves := []struct {
		reason wallet.Reason
		amount int64
	}{
		{wallet.ReasonKeyCost, bd.keyCost},
		{wallet.ReasonVendorFee, bd.vendorFee},
		{wallet.ReasonRegionFee, bd.regionFee},
		{wallet.ReasonSinglePullFee, bd.singlePullFee},
		{wallet.ReasonCapabilityFee, bd.capabilityFee},
		{wallet.ReasonServiceFee, bd.ServiceFee},
	}
	for _, m := range moves {
		if m.amount == 0 {
			continue
		}
		if _, err := wallet.CommitReservedTx(ctx, tx, wallet.Move{
			PassengerID: passengerID,
			Reason:      m.reason,
			Amount:      m.amount,
			RefType:     "pull_round",
			RefID:       pullRoundID,
		}); err != nil {
			return fmt.Errorf("decider: 落分项 %s: %w", m.reason, err)
		}
	}
	return nil
}

func (o *Orchestrator) insertPullRound(
	ctx context.Context, tx *sql.Tx,
	id string, pending Pending,
	purchase *providers.PurchaseResult, bd Breakdown,
	plan SplitPlan,
) error {
	rawResp, _ := json.Marshal(purchase.Raw)
	// 参与方分摊：多人车按实际分摊方案落 {passenger_id: 号数}
	// 单人 / 单独拉号时 plan 里就一条 = 老语义
	keysMap := plan.KeysMap()
	if len(keysMap) == 0 {
		keysMap = map[string]int{pending.PassengerID: purchase.Purchased}
	}
	split, _ := json.Marshal(keysMap)

	cols := bd.roundColumns()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, vendor_fee_total, region_fee_total,
		   single_pull_fee_total, capability_fee_total, service_fee_total,
		   participants_split_json, status, vendor_response_json, vendor_order_id,
		   created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, pending.VendorID, pending.ClientOrderID,
		nullIfEmpty(pending.BusID),
		pending.CountRequested, purchase.Purchased,
		cols[0], cols[1], cols[2], cols[3], cols[4], cols[5],
		string(split), "completed", string(rawResp),
		purchase.VendorOrderID,
		formatTime(o.now()), formatTime(o.now()))
	if err != nil {
		return fmt.Errorf("decider: 写 pull_round: %w", err)
	}
	return nil
}

// insertCredentials 每号一行 credential_ledger，返回我方 UUID 列表（对外派发用这个）。
//
// account_kind / subscription / source 三列（migration 046）：
//   - account_kind · 本轮买的哪种货架（企业/个人）· 来自 pending.AccountKind
//   - subscription · **号池回报的实测档位**（归一后）· 多数 vendor 买前不给档位 ·
//     所以这里的值来自 BatchImport 事件而非请求参数（docs/24 §5）
//   - source       · 号是谁提供的（用户视角的"来源"）· 空则不写
func (o *Orchestrator) insertCredentials(
	ctx context.Context, tx *sql.Tx,
	pending Pending, pullRoundID string,
	purchase *providers.PurchaseResult,
	imported []ImportedCred,
) ([]string, error) {
	// imported 长度可能 < purchase.Keys（号池部分失败）· 按短的走
	n := len(imported)
	if n > purchase.Purchased {
		n = purchase.Purchased
	}
	kind := string(pending.AccountKind.Normalize())
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := o.newID()
		var warrantyUntil any
		if purchase.Keys[i].WarrantyUntil != nil {
			warrantyUntil = formatTime(*purchase.Keys[i].WarrantyUntil)
		}
		// 号池回报的档位串归一 · 认不出的存 NULL（宁缺勿错 —— 档位错会让 quota 判断错）
		// 号池空 → 回落 pending.Plan（手动拉号里用户选的档 · 是"意图"档位）
		raw := imported[i].Subscription
		if raw == "" {
			raw = string(pending.Plan)
		}
		plan := normalizePlan(raw)
		var planVal any
		if plan != "" {
			planVal = string(plan)
		}
		// source · pending 优先（用户视角可能有别的口径）· 空则用 imported 兜底（手工池 offer.source）
		src := pending.Source
		if src == "" {
			src = imported[i].Source
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO credential_ledger
			  (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
			   current_group, vendor_id, vendor_order_id, source_pull_round_id,
			   status, disabled, pulled_at, warranty_until,
			   account_kind, subscription, source, key_masked)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'alive', 0, ?, ?, ?, ?, ?, ?)`,
			id, uint64(imported[i].ID),
			nullIfEmpty(pending.BusID),
			nullIfEmptyIfNoBus(pending.BusID, pending.PassengerID),
			pending.TargetGroup, pending.VendorID, purchase.VendorOrderID, pullRoundID,
			formatTime(o.now()), warrantyUntil,
			kind, planVal, nullIfEmpty(src),
			// key_masked 之前**根本没写** → 全空 → 前端待派列表渲染不出行 · 号派不了
			nullIfEmpty(imported[i].KeyMasked))
		if err != nil {
			return nil, fmt.Errorf("decider: 写 credential_ledger[%d]: %w", i, err)
		}
		// I-22 · 拉号真链路(前 6 家 BatchImport 路径)拉回来的号 · 同 tx 落 credplain(明文)。
		//
		// 老 bug:此处一直没接 · 明文全丢 · push_pool / handoff 走 placeholder 交付废号。
		// schema(043)+ credplain.SaveTx 早就写好 · 就这里没调。
		//
		// 手工池路径已经在下面 marketPopper 里覆盖 · 两条路径都落到 credplain 表 · 交付层
		// FetchPlaintext 统一读同一张表。
		if o.plaintextSaver != nil {
			if err := o.saveCredplainTx(ctx, tx, id, purchase.Keys[i]); err != nil {
				return nil, fmt.Errorf("decider: 落 credplain[%d]: %w", i, err)
			}
		}
		// 手工池路径:同 tx 里 reserved → sold（Step 3f · docs/24 §3）
		// 必须跟上面的 credential_ledger INSERT 同一 tx · 否则崩溃后 sweeper 会误释放：
		//   若 sold 单独 commit · ledger 已落但 sold 没落 → sweeper 5min 后把号放回 available →
		//   卖两次。同 tx 保证:要么两个都 commit · 要么两个都 rollback。
		if imported[i].StockItemID != "" {
			if o.marketStock == nil {
				return nil, fmt.Errorf(
					"decider: 号 %s 是手工池但 marketStock 未装配 · 不能同 tx 卖号", id)
			}
			if err := o.marketStock.SellTx(ctx, tx, imported[i].StockItemID, id); err != nil {
				return nil, fmt.Errorf("decider: market SellTx[%d]: %w", i, err)
			}
			// **I-01 修**：同 tx 里把明文从 market_stock_plaintext 迁到
			// credential_plaintext(kiro_rs_credential_id 主键 → credential_id 主键)。
			// popper nil 或找不到 stash(老导入的号 / TTL 过期)不 fatal · 号仍能卖 ·
			// 只是推池会走 placeholder(跟修复前行为一致 · 不新增回归)。
			if o.marketPopper != nil {
				if err := o.marketPopper.PopToCredplainTx(
					ctx, tx, uint64(imported[i].ID), id,
				); err != nil && !errors.Is(err, credplain.ErrNotFound) {
					// 真正的写库错才回滚 · ErrNotFound 只 slog(号是 I-01 修复前 stash 的老号)
					return nil, fmt.Errorf("decider: market PopToCredplainTx[%d]: %w", i, err)
				}
			}
		}
		out = append(out, id)
	}
	return out, nil
}

// insertSurchargeHits · I-27 · 每条命中规则一行 pull_round_surcharge。
//
// **amount 分摊**:每条规则的 amount(microunit) = 该规则在**该 kind 桶**里的占比 × 该 kind 桶实收总额。
// 简化实现:直接按 RateBp 占比分摊 kind 总额 · 跟 pricing.Engine.Eval 汇总口径一致。
//
// hits 空(env fallback / 表空)直接返 nil · 不落。跟 marketPopper.PopToCredplainTx 的 nil-safe 风格一致。
func insertSurchargeHits(
	ctx context.Context, tx *sql.Tx,
	pullRoundID string, hits []SurchargeHit, bd Breakdown, now time.Time,
) error {
	if len(hits) == 0 {
		return nil
	}
	// 按 kind 分组算总 rate_bp · 后面按占比摊 amount。
	kindBp := make(map[string]int64)
	for _, h := range hits {
		kindBp[h.Kind] += h.RateBp
	}
	// 每 kind 的实收总额(microunit) · 从 Breakdown 拆出来。
	kindAmount := func(kind string) int64 {
		switch kind {
		case "vendor":
			return bd.vendorFee
		case "zone":
			return bd.regionFee
		case "service":
			return bd.ServiceFee
		case "single_pull":
			return bd.singlePullFee
		case "capability", "retail", "adhoc":
			return bd.capabilityFee // 三种都归 capability 桶(pricing.go 落库口径)
		}
		return 0
	}
	nowStr := now.UTC().Format(time.RFC3339)
	for _, h := range hits {
		total := kindAmount(h.Kind)
		var amount int64
		if kindBp[h.Kind] > 0 {
			amount = total * h.RateBp / kindBp[h.Kind]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pull_round_surcharge
			  (pull_round_id, rule_id, rule_name, rule_kind, rate_bp, amount, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			pullRoundID, h.RuleID, h.RuleName, h.Kind, h.RateBp, amount, nowStr,
		); err != nil {
			return fmt.Errorf("INSERT pull_round_surcharge[%s]: %w", h.RuleID, err)
		}
	}
	return nil
}

// saveCredplainTx · I-22 · 拉号成功后同 tx 把号明文落 credential_plaintext。
//
// **I-35** · 走 canonical providers.Credential + credplain.SaveInputFrom 分派 ·
// 老 KeyPayload 通过 providers.FromKeyPayload 桥接。全项目 vendor→housepool→credplain→
// passengerpool 都走同一 canonical · 不再各自解构再重组。
//
// key 空 / 或校验错 = vendor 响应异常 · SaveInputFrom 返 err · settle tx 回滚(崩溃安全)。
func (o *Orchestrator) saveCredplainTx(
	ctx context.Context, tx *sql.Tx, credentialID string, k providers.KeyPayload,
) error {
	cred := providers.FromKeyPayload(k)
	in, err := credplain.SaveInputFrom(cred, credentialID)
	if err != nil {
		return err
	}
	return o.plaintextSaver.SaveTx(ctx, tx, in)
}

// normalizePlan · 号池回报的原始档位串 → 我方枚举。
//
// 号池可能返 "Pro" / "KIRO PRO+" / "pro_plus" 等各种写法（`docs/11-fields.md §1.1`）·
// 直接落库会让同一档位存成多个值 · quota 判断跟着错。认不出返 "" → 存 NULL。
func normalizePlan(raw string) providers.SubscriptionPlan {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	// 去掉品牌前缀和分隔符 · "KIRO PRO+" → "pro+"
	s = strings.TrimPrefix(s, "kiro")
	s = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)

	switch s {
	case "power":
		return providers.PlanPower
	case "pro":
		return providers.PlanPro
	case "pro+", "proplus":
		return providers.PlanProPlus
	case "promax":
		return providers.PlanProMax
	}
	return ""
}

// nullIfEmptyIfNoBus 只有单独拉号（BusID 空）时才把 owner_record_passenger_id 填上。
// schema 有 CHECK：号要么属车、要么属乘客的拉号记录，不能同时。
func nullIfEmptyIfNoBus(busID, passengerID string) any {
	if busID != "" {
		return nil
	}
	return passengerID
}

// advancePendingTx 事务内的条件推进（跟 Store.advance 逻辑同，但用现成 tx）。
func advancePendingTx(
	ctx context.Context, tx *sql.Tx,
	id string, from, to Status, set Fields, now time.Time,
) error {
	query := `UPDATE pending_purchase SET status = ?, updated_at = ?`
	args := []any{string(to), formatTime(now)}
	if set.VendorOrderID != "" {
		query += `, vendor_order_id = ?`
		args = append(args, set.VendorOrderID)
	}
	if set.PullRoundID != "" {
		query += `, pull_round_id = ?`
		args = append(args, set.PullRoundID)
	}
	if set.Error != "" {
		query += `, error = ?`
		args = append(args, set.Error)
	}
	query += ` WHERE id = ? AND status = ?`
	args = append(args, id, string(from))

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}
