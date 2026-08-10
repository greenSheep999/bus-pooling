import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Gift, Link2 as LinkIcon, Ticket, Users } from "lucide-react";
import { useMyInvite } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { EmptyState } from "@/components/ui/empty-state";
import { SkeletonCard } from "@/components/ui/skeleton";
import { fmtTime } from "@/lib/utils";

/** 邀请好友页 · /invite · 展示个人邀请码 + 邀请记录。
 *
 * 只下发结果字段（码 / 邀请数 / 剩余额度）· 不下发每次邀请给几次额度。 */
export default function Invite() {
  const { t } = useTranslation("invite");
  const { data, isLoading } = useMyInvite();
  const [copied, setCopied] = useState<"link" | "code" | null>(null);

  const code = data?.code ?? "";
  const referrals = data?.referrals ?? [];
  const link = code ? `${window.location.origin}/register?invite=${code}` : "";

  const copy = (text: string, which: "link" | "code") => {
    if (!text) return;
    navigator.clipboard.writeText(text);
    setCopied(which);
    setTimeout(() => setCopied(null), 1600);
  };

  return (
    <div className="space-y-section">
      <div className="space-y-2">
        <h1 className="text-hero font-semibold">{t("title")}</h1>
        <p className="text-fg-tertiary">
          {t("subtitle")}
        </p>
      </div>

      {isLoading && !data ? (
        <SkeletonCard lines={4} />
      ) : (
        <>
          {/* 我的码 + 链接 */}
          <Card className="p-7">
            <SectionHead title={t("my.title")} sub={t("my.sub")} />

            <div className="mt-4 flex items-baseline gap-2">
              <span className="text-label text-fg-tertiary">{t("my.code.label")}</span>
              <code className="select-all font-mono text-num font-semibold tracking-widest">
                {code || "—"}
              </code>
            </div>

            <div className="mt-3 flex flex-col gap-2 sm:flex-row">
              <input
                readOnly
                value={link || "—"}
                onFocus={(e) => e.currentTarget.select()}
                className="h-10 min-w-0 flex-1 rounded-xl border border-hairline bg-bg-elevated px-3 font-mono text-label text-fg-secondary outline-none focus:border-brand"
              />
              <Button
                className="h-10 shrink-0"
                onClick={() => copy(link, "link")}
                disabled={!link}
              >
                {copied === "link" ? <Check /> : <LinkIcon />}
                {copied === "link" ? t("copy.copied") : t("copy.link")}
              </Button>
            </div>
          </Card>

          {/* 成绩 · 只给结果不给规则细节 */}
          <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
            <Card className="p-6">
              <div className="flex items-center gap-2.5">
                <span className="grid size-8 place-items-center rounded-lg bg-brand-subtle">
                  <Users className="size-4 text-brand-strong" />
                </span>
                <span className="text-label font-medium text-fg-secondary">{t("stat.invited")}</span>
              </div>
              <div className="mt-3 text-hero font-semibold tnum">
                {data?.invited_count ?? 0}
                <span className="ml-1 text-body text-fg-tertiary">{t("stat.invited.unit")}</span>
              </div>
              <p className="mt-1 text-label text-fg-tertiary">{t("stat.invited.desc")}</p>
            </Card>

            <Card className="p-6">
              <div className="flex items-center gap-2.5">
                <span className="grid size-8 place-items-center rounded-lg bg-credit-bg">
                  <Gift className="size-4 text-credit-fg" />
                </span>
                <span className="text-label font-medium text-fg-secondary">{t("stat.waiver")}</span>
              </div>
              <div className="mt-3 text-hero font-semibold tnum">
                {data?.waiver_remaining ?? 0}
                <span className="ml-1 text-body text-fg-tertiary">{t("stat.waiver.unit")}</span>
              </div>
              <p className="mt-1 text-label text-fg-tertiary">
                {(data?.waiver_used ?? 0) > 0
                  ? t("stat.waiver.used", { n: data?.waiver_used })
                  : t("stat.waiver.fresh")}
              </p>
            </Card>
          </div>

          {/* 邀请记录 · 被邀请人只显示脱敏标识（后端就不给完整邮箱 · 那是第三方 PII） */}
          <Card className="p-7">
            <SectionHead
              title={t("records.title")}
              sub={
                referrals.length > 0
                  ? <>{t("records.sub.count_prefix")}<Em>{referrals.length}</Em>{t("records.sub.count_suffix")}</>
                  : t("records.sub.empty")
              }
            />
            {referrals.length === 0 ? (
              <EmptyState
                icon={Users}
                title={t("records.empty.title")}
                desc={t("records.empty.desc")}
              />
            ) : (
              <div className="mt-4">
                <BareHead>
                  <span className="min-w-0 flex-1">{t("records.col.friend")}</span>
                  <span className="w-24 shrink-0 text-center">{t("records.col.granted")}</span>
                  <span className="w-[92px] shrink-0 text-right">{t("records.col.joined")}</span>
                </BareHead>
                <BareList>
                  {referrals.map((r, i) => (
                    <BareRow key={`${r.invitee}-${i}`}>
                      <span className="flex min-w-0 flex-1 items-center gap-2">
                        <span className="grid size-7 shrink-0 place-items-center rounded-full bg-brand-subtle text-label font-semibold text-brand-strong">
                          {r.invitee.charAt(0).toUpperCase()}
                        </span>
                        <span className="truncate font-mono text-label text-fg-secondary">
                          {r.invitee}
                        </span>
                      </span>
                      <span className="w-24 shrink-0 text-center">
                        <Chip tone="ok">+{r.waiver_granted} {t("records.granted.unit") || "次"}</Chip>
                      </span>
                      <span className="w-[92px] shrink-0 text-right text-label tnum text-fg-tertiary">
                        {fmtTime(r.joined_at)}
                      </span>
                    </BareRow>
                  ))}
                </BareList>
                <p className="mt-3 text-label text-fg-tertiary">
                  {t("records.footnote")}
                </p>
              </div>
            )}
          </Card>

          <Card className="p-7">
            <SectionHead title={t("rules.title")} />
            <ul className="mt-3 space-y-2 text-label text-fg-secondary">
              <li className="flex gap-2">
                <Ticket className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
                {t("rules.step1")}
              </li>
              <li className="flex gap-2">
                <Ticket className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
                {t("rules.step2.prefix")}<Em plain>{t("rules.step2.em")}</Em>{t("rules.step2.suffix")}
              </li>
              <li className="flex gap-2">
                <Ticket className="mt-0.5 size-3.5 shrink-0 text-fg-tertiary" />
                {t("rules.step3")}
              </li>
            </ul>
          </Card>
        </>
      )}
    </div>
  );
}
