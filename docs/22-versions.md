> **写这份的原因**（2026-08-14 · 凌晨用户拍桌）：
>
> 用户："我都不知道什么时候能上线 · 上线的第一版第二版第三版第四版有什么功能模块了 · 现在感觉第一版还差很多 · 还有很多竟然是 mock 假的！"
>
> 前几个月我一直在做**零散补丁** —— 到某个业务节点才发现 vendor 侧缺东西 · 修一下 · 下次又发现。
> 用户从 docs/13 开始一直在说"上游全拿回来 · 只是怎么用的问题" · 我没听懂。
>
> 这份就是"我不再一次一个字段地补" —— 按**产品链路**做版本路线图。

# 22 · 产品版本路线图（v1 / v2 / v3 / v4）

## 现状盘点（不装懂 · 用代码位置说话）

**真跑得通的链路**（`config.prod.yaml: dry_run:false`，非 mock）：
1. ✅ 充值 · gateway 真跳 · settlement webhook 真验签 · wallet_ledger 双条 recharge+channel_fee（`internal/topup/topup.go:411, 475-503`）· **但依赖 BP_GW_BASE 三个 env 齐 · 否则整条链废**
2. ✅ 单人拉号 · 真调 vendor purchase HTTP · AES 加密号入 credential_ledger（`internal/providers/kiro/vendors/kiro91/adapter.go:89`）
3. ✅ 号死质保退款 · deathwatch 5min 扫 · webhook 触发 · wallet_ledger 分摊入账（`internal/deathwatch/refund.go:143-227`）
4. ✅ 缺货挂单抢号 · restock 触发 fire（`decider/orchestrator.go:395`）
5. ✅ vendor 60s 探针 · /api/vendors/status 真数据无 mock 兜底

**假的 · mock 的 · 断链的**（v3/v4 之后重排 · 2026-08-14 收工快照）：
1. ❌ **多人 bus 拉号不合流** —— `coalescer.Anon/Team` 返 ErrNotImplemented（`internal/coalescer/coalescer.go:73,78`）· 现在多人 bus 是**各自独立 decider.Pull** · vendor 侧 N 次 API 调用 · **v2 才能拼**
2. ❌ **我方给 vendor 充值** —— 没 UI 没路由 · 全靠人工登 vendor 后台
3. ✅ **对账端点** —— `/api/admin/reconcile` v3.1 已装
4. ✅ **告警外发** —— v3.5 已装 · BP_ALERT_WEBHOOK 未设则只留 ERROR 日志（`internal/vendorview/alert_notifier.go`）
5. ✅ **自动补车 Step 2** —— v3.2 · deathwatch RefundOnce 后真调 decider.Pull（`internal/deathwatch/refill.go`）
6. ✅ **vendor 余额自动切换** —— v3.3 · BalanceChecker + Picker 组合 · 没钱切下一家（`internal/decider/orchestrator.go`）
7. ✅ **profile Balance 恒 0 bug** —— 之前 profileResp 结构错 · vendor 返 flat JSON 却按 nested 解 · 3 家 vendor 已修（v1 收官）
8. ✅ **涨价历史 / 乘客对账 / webhook 落价** —— v4.2 / v4.3 / v4.4 端点全通

## 三个核心场景 · 每个场景的完整链路

### 场景 A · "一个乘客充钱 → 拉一个号 → 用一段时间 → 号死 → 退款"（**孤客链**）

```
[前端] 充值 → [gateway] → [settlement webhook] → wallet_ledger
                                                       ↓
[前端] 拉号 → wallet.Reserve → decider → vendor.Purchase → credential_ledger
                                              ↓（vendor 侧扣钱）
                                          vendor_ledger（对账）
                                                       ↓
[乘客用] ... N 分钟后号死 → deathwatch 探到 dead → pull_round.status=refunded
                                                       ↓
[vendor webhook] warranty_refund → 我方 wallet_ledger.warranty_refund 入账退回
```

**链路完整性**：✅ 全通 · v1 可上（前提：gateway env 齐 + profile bug 全修）

**这个场景决定 v1 能不能开 · 因为没这个就不叫拼车服务**

### 场景 B · "多个乘客拼一辆车 · 一起拉一批号 · 分摊费用"（**拼车链 · 拼车这个业务的核心**）

```
[前端] 建 bus + 3 人邀请加入
[3 人] 各自点"拉号" → api/bus.go 各自跑 decider.Pull
                              ↓
                      vendor 侧 3 次独立 purchase 请求 · 各自扣各自的钱
                              ↓
                      号各自入 credential_ledger（bus_id 相同 · 但订单独立）
```

**链路断点**：**这就不叫拼车 · 是"三个人各买一个"** —— 用户明说"拼车没有拼车的样子"。

**要闭合**：`coalescer.Anon` / `Team` 真实现（意图池 + 窗口调度 + 合并 decider 请求）· 我方对 vendor 的一次 purchase 拉 3 个号 · 我方按人头分摊。这是 v1 还是 v2 决定的**关键分歧**。

### 场景 C · "我方运营视角"（**后台链**）

```
[管理员] 登后台 → 看每家 vendor 余额 → 快没钱就充值
                → 看 wallet_ledger vs vendor_ledger 有没有对不上
                → pipeline_health 有告警自动推
                → 死号趋势 · 质保退款率 · 号寿命曲线
```

**链路断点**：**基本没有** · Balance() adapter 有但 3 家返 0 · 对账靠 CLI · 无告警外发。

**这块决定"上线之后能不能运维"**。

