import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
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
  const { t } = useTranslation("buses");
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
      setError(t("join-invite-modal.error-too-short"));
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
        setError(t("join-invite-modal.error-not-found"));
      } else if (msg.includes("409") || msg.toLowerCase().includes("bus_full")) {
        setError(t("join-invite-modal.error-full"));
      } else {
        setError(msg);
      }
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("join-invite-modal.title")}</DialogTitle>
          <DialogDescription>{t("join-invite-modal.desc")}</DialogDescription>
        </DialogHeader>

        <DialogBody>
          <form id="join-invite-form" onSubmit={onSubmit} className="space-y-5">
            <Field label={t("join-invite-modal.field-label")}>
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value.toUpperCase())}
                placeholder={t("join-invite-modal.placeholder")}
                autoFocus
                maxLength={12}
                autoCapitalize="characters"
                autoComplete="off"
                spellCheck={false}
                className="tnum tracking-widest"
              />
            </Field>

            <Alert tone="brand" icon={Info}>
              {t("join-invite-modal.alert-body")}
            </Alert>

            {error && (
              <Alert tone="danger" icon={Info}>{error}</Alert>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>{t("join-invite-modal.cancel")}</Button>
          <Button type="submit" form="join-invite-form" disabled={join.isPending}>
            <Ticket className="size-4" />
            {join.isPending ? t("join-invite-modal.submit-pending") : t("join-invite-modal.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
