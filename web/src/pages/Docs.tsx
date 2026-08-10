import { useState } from "react";
import type { ReactNode } from "react";
import { KeyRound, ShieldCheck, Terminal, Webhook } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CodeBlock } from "@/components/ui/code-block";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

/* 这是技术页 · 允许出现内部术语（credential / housepool 之类）
   —— 跟设置里的号池页同性质（CLAUDE.md §12.6 技术页例外） */

type Section = "start" | "pull" | "assign" | "webhook" | "errors";

export default function Docs() {
  const { t } = useTranslation("docs");
  const [sec, setSec] = useState<Section>("start");
  const NAV: { id: Section; label: string }[] = [
    { id: "start", label: t("nav.start") },
    { id: "pull", label: t("nav.pull") },
    { id: "assign", label: t("nav.assign") },
    { id: "webhook", label: t("nav.webhook") },
    { id: "errors", label: t("nav.errors") },
  ];

  return (
    <div className="space-y-section">
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">{t("hero.title")}</h1>
          <p className="text-fg-tertiary">
            {t("hero.desc")}
          </p>
        </div>
        <Button variant="ghost" asChild>
          <Link to="/settings/api-keys">
            <KeyRound />
            {t("hero.manage-key")}
          </Link>
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[180px_minmax(0,1fr)]">
        {/* 侧边导航 · 移动端横向滚动 */}
        <nav className="-mx-1 flex gap-1 overflow-x-auto px-1 lg:sticky lg:top-24 lg:mx-0 lg:h-fit lg:flex-col lg:px-0">
          {NAV.map((n) => (
            <button
              key={n.id}
              type="button"
              onClick={() => setSec(n.id)}
              className={cn(
                "shrink-0 rounded-xl px-3 py-2 text-left text-label font-medium transition-colors",
                sec === n.id
                  ? "bg-brand-subtle font-semibold text-brand-strong"
                  : "text-fg-tertiary hover:bg-bg-elevated hover:text-fg-secondary",
              )}
            >
              {n.label}
            </button>
          ))}
        </nav>

        <div className="min-w-0 space-y-6">
          {sec === "start" && <StartSection />}
          {sec === "pull" && <PullSection />}
          {sec === "assign" && <AssignSection />}
          {sec === "webhook" && <WebhookSection />}
          {sec === "errors" && <ErrorsSection />}
        </div>
      </div>
    </div>
  );
}

/* ─────────────── 开始 ─────────────── */

