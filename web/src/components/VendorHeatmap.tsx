import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useVendorDispatchEvents, type VendorStatusRow, type StatusWindow } from "@/api/hooks";
import { Card } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

/** 6 家 vendor 时间轴热力图 · 共享 x 轴 · 一眼看缺口
 *
 *  设计理念（decisions §11.14）：
 *   - **单位统一**：同窗口内 6 家共用一个桶宽 · x 轴范围一致 · 缺口一眼可见
 *   - **颜色深浅编码 count**：无数据 = 浅灰墩（不是空白 · 缺口跟"根本没开号"不混）
 *   - **每家一行**：按 vendor 卡的原顺序（Quality.Score 排序）· 纵向对齐
 *   - **时间从左（旧）到右（新）**：符合乘客直觉 · 最右 = 最近
 *
 *  桶宽跟窗口挂钩（跟单卡柱图对齐）：
 *   - ≤48h  · 1 小时/桶
 *   - ≤336h · 6 小时/桶
 *   - 更长  · 1 天/桶 */
export function VendorHeatmapSection({
  vendors,
  window,
}: {
  vendors: VendorStatusRow[];
  window: StatusWindow;
}) {
  const { t } = useTranslation("status");
  const hours = window === "24h" ? 24 : window === "168h" ? 168 : 720;

  if (vendors.length === 0) return null;

  return (
    <Card className="p-4 lg:p-5">
      <div className="mb-3 flex items-baseline justify-between">
        <h2 className="text-body font-semibold">{t("heatmap.title")}</h2>
        <span className="text-label text-fg-tertiary">{t("heatmap.subtitle", { hours })}</span>
      </div>
      <div className="space-y-1.5">
        {vendors.map((v) => (
          <HeatmapRow key={v.anon_id} vendor={v} window={window} hours={hours} />
        ))}
      </div>
      <TimeAxis hours={hours} />
    </Card>
  );
}

function HeatmapRow({
  vendor,
  window,
  hours,
}: {
  vendor: VendorStatusRow;
  window: StatusWindow;
  hours: number;
}) {
  const { data } = useVendorDispatchEvents(vendor.anon_id, window);
  const events = data?.events ?? [];

  const bucketMs =
    hours <= 48 ? 3600_000 :
    hours <= 336 ? 6 * 3600_000 :
                   24 * 3600_000;

  const buckets = useMemo(() => {
    const now = Date.now();
    const from = now - hours * 3600_000;
    const start = Math.floor(from / bucketMs) * bucketMs;
    const map = new Map<number, number>();
    for (let ts = start; ts <= now; ts += bucketMs) map.set(ts, 0);
    for (const e of events) {
      const ts = new Date(e.at).getTime();
      if (ts < start) continue;
      const key = Math.floor(ts / bucketMs) * bucketMs;
      map.set(key, (map.get(key) ?? 0) + e.count);
    }
    return Array.from(map.entries()).sort((a, b) => a[0] - b[0]);
  }, [events, hours, bucketMs]);

  // 找该行最大值 · 用于颜色深浅归一化（每家自己的量级）
  const maxKeys = buckets.reduce((m, [, k]) => Math.max(m, k), 0);

  return (
    <div className="flex items-center gap-3">
      {/* 左侧标签 · 固定宽 · 跟卡片对齐 */}
      <div className="w-24 shrink-0 truncate text-label font-medium text-fg-secondary">
        {vendor.anon_label}
      </div>
      {/* 热力条 · 一格一桶 · 颜色深浅 = count 归一化 */}
      <div className="flex flex-1 gap-[2px]">
        {buckets.map(([ts, keys]) => (
          <HeatCell key={ts} keys={keys} maxKeys={maxKeys} ts={ts} bucketMs={bucketMs} />
        ))}
      </div>
    </div>
  );
}

function HeatCell({
  keys,
  maxKeys,
  ts,
  bucketMs,
}: {
  keys: number;
  maxKeys: number;
  ts: number;
  bucketMs: number;
}) {
  // 归一到 [0, 1] · 空桶保留浅灰墩（缺口 vs "根本没开号"分不清 · 都是浅色）
  const intensity = maxKeys > 0 ? keys / maxKeys : 0;
  const style = keys === 0
    ? { backgroundColor: "var(--bg-tertiary, #f3f4f6)" }
    : {
        // 用 brand 色系 · alpha 从 0.15 到 1.0
        backgroundColor: `color-mix(in oklab, var(--brand, #6366f1) ${Math.round(15 + intensity * 85)}%, transparent)`,
      };
  const end = new Date(ts + bucketMs).toISOString();
  const start = new Date(ts).toISOString();
  const title = keys === 0
    ? `${start.slice(5, 16)} — ${end.slice(11, 16)} · 无开号`
    : `${start.slice(5, 16)} — ${end.slice(11, 16)} · ${keys} keys`;

  return (
    <div
      className={cn("h-4 flex-1 rounded-[2px] transition-opacity hover:opacity-70")}
      style={style}
      title={title}
    />
  );
}

function TimeAxis({ hours }: { hours: number }) {
  const { t } = useTranslation("status");
  // 分 5 段 label · 都是相对时间 · 用统一 unit（<=48h 用 hours · 否则用 days）
  const useDays = hours > 48;
  const marks = useMemo(() => {
    const n = 5;
    const out: { label: string; pos: number }[] = [];
    for (let i = 0; i < n; i++) {
      const pos = i / (n - 1);
      const ago = hours * (1 - pos);
      const label = useDays
        ? t("heatmap.days-ago", { days: Math.round(ago / 24) })
        : t("heatmap.hours-ago", { hours: Math.round(ago) });
      out.push({ label, pos });
    }
    return out;
  }, [hours, useDays, t]);

  return (
    <div className="mt-2 flex items-center gap-3">
      <div className="w-24 shrink-0" />
      <div className="relative flex-1 text-[10px] text-fg-tertiary">
        {marks.map((m, i) => (
          <span
            key={i}
            className="absolute -translate-x-1/2"
            style={{ left: `${m.pos * 100}%` }}
          >
            {m.label}
          </span>
        ))}
        <span className="invisible">.</span>
      </div>
    </div>
  );
}
