import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Info, Sparkles, Users } from "lucide-react";
import { useCreateBus, useMatchAnonBus } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";

/**
 * 搭车（anon）模态：
 *   1. 收 zone + max_unit_price 上限
 *   2. 调 anon/match auto_join=true
 *      - matched=true → 跳车详情（搭上了别人的车）
 *      - matched=false → 建一辆新 anon 车（当前用户是 owner · 让后来人来搭）
 *   两条路都能上车 · 没车友时你先开车·有车友时直接搭
 */
export function JoinAnonModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const nav = useNavigate();
  const match = useMatchAnonBus();
  const createBus = useCreateBus();
  const [zone, setZone] = useState<string>("cn");
  const [maxPriceCredits, setMaxPriceCredits] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setZone("cn");
      setMaxPriceCredits("");
      setError(null);
    }
  }, [open]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    const maxPriceMicro = maxPriceCredits ? Number(maxPriceCredits) * 1_000_000 : undefined;
    try {
      const result = await match.mutateAsync({
        zone,
        max_unit_price: maxPriceMicro,
        auto_join: true,
      });
      if (result.matched && result.bus) {
        onClose();
        nav(`/buses/${result.bus.id}`);
        return;
      }
      // 没匹配 · 建新 anon 车 · 当前用户是 owner · 等后来人搭
      const bus = await createBus.mutateAsync({
        name: "搭车池",
        kind: "anon",
        max_members: 5,
        anon_zone: zone,
        anon_max_unit_price: maxPriceMicro,
      });
      onClose();
      nav(`/buses/${bus.id}`);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "搭车失败·稍后重试");
    }
  };

  const pending = match.isPending || createBus.isPending;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>搭车</DialogTitle>
          <DialogDescription>
            找一辆区域 / 单价上限匹配的活跃车加入 · 找不到就自己建一辆等人来搭
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          <form id="join-anon-form" onSubmit={onSubmit} className="space-y-5">
            <Field label="区域">
              <Select value={zone} onValueChange={setZone}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cn">中国大陆</SelectItem>
                  <SelectItem value="overseas">海外</SelectItem>
                </SelectContent>
              </Select>
            </Field>

            <Field label="单价上限（积分 · 可留空）">
              <Input
                type="number"
                min={0}
                value={maxPriceCredits}
                onChange={(e) => setMaxPriceCredits(e.target.value)}
                placeholder="不限"
              />
            </Field>

            <Alert tone="brand" icon={Sparkles} title="怎么运作">
              系统先找区域和价格上限匹配的活跃车 · 有就直接加入·没有就自己建一辆让别人来搭。
              多人拼车按 N 分摊号价·车友越多每人越便宜。
            </Alert>

            {error && (
              <Alert tone="danger" icon={Info}>{error}</Alert>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>取消</Button>
          <Button type="submit" form="join-anon-form" disabled={pending}>
            <Users className="size-4" />
            {pending ? "撮合中…" : "开始搭车"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
