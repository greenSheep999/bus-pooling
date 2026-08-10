import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight, Loader2, Ticket, UserPlus } from "lucide-react";
import { z } from "zod";
import { useRegister } from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

type FieldKey = "email" | "username" | "password" | "confirm";

export default function Register() {
  const { t } = useTranslation("auth");
  /** 注册校验 · spec §2 要求前端校验密码强度 */
  const schema = z
    .object({
      email: z.string().email(t("register.err.email")),
      username: z
        .string()
        .min(2, t("register.err.username-min"))
        .max(24, t("register.err.username-max"))
        .regex(/^[\w一-龥-]+$/, t("register.err.username-format")),
      password: z
        .string()
        .min(8, t("register.err.password-min"))
        .regex(/\d/, t("register.err.password-digit"))
        .regex(/[a-zA-Z]/, t("register.err.password-letter")),
      confirm: z.string(),
    })
    .refine((v) => v.password === v.confirm, {
      path: ["confirm"],
      message: t("register.err.password-mismatch"),
    });
  // 注册成功后用 window.location 强刷·让 useQuery 拿新 session 重取
  // ?next= 支持邀请链接场景（/join/:code → 注册 → 回来继续加入）· 只收同源相对路径
  const [searchParams] = useSearchParams();
  const nextRaw = searchParams.get("next") ?? "";
  const nextPath =
    nextRaw.startsWith("/") && !nextRaw.startsWith("//") ? nextRaw : "/";
  // ?invite= 预填邀请码（/invite 页复制的链接带的）· 大写归一
  // **不读这个的话邀请链接等于废的** —— 用户点开看到空输入框·不手动填就没有邀请关系
  const prefillInvite = (searchParams.get("invite") ?? "").trim().toUpperCase();
  const register = useRegister();

  const [form, setForm] = useState({
    email: "", username: "", password: "", confirm: "", invite: prefillInvite,
  });
  /* 只在失焦或提交后才报错 —— 边打字边红是最烦人的表单反馈 */
  const [touched, setTouched] = useState<Partial<Record<FieldKey, boolean>>>({});

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  const parsed = schema.safeParse(form);
  const errorOf = (k: FieldKey): string | undefined => {
    if (!touched[k] || parsed.success) return undefined;
    return parsed.error.issues.find((i) => i.path[0] === k)?.message;
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched({ email: true, username: true, password: true, confirm: true } as Record<FieldKey, boolean>);
    if (!parsed.success || register.isPending) return;
    try {
      await register.mutateAsync({
        email: form.email.trim(),
        username: form.username.trim(),
        password: form.password,
        ...(form.invite.trim() ? { invite_code: form.invite.trim() } : {}),
      });
      // 强刷跳目标页 · 让所有 useQuery 用新 session 重取
      window.location.href = nextPath;
    } catch {
      /* 错误渲染在下面 */
    }
  };

  return (
    <Card className="w-full max-w-[440px] p-8">
      <div className="space-y-2">
        <h1 className="text-hero font-semibold">{t("register.title")}</h1>
        <p className="text-fg-tertiary">{t("register.subtitle")}</p>
      </div>

      <form onSubmit={onSubmit} className="mt-6 space-y-4">
        <Field label={t("register.email")} error={errorOf("email")}>
          <Input
            value={form.email}
            onChange={set("email")}
            onBlur={() => setTouched((t) => ({ ...t, email: true }))}
            placeholder="you@example.com"
            autoComplete="email"
            autoFocus
            className={cn(errorOf("email") && "border-danger-fg/50")}
          />
        </Field>

        <Field label={t("register.username")} error={errorOf("username")}>
          <Input
            value={form.username}
            onChange={set("username")}
            onBlur={() => setTouched((prev) => ({ ...prev, username: true }))}
            placeholder={t("register.username-placeholder")}
            autoComplete="username"
            className={cn(errorOf("username") && "border-danger-fg/50")}
          />
        </Field>

        <Field label={t("register.password")} hint={t("register.password-hint")} error={errorOf("password")}>
          <Input
            type="password"
            value={form.password}
            onChange={set("password")}
            onBlur={() => setTouched((prev) => ({ ...prev, password: true }))}
            placeholder="••••••••"
            autoComplete="new-password"
            className={cn(errorOf("password") && "border-danger-fg/50")}
          />
        </Field>

        <Field label={t("register.confirm")} error={errorOf("confirm")}>
          <Input
            type="password"
            value={form.confirm}
            onChange={set("confirm")}
            onBlur={() => setTouched((prev) => ({ ...prev, confirm: true }))}
            placeholder={t("register.confirm-placeholder")}
            autoComplete="new-password"
            className={cn(errorOf("confirm") && "border-danger-fg/50")}
          />
        </Field>

        {/* 邀请码 · 注册时填，永久绑账号（decisions §8.20）
            这里叫「邀请码」· 支付和提号那个叫「优惠码」，两个词不许混用 */}
        <Field
          label={t("register.invite")}
          hint={prefillInvite ? t("register.invite-hint-prefilled") : t("register.invite-hint")}
        >
          <Input
            value={form.invite}
            onChange={set("invite")}
            placeholder={t("register.invite-placeholder")}
            className="font-mono uppercase"
          />
        </Field>

        {form.invite.trim() && (
          /* 文案要对两种码都成立（decisions §8.29 §8.38）· 后端查白名单自动识别 */
          <Alert tone="brand" icon={Ticket}>
            {prefillInvite && form.invite.trim() === prefillInvite
              ? t("register.invite-alert-prefilled")
              : t("register.invite-alert")}
            {" · "}
            {t("register.invite-alert-explain")}
          </Alert>
        )}

        {register.isError && (
          <Alert tone="danger" icon={UserPlus} title={t("register.error-title")}>
            {(register.error as Error).message}
          </Alert>
        )}

        <Button
          type="submit"
          variant="brand"
          className="w-full"
          disabled={register.isPending}
        >
          {register.isPending ? <Loader2 className="animate-spin" /> : <ArrowRight />}
          {register.isPending ? t("register.submitting") : t("register.submit")}
        </Button>
      </form>

      <p className="mt-5 text-center text-label text-fg-tertiary">
        {t("register.has-account")}{" "}
        <Link to="/login" className="font-semibold text-brand-strong hover:underline">
          {t("register.go-login")}
        </Link>
      </p>
    </Card>
  );
}
