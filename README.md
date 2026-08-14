# bus-pooling

**多 provider 的号聚合与拼车中间层**。

一个乘客账号 → 接多家上游 provider 的 vendor（当前只做 kiro provider 下 6 家）→ 提供**聚合 / 比价 / Fallback / 拼车集单** 四大价值 → 号存我方 kiro.rs 号池 → 推乘客号池 / 拼车共享 / 乘客拿走。

**主入口是拼车**（最大价值），**次入口是单独拉号**（用户拉了自己派去向）。

**我们不是号商**：
- 不加价号成本（`pass-through`）
- 不做上游能做的（AWS 开号、vendor 内部号池、发车调度、vendor 拼车池）
- 不复刻号池能力（用 kiro.rs）
- **只收固定服务费**（每人每次拉号动作 1 元 / 1 USDT）
- 加**单次议价 20%**（`count == 1`；批量无议价）
- 加**通道费 5%**（waffo 收，`pass-through` 给乘客）

## 全景（一屏）

```
上游 6 家 vendor（外部）
  91kiro · kiro.ceo · kiro.ooo · kiroapp.io · kiroapp.cc · drop.kiro.ss
                       ↑ 我方 vendor adapter 拉号
                       │
     ┌─────────────────┴──────────────────┐
     │   bus-pooling · 中间层（本项目）    │
     │                                    │
     │   乘客账号 / 钱包 / 兑换码 / 支付   │
     │   策略引擎 / 集单调度 / 决策模型    │
     │   bus 实体 / 拉号记录 / webhook out │
     │                                    │
     └────────┬───────────────────────────┘
              ↓ BatchImport / PUT groups+disabled / DELETE
     ┌─────────────────────────────────────┐
     │   housepool（我方 kiro.rs）         │
     │   ─ credential 校验/探活/用量/并发   │
     │   ─ group: bus-<id> / record-<pid>  │
     └─────────────────────────────────────┘
                        ↓ 号出去（可选）
     ┌─────────────────────────────────────┐
     │   下游 · 3 种去向                    │
     │   ① 进车 (仍在 housepool 监控)       │
     │   ② 推乘客 kiro.rs (双写监控)        │
     │   ③ 拿走 handoff (离开系统)          │
     └─────────────────────────────────────┘
```

## 文档索引

**读文档顺序**（新人 / 未来 AI agent 都按这个顺序）：

| 序 | 文档 | 说明 |
|---|---|---|
| 0 | [`CLAUDE.md`](./CLAUDE.md) | 给未来 AI agent 的行为铁律（**动代码前必读**） |
| 1 | [`docs/00-values-and-phases.md`](./docs/00-values-and-phases.md) | 定位 · 三大价值 · 计费 · 3 阶段 · 业务规则 · 术语 |
| 2 | [`docs/01-architecture.md`](./docs/01-architecture.md) | 5 层分层 · 每层职责 · 目录清单 |
| 3 | [`docs/02-flows.md`](./docs/02-flows.md) | A-I 九条端到端技术时序 |
| 4 | [`docs/03-modules.md`](./docs/03-modules.md) | 业务包 15 上限 · 依赖图 · 模块清单 |
| 5 | [`docs/04-scenarios.md`](./docs/04-scenarios.md) | 31 用户场景路径（产品视角） |
| 6 | [`docs/decisions.md`](./docs/decisions.md) | 讨论过并否决的方案（**避免重复讨论**） |
| 7 | [`docs/vendors/*.md`](./docs/vendors/) | 6 家 vendor 官方 API 档案 |

**编码相关文档**（已就位）：`05-api-contract.md` · `06-db-schema.md` · `07-provider-contract.md` · `08-housepool-contract.md` · `09-transactions.md`（核心交易状态机）· `12-frontend-pages.md`（页面清单）· `13-frontend-research.md`（前端调研 + 主题）· `sprint-1a-frontend.md` + `sprint-1a-backend.md`（阶段 1a 前后端）。

**编码起手前尚待写**：`10-secrets.md`（阶段 1 落码时）· `11-testing-strategy.md`（首 e2e 前）。

