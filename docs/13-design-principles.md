# bus-pooling · 前端设计原则

> **本文来源**：概览页 v7（commit `15ff2b1`）经 20+ 轮用户反馈打磨出来的规范。
>
> **本文作用**：**统一后续页面**（拼车 · 提取 · 钱包 · 发车 · 设置 · 认证）的数据表达、交互、组件用法。**新写页面前必读**。
>
> **不写什么**：品牌色系 / 字号系统 / 阴影 token 见 `13-frontend-research.md`（外观规范）。**本文只讲用法。**

---

## 目录

1. [语言与量词](#1-语言与量词)
2. [时间格式](#2-时间格式)
3. [颜色语义](#3-颜色语义收紧的三色系统)
4. [Badge 三层](#4-badge-三层)
5. [数据颗粒度](#5-数据颗粒度)
6. [卡片系统](#6-卡片系统)
7. [表格](#7-表格)
8. [列表 + 空态 + 分页](#8-列表--空态--分页)
9. [下拉 / 弹窗](#9-下拉--弹窗)
10. [图表](#10-图表)
11. [页面级布局](#11-页面级布局)
12. [文案清单速查](#12-文案清单速查)

---

## 1. 语言与量词

### 1.1 内部 vs 对外

按 `CLAUDE.md §12.6` **严格双分离**：

- **代码里**（变量、类型、字段、日志）：`credential` / `pool` / `provider` / `vendor` / `housepool`
- **界面上**：号 / 号池 / 我的号池 / 车 / 拼车 / 拉号

**违反例**：

- ❌ UI 上写 "housepool 状态" → ✅ "号池状态"
- ❌ 代码里 `interface Key {}` → ✅ `interface Credential {}`

### 1.2 量词铁律

**「号」是名词、「个」是量词**。

- ❌ `12 号` · ✅ `12 个号`
- ❌ `10 号 key` · ✅ `10 个 key`
- ❌ `128 号可拉` · ✅ `128 个可拉`

**动作命名 vs 量词命名**：`extract`（提取 key）产出 **个 key**；`into_bus` / `push` 走的是 **个号**。同一个 credential 在不同动作里叫法不同 —— 前者强调"用户拿到的产物"（key 是 credential 里给用户用的凭据），后者强调"号池中流转"。

### 1.3 状态名 · 用户视角

内部 status 多态，对外收敛到"用户看了会做不同事"的 2~4 态。永远不用下游/技术层的词。

| 内部 | 对外 | 理由 |
|---|---|---|
| `status: dead` | 失效 | 「死」是内部技术词，用户读着不舒服 |
| `balance` | 剩余积分 | 「总余额」看不出是剩余还是消耗 |
| 图表名 | 用量趋势 | 「号池」是下游 kiro.rs 那层，不用 |
| `pool_status: down` | 号池已停运 | 三态 · 呼吸绿 / 静黄 / 静红 |

### 1.4 中英文空格

数字 + 中文之间加空格：`22 个号在池`、`128 个可拉`、`本月 42 次`。CJK-Latin 之间加空格是排版最佳实践。

---

## 2. 时间格式

**全站统一 `MM/DD HH:mm`**，11 字符等宽 + `tabular-nums` 保证列对齐。

```typescript
// src/lib/utils.ts
export function fmtTime(iso: string): string {
  const d = new Date(iso);
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mn = String(d.getMinutes()).padStart(2, "0");
  return `${mm}/${dd} ${hh}:${mn}`;
}
```

**为什么不做"今天省略日期"这类变体**：同列不同格式（`09:49` / `昨 22:16` / `7/28 10:20`）看着乱，无法一眼扫齐。

**动态时钟**（如概览页 hero）用 `useNowSecond` hook，读秒 tick + `tnum` 防抖：

```typescript
function useNowSecond() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}
```

---

## 3. 颜色语义（收紧的三色系统）

见 `tailwind.config.ts` colors block。

| 色 | Token | 语义 | 用于 |
|---|---|---|---|
| 绿 | `ok` / `credit` | 到账 · 积分 · 好 · 增长（好方向） | 剩余积分卡光晕 · header credit pill · +N 到账 · alive_rate ≥ 95% |
| 红 | `danger` | 花掉 · 失效 · 坏 · 增长（坏方向） | -N 花掉 · 号失效 badge · alive_rate < 88% · 消耗环比涨 |
| 黄 | `warn` | 中等 · 告急 · 提醒 | 号池告急 · 保修/补拉次数 > 0 · alive_rate 88~95% |
| 紫 | `brand` | 品牌 · 强调 · 主 CTA · 用户身份 | 「查看 →」链接 · nav tab active · 拼车 icon · 趋势图曲线 · 「我发起」badge |
| 灰 | `hairline` / `fg-tertiary` | 中性 · 辅助 · 空态 | 描边 · 副标 · 缺数据 `-` · 流转 badge 底 |

### 铁律：紫 ≠ 量级

紫是**品牌 / 身份**色，不能用在"多/少"的可视化。所有量级 Meter **三档统一**：

```typescript
value >= 阈值高 ? "#22C55E"   // 绿 · 好
  : value >= 阈值中 ? "#F59E0B" // 黄 · 中
    : "#EF4444"                  // 红 · 差
```

**为什么**：紫混进量级会跟"品牌强调"抢意义。用户看到紫段以为是"品牌钦定"，其实只是"中间档"。

### 符号语义

数字带符号 → 上语义色 + 加粗：

```typescript
// src/lib/utils.ts
export function signedToneClass(sign: "+" | "-" | ""): string {
  if (sign === "+") return "text-ok-fg";   // 绿
  if (sign === "-") return "text-danger-fg"; // 红
  return "text-fg";
}
```

**例外**：消耗指标（`spend_today` 的环比）**语义反转** —— 消费涨了对钱包是坏事，所以 `+41%` 用红、`-12%` 用绿。见 `Overview.tsx` 今日消费卡的对调逻辑。

---

## 4. Badge 三层

**三种 badge 三种语义 · 视觉必须分开**：

### 4.1 类型 badge · 语义色底

标"这是什么" —— 活动类型、状态、结果。

```tsx
<Chip tone="warn">提取</Chip>       // 语义色底 · 深色字
<Chip tone="brand">入车</Chip>
<Chip tone="danger">号失效</Chip>
<Chip tone="ok">充值</Chip>
```

规格：`text-label(12px)` · `py-0.5` · `rounded-md` · `whitespace-nowrap`

### 4.2 流转 badge · 中性灰 + 边框 + 微阴影

标"号从哪去哪" —— vendor → 车/号池，不带语义色。

```tsx
function FlowBadge({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex max-w-[160px] items-center gap-1 rounded-md
                     border border-hairline bg-bg-elevated px-2 py-[2px]
                     text-label font-medium text-fg-secondary shadow-card">
      <span className="truncate">{children}</span>
    </span>
  );
}
```

**为什么跟类型 badge 分开**：类型 = 分类信息（快速色彩扫描）· 流转 = 路径信息（准确读文字）。都上语义色会抢视觉。

### 4.3 状态小 pill · 10px 字号

标"这个东西的属性" —— 我发起、最优、缺货、阶段 3。

```tsx
<span className="inline-flex items-center rounded-md
                 bg-brand-subtle px-1.5 py-[1px]
                 text-[10px] font-semibold leading-[1.4] text-brand-strong">
  我发起
</span>
```

规格：**10px** 字号（比类型 badge 小一档）· `py-[1px]` · `rounded-md` · 语义色底但**尺寸最小**。

### 4.4 判定规则

- **有语义分类** → 类型 badge（tone: ok/warn/danger/brand/neutral）
- **是路径 / 引用** → 流转 badge（无 tone）
- **是属性标记** → 状态小 pill（tone 有但尺寸小）
- **不该套 badge**：内容是描述句、金额、事件叙述 → 直接文字

**违反例**：把「我发起」做成类型 badge → 会跟车名平级抢视觉。把 vendor 名做成类型 badge → 用户以为不同 vendor 有语义差异。

---

## 5. 数据颗粒度

### 5.1 KpiCard 结构

```
[icon] [Label]              ← 图标左边（不钉右上角）
[Value] [unit]               ← 大数字 + 灰单位
[sub 同类]         [subRight 异类]  ← 副行左右分栏
```

- **同类**（跟主数字同量纲）走 `sub` 左对齐
- **异类**（比率 / 时长 / 环比）走 `subRight` 右对齐

**例**：今日消费卡 —— 左「昨日 32（同为积分）」· 右「环比 +41%（比率异类）」。

### 5.2 数字加粗规则

数字类字段一律：
- `font-semibold`（加粗，跟叙述文字区分）
- `tnum`（tabular-nums，等宽对齐）
- 带 sign 时上 `signedToneClass`（+绿/-红）

在 sub 里嵌数字用 `<Num>` 组件：

```tsx
function Num({ children, sign = "" }) {
  return (
    <span className={cn("font-semibold tnum", signedToneClass(sign))}>
      {children}
    </span>
  );
}

// 用法
<>本月 <Num sign="+">+{fmtCredits(topup)}</Num> · <Num sign="-">-{fmtCredits(spend)}</Num></>
```

### 5.3 量在句子里读，不孤立右靠

活动记录、日志、通知这类**叙述型内容**里，量词嵌在句子里：

- ✅ **共提取 2 个 key，从 Kiro Drop → 我的号池**
- ❌ Kiro Drop → 我的号池 ...（右侧孤立列）2 个号

理由：数字孤立右靠是**表格**思维，叙述行需要顺着读。表格才用列对齐。

### 5.4 空态用英文 `-`

**中文破折号 `—`**（U+2014）在 `tnum` 下宽度是数字 2 倍，撑坏列宽。全用 **英文 `-`**（U+002D）。

- ✅ `{value ?? "-"}`
- ❌ `{value ?? "—"}`

### 5.5 缺数据整行灰

`out_of_stock` / `null` 状态：**整行 fg-tertiary + 所有列 `-`**，不画空 Meter、不显示 `0` 冒充数据。

`0` 是有效数据（保修 0 次 = 真的一次没保修），跟"缺数据"不同。分开处理。

### 5.6 单位不能省

主数字后必带单位（灰色小字）：`1,245` **积分**、`128` **次**、`12` **个**、`42h`。

**表头也必带量纲**：不写「有效成本」写「积分/小时」；不写「成本」写「积分/时」。表副标补公式。

**图表标注也带单位**：`峰值 62 积分 · 07/18` 不写 `峰值 62 · 07/18`。切 tab 时单位跟着换（`credits → 积分` / `pulls → 次` / `lifespan → h`）。

---

## 6. 卡片系统

见 `src/components/ui/primitives.tsx` Card。

### 6.1 三态

| 类 | 用于 |
|---|---|
| `card` | 默认 · 白底 · 灰描边 · 极淡阴影 |
| `card-focal` | 主指标 · 白底 + 右上角紫光晕 · 一屏最多 1 个 |
| `card-focal-credit` | 积分类 focal · 白底 + 右上角绿光晕（跟 header credit pill 同色系） |

`card-focal` 后加变体的模式（`-credit` 结尾）：新增语义色 focal 时按这个规律。

### 6.2 Hover

`card-hover` 状态（自动或手动打开）：

- `translate-y-1`（上浮 4px）
- `shadow-hover` 中性黑阴影（**不用紫**，避免跟 focal 强调色抢）
- `border-color: rgb(0 0 0 / 0.10)` 灰边

**hover 只有当卡可点时才启用**。装饰性卡片（信息展示）不要 hover 效果。

### 6.3 可点整卡

`<Card to="/xxx">` 传 `to` prop → 整卡渲染成 `<Link>` + 自动 `card-hover`。

**规则**：
- 卡片可进二级页 → 整卡 Link · 内部「查看 →」降级成 span（避免 `<a>` 嵌套 `<a>`）
- 卡片是终点（无更详细的页）→ 不做 Link · 不加 hover

**违反例**：右上角一个「查看 →」小链接，卡片本身不可点 —— hover 效果就变成鼠标浮在卡片中间但点了没反应，糟糕体验。

### 6.4 图标位置

**图标放左边跟 label 并排**，不钉右上角。右上角留给渐变 / 状态徽标 / 「查看 →」。

**为什么**：右上角孤图标 + 白底大空间 = 视觉不平衡；跟隔壁 3 张业务卡（图标在左）不一致。

---

## 7. 表格

见概览页 Vendor 监测。

### 7.1 表头

- **量纲要写清**：`积分/小时` / `%` / `h` / `个`，不用抽象名（避「有效成本」写「积分/小时」）
- **对齐**：数字列 `text-center`（表格中）或 `text-right`（数据列末尾）· 文字列 `text-left`
- 列宽 shrink-0 + 具体像素（`w-14` / `w-24`）避免自动布局跳动

### 7.2 副标

写清 **表在监测什么 + 关键指标怎么算**：

- ✅ 「按 vendor 汇总的号池表现 · 单价 / 寿命 / 耐用度 / 存活率 一览」
- ❌ 「耐用度 = 每号平均消耗多少积分才挂」（只讲一列，忽略其他）

单列公式解释放列 hover 提示，不占副标。

### 7.3 结果导向的可视化

Meter 类字段必须**明确越大越好还是越小越好**：

- 「耐用度」= 每号消耗多少积分 · **越大越扛用**
- 「存活率」= % · **越高越好**
- 「保修次数」= 30 分钟内挂被退款次数 · **越少越好**

不用「有效成本」这种量纲模糊的名字（成本涨了是好是坏？跟视角有关）。

### 7.4 排名色标

第 1 行用 `bg-ok-bg + text-ok-fg` 编号 + `Chip tone="ok"` 徽章（如「最优」）·  缺货行用 `danger` 系。中间行用 `bg-bg-elevated + text-fg-tertiary` 中性。

### 7.5 数据来源脚注

表底部灰色小字（`text-[11px] text-fg-tertiary`）写清**哪些指标从哪来**。让用户能判断"这不是 vendor 自吹，是我方系统聚合的"。

例：`数据来源：单价 / 寿命 / 耐用度 / 存活率 综合自 vendor 官方接口 与我方号池实测（近 30 天滚动平均）· 保修与补拉来自实际拉号记录`

---

## 8. 列表 + 空态 + 分页

### 8.1 首屏条数 + 加载更多

**无详情页的列表**（活动记录、拉号历史等），不做「全部 →」右上入口（死链接），改**底部「加载更多」按钮**：

```tsx
const STEP = 8;
const [shown, setShown] = useState(STEP);
const remain = Math.max(0, items.length - shown);

{remain > 0 && (
  <button
    onClick={() => setShown((s) => s + STEP)}
    className="rounded-lg border border-hairline bg-bg px-4 py-1.5
               font-medium text-fg-secondary shadow-card
               transition-colors hover:bg-bg-elevated"
  >
    加载更多 <span className="text-fg-tertiary">· 还剩 {remain} 条</span>
  </button>
)}
```

首屏 **8 条**，居中放"加载更多 · 还剩 N 条"。总数**仍在 sub 里注明**（`共 N 条`），用户知道全量。

### 8.2 有详情页的列表

有独立 `/xxx/history` 页 → 右上角可以放「全部 →」跳过去。**否则严禁死链接**。

### 8.3 空态叙述

未启用的功能（阶段 3+）用 `bg-bg-elevated` 底 + 灰化文字 + Chip 标阶段：

```tsx
<Card className="flex flex-col gap-4 bg-bg-elevated p-6">
  <div className="flex items-center justify-between">
    <div className="flex items-center gap-2.5">
      <span className="grid size-7 place-items-center rounded-lg bg-hairline">
        <Send className="size-3.5 text-fg-tertiary" />
      </span>
      <h3 className="text-body-lg font-semibold text-fg-tertiary">我的发车</h3>
    </div>
    <Chip tone="brand">阶段 3</Chip>
  </div>

  <Stat value="-" unit="未启用" size="num" />
  <p>绑定你的 AWS 账户 · 我方转发上游 vendor 开号 · 号池归你</p>

  {/* 空态字段列表 · 全 `-` */}
  <div className="mt-auto space-y-2.5 border-t border-hairline pt-3.5">
    {[...].map((t) => (
      <div className="flex items-center gap-2">
        <span className="size-[7px] rounded-full bg-hairline" />
        <span className="flex-1 text-fg-tertiary">{t}</span>
        <span className="text-fg-tertiary">-</span>
      </div>
    ))}
  </div>
</Card>
```

---

## 9. 下拉 / 弹窗

### 9.1 Trigger

- **最小宽度**：内容变化不缩水（`min-w-[200px]` 或够长选项字数）
- **微阴影**：用 `shadow-card`（同卡片默认阴影 token，别新造）
- **选中态明确**：标签展开告诉用户"选的是什么"（`全部 · 3 车 · 5 vendor` 而不是光「全部」）

### 9.2 Popover

- `w-64` 宽度上限
- `rounded-[14px]` 圆角比 `rounded-lg` 大一档（弹层视觉重）
- `border-hairline` + `shadow-pop`（阴影比 card 深）
- 外层套 `<div className="fixed inset-0 z-40" onClick={close} />` 做 backdrop 点击关闭

### 9.3 选项

- **文字左对齐 · 勾选右对齐**：`<span className="min-w-0 flex-1 truncate">{label}</span><Check className={picked ? "" : "invisible"} />`
- 用 `invisible` 保留占位（否则选中/未选文字左右跳）
- 未选项加 `disabled` + `opacity-45` + 副标"暂未开放"（如阶段 1a 只有中文时）

### 9.4 分组标题

小组标题（如「按车」「按 vendor」）用 macOS 菜单风格：

```tsx
<div className="px-3 pb-1 pt-2 text-[10px] font-medium
                uppercase tracking-wider text-fg-tertiary">
  按车
</div>
```

10px + uppercase + tracking-wider + fg-tertiary，跟选项字号明显区分。

### 9.5 二级菜单（flyout）

hover 或 click 展开，向**左**弹出（因为菜单本身贴右边缘）：

```tsx
<div className="absolute right-full top-0 z-50 mr-1 w-44
                rounded-[14px] border border-hairline bg-bg p-2 shadow-pop">
  {/* ... */}
</div>
```

---

## 10. 图表

见 `TrendChart.tsx`。

### 10.1 视觉基调

- **主曲线**：品牌紫 `#9147FF` · Area 型 · `type="monotone"` 平滑（避锯齿）
- **网格**：极浅灰 `#F2F2F2` · 只画水平线 · 无描边
- **轴文字**：`#A3A3A3` · 11px · 不上 currentColor（SVG 不稳）
- **平均虚线**：`#A3A3A3` `strokeDasharray="4 4"` · 比网格深一档（避打架）
- **峰值点**：品牌紫实心 + 白描边 + 上方带 label

### 10.2 Recharts focus outline

必修：Recharts 的 svg/g 元素被 focus 后有蓝色浏览器焦点框，全站关掉：

```css
/* index.css · 裸声明不放 @layer components（tailwind purge 会砍） */
.recharts-wrapper,
.recharts-wrapper *,
.recharts-surface,
.recharts-surface * {
  outline: none !important;
}
```

### 10.3 图例

图表下方居中，**必须**解释图上标注的语义（曲线 = 什么 · 虚线 = 什么 · 点 = 什么）：

```tsx
<div className="flex items-center justify-center gap-5 pt-3
                text-label text-fg-tertiary">
  <span className="flex items-center gap-1.5">
    <span className="inline-block h-[2px] w-4 bg-brand" />
    <span>当期用量</span>
  </span>
  <span className="flex items-center gap-1.5">
    <span className="inline-block h-[2px] w-4"
          style={{ background: "repeating-linear-gradient(...)" }} />
    <span>期间平均</span>
  </span>
  <span className="flex items-center gap-1.5">
    <span className="inline-block size-2 rounded-full bg-brand ring-2 ring-white" />
    <span>期间峰值</span>
  </span>
</div>
```

### 10.4 峰值 / 平均 label 带单位

**必须带单位**（跟 metric 切换联动）：

```typescript
`峰值 ${peak.value} ${unit} · ${peak.date.slice(5).replace("-", "/")}`
`平均 ${metric === "pulls" ? Math.round(avg) : avg.toFixed(1)} ${unit}`
```

`unit` 从 `UNIT` 表按 metric 派生：`credits → 积分` · `pulls → 次` · `lifespan → h`。

### 10.5 Margin

`AreaChart margin.top` 要足够放峰值 label（**至少 28px**）。默认 8px 会被裁。

### 10.6 Scope 筛选

图表可按维度切片时：**scope 下拉 + metric segmented 并排放右上**（不叠罗汉）。切换维度时曲线自动跟着变 · 平均 / 峰值都是当前 scope 下的重算值。

**为什么用下拉不用 Segmented**：scope 选项多（3 车 + 5 vendor + 全部 = 9 选项），Segmented 装不下；语言的选项是有限枚举，Segmented 适合 2~5 选项。

---

## 11. 页面级布局

### 11.1 顶栏对齐

Header 内层用 `px-gutter`（96px）跟 `main` 对齐。**不用 `px-8`**（32px 差 64px，logo 跟卡片对不上）。

### 11.2 Hero

```
[页标题 hero 32px]                  [状态 pill / 上排]
[日期时间 · 描述]                     [Segmented 全页时间维度 · 下排]
```

- 左列：`text-hero` 标题 + 灰字副行（动态时钟 + 加粗数字 + 空间叙述）
- 右列：两排右对齐（`flex flex-col items-end gap-2`）· 上放状态、下放时间范围

### 11.3 Section 头部

`<SectionHead title sub right>`：

- `items-start` 让 right 跟 title 顶对齐（不用 items-end 让 right 沉底）
- title 用 `text-section` · sub 用 `text-label text-fg-tertiary`
- sub 可以是 `ReactNode`（不仅 string）—— 里面能嵌 `<Num>` 加粗数字

### 11.4 网格

- 4 KPI：`grid grid-cols-4 gap-6`
- 3 业务卡：`grid grid-cols-3 gap-6`
- 表 + 环形：`grid grid-cols-[1fr_400px] gap-6`
- 页面最大宽度 `max-w-[1440px] mx-auto`

### 11.5 页面纵向节奏

模块之间用 `space-y-section`（56px）拉开距离，够呼吸不空旷。

---

## 12. 文案清单速查

前后端接口打通前，文案先写死在前端。所有"面向用户"的字符串按这个表：

### 12.1 动作动词

| 场景 | 用词 |
|---|---|
| 从 vendor 取号进 housepool | 拉号 |
| 号进入 bus | 入车 / 进车 |
| 号复制到 passengerpool | 推池 · 推 [目的地] |
| 号数据交给用户 + delete | 拿走 · handoff |
| bus 内号死后新拉一批 | 补车 |
| 号 30 分钟内挂被退款 | 保修 |
| 拉这家失败退到别家 | 补拉 · fallback |

### 12.2 状态词

| 内部 | 对外 |
|---|---|
| `alive` | 活着（隐去）/ 「N 个还活着」 |
| `dead` | 失效 |
| `pending` | 待派去向 · 待派 |
| `handed_off` | 拿走（列表隐藏，只出现在流水） |
| `out_of_stock` | 缺货 |
| kind: `single/anon/team` | 单人 bus / 匿名拼车 / 车队 |

### 12.3 单位

| 数据类 | 单位 |
|---|---|
| 积分数量 | `积分`（永远不用 credit / point） |
| 号数量 | `个号` |
| key 数量 | `个 key` |
| 拉号次数 | `次` |
| 时长（小时） | `h`（`< 48h`）· `d`（`>= 48h`） |
| 百分比 | `%` |
| 每小时消耗积分 | `积分/小时` |

### 12.4 界面副标 / 说明

**表 / 图表副标要点明"在监测什么"**，不要只讲一列公式：

- ✅ "按 vendor 汇总的号池表现 · 单价 / 寿命 / 耐用度 / 存活率 一览"
- ❌ "耐用度 = 每号平均消耗多少积分才挂"

**加下"数据来源"脚注**（表底 11px 灰字），让用户知道数据从哪汇聚：

- "数据来源：{列 1 / 列 2 / ...} 综合自 vendor 官方接口 与我方号池实测（近 30 天滚动平均）"

---

## 附录 · 参考实现

- **概览页**：`web/src/pages/Overview.tsx`（本文档所有原则的落地）
- **KpiCard 结构**：`web/src/components/KpiCard.tsx`
- **活动流 Row**：`web/src/components/rows.tsx`
- **TrendChart**：`web/src/components/TrendChart.tsx`
- **Primitives**：`web/src/components/ui/primitives.tsx`（Card / SectionHead / Chip / Meter / Segmented / Stat / Label / Muted）
- **Utils**：`web/src/lib/utils.ts`（`fmtTime` / `fmtCredits` / `fmtDelta` / `fmtK` / `fmtLifespan` / `signedToneClass` / `avatarColor`）

**写新页面时**：先读本文档，再抄概览页的对应结构，最后按业务需要微调。**不要重新发明轮子**。

---

**更新历史**

- 2026-08-08 · 概览页 v7 定型后首次沉淀（commit `15ff2b1`）
