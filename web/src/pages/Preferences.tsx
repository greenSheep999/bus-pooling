import { useEffect, useState } from "react";
import { Info, Loader2, Save, ShieldAlert } from "lucide-react";
import { Link } from "react-router-dom";
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
        crumb="拉号偏好"
        title="拉号偏好"
        desc="每天的总上限，和建新车时的默认值"
        right={
          <Button variant="brand" onClick={onSave} disabled={!gs || save.isPending}>
            {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
            {save.isPending ? "保存中…" : "保存"}
          </Button>
        }
      />

      {/* ── 硬上限 · 真的会拦下操作，放在最前面 ── */}
      <Card className="p-7">
        <SectionHead
          title="拉号上限"
          sub="超了就不拉 · 跨所有车累加 · 单独提取 key 也算在里面"
        />

        {/* 单价上限单独一行 —— 它是"每次都判"，跟下面两个"每天累加"的判法不一样 */}
        <div className="mt-4">
          <Field
            label="单价超过多少就不拉"
            hint="积分 / 个 · 留空 = 不限"
          >
            <Input
              type="number"
              min={1}
              value={maxPrice}
              onChange={(e) => setMaxPrice(e.target.value)}
              placeholder="不限"
              className="sm:max-w-[220px]"
            />
          </Field>
          <p className="mt-1.5 text-label text-fg-tertiary">
            上游涨价超过这个数就拉不动 —— 手动拉也拦（确认窗里会说明超了多少）·
            车里也能各自设更严的
          </p>
        </div>

        <div className="mt-5 grid grid-cols-1 gap-5 border-t border-hairline pt-5 sm:grid-cols-2">
          <div className="space-y-2">
            <Field label="每天最多拉几轮" hint="留空 = 不限">
              <Input
                type="number"
                min={1}
                value={roundLimit}
                onChange={(e) => setRoundLimit(e.target.value)}
                placeholder="不限"
              />
            </Field>
            <UsageBar
              used={gs?.used_today.rounds ?? 0}
              limit={numOrNull(roundLimit)}
              unit="轮"
            />
          </div>

          <div className="space-y-2">
            <Field label="每天最多花多少" hint="积分 · 留空 = 不限">
              <Input
                type="number"
                min={1}
                value={spendLimit}
                onChange={(e) => setSpendLimit(e.target.value)}
                placeholder="不限"
              />
            </Field>
            <UsageBar
              used={toCredits(gs?.used_today.spend ?? 0)}
              limit={numOrNull(spendLimit)}
              unit="积分"
            />
          </div>
        </div>

        <Alert tone="neutral" icon={ShieldAlert} className="mt-4">
          车里也能各自设限额 · 两个都不超才让拉（取更严的那个）·
          <Link to="/buses" className="ml-1 font-semibold text-brand-strong hover:underline">
            去车详情看每辆车的
          </Link>
        </Alert>
      </Card>

      {/* ── 新车默认值 · 只影响以后建的车 ── */}
      <Card className="p-7">
        <SectionHead
          title="新车默认值"
          sub="建新车时预填这些 · 改它不动已有的车"
        />

        <div className="mt-4 grid grid-cols-1 gap-5 sm:grid-cols-2">
          <Field label="每次拉几个">
            <Input
              type="number"
              min={1}
              value={count}
              onChange={(e) => setCount(e.target.value)}
              placeholder="3"
            />
          </Field>

          <Field label="默认来源">
            <Select value={vendor} onValueChange={setVendor}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">让系统比价</SelectItem>
                {Object.keys(VENDOR_NAME).map((id) => (
                  <SelectItem key={id} value={id}>
                    {vendorLabel(id, !!me?.invited)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field label="默认区域">
            <Select value={zone} onValueChange={setZone}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">自动</SelectItem>
                <SelectItem value="us">美国区</SelectItem>
                <SelectItem value="eu">欧洲区</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </div>

        <Alert tone="neutral" icon={Info} className="mt-4">
          这些只是<Em plain>新车的初值</Em> —— 已经建好的车按各自「补车策略」跑，改这里不影响它们
        </Alert>
      </Card>
    </div>
  );
}

/** 今日已用 / 上限 · 不限时不画进度条（没有分母，画了是假的） */
function UsageBar({
  used, limit, unit,
}: { used: number; limit: number | null; unit: string }) {
  if (limit == null) {
    return (
      <p className="text-label text-fg-tertiary">
        今日已用 <Em>{unit === "积分" ? fmtCredits(used * MICRO) : used}</Em> {unit} · 不限
      </p>
    );
  }

  const pct = limit > 0 ? used / limit : 0;
  const color = pct >= 1 ? "#EF4444" : pct >= 0.8 ? "#F59E0B" : "#22C55E";

  return (
    <div className="space-y-1">
      <Meter value={used} max={limit} color={color} />
      <p className="text-label text-fg-tertiary">
        今日已用{" "}
        <Em tone={pct >= 1 ? "spend" : pct >= 0.8 ? "warn" : undefined}>
          {unit === "积分" ? fmtCredits(used * MICRO) : used}
        </Em>
        {" / "}{unit === "积分" ? fmtCredits(limit * MICRO) : limit} {unit}
        {pct >= 1 && <span className="ml-1 font-semibold text-danger-fg">已拉满</span>}
      </p>
    </div>
  );
}
