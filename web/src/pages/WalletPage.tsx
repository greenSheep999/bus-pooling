import { useMemo, useState } from "react";
import {
  ArrowDownLeft, ArrowUpRight, Check, Copy, Gift, Loader2, Lock,
  ShieldCheck, Ticket, TrendingDown, TrendingUp, Wallet as WalletIcon,
} from "lucide-react";
import {
  useCreateTopup, useLedger, useRedeem, useWallet,
} from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead, Segmented,
} from "@/components/ui/primitives";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
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
  const { data: wallet } = useWallet();
  const { data: ledger } = useLedger();
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
          <h1 className="text-hero font-semibold">钱包</h1>
          <p className="text-fg-tertiary">
            充值得积分 · 拉号和提取从这里扣 · 号 30 分钟内失效自动退
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
              <span className="text-label font-semibold text-fg-secondary">可用余额</span>
            </div>

            <div className="mt-3 flex items-baseline gap-2">
              <span className="text-giant font-semibold tnum">
                {fmtCredits(wallet?.balance ?? 0)}
              </span>
              <span className="text-body-lg font-medium text-fg-tertiary">积分</span>
            </div>

            <div className="mt-5 grid grid-cols-3 gap-4 border-t border-hairline pt-4">
              <MiniTotal
                icon={Lock}
                label="冻结中"
                value={fmtCredits(wallet?.reserved ?? 0)}
                hint="拉号进行中占用"
              />
              <MiniTotal
                icon={TrendingUp}
                label="累计充值"
                value={fmtCredits(totals.topup)}
                tone="ok"
              />
              <MiniTotal
                icon={TrendingDown}
                label="累计消费"
                value={fmtCredits(totals.spend)}
                tone="spend"
              />
            </div>
          </Card>

          <LedgerCard entries={entries} />
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
  /* 输入的是**想到账的积分数**（CLAUDE.md §1.4）· 通道费 5% 加在上面 · 支付 = 积分 × 1.05 */
  const [wantCredits, setWantCredits] = useState<string>("100");
  const create = useCreateTopup();
  const [order, setOrder] = useState<TopupOrder | null>(null);

  const credits = Math.max(0, Math.round(Number(wantCredits) || 0)) * MICRO;
  const fee = Math.round(credits * 0.05);
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
          title="充值"
          sub={<>1 积分 = 1 元 · 通道费 5% 加在本金上</>}
        />

        <div className="mt-4 space-y-4">
          <Field label="想充多少积分" hint="积分">
            <Input
              type="number"
              min={1}
              value={wantCredits}
              onChange={(e) => setWantCredits(e.target.value)}
              placeholder="输入积分数"
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
                {p} 积分
              </Button>
            ))}
          </div>

          {/* 通道费只在充值这一步展示（decisions §8.21）
              拉号 / 提取 / 派号都是积分抵扣，跟通道费无关，那些地方不显示 */}
          <div className="space-y-2 rounded-xl border border-hairline bg-bg-elevated/50 p-3.5">
            <Row label="想到账" value={<><Em tone="ok">{toCredits(credits)}</Em> 积分</>} />
            <Row
              label={<>waffo 通道费 <span className="text-fg-tertiary">5%</span></>}
              value={<Em tone="spend">+{toCredits(fee)}</Em>}
            />
            <div className="border-t border-hairline pt-2">
              <Row
                label="你需支付"
                value={<><Em>{toCredits(paid)}</Em> 元</>}
                strong
              />
            </div>
          </div>

          <p className="text-label text-fg-tertiary">
            通道费由支付通道 waffo 收取，我方不加价也不承担 · 之后拉号、提取都是积分抵扣，不再收通道费
          </p>

          <Button
            className="w-full"
            variant="brand"
            onClick={onCreate}
            disabled={!valid || create.isPending}
          >
            {create.isPending ? <Loader2 className="animate-spin" /> : <WalletIcon />}
            {create.isPending ? "生成中…" : "生成充值单"}
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

