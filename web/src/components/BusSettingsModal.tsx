import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { AlertTriangle, Save, Trash2 } from "lucide-react";
import { useDissolveBus, useRenameBus } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { Bus } from "@/types";

/** 拼车设置 dialog · 2 段：编辑名字 / 危险区
 *  - 补车策略 = 高频编辑（保活 / 每轮 / 单价上限 会日常调） → 走一级 tab，不进设置
 *  - 危险区点了弹二级 dialog（防止跟"改名"共用一个弹窗一键误点） */
export function BusSettingsModal({
  open, onClose, bus, onDissolved,
}: {
  open: boolean;
  onClose: () => void;
  bus: Bus;
  onDissolved: () => void;
}) {
  const { t } = useTranslation("buses");
  const [dangerOpen, setDangerOpen] = useState(false);

  return (
    <>
      <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
        <DialogContent className="max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{t("settings-modal.title")}</DialogTitle>
          </DialogHeader>
          <DialogBody className="space-y-6">
            <RenameSection bus={bus} />
            <div className="h-px bg-hairline" />
            <DangerSection onOpen={() => setDangerOpen(true)} />
          </DialogBody>
        </DialogContent>
      </Dialog>

      <DangerDialog
        open={dangerOpen}
        onClose={() => setDangerOpen(false)}
        bus={bus}
        onDone={() => { setDangerOpen(false); onClose(); onDissolved(); }}
      />
    </>
  );
}

/* ─────────────── 段 1 · 编辑拼车名 ─────────────── */

function RenameSection({ bus }: { bus: Bus }) {
  const { t } = useTranslation("buses");
  const [name, setName] = useState(bus.name);
  const [saved, setSaved] = useState(false);
  const rename = useRenameBus(bus.id);
  const dirty = name.trim() !== "" && name.trim() !== bus.name;

  useEffect(() => { setName(bus.name); }, [bus.name]);

  const onSave = async () => {
    if (!dirty) return;
    setSaved(false);
    await rename.mutateAsync(name.trim());
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <section className="space-y-3">
      <h3 className="text-body-lg font-semibold">{t("settings-modal.rename.label")}</h3>
      <div className="flex gap-2">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("settings-modal.rename.placeholder")}
          className="flex-1"
        />
        {/* 保存按钮 · 只在 dirty 或刚保存完的 2s toast 期间显示 */}
        {(dirty || saved) && (
          <Button onClick={onSave} disabled={!dirty || rename.isPending}>
            <Save />
            {rename.isPending
              ? t("settings-modal.rename.saving")
              : saved
                ? t("settings-modal.rename.saved")
                : t("settings-modal.rename.save")}
          </Button>
        )}
      </div>
    </section>
  );
}

/* ─────────────── 段 2 · 危险区 · 入口 ─────────────── */

function DangerSection({ onOpen }: { onOpen: () => void }) {
  const { t } = useTranslation("buses");
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <AlertTriangle className="size-4 text-danger-fg" />
        <h3 className="text-body-lg font-semibold text-danger-fg">{t("settings-modal.danger.title")}</h3>
      </div>
      <div className="flex items-center justify-between gap-4 rounded-xl border border-danger-fg/20 bg-danger-bg/30 p-4">
        <div className="min-w-0 flex-1">
          <div className="font-semibold">{t("settings-modal.danger.dissolve-title")}</div>
          <p className="text-label text-fg-secondary">
            {t("settings-modal.danger.dissolve-desc")}
          </p>
        </div>
        <Button variant="danger" size="sm" onClick={onOpen} className="shrink-0">
          <Trash2 />
          {t("settings-modal.danger.dissolve-button")}
        </Button>
      </div>
    </section>
  );
}

/* ─────────────── 二级 dialog · 解散确认 ─────────────── */

function DangerDialog({
  open, onClose, bus, onDone,
}: {
  open: boolean;
  onClose: () => void;
  bus: Bus;
  onDone: () => void;
}) {
  const { t } = useTranslation("buses");
  const dissolve = useDissolveBus();
  const [confirmText, setConfirmText] = useState("");
  const canDissolve = confirmText === bus.name;

  useEffect(() => { if (open) setConfirmText(""); }, [open]);

  const onDissolve = async () => {
    if (!canDissolve) return;
    await dissolve.mutateAsync(bus.id);
    onDone();
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-[480px]">
        <DialogHeader>
          <DialogTitle className="text-danger-fg">
            {t("settings-modal.danger.confirm-title")}
          </DialogTitle>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <Alert tone="danger" icon={AlertTriangle} title={t("settings-modal.danger.confirm-alert-title")}>
            {t("settings-modal.danger.confirm-alert-body")}
          </Alert>
          <div className="space-y-2">
            <label className="block text-label font-semibold text-fg-tertiary">
              <Trans
                t={t}
                i18nKey="settings-modal.danger.confirm-label-prefix"
              />{" "}
              <span className="rounded bg-bg-elevated px-1.5 py-0.5 font-mono font-semibold text-fg">
                {bus.name}
              </span>{" "}
              {t("settings-modal.danger.confirm-label-suffix")}
            </label>
            <Input
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder={bus.name}
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t("settings-modal.danger.cancel")}
          </Button>
          <Button
            variant="danger"
            onClick={onDissolve}
            disabled={!canDissolve || dissolve.isPending}
          >
            <Trash2 />
            {dissolve.isPending
              ? t("settings-modal.danger.confirming")
              : t("settings-modal.danger.confirm-button")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
