import { useMemo, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import {
  ArrowDownLeft, ArrowUpRight, Check, Copy, Gift, Loader2, Lock,
  ShieldCheck, Ticket, TrendingDown, TrendingUp, Wallet as WalletIcon,
} from "lucide-react";
import {
  useCreateTopup, useLedger, useMyInvite, useRedeem, useWallet,
} from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead, Segmented,
} from "@/components/ui/primitives";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { cn, fmtCredits, fmtTime, MICRO, toCredits } from "@/lib/utils";
import type { LedgerEntry, LedgerType, TopupOrder } from "@/types";

/* 充值快捷档 · 元 */
const PRESETS = [50, 100, 200, 500];

export default function WalletPage() {
  const { t } = useTranslation("wallet");
  const { data: wallet } = useWallet();
  const { data: ledger, isLoading: ledgerLoading } = useLedger();
  const entries = ledger?.items ?? [];

  /* 累计充值 / 累计消费 · 从流水派生（后端将来会直接给，先从已有数据算）
     依赖写 ledger?.items 而不是 entries —— entries 每次 render 都是新数组（?? [] 的锅），
     会让 useMemo 永不命中 */
  const totals = useMemo(() => {
    let topup = 0, spend = 0;
    for (const e of ledger?.items ?? []) {
      if (e.amount > 0) topup += e.amount;
      else spend += -e.amount;
    }
    return { topup, spend };
  }, [ledger?.items]);

  return (
    <div className="space-y-section">
      {/* Hero · 余额 giant 48px · 全站唯一一处用这档（decisions §8.16 例外条款） */}
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">{t("hero.title")}</h1>
          <p className="text-fg-tertiary">
            {t("hero.subtitle")}
          </p>
        </div>
      </div>

      {/* minmax(0,1fr) 不能写 1fr —— 流水表里有 min-w-[560px]，
          `1fr` 的 auto 下限会让左列拒绝收缩、把右列 400px 挤出页面（1024-1152 会出现整页横向滚动条） */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_400px]">
        {/* 左：余额 + 流水 */}
        <div className="space-y-6">
          <Card focal focalTone="credit" className="p-7">
            <div className="flex items-center gap-2.5">
              <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-credit-bg">
                <WalletIcon className="size-3.5 text-credit-fg" />
              </span>
              <span className="text-label font-semibold text-fg-secondary">{t("balance.available")}</span>
            </div>

            <div className="mt-3 flex items-baseline gap-2">
              <span className="text-giant font-semibold tnum">
                {fmtCredits(wallet?.balance ?? 0)}
              </span>
              <span className="text-body-lg font-medium text-fg-tertiary">{t("balance.unit")}</span>
            </div>

            <div className="mt-5 grid grid-cols-3 gap-4 border-t border-hairline pt-4">
              <MiniTotal
                icon={Lock}
                label={t("balance.reserved.label")}
                value={fmtCredits(wallet?.reserved ?? 0)}
                hint={t("balance.reserved.hint")}
              />
              <MiniTotal
                icon={TrendingUp}
                label={t("balance.topup-total")}
                value={fmtCredits(totals.topup)}
                tone="ok"
              />
              <MiniTotal
                icon={TrendingDown}
                label={t("balance.spend-total")}
                value={fmtCredits(totals.spend)}
                tone="spend"
              />
            </div>
          </Card>

          <LedgerCard entries={entries} loading={ledgerLoading && !ledger} />
        </div>

        {/* 右：充值 + 兑换码 */}
        <div className="space-y-6">
          <TopupCard />
          <RedeemCard />
        </div>
      </div>
    </div>
  );
}

function MiniTotal({
  icon: Icon, label, value, hint, tone,
}: {
  icon: typeof Lock;
  label: string;
  value: string;
  hint?: string;
  tone?: "ok" | "spend";
}) {
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center gap-1.5 text-label text-fg-tertiary">
        <Icon className="size-3.5 shrink-0" />
        <span className="truncate font-medium">{label}</span>
      </div>
      <div
        className={cn(
          "text-stat font-semibold tnum",
          tone === "ok" ? "text-ok-fg" : tone === "spend" ? "text-danger-fg" : "text-fg",
        )}
      >
        {value}
      </div>
      {hint && <div className="truncate text-label text-fg-tertiary">{hint}</div>}
    </div>
  );
}

/* ─────────────── 充值 ─────────────── */

