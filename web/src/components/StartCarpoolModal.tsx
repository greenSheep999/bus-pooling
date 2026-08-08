import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Info, Sparkles, Zap } from "lucide-react";
import {
  useCreateBus, useMe, useVendorStats,
} from "@/api/hooks";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import { CollapsiblePanel } from "@/components/ui/collapsible";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  vendorLabel,
} from "@/lib/utils";

/** 发起拼车模态：建车 + 首次拉号一步完成
    - 基本：车名 · 数量 · vendor（可让系统选 · 同时作为策略里的 preferred_vendor）· count=1 单价提示
    - 主开关：自动补车（放高级选项外 · 用户先看到关键决策）
    - 高级折叠：3 个上限（单价 · 日轮次 · 日花费） */
export function StartCarpoolModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const { data: me } = useMe();
  const nav = useNavigate();
  const createBus = useCreateBus();
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [name, setName] = useState("我的车");
  const [count, setCount] = useState(3);
  const [vendorId, setVendorId] = useState<string>("auto");
  const [autoRefill, setAutoRefill] = useState(false);
  const [maxPrice, setMaxPrice] = useState("");
  const [dailyRoundLimit, setDailyRoundLimit] = useState("");
  const [dailySpendLimit, setDailySpendLimit] = useState("");
  const [refillWatermark, setRefillWatermark] = useState(3);
  const [perRoundCount, setPerRoundCount] = useState(3);

  // 打开时重置
  useEffect(() => {
    if (open) {
      setName("我的车");
      setCount(3);
      setVendorId("auto");
      setAutoRefill(false);
      setMaxPrice("");
      setDailyRoundLimit("");
      setDailySpendLimit("");
    }
  }, [open]);

  const bargain = count === 1;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const picked = vendorId === "auto" ? null : vendorId;
    const strategy = {
      auto_refill_enabled: autoRefill,
      refill_watermark: refillWatermark,
      refill_min_count: null,
      per_round_count: perRoundCount,
      max_unit_price: maxPrice ? Number(maxPrice) * 1_000_000 : null,
      daily_round_limit: dailyRoundLimit ? Number(dailyRoundLimit) : null,
      daily_spend_limit: dailySpendLimit ? Number(dailySpendLimit) * 1_000_000 : null,
      preferred_vendor: picked,
    };
    const bus = await createBus.mutateAsync({ name, strategy });
    onClose();
    nav(`/buses/${bus.id}`);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>发起拼车</DialogTitle>
          <DialogDescription>建一辆自己的车 · 首次发车一并完成</DialogDescription>
        </DialogHeader>

        <DialogBody>
          <form id="start-carpool-form" onSubmit={onSubmit} className="space-y-5">
            <Field label="车名">
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="给车起个名字"
                required
                maxLength={30}
              />
            </Field>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-[140px_minmax(0,1fr)]">
              <Field label="拉几个号">
                <Input
                  type="number"
                  min={1}
                  max={200}
                  value={count}
                  onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
                />
              </Field>
              <Field label="vendor">
                <Select value={vendorId} onValueChange={setVendorId}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">让系统选（按有效成本比价）</SelectItem>
                    {availableVendors.map((v) => (
                      <SelectItem key={v.vendor_id} value={v.vendor_id}>
                        {vendorLabel(v.vendor_id, !!me?.invited)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>

            {/* 单价提示 · 向下省视角（不用"议价费"内部术语） */}
            {bargain ? (
              <Alert tone="neutral" icon={Sparkles} title="拉 2 个及以上单价更低">
                一次只拉 1 个成本偏高 · 建议至少拉 2 个
              </Alert>
            ) : (
              <Alert tone="ok" icon={Sparkles} title="单价更划算">
                一次拉 <span className="font-semibold tnum">{count}</span> 个号 · 均摊后单价更低
              </Alert>
            )}

            {/* 主开关 · 是否开启自动补车 */}
            <div
              className="flex cursor-pointer items-center gap-3 rounded-2xl border border-hairline bg-bg p-4 transition-colors hover:bg-bg-elevated/40"
              onClick={() => setAutoRefill((v) => !v)}
            >
              <span className="shrink-0">
                {autoRefill ? (
                  <Zap className="size-4 text-brand-strong" />
                ) : (
                  <Zap className="size-4 text-fg-tertiary" />
                )}
              </span>
              <div className="min-w-0 flex-1">
                <div className="font-semibold">开启自动补车</div>
                <div className="mt-0.5 text-label text-fg-tertiary">
                  号池活号少于保活数时，系统自动拉一轮补车。关闭则手动决定何时拉号。
                </div>
              </div>
              <Switch
                checked={autoRefill}
                onCheckedChange={setAutoRefill}
                onClick={(e) => e.stopPropagation()}
              />
            </div>

            {autoRefill && (
              <div className="grid grid-cols-2 gap-4">
                <Field label="保活数（正常号少于此数就补）">
                  <Input
                    type="number"
                    min={1}
                    value={refillWatermark}
                    onChange={(e) => setRefillWatermark(Math.max(1, Number(e.target.value) || 1))}
                  />
                </Field>
                <Field label="每轮补几个">
                  <Input
                    type="number"
                    min={1}
                    value={perRoundCount}
                    onChange={(e) => setPerRoundCount(Math.max(1, Number(e.target.value) || 1))}
                  />
                </Field>
              </div>
            )}

            <CollapsiblePanel title="高级选项" subtitle="单价上限 · 日轮次 · 日花费">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <Field label="单价上限（积分）">
                  <Input
                    type="number"
                    value={maxPrice}
                    onChange={(e) => setMaxPrice(e.target.value)}
                    placeholder="不限"
                  />
                </Field>
                <Field label="日轮次上限">
                  <Input
                    type="number"
                    value={dailyRoundLimit}
                    onChange={(e) => setDailyRoundLimit(e.target.value)}
                    placeholder="不限"
                  />
                </Field>
                <Field label="日花费上限（积分）">
                  <Input
                    type="number"
                    value={dailySpendLimit}
                    onChange={(e) => setDailySpendLimit(e.target.value)}
                    placeholder="不限"
                  />
                </Field>
              </div>
            </CollapsiblePanel>

            <Alert tone="brand" icon={Info}>
              建车后自动发首轮车 · <span className="font-semibold text-fg">{count}</span> 个号进池 ·
              可在车详情页调策略、查看号列表、手动再拉
            </Alert>
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>取消</Button>
          <Button
            type="submit"
            form="start-carpool-form"
            disabled={createBus.isPending}
          >
            {createBus.isPending ? "发车中…" : "发车"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
