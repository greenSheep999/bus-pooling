# bus-pooling · 前端设计契约

> **本文管什么**：前端设计**规范 + 主题系统 + 交互用法**。所有新页面开写前必读。
>
> **不管**：具体页面的功能清单（去 `12-frontend-pages.md`）· 具体页面独立设计方案（去 `20-page-extract.md` / `21-page-prices.md`）· 部署（去 `14-deployment.md`）。
>
> **合并来源**（2026-08-15）：
> - 前身一 · `13-design-principles.md`（996 行 · 概览页 v7 后打磨的用法规范）
> - 前身二 · `13-frontend-research.md`（436 行 · 调研 + 主题 + token）
>
> 合并后两份职责压到一处：调研结论 → 主题 token → 规范用法。
>
> ---


# Part I · 调研背景 + 主题系统 + Token

## 1. 从头对齐：不是给"框架"设计，是给"用户"设计

**旧项目 kiro-auto 的失败模式**（也是 SaaS 通病）：
- 组件库先行 → 页面按组件铺 → 用户面对一大堆没主次的功能面板
- **本项目反过来**：**用户行为先行 → 组件为其服务 → 组件是隐形的**

## 2. 调研结论：2026 年顶级 AI/Web 产品的共同点

综合 [Linear README](https://linear.app/readme)、[Raycast](https://manual.raycast.com/action-panel)、[Vercel](https://vercel.com/docs/dashboard-features/command-menu)、[Apple HIG 2025](https://developer.apple.com/videos/play/wwdc2025/356/)、[Claude Design](https://claude.com/blog/how-the-product-designer-who-built-claude-design-uses-it-to-explore-ideas-before-building-them)、[ChatGPT/Claude UI 对比 2025](https://intuitionlabs.ai/articles/conversational-ai-ui-comparison-2025?)、[B2B SaaS Design Trends 2026](https://procreator.design/blog/b2b-saas-design-trends-and-examples/)、[SaaS Dashboard Design Trends 2026](https://adminlte.io/blog/saas-dashboard-design-examples/)：

### 2.1 视觉层面的 7 条铁律（一致收敛）

| # | 铁律 | 引用来源 |
|---|---|---|
| 1 | **边框、阴影、装饰持续缩水**——层次靠**字重 + 间距**表达，不靠盒子 | [SaaS Dashboard 2026](https://adminlte.io/blog/saas-dashboard-design-examples/) |
| 2 | **颜色只用于状态和含义**（成功/失败/告警/主动作），**不用于装饰** | 同上 |
| 3 | **一种系统字体**（Apple: SF Pro / Web: Inter），**避免混排** | [Apple HIG Typography](https://developer-rno.apple.com/design/human-interface-guidelines/ios/visual-design/typography/) |
| 4 | **语义颜色 tokens** 自动适配 light/dark（`bgPrimary` / `fgMuted` 而不是 `#FFFFFF`） | [Coinbase Design System](https://cds.coinbase.com/getting-started/colors) |
| 5 | **Dynamic type scale**（Body 17 / 大标题 32-40，等比缩放） | [Apple HIG 2025](https://superdesign.dev/blog/apple-design-system) |
| 6 | **紫色单一 primary CTA**（Twitch #9147FF 就是这个用法：只用于主 CTA + 一个 banner） | [Twitch Design System](https://www.shadcn.io/design/twitch) |
| 7 | **点击目标 ≥ 44px**（触屏一致标准）| [Apple UI Tips](https://developer.apple.com/design/tips/) |

### 2.2 交互层面的 6 条铁律

| # | 铁律 | 引用来源 |
|---|---|---|
| 1 | **⌘K Command Palette** 已成 SaaS 通用信号——2026 用户在 Linear/Notion/Vercel 之间迁移，⌘K 是共同肌肉记忆 | [Rise of ⌘K 2026](https://www.saasframe.io/blog/the-rise-of-cmd-k-why-every-saas-needs-a-search-modal-in-2026) |
| 2 | **键盘优先**（Linear：每个动作都有快捷键，鼠标是备份） | [Linear README](https://linear.app/readme) |
| 3 | **Progressive Disclosure**（第一屏只放最常用；深处功能收起） | [Progressive Disclosure UX 2026](https://www.uxpin.com/studio/blog/what-is-progressive-disclosure/) · [Jakob Nielsen 2026](https://jakobnielsenphd.substack.com/p/progressive-disclosure) |
| 4 | **Calm UI**——不推动用户，不弹通知，不用 badges 骗点击 | [B2B SaaS 2026](https://procreator.design/blog/b2b-saas-design-trends-and-examples/) |
| 5 | **首屏 < 10s 见结果**（Datadog 标杆：<10s time-to-insight） | [B2B SaaS Dashboards](https://www.orbix.studio/blogs/b2b-saas-dashboard-design-examples) |
| 6 | **Role-based views**——不同用户看到不同默认（乘客不看 vendor / admin 才看） | [Dashboard Data-Heavy Patterns](https://uitop.design/blog/best-dashboard-design-patterns-for-data-heavy-saas-platforms/) |

### 2.3 AI 产品交互 3 条特有

综合 [Claude Design Anthropic](https://www.anthropic.com/news/claude-design-anthropic-labs) 和 [ChatGPT vs Claude 分析](https://aiuxplayground.substack.com/p/claude-vs-chatgpt-a-deep-dive-into)：

- **对话区左 + 结果区右** 的双列布局（Claude Design 就是这样）
- **可折叠侧栏**（历史对话 / 车列表 / 拉号记录 —— 对我方是这三类历史流）
- **实时反馈**：号价、比价结果不用点"刷新"，实时算实时展示（进度感）

## 3. bus-pooling 的设计原则（本项目独有）

### 3.1 三个"用户不动脑子"的锚点

结合 `CLAUDE.md §12` 的状态收敛 + Apple HIG："**用户不动脑子就会用**"落成 3 个具体锚点：

**锚点 1 · 第一屏答一个问题**
> "现在我能干嘛 / 现在我该干嘛？"

首页只回答这个，不放 6 个统计卡。**"建车""拉号"两个按钮 + 已有的车 + 余额。仅此。**

**锚点 2 · 每个动作只有一个"主"按钮**
> Apple HIG：**"一个屏幕一个主 action"**

例：车详情页 → 主按钮"拉号"。次按钮"解散 / 邀请"缩到菜单里。**永远不出现两个紫色按钮抢焦点**。

**锚点 3 · 状态只有 2-3 态**
> 见 `CLAUDE.md §12.5`

号只有"活/失效"，不显示 `preparing/live/dying`；bus 只有"活跃/已解散"。**Grep 内部枚举 = 0 命中**。

### 3.2 主次分明的 3 层信息架构

| 层 | 内容 | 屏幕位置 |
|---|---|---|
| **L1 · 主线** | 你现在关心的一件事（车里号数 / 余额 / 主动作） | 顶部 + 中央 |
| **L2 · 上下文** | 支持主线决策的信息（vendor 状态 / 单价） | 侧栏 or 展开 |
| **L3 · 治理** | 设置 / 策略 / 历史 / API key | 二级页 or 折叠 |

**Progressive Disclosure**：L1 永远在，L2 hover/expand 才现，L3 只在设置里。

### 3.3 反例（本项目**不这样做**）

- ❌ 首页 6 个数据卡（today calls / today errors / active credentials / ...）—— 用户不知道该看哪
- ❌ 每个数值旁边一个"?"tooltip 解释 —— 说明设计没收敛好
- ❌ 表格 12 列全展开 —— 用户滚动到麻木
- ❌ Tab 有 8 个 —— 说明这个页面装了 8 个不同的产品
- ❌ 弹窗里再套弹窗 —— 迷失路径

## 4. 主题系统 · 品牌色 9147FF

### 4.1 品牌基础色

**Kiro Purple `#9147FF`**（与 Twitch 品牌色一致；`RGB(145,71,255)` · `HSL(263,100%,64%)`）

参考 [Twitch Design System](https://www.shadcn.io/design/twitch) 和 [Beyond Purple](https://blog.twitch.tv/en/2019/12/03/beyond-purple)：**紫色只做主 CTA + 品牌 banner**，不铺装饰面。

### 4.2 完整色谱（12 阶，方便 dark/light 自适应）

```
kiro-50    #F5F0FF   最浅背景 · light bg accent
kiro-100   #EBE0FF   卡片背景 · light hover
kiro-200   #D7C2FF   border · light
kiro-300   #BC9CFF   dim primary · light
kiro-400   #A574FF   secondary
kiro-500   #9147FF   ★ 主品牌色 · CTA · brand
kiro-600   #7B2FEB   pressed / active state
kiro-700   #6420C7   dark
kiro-800   #4E1A9E   deep（rare）
kiro-900   #3A1478   deepest（logo bg）
kiro-950   #260C4F   dark bg accent · dark mode brand hover
kiro-1000  #14062B   dark mode 最深
```

**WCAG AA 合规**（`kiro-500 #9147FF` vs 各背景 · 参考 [WCAG 2025 Guide](https://allaccessible.org/blog/color-contrast-accessibility-wcag-guide-2025)）：

| 组合 | 对比度 | AA 通过 |
|---|---|---|
| `kiro-500` 上白字 | 3.9:1 | ✅ 大字 · ⚠️ 小字要用 `kiro-600` |
| 白底 `kiro-500` 文字 | 3.9:1 | ⚠️ 用 `kiro-700 #6420C7` 更安全（5.3:1） |
| 黑底 `kiro-400` 文字 | 6.8:1 | ✅ |
| `kiro-950` 底 `kiro-300` 字 | 8.2:1 | ✅ |

**结论**：**按钮填色用 `kiro-500`**，**紫色文字（链接、strong）用 `kiro-700`**（light）或 `kiro-300`（dark）。

### 4.3 品牌渐变 & 透明

```
brand-gradient-primary:   linear-gradient(135deg, #9147FF 0%, #6420C7 100%)
brand-gradient-subtle:    linear-gradient(135deg, #9147FF08 0%, #9147FF00 100%)   // 用于卡片顶部微光
brand-gradient-glow:      radial-gradient(circle at 50% 0%, #9147FF33 0%, transparent 70%)  // 首页 hero 光晕

brand-alpha-05:  #9147FF0D   (5%)   // subtle bg
brand-alpha-10:  #9147FF1A   (10%)  // hover bg
brand-alpha-20:  #9147FF33   (20%)  // active / selected bg
brand-alpha-50:  #9147FF80   (50%)  // disabled / secondary emphasis
```

**用法约束**：
- **不搞彩虹渐变**（旧项目就有这个毛病）
- **每屏最多 1 个渐变元素**（首页 hero 或空态图）
- **透明色只用于交互状态**（hover / selected / focus），不用于装饰

### 4.4 语义 tokens（不直接用 kiro-* 编号，用语义名）

```
// 前端代码里只 import 这些，不 import 具体 hex
--fg-primary        // 主文字（light: #0A0A0A · dark: #F5F5F5）
--fg-secondary      // 次文字（light: #525252 · dark: #A3A3A3）
--fg-muted          // 弱文字 / placeholder
--fg-disabled

--bg-canvas         // 页面底 bg
--bg-surface        // 卡片 bg
--bg-elevated       // 弹窗 / hover 卡
--bg-inset          // 输入框 bg

--border-subtle
--border-default
--border-strong

--brand             // = kiro-500 in light · kiro-400 in dark
--brand-hover       // = kiro-600 in light · kiro-300 in dark
--brand-active      // = kiro-700 in light · kiro-200 in dark
--brand-subtle-bg   // = brand-alpha-05 in light · brand-alpha-10 in dark
--brand-fg          // 紫色文字（light: kiro-700 · dark: kiro-300）

--success           // 号活 · 支付成功
--warning           // 号可疑失效 · 余额低
--danger            // 失败 · 号死
--info

--focus-ring        // 键盘 focus 用（半透明品牌色）
```

**换品牌色**：**只改 `--brand-*` tokens**，全站变。

### 4.5 通过 Provider 配置主题

```tsx
// web/src/theme/ThemeProvider.tsx
export const kiroTheme: Theme = {
  brand: {
    50: '#F5F0FF',
    // ... 12 阶
    500: '#9147FF',
    // ...
    1000: '#14062B',
  },
  gradient: {
    primary: 'linear-gradient(135deg, #9147FF 0%, #6420C7 100%)',
    subtle: 'linear-gradient(135deg, #9147FF08 0%, transparent 100%)',
    glow: 'radial-gradient(circle at 50% 0%, #9147FF33 0%, transparent 70%)',
  },
  radius: {
    sm: '6px',
    md: '8px',
    lg: '12px',
    xl: '16px',
    full: '9999px',
  },
  font: {
    sans: '"Inter", "SF Pro", system-ui, sans-serif',
    mono: '"JetBrains Mono", "SF Mono", monospace',
  },
  scale: {
    // Apple-style dynamic type
    xs: '12px',
    sm: '13px',
    body: '15px',      // 默认正文
    md: '17px',        // apple body
    lg: '20px',
    xl: '24px',
    display: '32px',
    hero: '48px',
  },
}
```

**换主题 = 换 Provider**：`<ThemeProvider theme={kiroTheme}>` 或 `<ThemeProvider theme={darkTheme}>` 或未来 `<ThemeProvider theme={cursorTheme}>`（阶段 3+ 加 provider 时）。

## 5. 字体系统

### 5.1 字体选择

**主字体**：`Inter`（Web 端 SF Pro 替身，支持中文降级到系统字体）

**中文降级链**：
```
Inter, "SF Pro SC", "Helvetica Neue", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif
```

**Mono**：`JetBrains Mono`（API key / credential prefix 显示用）

### 5.2 字号阶梯（跟 Apple Dynamic Type 对齐）

| Token | Web px | 用途 |
|---|---|---|
| `--text-xs` | 12 | 小徽章 · 元信息 |
| `--text-sm` | 13 | 表格次要列 · 说明文字 |
| `--text-body` | 15 | 正文（Web 默认 · 比 Apple 17 略小） |
| `--text-md` | 17 | 强调正文 · 卡片主字段 |
| `--text-lg` | 20 | 二级标题 |
| `--text-xl` | 24 | 一级标题 |
| `--text-display` | 32 | 大数字（余额显示） |
| `--text-hero` | 48 | 空态 / 首页大标题 · 单页最多 1 次 |

### 5.3 字重

- **400 Regular**：正文
- **500 Medium**：卡片标题 · 表格 header
- **600 Semibold**：页标题 · 强调
- **700 Bold**：不用（保留品牌 logo 场景）

**层次靠字重 + 间距**（不用色块 / 边框）—— 见 §2.1 第 1 条。

## 6. 间距 & 圆角

### 6.1 4/8 基础间距

```
--space-1: 4px
--space-2: 8px
--space-3: 12px
--space-4: 16px      // 卡片内 padding 默认
--space-5: 20px
--space-6: 24px      // 卡片之间间距默认
--space-8: 32px      // 分组之间
--space-12: 48px     // 页边距
--space-16: 64px     // 大区块之间
```

### 6.2 圆角

```
--radius-sm: 6px     // 徽章 · 小 chip
--radius-md: 8px     // 输入框 · 按钮
--radius-lg: 12px    // 卡片 · 弹窗（默认）
--radius-xl: 16px    // 大卡片 · 主 hero
--radius-full: 9999px // 头像 · 圆形徽章
```

**约束**：**同页面不混用超过 2 种圆角**（保持视觉一致）。

## 7. 交互模式规范

### 7.1 必备 ⌘K（Command Palette）

**核心**：任何用户手上有键盘就能 `⌘K` 触发全局搜索 + 动作。

**能触发的动作**：
- 跳转任何页面（`⌘K 建车` 即触发 `/buses/new`）
- 拉号（`⌘K 拉号 5 个`）
- 切主题（`⌘K 深色`）
- API 调试（`⌘K vendor stock`）

**实现建议**：`cmdk` 库（headless）+ 我方样式。参考 [Vercel Command Menu](https://vercel.com/docs/dashboard-features/command-menu)。

### 7.2 键盘快捷键（跟 Linear 对齐）

| 键 | 动作 |
|---|---|
| `⌘K` | Command palette |
| `⌘/` | 快捷键帮助 |
| `⌘,` | 设置页 |
| `G` `H` | Go Home |
| `G` `B` | Go Buses |
| `G` `P` | Go Pull-records |
| `G` `W` | Go Wallet |
| `C` | Create（在 buses 页 = 建车；在 pull 页 = 拉号） |
| `?` | 显示所有快捷键 |
| `Esc` | 关闭弹窗 / 返回上一层 |

### 7.3 微交互（不做过 · 只做关键 3 个）

**过度动效反例**：每张卡片都 hover 抬起、每个按钮都 spring 弹跳 → 视觉噪音。**Apple 教的**是"动效服务于反馈"，不服务于装饰。

**本项目只做 3 个动效**：
1. **主 CTA 点击** · 60ms 缩到 96% + 阴影收 → 触觉反馈感
2. **数据变更** · 数值从 A→B 的 300ms 缓入渐变 · 用于余额变动、号数变动
3. **列表增删** · 200ms fade + slight slide（≤ 8px）· 用于拉号完成时新号加入列表

**其余**：hover 只改颜色 / border，不做位移 / 缩放。

### 7.4 反馈 · 4 层强度

- **静默** · 无副作用的读操作（列表刷新，不 toast）
- **inline** · 表单校验错误 · 在字段下方红字
- **toast** · 写操作成功（"车已建"）· 3s 自动消失 · 右下角
- **对话框** · 危险操作确认（解散 bus / 删除 API key）· 二次确认

### 7.5 空态

**每个页面必有空态**（[Progressive Disclosure Nielsen](https://jakobnielsenphd.substack.com/p/progressive-disclosure) 强调："第一次进入的用户看到什么" 决定了他会不会用）：

- 首页无车 → 大空态图（用 `brand-gradient-glow`）+ "建第一辆车" 主 CTA
- 车里无号 → 中空态 + "拉一次号试试" 按钮
- 拉号记录无 → 小空态 + "去单独拉号" 链接

## 8. Dark / Light 模式

**默认跟随系统**（`prefers-color-scheme`），用户可在设置手动覆盖。

**切换动效**：**无过渡**（Apple 就是硬切换 · [Apple HIG 2025](https://superdesign.dev/blog/apple-design-system)）。理由：过渡长了让人晕；短了没意义。

**Dark 品牌色**：`kiro-400 #A574FF` 代替 `kiro-500`（暗背景要更亮才有存在感）。

## 9. 无障碍（不是可选，是必需）

参考 [WCAG 2025 Guide](https://allaccessible.org/blog/color-contrast-accessibility-wcag-guide-2025) + [Coinbase Design System](https://cds.coinbase.com/getting-started/colors)：

- **文字对比度 ≥ 4.5:1**（正文）· ≥ 3:1（大字）
- **焦点环 3px** `outline` `kiro-500` alpha 40%（键盘 tab 可见）
- **点击目标 ≥ 44×44px**
- **图标必配 aria-label**
- **表单必配 label**（不用 placeholder 代替 label）
- **深浅模式对比度都过 AA**

## 10. 组件不是货架，是隐形基础设施

**关键理念** —— 你说的原话："**组件只是支撑**"。

**做法**：
- 用 [Radix UI](https://blog.logrocket.com/ux-design/linear-design-ui-libraries-design-kits-layout-grid/)（Linear 也用 Radix）做无样式底座
- 上面套我方样式（`kiro-*` tokens）
- **不用 shadcn/ui 的默认样式**——只用它的**结构**参考
- **每个组件只有 1 种视觉形态**——不搞"5 种 Button variant"（`primary/secondary/ghost/danger/link`）
- 只 3 种 button：
  - **Primary**（紫色填充）· 每屏 1 个
  - **Secondary**（描边）· 每屏 2-3 个
  - **Ghost**（纯文字）· 表格内 / 卡内动作

## 11. 术语双分离 · UI 层再复述

`CLAUDE.md §12` 已定，前端强制执行：

**UI 里绝不出现**（Grep 违反 = review 打回）：
- `housepool` / `record group` / `provider` / `adapter` / `decider` / `coalescer`
- `initiated` / `reserved` / `purchased` / `imported` / `pending_purchase`
- `credential`（对乘客叫"号"）
- `passenger`（对乘客不出现，就叫"我"）
- `bus_id`（内部字段，UI 只显示车名）

**UI 里只出现**：车 · 号 · 拉号 · 我的号池 · 车友 · 车队 · 补车 · 拿走。

## 12. Pencil mockup 交付物预告

你 confirm 本文后我用 Pencil 出：

1. **设计规范页**（Design tokens / typography / spacing / brand color palette 可视化）
2. **组件页**（Button / Input / Card / Modal / Table / Tab / Badge 各 1 个形态）
3. **12 个页面 mockup**（对齐 `12-frontend-pages.md`）
4. **状态 + 空态 + 弹窗**（关键路径完整）
5. **Light + Dark 各一版**

**不做**：8 种按钮变体图 · 6 种卡片装饰 · 5 种字号 fallback（那是过度设计）。

## 13. 未定 · 阶段 1a 起手时确定

- **前端字体加载**（Inter 从 CDN or self-host） · 起手时定
- **⌘K 库选型**（cmdk vs 自建） · 见到 Pencil 稿再定
- **图标库**（Lucide vs Radix Icons） · 阶段 1a 起手时定
- **动效库**（Framer Motion 是否必需，还是纯 CSS 够） · 见到需求量再定

## 14. 参考清单

**设计原则**：
- [Linear README](https://linear.app/readme) · 键盘优先 / 简洁
- [Apple HIG 2025 视频](https://developer.apple.com/videos/play/wwdc2025/356/) · 新设计系统
- [Apple Design System Breakdown 2026](https://superdesign.dev/blog/apple-design-system) · 可复制的 DESIGN.md
- [Coinbase Design System](https://cds.coinbase.com/getting-started/colors) · 语义 tokens 参考

**AI 产品**：
- [Comparing Conversational AI UIs 2025](https://intuitionlabs.ai/articles/conversational-ai-ui-comparison-2025?)
- [Claude Design 设计哲学](https://aiuxplayground.substack.com/p/claude-vs-chatgpt-a-deep-dive-into)
- [Anthropic Claude Design 发布](https://www.anthropic.com/news/claude-design-anthropic-labs)

**SaaS 交互模式**：
- [Vercel Command Menu](https://vercel.com/docs/dashboard-features/command-menu)
- [Rise of ⌘K 2026](https://www.saasframe.io/blog/the-rise-of-cmd-k-why-every-saas-needs-a-search-modal-in-2026)
- [Raycast Action Panel](https://manual.raycast.com/action-panel)
- [B2B SaaS Design Trends 2026](https://procreator.design/blog/b2b-saas-design-trends-and-examples/)
- [SaaS Dashboard Design 2026](https://adminlte.io/blog/saas-dashboard-design-examples/)
- [Dashboard Data-Heavy Patterns](https://uitop.design/blog/best-dashboard-design-patterns-for-data-heavy-saas-platforms/)

**Progressive Disclosure**：
- [UXPin 2026](https://www.uxpin.com/studio/blog/what-is-progressive-disclosure/)
- [Jakob Nielsen 2026](https://jakobnielsenphd.substack.com/p/progressive-disclosure)
- [10 Best B2B Dashboard 2026](https://www.orbix.studio/blogs/b2b-saas-dashboard-design-examples)

**紫色 & 无障碍**：
- [Twitch Design System](https://www.shadcn.io/design/twitch) · #9147FF 用法
- [Beyond Purple by Twitch](https://blog.twitch.tv/en/2019/12/03/beyond-purple)
- [Shades of Purple Practical Palette](https://thelinuxcode.com/shades-of-purple-color-a-practical-palette-tokens-and-real-world-ui-guidance/)
- [WCAG 2025 Contrast Guide](https://allaccessible.org/blog/color-contrast-accessibility-wcag-guide-2025)
- [Stéphanie Walter · 紫色可访问性](https://stephaniewalter.design/blog/yellow-purple-and-the-myth-of-accessibility-limits-color-palettes/)

---

# Part II · 前端设计规范（新页面必读）

## 目录

1. [语言与量词](#1-语言与量词)
2. [时间格式](#2-时间格式)
3. [颜色语义](#3-颜色语义收紧的三色系统)
4. [Badge 三层](#4-badge-三层)
5. [数据颗粒度](#5-数据颗粒度)
6. [卡片系统](#6-卡片系统)
7. [表格](#7-表格)
8. [列表 + 空态 + 分页](#8-列表--空态--分页)
   - **响应式规则见 §11.2 + §11.3**
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

### 5.2 描述里的重点 · 一律用 `<Em>`（不许各页手写 span）

**硬约束**：描述文字（页面 hero 副行 / 卡片 sub / 行内叙述）里被强调的数字或名字，**只能**用 `primitives.tsx` 的 `<Em>`。

```tsx
import { Em } from "@/components/ui/primitives";

<p className="text-fg-tertiary">
  <Em>{items.length}</Em> 辆车正在运转 · 今日消费 <Em tone="spend">-{fmtCredits(spend)}</Em> 积分
</p>
```

| 写法 | 用在 |
|---|---|
| `<Em>` | 默认 · `font-semibold tnum text-fg`（近黑）· 绝大多数数字 |
| `<Em tone="spend">` | 花掉的钱 / 失效数 → 红 |
| `<Em tone="ok">` | 到账 / 正常数 → 绿 |
| `<Em tone="warn">` | 要注意的数 → 黄 |
| `<Em plain>` | 强调的是**名字**不是数字（关掉 tabular-nums），如 vendor 名 / "手动模式" |

**为什么收成一个组件**：之前每页各写一遍 span，飘成三套 —— 概览/拼车用 `text-fg`，价格/车详情用 `text-fg-secondary`，卡片里又是另一套。**颜色由 tone 决定，不由页面决定**。

`Overview` 的 `<Num sign>` 是 `<Em>` 的薄包装（`+` → `tone="ok"` · `-` → `tone="spend"`），样式本体不在那里，别在页面里另写一套。

### 5.2b 标题到描述的间距 · 一律走 `SectionHead`

卡片标题 + 副标题**不许**手写 `<h2>` + `<p>` —— 手写的会漏 `space-y-1`，间距变 0px（跟别处 4px 不一致）。用 `<SectionHead title sub right>`，`sub` 收 `ReactNode` 所以能嵌 `<Em>`。

页面 hero（`h1` + 描述）统一 `space-y-2`（8px）· 卡片（`h2` + sub）统一 `space-y-1`（4px）。

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

### 5.6b 显式 grid 轨道**一律写 `minmax(0,1fr)`，不许写 `1fr`**

**硬约束**。`grid-cols-[1fr_400px]` 里的 `1fr` 其实是 `minmax(auto, 1fr)` —— 那个 `auto` 下限等于**轨道的 min-content**。轨道里只要有一个缩不下去的东西（表格 `min-w-[640px]`、`whitespace-nowrap` 的 chip、长 mono 串），轨道就**拒绝**缩到可用宽度，然后把兄弟列**挤出容器外**。

已经踩过两次：
- 概览 Vendor 监测 + 占比：容器 1248，轨道要 886 + 400 + 24 = **1310**，右边那张卡右边缘跑到容器外 61px
- 钱包（流水表 `min-w-[560px]`）：1024–1152 宽时**整页出现横向滚动条**

```tsx
// ❌ 会把兄弟列挤出去
<div className="grid gap-6 lg:grid-cols-[1fr_400px]">

// ✅ 下限归零，让里面的 overflow-x-auto 去横滚
<div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_400px]">
```

固定宽度那一侧（`400px` / `340px`）不用改 —— 它本来就没有 auto 下限。

**同理**：flex 子项要能收缩必须给 `min-w-0`（`flex-1` 单独用也有同样的 min-content 下限问题）。项目里 `min-w-0 flex-1` 这个组合到处都是，就是这个原因。

**怎么验**（别靠眼睛）：
```js
// 任何 display:grid 的容器，scrollWidth 超过自身宽度就是溢出
document.querySelectorAll('div').forEach(d => {
  if (getComputedStyle(d).display !== 'grid') return;
  const w = d.getBoundingClientRect().width;
  if (d.scrollWidth > Math.ceil(w) + 1) console.log('溢出', d.className);
});
// 页面级：
document.documentElement.scrollWidth > window.innerWidth
```
**要在多个宽度下跑**（390 / 768 / 1024 / 1152 / 1280 / 1440 / 1536）—— 这个 bug 只在"轨道被压到比 min-content 还窄"时才现形，宽屏看不出来。

### 5.6c 缩不下去的内容 → 自己横滚，不撑破页面

固定宽度内容（tab 条、300px 标签列的图表矩阵）在窄屏必然超出。**让它自己滚**，别让整页出现横向滚动条：

```tsx
// 负 margin 抵掉外层 padding，滚动区贴满边缘
<div className="-mx-7 overflow-x-auto px-7">{宽内容}</div>
```

- `TabsList` 已内置这层（车详情 6 个 tab 在 390px 上宽 414px）· 滚动条用 `[scrollbar-width:none]` 藏掉
- **绝对定位的浮层（tooltip）要放在滚动容器外面**，否则被裁掉

### 5.7 分隔线恒 1px · 小心两条贴成 2px

全站分隔线**只有 1px**（`border-hairline`）。没有 2px 分隔线 —— 唯一允许的 2px 是**头像叠圈**（`border-2 border-bg`，那是遮挡不是分隔）。

**看起来像 2px 的，实际都是两条 1px 贴在一起。** 两个已经踩过的坑：

1. **`BareHead` 嵌在 `BareList` 里**：`divide-y` 给第一行加 `border-t`，跟 head 自己的 `border-b` 贴住。
   → 已在 `BareHead` 里加 `[&+*]:!border-t-0` 兜住（**必须带 `!`**：`divide-y` 生成的选择器特异度 0,3,0，普通 `[&+*]` 只有 0,1,0 赢不了）。head 放 list 里或外都恒 1px。

2. **`divide-y` 容器里混了绝对定位层**（价格页箱线矩阵的 X 网格层 / hover 指示线）：绝对定位**仍算 DOM 子元素**，于是第一个真实行变成"非首个"也拿到 `border-t`。
   → 把 6 行单独包一个 `divide-y` 容器，绝对层留在外层 `relative` 上。

**改完怎么验**：别靠眼睛，量计算值 ——
```js
// 表头下沿 + 首行上沿 应该恒为 1px
getComputedStyle(head).borderBottomWidth + getComputedStyle(head.nextElementSibling).borderTopWidth
```

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

## 11. 页面级布局 + 响应式

### 11.1 统一容器 `page-container`

**Header 和 main 用同一个 utility**，宽度必然一致（`src/index.css`）：

```css
.page-container {
  @apply mx-auto w-full max-w-[1440px] px-4 sm:px-6 lg:px-12 xl:px-gutter;
}
```

响应式 padding：
- `< sm (640)`：16px 两侧
- `sm~lg (640~1024)`：24px
- `lg~xl (1024~1280)`：48px
- `≥ xl (1280)`：96px（`gutter`）

**不用 `px-gutter` 单独套**（96px 在小屏太宽）。**Header 内层必须用 `page-container`**，否则内容左右边界跟 main 对不上。

### 11.2 统一断点表（**新页面直接抄，别自己造区间**）

| 断点 | tailwind | 场景 | KPI 卡 | 业务卡 | 表 + 侧列 | 主导航 |
|---|---|---|---|---|---|---|
| **< sm** | (default) | 手机竖屏 (< 640) | 1 列 | 1 列 | 单列堆叠 | **logo 右 chevron 下拉面板** |
| **≥ sm** | 640+ | 大手机 / 手机横屏 | 2 列 | 1 列 | 单列堆叠 | 下拉面板 + Bell 出现 |
| **≥ md** | 768+ | 平板 | 2 列 | 2 列 | 单列堆叠 | 下拉面板 + 上游 pill 完整版 |
| **≥ lg** | 1024+ | 小桌面 / 大平板 | 4 列 | 3 列 | 单列堆叠 | **主导航 tab 展开（第二排）** |
| **≥ 2xl** | 1536+ | 大桌面 | 4 列 | 3 列 | **1fr + 400px 并排** | 完整 |

**关键 class 模板**：
- 4 KPI: `grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4`
- 3 业务卡: `grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3`
- 表 + 侧列: `grid grid-cols-1 gap-6 2xl:grid-cols-[1fr_400px]`（**xl 主列不够放 min-w-640 的表**，2xl 才启用）
- 顶部 hero 左右两列: `flex flex-col gap-4 md:flex-row md:items-end md:justify-between`

**永远从窄开始声明**（mobile-first）· 断点值从小到大加 prefix。

### 11.3 表格 / 长列表 · 横滚兜底

宽表和长行**不硬压缩**（挤压 badge 变形、数字换行）。用 `overflow-x-auto + min-w`：

```tsx
<div className="-mx-7 mt-5 overflow-x-auto px-7">
  <div className="min-w-[640px]">
    <BareHead>...</BareHead>
    <BareList>...</BareList>
  </div>
</div>
```

- **min-w** 设为表的自然宽度（列宽相加，Vendor 表 640px · 活动记录 640px）
- **-mx-7 + px-7** 让滚动条延伸到卡片边缘（视觉上"整卡横滚"）
- 窄屏出滚动条 · 宽屏不影响
- min-w 别写太大 —— **考虑跟 `2xl:grid-cols-[1fr_400px]` 并排后主列的可用宽度**（xl=1280 时主列 664，不够 720 但够 640）

**这是硬约束**：任何 5+ 列的数据表、任何带 badge + 长文本的行，都要包 `overflow-x-auto + min-w`。别指望"响应式列宽"能救 —— 挤到某个点必然溢出或错位。

### 11.4 Hero · 左右响应式换行

```
桌面 (md+)：
[页标题 hero 32px]                  [状态 pill / 上排]
[日期时间 · 描述]                     [Segmented 时间维度 · 下排]

移动 (<md)：
[页标题]
[日期时间 · 描述]
[状态 pill]
[Segmented（可横滚兜底）]
```

```tsx
<div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
  <div className="min-w-0 space-y-2">...</div>
  <div className="flex flex-col gap-2 md:items-end">
    <PoolStatus />
    <div className="-mx-1 overflow-x-auto px-1">
      <Segmented options={...} value={...} onChange={...} />
    </div>
  </div>
</div>
```

`min-w-0` 关键 —— 让 flex 子项能收缩（默认 `min-w: auto` 会被内容撑开导致父级 flex 挤压其他项）。

### 11.5 Header 元素响应策略 · mini pill 渐进展开

**核心原则**：所有 header 元素必须显示（信息不丢失），空间不够时**渐进缩小**（先隐藏辅助文字、保留数字/图标）而不是整个隐藏。

| 元素 | 移动 (< sm) | sm (640+) | md (768+) | lg (1024+) |
|---|---|---|---|---|
| Logo mark | ✅ 显示 | ✅ | ✅ | ✅ |
| Wordmark "bus-pooling" | 隐藏 | ✅ 显示 | ✅ | ✅ |
| **上游 pill** | `● 128` mini | `● 128` mini | `● 上游 128 个可拉` 完整 | 完整 |
| **积分 pill** | `钱包 1,245` mini | `钱包 1,245 积分` | 完整 | 完整 |
| Bell 通知 | 隐藏 | ✅ 显示 | ✅ | ✅ |
| Avatar | ✅（无 chevron） | ✅ | ✅ | ✅ |
| **下拉箭头**（触发 mobile nav） | ✅ 显示 | ✅ | ✅ | 隐藏 |
| **主导航 tab（第二排）** | 隐藏 → 走下拉 | 隐藏 | 隐藏 | ✅ 显示 |

**pill 收缩范式**：数据本身永远显示，只有量词/单位/label 可以按屏收起：

```tsx
<div className="flex shrink-0 items-center gap-1.5 whitespace-nowrap
                rounded-full ... px-2.5 py-1
                sm:gap-2 sm:px-3 sm:py-1.5">
  <Icon className="size-3.5" />
  <span className="font-semibold tnum">{value}</span>
  <span className="hidden sm:inline">积分</span>{/* 或 "个可拉"、md:inline */}
</div>
```

**决策依据**：移动端用户还是想知道"上游有多少、我有多少积分"—— 数据永远显示，只有 label 可以收。所有 header 元素必须 `shrink-0 whitespace-nowrap`，任何挤压都不变形。

### 11.6 移动端菜单 · logo 右侧下拉面板

**不用侧滑抽屉**（280px 从右滑入的模式学习成本高、遮挡内容）。**用 logo 右边的 chevron 触发向下展开的面板**：

```tsx
function MobileNav() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    const onResize = () => window.innerWidth >= 1024 && setOpen(false);
    window.addEventListener("keydown", onKey);
    window.addEventListener("resize", onResize);
    return () => { /* cleanup */ };
  }, [open]);

  return (
    <>
      <button
        onClick={() => setOpen((v) => !v)}
        className="grid size-7 shrink-0 place-items-center rounded-md
                   hover:bg-bg-elevated lg:hidden"
        aria-label="切换菜单"
      >
        <ChevronDown className={cn("size-4 text-fg-tertiary transition-transform",
                                    open && "rotate-180")} />
      </button>

      {open && (
        <>
          {/* 空白点击关 · 无背景遮罩，不遮挡下方内容 */}
          <div className="fixed inset-0 z-30 lg:hidden"
               onClick={() => setOpen(false)} />
          {/* 面板从 header 下沿向下摊开 · 全宽 */}
          <div className="absolute inset-x-0 top-full z-40 border-b border-hairline
                          bg-bg shadow-pop lg:hidden">
            <nav className="page-container flex flex-col gap-1 py-3">
              {TABS.map((t) => (
                <NavLink to={t.to} onClick={() => setOpen(false)} ...>
                  <t.icon /> {t.label}
                </NavLink>
              ))}
            </nav>
          </div>
        </>
      )}
    </>
  );
}
```

**父容器 `relative`**：header 排 1 容器加 `relative` 让 MobileNav 的 `top-full` 定位到 header 下沿。

**关键行为**：
- **chevron 旋转 180°**（`rotate-180`）指示 open/close 状态
- **点空白关**（`fixed inset-0` 空 div）· **ESC 关** · **resize 到 lg 自动关**
- **点链接自动关**（`NavLink onClick={close}`）
- **不遮罩背景** —— 移动端下拉面板是"轻交互"，不像模态框需要 backdrop

### 11.7 Header 分割线策略 · 中浅底深

Header 是双排结构时（lg+）：

- **排 1 与排 2 之间**：`h-px bg-hairline/40` **极浅**（40% hairline），只做视觉过渡
- **Header 底部（跟 main 分开）**：`border-b border-hairline` **正常灰**，明确分离 header 与内容

**中间浅 / 底部深**，不是反过来。为什么：排 1 排 2 都在 header 内属于同一层，用浅线过渡；header 跟内容是不同层，需要清晰边界。

**全宽**：排 1/2 中间分割线**不套 `page-container`**，撑满 viewport 边缘。header border-b 也是全宽。

### 11.8 组件级 shrink 保底

Chip / FlowBadge / StockBadge / CreditPill / 所有 pill 类小元素都**默认加**：

```tsx
"inline-flex shrink-0 whitespace-nowrap ..."
```

**这是组件应有属性**，别指望在调用侧每处补。见 `primitives.tsx` `Chip` 定义。

### 11.9 flex 子项收缩

flex 布局里想让某个子项能收缩（比如内容长的 `truncate` 生效），**必须**加 `min-w-0`：

```tsx
<div className="flex items-center gap-2">
  <div className="min-w-0 flex-1 truncate">{longText}</div>
  <Chip>标签</Chip>
</div>
```

默认 `min-width: auto` 让 flex item 至少跟内容一样宽，长文本就把兄弟挤没了。踩过。

**兄弟 badge 不换行**：flex 里的 badge 必须 `shrink-0 whitespace-nowrap`，让 `min-w-0` 的兄弟先 truncate 而不是 badge 换行。

### 11.10 Grid 分栏宽度陷阱

flex/grid 分栏时**别只调 gap**，务必给栏本身 `min-w`。踩过的坑：

```tsx
// ❌ 错做法：gap 越拉越大，栏本身反而瘦成 60px（栏比 gap 还窄，视觉稀疏）
<div className="grid grid-cols-3 gap-x-20">
  {/* 每栏只有 60px 时看着挤 · 但其实是栏瘦了 */}
</div>

// ✅ 对做法：先保证栏最小宽度，再调整 gap
<div className="grid grid-cols-3 gap-x-12 [&>div]:min-w-[120px]">
```

调整前用浏览器 `getBoundingClientRect` 测实际宽度，别凭感觉。用户说"挤"时先确认是**栏挤**还是**栏本身瘦**。

### 11.11 Promo bar · 顶部品牌色跑马灯

页面最顶（在 header 上方）品牌紫底 · 白字居中 · 后箭头 · 可点击跳落地页。多条文案 6s 轮播：

```tsx
const PROMOS = [
  { text: "阶段 1a · 拼车公测中 · ...", to: "/buses" },
];

function PromoBar() {
  const [i, setI] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setI((v) => (v + 1) % PROMOS.length), 6000);
    return () => clearInterval(id);
  }, []);
  return (
    <div className="bg-brand text-white">
      <Link to={PROMOS[i].to}
            className="page-container flex items-center justify-center gap-2
                       py-1.5 text-label font-medium hover:opacity-90">
        <span className="truncate text-center">{PROMOS[i].text}</span>
        <ArrowRight className="size-3.5 shrink-0" />
      </Link>
    </div>
  );
}
```

**永远在 header 之上**（不 sticky · 用户不需要一直看到）。

### 11.12 Footer 结构

**左品牌区 + 右 3 栏菜单** flex 布局：

```tsx
<footer className="mt-auto border-t border-hairline bg-bg-elevated">
  <div className="page-container py-10">
    {/* 上部：左品牌 · 右菜单 · lg 起并排 */}
    <div className="flex flex-col gap-10 lg:flex-row lg:justify-between lg:gap-16">
      {/* 品牌 · max-w-xs 收窄不占位 */}
      <div className="max-w-xs space-y-3">
        <Logo />
        <p>描述</p>
        <SocialIcons /> {/* 3 个：TG · Discord · GitHub · 带 border 描边 */}
      </div>

      {/* 3 栏菜单 · min-w-[120px] 保证不瘦 · gap 从内容自然拉开 */}
      <div className="grid grid-cols-2 gap-x-8 gap-y-8 sm:grid-cols-3
                      sm:gap-x-10 lg:gap-x-12 [&>div]:min-w-[120px]">
        <FooterCol title="产品">...</FooterCol>
        <FooterCol title="账户">...</FooterCol>
        <FooterCol title="说明与政策">...</FooterCol>
      </div>
    </div>

    {/* 底行 · copyright 左 · 状态右 */}
    <div className="mt-8 flex flex-col gap-3 border-t border-hairline
                    pt-6 text-label text-fg-tertiary md:flex-row
                    md:items-center md:justify-between">
      <span>© 2026 bus-pooling · 开源公益项目</span>
      <SystemStatus />
    </div>
  </div>
</footer>
```

**社群 icon 用内嵌 SVG**：lucide 1.x 没有 GitHub / Telegram / Discord。**别拉图标包**，SVG path 5 行内复制到组件即可。

**Footer 链接铁律**：
- **只列真实存在的路由 / 真会写的文档** —— 不堆 dead link
- 阶段 1a 三栏：`产品`（概览/拼车/提取/发车）· `账户`（钱包/资料/API/webhook/号池）· `说明与政策`（用户协议/隐私政策/合规声明/对接文档）
- 政策类等真写文档了再接入路由

### 11.13 页面纵向节奏

模块之间用 `space-y-section`（56px）· 卡片内层用 `mt-5`（20px）· 卡片间距 `gap-6`（24px）。**主内容 py 响应式**：`py-8 lg:py-12`。

**Layout 用 `flex flex-col min-h-dvh`** 让 footer 沉底：

```tsx
<div className="flex min-h-dvh flex-col bg-bg">
  <PromoBar />
  <header>...</header>
  <main className="flex-1 page-container py-8 lg:py-12">
    <Outlet />
  </main>
  <AppFooter />
</div>
```

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
- 2026-08-08 · 响应式改造 · header 双排 + 移动下拉面板 + mini pill 渐进 + promo bar + footer 3 栏
