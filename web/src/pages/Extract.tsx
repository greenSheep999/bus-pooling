import { useMemo, useState } from "react";
import { AlertTriangle, KeyRound } from "lucide-react";
import { useDownstream, usePullRecords } from "@/api/hooks";
import { AssignModal } from "@/components/AssignModal";
import { PullExtractModal } from "@/components/PullExtractModal";
import {
  BareHead, BareList, BareRow, Card, Chip, SectionHead,
} from "@/components/ui/primitives";
import { cn, fmtCredits, fmtLifespan, fmtTime, vendorName } from "@/lib/utils";
import type { Credential } from "@/types";
export default function Extract() {
  const { data: records } = usePullRecords();
  const { data: downstream } = useDownstream();
  const items = records?.items ?? [];

  const [pullOpen, setPullOpen] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const selectedRecords = useMemo(
    () => items.filter((c) => selected.has(c.id)),
    [items, selected],
  );

  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const toggleAll = () => {
    if (selected.size === items.length) setSelected(new Set());
    else setSelected(new Set(items.map((c) => c.id)));
  };

  const passengerpoolOk = !!downstream?.connected;

  return (
    <div className="space-y-section">
      <PullExtractModal open={pullOpen} onClose={() => setPullOpen(false)} />
      <AssignModal
        open={assignOpen}
        onClose={() => { setAssignOpen(false); setSelected(new Set()); }}
        records={selectedRecords}
        passengerpoolConnected={passengerpoolOk}
      />

      {/* Hero */}
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">提取 key</h1>
          <p className="text-fg-tertiary">
            单独拉号 · 拉出来的 key 进"待派"列表 · 之后你派 3 种去向：
            <span className="mx-1 rounded-md border border-hairline bg-bg-elevated px-1.5 py-[1px] text-label font-medium text-fg-secondary">进车</span>
            <span className="mx-1 rounded-md border border-hairline bg-bg-elevated px-1.5 py-[1px] text-label font-medium text-fg-secondary">推我的号池</span>
            <span className="mx-1 rounded-md border border-hairline bg-bg-elevated px-1.5 py-[1px] text-label font-medium text-fg-secondary">拿走</span>
          </p>
        </div>
        <button
          onClick={() => setPullOpen(true)}
          className="flex shrink-0 items-center gap-2 rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90"
        >
          <KeyRound className="size-4" />
          提取 key
        </button>
      </div>

      {/* 待派列表 */}
      <div className="space-y-5">
        <SectionHead
          title="待派 key"
          sub={`共 ${items.length} 个 · 选中后派去向`}
          right={
            selected.size > 0 && (
              <button
                onClick={() => setAssignOpen(true)}
                className="rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90"
              >
                派去向（<span className="tnum">{selected.size}</span>）
              </button>
            )
          }
        />

        {!passengerpoolOk && (
          <div className="flex items-start gap-2 rounded-lg border border-warn-fg/20 bg-warn-bg/40 p-3 text-label">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warn-fg" />
            <div className="min-w-0 flex-1">
              <div className="font-semibold text-warn-fg">未配置 passengerpool</div>
              <div className="text-fg-secondary">
                "推我的号池" 需要先在{" "}
                <a href="/settings/downstream" className="font-semibold text-brand-strong hover:underline">
                  设置 · 我的号池
                </a>
                {" "}里配 URL 和 token
              </div>
            </div>
          </div>
        )}

        {items.length === 0 ? (
          <Card className="p-12 text-center">
            <p className="text-fg-tertiary">还没有待派 key · 点右上"提取 key"拉一批</p>
          </Card>
        ) : (
          <Card className="p-4">
            <div className="overflow-x-auto">
              <div className="min-w-[640px]">
                <BareHead>
                  <span className="w-8 shrink-0 pl-2">
                    <input
                      type="checkbox"
                      checked={selected.size === items.length}
                      onChange={toggleAll}
                      className="size-4 accent-brand"
                    />
                  </span>
                  <span className="min-w-0 flex-1">key · vendor</span>
                  <span className="w-20 shrink-0 text-center">寿命</span>
                  <span className="w-24 shrink-0 text-center">已消耗</span>
                  <span className="w-24 shrink-0 text-right">拉入时间</span>
                </BareHead>
                <BareList>
                  {items.map((c) => (
                    <RecordRow
                      key={c.id} c={c}
                      picked={selected.has(c.id)}
                      onToggle={() => toggle(c.id)}
                    />
                  ))}
                </BareList>
              </div>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}

function RecordRow({
  c, picked, onToggle,
}: { c: Credential; picked: boolean; onToggle: () => void }) {
  return (
    <BareRow onClick={onToggle} className={cn(picked && "bg-brand-subtle/40")}>
      <span className="w-8 shrink-0 pl-2">
        <input
          type="checkbox"
          checked={picked}
          onChange={onToggle}
          onClick={(e) => e.stopPropagation()}
          className="size-4 accent-brand"
        />
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate font-mono text-label font-medium text-fg-secondary">
          {c.key_masked}
        </span>
        <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg-elevated px-1.5 py-[1px] text-[10px] font-medium text-fg-secondary shadow-card">
          {vendorName(c.vendor_id)}
        </span>
        <Chip tone="warn" className="shrink-0">待派</Chip>
      </span>
      <span className="w-20 shrink-0 text-center text-label font-medium tnum text-fg-secondary">
        {fmtLifespan(c.lifespan_seconds)}
      </span>
      <span className="w-24 shrink-0 text-center text-label font-semibold tnum">
        {fmtCredits(c.credits_used)}
        <span className="ml-0.5 font-medium text-fg-tertiary">积分</span>
      </span>
      <span className="w-24 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
        {fmtTime(c.pulled_at)}
      </span>
    </BareRow>
  );
}