## v1 / v2 / v3 / v4 版本规划（**推荐口径 · 待你拍板**）

我按"**用户最关心的价值 + 最低可运维**"排 · 不按代码模块。

### v1 · MVP · "孤客真跑通 + 每家 vendor 可观测"（**2-3 天到位**）

**能干嘛**：单个乘客充钱 → 拉号 → 用 → 号死自动退。**不做拼车**（多人 bus 就是"多个孤客加在一个 bus 里"临时策略）。

**必修**（不做上不了线）：
1. **kirodrop / kirooo / kiroappio profile 解析 bug**（Balance 恒 0 · P0 三条）
2. **kiroceo stock.Available 顶层聚合恒 0 bug**（decider 判有货偏错）
3. **kiroappio `key_revoked_abuse` webhook 未处理**（号被收回还当活 · 用户拿废号）
4. **kiro91 `reserved_keys_delivered` webhook 未处理**（签包量协议丢号 —— 现在没签就无风险 · 但 v1 要拦下不签保险）
5. **上游余额预检真开**（P5 已经在装配 · 但依赖 Balance() · 前面 profile bug 修完就自动激活）
6. **gateway 三 env 齐 · 部署验证**（P7 已有检查清单）
7. **backend admin 单页 · 就一屏**：显示每家 vendor 余额 + fleet 状态 + 最近 dispatch · 就 `/api/admin/data-health` 的可视化版

**v1 之后 · 用户能干**：真单人买号 · 号死真退款 · 我在后台能看 6 家 vendor 有没有钱

**v1 交付时间**：2-3 天（有 P0 bug + admin 页 · 其他都已经跑）

### v2 · 拼车真正拼起来 · **coalescer 真做**（**1 周**）

**能干嘛**：多人 bus 意图合流 · 一次 vendor purchase 拿 N 个号 · 按人头分摊。

**必做**：
1. `intent_pool` 表 + 窗口调度（500ms · 见 docs/21 §1）
2. `coalescer.Anon` / `Team` 真实现（不再 ErrNotImplemented）
3. API 层 · 拉号从"直调 decider.Pull"改成"写意图池 · 异步返 intent_id"
4. 前端 · UI 从"立即结果"改成"排队 · 稍后回调"
5. 测试重写（`coalescer_test.go` 全套改）

**v2 之后 · 用户能干**：真拼车省 vendor 侧钱 · 用户看到"跟另外 2 人一起买了 3 个号 · 每人分摊 33 积分"

### v3 · 运营闭环 · **后台管理 + 自动化补车**（**3-4 天**）

**能干嘛**：管理员看得清 · 告警收得到 · 号死了自动补。

**必做**：
1. 对账 dashboard 页 · wallet_ledger vs vendor_ledger 差异高亮
2. 告警外发 · pipeline_health 断了推 tg/email
3. 自动补车 Step 2（`docs/21 §2` · deathwatch RefundOnce 后 pending_refill 真调 decider.Pull）
4. vendor 余额自动切换（`docs/21 §3` · 没钱切下一家）
5. 号寿命 backfill · kirooo dispatch-log + kiroappcc `probeState`/`warrantyStatus` 落库（`docs/21 §5`）· AutoPick 打分从 50 常数变真实数据

### v4 · 精细化 · **前端展示 + 报表**（**3-5 天**）

**能干嘛**：给乘客看得清 · 给运营看得懂。

**必做**：
1. 前端 status 页展示"号寿命曲线 · 存活率 30d · 质保退款率"（现在字段有 · 数据有 · 只是没画）
2. 乘客对账页 · 我买过多少号 · 死了多少 · 退了多少
3. vendor 涨价历史（xi8 notifications 的 old_price_fen · docs/21 §4）
4. webhook 带的 price/stock 落库（`docs/21 §4` · 补 kiroappio/kiroappcc 实时价源）

## 用户核心问题的直答

**Q1 · "多少时间能上线"**
- v1（真的能收钱能拉号能退款 · 但不真拼车）：**2-3 天**（修 P0 bug + 装个 admin 单页）
- v2（真拼车拼起来）：**再 1 周**

**Q2 · "还有多少 mock"**
- 生产 · 除了 `coalescer.Anon/Team`（拼车合流 · 一整个功能没做）· **其他都是真代码**
- 本地 dev · `DRY_RUN=1` 兜底 · 生产 env 里没这个

**Q3 · "vendor → 我方 → 下游 链是不是通的"**
- **通的**：vendor purchase → credential_ledger → housepool → handoff → 死号 → deathwatch → wallet_ledger.warranty_refund 全是真代码
- **断的**：多人 bus 不合流（v2）· 我方给 vendor 充值靠人工登 vendor 后台（v3）

## 我为什么要停下来写这份 · 承认失误

之前每次做完一件 · 就问"下一件"—— 但下一件是"补哪个 vendor 字段"层面的东西 · 从来没抬到"整个场景闭没闭"层面。

用户已经跟我说 5+ 次："vendor 全接口拿回来 · 只是怎么用的问题" —— 我一直理解成"再多接几个端点" · 其实是"**别每次到某个业务点才发现漏 · 系统性一次拿全**"。这次是**版本视角 · 不是端点视角**。

这份路线图不再列具体端点缺口 · 那些放在 `docs/19-fields.md § 20-endpoint-classification.md § 21-1d-backlog.md` 里 —— 那三份是**素材** · 这份是**顺序**。

**建议早上你审这份 · 拍板 v1 范围** · 然后我按图施工 · 每完成一个 v 就 push 一次给你验收 · 不再"我又想到一个东西". 
