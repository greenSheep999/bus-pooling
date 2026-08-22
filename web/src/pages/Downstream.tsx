import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import {
  Activity, CheckCircle2, Database, Loader2, Plug, Save, Send, Zap,
} from "lucide-react";
import { useDownstream, useSaveDownstream, useTestDownstream } from "@/api/hooks";
import { SettingsHead } from "@/components/SettingsHead";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card, Chip, Em, SectionHead } from "@/components/ui/primitives";
import { SecretField } from "@/components/ui/secret-field";
import { Switch } from "@/components/ui/switch";
import { notify } from "@/lib/toast";
import { cn, fmtTime } from "@/lib/utils";
import type { DownstreamConfig } from "@/types";
import { useTranslation } from "react-i18next";

/** 推送规则 · 只保留真被后端消费的 toggle
 *
 *  **阶段 1 收官(2026-08-15 审计)撤 3 条死控件**:push_on_pull / resync_on_dead /
 *  retry_on_failure 后端 DB 落库了 · 但 delivery / pullrecord / decider / deathwatch
 *  没有任何读点 · 勾了不生效(CLAUDE §0.1 违反)· UI 撤掉避免误导。
 *
 *  只保留 bus_only · webhookout/events.go:92 真消费(BusOnly 过滤单号事件)。 */
const RULES: {
  key: keyof DownstreamConfig["rules"];
  titleKey: string;
  descKey: string;
}[] = [
  { key: "bus_only", titleKey: "rules.bus-only.title", descKey: "rules.bus-only.desc" },
];

