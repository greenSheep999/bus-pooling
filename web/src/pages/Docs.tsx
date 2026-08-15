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

type Section = "start" | "pull" | "assign" | "matrix" | "fields" | "webhook" | "errors";

export default function Docs() {
  const { t } = useTranslation("docs");
  const [sec, setSec] = useState<Section>("start");
  const NAV: { id: Section; label: string }[] = [
    { id: "start", label: t("nav.start") },
    { id: "pull", label: t("nav.pull") },
    { id: "assign", label: t("nav.assign") },
    { id: "matrix", label: t("nav.matrix") },
    { id: "fields", label: t("nav.fields") },
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
          {sec === "matrix" && <MatrixSection />}
          {sec === "fields" && <FieldsSection />}
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
        <SectionHead title={t("start.api-keys.title")} sub={t("start.api-keys.sub")} />
        <div className="mt-4 space-y-3">
          <CodeBlock lang="bash" code={t("start.api-keys.code")} />
          <Alert tone="neutral" icon={KeyRound} title={t("start.api-keys.alert.title")}>
            {t("start.api-keys.alert.body")}
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

/* ─────────────── 端点矩阵 ─────────────── */

// EndpointRow · 单端点一行 · null = 打 "—" · true = 打 "✓"
type EndpointRow = {
  method: "GET" | "POST" | "PUT" | "DELETE";
  path: string;
  authSession: boolean;
  authKey: boolean;
  idempotency: boolean;
  paginated: boolean;
  oneShot: boolean;
};

type EndpointGroup = { label: string; rows: EndpointRow[] };

// MATRIX · 覆盖 1f-E 任务清单里点名要覆盖的端点(至少覆盖) · 字段跟后端 handler 对齐 ·
// 别造 · 别照抄 md 文档(md 可能漂移 · 以真 handler 为准)。
const MATRIX: (t: (k: string) => string) => EndpointGroup[] = (t) => [
  {
    label: t("matrix.sec.account"),
    rows: [
      // API key(§2 · session-only 创建 · list/revoke 两种鉴权都行)
      { method: "GET",    path: "/api/me/api-keys",           authSession: true, authKey: true,  idempotency: false, paginated: false, oneShot: false },
      { method: "POST",   path: "/api/me/api-keys",           authSession: true, authKey: false, idempotency: false, paginated: false, oneShot: true  },
      { method: "DELETE", path: "/api/me/api-keys/{id}",      authSession: true, authKey: true,  idempotency: false, paginated: false, oneShot: false },
      { method: "POST",   path: "/api/me/password",           authSession: true, authKey: false, idempotency: false, paginated: false, oneShot: false },
    ],
  },
  {
    label: t("matrix.sec.pull"),
    rows: [
      // 拉号 · 单独 + 车级 · 都要幂等
      { method: "POST",   path: "/api/me/pull",                          authSession: true, authKey: true, idempotency: true,  paginated: false, oneShot: false },
      { method: "POST",   path: "/api/me/buses/{bus_id}/pull",           authSession: true, authKey: true, idempotency: true,  paginated: false, oneShot: false },
      // 派去向 · 要幂等
      { method: "POST",   path: "/api/me/pull-records/assign",           authSession: true, authKey: true, idempotency: true,  paginated: false, oneShot: false },
      // 拉号记录 · 只读 · 分页
      { method: "GET",    path: "/api/me/pull-records",                  authSession: true, authKey: true, idempotency: false, paginated: true,  oneShot: false },
      { method: "GET",    path: "/api/me/pull-records/{record_id}",      authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/me/pull/events",                   authSession: true, authKey: true, idempotency: false, paginated: true,  oneShot: false },
      { method: "GET",    path: "/api/me/assign/events",                 authSession: true, authKey: true, idempotency: false, paginated: true,  oneShot: false },
    ],
  },
  {
    label: t("matrix.sec.bus"),
    rows: [
      { method: "GET",    path: "/api/me/buses",                         authSession: true, authKey: true, idempotency: false, paginated: true,  oneShot: false },
      { method: "POST",   path: "/api/me/buses",                         authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/me/buses/{bus_id}",                authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "PUT",    path: "/api/me/buses/{bus_id}/strategy",       authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/me/buses/{bus_id}/credentials",    authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/me/buses/{bus_id}/pulls",          authSession: true, authKey: true, idempotency: false, paginated: true,  oneShot: false },
    ],
  },
  {
    label: t("matrix.sec.handoff"),
    rows: [
      // 拿走三段式 · 明文只在 GET fulfill 出现 · 打 one-shot
      { method: "POST",   path: "/api/me/handoff",                       authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/me/handoff/{token}",               authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: true  },
      { method: "POST",   path: "/api/me/handoff/{token}/confirm",       authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
    ],
  },
  {
    label: t("matrix.sec.strategy"),
    rows: [
      { method: "GET",    path: "/api/me/strategy",                      authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "PUT",    path: "/api/me/strategy",                      authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
    ],
  },
  {
    label: t("matrix.sec.downstream"),
    rows: [
      { method: "GET",    path: "/api/me/downstream",                    authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "PUT",    path: "/api/me/downstream/passengerpool",      authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "POST",   path: "/api/me/downstream/passengerpool/test", authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/me/downstream/webhook",            authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "PUT",    path: "/api/me/downstream/webhook",            authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      // 轮换 secret · 明文一次性返回
      { method: "POST",   path: "/api/me/downstream/webhook/secret",     authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: true  },
      { method: "POST",   path: "/api/me/downstream/webhook/test",       authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
    ],
  },
  {
    label: t("matrix.sec.vendors"),
    rows: [
      // /api/vendors/* 都要鉴权(定价按调用者身份 · 05 §9 说明)
      { method: "GET",    path: "/api/vendors/stock",                    authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/vendors/{vendor_id}/stock",        authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/vendors/stats",                    authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/vendors/auto-pick",                authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
      { method: "GET",    path: "/api/vendors/prices",                   authSession: true, authKey: true, idempotency: false, paginated: false, oneShot: false },
    ],
  },
];

function MatrixSection() {
  const { t } = useTranslation("docs");
  const groups = MATRIX(t);
  return (
    <Card className="p-7">
      <SectionHead title={t("matrix.hero-title")} sub={t("matrix.hero-sub")} />

      <div className="mt-4 space-y-6">
        {groups.map((g) => (
          <div key={g.label}>
            <div className="mb-2 text-label font-semibold text-fg-secondary">{g.label}</div>
            <div className="overflow-x-auto">
              <div className="min-w-[680px]">
                <BareHead>
                  <span className="min-w-0 flex-1">{t("matrix.header.endpoint")}</span>
                  <span className="w-32 shrink-0 text-center">{t("matrix.header.auth")}</span>
                  <span className="w-16 shrink-0 text-center">{t("matrix.header.idempotency")}</span>
                  <span className="w-16 shrink-0 text-center">{t("matrix.header.paginated")}</span>
                  <span className="w-20 shrink-0 text-center">{t("matrix.header.one-shot")}</span>
                </BareHead>
                <BareList>
                  {g.rows.map((row) => (
                    <BareRow key={row.method + " " + row.path}>
                      <span className="min-w-0 flex-1 truncate">
                        <MethodChip method={row.method} />
                        <code className="ml-2 font-mono text-label text-fg-secondary">{row.path}</code>
                      </span>
                      <span className="w-32 shrink-0 text-center">
                        <AuthBadges session={row.authSession} apiKey={row.authKey} />
                      </span>
                      <span className="w-16 shrink-0 text-center">
                        <MarkCell on={row.idempotency} />
                      </span>
                      <span className="w-16 shrink-0 text-center">
                        <MarkCell on={row.paginated} />
                      </span>
                      <span className="w-20 shrink-0 text-center">
                        <MarkCell on={row.oneShot} tone="warn" />
                      </span>
                    </BareRow>
                  ))}
                </BareList>
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className="mt-6 space-y-2 border-t border-hairline pt-4 text-label text-fg-tertiary">
        <p><NoteMd text={t("matrix.footnote-1")} /></p>
        <p><NoteMd text={t("matrix.footnote-2")} /></p>
        <p><NoteMd text={t("matrix.footnote-3")} /></p>
        <p><NoteMd text={t("matrix.footnote-4")} /></p>
      </div>
    </Card>
  );
}

// MethodChip · HTTP method 色卡 · GET 蓝 / POST 绿 / PUT 黄 / DELETE 红
function MethodChip({ method }: { method: EndpointRow["method"] }) {
  const tone: "brand" | "ok" | "warn" | "danger" =
    method === "GET" ? "brand"
      : method === "POST" ? "ok"
      : method === "PUT" ? "warn"
      : "danger";
  return (
    <Chip tone={tone}>
      <span className="font-mono font-semibold">{method}</span>
    </Chip>
  );
}

// AuthBadges · session / API key 两种 · 都支持就都显示 · session-only 显示单一
function AuthBadges({ session, apiKey }: { session: boolean; apiKey: boolean }) {
  return (
    <span className="inline-flex gap-1">
      {session && <Chip tone="neutral">session</Chip>}
      {apiKey && <Chip tone="brand">API key</Chip>}
    </span>
  );
}

// MarkCell · ✓ / — 单元格
function MarkCell({ on, tone = "brand" }: { on: boolean; tone?: "brand" | "warn" }) {
  if (!on) return <span className="text-fg-tertiary">—</span>;
  return (
    <span className={cn(
      "font-semibold",
      tone === "warn" ? "text-warn-fg" : "text-brand-strong",
    )}>
      ✓
    </span>
  );
}

// NoteMd · 极简 markdown 加粗 · **x** → <strong>x</strong> · 只处理这一层
function NoteMd({ text }: { text: string }) {
  const parts = text.split(/\*\*(.+?)\*\*/g);
  return (
    <>
      {parts.map((p, i) => (i % 2 === 0
        ? <span key={i}>{p}</span>
        : <strong key={i} className="font-semibold text-fg-secondary">{p}</strong>
      ))}
    </>
  );
}

/* ─────────────── 字段全表 ─────────────── */

// FieldRow · 请求 / 响应字段一行 · code=字段名(json tag) · type=TS 或 Go 类型友好版
// · required=必填标记 · notes=简短说明(含错误码 / 默认值等)
type FieldRow = {
  code: string;
  type: string;
  required: boolean | "n/a";
  notes: string;
};

type FieldGroup = { titleKey: string; subKey: string; rows: FieldRow[] };

// FIELD_GROUPS · 字段来自真后端 struct(json tag) · CLAUDE.md §0 TS 是可执行契约。
// 别造 · 别参照旧 md · 每行对应 handler / DTO / TS 里能 grep 到的实名。
const FIELD_GROUPS: FieldGroup[] = [
  // ── 拉号 ─────────────────────────────────────
  {
    titleKey: "fields.pull-req.title",
    subKey: "fields.pull-req.sub",
    rows: [
      { code: "count",       type: "int",    required: true,  notes: "≥1 · 服务端还会按你的 daily_round_limit 拦" },
      { code: "vendor_id",   type: "string", required: false, notes: "空 / \"auto\" = 让系统挑 · 传值必须是已装配的 vendor · 否则 400 bad_vendor" },
      { code: "zone",        type: "string", required: false, notes: "us | eu | 空 / auto = 让系统挑" },
      { code: "coupon_code", type: "string", required: false, notes: "阶段 1a 保留字段 · 后端接收但不消费(防 decodeStrict 拒未知字段)" },
    ],
  },
  {
    titleKey: "fields.bus-pull-req.title",
    subKey: "fields.bus-pull-req.sub",
    rows: [
      { code: "count",       type: "int",    required: true,  notes: "≥1" },
      { code: "vendor_id",   type: "string", required: false, notes: "同 solo pull · 车级 preferred_vendor 可覆盖" },
      { code: "zone",        type: "string", required: false, notes: "同 solo pull" },
      { code: "coupon_code", type: "string", required: false, notes: "同 solo pull · 保留字段" },
    ],
  },
  {
    titleKey: "fields.pull-resp.title",
    subKey: "fields.pull-resp.sub",
    rows: [
      { code: "pull_round_id",     type: "string",   required: true, notes: "本轮唯一 id(UUID v7) · 用于对账 / 售后" },
      { code: "vendor_id",         type: "string",   required: true, notes: "实际拉的 vendor · 可能跟请求不同(auto 或服务端否决)" },
      { code: "purchased",         type: "int",      required: true, notes: "实际成功进池数 · 可能小于 count(部分成功)" },
      { code: "credential_ids",    type: "string[]", required: true, notes: "我方 UUID · 后续 assign / handoff / query 都用这个" },
      { code: "unit_price",        type: "int64",    required: true, notes: "microunit · 一口价 · 所有分项已算入 · 内部分层不下发" },
      { code: "service_fee",       type: "int64",    required: true, notes: "microunit · 服务费显式列出 · 已包含在 total_debit 里" },
      { code: "total_debit",       type: "int64",    required: true, notes: "microunit · = unit_price × purchased · 本轮总扣款" },
      { code: "balance_remaining", type: "int64",    required: true, notes: "microunit · 扣完后钱包余额" },
    ],
  },
  // ── 派去向 ─────────────────────────────────
  {
    titleKey: "fields.assign-req.title",
    subKey: "fields.assign-req.sub",
    rows: [
      { code: "credential_ids", type: "string[]", required: true,  notes: "至少 1 个 · 都必须是本人名下号 · 否则响应里 errors[] 里标" },
      { code: "destination",    type: "string",   required: true,  notes: "into_bus | push_pool · 一次一个去向(不做混合) · handoff 走独立端点" },
      { code: "bus_id",         type: "string",   required: false, notes: "destination=into_bus 时必填 · 必须是本人在里的车" },
    ],
  },
  {
    titleKey: "fields.assign-resp.title",
    subKey: "fields.assign-resp.sub",
    rows: [
      { code: "assigned",   type: "int",       required: true,  notes: "本次成功派的数量 · 可能小于请求(部分失败)" },
      { code: "errors",     type: "object[]",  required: true,  notes: "每项含 { credential_id, code, message } · 全成功时空数组" },
      { code: "settlement", type: "object",    required: false, notes: "派进多人车才有 · 含 { income, lost, skipped_usernames } · 单人车 / 无清算时省略" },
    ],
  },
  // ── 拿走 · 三段式 ────────────────────────
  {
    titleKey: "fields.handoff-init.title",
    subKey: "fields.handoff-init.sub",
    rows: [
      { code: "credential_ids", type: "string[] (req)",  required: true, notes: "请求体 · 至少 1 个 · 必须都是本人名下的活号" },
      { code: "download_token", type: "string (resp)",   required: "n/a", notes: "响应 · 32 位十六进制 · TTL 5 分钟" },
      { code: "expires_at",     type: "string (resp)",   required: "n/a", notes: "响应 · token 过期时间(ISO-8601 UTC)" },
    ],
  },
  // ── API key ────────────────────────────────
  {
    titleKey: "fields.apikey-create.title",
    subKey: "fields.apikey-create.sub",
    rows: [
      { code: "key",             type: "string",  required: "n/a", notes: "**明文 · 此响应唯一一次出现** · 关闭后再也拿不回 · 服务端只落 hash" },
      { code: "item.id",         type: "string",  required: "n/a", notes: "UUID v7" },
      { code: "item.name",       type: "string",  required: "n/a", notes: "用户建 key 时填的备注 · 可空" },
      { code: "item.prefix",     type: "string",  required: "n/a", notes: "明文头 8 位十六进制 · 用来在列表页认出这个 key" },
      { code: "item.created_at", type: "string",  required: "n/a", notes: "ISO-8601 UTC" },
      { code: "item.last_used_at", type: "string?", required: "n/a", notes: "null = 建了没用过 · 用过就填最近一次" },
      { code: "item.revoked",    type: "bool",    required: "n/a", notes: "创建时永远 false" },
    ],
  },
  {
    titleKey: "fields.apikey-list.title",
    subKey: "fields.apikey-list.sub",
    rows: [
      { code: "id",           type: "string",   required: "n/a", notes: "UUID v7" },
      { code: "name",         type: "string",   required: "n/a", notes: "可空" },
      { code: "prefix",       type: "string",   required: "n/a", notes: "明文头 8 位 · 反推不出完整 key" },
      { code: "created_at",   type: "string",   required: "n/a", notes: "ISO-8601 UTC" },
      { code: "last_used_at", type: "string?",  required: "n/a", notes: "null 或 ISO-8601 UTC" },
      { code: "revoked",      type: "bool",     required: "n/a", notes: "true 时 key 已 401 · 台账保留 · 见 05 §2.3" },
    ],
  },
  // ── 车级策略 ─────────────────────────────
  {
    titleKey: "fields.bus-strategy-put.title",
    subKey: "fields.bus-strategy-put.sub",
    rows: [
      { code: "auto_refill_enabled", type: "bool",    required: false, notes: "自动补车总开关 · 纯车级(migration 040) · false=关闭" },
      { code: "refill_watermark",    type: "int",     required: false, notes: "紧急线 · 车里剩几个号触发补车 · 纯车级 · 0=不触发" },
      { code: "refill_min_count",    type: "int?",    required: false, notes: "每次补几个 · null=按 gap 补差额" },
      { code: "per_round_count",     type: "int?",    required: false, notes: "手动拉号默认份数 · null=跟随全局" },
      { code: "max_unit_price",      type: "int64?",  required: false, notes: "microunit · 本车单价上限 · 跟全局取更严 · null=跟随全局" },
      { code: "daily_round_limit",   type: "int?",    required: false, notes: "废弃 · 车级不生效 · 只读全局(migration 040)" },
      { code: "daily_spend_limit",   type: "int64?",  required: false, notes: "废弃 · 车级不生效 · 只读全局(migration 040)" },
      { code: "preferred_vendor",    type: "string?", required: false, notes: "本车优先 vendor · null=跟随全局" },
    ],
  },
  // ── 下游 · passengerpool ─────────────
  {
    titleKey: "fields.downstream-put.title",
    subKey: "fields.downstream-put.sub",
    rows: [
      { code: "passengerpool_url", type: "string?", required: false, notes: "空字符串或 nil = 别改 · 校验通过后才写库" },
      { code: "token",             type: "string",  required: false, notes: "明文 · 加密后落库 · 空字符串 = 不改现有 token · GET 只回 masked" },
      { code: "rules.push_on_pull",    type: "bool", required: false, notes: "四条推送策略在一个 rules 对象里 · 有 rules 就整体更新" },
      { code: "rules.resync_on_dead",  type: "bool", required: false, notes: "同上" },
      { code: "rules.retry_on_failure",type: "bool", required: false, notes: "同上" },
      { code: "rules.bus_only",        type: "bool", required: false, notes: "同上" },
    ],
  },
  // ── 下游 · webhook ─────────────────
  {
    titleKey: "fields.webhook-put.title",
    subKey: "fields.webhook-put.sub",
    rows: [
      { code: "url",     type: "string?",   required: false, notes: "回调地址 · nil=别改 · 校验通过后才写库" },
      { code: "enabled", type: "bool?",     required: false, notes: "总开关 · nil=别改 · URL/secret 都没配时前端强制 false" },
      { code: "events",  type: "string[]?", required: false, notes: "订阅哪几个事件 · null / 缺席=别改 · [] = 清空 · 只接受官方 4 个事件(new_keys_available/all_keys_dead/warranty_refund/boarded)" },
    ],
  },
];

function FieldsSection() {
  const { t } = useTranslation("docs");
  return (
    <>
      <Card className="p-7">
        <SectionHead title={t("fields.hero-title")} sub={t("fields.hero-sub")} />
      </Card>
      {FIELD_GROUPS.map((g) => (
        <FieldTable key={g.titleKey} title={t(g.titleKey)} sub={t(g.subKey)} rows={g.rows} />
      ))}
    </>
  );
}

function FieldTable({ title, sub, rows }: { title: string; sub: string; rows: FieldRow[] }) {
  const { t } = useTranslation("docs");
  return (
    <Card className="p-7">
      <SectionHead title={title} sub={sub} />
      <div className="mt-4 overflow-x-auto">
        <div className="min-w-[640px]">
          <BareHead>
            <span className="w-[220px] shrink-0">{t("fields.header.code")}</span>
            <span className="w-[140px] shrink-0">{t("fields.header.type")}</span>
            <span className="w-16 shrink-0 text-center">{t("fields.header.required")}</span>
            <span className="min-w-0 flex-1">{t("fields.header.meaning")}</span>
          </BareHead>
          <BareList>
            {rows.map((row) => (
              <BareRow key={row.code}>
                <code className="w-[220px] shrink-0 truncate font-mono text-label font-semibold text-fg-secondary">
                  {row.code}
                </code>
                <code className="w-[140px] shrink-0 truncate font-mono text-label text-fg-tertiary">
                  {row.type}
                </code>
                <span className="w-16 shrink-0 text-center">
                  {row.required === true
                    ? <Chip tone="warn">✓</Chip>
                    : row.required === false
                      ? <span className="text-fg-tertiary">—</span>
                      : <span className="text-fg-tertiary">·</span>}
                </span>
                <span className="min-w-0 flex-1 text-label text-fg-tertiary">
                  <NoteMd text={row.notes} />
                </span>
              </BareRow>
            ))}
          </BareList>
        </div>
      </div>
    </Card>
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
  // 跟 docs/05-api-contract §错误码全表 对齐 · 有变动优先改 md · 这里保持一致
  const ERRORS: { http: number; code: string; meaning: string }[] = [
    { http: 400, code: "bad_json", meaning: t("errors.item.bad-json") },
    { http: 400, code: "bad_idempotency_key", meaning: t("errors.item.bad-idempotency-key") },
    { http: 400, code: "bad_count", meaning: t("errors.item.bad-count") },
    { http: 400, code: "bad_vendor", meaning: t("errors.item.bad-vendor") },
    { http: 400, code: "bad_bus_id", meaning: t("errors.item.bad-bus-id") },
    { http: 401, code: "invalid_api_key", meaning: t("errors.item.invalid-api-key") },
    { http: 402, code: "insufficient_balance", meaning: t("errors.item.insufficient-balance") },
    { http: 403, code: "session_required", meaning: t("errors.item.session-required") },
    { http: 404, code: "not_found", meaning: t("errors.item.not-found") },
    { http: 404, code: "token_expired", meaning: t("errors.item.token-expired") },
    { http: 409, code: "no_stock", meaning: t("errors.item.no-stock") },
    { http: 409, code: "price_over_cap", meaning: t("errors.item.price-over-cap") },
    { http: 409, code: "daily_limit_reached", meaning: t("errors.item.daily-limit-reached") },
    { http: 409, code: "idempotency_conflict", meaning: t("errors.item.idempotency-conflict") },
    { http: 429, code: "rate_limited", meaning: t("errors.item.rate-limited") },
    { http: 502, code: "upstream_error", meaning: t("errors.item.upstream-error") },
    { http: 503, code: "service_unavailable", meaning: t("errors.item.service-unavailable") },
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
