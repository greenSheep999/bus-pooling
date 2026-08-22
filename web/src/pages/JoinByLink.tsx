import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Check, Info, Loader2, Ticket, Users } from "lucide-react";
import { useJoinByInviteCode, useMe } from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/primitives";
import { notify } from "@/lib/toast";

/**
 * /join/:code · 邀请链接落地页。
 *
 * 车主复制的是链接（不是裸 code）· 朋友点开就到这里：
 *   - 已登录 → 自动加入 → 跳车详情
 *   - 未登录 → 提示先登录/注册·带 next 回跳（登录后自动回来继续加入）
 *
 * 幂等：已经是成员时后端返 200 + 车现状·这里当成功处理。
 */
export default function JoinByLink() {
  const { t } = useTranslation("auth");
  const { code = "" } = useParams();
  const nav = useNavigate();
  const { data: me, isLoading: meLoading, isError: meError } = useMe();
  const join = useJoinByInviteCode();
  const [error, setError] = useState<string | null>(null);
  // StrictMode 下 effect 会跑两次 · 用 ref 挡住重复提交
  const attempted = useRef(false);

  const normalized = code.trim().toUpperCase();

  useEffect(() => {
    if (meLoading || meError || !me || attempted.current || !normalized) return;
    attempted.current = true;
    join
      .mutateAsync(normalized)
      .then((bus) => {
        notify.ok({ title: t("common:toast.joined") });
        nav(`/buses/${bus.id}`, { replace: true });
      })
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : String(err);
        if (msg.includes("404") || msg.toLowerCase().includes("not_found")) {
          setError(t("join.err.not-found"));
        } else if (msg.includes("409") || msg.toLowerCase().includes("bus_full")) {
          setError(t("join.err.bus-full"));
        } else {
          setError(msg);
        }
      });
    // join / nav 不进依赖 —— 只想跑一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me, meLoading, meError, normalized]);

  if (!normalized) {
    return (
      <Card className="w-full max-w-[440px] p-8">
        <h1 className="text-hero font-semibold">{t("join.empty.title")}</h1>
        <p className="mt-2 text-fg-tertiary">{t("join.empty.desc")}</p>
        <Button className="mt-6 w-full" asChild>
          <Link to="/buses">{t("join.empty.cta")}</Link>
        </Button>
      </Card>
    );
  }

  // 未登录（useMe 401）→ 引导登录·带 next 回跳这个页面
  if (meError || (!meLoading && !me)) {
    const next = encodeURIComponent(`/join/${normalized}`);
    return (
      <Card className="w-full max-w-[440px] p-8">
        <div className="flex items-center gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-brand-subtle">
            <Ticket className="size-5 text-brand-strong" />
          </span>
          <div>
            <h1 className="text-body-lg font-semibold">{t("join.guest.title")}</h1>
            <p className="text-label text-fg-tertiary">{t("join.guest.subtitle")}</p>
          </div>
        </div>

        <div className="mt-5 rounded-xl border border-hairline bg-bg-elevated p-4 text-center">
          <div className="text-label text-fg-tertiary">{t("join.guest.code-label")}</div>
          <code className="mt-1 block font-mono text-num font-semibold tracking-widest">
            {normalized}
          </code>
        </div>

        <Alert tone="brand" icon={Users} className="mt-5">
          {t("join.guest.alert")}
        </Alert>

        <div className="mt-6 space-y-2">
          <Button className="w-full" asChild>
            <Link to={`/login?next=${next}`}>{t("join.guest.login")}</Link>
          </Button>
          <Button variant="ghost" className="w-full" asChild>
            <Link to={`/register?next=${next}`}>{t("join.guest.register")}</Link>
          </Button>
        </div>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className="w-full max-w-[440px] p-8">
        <h1 className="text-hero font-semibold">{t("join.error.title")}</h1>
        <Alert tone="danger" icon={Info} className="mt-4">{error}</Alert>
        <Button variant="ghost" className="mt-6 w-full" asChild>
          <Link to="/buses">{t("join.error.cta")}</Link>
        </Button>
      </Card>
    );
  }

  return (
    <Card className="w-full max-w-[440px] p-8 text-center">
      <span className="mx-auto grid size-12 place-items-center rounded-2xl bg-brand-subtle">
        {join.isPending ? (
          <Loader2 className="size-6 animate-spin text-brand-strong" />
        ) : (
          <Check className="size-6 text-brand-strong" />
        )}
      </span>
      <h1 className="mt-4 text-body-lg font-semibold">{t("join.processing.title")}</h1>
      <p className="mt-1 text-label text-fg-tertiary">{t("join.processing.code", { code: normalized })}</p>
    </Card>
  );
}
