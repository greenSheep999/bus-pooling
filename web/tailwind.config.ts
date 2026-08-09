import type { Config } from "tailwindcss";
import animate from "tailwindcss-animate";

export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // 品牌 · Kiro 紫（导航高亮 / focal 卡 / 主 CTA）
        // 前景类（DEFAULT/strong/light/solid）暗色下也用 · 深底上高饱和字体反而清晰
        // 背景类（faint/subtle/bg）走 CSS 变量 · 深色下换成低透明覆盖 · 见 index.css
        brand: {
          DEFAULT: "#9147FF",
          strong: "hsl(var(--brand-strong))",
          light: "#A574FF",
          soft: "#C9A9FF",
          faint: "hsl(var(--brand-faint))",
          subtle: "hsl(var(--brand-subtle))",
        },
        // 积分 / 余额 · 绿色系（decisions §8 · 不用紫）
        credit: {
          bg: "hsl(var(--credit-bg))",
          fg: "hsl(var(--credit-fg))",
        },
        // 语义 · bg 变量化 · fg / solid 前景两个模式都用同一个（对比度足够）
        ok:     { bg: "hsl(var(--ok-bg))",     fg: "hsl(var(--ok-fg))",     solid: "#22C55E" },
        warn:   { bg: "hsl(var(--warn-bg))",   fg: "hsl(var(--warn-fg))",   solid: "#F59E0B" },
        danger: { bg: "hsl(var(--danger-bg))", fg: "hsl(var(--danger-fg))", solid: "#EF4444" },
        // 号去向 tag
        dest: {
          bus: "#9147FF",
          push: "#6420C7",
          pending: "#C9A9FF",
          handoff: "#D4D4D8",
        },
        // 前景 / 背景（支持 dark）
        fg: {
          DEFAULT: "hsl(var(--fg))",
          secondary: "hsl(var(--fg-secondary))",
          tertiary: "hsl(var(--fg-tertiary))",
        },
        bg: {
          DEFAULT: "hsl(var(--bg))",
          elevated: "hsl(var(--bg-elevated))",
        },
        hairline: "hsl(var(--hairline))",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "-apple-system", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "monospace"],
      },
      fontSize: {
        // 组件内文字 · 标准三档（12 / 13 / 14）
        label: ["12px", { lineHeight: "1.4" }],      // 表头 / chip / 次要说明
        body: ["13px", { lineHeight: "1.5" }],       // 组件主力正文（表格 / 列表 / 卡内）
        "body-lg": ["14px", { lineHeight: "1.5" }],  // 卡片标题 / 强调正文（少用）
        // 标题 / 数字（不动）
        section: ["20px", { lineHeight: "1.3", letterSpacing: "-0.01em" }],
        stat: ["24px", { lineHeight: "1.15", letterSpacing: "-0.02em" }],
        num: ["30px", { lineHeight: "1.1", letterSpacing: "-0.02em" }],
        hero: ["36px", { lineHeight: "1.1", letterSpacing: "-0.02em" }],
        giant: ["48px", { lineHeight: "1.05", letterSpacing: "-0.03em" }],
      },
      borderRadius: {
        card: "14px",
        panel: "20px",
        focal: "24px",
      },
      boxShadow: {
        // 阴影颜色走 CSS 变量 · 深色下换成不透明黑（浅色下 10% 淡黑）
        // 变量定义在 index.css `:root` / `.dark`
        card: "0 2px 8px 0 rgb(var(--shadow-card))",
        hover: "0 12px 32px -4px rgb(var(--shadow-hover))",
        pop: "0 12px 32px -4px rgb(var(--shadow-pop))",
        modal: "0 24px 64px -8px rgb(var(--shadow-modal))",
      },
      spacing: {
        section: "56px",
        gutter: "96px",
      },
      backgroundImage: {
        // focal 卡右上角光晕（紫 = 品牌强调；绿 = 积分类，跟 credit pill 视觉统一）
        "glow-tr":
          "radial-gradient(70% 100% at 100% 0%, rgb(145 71 255 / 0.14) 0%, transparent 100%)",
        "glow-tr-credit":
          "radial-gradient(70% 100% at 100% 0%, rgb(34 197 94 / 0.16) 0%, transparent 100%)",
        "glow-t":
          "radial-gradient(50% 100% at 50% 0%, rgb(145 71 255 / 0.14) 0%, transparent 100%)",
      },
      transitionDuration: { DEFAULT: "180ms" },
      transitionTimingFunction: { DEFAULT: "cubic-bezier(0.22,1,0.36,1)" },
      keyframes: {
        /* 呼吸：opacity 1 → 0.35 → 1 · 2.4s 缓入缓出（心跳感，不刺眼） */
        breath: {
          "0%, 100%": { opacity: "1", transform: "scale(1)" },
          "50%": { opacity: "0.45", transform: "scale(0.85)" },
        },
        /* 翻牌：旧字符上半片绕底边往下翻走（机场航班牌）
           只翻 90° —— 翻满 180° 会看到背面，而背面是空的 */
        splitflap: {
          "0%": { transform: "rotateX(0deg)", opacity: "1" },
          "100%": { transform: "rotateX(-90deg)", opacity: "0" },
        },
      },
      animation: {
        breath: "breath 2.4s cubic-bezier(0.4, 0, 0.6, 1) infinite",
        splitflap: "splitflap 400ms ease-in forwards",
      },
    },
  },
  plugins: [animate],
} satisfies Config;