function StartSection() {
  const { t } = useTranslation("docs");
  return (
    <>
      <Card className="p-7">
        <SectionHead title={t("start.auth.title")} sub={t("start.auth.sub")} />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="bash"
            code={t("start.auth.code")}
          />
          <Alert tone="warn" icon={ShieldCheck} title={t("start.auth.alert.title")}>
            {t("start.auth.alert.body-1")}<code className="font-mono">/api/me/*</code>{t("start.auth.alert.body-2")}
          </Alert>
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title={t("start.convention.title")} sub={t("start.convention.sub")} />
        <div className="mt-4 space-y-3">
          <ConventionRow label={t("start.convention.money.label")} value={<>{t("start.convention.money.value")} <Em>1_000_000</Em></>} />
          <ConventionRow label={t("start.convention.time.label")} value={t("start.convention.time.value")} />
          <ConventionRow
            label={t("start.convention.page.label")}
            value={<code className="font-mono text-label">?page=1&page_size=50</code>}
          />
          <ConventionRow
            label={t("start.convention.error.label")}
            value={<>{t("start.convention.error.value-prefix")}<code className="font-mono">code</code>{t("start.convention.error.value-suffix")}</>}
          />
          <ConventionRow
            label={t("start.convention.idempotency.label")}
            value={
              <>
                {t("start.convention.idempotency.value-1")}<Em plain>{t("start.convention.idempotency.value-em")}</Em>{t("start.convention.idempotency.value-2")}{" "}
                <code className="font-mono text-label">X-Idempotency-Key</code>{t("start.convention.idempotency.value-3")}
              </>
            }
          />
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title={t("start.idempotency.title")} sub={t("start.idempotency.sub")} />
        <div className="mt-4">
          <CodeBlock
            lang="bash"
            code={t("start.idempotency.code")}
          />
        </div>
        <p className="mt-3 text-label text-fg-tertiary">{t("start.idempotency.note")}</p>
      </Card>
    </>
  );
}

function ConventionRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 border-b border-hairline pb-3 last:border-0 last:pb-0 sm:flex-row sm:items-baseline sm:gap-4">
      <span className="w-24 shrink-0 text-label font-semibold text-fg-secondary">{label}</span>
      <span className="min-w-0 text-label text-fg-secondary">{value}</span>
    </div>
  );
}

/* ─────────────── 拉号 ─────────────── */

function PullSection() {
  const { t } = useTranslation("docs");
  return (
    <>
      <Card className="p-7">
        <SectionHead
          title={t("pull.bus.title")}
          sub={<><code className="font-mono">POST /api/me/buses/{"{bus_id}"}/pull</code></>}
        />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="json"
            code={t("pull.bus.request-code")}
          />
          <CodeBlock
            lang="json"
            code={t("pull.bus.response-code")}
          />
          <Alert tone="neutral" icon={Terminal}>
            {t("pull.bus.alert")}
          </Alert>
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead
          title={t("pull.solo.title")}
          sub={<><code className="font-mono">POST /api/me/pull</code>{t("pull.solo.sub-suffix")}</>}
        />
        <div className="mt-4">
          <CodeBlock lang="bash" code={t("pull.solo.code")} />
        </div>
      </Card>
    </>
  );
}

/* ─────────────── 派去向 ─────────────── */

function AssignSection() {
  const { t } = useTranslation("docs");
  return (
    <>
      <Card className="p-7">
        <SectionHead
          title={t("assign.section.title")}
          sub={<><code className="font-mono">POST /api/me/pull-records/assign</code>{t("assign.section.sub-suffix")}</>}
        />
        <div className="mt-4 space-y-3">
          <div className="divide-y divide-hairline">
            <DestRow
              code="into_bus"
              title={t("assign.dest.into-bus.title")}
              desc={t("assign.dest.into-bus.desc")}
            />
            <DestRow
              code="push_pool"
              title={t("assign.dest.push-pool.title")}
              desc={t("assign.dest.push-pool.desc")}
            />
            <DestRow
              code="handoff"
              title={t("assign.dest.handoff.title")}
              desc={t("assign.dest.handoff.desc")}
            />
          </div>
          <CodeBlock
            lang="json"
            code={t("assign.request-code")}
          />
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title={t("assign.handoff.title")} sub={t("assign.handoff.sub")} />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="bash"
            code={t("assign.handoff.code")}
          />
          <Alert tone="warn" icon={ShieldCheck} title={t("assign.handoff.alert.title")}>
            {t("assign.handoff.alert.body")}
          </Alert>
        </div>
      </Card>
    </>
  );
}

function DestRow({ code, title, desc }: { code: string; title: string; desc: string }) {
  return (
    <div className="flex flex-col gap-1 py-3 sm:flex-row sm:items-baseline sm:gap-4">
      <code className="w-28 shrink-0 font-mono text-label font-semibold text-brand-strong">
        {code}
      </code>
      <div className="min-w-0">
        <div className="text-label font-semibold">{title}</div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>
    </div>
  );
}

/* ─────────────── Webhook ─────────────── */

function WebhookSection() {
  const { t } = useTranslation("docs");
  return (
    <>
      <Card className="p-7">
        <SectionHead title={t("webhook.event.title")} sub={t("webhook.event.sub")} />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="json"
            code={t("webhook.event.code")}
          />
          <p className="text-label text-fg-tertiary">
            {t("webhook.event.link-prefix")}<Link to="/settings/webhook" className="font-semibold text-brand-strong hover:underline">{t("webhook.event.link")}</Link>{t("webhook.event.link-suffix")}
          </p>
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title={t("webhook.verify.title")} sub={t("webhook.verify.sub")} />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="js"
            code={t("webhook.verify.code")}
          />
          <Alert tone="danger" icon={Webhook} title={t("webhook.verify.alert.title")}>
            {t("webhook.verify.alert.body-1")}<code className="font-mono">timingSafeEqual</code>{t("webhook.verify.alert.body-2")}
          </Alert>
        </div>
      </Card>
    </>
  );
}

/* ─────────────── 错误码 ─────────────── */

function ErrorsSection() {
  const { t } = useTranslation("docs");
  const ERRORS: { http: number; code: string; meaning: string }[] = [
    { http: 400, code: "bad_json", meaning: t("errors.item.bad-json") },
    { http: 400, code: "bad_count", meaning: t("errors.item.bad-count") },
    { http: 400, code: "bad_vendor", meaning: t("errors.item.bad-vendor") },
    { http: 400, code: "bad_bus_id", meaning: t("errors.item.bad-bus-id") },
    { http: 401, code: "invalid_api_key", meaning: t("errors.item.invalid-api-key") },
    { http: 402, code: "insufficient_balance", meaning: t("errors.item.insufficient-balance") },
    { http: 403, code: "session_required", meaning: t("errors.item.session-required") },
    { http: 404, code: "not_found", meaning: t("errors.item.not-found") },
    { http: 409, code: "no_stock", meaning: t("errors.item.no-stock") },
    { http: 409, code: "idempotency_conflict", meaning: t("errors.item.idempotency-conflict") },
    { http: 429, code: "rate_limited", meaning: t("errors.item.rate-limited") },
    { http: 502, code: "vendor_error", meaning: t("errors.item.vendor-error") },
    { http: 503, code: "housepool_unavailable", meaning: t("errors.item.housepool-unavailable") },
    { http: 500, code: "internal", meaning: t("errors.item.internal") },
  ];
  return (
    <Card className="p-7">
      <SectionHead
        title={t("errors.title")}
        sub={<>{t("errors.sub-prefix")}<code className="font-mono">code</code>{t("errors.sub-suffix")}</>}
      />

      <div className="mt-4 overflow-x-auto">
        <div className="min-w-[520px]">
          <BareHead>
            <span className="w-14 shrink-0">{t("errors.header.http")}</span>
            <span className="w-[200px] shrink-0">{t("errors.header.code")}</span>
            <span className="min-w-0 flex-1">{t("errors.header.meaning")}</span>
          </BareHead>
          <BareList>
            {ERRORS.map((e) => (
              <BareRow key={e.code}>
                <span className="w-14 shrink-0">
                  <Chip tone={e.http >= 500 ? "danger" : e.http >= 400 ? "warn" : "neutral"}>
                    {e.http}
                  </Chip>
                </span>
                <code className="w-[200px] shrink-0 truncate font-mono text-label font-semibold text-fg-secondary">
                  {e.code}
                </code>
                <span className="min-w-0 flex-1 text-label text-fg-tertiary">{e.meaning}</span>
              </BareRow>
            ))}
          </BareList>
        </div>
      </div>

      <Alert tone="neutral" icon={Terminal} className="mt-4">
        <code className="font-mono">429</code>{t("errors.alert.part-1")}<code className="font-mono">502/503</code>{t("errors.alert.part-2")}<code className="font-mono">402</code>{t("errors.alert.part-3")}<code className="font-mono">4xx</code>{t("errors.alert.part-4")}
      </Alert>
    </Card>
  );
}
