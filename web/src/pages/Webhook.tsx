import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
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
import type { WebhookDelivery, WebhookEvent } from "@/types";

/** 可订阅事件 · 4 个 · 对齐 docs/05-api-contract §11(1e-2 定稿)
 *  event id 是对外契约的一部分（用户要在自己代码里 switch），所以这里**故意露出原名**
 *  —— 技术页例外，跟对接文档同性质（CLAUDE.md §12.6） */
const EVENT_IDS = [
  "new_keys_available",
  "all_keys_dead",
  "warranty_refund",
  "boarded",
] as const;

const EVENT_KEY: Record<(typeof EVENT_IDS)[number], string> = {
  "new_keys_available": "new-keys-available",
  "all_keys_dead":      "all-keys-dead",
  "warranty_refund":    "warranty-refund",
  "boarded":            "boarded",
};

export default function Webhook() {
  const { t } = useTranslation("settings");
  const { data: cfg } = useWebhook();
  const { data: deliveries } = useWebhookDeliveries();
  const save = useSaveWebhook();
  const test = useTestWebhook();
  const regen = useRegenWebhookSecret();

  const [url, setUrl] = useState("");
  const [newSecret, setNewSecret] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; code: number; ms: number; error?: string } | null>(null);

  useEffect(() => {
    if (cfg && !url) setUrl(cfg.url);
  }, [cfg, url]);

  const dirty = !!cfg && url !== cfg.url;
  const subscribed = new Set(cfg?.events ?? []);

  const events = EVENT_IDS.map((id) => ({
    id,
    title: t(`webhook.event.${EVENT_KEY[id]}.title`),
    desc: t(`webhook.event.${EVENT_KEY[id]}.desc`),
  }));

  const toggleEvent = (id: WebhookEvent, on: boolean) => {
    if (!cfg) return;
    const next = on
      ? [...cfg.events, id]
      : cfg.events.filter((e) => e !== id);
    save.mutate({ events: next });
  };

  const onTest = async () => {
    setTestResult(null);
    const r = await test.mutateAsync();
    setTestResult({ ok: r.ok, code: r.status_code, ms: r.latency_ms, error: r.error });
  };

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb={t("webhook.head.crumb")}
        title={t("webhook.head.title")}
        desc={t("webhook.head.desc")}
        right={
          <div className="flex items-center gap-2">
            {cfg?.enabled ? <Chip tone="ok" dot>{t("webhook.state.enabled")}</Chip> : <Chip tone="neutral" dot>{t("webhook.state.disabled")}</Chip>}
            <Switch
              checked={cfg?.enabled ?? false}
              disabled={!cfg || save.isPending}
              onCheckedChange={(v) => save.mutate({ enabled: v })}
              aria-label={t("webhook.switch.aria")}
            />
          </div>
        }
      />

      {/* 端点卡 · focal */}
      <Card focal focalTone="brand" className="p-7">
        <SectionHead
          title={t("webhook.endpoint.title")}
          sub={t("webhook.endpoint.sub")}
        />

        <div className="mt-4 space-y-4">
          <Field label={t("webhook.endpoint.url-label")} hint={t("webhook.endpoint.url-hint")}>
            <Input
              value={url}
              onChange={(e) => { setUrl(e.target.value); setTestResult(null); }}
              placeholder={t("webhook.endpoint.url-placeholder")}
              className="font-mono"
            />
          </Field>

          <div className="space-y-1.5">
            <div className="flex flex-wrap items-baseline gap-x-3">
              <span className="text-label font-semibold text-fg-secondary">{t("webhook.endpoint.secret-label")}</span>
              <span className="text-label text-fg-tertiary">
                {newSecret
                  ? t("webhook.endpoint.secret-hint-visible")
                  : cfg?.secret_masked
                    ? t("webhook.endpoint.secret-hint-masked")
                    : t("webhook.endpoint.secret-hint-empty")}
              </span>
            </div>
            <SecretField masked={cfg?.secret_masked} plaintext={newSecret} />
          </div>

          {newSecret && (
            <Alert tone="warn" icon={AlertTriangle} title={t("webhook.endpoint.old-secret-invalid.title")}>
              {t("webhook.endpoint.old-secret-invalid.body")}
            </Alert>
          )}

          {testResult && (
            <Alert
              tone={testResult.ok ? "ok" : "danger"}
              icon={testResult.ok ? CheckCircle2 : X}
              title={testResult.ok ? t("webhook.endpoint.test-ok") : t("webhook.endpoint.test-fail")}
            >
              {testResult.ok || !testResult.error
                ? <>{t("webhook.endpoint.test-detail-prefix")}<Em>{testResult.code}</Em>{t("webhook.endpoint.test-detail-middle")}<Em>{testResult.ms}</Em>{t("webhook.endpoint.test-detail-suffix")}</>
                : <>{testResult.error}{testResult.ms > 0 && <> · <Em>{testResult.ms}</Em> ms</>}</>}
            </Alert>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="brand"
              onClick={() => save.mutate({ url: url.trim() })}
              disabled={!dirty || save.isPending}
            >
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              {save.isPending ? t("webhook.action.saving") : t("webhook.action.save")}
            </Button>
            <Button variant="ghost" onClick={onTest} disabled={!url.trim() || test.isPending}>
              {test.isPending ? <Loader2 className="animate-spin" /> : <Zap />}
              {test.isPending ? t("webhook.action.sending") : t("webhook.action.send-test")}
            </Button>
            <Button
              variant="ghost"
              onClick={async () => {
                const r = await regen.mutateAsync();
                setNewSecret(r.secret);
              }}
              disabled={regen.isPending}
              title={t("webhook.action.regen-title")}
            >
              {regen.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              {t("webhook.action.regen")}
            </Button>
            {dirty && <span className="text-label text-fg-tertiary">{t("webhook.unsaved")}</span>}
          </div>
        </div>
      </Card>

      {/* 订阅事件 */}
      <Card className="p-7">
        <SectionHead
          title={t("webhook.subs.title")}
          sub={<>{t("webhook.subs.subscribed-prefix")}<Em>{subscribed.size}</Em>{t("webhook.subs.sub-separator")}{events.length}{t("webhook.subs.sub-suffix")}</>}
        />

        <div className="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-2">
          {events.map((ev) => {
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

const DFILTER_VALUES: DFilter[] = ["all", "ok", "failed"];

function DeliveriesCard({ items }: { items: WebhookDelivery[] }) {
  const { t } = useTranslation("settings");
  const [filter, setFilter] = useState<DFilter>("all");
  const dfilters = DFILTER_VALUES.map((value) => ({
    value,
    label: t(`webhook.deliveries.filter.${value}`),
  }));

  const shown = items.filter((d) =>
    filter === "all" ? true : filter === "ok" ? d.ok : !d.ok,
  );
  const failed = items.filter((d) => !d.ok).length;

  return (
    <Card className="p-7">
      <SectionHead
        title={t("webhook.deliveries.title")}
        sub={
          <>
            {t("webhook.deliveries.sub-total-prefix")}<Em>{items.length}</Em>{t("webhook.deliveries.sub-total-middle")}<Em tone="ok">{items.length - failed}</Em>{t("webhook.deliveries.sub-total-fail")}<Em tone="spend">{failed}</Em>
          </>
        }
        right={<Segmented options={dfilters} value={filter} onChange={setFilter} />}
      />

      {shown.length === 0 ? (
        <div className="grid place-items-center gap-3 py-12 text-center">
          <span className="grid size-10 place-items-center rounded-full bg-bg-elevated">
            <Send className="size-4 text-fg-tertiary" />
          </span>
          <p className="text-label text-fg-tertiary">
            {items.length === 0 ? t("webhook.deliveries.empty-none") : t("webhook.deliveries.empty-filter")}
          </p>
        </div>
      ) : (
        <div className="mt-4 overflow-x-auto">
          <div className="min-w-[620px]">
            <BareHead>
              <span className="w-[92px] shrink-0">{t("webhook.deliveries.col.time")}</span>
              <span className="w-20 shrink-0">{t("webhook.deliveries.col.result")}</span>
              <span className="min-w-0 flex-1">{t("webhook.deliveries.col.event")}</span>
              <span className="w-16 shrink-0 text-right">{t("webhook.deliveries.col.http")}</span>
              <span className="w-16 shrink-0 text-right">{t("webhook.deliveries.col.retry")}</span>
              <span className="w-20 shrink-0 text-right">{t("webhook.deliveries.col.latency")}</span>
            </BareHead>
            <BareList>
              {shown.map((d) => (
                <BareRow key={d.id}>
                  <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
                    {fmtTime(d.created_at)}
                  </span>
                  <span className="w-20 shrink-0">
                    {d.ok
                      ? <Chip tone="ok" icon={<Check className="size-3" />}>{t("webhook.deliveries.chip.ok")}</Chip>
                      : <Chip tone="danger" icon={<X className="size-3" />}>{t("webhook.deliveries.chip.failed")}</Chip>}
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
                    {d.attempt > 1 ? t("webhook.deliveries.retry-count", { count: d.attempt }) : "-"}
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
          {t("webhook.deliveries.retry-note")}
        </Alert>
      )}
    </Card>
  );
}
