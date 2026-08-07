import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

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

/** 用量 k 格式：6400 → "6.4" */
export function fmtK(v: number): string {
  return (v / 1000).toFixed(1);
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

export function vendorColor(id: string): string {
  return VENDOR_COLOR[id] ?? "#9147FF";
}

/** 相对时间：8/07 18:24 / 昨 20:15 / 18:24 */
export function fmtTime(iso: string): string {
  const d = new Date(iso);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (sameDay) return hm;
  const y = new Date(now);
  y.setDate(y.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return `昨 ${hm}`;
  return `${d.getMonth() + 1}/${String(d.getDate()).padStart(2, "0")} ${hm}`;
}

/** 寿命：秒 → "42h" / "3.2d" */
export function fmtLifespan(seconds: number): string {
  const h = seconds / 3600;
  if (h < 48) return `${Math.round(h)}h`;
  return `${(h / 24).toFixed(1)}d`;
}
