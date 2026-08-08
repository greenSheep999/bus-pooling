import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

/** tailwind-merge 必须知道项目自定义的 fontSize 名字
 *  否则它把 `text-label` 当成**颜色类**，跟 `text-white` 判定冲突后覆盖掉
 *  症状：brand 按钮在带 size="sm"（含 text-label）时变成紫底黑字
 *  自定义字号来自 tailwind.config.ts fontSize */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [
        { text: ["label", "body", "body-lg", "section", "stat", "num", "hero", "giant"] },
      ],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** microunit → 积分整数（1 积分 = 1 元 = 1_000_000 microunit，decisions §8.7） */
export const MICRO = 1_000_000;

export function toCredits(micro: number): number {
  return Math.round(micro / MICRO);
}

/** 积分格式化：整数无小数点，带千分位 */
export function fmtCredits(micro: number, opts?: { sign?: boolean }): string {
  const v = toCredits(micro);
  const s = Math.abs(v).toLocaleString("zh-CN");
  if (!opts?.sign) return s;
  return v > 0 ? `+${s}` : v < 0 ? `-${s}` : s;
}

/** 环比：今值 vs 昨值 → "+41%" / "-12%" / "-"（昨值为 0 时无从比较） */
export function fmtDelta(now: number, prev: number): string {
  if (!prev) return "-";
  const pct = Math.round(((now - prev) / prev) * 100);
  return pct > 0 ? `+${pct}%` : `${pct}%`;
}

/** 带符号数字的语义色：正 = 到账（绿）· 负 = 花掉（红）· 0/无符号 = 中性 */
export function signedToneClass(sign: "+" | "-" | ""): string {
  if (sign === "+") return "text-ok-fg";
  if (sign === "-") return "text-danger-fg";
  return "text-fg";
}

/** 用量 k 格式：6400 → "6.4" */
export function fmtK(v: number): string {
  return (v / 1000).toFixed(1);
}

/** 连续被跳过几次自动挂起（decisions §8.26）· 充值后归零 */
export const SUSPEND_AFTER = 3;

/** waffo 通道费 5% · pass-through，我方不加不承担（decisions §2.13）
 *  **只在充值时收一次**（§8.21）—— 拉号 / 提取 / 派号都是积分抵扣，那些页面不许显示通道费 */
export const CHANNEL_FEE_RATE = 0.05;

/** 服务费 · 每人每次拉号动作 · **两档固定费**（decisions §8.31）
 *  - 有系统邀请码（社群）：1 积分 = 1 RMB
 *  - 无系统邀请码（零售）：7 积分 ≈ 1 USD
 *
 *  仍是**固定费不是百分比** —— `00 §3` 的对齐激励（我方没有动机加价号成本）靠这个。
 *  7 是**定价档位不是实时汇率**：汇率波动不该让用户看到服务费每天变。
 *  真实汇率只用于 vendor 成本换算（`vendor_pricing.credits_per_unit`），两回事。 */
export const SERVICE_FEE_COMMUNITY = 1 * MICRO;
export const SERVICE_FEE_RETAIL = 7 * MICRO;

/** 按身份取服务费 · invited = 注册时填过**系统**邀请码（个人码不算 · §8.29） */
export function serviceFee(invited: boolean | undefined): number {
  return invited ? SERVICE_FEE_COMMUNITY : SERVICE_FEE_RETAIL;
}

/** 充值：付款额 → 通道费 + 实际到账 */
export function topupBreakdown(paid: number): { fee: number; credits: number } {
  const fee = Math.round(paid * CHANNEL_FEE_RATE);
  return { fee, credits: paid - fee };
}

/** 号额度阈值配色（decisions §8.14 · 10k 积分寿终阈值） */
export const QUOTA_MAX = 10_000;

export function quotaLevel(used: number): "ok" | "warn" | "danger" {
  if (used >= 9000) return "danger";
  if (used >= 7000) return "warn";
  return "ok";
}

export const QUOTA_COLOR: Record<ReturnType<typeof quotaLevel>, string> = {
  ok: "#22C55E",
  warn: "#F59E0B",
  danger: "#EF4444",
};

/** vendor 内部 id → 展示名（12-frontend-pages.md · 绝不暴露 id） */
export const VENDOR_NAME: Record<string, string> = {
  "91kiro": "Kiro Market",
  kiroceo: "Kiro CEO",
  kirooo: "Kiro OOO",
  kiroappio: "Kiro App IO",
  kiroappcc: "Kiro App CC",
  kirodrop: "Kiro Drop",
};

/** vendor 身份色（同色系紫深浅，不用杂色） */
export const VENDOR_COLOR: Record<string, string> = {
  "91kiro": "#9147FF",
  kiroceo: "#A574FF",
  kirooo: "#E3D5FF",
  kiroappio: "#D4D4D8",
  kiroappcc: "#A1A1AA",
  kirodrop: "#C9A9FF",
};

export function vendorName(id: string): string {
  return VENDOR_NAME[id] ?? id;
}

/** vendor 匿名编号（散客视角）· decisions §8.20
    无注册邀请码的用户看不到真名，只看 "AWS-Q Kiro Vendor 01"
    编号顺序 = VENDOR_NAME 键顺序（跟 CLAUDE.md §1.1 六家列表一致）· 同一用户每次看到的编号一致 */
const VENDOR_ANON_INDEX: Record<string, string> = Object.fromEntries(
  Object.keys(VENDOR_NAME).map((id, i) => [
    id,
    `AWS-Q Kiro Vendor ${String(i + 1).padStart(2, "0")}`,
  ]),
);

/** vendor 显示名 · 按身份决定真名还是匿名编号
    @param invited 是否有注册邀请码（社群成员） */
export function vendorLabel(id: string, invited: boolean): string {
  if (invited) return vendorName(id);
  return VENDOR_ANON_INDEX[id] ?? "AWS-Q Kiro Vendor";
}

export function vendorColor(id: string): string {
  return VENDOR_COLOR[id] ?? "#9147FF";
}

/** 相对时间：8/07 18:24 / 昨 20:15 / 18:24 */
/** 时间统一 · 全用 "MM/DD HH:mm" —— 每行 11 字符等宽，列对齐；
    不做"今天只显示时分 / 昨 HH:mm"的省略变体（同列不同格式看着乱） */
export function fmtTime(iso: string): string {
  const d = new Date(iso);
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mn = String(d.getMinutes()).padStart(2, "0");
  return `${mm}/${dd} ${hh}:${mn}`;
}

/** 寿命：秒 → "42h" / "3.2d" */
export function fmtLifespan(seconds: number): string {
  const h = seconds / 3600;
  if (h < 48) return `${Math.round(h)}h`;
  return `${(h / 24).toFixed(1)}d`;
}

/* ── 头像：同一标识恒定出同一个浅色（不是每次渲染随机） ── */

const AVATAR_HUES = [262, 210, 158, 24, 340, 190, 45, 285];

/* bg 83% · 比 credit/ok 这些 badge 底色（L≈94%）深一档，不跟它们糊成一片 */
export function avatarColor(seed: string): { bg: string; fg: string } {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) | 0;
  const hue = AVATAR_HUES[Math.abs(h) % AVATAR_HUES.length];
  return {
    bg: `hsl(${hue} 68% 83%)`,
    fg: `hsl(${hue} 62% 26%)`,
  };
}

export function avatarLetter(seed: string): string {
  return (seed.trim()[0] ?? "?").toUpperCase();
}
