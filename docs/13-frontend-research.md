# bus-pooling · 前端调研 + 设计原则 + 主题系统

> 前置：`04-scenarios.md` · `12-frontend-pages.md`
>
> **调研时点**：2026-08-07
> **目标**：不被前端框架 / 组件库局限。设计出苹果级视觉 + 交互。**用户不用动脑子就会用**。
>
> **本文只写"要做成什么样"**（设计原则 + 主题系统 + 交互规范）。**具体页面 UI 稿在 Pencil**（Pencil 是 mockup 工具，你 confirm 本文后我出稿）。

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
