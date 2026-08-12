import { useEffect, useState } from "react";
import { Info, Loader2, Save, ShieldAlert } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useGlobalStrategy, useMe, useSaveGlobalStrategy } from "@/api/hooks";
import { SettingsHead } from "@/components/SettingsHead";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
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
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!gs || loaded) return;
    setRoundLimit(gs.daily_round_limit == null ? "" : String(gs.daily_round_limit));
    setSpendLimit(gs.daily_spend_limit == null ? "" : String(toCredits(gs.daily_spend_limit)));
    setCount(String(gs.per_round_count));
    setMaxPrice(gs.max_unit_price == null ? "" : String(toCredits(gs.max_unit_price)));
    setVendor(gs.preferred_vendor ?? "auto");
    setZone(gs.default_zone);
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

      {/* ── 新车默认值 · 只影响以后建的车 ── */}
      <Card className="p-7">
        <SectionHead
          title={t("defaults.title")}
          sub={t("defaults.sub")}
        />

        <div className="mt-4 grid grid-cols-1 gap-5 sm:grid-cols-2">
          <Field label={t("defaults.count.label")}>
            <Input
              type="number"
              min={1}
              value={count}
              onChange={(e) => setCount(e.target.value)}
              placeholder="3"
            />
          </Field>

          <Field label={t("defaults.vendor.label")}>
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
