package kirors

// ImportInput 是对家 batch-import 端点的一号入参(**kirors 自有类型**·不依赖上层 passengerpool)。
//
// 上层(Pusher)把 passengerpool.PushCredential 翻译成这个 struct。
// 打破 passengerpool ↔ kirors 的依赖循环。
type ImportInput struct {
	// CredentialID 只用来把 SSE 结果映射回上层的 credential_id ·
	// **不会**发给对家(对家不需要知道我方 id)。
	CredentialID string
	RefreshToken string
	AccessToken  string
	KiroAPIKey   string
	Email        string
	Region       string
	Groups       []string
	// SourceChannel 对家收到的 label · 用打码 label 不用 vendor 真名
	SourceChannel string
}

// toImportCredential 把上层的 ImportInput 翻译成对家的 wireImportCredential。
//
// 明文三字段在这里进入 wire struct · 序列化后走 HTTP body · **绝不**回落任何 log。
// verify 由 Client 主流程统一决定 · 不在 credential 级别。
func toImportCredential(c ImportInput) wireImportCredential {
	return wireImportCredential{
		RefreshToken:  c.RefreshToken,
		AccessToken:   c.AccessToken,
		KiroAPIKey:    c.KiroAPIKey,
		Email:         c.Email,
		Region:        c.Region,
		Groups:        c.Groups,
		SourceChannel: c.SourceChannel,
	}
}

// fromEvent 把 wire 事件翻译成外部用的 EventResult。
func fromEvent(w wireBatchImportEvent) EventResult {
	e := EventResult{Status: w.Status}
	if w.Index != nil {
		e.Index = *w.Index
	}
	if w.Error != nil {
		e.Error = *w.Error
	}
	e.Verified = w.Status == "verified"
	return e
}

// fromSummary 把 wire summary 翻译成外部用的 SummaryResult。
func fromSummary(w wireBatchImportSummary) SummaryResult {
	return SummaryResult{
		Total:      w.Total,
		Imported:   w.Imported,
		Verified:   w.Verified,
		Duplicate:  w.Duplicate,
		Failed:     w.Failed,
		RolledBack: w.RolledBack,
	}
}
