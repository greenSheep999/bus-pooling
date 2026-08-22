package providers

import "time"

// Credential · vendor 拉回来的号 · 全项目 canonical type。
//
// **权威源**:housepool 后端的 wireImportCredential 字段(见 internal/housepool/kirors/types.go)·
// 是"号数据的最上层契约"。我方 vendor adapter 只负责把 vendor 私有 JSON → Credential ·
// 下游层(housepool / credplain / passengerpool)从 Credential 各自派生入参 struct(见转换函数)。
//
// **Go 风格字段名**:IssuerURL(不是 IssuerUrl)·RefreshToken 等 · 只在 kirors 包做最终
// kebab/camel 名字映射(那个包本来就是 housepool 后端适配器)。
//
// **字段分类**:
//   - AuthMethod 决定 RefreshToken/AccessToken/KiroAPIKey 哪个非空
//   - Email/IssuerURL/StartURL/TokenEndpoint/Scopes/Region · 号自身元数据(SSO discovery)
//   - VendorKeyID/Paid/WarrantyUntil/Free · vendor 侧记账用(补拉 / 质保 / 混价单)
//
// **不放**:Groups/Priority/SourceChannel · 那是"号进哪个 group / 归哪个 channel"·
// 下游层决定(bus 或 record-<pid>)· 不属于 vendor 拉回来的号本身。转换函数收这些参数。
type Credential struct {
	// AuthMethod 决定 RefreshToken/AccessToken/KiroAPIKey 哪个字段有值。
	// 空 = 未声明(老 adapter 兼容)· 下游按 AuthAPIKey 兜底。
	AuthMethod AuthMethod

	// —— 鉴权字段(按 AuthMethod 三选一)——
	RefreshToken string // AuthRefreshToken 用(SSO refresh token · 可能 "sso:refresh" 冒号串)
	AccessToken  string // AuthBearer 用(短期 bearer · 当前无 vendor)
	KiroAPIKey   string // AuthAPIKey 用(ksk_...)

	// —— SSO 元数据(refresh_token 号导入号池后端要用)——
	Email         string
	IssuerURL     string
	StartURL      string
	TokenEndpoint string
	Scopes        string
	Region        string

	// —— vendor 记账元数据 ——
	VendorKeyID   string     // vendor 侧 key id(补拉 / 对账用)
	Paid          Money      // 这一把实际扣的(质保退款用)
	WarrantyUntil *time.Time // 空 = 无质保
	Free          bool
}

// ToKeyPayload · 过渡期兼容 · 让老代码继续读 KeyPayload。
//
// **I-35 迁移期**:5 家 vendor adapter 老代码返 KeyPayload · 我们不改老代码 · 用
// FromKeyPayload() 转过来。反向 ToKeyPayload() 让消费方逐层迁移 · 未迁的地方仍
// 能读 4-tuple 视图。
//
// 全部迁完后**删掉 KeyPayload 定义** · 单点收敛。
//
// **转换到下游 3 层**(housepool/credplain/passengerpool)的适配函数**放在各自包里** —
// 避免 providers 反向依赖那些下游包造成循环。见:
//   - housepool.ImportCredentialFrom(cred, groups) → wireImportCredential
//   - credplain.SaveInputFrom(cred, ledgerID) → SaveInput
//   - passengerpool.PushCredentialFrom(cred, ledgerID, vendorLabel) → PushCredential
func (c Credential) ToKeyPayload() KeyPayload {
	// KeyPayload 老字段:Key/Account/Password/IssuerURL/Region + AuthMethod(I-21 加)。
	// Key 按 AuthMethod 选:refresh_token 号 → RefreshToken · 其他 → KiroAPIKey。
	var key string
	switch c.AuthMethod {
	case AuthRefreshToken:
		key = c.RefreshToken
	case AuthBearer:
		key = c.AccessToken
	default:
		key = c.KiroAPIKey
	}
	return KeyPayload{
		VendorKeyID:   c.VendorKeyID,
		Key:           key,
		AuthMethod:    c.AuthMethod,
		Account:       c.Email, // 老 KeyPayload.Account 等于 SSO Email
		IssuerURL:     c.IssuerURL,
		Region:        c.Region,
		Paid:          c.Paid,
		WarrantyUntil: c.WarrantyUntil,
		Free:          c.Free,
	}
}

// FromKeyPayload · 老 vendor adapter 返 KeyPayload · 反向转 Credential 用。
//
// 迁移路径:decider 层拿到 []KeyPayload 后先转成 []Credential · 之后所有下游
// 都消费 Credential。等所有 vendor adapter 都改成直接返 Credential · 这个函数删掉。
func FromKeyPayload(k KeyPayload) Credential {
	c := Credential{
		VendorKeyID:   k.VendorKeyID,
		AuthMethod:    k.AuthMethod,
		Email:         k.Account,
		IssuerURL:     k.IssuerURL,
		Region:        k.Region,
		Paid:          k.Paid,
		WarrantyUntil: k.WarrantyUntil,
		Free:          k.Free,
	}
	// 空 AuthMethod = 老 4-tuple · 兜底 AuthAPIKey(跟 decider/import.go 分派兜底一致)
	authMethod := k.AuthMethod
	if authMethod == "" {
		authMethod = AuthAPIKey
		c.AuthMethod = authMethod
	}
	switch authMethod {
	case AuthRefreshToken:
		c.RefreshToken = k.Key
	case AuthBearer:
		c.AccessToken = k.Key
	default:
		c.KiroAPIKey = k.Key
	}
	return c
}