/** 充值单 · 扫码付到 waffo */
function TopupOrderModal({
  order, onClose,
}: { order: TopupOrder | null; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  /* 有 checkout_url：跳 gateway 收款页（waffo）· 有 qr_content：渲染二维码
     两个都空是老 mock 单·退回旧提示 */
  const checkoutURL = order?.checkout_url ?? "";
  const qrContent = order?.qr_content ?? "";

  return (
    <Dialog open={!!order} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[420px]">
        <DialogHeader>
          <DialogTitle>去支付</DialogTitle>
          <p className="text-label text-fg-tertiary">
            付 <Em>{order ? toCredits(order.paid) : 0}</Em> 元 ·
            到账 <Em tone="ok">{order ? toCredits(order.credits) : 0}</Em> 积分
          </p>
        </DialogHeader>
        <DialogBody>
          {/* 有 QR 显示 QR，有 checkout URL 显示"打开支付页"·waffo 一般是跳转不是扫码 */}
          {qrContent ? (
            <div className="grid place-items-center rounded-xl border border-hairline bg-bg-elevated p-6">
              <img
                alt="收款二维码"
                className="size-40 rounded-lg border border-hairline bg-white"
                src={`https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=${encodeURIComponent(qrContent)}`}
              />
            </div>
          ) : (
            <div className="grid place-items-center rounded-xl border border-hairline bg-bg-elevated p-6">
              <p className="text-label text-fg-tertiary">点下面按钮跳去支付页</p>
            </div>
          )}

          <div className="mt-3 flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-lg border border-hairline bg-bg-elevated px-3 py-2 font-mono text-label">
              {checkoutURL || qrContent}
            </code>
            <Button
              variant="ghost"
              size="icon"
              aria-label="复制支付链接"
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
              打开支付页
            </Button>
          )}

          <Alert tone="neutral" icon={ShieldCheck} className="mt-3">
            支付完成后积分自动到账 · 15 分钟内有效 · 未付款自动作废
          </Alert>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ─────────────── 兑换码 ─────────────── */

function RedeemCard() {
  const [code, setCode] = useState("");
  const redeem = useRedeem();
  const [okMsg, setOkMsg] = useState<string | null>(null);

  const onRedeem = async () => {
    if (!code.trim()) return;
    setOkMsg(null);
    try {
      const r = await redeem.mutateAsync(code.trim());
      setOkMsg(`到账 ${fmtCredits(r.credits)} 积分`);
      setCode("");
    } catch {
      /* 错误走下面 redeem.error 渲染 */
    }
  };

  return (
    <Card className="p-7">
      <SectionHead title="兑换码" sub="社群发的码 · 兑换直接进余额" />

      <div className="mt-4 space-y-3">
        <Field label="兑换码">
          <Input
            value={code}
            onChange={(e) => { setCode(e.target.value); setOkMsg(null); }}
            onKeyDown={(e) => e.key === "Enter" && onRedeem()}
            placeholder="KIRO-XXXX"
            className="font-mono uppercase"
          />
        </Field>

        <Button
          className="w-full"
          onClick={onRedeem}
          disabled={!code.trim() || redeem.isPending}
        >
          {redeem.isPending ? <Loader2 className="animate-spin" /> : <Ticket />}
          {redeem.isPending ? "兑换中…" : "兑换"}
        </Button>

        {okMsg && <Alert tone="ok" icon={Gift} title="兑换成功">{okMsg}</Alert>}
        {redeem.isError && (
          <Alert tone="danger" icon={Ticket} title="兑换失败">
            {(redeem.error as Error).message}
          </Alert>
        )}
      </div>
    </Card>
  );
}

/* ─────────────── 流水 ─────────────── */

/** 流水类型 → 用户能看懂的话（内部枚举不出现在 UI · CLAUDE.md §12.6） */
const LEDGER_LABEL: Record<LedgerType, string> = {
  topup: "充值",
  spend: "消费",
  redeem: "兑换",
  refund: "退款",
  warranty_refund: "质保退款",
};

type FilterKey = "all" | "in" | "out";

const FILTERS: { value: FilterKey; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "in", label: "入账" },
  { value: "out", label: "出账" },
];

function LedgerCard({ entries }: { entries: LedgerEntry[] }) {
  const [filter, setFilter] = useState<FilterKey>("all");

  /* 筛选按"钱进来还是出去"分，不按内部 type 枚举 —— 用户只关心这个 */
  const shown = entries.filter((e) =>
    filter === "all" ? true : filter === "in" ? e.amount > 0 : e.amount < 0,
  );

  return (
    <Card className="p-7">
      <SectionHead
        title="流水"
        sub={<>共 <Em>{entries.length}</Em> 条</>}
        right={<Segmented options={FILTERS} value={filter} onChange={setFilter} />}
      />

      {shown.length === 0 ? (
        <div className="py-12 text-center text-label text-fg-tertiary">
          {entries.length === 0 ? "还没有流水" : "这个筛选下没有记录"}
        </div>
      ) : (
        <div className="mt-4 overflow-x-auto">
          <div className="min-w-[560px]">
            <BareHead>
              <span className="w-[92px] shrink-0">时间</span>
              <span className="w-20 shrink-0">类型</span>
              <span className="min-w-0 flex-1">说明</span>
              <span className="w-24 shrink-0 text-right">变动</span>
              <span className="w-24 shrink-0 text-right">余额</span>
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
  const income = e.amount > 0;
  return (
    <BareRow>
      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(e.created_at)}
      </span>
      <span className="w-20 shrink-0">
        <Chip tone={income ? "ok" : "neutral"}>{LEDGER_LABEL[e.type]}</Chip>
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
