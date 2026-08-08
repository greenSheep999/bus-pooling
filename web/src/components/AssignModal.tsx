import { useEffect, useState } from "react";
import { AlertTriangle, ArrowRight, Bus, Copy, Download, Send } from "lucide-react";
import {
  useAssign, useBuses, useMe,
} from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Chip } from "@/components/ui/primitives";
import { VendorTag } from "@/components/ui/tags";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import {
  cn, vendorLabel,
} from "@/lib/utils";
import type { Credential } from "@/types";

type Kind = "into_bus" | "push_pool" | "handoff";

/** 派去向弹层 · 3 种：进车 / 推我的号池 / 拿走 handoff
 *  presetKind：从底部悬浮栏直接带去向进来 · 跳过"先开弹窗再选去向"这一步
 *  此时隐藏去向选择器 · 弹窗只做"确认 + 补必填项（进车选哪辆）" */
export function AssignModal({
  open, onClose, records, passengerpoolConnected, presetKind,
}: {
  open: boolean;
  onClose: () => void;
  records: Credential[];
  passengerpoolConnected: boolean;
  presetKind?: Kind;
}) {
  const { data: me } = useMe();
  const assign = useAssign();
  const { data: buses } = useBuses();
  const [kind, setKind] = useState<Kind>(presetKind ?? "into_bus");
  const [busId, setBusId] = useState("");
  const [handoffPreview, setHandoffPreview] = useState<Credential[] | null>(null);

  useEffect(() => {
    if (open) {
      setKind(presetKind ?? "into_bus");
      setBusId("");
      setHandoffPreview(null);
    }
  }, [open, presetKind]);

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
          {/* preset 模式（从悬浮栏带去向进来）· 标题直接说要做什么，不再说"选一种去向" */}
          <DialogTitle>
            {presetKind === "into_bus" && "确认加入拼车"}
            {presetKind === "push_pool" && "确认推我的号池"}
            {presetKind === "handoff" && "确认下载拿走"}
            {!presetKind && "派去向"}
          </DialogTitle>
          <p className="text-label text-fg-tertiary">
            {presetKind ? (
              <>
                共 <span className="font-semibold tnum text-fg-secondary">{records.length}</span> 个 key ·
                核对后确认
              </>
            ) : (
              <>
                选中 <span className="font-semibold tnum text-fg-secondary">{records.length}</span> 个 key · 选一种去向
              </>
            )}
          </p>
        </DialogHeader>

        {handoffPreview ? (
          <>
            <DialogBody>
              <Alert tone="danger" icon={AlertTriangle} title="这是唯一一次可见，请立即复制保存">
                拿走后我方立即删除 · 之后再也拿不到明文
              </Alert>
              <div className="mt-3 max-h-60 space-y-2 overflow-y-auto rounded-xl border border-hairline bg-bg-elevated p-3">
                {handoffPreview.map((r) => (
                  <div key={r.id} className="flex items-center gap-3 text-label">
                    <VendorTag name={vendorLabel(r.vendor_id, !!me?.invited)} />
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
              {presetKind ? (
                /* 从悬浮栏带了去向进来 · 不再让用户重选 · 只做确认 + 补必填项 */
                <div className="space-y-4">
                  {/* 这个去向意味着什么 · 一句话（标题已说做什么，这里说后果） */}
                  <Alert
                    tone={presetKind === "handoff" ? "warn" : "neutral"}
                    icon={
                      presetKind === "into_bus" ? Bus
                        : presetKind === "push_pool" ? Send
                          : Download
                    }
                  >
                    {presetKind === "into_bus" && "号进车后由车管理 · 车内成员共享 · 我方持续监控存活"}
                    {presetKind === "push_pool" && "双写到你配的号池 · 我方仍监控存活"}
                    {presetKind === "handoff" && "下载明文 key 后号离开系统 · 我方不再监控，也无法重新下载"}
                  </Alert>

                  {/* 进车必须选车 */}
                  {presetKind === "into_bus" && (
                    <Field label="派到哪辆车">
                      <Select value={busId} onValueChange={setBusId}>
                        <SelectTrigger><SelectValue placeholder="选择⋯" /></SelectTrigger>
                        <SelectContent>
                          {(buses?.items ?? []).map((b) => (
                            <SelectItem key={b.id} value={b.id}>{b.name}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                  )}

                  {/* 选中的 key 清单 */}
                  <div className="rounded-xl border border-hairline bg-bg-elevated/40 p-3">
                    <div className="mb-2 text-label font-semibold">这些 key</div>
                    <div className="max-h-40 space-y-1 overflow-y-auto">
                      {records.map((r) => (
                        <div key={r.id} className="flex items-center gap-2 text-label">
                          <span className="min-w-0 flex-1 truncate font-mono text-fg-secondary">
                            {r.key_masked}
                          </span>
                          <VendorTag name={vendorLabel(r.vendor_id, !!me?.invited)} />
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              ) : (
                /* 没带去向（兜底路径）· 让用户选 */
                <div className="space-y-2">
                  <KindOption
                    icon={Bus} title="进车" desc="进入一辆已有的车 · 号池由车管理"
                    picked={kind === "into_bus"} onPick={() => setKind("into_bus")}
                  />
                  {kind === "into_bus" && (
                    <div className="ml-11 rounded-xl bg-bg-elevated p-3">
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
              )}
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
        "flex w-full items-start gap-3 rounded-xl border p-3 text-left transition-colors",
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