function TopupCard() {
  const { t } = useTranslation("wallet");
  /* 输入的是**想到账的积分数**（CLAUDE.md §1.4）· 通道费 5% 加在上面 · 支付 = 积分 × 1.05 */
  const [wantCredits, setWantCredits] = useState<string>("100");
  const create = useCreateTopup();
  const [order, setOrder] = useState<TopupOrder | null>(null);

  const credits = Math.max(0, Math.round(Number(wantCredits) || 0)) * MICRO;
  // 有个人邀请码额度时这单免手续费（decisions §8.29）· 真正的扣减在后端起单时做，
  // 这里只是**预览** —— 并发下可能被别的单抢走额度，最终以后端返的 fee_waived 为准
  const { data: myInvite } = useMyInvite();
  const willWaive = (myInvite?.waiver_remaining ?? 0) > 0;
  const fee = willWaive ? 0 : Math.round(credits * 0.05);
  const paid = credits + fee; // 乘客实付（CNY 口径，1 积分 ≡ 1 元）
  const valid = credits > 0;

  const onCreate = async () => {
    if (!valid) return;
    const o = await create.mutateAsync(credits);
    setOrder(o);
  };

  return (
    <>
      <Card className="p-7">
        <SectionHead
          title={t("topup.title")}
          sub={t("topup.sub")}
        />

        <div className="mt-4 space-y-4">
          <Field label={t("topup.field.want-credits.label")} hint={t("topup.field.want-credits.hint")}>
            <Input
              type="number"
              min={1}
              value={wantCredits}
              onChange={(e) => setWantCredits(e.target.value)}
              placeholder={t("topup.field.want-credits.placeholder")}
            />
          </Field>

          <div className="flex flex-wrap gap-2">
            {PRESETS.map((p) => (
              <Button
                key={p}
                variant={Number(wantCredits) === p ? "brand" : "ghost"}
                size="sm"
                onClick={() => setWantCredits(String(p))}
              >
                {t("topup.preset", { value: p })}
              </Button>
            ))}
          </div>

          {/* 通道费只在充值这一步展示（decisions §8.21）
              拉号 / 提取 / 派号都是积分抵扣，跟通道费无关，那些地方不显示 */}
          <div className="space-y-2 rounded-xl border border-hairline bg-bg-elevated/50 p-3.5">
            <Row
              label={t("topup.preview.want")}
              value={
                <Trans
                  i18nKey="topup.preview.want-value"
                  ns="wallet"
                  values={{ credits: toCredits(credits) }}
                  components={[<Em tone="ok" />]}
                />
              }
            />
            {willWaive ? (
              <Row
                label={
                  <Trans
                    i18nKey="topup.preview.fee-label"
                    ns="wallet"
                    components={[<span className="text-fg-tertiary" />]}
                  />
                }
                value={
                  <span className="flex items-center gap-1.5">
                    <span className="text-fg-tertiary line-through">
                      +{toCredits(Math.round(credits * 0.05))}
                    </span>
                    <Em tone="ok">{t("topup.preview.fee-waived-mark")}</Em>
                  </span>
                }
              />
            ) : (
              <Row
                label={
                  <Trans
                    i18nKey="topup.preview.fee-label"
                    ns="wallet"
                    components={[<span className="text-fg-tertiary" />]}
                  />
                }
                value={<Em tone="spend">+{toCredits(fee)}</Em>}
              />
            )}
            <div className="border-t border-hairline pt-2">
              <Row
                label={t("topup.preview.paid-label")}
                value={
                  <Trans
                    i18nKey="topup.preview.paid-value"
                    ns="wallet"
                    values={{ paid: toCredits(paid) }}
                    components={[<Em />]}
                  />
                }
                strong
              />
            </div>
          </div>

          <p className="text-label text-fg-tertiary">
            {willWaive ? (
              <Trans
                i18nKey="topup.hint.waived"
                ns="wallet"
                values={{ count: myInvite?.waiver_remaining ?? 0 }}
                components={[<Em plain />]}
              />
            ) : (
              t("topup.hint.default")
            )}
          </p>

          <Button
            className="w-full"
            variant="brand"
            onClick={onCreate}
            disabled={!valid || create.isPending}
          >
            {create.isPending ? <Loader2 className="animate-spin" /> : <WalletIcon />}
            {create.isPending ? t("topup.submit.pending") : t("topup.submit.default")}
          </Button>
        </div>
      </Card>

      <TopupOrderModal order={order} onClose={() => setOrder(null)} />
    </>
  );
}

