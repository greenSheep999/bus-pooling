# bus-pooling · 统一设计契约（每张 mockup 必须遵守）

## 产品性格 · 定位

**这是乘客端消费产品**，不是 admin dashboard。目标用户是"想快速拿号的普通用户"，不是运维/开发。

**性格**：
- 前卫、大胆、克制、有品
- 像 Apple 官网 / Cash App / Arc Browser / Perplexity 这类消费级产品
- **不像** Linear / Vercel / Grafana / 传统 admin dashboard

**严令禁止**：
- ❌ **左侧导航栏**（sidebar navigation）—— 任何页面都不许有
- ❌ Dashboard 感（六个数据卡、表格 8 列、状态 chip 一堆）
- ❌ 密集堆功能
- ❌ 装饰性图标 / 边框 / 阴影

## 布局形态 · 全局统一

- **顶部一条 64px 窄栏**（贯穿全站，唯一导航）
  - 左：Kiro 紫色 logo（一个"K"或圆形品牌图）
  - 中：4 个主入口 tab — **拼车 · 拉号 · 钱包 · 我**（当前 tab 用紫色文字 + 底部 2px 紫线，其余灰色）
  - 右：余额小 chip（"¥ 1,245"purple ghost，点击进 wallet），头像圆形（40px，首字母，灰底）
  - 右上角有一个几乎不可见的 "⌘K" 小 hint
- **主体全宽**，最大内容宽 1120px 居中
- **页边距 48px 起**（不用 32px）
- **卡片间距 40px 起**（不用 24px）
- **分组间距 64-96px**
- **一屏一焦点** —— 每页只回答用户 1 个问题
- 桌面主战场 1440×1024

## 视觉理念

1. **Apple / Arc 风** — 大字、大留白、素净卡片、微妙渐变
2. **单一 primary 紫色** #9147FF — 每屏最多 1 个紫按钮
3. **hero 渐变** — 只在首页顶部 + 空态；`radial-gradient(circle at 50% 0%, #9147FF33, transparent 70%)` 淡淡光晕
4. **无边框卡片** — 用 subtle bg (`#FAFAFA` 或 `#F5F0FF` 淡紫) 划分，不用 1px 灰边框
5. **无表格 · 用卡片流** — 号列表、拉号记录都是卡片，不是表格
6. **数值巨大** — 余额用 hero 48-64px semibold；重要数字总用 display-32+
7. **中文 UI**，字重只用 400 / 500 / 600
8. **圆角一致** — 卡片 16px（比默认 12px 大 → 更消费端）；按钮 10px；徽章 6px

## 品牌色 Token（严格用这些）

- `kiro-500 #9147FF` · 主品牌 · 主 CTA
- `kiro-700 #6420C7` · light 模式紫色文字
- `kiro-400 #A574FF` · dark 模式紫色
- `kiro-50  #F5F0FF` · light 模式 subtle bg（卡片底）
- `kiro-100 #EBE0FF` · light 模式 hover bg
- `kiro-950 #260C4F` · dark 模式 accent
- `brand-alpha-08 #9147FF14` · 极淡紫（hero 光晕、subtle 状态）
- `brand-alpha-20 #9147FF33` · active/selected bg

**渐变（每页最多 1 处）**：
- `linear-gradient(135deg, #9147FF, #6420C7)` — 主 CTA 特殊按钮 or 品牌 banner
- `radial-gradient(circle at 50% 0%, #9147FF33 0%, transparent 60%)` — hero 顶部光晕

## 字体阶梯（Inter 主 · 中文 PingFang SC / Microsoft YaHei 降级）

- xs 12 · sm 13 · body 15 · md 17 · lg 20 · xl 24 · display 32 · hero 48 · **giant 64**（余额用 giant）
- 字重 400 / 500 / 600 三档，不用 700

## 消费级 vs Dashboard 对照（严格遵守左列）

| ✅ 消费级（要） | ❌ Dashboard（不要） |
|---|---|
| 一屏一焦点 | 6 个数据卡横铺 |
| 卡片流 · 一号一卡 | 表格 8 列 |
| 一个纯色圆点标状态 | 一堆状态 chip |
| 巨大数字（余额 giant 64） | 中等数字 + label |
| hero 顶部渐变光晕 | 无光晕 · 全灰 |
| 顶部窄栏 4 tab | 左侧 sidebar 8 项 |
| 卡片间距 40px+ | 卡片间距 16px |
| 按钮内联费用（"拉 5 号 · ¥ 106"） | 独立预估栏 |
| 大按钮圆角 10px | 小按钮圆角 6px |
| 空态用 hero 光晕 + 一句话引导 | 空态放"暂无数据" |

