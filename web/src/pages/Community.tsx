import { useState } from "react";
import { BadgeCheck, Gift, Megaphone, Ticket, Users } from "lucide-react";
import { useBindSystemCode, useMe } from "@/api/hooks";
import { DiscordLogo, TelegramLogo } from "@/components/ui/brand-icons";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card, Chip, Em, SectionHead } from "@/components/ui/primitives";

/** 社群页 · /community
 *
 * 两件事：
 *   1. 社群入口（TG / Discord 等 · 链接来自后端配置·不硬编）
 *   2. 绑专属邀请码 —— 补绑入口（注册时没填的可以在这里绑）
 *
 * 文案见 decisions §8.38。 */
export default function Community() {
  const { data: me } = useMe();
  const bind = useBindSystemCode();
  const [code, setCode] = useState("");
  const [msg, setMsg] = useState<{ tone: "ok" | "danger"; text: string } | null>(null);

  /* 已绑码 · tier 明确为 wholesale/insider 才算 · retail 或未定义都算未绑
     严格白名单判断 · 防 tier 字段缺失时误判"已绑" · decisions §8.39 */
  const alreadyMember = me?.tier === "wholesale" || me?.tier === "insider";

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const c = code.trim().toUpperCase();
    if (!c) return;
    setMsg(null);
    try {
      await bind.mutateAsync(c);
      setMsg({ tone: "ok", text: "绑定成功 · 你现在是社群成员了" });
      setCode("");
    } catch (err: unknown) {
      const raw = err instanceof Error ? err.message : String(err);
      const text = raw.includes("404") || raw.toLowerCase().includes("not_found")
        ? "这个专属邀请码无效、已停用或已用满"
        : raw.includes("409")
          ? "你已经是社群成员了"
          : raw;
      setMsg({ tone: "danger", text });
    }
  };

  return (
    <div className="space-y-section">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-hero font-semibold">社群</h1>
          {alreadyMember && (
            <Chip tone="brand" icon={<BadgeCheck className="size-3" />}>社群成员</Chip>
          )}
        </div>
        <p className="text-fg-tertiary">
          有专属邀请码就绑上 · 拼车更便宜
        </p>
      </div>

      {/* 绑码 · 已经是成员就不给表单（避免重复绑的困惑） */}
      <Card className="p-7">
        <SectionHead title="加入社群" sub="公告 · 技术支持 · 公测期间的额度发放都在这里" />
        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          <ChannelRow
            logo={<TelegramLogo className="size-8" />}
            name="Telegram"
            desc="主频道 · 公告和额度发放"
          />
          <ChannelRow
            logo={<DiscordLogo className="size-8" />}
            name="Discord"
            desc="讨论 · 技术支持"
          />
        </div>
        <p className="mt-4 text-label text-fg-tertiary">
          社群链接开放后会在这里更新 · 也会通过顶部公告推送
        </p>
      </Card>

      <Card className="p-7">
        <SectionHead
          title="绑定专属邀请码"
          sub={
            alreadyMember
              ? "你已经绑过了 · 身份永久有效"
              : "身份标识 · 邀请制 · 一个账号只能绑一次"
          }
        />

        {alreadyMember ? (
          <Alert tone="ok" icon={BadgeCheck} className="mt-4">
            已绑定 · 你的拼车价格已经是社群价
          </Alert>
        ) : (
          <form onSubmit={onSubmit} className="mt-4 space-y-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <Field label="专属邀请码" className="min-w-0 flex-1">
                <Input
                  value={code}
                  onChange={(e) => setCode(e.target.value.toUpperCase())}
                  placeholder="KIROXXXX"
                  maxLength={32}
                  autoCapitalize="characters"
                  autoComplete="off"
                  spellCheck={false}
                  className="tnum tracking-widest"
                />
              </Field>
              <Button
                type="submit"
                className="h-10 shrink-0"
                disabled={bind.isPending || code.trim() === ""}
              >
                <Ticket className="size-4" />
                {bind.isPending ? "绑定中…" : "绑定"}
              </Button>
            </div>

            {msg && (
              <Alert tone={msg.tone === "ok" ? "ok" : "danger"}>{msg.text}</Alert>
            )}

            <p className="text-label text-fg-tertiary">
              拿不到也不影响使用 · 持续拼车的车友会陆续收到
            </p>
          </form>
        )}
      </Card>

      <Card className="p-7">
        <SectionHead title="加入社群能得到什么" />
        <ul className="mt-3 space-y-2 text-label text-fg-secondary">
          <li className="flex gap-2">
            <Megaphone className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
            第一时间知道拼车服务的最新动态
          </li>
          <li className="flex gap-2">
            <Gift className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
            不定期发<Em plain>充值优惠券</Em>和兑换码
          </li>
          <li className="flex gap-2">
            <Users className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
            认识更多一起拼车的车友 · 人多摊得更便宜
          </li>
        </ul>
      </Card>
    </div>
  );
}

/** 社群渠道行 · 链接未配置时不给可点的死链 */
function ChannelRow({
  logo, name, desc,
}: {
  /** 官方彩色 logo（品牌配色 · 不跟站内主题色混） */
  logo: React.ReactNode;
  name: string;
  desc: string;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-hairline bg-bg-elevated p-4">
      <span className="grid size-9 shrink-0 place-items-center">
        {logo}
      </span>
      <div className="min-w-0 flex-1">
        <div className="font-semibold">{name}</div>
        <div className="text-label text-fg-tertiary">{desc}</div>
      </div>
      <Chip tone="neutral">即将开放</Chip>
    </div>
  );
}
