import { useEffect, useState } from "react";
import {
  AlertTriangle, ArrowRight, Bus, Coins, Copy, Download, Send, UserMinus,
} from "lucide-react";
import {
  useAssign, useBuses, useMe,
} from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Chip, Em, Muted } from "@/components/ui/primitives";
import { VendorTag } from "@/components/ui/tags";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import {
  cn, fmtCredits, vendorLabel,
} from "@/lib/utils";
import type { Bus as BusType, Credential, Money } from "@/types";

type Kind = "into_bus" | "push_pool" | "handoff";

/** 车友分摊一笔 · 谁 / 该付多少 / 为什么没付 */
type Share = {
  username: string;
  /** 该成员按 share_pct 应付给我的部分 */
  amount: Money;
  /** 没参与的原因 · null = 正常付了 */
  skipReason: "short" | "suspended" | null;
  /** skipReason === "short" 时差多少 */
  short: Money;
};

type Settlement =
  /** 单人车 · 没有分摊对象 */
  | { kind: "solo" }
  /** 多人车 · 有分摊 */
  | {
      kind: "split";
      /** 付得起的人合计给我的（跳过的人不算） */
      income: Money;
      /** 付得起 · 扣钱 + 保留取号权 */
      payers: Share[];
      /** 付不起 · 本次跳过（不扣钱 · 撤 client_key 不给取号） */
      skipped: Share[];
      /** 因为跳过而少收的 */
      lost: Money;
    };

/** 自费拉的号派进多人车 · 按 share_pct 即时清算（decisions §8.23）
 *  我垫了全款 → 其他成员按各自比例买入份额 → 我净支出 = 我自己那份
 *  号价在提取时已付给 vendor，这里只是内部记账转移，不再收 vendor 费用
 *
 *  余额不足的成员**只是这一次不参与**（不扣钱 · 不给取号），车照进：
 *  号已经是我的了，少一个人分摊只是我少收回一点，不是派不进去
 *  挂起的成员（§8.26）同理不参与 —— 他连 client_key 都被撤了 */
function calcSettlement(bus: BusType | undefined, cost: Money, myId: string): Settlement | null {
  if (!bus) return null;
  const members = bus.members ?? [];
  const others = members.filter((m) => m.passenger_id !== myId);
  if (others.length === 0) return { kind: "solo" };

  const shares: Share[] = others.map((m) => {
    const amount = Math.round((cost * m.share_pct) / 100);
    const short = Math.max(0, amount - m.balance);
    return {
      username: m.username,
      amount,
      skipReason:
        m.status === "suspended" ? "suspended"
          : short > 0 ? "short"
            : null,
      short,
    };
  });

  const payers = shares.filter((x) => x.skipReason === null);
  const skipped = shares.filter((x) => x.skipReason !== null);

  return {
    kind: "split",
    // 只算付得起的人 · 逐项求和（避免各自四舍五入后跟总额差 1）
    income: payers.reduce((s, x) => s + x.amount, 0),
    payers,
    skipped,
    lost: skipped.reduce((s, x) => s + x.amount, 0),
  };
}

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

  /* 这批号我已经垫的钱 · 派进多人车时按份额跟车友清算 */
  const cost = records.reduce((s, r) => s + r.paid, 0);
  const settlement = calcSettlement(
    (buses?.items ?? []).find((b) => b.id === busId),
    cost,
    me?.id ?? "",
  );
  /* 只有"没一个人付得起"才拦 —— 那时候派进去等于纯赠送，先让车友充值
     只是某几个人不够 → 照派 · 跳过他们（§8.23） */
  const settlementBlocked = settlement?.kind === "split" && settlement.payers.length === 0;

  const canSubmit =
    kind === "handoff" ? true
      : kind === "into_bus" ? !!busId && !settlementBlocked
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
                共 <Em>{records.length}</Em> 个 key ·
                核对后确认
              </>
            ) : (
              <>
                选中 <Em>{records.length}</Em> 个 key · 选一种去向
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

                  {/* 进车必须选车 · 选完才算得出清算 */}
                  {presetKind === "into_bus" && (
                    <div className="space-y-2">
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
                      {settlement && <SettlementNote settlement={settlement} />}
                    </div>
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
                    <div className="ml-11 space-y-2 rounded-xl bg-bg-elevated p-3">
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
                      {settlement && <SettlementNote settlement={settlement} />}
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

/** 清算提示 · 选完车之后才出（选车前不知道跟谁分摊，算不出来）
 *  §8.23：只给结果，不列明细 —— 用户要知道的就是"我能收回多少"
 *  逐成员份额在车详情页成员 tab 看，这里不重复
 *  例外：余额不足时必须展开说是谁（这时候用户得知道找谁） */
function SettlementNote({ settlement }: { settlement: Settlement }) {
  if (settlement.kind === "solo") {
    return <Muted>独享车 · 无分摊</Muted>;
  }

  const skipLabel = settlement.skipped
    .map((s) => `@${s.username}${s.skipReason === "suspended" ? "（已挂起）" : "（余额不足）"}`)
    .join(" · ");

  /* 一个人都参与不了 · 派进去等于纯赠送 · 拦下来 */
  if (settlement.payers.length === 0) {
    return (
      <Alert tone="danger" icon={AlertTriangle} title="没有车友能参与这次分摊">
        {skipLabel} · 现在派进去等于你白送 · 等他们充值或先解挂
      </Alert>
    );
  }

  return (
    <div className="space-y-2">
      <Alert tone="ok" icon={Coins}>
        车友分摊后你将收到{" "}
        <span className="font-semibold tnum text-ok-fg">{fmtCredits(settlement.income)}</span> 积分
      </Alert>
      {/* 跳过的只是这次不参与 · 车照进 · 所以是 warn 不是 danger */}
      {settlement.skipped.length > 0 && (
        <Alert tone="warn" icon={UserMinus} title={`${skipLabel} · 本次跳过`}>
          不扣他积分，也不给取这批号 · 你少收{" "}
          <span className="font-semibold tnum">{fmtCredits(settlement.lost)}</span> 积分
        </Alert>
      )}
    </div>
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
