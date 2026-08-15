import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

import type { PassengerTier } from "@/types";

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

/** microunit → 积分整数(1 积分 = 1_000_000 microunit · CLAUDE §1.4 内部对账基准) */
export const MICRO = 1_000_000;

export function toCredits(micro: number): number {
  return Math.round(micro / MICRO);
}

/** 积分 → USD 支付额展示(CLAUDE §1.4 唯一对乘客展示的币种)
 *
 *  公式:`(credits + fee) / 7` · fee=credits×0.05 · 7 是 CNY/USD 汇率(展示层)。
 *  返 2 位小数字符串 · 如 "15.00" · **绝不展示"元 / CNY"**(§0.1 铁律)。
 *  中间计算走内部对账口径(1 积分 ≡ 1 CNY) · 但不告诉乘客。 */
const TOPUP_FEE_RATE = 0.05;
const USD_RATE = 7;

export function creditsToUSD(credits: number, feeWaived = false): string {
  if (!Number.isFinite(credits) || credits <= 0) return "0.00";
  const fee = feeWaived ? 0 : credits * TOPUP_FEE_RATE;
  const usd = (credits + fee) / USD_RATE;
  return usd.toFixed(2);
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

/** 支付通道费率 · 这一项是**对用户公示**的（充值卡里明示是通道收取）
 *  只在充值时出现 —— 拉号 / 提取 / 派号都是积分抵扣，那些页面不显示它 */
export const CHANNEL_FEE_RATE = 0.05;

/** 展示层汇率 · CNY/USD · 7 元 = 1 美元（CLAUDE.md §1.4）
 *  只用于 landing / 充值等对外**展示**层 —— 后端记账单位始终是积分 */
export const CNY_PER_USD = 7;

/** 充值预设金额（积分）· landing 落地页展示 + 钱包页复用 · 单点更新 */
export const TOPUP_PRESETS = [50, 100, 200, 500] as const;

/** 充值：付款额 → 通道费 + 实际到账 */
export function topupBreakdown(paid: number): { fee: number; credits: number } {
  const fee = Math.round(paid * CHANNEL_FEE_RATE);
  return { fee, credits: paid - fee };
}

/** 想到账 N 积分 · 通道费加在上面 · 支付金额换算成 USD（CLAUDE.md §1.4）
 *  返回单位统一为 USD · toFixed(2) 由调用方按展示需求处理 */
export function topupUsdBreakdown(wantCredits: number): {
  usdCredits: number;
  usdFee: number;
  usdTotal: number;
} {
  const usdCredits = wantCredits / CNY_PER_USD;
  const usdFee = usdCredits * CHANNEL_FEE_RATE;
  return { usdCredits, usdFee, usdTotal: usdCredits + usdFee };
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

/** vendor 显示名 · 按档次决定真名还是匿名编号
 *
 *  **只有 `wholesale`（批发商）看真名**（docs/10-pricing §2.1）· `retail` / `community` 都看匿名编号。
 *
 *  ⚠️ 别传 `me.invited` —— 那个字段 `community` 也是 true · 会把真名漏给社群档。
 *  一律传 `me?.tier`。 */
export function vendorLabel(id: string, tier: PassengerTier | undefined): string {
  if (tier === "wholesale") return vendorName(id);
  return VENDOR_ANON_INDEX[id] ?? "AWS-Q Kiro Vendor";
}

/** ⚠️ 档次名（retail/community/wholesale）**只在内部**用 · UI 上不要展示三档差别
 *  用户视角只有：有专属邀请码（社群成员）vs 没有 · 具体哪档不对外
 *  见 CLAUDE.md §0.1 §12.6（对外文案不出现内部术语）· docs/10-pricing §2.1 */

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
