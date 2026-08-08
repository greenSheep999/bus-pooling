import { Users, Zap, ZapOff } from "lucide-react";
import type { Bus } from "@/types";
import { useBusCredentials } from "@/api/hooks";
import { Card, Label, Muted, Stat } from "./ui/primitives";
import { cn, fmtCredits, fmtLifespan, toCredits, vendorColor, vendorName } from "@/lib/utils";

/** 车卡 · 沿用概览页拼车卡视觉语言（Stat + 号池分段 Meter + 明细分号）
    每张卡展示自己车的号在各 vendor 上的分布 · 底部策略摘要 */
export function BusCard({ bus, role }: { bus: Bus; role?: "owner" | "member" }) {
  const { data: creds } = useBusCredentials(bus.id);
  const items = creds ?? [];
  const alive = items.filter((c) => c.status === "alive");
  const totalAlive = alive.length;

  // 按 vendor 汇总活号数
  const byVendor = new Map<string, number>();
  for (const c of alive) {
    byVendor.set(c.vendor_id, (byVendor.get(c.vendor_id) ?? 0) + 1);
  }
  const vendorRows = [...byVendor.entries()]
    .map(([id, n]) => ({ id, n }))
    .sort((a, b) => b.n - a.n);

  const s = bus.strategy;

  return (
    <Card to={`/buses/${bus.id}`} className="flex flex-col gap-4 p-6">
      {/* 头 · 图标 + 车名 + 「我发起」·「查看 →」 */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-brand-subtle">
            <Users className="size-3.5 text-brand-strong" />
          </span>
          <h3 className="min-w-0 truncate text-body-lg font-semibold">
            {bus.name}
          </h3>
          {role === "owner" && (
            <span className="shrink-0 whitespace-nowrap rounded-md bg-brand-subtle px-1.5 py-[1px] text-[10px] font-semibold leading-[1.4] text-brand-strong">
              我发起
            </span>
          )}
        </div>
        <span className="shrink-0 text-label font-semibold text-brand-strong">
          查看 →
        </span>
      </div>

      {/* 主数字 · 号在池 · 副行策略提示 */}
      <Stat
        value={String(totalAlive)}
        unit={`个号在池 · 失效 ${bus.dead_count}`}
        size="num"
      />

      {/* 号池分布 · 按 vendor 分段 · 沿用概览拼车卡视觉 */}
      <div className="space-y-2.5">
        <Label>号池分布 · 按 vendor</Label>
        <div className="flex h-2.5 overflow-hidden rounded-full bg-hairline">
          {vendorRows.map((v, i) => (
            <div
              key={v.id}
              style={{
                width: `${(v.n / Math.max(1, totalAlive)) * 100}%`,
                backgroundColor: shadeFor(i),
              }}
            />
          ))}
        </div>
        <div className="space-y-2 pt-1">
          {vendorRows.length === 0 ? (
            <div className="text-label text-fg-tertiary">号池空 · 待拉号</div>
          ) : (
            vendorRows.map((v, i) => (
              <div key={v.id} className="flex items-center gap-2">
                <span
                  className="size-[7px] shrink-0 rounded-full"
                  style={{ backgroundColor: shadeFor(i) }}
                />
                <span className="min-w-0 flex-1 truncate font-medium text-fg-secondary">
                  {vendorName(v.id)}
                </span>
                <span className="font-semibold tnum">{v.n} 个</span>
              </div>
            ))
          )}
        </div>
      </div>

      {/* 策略摘要一行 · 简洁不占位 */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-label">
        {s.auto_refill_enabled ? (
          <span className="flex items-center gap-1 font-medium text-brand-strong">
            <Zap className="size-3.5" />
            自动补车 · 水位 <span className="font-semibold tnum">{s.refill_watermark}</span>
          </span>
        ) : (
          <span className="flex items-center gap-1 font-medium text-fg-tertiary">
            <ZapOff className="size-3.5" />
            手动模式
          </span>
        )}
        {s.max_unit_price && (
          <span className="text-fg-tertiary">
            单价 ≤ <span className="font-semibold tnum text-fg-secondary">{toCredits(s.max_unit_price)}</span>
          </span>
        )}
        {s.preferred_vendor && (
          <span className="text-fg-tertiary">
            首选 <span className="font-medium text-fg-secondary">{vendorName(s.preferred_vendor)}</span>
          </span>
        )}
      </div>

      {/* 底行 · 分隔线 · 今日消费 + 平均寿命 */}
      <div className="mt-auto flex items-center justify-between border-t border-hairline pt-3.5">
        <Muted className="font-medium">
          今日消费{" "}
          <span
            className={cn(
              "font-semibold tnum",
              bus.spend_today > 0 ? "text-danger-fg" : "text-fg-tertiary",
            )}
          >
            {bus.spend_today > 0 ? `-${fmtCredits(bus.spend_today)}` : "0"}
          </span>
        </Muted>
        <Muted className="font-medium">
          平均寿命{" "}
          <span className="font-semibold tnum text-fg-secondary">
            {fmtLifespan(bus.avg_lifespan_seconds)}
          </span>
        </Muted>
      </div>
    </Card>
  );
}

/** 号池分布的紫色渐深条 · 3 档循环（跟概览拼车卡同款） */
function shadeFor(i: number): string {
  const shades = ["#9147FF", "#A574FF", "#C9A9FF"];
  // 3 档以上继续 vendor 主色兜底
  return shades[i] ?? vendorColor(String(i));
}
