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
import { notify } from "@/lib/toast";

/** 表单校验用 inline · API 错误用 sonner —— docs/13 §7.4 反馈铁律：
 *  校验 = 用户还在输入 · 反馈要贴输入位；
 *  API = 提交完成后的结果 · 走全局 toast · 通道统一（不在这里另开红条） */

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
    const trimmed = code.trim();
    // 长度校验是"用户还在输入"的问题 · 走 inline —— toast 一闪就没了看不清哪儿错
    if (trimmed.length < 4) {
      setError(t("join-invite-modal.error-too-short"));
      return;
    }
    setError(null);
    try {
      const bus = await join.mutateAsync(trimmed);
      notify.ok({ title: t("common:toast.joined") });
      onClose();
      nav(`/buses/${bus.id}`);
    } catch (err: unknown) {
      // API 错误走全局 toast · 通道统一（不在模态里另挂红条）
      const msg = err instanceof Error ? err.message : String(err);
      const lower = msg.toLowerCase();
      if (msg.includes("404") || lower.includes("not_found")) {
        notify.fail(err, t("join-invite-modal.error-not-found"));
      } else if (msg.includes("409") || lower.includes("bus_full")) {
        notify.fail(err, t("join-invite-modal.error-full"));
      } else {
        notify.fail(err, t("join-invite-modal.error-generic"));
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

            {/* 只留输入位校验 · API 错误已走 notify.fail */}
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
