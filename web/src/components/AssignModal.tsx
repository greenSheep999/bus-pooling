import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle, ArrowRight, Bus, Coins, Copy, Download, Send, UserMinus,
} from "lucide-react";
import {
  useAssign, useBuses, useHandoffConfirm, useHandoffFulfill, useHandoffInit, useMe,
} from "@/api/hooks";
import type { HandoffKeys } from "@/api/hooks";
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
import { notify } from "@/lib/toast";
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
  const { t } = useTranslation("extract");
  const { data: me } = useMe();
  const assign = useAssign();
  const { data: buses } = useBuses();
  const [kind, setKind] = useState<Kind>(presetKind ?? "into_bus");
  const [busId, setBusId] = useState("");
  /* 拿走三段式的中间态 · token 留着给第 ③ 步 confirm 用
     keys 是**后端实时从号池读出来的明文**，不落我方库（09-transactions §4） */
  const [handoff, setHandoff] = useState<{
    token: string;
    keys: HandoffKeys["keys"];
  } | null>(null);
  // P1-l/m · assign 后端拒推(credential_dead / credential_quota_exceeded)· 弹窗保开显 error
  const [assignErrors, setAssignErrors] = useState<{ credential_id: string; code: string; message: string }[]>([]);
  const handoffInit = useHandoffInit();
  const handoffFulfill = useHandoffFulfill();
  const handoffConfirm = useHandoffConfirm();

  useEffect(() => {
    if (open) {
      setKind(presetKind ?? "into_bus");
      setBusId("");
      setHandoff(null);
      setAssignErrors([]);
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

    /* 拿走走三段式（09-transactions §4）：这一步做 ① 发 token + ② 取明文，
       号此刻**还在池里** —— 用户点「我已保存」才 ③ confirm 触发删除。
       ② 失败（比如断线）可以重试，因为号没删。 */
    if (kind === "handoff") {
      try {
        const tok = await handoffInit.mutateAsync(records.map((r) => r.id));
        const got = await handoffFulfill.mutateAsync(tok.download_token);
        setHandoff({ token: tok.download_token, keys: got.keys });
        notify.info({
          title: t("common:toast.handoff_ready_title", { count: got.keys.length }),
          desc: t("common:toast.handoff_ready_desc"),
        });
      } catch (err) {
        notify.fail(err, t("common:toast.generic_fail"));
      }
      return;
    }

    try {
      const result = await assign.mutateAsync({
        credential_ids: records.map((r) => r.id),
        destination: kind,
        ...(kind === "into_bus" ? { bus_id: busId } : {}),
      });
      // **P1-l/m 拒推场景**: 后端 errors[] 里带 credential_dead / credential_quota_exceeded
      // 全被拒(assigned=0) · 弹窗保开 · 显 error 列表让用户知道
      // 部分成功(assigned>0 · 部分 errors) · 也保弹窗显 · 但成功那部分 UI 已刷
      if (result.errors && result.errors.length > 0) {
        setAssignErrors(result.errors);
        notify.warn({ title: t("common:toast.assign_partial") });
        return;
      }
      notify.ok({
        title: kind === "into_bus"
          ? t("common:toast.assign_bus_title", { count: result.assigned })
          : t("common:toast.assign_push_title", { count: result.assigned }),
      });
      onClose();
    } catch (err) {
      notify.fail(err, t("common:toast.generic_fail"));
    }
  };

  /** ③ 用户说"我已保存" → 这时才真删号 */
  const onConfirmHandoff = async () => {
    if (!handoff) return;
    try {
      await handoffConfirm.mutateAsync(handoff.token);
      notify.ok({ title: t("common:toast.handoff_done") });
      setHandoff(null);
      onClose();
    } catch (err) {
      notify.fail(err, t("common:toast.generic_fail"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[560px]">
        <DialogHeader>
          {/* preset 模式（从悬浮栏带去向进来）· 标题直接说要做什么，不再说"选一种去向" */}
          <DialogTitle>
            {presetKind === "into_bus" && t("assign-modal.title-into-bus")}
            {presetKind === "push_pool" && t("assign-modal.title-push-pool")}
            {presetKind === "handoff" && t("assign-modal.title-handoff")}
            {!presetKind && t("assign-modal.title-default")}
          </DialogTitle>
          <p className="text-label text-fg-tertiary">
            {presetKind ? (
              <>
                {t("assign-modal.sub-preset-prefix")}<Em>{records.length}</Em>{t("assign-modal.sub-preset-suffix")}
              </>
            ) : (
              <>
                {t("assign-modal.sub-select-prefix")}<Em>{records.length}</Em>{t("assign-modal.sub-select-suffix")}
              </>
            )}
          </p>
        </DialogHeader>

        {handoff ? (
          <>
            <DialogBody>
              {/* 号此刻**还没删** —— 点下面「我已保存」才删。所以这里说"确认后删除"而不是"已删除" */}
              <Alert tone="danger" icon={AlertTriangle} title={t("assign-modal.handoff-alert-title")}>
                {t("assign-modal.handoff-alert-body")}
              </Alert>
              <div className="mt-3 max-h-60 space-y-2 overflow-y-auto rounded-xl border border-hairline bg-bg-elevated p-3">
                {handoff.keys.map((k) => (
                  <div key={k.credential_id} className="flex items-center gap-3 text-label">
                    <VendorTag name={vendorLabel(k.vendor_id, me?.tier)} />
                    {/* 真明文（不是打码版）· 后端从号池实时读，不落我方库 */}
                    <code className="min-w-0 flex-1 truncate font-mono">{k.key}</code>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => navigator.clipboard.writeText(k.key)}
                      aria-label={t("assign-modal.handoff-copy-aria")}
                    >
                      <Copy />
                    </Button>
                  </div>
                ))}
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="mt-2"
                onClick={() => navigator.clipboard.writeText(handoff.keys.map((k) => k.key).join("\n"))}
              >
                <Copy />
                {t("assign-modal.handoff-copy-all")}
              </Button>
            </DialogBody>
            <DialogFooter>
              {/* 「返回」不 confirm → 号留在池里，可以重来（这正是三段式的意义） */}
              <Button variant="ghost" onClick={() => setHandoff(null)}>{t("assign-modal.handoff-back")}</Button>
              <Button
                variant="danger"
                onClick={onConfirmHandoff}
                disabled={handoffConfirm.isPending}
              >
                <ArrowRight />
                {handoffConfirm.isPending ? t("assign-modal.handoff-confirm-pending") : t("assign-modal.handoff-confirm")}
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
                    {presetKind === "into_bus" && t("assign-modal.preset-into-bus-desc")}
                    {presetKind === "push_pool" && t("assign-modal.preset-push-pool-desc")}
                    {presetKind === "handoff" && t("assign-modal.preset-handoff-desc")}
                  </Alert>

                  {/* 进车必须选车 · 选完才算得出清算 */}
                  {presetKind === "into_bus" && (
                    <div className="space-y-2">
                      <Field label={t("assign-modal.field-target-bus")}>
                        <Select value={busId} onValueChange={setBusId}>
                          <SelectTrigger><SelectValue placeholder={t("assign-modal.select-placeholder")} /></SelectTrigger>
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
                    <div className="mb-2 text-label font-semibold">{t("assign-modal.keys-title")}</div>
                    <div className="max-h-40 space-y-1 overflow-y-auto">
                      {records.map((r) => (
                        <div key={r.id} className="flex items-center gap-2 text-label">
                          <span className="min-w-0 flex-1 truncate font-mono text-fg-secondary">
                            {r.key_masked}
                          </span>
                          <VendorTag name={vendorLabel(r.vendor_id, me?.tier)} />
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              ) : (
                /* 没带去向（兜底路径）· 让用户选 */
                <div className="space-y-2">
                  <KindOption
                    icon={Bus} title={t("assign-modal.kind-into-bus-title")} desc={t("assign-modal.kind-into-bus-desc")}
                    picked={kind === "into_bus"} onPick={() => setKind("into_bus")}
                  />
                  {kind === "into_bus" && (
                    <div className="ml-11 space-y-2 rounded-xl bg-bg-elevated p-3">
                      <Field label={t("assign-modal.field-target-bus-alt")}>
                        <Select value={busId} onValueChange={setBusId}>
                          <SelectTrigger>
                            <SelectValue placeholder={t("assign-modal.select-placeholder")} />
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
                    icon={Send} title={t("assign-modal.kind-push-pool-title")} desc={t("assign-modal.kind-push-pool-desc")}
                    picked={kind === "push_pool"} onPick={() => setKind("push_pool")}
                    disabled={!passengerpoolConnected}
                    disabledReason={!passengerpoolConnected ? t("assign-modal.kind-push-pool-disabled-reason") : undefined}
                  />

                  <KindOption
                    icon={Download} title={t("assign-modal.kind-handoff-title")} desc={t("assign-modal.kind-handoff-desc")}
                    picked={kind === "handoff"} onPick={() => setKind("handoff")}
                    warn
                  />
                </div>
              )}

              {/* P1-l/m · 拒推错误列表 · 号已失效 / 号已用完额度 */}
              {assignErrors.length > 0 && (
                <Alert
                  tone="danger"
                  icon={AlertTriangle}
                  title={t("assign-modal.errors-title", { count: assignErrors.length })}
                  className="mt-3"
                >
                  <ul className="space-y-1 text-label">
                    {assignErrors.map((e) => (
                      <li key={e.credential_id} className="flex items-start gap-2">
                        <span className="font-mono text-fg-tertiary">
                          {e.credential_id.slice(-8)}
                        </span>
                        <span>{e.message}</span>
                      </li>
                    ))}
                  </ul>
                </Alert>
              )}
            </DialogBody>

            <DialogFooter>
              <span className="mr-auto text-label text-fg-tertiary">
                {kind === "handoff" && t("assign-modal.footer-handoff-hint")}
              </span>
              <Button variant="ghost" onClick={onClose}>{t("assign-modal.cancel")}</Button>
              <Button
                onClick={onSubmit}
                disabled={!canSubmit || assign.isPending}
              >
                {assign.isPending ? t("assign-modal.submit-pending") : t("assign-modal.submit")}
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
  const { t } = useTranslation("extract");
  if (settlement.kind === "solo") {
    return <Muted>{t("assign-modal.settlement-solo")}</Muted>;
  }

  const skipLabel = settlement.skipped
    .map((s) => `@${s.username}${s.skipReason === "suspended" ? t("assign-modal.settlement-skipped-suspended") : t("assign-modal.settlement-skipped-short")}`)
    .join(" · ");

  /* 一个人都参与不了 · 派进去等于纯赠送 · 拦下来 */
  if (settlement.payers.length === 0) {
    return (
      <Alert tone="danger" icon={AlertTriangle} title={t("assign-modal.settlement-none-title")}>
        {skipLabel}{t("assign-modal.settlement-none-body-suffix")}
      </Alert>
    );
  }

  return (
    <div className="space-y-2">
      <Alert tone="ok" icon={Coins}>
        {t("assign-modal.settlement-payers-prefix")}
        <span className="font-semibold tnum text-ok-fg">{fmtCredits(settlement.income)}</span>{t("assign-modal.settlement-payers-suffix")}
      </Alert>
      {/* 跳过的只是这次不参与 · 车照进 · 所以是 warn 不是 danger */}
      {settlement.skipped.length > 0 && (
        <Alert tone="warn" icon={UserMinus} title={`${skipLabel}${t("assign-modal.settlement-skipped-title-suffix")}`}>
          {t("assign-modal.settlement-skipped-body-prefix")}
          <span className="font-semibold tnum">{fmtCredits(settlement.lost)}</span>{t("assign-modal.settlement-skipped-body-suffix")}
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
  const { t } = useTranslation("extract");
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
          {warn && <Chip tone="warn" className="text-[10px]">{t("assign-modal.chip-fire-and-forget")}</Chip>}
          {disabled && disabledReason && (
            <Chip tone="neutral" className="text-[10px]">{disabledReason}</Chip>
          )}
        </div>
        <div className="text-label text-fg-tertiary">{desc}</div>
      </div>
    </button>
  );
}
