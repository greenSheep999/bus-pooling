import { AlertTriangle, Clock, Coins, Package, ShieldCheck, Sparkles, TrendingUp } from "lucide-react";
import { useAutoPick, useVendorHistory, useVendorStock } from "@/api/hooks";
import { Muted } from "@/components/ui/primitives";
import { cn, fmtLifespan, toCredits } from "@/lib/utils";
import type { Zone } from "@/types";

/** 上游即时状态面板 · docs/14 §4.3 · decisions §8.20
 *  - vendor = auto：展示**系统派号推荐结果**（推荐到谁 + 最终价 + 库存质保成活率 + 理由）
 *    散客默认走这条 · 必须能看到价格才能下单 · 不留"选具体 vendor 看详情"空占位
 *  - vendor = 具体：展示该 vendor 即时状态
 *  单价一律是**最终价**（含附加费）· 前端拿不到原价 */
export function UpstreamStatusPanel({
  vendorId, zone, inviteCode,
}: {
  vendorId: string;                 // "auto" | 具体 id
  zone: Zone | "auto";
  /** 消费邀请码 · 填了本次免加价 */
  inviteCode?: string;
}) {
  const isAuto = vendorId === "auto";
  const { data: pick, isLoading: pickLoading } = useAutoPick(zone, inviteCode);
  const { data: stock, isLoading: stockLoading } = useVendorStock(
    isAuto ? undefined : vendorId,
    inviteCode,
  );
  const { data: history } = useVendorHistory(isAuto ? undefined : vendorId);

  /* ── auto：系统派号推荐 ── */
  if (isAuto) {
    if (pickLoading || !pick) {
      return <PanelShell><Muted>正在挑最优 vendor…</Muted></PanelShell>;
    }
    const outOfStock = pick.available === 0;
    return (
      <PanelShell>
        {/* 头 · 系统派号 + 推荐到谁 */}
        <div className="mb-3 flex items-start gap-2">
          <Sparkles className="mt-0.5 size-4 shrink-0 text-brand-strong" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2 text-label">
              <span className="font-semibold text-fg">系统派号</span>
              {!outOfStock && (
                <>
                  <span className="text-fg-tertiary">→</span>
                  <span className="font-semibold text-brand-strong">{pick.vendor_label}</span>
                  {pick.zone && <span className="text-fg-tertiary">· {pick.zone}</span>}
                </>
              )}
            </div>
            <p className="mt-0.5 text-label text-fg-tertiary">{pick.reason}</p>
          </div>
        </div>

        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-label">
          <StatRow
            icon={Package}
            label="库存"
            value={
              outOfStock ? (
                <span className="text-danger-fg">0 · 缺货</span>
              ) : (
                <><strong className="tnum">{pick.available}</strong> 个 <Muted>可提</Muted></>
              )
            }
          />
          <StatRow
            icon={Coins}
            label="单价"
            value={<><strong className="tnum">{toCredits(pick.unit_price)}</strong> 积分 / 个</>}
          />
          <StatRow
            icon={ShieldCheck}
            label="质保"
            value={
              pick.warranty_minutes === 0 ? (
                <span className="text-warn-fg">无质保</span>
              ) : (
                <><strong className="tnum">{pick.warranty_minutes}</strong> 分钟内失效可退</>
              )
            }
          />
          <StatRow
            icon={Clock}
            label="单次上限"
            value={<><strong className="tnum">{pick.max_per_order}</strong> 个 / 单</>}
          />
          <StatRow
            icon={TrendingUp}
            label="历史存活"
            value={
              pick.alive_rate_30d > 0 ? (
                <>
                  平均 <strong className="tnum">{fmtLifespan(pick.avg_lifespan_seconds)}</strong>
                  {" · "}
                  <Muted>30 天成活率</Muted>{" "}
                  <strong className={cn("tnum", pick.alive_rate_30d >= 80 ? "text-ok-fg" : "text-warn-fg")}>
                    {pick.alive_rate_30d}%
                  </strong>
                </>
              ) : (
                <Muted>暂无数据</Muted>
              )
            }
            wide
          />
        </dl>
      </PanelShell>
    );
  }

  /* ── 具体 vendor ── */
  if (stockLoading || !stock) {
    return <PanelShell><Muted>加载 vendor 状态…</Muted></PanelShell>;
  }

  /* 找该 zone 对应库存 · 无区域 vendor 用 zones[0]（label="全区"） */
  const noRegion = stock.zones.length === 1 && stock.zones[0].label === "全区";
  const activeZone = noRegion
    ? stock.zones[0]
    : zone === "auto"
      ? stock.zones.reduce((a, b) => (a.unit_price <= b.unit_price ? a : b))
      : stock.zones.find((z) => z.zone === zone) ?? stock.zones[0];

  const outOfStock = activeZone.available === 0;
  const noWarranty = stock.warranty_minutes === 0;
  const holdCapWarn = stock.hold_cap_remaining !== null && stock.hold_cap_remaining <= 3;

  return (
    <PanelShell>
      <div className="mb-3 flex items-center justify-between text-label">
        <div className="font-semibold text-fg">上游即时状态</div>
        {!noRegion && zone === "auto" && (
          <span className="text-fg-tertiary">默认区 · {activeZone.label}</span>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-label">
        <StatRow
          icon={Package}
          label="库存"
          value={
            outOfStock ? (
              <span className="text-danger-fg">0 · 缺货</span>
            ) : (
              <><strong className="tnum">{activeZone.available}</strong> 个 <Muted>可提</Muted></>
            )
          }
        />
        <StatRow
          icon={Coins}
          label="单价"
          value={<><strong className="tnum">{toCredits(activeZone.unit_price)}</strong> 积分 / 个</>}
        />
        <StatRow
          icon={ShieldCheck}
          label="质保"
          value={
            noWarranty ? (
              <span className="text-warn-fg">无质保</span>
            ) : (
              <><strong className="tnum">{stock.warranty_minutes}</strong> 分钟内失效可退</>
            )
          }
        />
        <StatRow
          icon={Clock}
          label="单次上限"
          value={<><strong className="tnum">{stock.max_per_order}</strong> 个 / 单</>}
        />
        {history && (
          <StatRow
            icon={TrendingUp}
            label="历史存活"
            value={
              history.total_pulled_30d > 0 ? (
                <>
                  平均 <strong className="tnum">{fmtLifespan(history.avg_lifespan_seconds)}</strong>
                  {" · "}
                  <Muted>30 天成活率</Muted>{" "}
                  <strong className={cn("tnum", history.alive_rate_30d >= 80 ? "text-ok-fg" : "text-warn-fg")}>
                    {history.alive_rate_30d}%
                  </strong>
                </>
              ) : (
                <Muted>近 30 天没拉过</Muted>
              )
            }
            wide
          />
        )}
      </dl>

      {(holdCapWarn || stock.currency === "cny_usd" || noRegion) && (
        <div className="mt-3 space-y-2">
          {holdCapWarn && stock.hold_cap_remaining !== null && (
            <VendorNote tone="warn">
              这家 vendor 名下持有额度只剩{" "}
              <strong className="tnum">{stock.hold_cap_remaining}</strong> 个 · 拉多了会被拒
            </VendorNote>
          )}
          {stock.currency === "cny_usd" && (
            <VendorNote tone="warn">
              这家 vendor 按美元定价 · 实扣按当日汇率折算成积分
            </VendorNote>
          )}
          {noRegion && (
            <VendorNote tone="neutral">这家 vendor 不分区域 · 一档到底</VendorNote>
          )}
        </div>
      )}
    </PanelShell>
  );
}

/* ─────────────── 小组件 ─────────────── */

function PanelShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-hairline bg-bg-elevated/40 p-4">{children}</div>
  );
}

function StatRow({
  icon: Icon, label, value, wide,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: React.ReactNode;
  wide?: boolean;
}) {
  return (
    <div className={cn("flex items-baseline gap-2", wide && "col-span-2")}>
      <Icon className="size-3.5 shrink-0 text-fg-tertiary" />
      <dt className="shrink-0 text-fg-tertiary">{label}</dt>
      <dd className="ml-auto min-w-0 truncate text-right text-fg">{value}</dd>
    </div>
  );
}

function VendorNote({
  tone, children,
}: { tone: "warn" | "neutral"; children: React.ReactNode }) {
  return (
    <div
      className={cn(
        "flex items-start gap-2 rounded-lg p-2 text-label",
        tone === "warn" ? "bg-warn-bg/40 text-warn-fg" : "bg-bg-elevated text-fg-secondary",
      )}
    >
      <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </div>
  );
}
