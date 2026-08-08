import type { LucideIcon } from "lucide-react";
import { Bot, ChevronRight, Database, KeyRound, SlidersHorizontal } from "lucide-react";
import { Link } from "react-router-dom";
import type { ReactNode } from "react";
import { useApiKeys, useDownstream, useGlobalStrategy, useWebhook } from "@/api/hooks";
import { Card, Chip, Em } from "@/components/ui/primitives";
import { fmtCredits } from "@/lib/utils";

/** 设置索引 · 设置的主入口
 *  「我的」不在这里 —— 账号本身不是一种设置，它在 /me */
export default function Settings() {
  const { data: gs } = useGlobalStrategy();
  const { data: ds } = useDownstream();
  const { data: wh } = useWebhook();
  const { data: keys } = useApiKeys();

  const activeKeys = (keys ?? []).filter((k) => !k.revoked).length;

  return (
    <div className="space-y-section">
      <div className="min-w-0 space-y-2">
        <h1 className="text-hero font-semibold">设置</h1>
        <p className="text-fg-tertiary">号池对接 · 事件通知 · API 访问</p>
      </div>

      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        {/* 拉号偏好放第一张 —— 它是唯一会**拦下操作**的设置（每日上限），
            另外三个都是"连了/没连"性质。用户最该先看到会花钱和被限流的那个 */}
        <SettingCard
          to="/settings/preferences"
          icon={SlidersHorizontal}
          title="拉号偏好"
          desc="每天总上限 · 建新车时的默认值"
          status={
            gs && (gs.daily_round_limit != null || gs.daily_spend_limit != null)
              ? <Chip tone="ok" dot>已设上限</Chip>
              : <Chip tone="warn" dot>未设上限</Chip>
          }
          meta={
            gs
              ? (
                <>
                  今日 <Em>{gs.used_today.rounds}</Em>
                  {gs.daily_round_limit != null && <>/{gs.daily_round_limit}</>} 轮 ·
                  花 <Em>{fmtCredits(gs.used_today.spend)}</Em>
                  {gs.daily_spend_limit != null && <>/{fmtCredits(gs.daily_spend_limit)}</>}
                </>
              )
              : null
          }
        />

        <SettingCard
          to="/settings/downstream"
          icon={Database}
          title="我的号池"
          desc="把号同步到你自己的 kiro.rs · 号死了这边也帮你清"
          status={
            ds?.connected
              ? <Chip tone="ok" dot>已连通</Chip>
              : <Chip tone="danger" dot>未连通</Chip>
          }
          meta={
            ds
              ? <>推送成功率 <Em>{(ds.push_success_rate * 100).toFixed(1)}%</Em> · 累计 <Em>{ds.push_total}</Em> 次</>
              : null
          }
        />

        <SettingCard
          to="/settings/webhook"
          icon={Bot}
          title="机器人通知"
          desc="拉号完成、号失效、余额不足推到你的 webhook"
          status={
            wh?.enabled
              ? <Chip tone="ok" dot>启用中</Chip>
              : <Chip tone="neutral" dot>已停用</Chip>
          }
          meta={wh ? <>已订阅 <Em>{wh.events.length}</Em> 个事件</> : null}
        />

        <SettingCard
          to="/settings/api-keys"
          icon={KeyRound}
          title="API key"
          desc="脚本和机器人拿它调我方接口"
          status={
            activeKeys > 0
              ? <Chip tone="ok" dot>{activeKeys} 个可用</Chip>
              : <Chip tone="neutral" dot>还没建</Chip>
          }
          meta={keys ? <>共 <Em>{keys.length}</Em> 个 · 已吊销 <Em>{keys.length - activeKeys}</Em> 个</> : null}
        />
      </div>

      <p className="text-label text-fg-tertiary">
        每辆车的补车策略跟车绑（decisions §8.6），在
        <Link to="/buses" className="font-semibold text-brand-strong hover:underline">车详情</Link>
        里单独配 —— 这里只管全局的上限和新车默认值 · 账号和密码在
        <Link to="/me" className="font-semibold text-brand-strong hover:underline">我的</Link>
      </p>
    </div>
  );
}

function SettingCard({
  to, icon: Icon, title, desc, status, meta,
}: {
  to: string;
  icon: LucideIcon;
  title: string;
  desc: string;
  status: ReactNode;
  meta: ReactNode;
}) {
  /* Card 传 to 就整卡可点 + 自带 hover 悬浮（可点区域 = 浮起区域） */
  return (
    <Card to={to} className="flex flex-col gap-3 p-6">
      <div className="flex items-start justify-between gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-bg-elevated">
          <Icon className="size-4 text-fg-secondary" />
        </span>
        {status}
      </div>

      <div className="min-w-0 space-y-1">
        <div className="flex items-center gap-1.5">
          <span className="font-semibold">{title}</span>
          <ChevronRight className="size-3.5 text-fg-tertiary" />
        </div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>

      {meta && <div className="mt-auto text-label text-fg-tertiary">{meta}</div>}
    </Card>
  );
}
