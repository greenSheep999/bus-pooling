import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Info, Ticket } from "lucide-react";
import { useJoinByInviteCode } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";

/**
 * 输入拼车码加入一辆车。
 * - 无效码 → 404 → 前端提示"邀请码无效或车已解散"
 * - 车满 → 409 → 提示
 * - 已成员 → 200 幂等 → 直接跳车详情
 */
export function JoinByInviteModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const nav = useNavigate();
  const join = useJoinByInviteCode();
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setCode("");
      setError(null);
    }
  }, [open]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    const trimmed = code.trim();
    if (trimmed.length < 4) {
      setError("拼车码至少 4 位");
      return;
    }
    try {
      const bus = await join.mutateAsync(trimmed);
      onClose();
      nav(`/buses/${bus.id}`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      // 后端 404 = 拼车码无效 · 409 = 车满 · 别的按原文
      if (msg.includes("404") || msg.toLowerCase().includes("not_found")) {
        setError("拼车码无效或车已解散");
      } else if (msg.includes("409") || msg.toLowerCase().includes("bus_full")) {
        setError("车已满 · 找车主确认或另找一辆");
      } else {
        setError(msg);
      }
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>输拼车码加入</DialogTitle>
          <DialogDescription>用朋友给的拼车码加入他的车 · 共享号池按人头分摊</DialogDescription>
        </DialogHeader>

        <DialogBody>
          <form id="join-invite-form" onSubmit={onSubmit} className="space-y-5">
            <Field label="拼车码">
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value.toUpperCase())}
                placeholder="AAAA0000"
                autoFocus
                maxLength={12}
                autoCapitalize="characters"
                autoComplete="off"
                spellCheck={false}
                className="tnum tracking-widest"
              />
            </Field>

            <Alert tone="brand" icon={Info}>
              拼车码大小写不区分 · 一个码可以重复用（车主可随时换码）
            </Alert>

            {error && (
              <Alert tone="danger" icon={Info}>{error}</Alert>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>取消</Button>
          <Button type="submit" form="join-invite-form" disabled={join.isPending}>
            <Ticket className="size-4" />
            {join.isPending ? "加入中…" : "加入"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
