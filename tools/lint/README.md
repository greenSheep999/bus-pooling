# tools/lint · CI 硬约束

## 内部术语泄漏检查（no_internal_terms.py）

**CLAUDE.md §0.1 / §0.2 / §7.3 / §11 / §12.6 硬约束**：

代码里（**注释 + 字符串字面量 + 日志 message + 错误 message**）绝不能出现下列会泄漏内部
服务组合 / 上游依赖 / 定价机制 / 资源命名的字面量：

1. **定价机制词**：加价 / 议价 / 分项（描述分层机制的时候）/ 收费点
2. **vendor 真名**：kiro91 / 91kiro / kiroceo / kiro.ceo / kirooo / kiro.ooo /
   kiroappio / kiroapp.io / kiroappcc / kiroapp.cc / kirodrop / drop.kiro.ss
3. **上游服务名**：kiro.rs / waffo / epusdt / bybit_internal / binance_internal
4. **明文 secret**：`sk-live-...` / `usr-...`（真 API key 长度） / `whsec-...`

## 允许出现的位置（白名单）

**语法层面无法自动区分 identifier 和泄漏字面量** —— 所以用白名单列出可命中的位置：

- Go 常量声明（`Vendor91Kiro VendorID = "kiro91"`）
- import 路径 · package 名 · struct field 名 · yaml tag / json tag
- env 名（`BP_VENDOR_KIRO91_API_KEY`）
- SQL migration 里的枚举值（`CHECK IN (...)`）· INSERT 时的 channel 字面量
- API path 段 / URL 参数
- vendor adapter 自身包内的 identifier 引用
- test 里的 SQL seed / URL / API key format

**加白名单需要 justification**（注释里说清为什么）。

## 使用

```bash
make lint-terms       # 只跑术语检查
make lint             # go vet + terms
make ci               # lint + test（CI 用）
```

`go test ./tools/lint/...` 也会跑一次 · 所以 `make test` 也会捎带检查。

## 违规时怎么修

- 定价词 → **分项** / 计费项
- vendor 真名 → **本 vendor** / **其他 vendor** / **vendor**
- 上游服务名 kiro.rs → **housepool 后端**
- 上游 gateway 名 → **支付网关** / 通道商

如果字面量确实是必要的 identifier · 加白名单到 `no_internal_terms.py` 的 `_ALLOWLIST`。

## 覆盖范围

扫 `internal/` + `cmd/` 下所有 `.go` 和 `.sql` 文件（包括 `_test.go`）。
排除 `vendor/` / `node_modules/` / `.git/`。
