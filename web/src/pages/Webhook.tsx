import { useEffect, useState } from "react";
import {
  AlertTriangle, Bot, Check, CheckCircle2, Loader2, RefreshCw, Save, Send, X, Zap,
} from "lucide-react";
import {
  useRegenWebhookSecret, useSaveWebhook, useTestWebhook, useWebhook, useWebhookDeliveries,
} from "@/api/hooks";
import { SettingsHead } from "@/components/SettingsHead";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead, Segmented,
} from "@/components/ui/primitives";
import { SecretField } from "@/components/ui/secret-field";
import { Switch } from "@/components/ui/switch";
import { fmtTime } from "@/lib/utils";
import type { WebhookDelivery } from "@/types";

/** 可订阅事件 · 5 个（spec §10b）
 *  event id 是对外契约的一部分（用户要在自己代码里 switch），所以这里**故意露出原名**
 *  —— 技术页例外，跟对接文档同性质（CLAUDE.md §12.6） */
const EVENTS: { id: string; title: string; desc: string }[] = [
  { id: "round.completed", title: "拉号完成", desc: "一轮拉号跑完 · 带拿到几个号、花了多少" },
  { id: "round.failed", title: "拉号失败", desc: "整轮没拿到号 · 带失败原因" },
  { id: "credential.dead", title: "号失效了", desc: "我方探到某个号死了 · 补车前的信号" },
  { id: "bus.refilled", title: "自动补车触发", desc: "水位跌破阈值、系统自己补了一轮" },
  { id: "wallet.low", title: "余额不足预警", desc: "余额低到快拉不动号了" },
];

export default function Webhook() {
  const { data: cfg } = useWebhook();
  const { data: deliveries } = useWebhookDeliveries();
  const save = useSaveWebhook();
  const test = useTestWebhook();
  const regen = useRegenWebhookSecret();

  const [url, setUrl] = useState("");
  const [newSecret, setNewSecret] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; code: number; ms: number } | null>(null);

  useEffect(() => {
    if (cfg && !url) setUrl(cfg.url);
  }, [cfg, url]);

  const dirty = !!cfg && url !== cfg.url;
  const subscribed = new Set(cfg?.events ?? []);

  const toggleEvent = (id: string, on: boolean) => {
    if (!cfg) return;
    const next = on
      ? [...cfg.events, id]
      : cfg.events.filter((e) => e !== id);
    save.mutate({ events: next });
  };

  const onTest = async () => {
    setTestResult(null);
    const r = await test.mutateAsync();
    setTestResult({ ok: r.ok, code: r.status_code, ms: r.latency_ms });
  };

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb="机器人通知"
        title="机器人通知"
        desc="事件推到你的 webhook · 拉号完成、号失效、余额不足都能收"
        right={
          <div className="flex items-center gap-2">
            {cfg?.enabled ? <Chip tone="ok" dot>启用中</Chip> : <Chip tone="neutral" dot>已停用</Chip>}
            <Switch
              checked={cfg?.enabled ?? false}
              disabled={!cfg || save.isPending}
              onCheckedChange={(v) => save.mutate({ enabled: v })}
              aria-label="启用 webhook"
            />
          </div>
        }
      />

      {/* 端点卡 · focal */}
      <Card focal focalTone="brand" className="p-7">
        <SectionHead
          title="Webhook 端点"
          sub="我方 POST 事件到这个地址 · 请求带 HMAC 签名，用下面的密钥验"
        />

        <div className="mt-4 space-y-4">
          <Field label="接收地址" hint="https://…">
            <Input
              value={url}
              onChange={(e) => { setUrl(e.target.value); setTestResult(null); }}
              placeholder="https://bot.example.com/kiro-events"
              className="font-mono"
            />
          </Field>

          <div className="space-y-1.5">
            <div className="flex flex-wrap items-baseline gap-x-3">
              <span className="text-label font-semibold text-fg-secondary">签名密钥</span>
              <span className="text-label text-fg-tertiary">
                {newSecret ? "这是唯一一次可见，请立即复制" : "只存打码版 · 要换就重新生成"}
              </span>
            </div>
            <SecretField masked={cfg?.secret_masked ?? "-"} plaintext={newSecret} />
          </div>

          {newSecret && (
            <Alert tone="warn" icon={AlertTriangle} title="旧密钥已失效">
              记得同步更新你机器人那边的验签配置，否则收到的事件会验签失败
            </Alert>
          )}

          {testResult && (
            <Alert
              tone={testResult.ok ? "ok" : "danger"}
              icon={testResult.ok ? CheckCircle2 : X}
              title={testResult.ok ? "测试事件已送达" : "送不到"}
            >
              HTTP <Em>{testResult.code}</Em> · 耗时 <Em>{testResult.ms}</Em> ms
            </Alert>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="brand"
              onClick={() => save.mutate({ url: url.trim() })}
              disabled={!dirty || save.isPending}
            >
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              {save.isPending ? "保存中…" : "保存"}
            </Button>
            <Button variant="ghost" onClick={onTest} disabled={!url.trim() || test.isPending}>
              {test.isPending ? <Loader2 className="animate-spin" /> : <Zap />}
              {test.isPending ? "发送中…" : "发测试事件"}
            </Button>
            <Button
              variant="ghost"
              onClick={async () => {
                const r = await regen.mutateAsync();
                setNewSecret(r.secret);
              }}
              disabled={regen.isPending}
              title="旧密钥立即失效"
            >
              {regen.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              重新生成密钥
            </Button>
            {dirty && <span className="text-label text-fg-tertiary">有未保存的修改</span>}
          </div>
        </div>
      </Card>

      {/* 订阅事件 */}
      <Card className="p-7">
        <SectionHead
          title="订阅事件"
          sub={<>已订阅 <Em>{subscribed.size}</Em> / {EVENTS.length} 个</>}
        />

        <div className="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-2">
          {EVENTS.map((ev) => {
            const on = subscribed.has(ev.id);
            return (
              <div
                key={ev.id}
                className="flex items-start justify-between gap-3 rounded-xl border border-hairline bg-bg-elevated/40 p-3.5"
              >
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-semibold">{ev.title}</span>
                    {/* event id 是对外契约 · 用户代码里要用，所以露出来 */}
                    <code className="rounded bg-bg-elevated px-1.5 py-px font-mono text-[10px] font-medium text-fg-tertiary">
                      {ev.id}
                    </code>
                  </div>
                  <p className="text-label text-fg-tertiary">{ev.desc}</p>
                </div>
                <Switch
                  className="mt-0.5 shrink-0"
                  checked={on}
                  disabled={!cfg || save.isPending}
                  onCheckedChange={(v) => toggleEvent(ev.id, v)}
                  aria-label={ev.title}
                />
              </div>
            );
          })}
        </div>
      </Card>

      <DeliveriesCard items={deliveries ?? []} />
    </div>
  );
}

/* ─────────────── 投递记录 ─────────────── */

type DFilter = "all" | "ok" | "failed";

const DFILTERS: { value: DFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "ok", label: "成功" },
  { value: "failed", label: "失败" },
];