## 交互 & 动效

- **⌘K Command Palette**：全局搜索 + 跳转（右上角有微 hint）
- **键盘优先**：主动作有快捷键
- **微动效**（**必做**，别偷懒）：
  - 卡片入场：300ms fade + 8px slide-up
  - 卡片 hover：4px 上浮 + subtle shadow（阴影颜色 `#9147FF14`）
  - 主 CTA 点击：60ms 缩到 96%
  - 数字变化：600ms tween（旧数字 fade-out + 新数字 fade-in）
  - Tab 切换：底部紫线 slide 200ms
  - 弹窗：从下方 slide-up + fade 400ms
- **无表格斑马纹 · 无边框卡片**
- **hover 变化极轻**：只用 bg 变浅紫 + 4px 上浮

## 状态展示（不可违反 · 只 2-3 态）

- 号：**"活" / "已失效"** 两态 · 用一个纯色圆点标（活=淡绿 · 失效=淡红），文字很小
- 车：**"活跃" / "已解散"**
- 拉号轮次：**"成功" / "部分成功" / "失败"**
- vendor：显示名（`91kiro`→"Kiro Market"），不显示内部 id

## 术语（禁用内部词）

**UI 里绝不出现**：
- `housepool` / `record group` / `provider` / `adapter` / `decider`
- `initiated` / `reserved` / `purchased` / `imported`
- `credential`（对乘客叫 **"号"**）
- `passenger`（对乘客不出现）

**UI 里允许**：车 · 号 · 拉号 · 我的号池 · 车友 · 补车 · 拿走

## 中文文案调性

- 简短、直接、无表情符号
- 不用感叹号 / 不用"哦""吧"
- 状态用名词短语（"已到账"）
- 按钮用动词（"拉号" / "建车" / "确认"）
- 引导话像"跟朋友说"（"选一辆车拉几个号" 而不是"请选择目标 bus"）

## 参考产品（照这些抄气质）

- **Apple 官网**（产品页）：大 hero + 大字 + 大留白
- **Apple Pay / Apple Health**：巨大数字余额 · 卡片流
- **Cash App**：金融消费的余额展示 · 大字动画
- **Arc Browser**：前卫大胆 · 微妙动效 · 品牌感
- **Perplexity / Poe**：AI 消费端的极简对话流
- **Framer 官网**：marketing 级视觉但产品化
- **Superhuman**：极简 + 键盘感

**照这些不要抄**：Linear / Vercel dashboard / Grafana / 任何 admin 后台

## 空态

**每页必有**。规范：
- 大标题（display 32）：一句话说"这里空的"（例："还没有车"）
- 副标题（body 15 灰）：一句引导（"建你的第一辆车，开始拉号"）
- 一个大 primary 紫色 CTA 按钮
- 顶部有淡淡的 hero 渐变光晕作装饰

## 常见反例（打回）

- ❌ 左侧导航栏（无论宽窄）
- ❌ 表格（除非绝对必要，如流水明细可考虑 · 且要极简无斑马纹无边框）
- ❌ 状态 chip 一堆颜色（活/失效只用一个圆点）
- ❌ 6 卡横铺 dashboard
- ❌ 多种圆角混（6+8+12+16 全上）
- ❌ 多个紫按钮抢焦点
- ❌ 装饰性渐变铺卡片
- ❌ 页边距小于 48px
- ❌ 卡片间距小于 40px

## 页面共有元素（顶部窄栏）

**每一张 mockup 都必须画这条 64px 顶栏**（登录/注册除外，那两页是 auth layout 无栏）：

```
[K logo]      拼车  拉号  钱包  我           [¥ 1,245] [D avatar]
                                                     [⌘K faint]
```

- Logo：22px 紫色圆角小方块 K，左侧 padding 32px
- Tabs：4 个中文，间距 32px，Medium 字重 15px；当前 tab 紫色 + 底部 2px 紫线；其余深灰
- 余额 chip：pill 形状 kiro-50 bg + kiro-700 文字，点击进钱包
- 头像：40px 圆形，首字母 D，灰底
- 分割：底部有 1px 极浅灰线 or 直接无（更 Apple）
- 高度 64px，右边距 32px

**当前活跃 tab** 根据页面：
- home / 建车 / bus detail / join bus → 拼车
- solo pull / pull records / assign / handoff → 拉号
- wallet → 钱包
- strategy / downstream / api-keys / profile → 我
