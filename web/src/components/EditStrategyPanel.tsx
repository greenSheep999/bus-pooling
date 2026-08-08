import { useState } from "react";
import { Save, Zap, ZapOff } from "lucide-react";
import { useUpdateStrategy, useVendorStats } from "@/api/hooks";
import { Card, Chip } from "./ui/primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { toCredits, vendorName } from "@/lib/utils";
import type { BusStrategy } from "@/types";

/** 补车策略编辑 · decisions §8.6 · 策略跟车绑不是全局 */
export function EditStrategyPanel({
  busId, strategy,
}: { busId: string; strategy: BusStrategy }) {
  const upd = useUpdateStrategy(busId);
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [auto, setAuto] = useState(strategy.auto_refill_enabled);
  const [watermark, setWatermark] = useState(strategy.refill_watermark);
  const [perRound, setPerRound] = useState(strategy.per_round_count ?? 3);
  const [maxPrice, setMaxPrice] = useState(
    strategy.max_unit_price ? String(toCredits(strategy.max_unit_price)) : "",
  );
  const [dailyRound, setDailyRound] = useState(
    strategy.daily_round_limit ? String(strategy.daily_round_limit) : "",
  );
  const [dailySpend, setDailySpend] = useState(
    strategy.daily_spend_limit ? String(toCredits(strategy.daily_spend_limit)) : "",
  );
  const [pref, setPref] = useState(strategy.preferred_vendor ?? "auto");
  const [saved, setSaved] = useState(false);

  const onSave = async () => {
    setSaved(false);
    await upd.mutateAsync({
      auto_refill_enabled: auto,
      refill_watermark: watermark,
      refill_min_count: null,
      per_round_count: perRound,
      max_unit_price: maxPrice ? Number(maxPrice) * 1_000_000 : null,
      daily_round_limit: dailyRound ? Number(dailyRound) : null,
      daily_spend_limit: dailySpend ? Number(dailySpend) * 1_000_000 : null,
      preferred_vendor: pref === "auto" ? null : pref,
    });
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <Card className="p-7">
      <div className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-section font-semibold">补车策略</h2>
          <p className="text-label text-fg-tertiary">策略跟这辆车绑 · 不影响其他车</p>
        </div>
        <Button onClick={onSave} disabled={upd.isPending}>
          <Save />
          {upd.isPending ? "保存中…" : saved ? "已保存 ✓" : "保存策略"}
        </Button>
      </div>

      <div className="space-y-6">
        {/* 主开关 · 是否自动补车 */}
        <div
          className="flex cursor-pointer items-center gap-3 rounded-lg border border-hairline bg-bg p-4 transition-colors hover:bg-bg-elevated/40"
          onClick={() => setAuto((v) => !v)}
        >
          <span className="shrink-0">
            {auto ? (
              <Zap className="size-4 text-brand-strong" />
            ) : (
              <ZapOff className="size-4 text-fg-tertiary" />
            )}
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{auto ? "自动补车" : "手动模式"}</div>
            <div className="mt-0.5 text-label text-fg-tertiary">
              {auto
                ? "号池活号跌破水位 · 系统自动拉一轮补车"
                : "号少时只提醒 · 不自动拉 · 你手动决定何时拉号"}
            </div>
          </div>
          <Switch
            checked={auto}
            onCheckedChange={setAuto}
            onClick={(e) => e.stopPropagation()}
          />
        </div>

        {auto && (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="水位阈值（活号 ≤ 此数触发）">
              <Input
                type="number" min={1} value={watermark}
                onChange={(e) => setWatermark(Math.max(1, Number(e.target.value) || 1))}
              />
            </Field>
            <Field label="每轮补几个">
              <Input
                type="number" min={1} value={perRound}
                onChange={(e) => setPerRound(Math.max(1, Number(e.target.value) || 1))}
              />
            </Field>
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label="单价上限（积分）">
            <Input
              type="number" value={maxPrice}
              onChange={(e) => setMaxPrice(e.target.value)}
              placeholder="不限"
            />
          </Field>
          <Field label="日轮次上限">
            <Input
              type="number" value={dailyRound}
              onChange={(e) => setDailyRound(e.target.value)}
              placeholder="不限"
            />
          </Field>
          <Field label="日花费上限（积分）">
            <Input
              type="number" value={dailySpend}
              onChange={(e) => setDailySpend(e.target.value)}
              placeholder="不限"
            />
          </Field>
        </div>

        <Field label="首选 vendor">
          <Select value={pref} onValueChange={setPref}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">让系统比价选</SelectItem>
              {availableVendors.map((v) => (
                <SelectItem key={v.vendor_id} value={v.vendor_id}>
                  {vendorName(v.vendor_id)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>

        <p className="rounded-lg bg-bg-elevated p-3 text-label text-fg-tertiary">
          <Chip tone="brand" className="mr-2">Tip</Chip>
          策略变更立即生效 · 下次自动补车 / 手动拉号都按新策略走
        </p>
      </div>
    </Card>
  );
}
