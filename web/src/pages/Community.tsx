import { useState } from "react";
import { ArrowUpRight, BadgeCheck, Gift, Megaphone, Ticket, Users } from "lucide-react";
import { useBindSystemCode, useCommunityChannels, useMe, type CommunityChannel } from "@/api/hooks";
import { DiscordLogo, GithubMark, TelegramLogo } from "@/components/ui/brand-icons";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useTranslation } from "react-i18next";
import { Card, Chip, Em, SectionHead } from "@/components/ui/primitives";

/** 社群页 · /community
 *
 * 两件事：
 *   1. 社群入口（TG / Discord 等 · 链接来自后端配置·不硬编）
 *   2. 绑专属邀请码 —— 补绑入口（注册时没填的可以在这里绑）
 *
 * 文案见 decisions §8.38。 */
export default function Community() {
  const { t, i18n } = useTranslation("community");
  const { data: me } = useMe();
  const { data: channelsData } = useCommunityChannels();
  const channels = channelsData?.channels ?? [];
  const bind = useBindSystemCode();
  const [code, setCode] = useState("");
  const [msg, setMsg] = useState<{ tone: "ok" | "danger"; text: string } | null>(null);

  /* 已绑码 · tier 明确为 community/wholesale 才算 · retail 或未定义都算未绑
     严格白名单判断 · 防 tier 字段缺失时误判"已绑" · docs/18 §2.1 */
  const alreadyMember = me?.tier === "community" || me?.tier === "wholesale";

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const c = code.trim().toUpperCase();
    if (!c) return;
    setMsg(null);
    try {
      await bind.mutateAsync(c);
      setMsg({ tone: "ok", text: t("bind.success") });
      setCode("");
    } catch (err: unknown) {
      const raw = err instanceof Error ? err.message : String(err);
      const text = raw.includes("404") || raw.toLowerCase().includes("not_found")
        ? t("bind.error.invalid")
        : raw.includes("409")
          ? t("bind.error.already")
          : raw;
      setMsg({ tone: "danger", text });
    }
  };

  return (
    <div className="space-y-section">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-hero font-semibold">{t("title")}</h1>
          {alreadyMember && (
            <Chip tone="brand" icon={<BadgeCheck className="size-3" />}>{t("chip.member")}</Chip>
          )}
        </div>
        <p className="text-fg-tertiary">
          {t("subtitle")}
        </p>
      </div>

      {/* 绑码 · 已经是成员就不给表单（避免重复绑的困惑） */}
      <Card className="p-7">
        <SectionHead title={t("join.title")} sub={t("join.sub")} />
        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          {channels.length === 0 ? (
            <>
              <ChannelRow logo={<TelegramLogo className="size-8" />} name="Telegram" desc={t("channel.telegram.desc")} />
              <ChannelRow logo={<DiscordLogo className="size-8" />} name="Discord" desc={t("channel.discord.desc")} />
            </>
          ) : (
            channels.map((c) => (
              <ChannelRow
                key={c.id}
                logo={channelLogo(c.id)}
                name={pickChannelName(c, i18n.language)}
                desc={channelDesc(c.id, t)}
                url={c.url}
              />
            ))
          )}
        </div>
        <p className="mt-4 text-label text-fg-tertiary">
          {t("channel.footnote")}
        </p>
      </Card>

      <Card className="p-7">
        <SectionHead
          title={t("bind.title")}
          sub={
            alreadyMember
              ? t("bind.sub.member")
              : t("bind.sub.guest")
          }
        />

        {alreadyMember ? (
          <Alert tone="ok" icon={BadgeCheck} className="mt-4">
            {t("bind.already")}
          </Alert>
        ) : (
          <form onSubmit={onSubmit} className="mt-4 space-y-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <Field label={t("bind.field.label")} className="min-w-0 flex-1">
                <Input
                  value={code}
                  onChange={(e) => setCode(e.target.value.toUpperCase())}
                  placeholder={t("bind.field.placeholder")}
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
                {bind.isPending ? t("bind.submitting") : t("bind.submit")}
              </Button>
            </div>

            {msg && (
              <Alert tone={msg.tone === "ok" ? "ok" : "danger"}>{msg.text}</Alert>
            )}

            <p className="text-label text-fg-tertiary">
              {t("bind.hint")}
            </p>
          </form>
        )}
      </Card>

      <Card className="p-7">
        <SectionHead title={t("perks.title")} />
        <ul className="mt-3 space-y-2 text-label text-fg-secondary">
          <li className="flex gap-2">
            <Megaphone className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
            {t("perks.news")}
          </li>
          <li className="flex gap-2">
            <Gift className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
            {t("perks.gift.prefix")}<Em plain>{t("perks.gift.em")}</Em>{t("perks.gift.suffix")}
          </li>
          <li className="flex gap-2">
            <Users className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
            {t("perks.friends")}
          </li>
        </ul>
      </Card>
    </div>
  );
}

/** 社群渠道行 · 有 url 时渲染成可点外链 · 无 url 时纯展示 + soon 徽标 */
function ChannelRow({
  logo, name, desc, url,
}: {
  /** 官方彩色 logo（品牌配色 · 不跟站内主题色混） */
  logo: React.ReactNode;
  name: string;
  desc: string;
  url?: string;
}) {
  const { t } = useTranslation("community");
  const Wrapper = url ? "a" : "div";
  const wrapperProps = url
    ? { href: url, target: "_blank", rel: "noopener noreferrer" as const }
    : {};
  return (
    <Wrapper
      {...wrapperProps}
      className={
        "flex items-center gap-3 rounded-xl border border-hairline bg-bg-elevated p-4 transition-colors " +
        (url ? "hover:border-brand hover:bg-bg" : "")
      }
    >
      <span className="grid size-9 shrink-0 place-items-center">
        {logo}
      </span>
      <div className="min-w-0 flex-1">
        <div className="font-semibold">{name}</div>
        <div className="text-label text-fg-tertiary">{desc}</div>
      </div>
      {url ? (
        <ArrowUpRight className="size-4 shrink-0 text-fg-tertiary" />
      ) : (
        <Chip tone="neutral">{t("channel.soon")}</Chip>
      )}
    </Wrapper>
  );
}

/** 按 channel id 挑官方彩色 logo · 未知 id 兜底文字 */
function channelLogo(id: string): React.ReactNode {
  if (id.startsWith("telegram")) return <TelegramLogo className="size-8" />;
  if (id === "discord") return <DiscordLogo className="size-8" />;
  if (id === "github") return <GithubMark className="size-7 text-fg" />;
  return null;
}

/** 描述文案从 i18n community.channel.<id>.desc 拿 · 缺就空 */
function channelDesc(id: string, t: (k: string) => string): string {
  const key = `channel.${id}.desc`;
  const v = t(key);
  return v === key ? "" : v;
}

/** 从 name_i18n 挑当前语言 · 找不到 fallback 到 name */
function pickChannelName(c: CommunityChannel, lang: string): string {
  const m = c.name_i18n;
  if (!m) return c.name;
  if (m[lang]) return m[lang];
  const short = lang.split("-")[0];
  if (m[short]) return m[short];
  return c.name;
}
