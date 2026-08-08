import { useEffect, useState } from "react";
import { AlertCircle, KeyRound } from "lucide-react";
import { usePull, useVendorStats } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { vendorName } from "@/lib/utils";

/** 次入口拉号模态（/extract 页触发）· 拉完进 record group 待派去向 */
export function PullExtractModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const pull = usePull();
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [count, setCount] = useState(1);
  const [vendorId, setVendorId] = useState("auto");

  useEffect(() => {
    if (open) {
      setCount(1);
      setVendorId("auto");
    }
  }, [open]);

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
              <KeyRound className="size-4 text-warn-fg" />
              提取 key
            </span>
          </DialogTitle>
        </DialogHeader>

        <DialogBody>
          <form id="pull-extract-form" onSubmit={onSubmit} className="space-y-4">
            <p className="text-label text-fg-tertiary">
              拉出来的 key 会进"待派"列表 · 之后你派 3 种去向：进车 / 推我的号池 / 拿走
            </p>

            <Field label="数量">
              <Input
                type="number" min={1} max={200} value={count}
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
                      {vendorName(v.vendor_id)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {bargain && (
              <div className="flex items-start gap-2 rounded-lg bg-warn-bg p-3 text-label">
                <AlertCircle className="mt-0.5 size-4 shrink-0 text-warn-fg" />
                <div>
                  <div className="font-semibold text-warn-fg">单次拉号 · 加 20% 议价</div>
                  <div className="text-fg-secondary">拉 2 个及以上不收议价费</div>
                </div>
              </div>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>取消</Button>
          <Button type="submit" form="pull-extract-form" disabled={pull.isPending}>
            {pull.isPending ? "拉号中…" : `拉 ${count} 个 key`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
