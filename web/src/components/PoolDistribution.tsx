import { useTranslation } from "react-i18next";
import type { Credential } from "@/types";
import { useMe } from "@/api/hooks";
import { Label } from "./ui/primitives";
import {
  vendorColor, vendorLabel,
} from "@/lib/utils";

/** 号池分布 · 按 vendor 分段 · 沿用概览拼车卡的紫渐深条
    - full · 展示分段条 + 明细分行（BusCard 紧凑卡用）
    - compact · 只展示分段条 + inline chip 明细（Focal 大卡用） */
export function PoolDistribution({
  credentials, variant = "full", label,
}: {
  credentials: Credential[] | undefined;
  variant?: "full" | "compact";
  label?: string;
}) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const alive = (credentials ?? []).filter((c) => c.status === "alive");
  const total = alive.length;

  const byVendor = new Map<string, number>();
  for (const c of alive) byVendor.set(c.vendor_id, (byVendor.get(c.vendor_id) ?? 0) + 1);
  const rows = [...byVendor.entries()]
    .map(([id, n]) => ({ id, n }))
    .sort((a, b) => b.n - a.n);

  if (variant === "compact") {
    return (
      <div className="space-y-1.5">
        {label !== undefined && (
          <div className="flex items-baseline justify-between">
            <Label>{label}</Label>
            <span className="text-label text-fg-tertiary">
              {rows.length
                ? t("pool-distribution.vendor-count", { count: rows.length })
                : t("pool-distribution.empty")}
            </span>
          </div>
        )}
        <div className="flex h-2 overflow-hidden rounded-full bg-hairline">
          {rows.map((v, i) => (
            <div
              key={v.id}
              style={{
                width: `${(v.n / Math.max(1, total)) * 100}%`,
                backgroundColor: shadeFor(i),
              }}
            />
          ))}
        </div>
        {rows.length > 0 && (
          <div className="flex flex-wrap gap-x-3 gap-y-1 pt-0.5">
            {rows.map((v, i) => (
              <span key={v.id} className="flex items-center gap-1.5 text-label">
                <span
                  className="size-[7px] shrink-0 rounded-full"
                  style={{ backgroundColor: shadeFor(i) }}
                />
                <span className="font-medium text-fg-secondary">{vendorLabel(v.id, me?.tier)}</span>
                <span className="font-semibold tnum text-fg-tertiary">{v.n}</span>
              </span>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-2.5">
      {label !== undefined && <Label>{label}</Label>}
      <div className="flex h-2.5 overflow-hidden rounded-full bg-hairline">
        {rows.map((v, i) => (
          <div
            key={v.id}
            style={{
              width: `${(v.n / Math.max(1, total)) * 100}%`,
              backgroundColor: shadeFor(i),
            }}
          />
        ))}
      </div>
      <div className="space-y-2 pt-1">
        {rows.length === 0 ? (
          <div className="text-label text-fg-tertiary">{t("pool-distribution.empty-full")}</div>
        ) : (
          rows.map((v, i) => (
            <div key={v.id} className="flex items-center gap-2">
              <span
                className="size-[7px] shrink-0 rounded-full"
                style={{ backgroundColor: shadeFor(i) }}
              />
              <span className="min-w-0 flex-1 truncate font-medium text-fg-secondary">
                {vendorLabel(v.id, me?.tier)}
              </span>
              <span className="font-semibold tnum">{t("pool-distribution.row-unit", { count: v.n })}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function shadeFor(i: number): string {
  const shades = ["#9147FF", "#A574FF", "#C9A9FF", "#DBBFFF"];
  return shades[i] ?? vendorColor(String(i));
}
