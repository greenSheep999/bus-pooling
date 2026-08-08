import { useEffect, useState } from "react";
import { ArrowRight, Bus, Copy, Download, Send, X } from "lucide-react";
import { useAssign, useBuses } from "@/api/hooks";
import { Chip } from "./ui/primitives";
import { cn, vendorName } from "@/lib/utils";
import type { Credential } from "@/types";

/** 派去向弹层 · 3 种：进车 / 推我的号池 / 拿走 handoff
    - 进车：选一辆 bus（下拉）
    - 推我的号池：需 passengerpool 已配置（否则灰化 + 引导）
    - 拿走 handoff：确认后展示明文（唯一一次可见） */
type Kind = "into_bus" | "push_pool" | "handoff";

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
    if (!open) return;
    setKind("into_bus");
    setBusId("");
    setHandoffPreview(null);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, onClose]);

  if (!open) return null;

  const canSubmit =
    kind === "handoff"
      ? true
      : kind === "into_bus"
        ? !!busId
        : passengerpoolConnected;

  const onSubmit = async () => {
    if (!canSubmit || records.length === 0) return;
    if (kind === "handoff") {
      // 展示明文（handoff 唯一可见时机）
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
    <>
      <div className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm" onClick={onClose} />
      <div className="fixed inset-x-4 top-1/2 z-50 mx-auto max-w-[560px] -translate-y-1/2 rounded-[16px] border border-hairline bg-bg shadow-modal">
        <div className="flex items-center justify-between border-b border-hairline px-6 py-4">
          <div>
            <h2 className="font-semibold">派去向</h2>
            <p className="text-label text-fg-tertiary">
              选中 <span className="font-semibold tnum text-fg-secondary">{records.length}</span> 个 key · 选一种去向
            </p>
          </div>
          <button
            onClick={onClose}
            className="grid size-8 place-items-center rounded-lg transition-colors hover:bg-bg-elevated"
            aria-label="关闭"
          >
            <X className="size-4 text-fg-secondary" />
          </button>
        </div>

        {handoffPreview ? (
          <HandoffPreview records={handoffPreview} onConfirm={onConfirmHandoff} onBack={() => setHandoffPreview(null)} />
        ) : (
          <>
            <div className="space-y-2 p-6">
              <KindOption
                icon={Bus} title="进车" desc="进入一辆已有的车 · 号池由车管理"
                picked={kind === "into_bus"} onPick={() => setKind("into_bus")}
              />
              {kind === "into_bus" && (
                <div className="ml-11 rounded-lg bg-bg-elevated p-3">
                  <label className="block text-label font-semibold text-fg-secondary">选一辆车</label>
                  <select
                    value={busId} onChange={(e) => setBusId(e.target.value)}
                    className="mt-1.5 w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-medium focus:border-brand focus:outline-none"
                  >
                    <option value="">选择⋯</option>
                    {(buses?.items ?? []).map((b) => (
                      <option key={b.id} value={b.id}>{b.name}</option>
                    ))}
                  </select>
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

            <div className="flex items-center justify-between gap-2 border-t border-hairline px-6 py-4">
              <span className="text-label text-fg-tertiary">
                {kind === "handoff" && "⚠️ 拿走后号数据只能看这一次"}
              </span>
              <div className="flex items-center gap-2">
                <button
                  onClick={onClose}
                  className="rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary hover:bg-bg-elevated"
                >
                  取消
                </button>
                <button
                  onClick={onSubmit}
                  disabled={!canSubmit || assign.isPending}
                  className="rounded-lg bg-brand px-4 py-2 font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-45"
                >
                  {assign.isPending ? "处理中…" : "确认"}
                </button>
              </div>
            </div>
          </>
        )}
      </div>
    </>
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

function HandoffPreview({
  records, onConfirm, onBack,
}: { records: Credential[]; onConfirm: () => void; onBack: () => void }) {
  return (
    <div className="p-6">
      <div className="mb-3 rounded-lg bg-danger-bg p-3 text-label">
        <div className="font-semibold text-danger-fg">⚠️ 这是唯一一次可见，请立即复制保存</div>
        <div className="mt-0.5 text-fg-secondary">
          号数据交给你后我方立即从 housepool 删除 · 之后再也拿不到明文
        </div>
      </div>
      <div className="max-h-60 space-y-2 overflow-y-auto rounded-lg border border-hairline bg-bg-elevated p-3">
        {records.map((r) => (
          <div key={r.id} className="flex items-center gap-3 text-label">
            <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg px-1.5 py-[1px] text-[10px] font-medium text-fg-secondary">
              {vendorName(r.vendor_id)}
            </span>
            <code className="min-w-0 flex-1 truncate font-mono">{r.key_masked}</code>
            <button
              onClick={() => navigator.clipboard.writeText(r.key_masked)}
              className="grid size-7 place-items-center rounded-md border border-hairline transition-colors hover:bg-bg-elevated"
              aria-label="复制"
            >
              <Copy className="size-3.5 text-fg-secondary" />
            </button>
          </div>
        ))}
      </div>
      <div className="mt-4 flex items-center justify-end gap-2">
        <button
          onClick={onBack}
          className="rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary hover:bg-bg-elevated"
        >
          返回
        </button>
        <button
          onClick={onConfirm}
          className="flex items-center gap-1.5 rounded-lg bg-danger-fg px-4 py-2 font-semibold text-white transition-opacity hover:opacity-90"
        >
          <ArrowRight className="size-4" />
          我已保存 · 确认拿走
        </button>
      </div>
    </div>
  );
}
