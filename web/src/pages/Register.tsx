import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ArrowRight, Loader2, Ticket, UserPlus } from "lucide-react";
import { z } from "zod";
import { useRegister } from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card } from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

/** 注册校验 · spec §2 要求前端校验密码强度 */
const schema = z
  .object({
    email: z.string().email("邮箱格式不对"),
    username: z
      .string()
      .min(2, "至少 2 个字")
      .max(24, "最多 24 个字")
      .regex(/^[\w一-龥-]+$/, "只能用字母、数字、下划线、横线或中文"),
    password: z.string().min(8, "至少 8 位").regex(/\d/, "要含数字").regex(/[a-zA-Z]/, "要含字母"),
    confirm: z.string(),
  })
  .refine((v) => v.password === v.confirm, {
    path: ["confirm"],
    message: "两次输入不一致",
  });

type FieldKey = "email" | "username" | "password" | "confirm";

export default function Register() {
  const nav = useNavigate();
  const register = useRegister();

  const [form, setForm] = useState({
    email: "", username: "", password: "", confirm: "", invite: "",
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
    setTouched({ email: true, username: true, password: true, confirm: true });
    if (!parsed.success || register.isPending) return;
    try {
      await register.mutateAsync({
        email: form.email.trim(),
        username: form.username.trim(),
        password: form.password,
        ...(form.invite.trim() ? { invite_code: form.invite.trim() } : {}),
      });
      nav("/");
    } catch {
      /* 错误渲染在下面 */
    }
  };

  return (
    <Card className="w-full max-w-[440px] p-8">
      <div className="space-y-2">
        <h1 className="text-hero font-semibold">注册</h1>
        <p className="text-fg-tertiary">建个账号就能开始拼车</p>
      </div>

      <form onSubmit={onSubmit} className="mt-6 space-y-4">
        <Field label="邮箱" error={errorOf("email")}>
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

        <Field label="用户名" error={errorOf("username")}>
          <Input
            value={form.username}
            onChange={set("username")}
            onBlur={() => setTouched((t) => ({ ...t, username: true }))}
            placeholder="车友都看得到这个名字"
            autoComplete="username"
            className={cn(errorOf("username") && "border-danger-fg/50")}
          />
        </Field>

        <Field label="密码" hint="至少 8 位 · 含字母和数字" error={errorOf("password")}>
          <Input
            type="password"
            value={form.password}
            onChange={set("password")}
            onBlur={() => setTouched((t) => ({ ...t, password: true }))}
            placeholder="••••••••"
            autoComplete="new-password"
            className={cn(errorOf("password") && "border-danger-fg/50")}
          />
        </Field>

        <Field label="确认密码" error={errorOf("confirm")}>
          <Input
            type="password"
            value={form.confirm}
            onChange={set("confirm")}
            onBlur={() => setTouched((t) => ({ ...t, confirm: true }))}
            placeholder="再输一次"
            autoComplete="new-password"
            className={cn(errorOf("confirm") && "border-danger-fg/50")}
          />
        </Field>

        {/* 邀请码 · 注册时填，永久绑账号（decisions §8.20）
            这里叫「邀请码」· 支付和提号那个叫「优惠码」，两个词不许混用 */}
        <Field label="邀请码" hint="选填 · 社群成员才有">
          <Input
            value={form.invite}
            onChange={set("invite")}
            placeholder="有就填，没有也能注册"
            className="font-mono uppercase"
          />
        </Field>

        {form.invite.trim() && (
          <Alert tone="brand" icon={Ticket}>
            填了邀请码 · 注册后免加价，并且能看到上游的真实名字
          </Alert>
        )}

        {register.isError && (
          <Alert tone="danger" icon={UserPlus} title="注册失败">
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
          {register.isPending ? "注册中…" : "注册"}
        </Button>
      </form>

      <p className="mt-5 text-center text-label text-fg-tertiary">
        已经有账号？{" "}
        <Link to="/login" className="font-semibold text-brand-strong hover:underline">
          去登录
        </Link>
      </p>
    </Card>
  );
}
