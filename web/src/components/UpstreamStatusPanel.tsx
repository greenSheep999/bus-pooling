import { useTranslation } from "react-i18next";
import { AlertTriangle, Clock, Coins, Package, ShieldCheck, Sparkles, TrendingUp } from "lucide-react";
import { useAutoPick, useVendorHistory, useVendorStock } from "@/api/hooks";
import { Muted } from "@/components/ui/primitives";
import { cn, fmtLifespan, toCredits } from "@/lib/utils";
import type { Zone } from "@/types";

/** 上游即时状态面板 · docs/14 §4.3 · decisions §8.20
 *  - vendor = auto：展示**系统派号推荐结果**（推荐到谁 + 最终价 + 库存质保成活率 + 理由）
 *    散客默认走这条 · 必须能看到价格才能下单 · 不留"选具体 vendor 看详情"空占位
 *  - vendor = 具体：展示该 vendor 即时状态
 *  单价一律是**最终价**（已含所有分项）· 前端拿不到原价 */
export function UpstreamStatusPanel({
  vendorId, zone, inviteCode,
}: {
  vendorId: string;                 // "auto" | 具体 id
  zone: Zone | "auto";
  /** 消费邀请码 · 填了本次按优惠价 */
  inviteCode?: string;
}) {
  const { t } = useTranslation("extract");
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
      return <PanelShell><Muted>{t("upstream-panel.loading-auto")}</Muted></PanelShell>;
    }
    const outOfStock = pick.available === 0;
    return (
      <PanelShell>
        {/* 头 · 系统派号 + 推荐到谁 */}
        <div className="mb-3 flex items-start gap-2">
          <Sparkles className="mt-0.5 size-4 shrink-0 text-brand-strong" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2 text-label">
              <span className="font-semibold text-fg">{t("upstream-panel.auto-title")}</span>
              {!outOfStock && (
                <>
                  <span className="text-fg-tertiary">{t("upstream-panel.auto-arrow")}</span>
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
            label={t("upstream-panel.stat.stock")}
            value={
              outOfStock ? (
                <span className="text-danger-fg">{t("upstream-panel.stock.out-of-stock")}</span>
              ) : (
                <><strong className="tnum">{pick.available}</strong> {t("upstream-panel.stock.available")} <Muted>{t("upstream-panel.stock.available-suffix")}</Muted></>
              )
            }
          />
          <StatRow
            icon={Coins}
            label={t("upstream-panel.stat.unit-price")}
            value={<><strong className="tnum">{toCredits(pick.unit_price)}</strong> {t("upstream-panel.unit-price-value")}</>}
          />
          <StatRow
            icon={ShieldCheck}
            label={t("upstream-panel.stat.warranty")}
            value={
              pick.warranty_minutes === 0 ? (
                <span className="text-warn-fg">{t("upstream-panel.warranty.none")}</span>
              ) : (
                <><strong className="tnum">{pick.warranty_minutes}</strong> {t("upstream-panel.warranty.minutes-suffix")}</>
              )
            }
          />
          <StatRow
            icon={Clock}
            label={t("upstream-panel.stat.max-per-order")}
            value={<><strong className="tnum">{pick.max_per_order}</strong> {t("upstream-panel.max-per-order-value")}</>}
          />
          <StatRow
            icon={TrendingUp}
            label={t("upstream-panel.stat.history")}
            value={
              pick.alive_rate_30d > 0 ? (
                <>
                  {t("upstream-panel.history-line.avg-prefix")} <strong className="tnum">{fmtLifespan(pick.avg_lifespan_seconds)}</strong>
                  {t("upstream-panel.history-line.sep")}
                  <Muted>{t("upstream-panel.history-line.alive-rate-30d")}</Muted>{" "}
                  <strong className={cn("tnum", pick.alive_rate_30d >= 80 ? "text-ok-fg" : "text-warn-fg")}>
                    {pick.alive_rate_30d}{t("upstream-panel.history-line.percent")}
                  </strong>
                </>
              ) : (
                <Muted>{t("upstream-panel.history-line.empty")}</Muted>
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
    return <PanelShell><Muted>{t("upstream-panel.loading-vendor")}</Muted></PanelShell>;
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
        <div className="font-semibold text-fg">{t("upstream-panel.vendor-title")}</div>
        {!noRegion && zone === "auto" && (
          <span className="text-fg-tertiary">{t("upstream-panel.default-zone-prefix")}{activeZone.label}</span>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-label">
        <StatRow
          icon={Package}
          label={t("upstream-panel.stat.stock")}
          value={
            outOfStock ? (
              <span className="text-danger-fg">{t("upstream-panel.stock.out-of-stock")}</span>
            ) : (
              <><strong className="tnum">{activeZone.available}</strong> {t("upstream-panel.stock.available")} <Muted>{t("upstream-panel.stock.available-suffix")}</Muted></>
            )
          }
        />
        <StatRow
          icon={Coins}
          label={t("upstream-panel.stat.unit-price")}
          value={<><strong className="tnum">{toCredits(activeZone.unit_price)}</strong> {t("upstream-panel.unit-price-value")}</>}
        />
        <StatRow
          icon={ShieldCheck}
          label={t("upstream-panel.stat.warranty")}
          value={
            noWarranty ? (
              <span className="text-warn-fg">{t("upstream-panel.warranty.none")}</span>
            ) : (
              <><strong className="tnum">{stock.warranty_minutes}</strong> {t("upstream-panel.warranty.minutes-suffix")}</>
            )
          }
        />
        <StatRow
          icon={Clock}
          label={t("upstream-panel.stat.max-per-order")}
          value={<><strong className="tnum">{stock.max_per_order}</strong> {t("upstream-panel.max-per-order-value")}</>}
        />
        {history && (
          <StatRow
            icon={TrendingUp}
            label={t("upstream-panel.stat.history")}
            value={
              history.total_pulled_30d > 0 ? (
                <>
                  {t("upstream-panel.history-line.avg-prefix")} <strong className="tnum">{fmtLifespan(history.avg_lifespan_seconds)}</strong>
                  {t("upstream-panel.history-line.sep")}
                  <Muted>{t("upstream-panel.history-line.alive-rate-30d")}</Muted>{" "}
                  <strong className={cn("tnum", history.alive_rate_30d >= 80 ? "text-ok-fg" : "text-warn-fg")}>
                    {history.alive_rate_30d}{t("upstream-panel.history-line.percent")}
                  </strong>
                </>
              ) : (
                <Muted>{t("upstream-panel.history-line.empty-recent")}</Muted>
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
              {t("upstream-panel.note.hold-cap-prefix")}{" "}
              <strong className="tnum">{stock.hold_cap_remaining}</strong> {t("upstream-panel.note.hold-cap-suffix")}
            </VendorNote>
          )}
          {stock.currency === "cny_usd" && (
            <VendorNote tone="warn">
              {t("upstream-panel.note.currency")}
            </VendorNote>
          )}
          {noRegion && (
            <VendorNote tone="neutral">{t("upstream-panel.note.no-region")}</VendorNote>
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