**首上线前**：见 `docs/14-deployment.md`（systemd + Caddy + smoke test + backup）。

## 快速定位

**"我想找 X"**：

- 号最终去哪 → `04-scenarios.md`
- 具体一次拉号里都发生了什么 → `02-flows.md` C/D
- 单次议价怎么算 → `00-values-and-phases.md §3` + `04-scenarios.md D6/D7`
- housepool 是什么 · 为什么不能省 → `01-architecture.md §2 Layer 4`
- 某家 vendor 的 API 长啥样 → `docs/vendors/<vendor>.md`
- 我该写代码在哪个包 → `03-modules.md`
- 有个想法要不要做 → `decisions.md`（很可能讨论过并已否决）

## 三阶段（长期路线，见 `00-values-and-phases.md §7`）

| 阶段 | 内容 |
|---|---|
| **阶段 1** | MVP 拉存推：6 家 vendor + 主入口拼车 + 次入口单独拉号 + 自动补车 + 双写 + handoff |
| **阶段 2** | 邀请码组队 + 列队策略 + 压车治理 |
| **阶段 3** | 数据图表 + 发车（乘客 AWS 转发 vendor）+ 市场分成 |

## 开发

**栈**：Go 1.26 + SQLite（WAL 单节点）+ kiro.rs client · 前端 React 19 + Vite + Tailwind。

### 后端

```bash
# 首次：生成主密钥（AES-256-GCM · 加密 vendor 凭证和号池 token 用）
go run ./cmd/bus-pooling genkey        # 输出 BP_MASTER_KEY=...，存进 env 别进 git

cp config.example.yaml config.yaml     # config.yaml 已在 .gitignore
go run ./cmd/bus-pooling migrate up    # 建表（累计 27 张业务表 · 详见 docs/06-db-schema.md § Migration 顺序）
go run ./cmd/bus-pooling migrate status
BP_MASTER_KEY=<上面那个> go run ./cmd/bus-pooling serve

curl localhost:8080/healthz
```

其他子命令：`migrate down [n]` 回滚最近 n 个。

**`DRY_RUN` 默认 true** —— vendor 调用走 mock 不扣真钱。上线才显式 `DRY_RUN=0`。

### 前端

```bash
cd web
npm install
npm run dev        # localhost:3100 · 走 vite proxy 到后端 :8080
npm run build      # tsc -b + vite build
npm run lint
```

**MSW 假数据模式**（独立开发前端时用，不用起后端）：

```bash
VITE_USE_MOCK=1 npm run dev
```

默认（`VITE_USE_MOCK` 空或 `0`）走真后端 · fetch `/api/*` 经 vite proxy 转发到 `localhost:8080`。启动时浏览器 console 会打 `[bus-pooling] 真后端模式` 或 `MSW 已启用` 提示当前走哪边。

**前后端一起跑**（两个 tab）：

```bash
# tab 1
BP_MASTER_KEY=$(go run ./cmd/bus-pooling genkey | tr -d '\n') go run ./cmd/bus-pooling migrate up
BP_MASTER_KEY=... BP_INSECURE_COOKIE=1 go run ./cmd/bus-pooling serve

# tab 2
cd web && npm run dev
# 浏览器打开 http://localhost:3100
```

> **前端类型检查必须用 `npm run build`（内含 `tsc -b`）** —— 根 `tsconfig.json` 用
> project references 且 `files` 为空，单跑 `npx tsc --noEmit` 什么都不检查。

### 测试

```bash
go test ./...              # 后端
go test -race ./...        # 并发相关（wallet / 状态机）建议带 -race
bash tests/e2e/run-e2e.sh  # e2e 一键 · 幂等 / 5 并发 / kill 恢复
```

### 环境变量（后端）

