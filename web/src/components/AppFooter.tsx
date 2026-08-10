import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { useCommunityChannels } from "@/api/hooks";
import { DiscordMark, GithubMark, TelegramMark } from "@/components/ui/brand-icons";
import { BrandLogo } from "@/components/BrandLogo";

/** 底部 footer · 登录前后共用（AppLayout 和 Landing 都挂它）
 *  左侧品牌 + 社群 icon · 右侧 3 栏菜单靠右
 *  真实存在的政策入口，不堆 dead link */
export function AppFooter() {
  const { t } = useTranslation();
  const { data: channelsData } = useCommunityChannels();
  const channels = channelsData?.channels ?? [];
  // 按 id 挑一条 · 后端没配就不渲染，绝不给死链
  const tg = channels.find((c) => c.id === "telegram_channel") ?? channels.find((c) => c.id === "telegram_group");
  const discord = channels.find((c) => c.id === "discord");
  const github = channels.find((c) => c.id === "github");
  return (
    <footer className="mt-auto border-t border-hairline bg-bg-elevated">
      <div className="page-container py-10">
        {/* 布局：品牌左（自适应窄一点，别撑）· 3 栏菜单右 · gap-16 拉开距离 · 移动堆叠 */}
        <div className="flex flex-col gap-10 lg:flex-row lg:justify-between lg:gap-16">
          {/* 品牌区 · 左侧 · max-w 收紧不占位 */}
          <div className="max-w-xs space-y-3">
            <BrandLogo />
            <p className="text-label leading-relaxed text-fg-tertiary">
              {t("footer.blurb")}
            </p>
            {/* 社群 · Telegram · Discord · GitHub（就这 3 个 · 不放邮件） */}
            <div className="flex items-center gap-2 pt-1">
              {tg && (
                <SocialLink href={tg.url} label="Telegram">
                  <TelegramMark className="size-[18px]" />
                </SocialLink>
              )}
              {discord && (
                <SocialLink href={discord.url} label="Discord">
                  <DiscordMark className="size-[18px]" />
                </SocialLink>
              )}
              {github && (
                <SocialLink href={github.url} label="GitHub">
                  <GithubMark className="size-[18px]" />
                </SocialLink>
              )}
            </div>
          </div>

          {/* 3 栏菜单 · 每栏至少 120px 保证不瘦成一条 · gap 从内容自然拉开
              lg 起间距 gap-12（48px）· 之前 gap-20 是栏比 gap 还窄的错做法 */}
          <div className="grid grid-cols-2 gap-x-8 gap-y-8 sm:grid-cols-3 sm:gap-x-10 lg:gap-x-12 [&>div]:min-w-[120px]">
            <FooterCol title={t("footer.product")}>
              <FooterLink to="/overview">{t("nav.overview")}</FooterLink>
              <FooterLink to="/buses">{t("nav.buses")}</FooterLink>
              <FooterLink to="/extract">{t("nav.extract")}</FooterLink>
              <FooterLink to="/dispatch">{t("nav.dispatch")}</FooterLink>
            </FooterCol>

            <FooterCol title={t("footer.account")}>
              <FooterLink to="/wallet">{t("footer.wallet")}</FooterLink>
              <FooterLink to="/me">{t("footer.me")}</FooterLink>
              <FooterLink to="/settings">{t("avatar.settings")}</FooterLink>
              {/* footer 里把三类设置也直接列出来（页脚就是给人扫的，多一层点击没意义） */}
              <FooterLink to="/settings/downstream">{t("footer.downstream")}</FooterLink>
              <FooterLink to="/settings/webhook">{t("footer.webhook")}</FooterLink>
              <FooterLink to="/settings/api-keys">{t("footer.api_keys")}</FooterLink>
            </FooterCol>

            <FooterCol title={t("footer.docs_policy")}>
              <FooterLink to="/legal/terms">{t("legal.pages.terms")}</FooterLink>
              <FooterLink to="/legal/usage">{t("legal.pages.usage")}</FooterLink>
              <FooterLink to="/legal/services">{t("legal.pages.services")}</FooterLink>
              <FooterLink to="/legal/regions">{t("legal.pages.regions")}</FooterLink>
              <FooterLink to="/docs">{t("footer.docs")}</FooterLink>
            </FooterCol>
          </div>
        </div>

        {/* 底行 · copyright + 状态入口 */}
        <div className="mt-8 flex flex-col gap-3 border-t border-hairline pt-6 text-label text-fg-tertiary md:flex-row md:items-center md:justify-between">
          <span>{t("footer.copyright")}</span>
          <div className="flex items-center gap-4">
            <span className="flex items-center gap-1.5">
              {/* 这个点是真实语义状态（系统健康）· 不是装饰 */}
              <span className="size-1.5 rounded-full bg-ok-solid" />
              {t("footer.status_ok")}
            </span>
            <Link
              to="/status"
              className="transition-colors hover:text-fg-secondary"
            >
              {t("footer.status_page")}
            </Link>
          </div>
        </div>
      </div>
    </footer>
  );
}

function SocialLink({
  href, label, children,
}: { href: string; label: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target={href.startsWith("http") ? "_blank" : undefined}
      rel="noreferrer"
      aria-label={label}
      title={label}
      className="grid size-9 place-items-center rounded-lg border border-hairline bg-bg text-fg-secondary transition-colors hover:border-fg-secondary hover:text-fg"
    >
      {children}
    </a>
  );
}

function FooterCol({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <h4 className="text-[10px] font-semibold uppercase tracking-wider text-fg-tertiary">
        {title}
      </h4>
      <ul className="space-y-2">{children}</ul>
    </div>
  );
}

function FooterLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <li>
      <Link
        to={to}
        className="text-label font-medium text-fg-secondary transition-colors hover:text-fg"
      >
        {children}
      </Link>
    </li>
  );
}