function DeliveriesCard({ items }: { items: WebhookDelivery[] }) {
  const [filter, setFilter] = useState<DFilter>("all");

  const shown = items.filter((d) =>
    filter === "all" ? true : filter === "ok" ? d.ok : !d.ok,
  );
  const failed = items.filter((d) => !d.ok).length;

  return (
    <Card className="p-7">
      <SectionHead
        title="投递记录"
        sub={
          <>
            共 <Em>{items.length}</Em> 条 ·
            成功 <Em tone="ok">{items.length - failed}</Em> ·
            失败 <Em tone="spend">{failed}</Em>
          </>
        }
        right={<Segmented options={DFILTERS} value={filter} onChange={setFilter} />}
      />

      {shown.length === 0 ? (
        <div className="grid place-items-center gap-3 py-12 text-center">
          <span className="grid size-10 place-items-center rounded-full bg-bg-elevated">
            <Send className="size-4 text-fg-tertiary" />
          </span>
          <p className="text-label text-fg-tertiary">
            {items.length === 0 ? "还没有投递记录 · 发个测试事件试试" : "这个筛选下没有记录"}
          </p>
        </div>
      ) : (
        <div className="mt-4 overflow-x-auto">
          <div className="min-w-[620px]">
            <BareHead>
              <span className="w-[92px] shrink-0">时间</span>
              <span className="w-20 shrink-0">结果</span>
              <span className="min-w-0 flex-1">事件</span>
              <span className="w-16 shrink-0 text-right">HTTP</span>
              <span className="w-16 shrink-0 text-right">重试</span>
              <span className="w-20 shrink-0 text-right">耗时</span>
            </BareHead>
            <BareList>
              {shown.map((d) => (
                <BareRow key={d.id}>
                  <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
                    {fmtTime(d.created_at)}
                  </span>
                  <span className="w-20 shrink-0">
                    {d.ok
                      ? <Chip tone="ok" icon={<Check className="size-3" />}>成功</Chip>
                      : <Chip tone="danger" icon={<X className="size-3" />}>失败</Chip>}
                  </span>
                  <span className="min-w-0 flex-1 truncate font-mono text-label text-fg-secondary">
                    {d.event}
                  </span>
                  <span className="w-16 shrink-0 text-right">
                    {d.status_code
                      ? <Em tone={d.ok ? undefined : "spend"}>{d.status_code}</Em>
                      : <span className="text-label text-fg-tertiary">-</span>}
                  </span>
                  <span className="w-16 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
                    {d.attempt > 1 ? `${d.attempt} 次` : "-"}
                  </span>
                  <span className="w-20 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
                    {d.latency_ms} ms
                  </span>
                </BareRow>
              ))}
            </BareList>
          </div>
        </div>
      )}

      {failed > 0 && (
        <Alert tone="neutral" icon={Bot} className="mt-4">
          失败会自动退避重试 3 次（5s / 30s / 5min）· 一直失败先检查你那边能不能被公网访问
        </Alert>
      )}
    </Card>
  );
}