export default function Downstream() {
  const { t } = useTranslation("settings");
  const { data: cfg } = useDownstream();
  const save = useSaveDownstream();
  const test = useTestDownstream();

  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [testResult, setTestResult] = useState<{ ok: boolean; ms: number; error?: string } | null>(null);

  /* 服务端值到了再灌进表单 · 用户已经改过就不覆盖他的输入 */
  useEffect(() => {
    if (cfg && !url) setUrl(cfg.passengerpool_url);
  }, [cfg, url]);

  const dirty = !!cfg && (url !== cfg.passengerpool_url || token.trim() !== "");

  const onSave = async () => {
    try {
      await save.mutateAsync({
        passengerpool_url: url.trim(),
        ...(token.trim() ? { token: token.trim() } : {}),
      });
      setToken("");
      notify.ok({ title: t("common:toast.downstream_ok") });
    } catch (err) {
      notify.fail(err, t("common:toast.generic_fail"));
    }
  };

  const onTest = async () => {
    setTestResult(null);
    const r = await test.mutateAsync({ url: url.trim(), token: token.trim() || undefined });
    setTestResult({ ok: r.ok, ms: r.latency_ms, error: r.error });
  };

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb={t("header.crumb")}
        title={t("header.title")}
        desc={t("header.desc")}
        right={
          cfg?.connected
            ? <Chip tone="ok" dot>{t("header.connected")}</Chip>
            : <Chip tone="danger" dot>{t("header.disconnected")}</Chip>
        }
      />

      {/* 3 状态卡 */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
        <StatusCard
          icon={Plug}
          label={t("status.connection.label")}
          value={cfg?.connected ? t("status.connection.value.ok") : t("status.connection.value.down")}
          tone={cfg?.connected ? "ok" : "danger"}
          sub={
            cfg?.last_heartbeat_at
              ? <>{t("status.connection.heartbeat", { time: fmtTime(cfg.last_heartbeat_at) })}</>
              : t("status.connection.no-heartbeat")
          }
        />
        <StatusCard
          icon={Activity}
          label={t("status.success-rate.label")}
          value={cfg ? `${(cfg.push_success_rate * 100).toFixed(1)}%` : "-"}
          tone={!cfg ? undefined : cfg.push_success_rate >= 0.95 ? "ok" : "warn"}
          sub={cfg ? <>{t("status.success-rate.failed-prefix")} <Em tone="spend">{cfg.push_failed}</Em> {t("status.success-rate.failed-suffix")}</> : null}
        />
        <StatusCard
          icon={Send}
          label={t("status.total.label")}
          value={cfg ? String(cfg.push_total) : "-"}
          sub={t("status.total.unit")}
        />
      </div>

      {/* 端点卡 · focal */}
      <Card focal focalTone="brand" className="p-7">
        <SectionHead
          title={t("endpoint.title")}
          sub={t("endpoint.desc")}
        />

        <div className="mt-4 space-y-4">
          <Field label={t("endpoint.url.label")} hint={t("endpoint.url.hint")}>
            <Input
              value={url}
              onChange={(e) => { setUrl(e.target.value); setTestResult(null); }}
              placeholder={t("endpoint.url.placeholder")}
              className="font-mono"
            />
          </Field>

          <Field
            label={t("endpoint.token.label")}
            hint={token.trim() ? t("endpoint.token.hint.new") : t("endpoint.token.hint.empty")}
          >
            <Input
              value={token}
              onChange={(e) => { setToken(e.target.value); setTestResult(null); }}
              placeholder={t("endpoint.token.placeholder")}
              type="password"
              className="font-mono"
            />
          </Field>

          {/* 当前已存的密钥 · 只有打码版，拿不回明文 */}
          {cfg && !token.trim() && (
            <div className="space-y-1.5">
              <span className="text-label font-semibold text-fg-secondary">{t("endpoint.token.current")}</span>
              <SecretField masked={cfg.passengerpool_token_masked} />
            </div>
          )}

          {testResult && (
            <Alert
              tone={testResult.ok ? "ok" : "danger"}
              icon={testResult.ok ? CheckCircle2 : Plug}
              title={testResult.ok ? t("test.ok.title") : t("test.fail.title")}
            >
              {testResult.ok
                ? <>{t("test.ok.desc-prefix")} <Em>{testResult.ms}</Em> {t("test.ok.desc-suffix")}</>
                : testResult.error
                  ? <>{testResult.error}{testResult.ms > 0 && <> · <Em>{testResult.ms}</Em> ms</>}</>
                  : t("test.fail.desc")}
            </Alert>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button variant="brand" onClick={onSave} disabled={!dirty || save.isPending}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              {save.isPending ? t("action.saving") : t("common:action.save")}
            </Button>
            <Button variant="ghost" onClick={onTest} disabled={!url.trim() || test.isPending}>
              {test.isPending ? <Loader2 className="animate-spin" /> : <Zap />}
              {test.isPending ? t("action.testing") : t("action.test")}
            </Button>
            {dirty && <span className="text-label text-fg-tertiary">{t("action.dirty")}</span>}
          </div>
        </div>
      </Card>

      {/* 推送策略 */}
      <Card className="p-7">
        <SectionHead title={t("rules.title")} sub={t("rules.subtitle")} />

        <div className="mt-4 divide-y divide-hairline">
          {RULES.map((r) => (
            <div key={r.key} className="flex items-start justify-between gap-4 py-3.5">
              <div className="min-w-0 space-y-0.5">
                <div className="font-semibold">{t(r.titleKey)}</div>
                <p className="text-label text-fg-tertiary">{t(r.descKey)}</p>
              </div>
              <Switch
                className="mt-0.5 shrink-0"
                checked={cfg?.rules[r.key] ?? false}
                disabled={!cfg || save.isPending}
                onCheckedChange={(v) =>
                  cfg && save.mutate({ rules: { ...cfg.rules, [r.key]: v } })
                }
                aria-label={t(r.titleKey)}
              />
            </div>
          ))}
        </div>
      </Card>

      {/* 没配时给个说明 · 别让用户猜为什么推送没生效 */}
      {cfg && !cfg.passengerpool_url && (
        <Alert tone="warn" icon={Database} title={t("empty.title")}>
          {t("empty.desc")}
        </Alert>
      )}
    </div>
  );
}

function StatusCard({
  icon: Icon, label, value, sub, tone,
}: {
  icon: typeof Plug;
  label: string;
  value: string;
  sub?: ReactNode;
  tone?: "ok" | "warn" | "danger";
}) {
  return (
    <Card className="p-6">
      <div className="flex items-center gap-2.5">
        <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-bg-elevated">
          <Icon className="size-3.5 text-fg-secondary" />
        </span>
        <span className="text-label font-semibold text-fg-secondary">{label}</span>
      </div>
      <div
        className={cn(
          "mt-2.5 text-stat font-semibold tnum",
          tone === "ok" ? "text-ok-fg"
            : tone === "warn" ? "text-warn-fg"
              : tone === "danger" ? "text-danger-fg"
                : "text-fg",
        )}
      >
        {value}
      </div>
      {sub && <div className="mt-0.5 text-label text-fg-tertiary">{sub}</div>}
    </Card>
  );
}
