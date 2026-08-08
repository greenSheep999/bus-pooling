package decider

import (
	"context"
	"errors"
	"fmt"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// importToPool 把 vendor 拉到的号导入号池指定 group，返回号池侧的 credential id。
//
// vendor 给的四件套 → 号池要的 ImportCredential 归一化。**只对号池成功导入的号返 id** ——
// duplicate / failed 都跳过，让上层按实际数处理（避免"我以为进池了，实际没有"）。
func (o *Orchestrator) importToPool(
	ctx context.Context,
	group string,
	purchase *providers.PurchaseResult,
) ([]housepool.CredentialID, error) {

	creds := make([]housepool.ImportCredential, 0, len(purchase.Keys))
	for _, k := range purchase.Keys {
		creds = append(creds, housepool.ImportCredential{
			// 91kiro 的四件套映射到号池 · ksk 走 KiroAPIKey，AWS Identity Center 三件套走对应字段
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

	ids := make([]housepool.CredentialID, 0, len(purchase.Keys))
	for evt := range result.Events {
		if evt.Status == housepool.ImportStatusVerified && evt.CredentialID != nil {
			ids = append(ids, *evt.CredentialID)
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
