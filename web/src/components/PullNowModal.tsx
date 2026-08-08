import { useEffect, useState } from "react";
import { KeyRound, Sparkles } from "lucide-react";
import { usePullForBus, useVendorStats } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { toCredits, vendorName } from "@/lib/utils";

/** 车详情立即拉号模态 · 参数用车策略默认，允许覆盖 */
export function PullNowModal({
  open, onClose, busId, defaultCount, preferredVendor, maxUnitPrice,
}: {
  open: boolean;
  onClose: () => void;
  busId: string;
  defaultCount: number;
  preferredVendor: string | null;
  maxUnitPrice: number | null;
}) {
  const pull = usePullForBus(busId);
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [count, setCount] = useState(defaultCount);
  const [vendorId, setVendorId] = useState(preferredVendor ?? "auto");

  useEffect(() => {
    if (open) {
      setCount(defaultCount);
      setVendorId(preferredVendor ?? "auto");
    }
  }, [open, defaultCount, preferredVendor]);

  const bargain = count === 1;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const picked = vendorId === "auto" ? undefined : vendorId;
    await pull.mutateAsync({ count, vendor_id: picked });
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>
            <span className="inline-flex items-center gap-2">
              <KeyRound className="size-4 text-brand-strong" />
              立即拉号
            </span>
          </DialogTitle>
        </DialogHeader>

        <DialogBody>
          <form id="pull-now-form" onSubmit={onSubmit} className="space-y-4">
            <Field label="拉几个号">
              <Input
                type="number" min={1} max={200} value={count}
                onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
              />
            </Field>

            <Field
              label="vendor"
              hint={
                maxUnitPrice ? (
                  <>单价上限 <span className="font-semibold tnum">{toCredits(maxUnitPrice)}</span> 积分</>
                ) : undefined
              }
            >
              <Select value={vendorId} onValueChange={setVendorId}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">让系统选（按有效成本比价）</SelectItem>
                  {availableVendors.map((v) => (
                    <SelectItem key={v.vendor_id} value={v.vendor_id}>
                      {vendorName(v.vendor_id)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {bargain && (
              <Alert tone="neutral" icon={Sparkles} title="拉 2 个及以上单价更低">
                一次只拉 1 个成本偏高
              </Alert>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>取消</Button>
          <Button type="submit" form="pull-now-form" disabled={pull.isPending}>
            {pull.isPending ? "拉号中…" : `拉 ${count} 个号`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
