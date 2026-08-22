import { useEffect, useMemo, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import {
  ArrowDownLeft, ArrowUpRight, Check, Gift, Info, Loader2, Lock,
  ShieldCheck, Ticket, TrendingDown, TrendingUp, Wallet as WalletIcon,
} from "lucide-react";
import {
  useCouponLookup, useCreateTopup, useLedger, useMe, useMyInvite, useRedeem, useTopupChannels, useWallet,
} from "@/api/hooks";
import { BybitLogo, WaffoLogo } from "@/components/PaymentLogo";
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
import { notify } from "@/lib/toast";
import {
  cn, creditsToUSD, fmtCredits, fmtTime, MICRO, toCredits, TOPUP_PRESETS,
} from "@/lib/utils";
import type { LedgerEntry, LedgerType } from "@/types";

/* 充值快捷档 · 复用 lib/utils.ts 里的常量 · landing / wallet 单点更新 */
const PRESETS = TOPUP_PRESETS;

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
  /* 输入的是**想到账的积分数** · CLAUDE §1.4 · 通道费 5% 加在上面 · 展示金额只用 USD */
  const [wantCredits, setWantCredits] = useState<string>("100");
  /* 选中的通道 · 默认 hosted 里第一个 enabled 的 */
  const [channelId, setChannelId] = useState<string>("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const create = useCreateTopup();
  const { data: channelsResp } = useTopupChannels();
  const channels = channelsResp?.channels ?? [];
  const { data: me } = useMe();
  /* CLAUDE §1.3 铁律: UI 只区分"绑了专属邀请码 vs 没绑" · 不暴露 tier 具体档次
     tier != "retail" 时统一显示"社群成员" · retail 时不显 chip */
  const isCommunityMember = me?.tier && me.tier !== "retail";

  const credits = Math.max(0, Math.round(Number(wantCredits) || 0)) * MICRO;
  const { data: myInvite } = useMyInvite();
  const willWaive = (myInvite?.waiver_remaining ?? 0) > 0;
  const usdAmount = creditsToUSD(toCredits(credits), willWaive);
  /* 手续费单独算 · 差额 · fee = (credits × 0.05) / 7 = credits / 140 · toFixed(2) */
  const feeUSD = (toCredits(credits) / 140).toFixed(2);
  const valid = credits > 0;

  /* 默认选中第一个 enabled 通道 · channels 加载完后 · 一次性设 */
  const defaultChannel = useMemo(
    () => channels.find((c) => c.enabled)?.id ?? "",
    [channels],
  );
  const activeChannel = channelId || defaultChannel;
  const selectedChannel = channels.find((c) => c.id === activeChannel);

  const onOpenConfirm = () => {
    if (!valid || !activeChannel) return;
    setConfirmOpen(true);
  };
  const onConfirmAndPay = async (couponCode: string) => {
    if (!valid || !activeChannel) return;
    /* couponCode · 空串 = 不用 · decisions §8.43
       hook 已支持 · 后端 topupRequest 加字段 + 落库前 · 会被 decodeStrict 拒
       所以先在 hook 内 gate 一下 · 有值才带 · Go 侧字段落地后自然生效 */
    try {
      const o = await create.mutateAsync({
        credits,
        channel: activeChannel,
        couponCode: couponCode || undefined,
      });
      setConfirmOpen(false);
      notify.info({ title: t("common:toast.topup_redirect") });
      /* 拿到 checkout_url 直接跳 · 无中间 dialog(hosted 通道就是要跳)*/
      if (o.checkout_url) {
        window.location.href = o.checkout_url;
      }
    } catch (err) {
      notify.fail(err, t("common:toast.generic_fail"));
    }
  };

  return (
    <>
      <Card className="p-7">
        <div className="flex items-start justify-between gap-3">
          <SectionHead
            title={t("topup.title")}
            sub={t("topup.sub")}
          />
          {isCommunityMember && (
            <span
              title={t("topup.tier.community-tip")}
              aria-label={t("topup.tier.community-tip")}
              className="shrink-0 cursor-help"
            >
              <Chip tone="brand" icon={<ShieldCheck className="size-3" />}>
                {t("topup.tier.community")}
              </Chip>
            </span>
          )}
        </div>

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

          {/* 通道卡片 · 一行 4 张(md+ 4 列 · 移动端 2 列)· logo-only + title tooltip
              hover 显品牌名 · disabled 覆盖 "即将开放" · 单选 · brand 边框选中 · 无花花绿绿 */}
          <div className="space-y-2">
            <label className="text-label font-semibold text-fg-secondary">
              {t("topup.channel.label")}
            </label>
            <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-5">
              {channels.map((c) => {
                const on = c.id === activeChannel;
                /* hover 只显名称 · disabled 状态视觉本身已经传达(灰化 + 不可点)
                   把 coming-soon 塞进卡里挤爆布局 · 一直不能这么做 */
                const hoverLabel = c.display_name;
                return (
                  <button
                    key={c.id}
                    type="button"
                    disabled={!c.enabled}
                    onClick={() => c.enabled && setChannelId(c.id)}
                    className={cn(
                      "group relative grid h-8 place-items-center overflow-hidden rounded-md border bg-bg-elevated transition-all",
                      c.enabled && on && "border-brand-strong ring-1 ring-brand-strong/20",
                      c.enabled && !on && "border-hairline hover:border-fg-tertiary/60",
                      !c.enabled && "cursor-not-allowed border-hairline opacity-60",
                    )}
                    aria-pressed={c.enabled && on}
                    aria-label={hoverLabel}
                  >
                    {/* logo · hover/focus 时淡出 */}
                    <span className="transition-opacity duration-150 group-hover:opacity-0 group-focus-visible:opacity-0">
                      {c.id === "bybit" ? (
                        <BybitLogo className="h-2.5 w-auto" />
                      ) : c.id === "waffo" ? (
                        <WaffoLogo className="h-2.5 w-auto" />
                      ) : (
                        <img
                          src={`/logos/payment/${c.id}.svg`}
                          alt=""
                          className="h-4 w-auto object-contain"
                          onError={(e) => { e.currentTarget.style.display = "none"; }}
                        />
                      )}
                    </span>
                    {/* 名称覆盖 · 默认隐藏 · hover/focus 淡入 */}
                    <span
                      aria-hidden
                      className="pointer-events-none absolute inset-0 grid place-items-center text-[11px] font-semibold text-fg-secondary opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100"
                    >
                      {hoverLabel}
                    </span>
                    {c.enabled && on && (
                      <span aria-hidden className="absolute right-1 top-1 grid size-3 place-items-center rounded-full bg-brand-strong text-white">
                        <Check className="size-2" strokeWidth={3} />
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          </div>

          {/* 预览 · 只默认色 · Total 大字号强调
              个人邀请码免手续费额度直接内嵌 fee 行 · 不再单独 hint */}
          <div className="rounded-xl border border-hairline bg-bg-elevated/50 p-4">
            <div className="space-y-1.5 text-label">
              <Row
                label={t("topup.preview.want")}
                value={
                  <span>
                    <span className="font-semibold tnum">{toCredits(credits).toLocaleString()}</span>{" "}
                    <span className="text-fg-tertiary">{t("topup.preview.credits-unit")}</span>
                  </span>
                }
              />
              <Row
                label={
                  <span className="group/tt relative inline-flex cursor-help items-center gap-1">
                    {t("topup.preview.fee-label")}
                    <Info className="size-3 text-fg-tertiary/60" aria-hidden />
                    {/* CSS-only tooltip · group hover + focus 都可触发 · 定位在 label 上方
                        原生 title 延迟 500ms 且小图标 hover 不稳 · 换自建 */}
                    <span
                      role="tooltip"
                      className="pointer-events-none absolute left-0 top-full z-20 mt-1 w-max max-w-[260px] rounded-md bg-fg px-2 py-1.5 text-[11px] font-normal leading-tight text-bg opacity-0 shadow-lg transition-opacity duration-100 group-hover/tt:opacity-100"
                    >
                      {t("topup.preview.fee-tooltip")}
                    </span>
                  </span>
                }
                value={
                  willWaive ? (
                    <span className="flex items-baseline gap-1.5 tnum">
                      <span className="text-fg-tertiary line-through">
                        {feeUSD} USD
                      </span>
                      <span className="text-fg-tertiary">{t("topup.preview.fee-waived-mark")}</span>
                    </span>
                  ) : (
                    <span className="tnum">
                      +{feeUSD} <span className="text-fg-tertiary">USD</span>
                    </span>
                  )
                }
              />
            </div>
            {/* Total · You pay 稍粗 · 别太大挤 · text-lg bold */}
            <div className="mt-3 flex items-baseline justify-between gap-3 border-t border-hairline pt-3">
              <span className="text-label font-semibold text-fg-secondary">
                {t("topup.preview.paid-label")}
              </span>
              <span className="tnum text-lg font-bold text-fg">
                {usdAmount} <span className="text-sm font-medium text-fg-tertiary">USD</span>
              </span>
            </div>
            {willWaive && (
              <p className="mt-2 text-[11px] text-fg-tertiary">
                <Trans
                  i18nKey="topup.hint.waived"
                  ns="wallet"
                  values={{ count: myInvite?.waiver_remaining ?? 0 }}
                  components={[<span className="font-semibold text-fg" />]}
                />
              </p>
            )}
          </div>

          <Button
            className="w-full"
            variant="brand"
            onClick={onOpenConfirm}
            disabled={!valid || !activeChannel}
          >
            <WalletIcon />
            {t("topup.submit.default")}
          </Button>
        </div>
      </Card>

      <TopupConfirmDialog
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        credits={toCredits(credits)}
        usdAmount={usdAmount}
        feeUSD={feeUSD}
        willWaive={willWaive}
        channelName={selectedChannel?.display_name ?? ""}
        submitting={create.isPending}
        onConfirm={onConfirmAndPay}
      />
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

/** 充值确认弹窗 · 点"生成充值单"后弹出 · 展示订单核心 + 协议条款 + 勾选同意才能付
 *
 *  为什么要有这一步(vs 直接跳):
 *  - 支付是**用户实付真金白银**的动作 · 得二次确认防误操作
 *  - 展示条款(不承诺可用时长 / 只退积分 / 通道费不退 · 见 docs/00 §7.5 规则 B)
 *  - 勾选"同意"才启用支付按钮 · 留痕(乘客表示知晓)
 *  - 撤 CNY 展示 · 只显示 USD 金额(§0.1) */
function TopupConfirmDialog({
  open, onClose, credits, usdAmount, feeUSD, willWaive, channelName, submitting, onConfirm,
}: {
  open: boolean;
  onClose: () => void;
  credits: number;
  usdAmount: string;
  feeUSD: string;
  willWaive: boolean;
  channelName: string;
  submitting: boolean;
  onConfirm: (couponCode: string) => void;
}) {
  const { t } = useTranslation("wallet");
  const [coupon, setCoupon] = useState("");

  /* 优惠码 debounce · 输码 500ms 无变化才查后端 · 避免每 keystroke 一次请求 */
  const [debouncedCoupon, setDebouncedCoupon] = useState("");
  useEffect(() => {
    const h = setTimeout(() => setDebouncedCoupon(coupon.trim()), 500);
    return () => clearTimeout(h);
  }, [coupon]);
  const { data: couponInfo, error: couponError, isFetching: couponFetching } =
    useCouponLookup(debouncedCoupon, "topup");

  /* 折后 USD = usdAmount * (1 - discount_bp/10000)
     usdAmount 是 "13.50" 字符串 · 折扣 basis point 转小数 · 保 2 位 */
  const discountBP = couponInfo?.discount_bp ?? 0;
  const usdNum = Number(usdAmount);
  const discountUSD = discountBP > 0 ? (usdNum * discountBP / 10000).toFixed(2) : null;
  const usdAmountAfter = discountBP > 0 ? (usdNum - usdNum * discountBP / 10000).toFixed(2) : usdAmount;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !submitting && onClose()}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{t("topup.confirm.title")}</DialogTitle>
          <p className="text-label text-fg-tertiary">{t("topup.confirm.sub")}</p>
        </DialogHeader>
        <DialogBody>
          {/* 订单摘要 · 通道 + 到账积分 · 无色 */}
          <div className="rounded-xl border border-hairline bg-bg-elevated/50 p-4">
            <div className="space-y-1.5 text-label">
              <Row label={t("topup.confirm.channel")} value={<span className="font-semibold">{channelName}</span>} />
              <Row
                label={t("topup.confirm.credits")}
                value={
                  <span>
                    <span className="font-semibold tnum">{credits.toLocaleString()}</span>{" "}
                    <span className="text-fg-tertiary">{t("topup.preview.credits-unit")}</span>
                  </span>
                }
              />
              <Row
                label={
                  <span className="group/tt relative inline-flex cursor-help items-center gap-1">
                    {t("topup.preview.fee-label")}
                    <Info className="size-3 text-fg-tertiary/60" aria-hidden />
                    <span
                      role="tooltip"
                      className="pointer-events-none absolute left-0 top-full z-20 mt-1 w-max max-w-[260px] rounded-md bg-fg px-2 py-1.5 text-[11px] font-normal leading-tight text-bg opacity-0 shadow-lg transition-opacity duration-100 group-hover/tt:opacity-100"
                    >
                      {t("topup.preview.fee-tooltip")}
                    </span>
                  </span>
                }
                value={
                  willWaive ? (
                    <span className="flex items-baseline gap-1.5 tnum">
                      <span className="text-fg-tertiary line-through">{feeUSD} USD</span>
                      <span className="text-fg-tertiary">{t("topup.preview.fee-waived-mark")}</span>
                    </span>
                  ) : (
                    <span className="tnum">+{feeUSD} <span className="text-fg-tertiary">USD</span></span>
                  )
                }
              />
              {/* 优惠行 · 输码后 debounce lookup · 有效码返 discount_bp · 显减免 */}
              {discountUSD && (
                <Row
                  label={t("topup.confirm.coupon.discount-label")}
                  value={
                    <span className="tnum text-fg-secondary">
                      -{discountUSD} <span className="text-fg-tertiary">USD</span>
                    </span>
                  }
                />
              )}
            </div>
            <div className="mt-3 flex items-baseline justify-between gap-3 border-t border-hairline pt-3">
              <span className="text-label font-semibold text-fg-secondary">{t("topup.confirm.pay")}</span>
              <span className="tnum text-lg font-bold">
                {usdAmountAfter} <span className="text-sm font-medium text-fg-tertiary">USD</span>
              </span>
            </div>
          </div>

          {/* 优惠码输入 · decisions §8.43 · 社群发放的一次性减免码
              一次性充值使用 · 减实付 USD · 不加积分 · 到账 N 就是 N
              可选 · 空 = 不用 · 前端只做输入 UI · 后端未接前不假算减免额
              后端接通后 preview 里加"优惠 -X.XX USD"行(P1) */}
          <div className="mt-4 space-y-2">
            <label className="flex items-center gap-1.5 text-label font-semibold text-fg-secondary">
              <Ticket className="size-3.5 text-fg-tertiary" aria-hidden />
              {t("topup.confirm.coupon.label")}
            </label>
            <Input
              value={coupon}
              onChange={(e) => setCoupon(e.target.value.toUpperCase())}
              placeholder={t("topup.confirm.coupon.placeholder")}
              className="font-mono uppercase tracking-wide"
              maxLength={32}
              disabled={submitting}
            />
            {/* 校验反馈 · 输码后立即显 · 空 = hint · 有码 = 校验中/valid/error */}
            {debouncedCoupon.length === 0 ? (
              <p className="text-[11px] text-fg-tertiary">{t("topup.confirm.coupon.hint")}</p>
            ) : couponFetching ? (
              <p className="text-[11px] text-fg-tertiary">{t("topup.confirm.coupon.checking")}</p>
            ) : couponError ? (
              <p className="text-[11px] text-danger-fg">
                {(couponError as { message?: string })?.message || t("topup.confirm.coupon.invalid")}
              </p>
            ) : couponInfo ? (
              <p className="text-[11px] text-brand-strong">
                {t("topup.confirm.coupon.applied", { pct: (couponInfo.discount_bp ?? 0) / 100 })}
              </p>
            ) : null}
          </div>

          {/* 简短协议 · neutral(灰)Alert · Info icon · 不用醒目色 */}
          <Alert tone="neutral" icon={Info} className="mt-3 text-[11px]">
            {t("topup.confirm.terms-short")}
          </Alert>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            {t("topup.confirm.cancel")}
          </Button>
          <Button
            variant="brand"
            onClick={() => onConfirm(coupon.trim())}
            disabled={submitting}
          >
            {submitting ? <Loader2 className="animate-spin" /> : <ShieldCheck />}
            {submitting ? t("topup.confirm.submitting") : t("topup.confirm.submit")}
          </Button>
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
      notify.ok({ title: t("common:toast.redeem_ok", { amount: fmtCredits(r.credits) }) });
      setCode("");
    } catch (err) {
      notify.fail(err, t("common:toast.generic_fail"));
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
