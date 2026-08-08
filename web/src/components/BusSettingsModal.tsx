import { useEffect, useState } from "react";
import { AlertTriangle, Save, Trash2 } from "lucide-react";
import { useDissolveBus, useRenameBus } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { Bus } from "@/types";

/** 车设置 dialog · 2 段：编辑名字 / 危险区
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
  const [dangerOpen, setDangerOpen] = useState(false);

  return (
    <>
      <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
        <DialogContent className="max-w-[520px]">
          <DialogHeader>
            <DialogTitle>车设置</DialogTitle>
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

/* ─────────────── 段 1 · 编辑车名 ─────────────── */

function RenameSection({ bus }: { bus: Bus }) {
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
      <h3 className="text-body-lg font-semibold">车名</h3>
      <div className="flex gap-2">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="给车起个名字"
          className="flex-1"
        />
        {/* 保存按钮 · 只在 dirty 或刚保存完的 2s toast 期间显示 */}
        {(dirty || saved) && (
          <Button onClick={onSave} disabled={!dirty || rename.isPending}>
            <Save />
            {rename.isPending ? "保存中…" : saved ? "已保存 ✓" : "保存"}
          </Button>
        )}
      </div>
    </section>
  );
}

/* ─────────────── 段 2 · 危险区 · 入口 ─────────────── */

function DangerSection({ onOpen }: { onOpen: () => void }) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <AlertTriangle className="size-4 text-danger-fg" />
        <h3 className="text-body-lg font-semibold text-danger-fg">危险区</h3>
      </div>
      <div className="flex items-center justify-between gap-4 rounded-xl border border-danger-fg/20 bg-danger-bg/30 p-4">
        <div className="min-w-0 flex-1">
          <div className="font-semibold">解散这辆车</div>
          <p className="text-label text-fg-secondary">
            活号挪到你的提取记录 · 死号归档 · 已扣积分不退
          </p>
        </div>
        <Button variant="danger" size="sm" onClick={onOpen} className="shrink-0">
          <Trash2 />
          解散车
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
          <DialogTitle className="text-danger-fg">解散车</DialogTitle>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <Alert tone="danger" icon={AlertTriangle} title="此操作不可撤销">
            解散后活号挪到提取记录 · 死号归档 · 已扣积分不退
          </Alert>
          <div className="space-y-2">
            <label className="block text-label font-semibold text-fg-tertiary">
              输入车名 <span className="rounded bg-bg-elevated px-1.5 py-0.5 font-mono font-semibold text-fg">{bus.name}</span> 确认
            </label>
            <Input
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder={bus.name}
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>取消</Button>
          <Button
            variant="danger"
            onClick={onDissolve}
            disabled={!canDissolve || dissolve.isPending}
          >
            <Trash2 />
            {dissolve.isPending ? "解散中…" : "确认解散"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
