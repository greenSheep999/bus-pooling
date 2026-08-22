// credplain/lookup.go · 实现 passengerpool.PlaintextLookup 接口
//
// 用途: pusher 走 push_pool 时 · 先从本地 credential_plaintext 表拿明文
// 找不到就返 ErrNotSupported · 让 pusher 走 placeholder 兜底(上游无 reveal · 挡不住)

package credplain

import (
	"context"
	"errors"

	"github.com/bus-pooling/bus-pooling/internal/delivery/passengerpool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// LookupAdapter · Store 实现 passengerpool.PlaintextLookup
type LookupAdapter struct {
	store *Store
}

// NewLookup · 返实现 PlaintextLookup 的 adapter
func NewLookup(s *Store) *LookupAdapter {
	return &LookupAdapter{store: s}
}

// FetchPlaintext · 批量查 credential_plaintext 表 · 全部拿到才返 · 缺一个整批失败
// pusher 侧再决定要不要走 placeholder 兜底
//
// **P1-1 修**(2026-08-22 · 审计):老代码直接把 Plaintext 的 refresh/access/api 三字段
// 全塞给 PushCredential · 若数据脏(某行两个字段都非空)vendor 会用不确定字段。
// 现在按 AuthMethod 走 canonical Credential + PushCredentialFrom · 只填一个明文字段。
func (a *LookupAdapter) FetchPlaintext(ctx context.Context, credentialIDs []string) (map[string]passengerpool.PushCredential, error) {
	if a == nil || a.store == nil {
		return nil, errors.New("credplain.LookupAdapter: store 未装配")
	}
	out := make(map[string]passengerpool.PushCredential, len(credentialIDs))
	for _, id := range credentialIDs {
		p, err := a.store.Get(ctx, id)
		if err != nil {
			// 缺一个 · 整批走上层兜底(placeholder) · 别只推部分
			return nil, err
		}
		// 转 canonical Credential · 走 PushCredentialFrom 按 AuthMethod 分派一个字段
		cred := providers.Credential{
			AuthMethod:   providers.AuthMethod(p.AuthMethod),
			RefreshToken: p.RefreshToken,
			AccessToken:  p.AccessToken,
			KiroAPIKey:   p.KiroAPIKey,
			Email:        p.Email,
		}
		// vendorLabel 空 · pusher 内部会填 · 这里只负责明文
		out[id] = passengerpool.PushCredentialFrom(cred, id, "")
	}
	return out, nil
}
