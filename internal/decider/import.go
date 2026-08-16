package decider

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/marketstock"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ImportedCred · 导入结果 · 号池 id + 号池**回报的元数据**。
//
// 号池 BatchImport 的 SSE 事件里带 `subscription`（号的真实订阅档）· 这是
// **唯一权威来源** —— 多数 vendor 买前不给档位（实测某家 personal 池
// `?plan=` 参数被忽略）· 只能导入后从号池观察。
//
// 拿到就要一路带到 settle 落 credential_ledger.subscription（docs/24 §8 F1/F2）·
// 丢了的话 quota 判断和推下游都拿不到档位。
type ImportedCred struct {
	ID housepool.CredentialID
	// Subscription 号池回报的原始档位串（可能是 "Pro" / "KIRO PRO+" 等）·
	// **未归一** —— 归一在 settle 落库前做（providers.SubscriptionPlan.Valid()）
	Subscription string
	// Usage 号池回报的用量串（有则存 · 空表示号池没给）
	Usage string
	// StockItemID · 只手工池路径有值（marketstock.market_stock_item.id）
	// settle 里跟 credential_ledger INSERT 同 tx 调 SellTx · 完成 reserved→sold
	// 空表示不是手工池 · settle 不调 SellTx
	StockItemID string
	// Source · 手工池路径有值（offer.source · "这号谁提的"）· 前 6 家路径为空
	// settle 里落 credential_ledger.source · 如果 pending.Source 空 · 用这个兜底
	Source string
}

// importToPool 把 vendor 拉到的号导入号池指定 group，返回号池侧的 credential id + 元数据。
//
// duplicate 事件也算成功 —— 崩溃恢复走重放路径时，号池 BatchImport 幂等，
// 相同 KiroAPIKey 会返 duplicate + 原 CredentialID。忽略 duplicate 会让
// recoverImported 永远拿不到 id · imported→need_manual 死路。
// 只有 failed / rolled-back 事件才当失败跳过。
func (o *Orchestrator) importToPool(
	ctx context.Context,
	group string,
	purchase *providers.PurchaseResult,
) ([]housepool.CredentialID, error) {
	res, err := o.importToPoolWithMeta(ctx, group, purchase)
	if err != nil {
		return idsOf(res), err
	}
	return idsOf(res), nil
}