function Row({
  label, value, strong,
}: { label: React.ReactNode; value: React.ReactNode; strong?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4 text-label">
      <span className={cn(strong ? "font-semibold" : "text-fg-tertiary")}>{label}</span>
      <span className="shrink-0">{value}</span>
    </div>
  );
}

/** 充值单 · 扫码或跳转到支付通道 */
function TopupOrderModal({
  order, onClose,
}: { order: TopupOrder | null; onClose: () => void }) {
  const { t } = useTranslation("wallet");
  const [copied, setCopied] = useState(false);

  /* 有 checkout_url：跳支付通道收款页 · 有 qr_content：渲染二维码
     两个都空是老 mock 单·退回旧提示 */
  const checkoutURL = order?.checkout_url ?? "";
  const qrContent = order?.qr_content ?? "";

  return (
    <Dialog open={!!order} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[420px]">
        <DialogHeader>
          <DialogTitle>{t("topup.modal.title")}</DialogTitle>
          <p className="text-label text-fg-tertiary">
            <Trans
              i18nKey="topup.modal.summary"
              ns="wallet"
              values={{
                paid: order ? toCredits(order.paid) : 0,
                credits: order ? toCredits(order.credits) : 0,
              }}
              components={[<Em />, <Em tone="ok" />]}
            />
          </p>
        </DialogHeader>
        <DialogBody>
          {/* 有 QR 显示 QR，有 checkout URL 显示"打开支付页"·托管通道一般是跳转不是扫码 */}
          {qrContent ? (
            <div className="grid place-items-center rounded-xl border border-hairline bg-bg-elevated p-6">
              <img
                alt={t("topup.modal.qr-alt")}
                className="size-40 rounded-lg border border-hairline bg-white"
                src={`https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=${encodeURIComponent(qrContent)}`}
              />
            </div>
          ) : (
            <div className="grid place-items-center rounded-xl border border-hairline bg-bg-elevated p-6">
              <p className="text-label text-fg-tertiary">{t("topup.modal.fallback")}</p>
            </div>
          )}

          <div className="mt-3 flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-lg border border-hairline bg-bg-elevated px-3 py-2 font-mono text-label">
              {checkoutURL || qrContent}
            </code>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("topup.modal.copy-aria")}
              onClick={() => {
                const s = checkoutURL || qrContent;
                if (!s) return;
                navigator.clipboard.writeText(s);
                setCopied(true);
                setTimeout(() => setCopied(false), 1600);
              }}
            >
              {copied ? <Check /> : <Copy />}
            </Button>
          </div>

          {checkoutURL && (
            <Button
              className="mt-3 w-full"
              variant="brand"
              onClick={() => window.open(checkoutURL, "_blank", "noopener,noreferrer")}
            >
              {t("topup.modal.open-checkout")}
            </Button>
          )}

          <Alert tone="neutral" icon={ShieldCheck} className="mt-3">
            {t("topup.modal.notice")}
          </Alert>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>{t("topup.modal.close")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ─────────────── 兑换码 ─────────────── */

function RedeemCard() {
  const { t } = useTranslation("wallet");
  const [code, setCode] = useState("");
  const redeem = useRedeem();
  const [okMsg, setOkMsg] = useState<string | null>(null);

  const onRedeem = async () => {
    if (!code.trim()) return;
    setOkMsg(null);
    try {
      const r = await redeem.mutateAsync(code.trim());
      setOkMsg(t("redeem.success.message", { credits: fmtCredits(r.credits) }));
      setCode("");
    } catch {
      /* 错误走下面 redeem.error 渲染 */
    }
  };

  return (
    <Card className="p-7">
      <SectionHead title={t("redeem.title")} sub={t("redeem.sub")} />

      <div className="mt-4 space-y-3">
        <Field label={t("redeem.field.label")}>
          <Input
            value={code}
            onChange={(e) => { setCode(e.target.value); setOkMsg(null); }}
            onKeyDown={(e) => e.key === "Enter" && onRedeem()}
            placeholder={t("redeem.field.placeholder")}
            className="font-mono uppercase"
          />
        </Field>

        <Button
          className="w-full"
          onClick={onRedeem}
          disabled={!code.trim() || redeem.isPending}
        >
          {redeem.isPending ? <Loader2 className="animate-spin" /> : <Ticket />}
          {redeem.isPending ? t("redeem.submit.pending") : t("redeem.submit.default")}
        </Button>

        {okMsg && <Alert tone="ok" icon={Gift} title={t("redeem.success.title")}>{okMsg}</Alert>}
        {redeem.isError && (
          <Alert tone="danger" icon={Ticket} title={t("redeem.error.title")}>
            {(redeem.error as Error).message}
          </Alert>
        )}
      </div>
    </Card>
  );
}

/* ─────────────── 流水 ─────────────── */

/** 流水类型 → 用户能看懂的话（内部枚举不出现在 UI · CLAUDE.md §12.6） */
const LEDGER_LABEL_KEY: Record<LedgerType, string> = {
  topup: "ledger.type.topup",
  spend: "ledger.type.spend",
  redeem: "ledger.type.redeem",
  refund: "ledger.type.refund",
  warranty_refund: "ledger.type.warranty-refund",
  // 正负都用这一个词 —— 金额符号已经说明方向（收 / 付）
  share: "ledger.type.share",
};

type FilterKey = "all" | "in" | "out";

const FILTER_KEYS: { value: FilterKey; labelKey: string }[] = [
  { value: "all", labelKey: "ledger.filter.all" },
  { value: "in", labelKey: "ledger.filter.in" },
  { value: "out", labelKey: "ledger.filter.out" },
];

function LedgerCard({
  entries, loading,
}: {
  entries: LedgerEntry[];
  loading?: boolean;
}) {
  const { t } = useTranslation("wallet");
  const [filter, setFilter] = useState<FilterKey>("all");
  const filters = FILTER_KEYS.map((f) => ({ value: f.value, label: t(f.labelKey) }));

  /* 筛选按"钱进来还是出去"分，不按内部 type 枚举 —— 用户只关心这个 */
  const shown = entries.filter((e) =>
    filter === "all" ? true : filter === "in" ? e.amount > 0 : e.amount < 0,
  );

  return (
    <Card className="p-7">
      <SectionHead
        title={t("ledger.title")}
        sub={
          <Trans
            i18nKey="ledger.sub"
            ns="wallet"
            values={{ count: entries.length }}
            components={[<Em />]}
          />
        }
        right={<Segmented options={filters} value={filter} onChange={setFilter} />}
      />

      {loading ? (
        <SkeletonTable rows={6} cols={["w-20", "w-16", "w-1/3", "w-20", "w-20"]} />
      ) : shown.length === 0 ? (
        entries.length === 0 ? (
          <EmptyState
            icon={WalletIcon}
            title={t("ledger.empty.title")}
            desc={t("ledger.empty.desc")}
          />
        ) : (
          /* 有数据但筛选后为空 —— 引导改筛选·不要引导他去充值 */
          <EmptyState
            icon={WalletIcon}
            title={t("ledger.empty-filtered.title")}
            desc={t("ledger.empty-filtered.desc")}
          />
        )
      ) : (
        <div className="mt-4 overflow-x-auto">
          <div className="min-w-[560px]">
            <BareHead>
              <span className="w-[92px] shrink-0">{t("ledger.header.time")}</span>
              <span className="w-20 shrink-0">{t("ledger.header.type")}</span>
              <span className="min-w-0 flex-1">{t("ledger.header.memo")}</span>
              <span className="w-24 shrink-0 text-right">{t("ledger.header.change")}</span>
              <span className="w-24 shrink-0 text-right">{t("ledger.header.balance")}</span>
            </BareHead>
            <BareList>
              {shown.map((e) => (
                <LedgerRow key={e.id} e={e} />
              ))}
            </BareList>
          </div>
        </div>
      )}
    </Card>
  );
}

function LedgerRow({ e }: { e: LedgerEntry }) {
  const { t } = useTranslation("wallet");
  const income = e.amount > 0;
  return (
    <BareRow>
      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(e.created_at)}
      </span>
      <span className="w-20 shrink-0">
        <Chip tone={income ? "ok" : "neutral"}>{t(LEDGER_LABEL_KEY[e.type])}</Chip>
      </span>
      <span className="min-w-0 flex-1 truncate text-label text-fg-secondary">{e.memo}</span>
      <span className="flex w-24 shrink-0 items-center justify-end gap-1">
        {income
          ? <ArrowDownLeft className="size-3.5 shrink-0 text-ok-fg" />
          : <ArrowUpRight className="size-3.5 shrink-0 text-danger-fg" />}
        <Em tone={income ? "ok" : "spend"}>
          {income ? "+" : "-"}{fmtCredits(Math.abs(e.amount))}
        </Em>
      </span>
      <span className="w-24 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
        {fmtCredits(e.balance_after)}
      </span>
    </BareRow>
  );
}