| 环境变量 | 必需 | 说明 |
|---|---|---|
| `BP_MASTER_KEY` | ✅ | 32 字节 hex · AES-256-GCM 主密钥 · `bp genkey` 生成 |
| `BP_DB_PATH` | | SQLite 文件路径 · 默认 `data/bus-pooling.db` |
| `BP_ADDR` | | 监听地址 · 默认 `:8080` |
| `BP_INSECURE_COOKIE` | | 非空 = 允许 http 起 cookie（本地调试）· 生产必空 |
| `DRY_RUN` | | `0` = 走真 vendor + kiro.rs · 默认 true（mock） |
| `BP_ALLOW_LIVE_PULL` | | 跟 `DRY_RUN=0` 配套的第二道锁 · 都要显式设才拉真号 |
| `BP_STRICT_HANDOFF` | | `1` = handoff fulfill 拒占位明文（上线前必开·防降级） |
| `BP_ENABLE_DEV_TOPUP` | | `1` = 挂 `/api/internal/topup/{id}/paid` dev 端点（生产必空） |
| `BP_HOUSEPOOL_URL` | | kiro.rs 部署地址 · 例：`https://kiro.aibbq.xyz` |
| `BP_HOUSEPOOL_ADMIN_KEY` | | kiro.rs 的 admin API key |
| `BP_HOUSEPOOL_EXPECTED_VERSION` | | 绑 kiro.rs **语义版本**（不是 commit SHA · 上游未暴露 build sha）· 启动时对比 `GET /admin/system/update/check` 里的 `current_version` · 不等则拒启动 · 空 = 不校。兼容旧名 `BP_HOUSEPOOL_EXPECTED_SHA` |
| `BP_VENDOR_KIRO91_API_KEY` | 1a live | 91kiro 我方账户 API key (`usr-` 前缀) |
| `BP_VENDOR_KIRO91_ENABLED` | | 非空 = 注册 91kiro adapter |
| `BP_VENDOR_KIRO91_WEBHOOK_SECRET` | | 收 91kiro webhook 时验签用 |
| `BP_VENDOR_KIRODROP_WEBHOOK_SECRET` | | 收 kirodrop webhook 时验签用 |
| `BP_RATE_SERVICE_BP` | live 必需 | 服务费率 · 万分位（500 = 5%）· 零费率拒启动 |
| `BP_RATE_VENDOR_BP` `BP_RATE_REGION_BP` `BP_RATE_SINGLE_PULL_BP` | | 其他加价链层 · 阶段 1a 全 0 |
| `BP_GW_BASE` | 接 payment | 404bus-payment-gateway base URL · 例：`http://127.0.0.1:18099` |
| `BP_GW_TOKEN` | 接 payment | gateway `-add-client` 拿到的 bearer_token |
| `BP_GW_SETTLEMENT_SECRET` | 接 payment | gateway `-add-client` 拿到的 settlement_secret · HMAC-SHA256 key |
| `BP_GW_SUCCESS_URL` | | 可选 · waffo checkout 完成后回跳的前端 URL |

**结算回调**：装配 gateway 时 (`BP_GW_*` 三条齐)，会自动挂
`POST /api/hooks/paymentgw/settlement`。CLI 建 client 时 `settlement_url` 填这个路径。

### 目录

```
cmd/bus-pooling/     入口（serve / migrate / genkey）
internal/            后端包 · 上限 15 个业务包（见 §4.1 铁律）
  config db httpx secrets    基础设施（不算业务包）
web/                 前端 SPA
docs/                设计文档（改代码前先读，见下面文档索引）
```

## 项目原则（防止重蹈旧项目 kiro-auto 覆辙）

1. **业务包上限 15 个** —— 破了要写清理由（旧项目 90+ 模块崩了）
2. **一份文档一件事** —— 定位在 `00`、层次在 `01`、时序在 `02`、模块在 `03`、场景在 `04`；不叠加
3. **不做上游能做的** —— vendor 内部的号池、拼车、发车调度全不复刻
4. **每阶段有"不做清单"** —— 防蔓延
5. **术语铁律** —— 见 `CLAUDE.md`；作废术语（solo bus / 混合上车 / 方式 A/B / 交付方式 B 是 fire-and-forget）永远不用
6. **decisions.md 记讨论过并否决的方案** —— 未来 AI agent 不再回滚讨论
