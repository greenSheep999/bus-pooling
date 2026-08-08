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
import { cn, fmtTime } from "@/lib/utils";
import type { DownstreamConfig } from "@/types";

/** 4 条推送规则 · 跟 DownstreamConfig.rules 一一对应
 *  这是技术页（spec §10），允许出现 kiro.rs 这类术语 —— 类比对接文档 */
const RULES: {
  key: keyof DownstreamConfig["rules"];
  title: string;
  desc: string;
}[] = [
  {
    key: "push_on_pull",
    title: "号进车立即推",
    desc: "拉到号就同步到你的号池（双写 · 我方保留副本继续监控存活）",
  },
  {
    key: "resync_on_dead",
    title: "号死了同步删",
    desc: "我方探到号失效时，也从你号池里删掉，省得你的客户端拿到死号",
  },
  {
    key: "retry_on_failure",
    title: "推送失败自动重试",
    desc: "退避重试 5s → 30s → 5min · 三次都失败才记失败",
  },
  {
    key: "bus_only",
    title: "只推拼车的号",
    desc: "开 = 只推进了车的号 · 关 = 拼车和单独提取的号都推",
  },
];

export default function Downstream() {
  const { data: cfg } = useDownstream();
  const save = useSaveDownstream();
  const test = useTestDownstream();

  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [testResult, setTestResult] = useState<{ ok: boolean; ms: number } | null>(null);

  /* 服务端值到了再灌进表单 · 用户已经改过就不覆盖他的输入 */
  useEffect(() => {
    if (cfg && !url) setUrl(cfg.passengerpool_url);
  }, [cfg, url]);

  const dirty = !!cfg && (url !== cfg.passengerpool_url || token.trim() !== "");

  const onSave = async () => {
    await save.mutateAsync({
      passengerpool_url: url.trim(),
      ...(token.trim() ? { token: token.trim() } : {}),
    });
    setToken("");
  };

  const onTest = async () => {
    setTestResult(null);
    const r = await test.mutateAsync({ url: url.trim(), token: token.trim() || undefined });
    setTestResult({ ok: r.ok, ms: r.latency_ms });
  };

  return (
    <div className="space-y-section">
      <SettingsHead
        crumb="我的号池"
        title="我的号池"
        desc="把号同步到你自己的 kiro.rs · 我方仍保留副本监控存活，号死了这边也帮你清"
        right={
          cfg?.connected
            ? <Chip tone="ok" dot>已连通</Chip>
            : <Chip tone="danger" dot>未连通</Chip>
        }
      />

      {/* 3 状态卡 */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
        <StatusCard
          icon={Plug}
          label="连通状态"
          value={cfg?.connected ? "正常" : "断开"}
          tone={cfg?.connected ? "ok" : "danger"}
          sub={
            cfg?.last_heartbeat_at
              ? <>最近心跳 {fmtTime(cfg.last_heartbeat_at)}</>
              : "还没有心跳"
          }
        />
        <StatusCard
          icon={Activity}
          label="推送成功率"
          value={cfg ? `${(cfg.push_success_rate * 100).toFixed(1)}%` : "-"}
          tone={!cfg ? undefined : cfg.push_success_rate >= 0.95 ? "ok" : "warn"}
          sub={cfg ? <>失败 <Em tone="spend">{cfg.push_failed}</Em> 次</> : null}
        />
        <StatusCard
          icon={Send}
          label="累计推送"
          value={cfg ? String(cfg.push_total) : "-"}
          sub="号次"
        />
      </div>

      {/* 端点卡 · focal */}
      <Card focal focalTone="brand" className="p-7">
        <SectionHead
          title="号池端点"
          sub="你自己那台 kiro.rs 的地址和管理密钥 · 我方用它做 BatchImport"
        />

        <div className="mt-4 space-y-4">
          <Field label="号池地址" hint="https://…">
            <Input
              value={url}
              onChange={(e) => { setUrl(e.target.value); setTestResult(null); }}
              placeholder="https://kiro-my.example.com"
              className="font-mono"
            />
          </Field>

          <Field
            label="管理密钥"
            hint={token.trim() ? "保存后只留打码版" : "留空 = 不改"}
          >
            <Input
              value={token}
              onChange={(e) => { setToken(e.target.value); setTestResult(null); }}
              placeholder="输入新密钥以替换"
              type="password"
              className="font-mono"
            />
          </Field>

          {/* 当前已存的密钥 · 只有打码版，拿不回明文 */}
          {cfg && !token.trim() && (
            <div className="space-y-1.5">
              <span className="text-label font-semibold text-fg-secondary">当前密钥</span>
              <SecretField masked={cfg.passengerpool_token_masked} />
            </div>
          )}

          {testResult && (
            <Alert
              tone={testResult.ok ? "ok" : "danger"}
              icon={testResult.ok ? CheckCircle2 : Plug}
              title={testResult.ok ? "连通正常" : "连不上"}
            >
              {testResult.ok
                ? <>握手耗时 <Em>{testResult.ms}</Em> ms</>
                : "检查地址和密钥是否正确 · 以及这台机器能不能被我方访问"}
            </Alert>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button variant="brand" onClick={onSave} disabled={!dirty || save.isPending}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />}
              {save.isPending ? "保存中…" : "保存"}
            </Button>
            <Button variant="ghost" onClick={onTest} disabled={!url.trim() || test.isPending}>
              {test.isPending ? <Loader2 className="animate-spin" /> : <Zap />}
              {test.isPending ? "测试中…" : "测试连接"}
            </Button>
            {dirty && <span className="text-label text-fg-tertiary">有未保存的修改</span>}
          </div>
        </div>
      </Card>

      {/* 推送策略 */}
      <Card className="p-7">
        <SectionHead title="推送策略" sub="什么时候推、失败怎么办" />

        <div className="mt-4 divide-y divide-hairline">
          {RULES.map((r) => (
            <div key={r.key} className="flex items-start justify-between gap-4 py-3.5">
              <div className="min-w-0 space-y-0.5">
                <div className="font-semibold">{r.title}</div>
                <p className="text-label text-fg-tertiary">{r.desc}</p>
              </div>
              <Switch
                className="mt-0.5 shrink-0"
                checked={cfg?.rules[r.key] ?? false}
                disabled={!cfg || save.isPending}
                onCheckedChange={(v) =>
                  cfg && save.mutate({ rules: { ...cfg.rules, [r.key]: v } })
                }
                aria-label={r.title}
              />
            </div>
          ))}
        </div>
      </Card>

      {/* 没配时给个说明 · 别让用户猜为什么推送没生效 */}
      {cfg && !cfg.passengerpool_url && (
        <Alert tone="warn" icon={Database} title="还没配号池">
          配好之后，拉到的号才能自动同步过去 · 不配也能用，只是得手动下载拿走
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
