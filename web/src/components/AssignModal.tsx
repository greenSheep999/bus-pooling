import { useEffect, useState } from "react";
import { ArrowRight, Bus, Copy, Download, Send } from "lucide-react";
import { useAssign, useBuses } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Chip } from "@/components/ui/primitives";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { cn, vendorName } from "@/lib/utils";
import type { Credential } from "@/types";

type Kind = "into_bus" | "push_pool" | "handoff";

/** 派去向弹层 · 3 种：进车 / 推我的号池 / 拿走 handoff */
export function AssignModal({
  open, onClose, records, passengerpoolConnected,
}: {
  open: boolean;
  onClose: () => void;
  records: Credential[];
  passengerpoolConnected: boolean;
}) {
  const assign = useAssign();
  const { data: buses } = useBuses();
  const [kind, setKind] = useState<Kind>("into_bus");
  const [busId, setBusId] = useState("");
  const [handoffPreview, setHandoffPreview] = useState<Credential[] | null>(null);

  useEffect(() => {
    if (open) {
      setKind("into_bus");
      setBusId("");
      setHandoffPreview(null);
    }
  }, [open]);

  const canSubmit =
    kind === "handoff" ? true
      : kind === "into_bus" ? !!busId
        : passengerpoolConnected;

  const onSubmit = async () => {
    if (!canSubmit || records.length === 0) return;
    if (kind === "handoff") {
      setHandoffPreview(records);
      return;
    }
    await assign.mutateAsync({
      credential_ids: records.map((r) => r.id),
      destination: kind,
      ...(kind === "into_bus" ? { bus_id: busId } : {}),
    });
    onClose();
  };

  const onConfirmHandoff = async () => {
    await assign.mutateAsync({
      credential_ids: records.map((r) => r.id),
      destination: "handoff",
    });
    setHandoffPreview(null);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[560px]">
        <DialogHeader>
          <DialogTitle>派去向</DialogTitle>
          <p className="text-label text-fg-tertiary">
            选中 <span className="font-semibold tnum text-fg-secondary">{records.length}</span> 个 key · 选一种去向
          </p>
        </DialogHeader>

        {handoffPreview ? (
          <>
            <DialogBody>
              <div className="rounded-lg bg-danger-bg p-3 text-label">
                <div className="font-semibold text-danger-fg">⚠️ 这是唯一一次可见，请立即复制保存</div>
                <div className="mt-0.5 text-fg-secondary">
                  号数据交给你后我方立即从 housepool 删除 · 之后再也拿不到明文
                </div>
              </div>
              <div className="mt-3 max-h-60 space-y-2 overflow-y-auto rounded-lg border border-hairline bg-bg-elevated p-3">
                {handoffPreview.map((r) => (
                  <div key={r.id} className="flex items-center gap-3 text-label">
                    <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg px-1.5 py-[1px] text-[10px] font-medium text-fg-secondary">
                      {vendorName(r.vendor_id)}
                    </span>
                    <code className="min-w-0 flex-1 truncate font-mono">{r.key_masked}</code>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => navigator.clipboard.writeText(r.key_masked)}
                      aria-label="复制"
                    >
                      <Copy />
                    </Button>
                  </div>
                ))}
              </div>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setHandoffPreview(null)}>返回</Button>
              <Button variant="danger" onClick={onConfirmHandoff}>
                <ArrowRight />
                我已保存 · 确认拿走
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogBody>
              <div className="space-y-2">
                <KindOption
                  icon={Bus} title="进车" desc="进入一辆已有的车 · 号池由车管理"
                  picked={kind === "into_bus"} onPick={() => setKind("into_bus")}
                />
                {kind === "into_bus" && (
                  <div className="ml-11 rounded-lg bg-bg-elevated p-3">
                    <Field label="选一辆车">
                      <Select value={busId} onValueChange={setBusId}>
                        <SelectTrigger>
                          <SelectValue placeholder="选择⋯" />
                        </SelectTrigger>
                        <SelectContent>
                          {(buses?.items ?? []).map((b) => (
                            <SelectItem key={b.id} value={b.id}>{b.name}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                  </div>
                )}

                <KindOption
                  icon={Send} title="推我的号池" desc="双写到你配的 passengerpool · 我方仍监控存活"
                  picked={kind === "push_pool"} onPick={() => setKind("push_pool")}
                  disabled={!passengerpoolConnected}
                  disabledReason={!passengerpoolConnected ? "未配置" : undefined}
                />

                <KindOption
                  icon={Download} title="拿走号数据" desc="下载明文 key · 号离开系统 · 我方不再监控"
                  picked={kind === "handoff"} onPick={() => setKind("handoff")}
                  warn
                />
              </div>
            </DialogBody>

            <DialogFooter>
              <span className="mr-auto text-label text-fg-tertiary">
                {kind === "handoff" && "⚠️ 拿走后号数据只能看这一次"}
              </span>
              <Button variant="ghost" onClick={onClose}>取消</Button>
              <Button
                onClick={onSubmit}
                disabled={!canSubmit || assign.isPending}
              >
                {assign.isPending ? "处理中…" : "确认"}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function KindOption({
  icon: Icon, title, desc, picked, onPick, disabled, disabledReason, warn,
}: {
  icon: any;
  title: string;
  desc: string;
  picked: boolean;
  onPick: () => void;
  disabled?: boolean;
  disabledReason?: string;
  warn?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onPick}
      disabled={disabled}
      className={cn(
        "flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors",
        picked
          ? "border-brand bg-brand-subtle/40"
          : "border-hairline hover:bg-bg-elevated",
        disabled && "cursor-not-allowed opacity-45",
      )}
    >
      <span className={cn(
        "mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg",
        picked ? "bg-brand-subtle" : "bg-bg-elevated",
      )}>
        <Icon className={cn("size-4", picked ? "text-brand-strong" : "text-fg-secondary")} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 font-semibold">
          {title}
          {warn && <Chip tone="warn" className="text-[10px]">fire-and-forget</Chip>}
          {disabled && disabledReason && (
            <Chip tone="neutral" className="text-[10px]">{disabledReason}</Chip>
          )}
        </div>
        <div className="text-label text-fg-tertiary">{desc}</div>
      </div>
    </button>
  );
}