func idsOf(cs []ImportedCred) []housepool.CredentialID {
	out := make([]housepool.CredentialID, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// importToPoolWithMeta · 同 importToPool · 但保留号池回报的 subscription / usage。
//
// **分两条路径**（docs/24 §3 · Step 3f）：
//
//  1. 前 6 家 vendor → 号刚从上游 API 买来 · 还没进 housepool ·
//     走 BatchImport(verify=true) 落 prebuy-pool group（老路径）
//  2. 我方第 7 家 kiro_market → 号已在 housepool 里（运营导入时就进去了）·
//     PurchaseResult.Raw 塞了 marketstock.Meta · 识别到就跳过 BatchImport ·
//     直接把 credential_id 从 prebuy-pool 移到目标 group（bus-<id> / record-<pid>）
//
// 这一层分流让 orchestrator 的状态机不用感知 vendor 类别 · 上游是不是 API 拉的
// 只在这一层的实现细节里区别 · settle 拿到的 ImportedCred 完全同构。
func (o *Orchestrator) importToPoolWithMeta(
	ctx context.Context,
	group string,
	purchase *providers.PurchaseResult,
) ([]ImportedCred, error) {

	// 分流 · Raw 是 marketstock.Meta = 号已在池 · 只搬 group
	if meta, _ := marketstock.UnpackMeta(purchase.Raw); meta != nil {
		return o.moveMarketStockToGroup(ctx, group, purchase, meta)
	}
	// 老路径 · 前 6 家：BatchImport 到目标 group

	creds := make([]housepool.ImportCredential, 0, len(purchase.Keys))
	for _, k := range purchase.Keys {
		creds = append(creds, housepool.ImportCredential{
			// vendor 四件套映射到号池 · key 走 KiroAPIKey · account/password/issuer_url 走对应字段
			KiroAPIKey: k.Key,
			Email:      k.Account,
			IssuerURL:  k.IssuerURL,
			Region:     k.Region,
			Groups:     []string{group},
		})
	}

	result, err := o.pool.BatchImport(ctx, housepool.BatchImportRequest{
		Credentials: creds,
		Verify:      true, // 不验活的话导入的号可能一上线就是死的
	})
	if err != nil {
		return nil, fmt.Errorf("decider: 号池导入: %w", err)
	}

	ids := make([]ImportedCred, 0, len(purchase.Keys))
	for evt := range result.Events {
		switch evt.Status {
		case housepool.ImportStatusVerified, housepool.ImportStatusDuplicate:
			if evt.CredentialID != nil {
				// Subscription / Usage 是号池对这个号的实测回报 · 一路带到 settle
				ids = append(ids, ImportedCred{
					ID:           *evt.CredentialID,
					Subscription: evt.Subscription,
					Usage:        evt.Usage,
				})
			}
		}
	}
	// 排空 Summary，确保流关闭时 Err() 有值
	for range result.Summary {
	}
	if err := result.Err(); err != nil {
		return ids, fmt.Errorf("decider: 号池流中断: %w", err)
	}
	if len(ids) == 0 {
		return nil, errors.New("decider: 号池没有一个成功导入的号")
	}
	return ids, nil
}

// moveMarketStockToGroup · 手工池路径 · 号已在 prebuy-pool · 只改 group
//
// 跟 BatchImport 路径的差别:
//   - 号池侧: 老 credential 转 group（UpdateCredential(Groups=&[target])）· 不新导入
//   - store 侧: reserved → sold 的动作在 settle 里的同一个 tx 做（见 settleWithMeta）
//     这里只负责把 credential_id + stock_item_id 组好交给上层
//
// **为什么不在这里 sell**：sold 必须跟 credential_ledger 的 INSERT 在同一 tx ·
// 否则崩溃后 sweeper 会误释放 · 落 ledger 的地方（settle）才拿得到 tx。
func (o *Orchestrator) moveMarketStockToGroup(
	ctx context.Context,
	group string,
	purchase *providers.PurchaseResult,
	meta *marketstock.Meta,
) ([]ImportedCred, error) {
	// 保护性 · 一致性检查
	if len(meta.StockItemIDs) != len(purchase.Keys) {
		return nil, fmt.Errorf(
			"decider: market meta StockItemIDs(%d) 跟 Keys(%d) 不一致",
			len(meta.StockItemIDs), len(purchase.Keys))
	}

	out := make([]ImportedCred, 0, len(purchase.Keys))
	newGroups := []string{group}
	for i, k := range purchase.Keys {
		credID, err := parseCredentialID(k.Key)
		if err != nil {
			return nil, fmt.Errorf("decider: market Keys[%d] 解析 credential_id: %w", i, err)
		}
		// 只改 group · UpdateCredential 只写非 nil 字段 · 别的字段号池侧不动
		if err := o.pool.UpdateCredential(ctx, credID, housepool.CredentialPatch{
			Groups: &newGroups,
		}); err != nil {
			return nil, fmt.Errorf("decider: market 转 group[%d]: %w", i, err)
		}
		out = append(out, ImportedCred{
			ID: credID,
			// 手工池的档位权威源是**运营上架时选的 offer.Subscription** ·
			// 从 meta 拿 · 不用等号池 Balance 观察（那是应对上游返档位不准的情况）·
			// settle 里 normalizePlan 会再走一遍归一，认不出的还是存 NULL。
			Subscription: meta.Subscription,
			// StockItemID 塞在 ImportedCred 里让 settle 拿到 · 卖号事务需要
			StockItemID: meta.StockItemIDs[i],
			// Source 手工池 offer.source（"这号谁提的" · 运营配置）·
			// settle 里如果 pending.Source 为空 · 用这个兜底
			Source: meta.Source,
		})
	}
	return out, nil
}

// parseCredentialID · 从 KeyPayload.Key 里解析 credential id
// marketstock.Vendor 的 Purchase 把 credential_id 转字符串塞进 Key 字段（KeyPayloadJustKey 形态）
func parseCredentialID(s string) (housepool.CredentialID, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return housepool.CredentialID(v), nil
}
