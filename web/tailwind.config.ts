import type { Config } from "tailwindcss";
import animate from "tailwindcss-animate";

export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // 品牌 · Kiro 紫（导航高亮 / focal 卡 / 主 CTA）
        brand: {
          DEFAULT: "#9147FF",
          strong: "#6420C7",
          light: "#A574FF",
          soft: "#C9A9FF",
          faint: "#E3D5FF",
          subtle: "#F5F0FF",
        },
        // 积分 / 余额 · 绿色系（decisions §8 · 不用紫）
        credit: {
          bg: "#E8F7EF",
          fg: "#1F7A47",
        },
        // 语义
        ok: { bg: "#E8F7EF", fg: "#1F7A47", solid: "#22C55E" },
        warn: { bg: "#FFF8E1", fg: "#B8860B", solid: "#F59E0B" },
        danger: { bg: "#FDECEE", fg: "#B91C1C", solid: "#EF4444" },
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
        card: "0 2px 8px 0 rgb(10 10 10 / 0.03)",
        // hover 不带紫（避免跟 focal 强调色抢），中性黑阴影放大即可
        hover: "0 12px 32px -4px rgb(10 10 10 / 0.14)",
        pop: "0 12px 32px -4px rgb(10 10 10 / 0.08)",
        modal: "0 24px 64px -8px rgb(10 10 10 / 0.20)",
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
      },
      animation: {
        breath: "breath 2.4s cubic-bezier(0.4, 0, 0.6, 1) infinite",
      },
    },
  },
  plugins: [animate],
} satisfies Config;
