import { useEffect, useState } from "react";
import { Info, Loader2, Save, ShieldAlert, Zap } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useGlobalStrategy, useMe, useSaveGlobalStrategy } from "@/api/hooks";
import { SettingsHead } from "@/components/SettingsHead";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Card, Em, Meter, SectionHead } from "@/components/ui/primitives";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { fmtCredits, MICRO, toCredits, vendorLabel, VENDOR_NAME } from "@/lib/utils";

/** 空字符串 = 不限 · 输入框拿字符串存，保存时转 number|null */
const numOrNull = (s: string): number | null => {
  const t = s.trim();
  if (t === "") return null;
  const n = Number(t);
  return Number.isFinite(n) && n > 0 ? Math.round(n) : null;
};

export default function Preferences() {
  const { t } = useTranslation("settings");
  const { data: me } = useMe();
  const { data: gs } = useGlobalStrategy();
  const save = useSaveGlobalStrategy();

  /* 限额（硬上限） */
  const [roundLimit, setRoundLimit] = useState("");
  const [spendLimit, setSpendLimit] = useState("");
  /* 新车默认值 */
  const [count, setCount] = useState("");
  const [maxPrice, setMaxPrice] = useState("");
  const [vendor, setVendor] = useState("auto");
  const [zone, setZone] = useState("auto");
  /* 建车 seed(1f-refactor · migration 040 · 只做新车默认 · 不做运行时 fallback) */
  const [autoRefill, setAutoRefill] = useState(false);
  const [watermark, setWatermark] = useState("");
  const [minCount, setMinCount] = useState("");
  /* 全局跨车调度护栏(migration 040 · 真正需要全局才能表达 · CLAUDE §1.5) */
  const [dailyBudget, setDailyBudget] = useState("");
  const [minReserve, setMinReserve] = useState("");
  const [allowlist, setAllowlist] = useState<string[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!gs || loaded) return;
    setRoundLimit(gs.daily_round_limit == null ? "" : String(gs.daily_round_limit));
    setSpendLimit(gs.daily_spend_limit == null ? "" : String(toCredits(gs.daily_spend_limit)));
    setCount(String(gs.per_round_count));
    setMaxPrice(gs.max_unit_price == null ? "" : String(toCredits(gs.max_unit_price)));
    setVendor(gs.preferred_vendor ?? "auto");
    setZone(gs.default_zone);
    setAutoRefill(gs.default_auto_refill_enabled);
    setWatermark(String(gs.default_refill_watermark));
    setMinCount(gs.default_refill_min_count == null ? "" : String(gs.default_refill_min_count));
    setDailyBudget(gs.auto_refill_daily_budget == null ? "" : String(toCredits(gs.auto_refill_daily_budget)));
    setMinReserve(gs.auto_refill_min_wallet_reserve == null ? "" : String(toCredits(gs.auto_refill_min_wallet_reserve)));
    setAllowlist(gs.auto_refill_vendor_allowlist ?? []);
    setLoaded(true);
  }, [gs, loaded]);

  const onSave = () =>
    save.mutate({
      daily_round_limit: numOrNull(roundLimit),
      daily_spend_limit: numOrNull(spendLimit) == null ? null : numOrNull(spendLimit)! * MICRO,
      per_round_count: numOrNull(count) ?? 1,
      max_unit_price: numOrNull(maxPrice) == null ? null : numOrNull(maxPrice)! * MICRO,
      preferred_vendor: vendor === "auto" ? null : vendor,
      default_zone: zone,
      /* 建车 seed · auto/watermark 非空(required-like) · min_count 允许 null(按 gap 补) */
      default_auto_refill_enabled: autoRefill,
      default_refill_watermark: numOrNull(watermark) ?? 0,
      default_refill_min_count: numOrNull(minCount),
      /* 全局跨车调度护栏(migration 040) · null = 不限 · [] = 不限 */
      auto_refill_daily_budget: numOrNull(dailyBudget) == null ? null : numOrNull(dailyBudget)! * MICRO,
      auto_refill_min_wallet_reserve: numOrNull(minReserve) == null ? null : numOrNull(minReserve)! * MICRO,
      auto_refill_vendor_allowlist: allowlist.length === 0 ? null : allowlist,
    });

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb={t("head.crumb")}
        title={t("head.title")}
        desc={t("head.desc")}
        right={
          <Button variant="brand" onClick={onSave} disabled={!gs || save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            {save.isPending ? t("action.saving") : t("action.save")}
          </Button>
        }
      />

      {/* ── 硬上限 · 真的会拦下操作，放在最前面 ── */}
      <Card className="p-7">
        <SectionHead
          title={t("limit.title")}
          sub={t("limit.sub")}
        />

        {/* 单价上限单独一行 —— 它是"每次都判"，跟下面两个"每天累加"的判法不一样 */}
        <div className="mt-4">
          <Field
            label={t("limit.max-price.label")}
            hint={t("limit.max-price.hint")}
          >
            <Input
              type="number"
              min={1}
              value={maxPrice}
              onChange={(e) => setMaxPrice(e.target.value)}
              placeholder={t("limit.placeholder.unlimited")}
              className="sm:max-w-[220px]"
            />
          </Field>
          <p className="mt-1.5 text-label text-fg-tertiary">
            {t("limit.max-price.note")}
          </p>
        </div>

        <div className="mt-5 grid grid-cols-1 gap-5 border-t border-hairline pt-5 sm:grid-cols-2">
          <div className="space-y-2">
            <Field label={t("limit.rounds.label")} hint={t("limit.rounds.hint")}>
              <Input
                type="number"
                min={1}
                value={roundLimit}
                onChange={(e) => setRoundLimit(e.target.value)}
                placeholder={t("limit.placeholder.unlimited")}
              />
            </Field>
            <UsageBar
              used={gs?.used_today.rounds ?? 0}
              limit={numOrNull(roundLimit)}
              unit={t("usage.unit.round")}
            />
          </div>

          <div className="space-y-2">
            <Field label={t("limit.spend.label")} hint={t("limit.spend.hint")}>
              <Input
                type="number"
                min={1}
                value={spendLimit}
                onChange={(e) => setSpendLimit(e.target.value)}
                placeholder={t("limit.placeholder.unlimited")}
              />
            </Field>
            <UsageBar
              used={toCredits(gs?.used_today.spend ?? 0)}
              limit={numOrNull(spendLimit)}
              unit={t("usage.unit.credits")}
            />
          </div>
        </div>

        <Alert tone="neutral" icon={ShieldAlert} className="mt-4">
          {t("limit.alert")}
          <Link to="/buses" className="ml-1 font-semibold text-brand-strong hover:underline">
            {t("limit.alert.link")}
          </Link>
        </Alert>
      </Card>

      {/* ── 新车默认值 · 建新车 seed + 车级"跟随全局"时的运行时值(§4.3.5.4) ── */}
      <Card className="p-7">
        <SectionHead
          title={t("defaults.title")}
          sub={t("defaults.sub")}
        />

        <div className="mt-4 grid grid-cols-1 gap-5 sm:grid-cols-2">
          <Field label={t("defaults.count.label")} hint={t("defaults.hint-runtime")}>
            <Input
              type="number"
              min={1}
              value={count}
              onChange={(e) => setCount(e.target.value)}
              placeholder="3"
            />
          </Field>

          <Field label={t("defaults.vendor.label")} hint={t("defaults.hint-runtime")}>
            <Select value={vendor} onValueChange={setVendor}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">{t("defaults.vendor.auto")}</SelectItem>
                {Object.keys(VENDOR_NAME).map((id) => (
                  <SelectItem key={id} value={id}>
                    {vendorLabel(id, me?.tier)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field label={t("defaults.zone.label")}>
            <Select value={zone} onValueChange={setZone}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">{t("defaults.zone.auto")}</SelectItem>
                <SelectItem value="us">{t("defaults.zone.us")}</SelectItem>
                <SelectItem value="eu">{t("defaults.zone.eu")}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </div>

        <Alert tone="neutral" icon={Info} className="mt-4">
          {t("defaults.alert.pre")}<Em plain>{t("defaults.alert.em")}</Em>{t("defaults.alert.post")}
        </Alert>
      </Card>

      {/* ── 自动补车全局默认(1f-B · §4.3.5.4)· 建新车 seed + 车级 null 时 fallback ── */}
      <Card className="p-7">
        <SectionHead
          title={t("defaults-auto.title")}
          sub={t("defaults-auto.sub")}
        />

        {/* 主开关 */}
        <div
          className="mt-4 flex cursor-pointer items-center gap-3 rounded-2xl border border-hairline bg-bg p-4 transition-colors hover:bg-bg-elevated/40"
          onClick={() => setAutoRefill((v) => !v)}
        >
          <Zap className={autoRefill ? "size-4 text-brand-strong" : "size-4 text-fg-tertiary"} />
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{t("defaults-auto.enabled.label")}</div>
            <div className="mt-0.5 text-label text-fg-tertiary">
              {t("defaults-auto.enabled.hint")}
            </div>
          </div>
          <Switch
            checked={autoRefill}
            onCheckedChange={setAutoRefill}
            onClick={(e) => e.stopPropagation()}
          />
        </div>

        <div className="mt-4 grid grid-cols-1 gap-5 sm:grid-cols-2">
          <Field label={t("defaults-auto.watermark.label")} hint={t("defaults-auto.watermark.hint")}>
            <Input
              type="number"
              min={0}
              value={watermark}
              onChange={(e) => setWatermark(e.target.value)}
              placeholder="0"
            />
          </Field>

          <Field label={t("defaults-auto.min-count.label")} hint={t("defaults-auto.min-count.hint")}>
            <Input
              type="number"
              min={1}
              value={minCount}
              onChange={(e) => setMinCount(e.target.value)}
              placeholder={t("defaults-auto.min-count.placeholder")}
            />
          </Field>
        </div>

        <Alert tone="neutral" icon={Info} className="mt-4">
          {t("defaults-auto.alert")}
        </Alert>
      </Card>

      {/* ── 跨车调度护栏(migration 040 · CLAUDE §1.5)· 真正需要全局才能表达的 ── */}
      <Card className="p-7">
        <SectionHead
          title={t("guardrails.title")}
          sub={t("guardrails.sub")}
        />

        <div className="mt-4 grid grid-cols-1 gap-5 sm:grid-cols-2">
          <Field label={t("guardrails.daily-budget.label")} hint={t("guardrails.daily-budget.hint")}>
            <Input
              type="number"
              min={0}
              value={dailyBudget}
              onChange={(e) => setDailyBudget(e.target.value)}
              placeholder={t("guardrails.placeholder-unlimited")}
            />
          </Field>

          <Field label={t("guardrails.min-reserve.label")} hint={t("guardrails.min-reserve.hint")}>
            <Input
              type="number"
              min={0}
              value={minReserve}
              onChange={(e) => setMinReserve(e.target.value)}
              placeholder={t("guardrails.placeholder-unlimited")}
            />
          </Field>
        </div>

        {/* vendor 白名单 · 多选 chip · 空 = 不限(所有启用 vendor) */}
        <div className="mt-5 space-y-2">
          <label className="text-label font-semibold text-fg-secondary">
            {t("guardrails.allowlist.label")}
          </label>
          <p className="text-label text-fg-tertiary">
            {t("guardrails.allowlist.hint")}
          </p>
          <div className="flex flex-wrap gap-2">
            {Object.keys(VENDOR_NAME).map((id) => {
              const on = allowlist.includes(id);
              return (
                <button
                  key={id}
                  type="button"
                  onClick={() => setAllowlist(on ? allowlist.filter((v) => v !== id) : [...allowlist, id])}
                  className={
                    "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-label transition-colors " +
                    (on
                      ? "border-brand-hairline bg-brand-subtle/40 font-semibold text-brand-fg"
                      : "border-hairline bg-bg-elevated/40 text-fg-secondary hover:bg-bg-elevated")
                  }
                >
                  {vendorLabel(id, me?.tier)}
                </button>
              );
            })}
          </div>
          {allowlist.length === 0 && (
            <p className="text-label text-fg-tertiary">
              {t("guardrails.allowlist.empty")}
            </p>
          )}
        </div>

        <Alert tone="neutral" icon={ShieldAlert} className="mt-4">
          {t("guardrails.alert")}
        </Alert>
      </Card>
    </div>
  );
}

/** 今日已用 / 上限 · 不限时不画进度条（没有分母，画了是假的） */
function UsageBar({
  used, limit, unit,
}: { used: number; limit: number | null; unit: string }) {
  const { t } = useTranslation("settings");
  const isCredits = unit === t("usage.unit.credits");
  if (limit == null) {
    return (
      <p className="text-label text-fg-tertiary">
        {t("usage.today")} <Em>{isCredits ? fmtCredits(used * MICRO) : used}</Em> {unit} {t("usage.unlimited-suffix")}
      </p>
    );
  }

  const pct = limit > 0 ? used / limit : 0;
  const color = pct >= 1 ? "#EF4444" : pct >= 0.8 ? "#F59E0B" : "#22C55E";

  return (
    <div className="space-y-1">
      <Meter value={used} max={limit} color={color} />
      <p className="text-label text-fg-tertiary">
        {t("usage.today")}{" "}
        <Em tone={pct >= 1 ? "spend" : pct >= 0.8 ? "warn" : undefined}>
          {isCredits ? fmtCredits(used * MICRO) : used}
        </Em>
        {" / "}{isCredits ? fmtCredits(limit * MICRO) : limit} {unit}
        {pct >= 1 && <span className="ml-1 font-semibold text-danger-fg">{t("usage.filled")}</span>}
      </p>
    </div>
  );
}
