import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight, Loader2, Lock } from "lucide-react";
import { useLogin } from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card } from "@/components/ui/primitives";
import { Checkbox } from "@/components/ui/checkbox";

export default function Login() {
  const { t } = useTranslation("auth");
  const [searchParams] = useSearchParams();
  const login = useLogin();

  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const [remember, setRemember] = useState(true);

  const valid = account.trim() !== "" && password !== "";

  // 被踢时 client.ts 会带 ?next=/somewhere · 登录成功后跳回原路径
  // 校验：只跳同源相对路径·防 open redirect
  const nextRaw = searchParams.get("next") ?? "";
  const nextPath =
    nextRaw.startsWith("/") && !nextRaw.startsWith("//") ? nextRaw : "/";

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!valid || login.isPending) return;
    try {
      await login.mutateAsync({ account: account.trim(), password, remember });
      // 用 window.location 强刷·让所有 useQuery 拿新 session 重取（比 invalidate 更彻底）
      window.location.href = nextPath;
    } catch {
      /* 错误渲染在下面 · 不跳转 */
    }
  };

  return (
    <Card className="w-full max-w-[400px] p-8">
      <div className="space-y-2">
        <h1 className="text-hero font-semibold">{t("login.title")}</h1>
        <p className="text-fg-tertiary">{t("login.subtitle")}</p>
      </div>

      <form onSubmit={onSubmit} className="mt-6 space-y-4">
        <Field label={t("login.account")}>
          <Input
            value={account}
            onChange={(e) => setAccount(e.target.value)}
            placeholder="you@example.com"
            autoComplete="username"
            autoFocus
          />
        </Field>

        <Field label={t("login.password")}>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="••••••••"
            autoComplete="current-password"
          />
        </Field>

        <div className="flex items-center justify-between gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-label font-medium text-fg-secondary">
            <Checkbox
              checked={remember}
              onCheckedChange={(v) => setRemember(v === true)}
            />
            {t("login.remember")}
          </label>
          {/* 忘记密码没端点（阶段 3+）· 不放假链接，直接说明 */}
          <span
            className="cursor-default text-label text-fg-tertiary"
            title={t("login.forgot-tip")}
          >
            {t("login.forgot")}
          </span>
        </div>

        {login.isError && (
          <Alert tone="danger" icon={Lock} title={t("login.error-title")}>
            {(login.error as Error).message}
          </Alert>
        )}

        <Button
          type="submit"
          variant="brand"
          className="w-full"
          disabled={!valid || login.isPending}
        >
          {login.isPending ? <Loader2 className="animate-spin" /> : <ArrowRight />}
          {login.isPending ? t("login.submitting") : t("login.submit")}
        </Button>
      </form>

      <p className="mt-5 text-center text-label text-fg-tertiary">
        {t("login.no-account")}{" "}
        <Link to="/register" className="font-semibold text-brand-strong hover:underline">
          {t("login.go-register")}
        </Link>
      </p>
    </Card>
  );
}
