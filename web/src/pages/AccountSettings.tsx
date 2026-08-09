import { useState } from "react";
import type { ReactNode } from "react";
import {
  BadgeCheck, CheckCircle2, KeyRound, Loader2,
} from "lucide-react";
import { useChangePassword, useMe } from "@/api/hooks";
import { SettingsHead } from "@/components/SettingsHead";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card, Chip, SectionHead } from "@/components/ui/primitives";
import { SkeletonLine } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/** 账号设置 · 邮箱 / 用户名 / 密码 / 社交登录绑定
 *  邮箱和用户名当前只读（阶段 1 后端没提供修改 API · 见 decisions §233）
 *  改邮箱 / 忘记密码 / 社交登录（Google / GitHub）阶段 3+ 引入邮箱验证时一起做 */
export default function AccountSettings() {
  return (
    <div className="space-y-section">
      <SettingsHead
        crumb="账号设置"
        title="账号设置"
        desc="邮箱、用户名、登录密码和社交登录"
      />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <AccountInfoCard />
        <ChangePasswordCard />
      </div>

      <SocialBindingsCard />
    </div>
  );
}

/* ─── 账号信息 · 只读展示（邮箱 + 用户名） ─────────────── */

function AccountInfoCard() {
  const { data: me } = useMe();

  return (
    <Card className="p-7">
      <SectionHead title="账号信息" sub="改邮箱或用户名要联系管理员 · 阶段 3+ 会加自助修改" />

      <div className="mt-5 divide-y divide-hairline border-t border-hairline">
        <InfoRow
          label="邮箱"
          value={
            me ? (
              <span className="flex items-center gap-2">
                <span className="truncate">{me.email}</span>
                {me.email_verified
                  ? <Chip tone="ok" icon={<BadgeCheck className="size-3" />}>已验证</Chip>
                  : <Chip tone="warn">未验证</Chip>}
              </span>
            ) : <SkeletonLine w={220} />
          }
        />
        <InfoRow label="用户名" value={me ? me.username : <SkeletonLine w={120} />} />
        <InfoRow
          label="注册时间"
          value={
            me
              ? new Date(me.created_at).toLocaleDateString("zh-CN", {
                  year: "numeric", month: "2-digit", day: "2-digit",
                })
              : <SkeletonLine w={100} />
          }
        />
      </div>
    </Card>
  );
}

function InfoRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-3">
      <span className="shrink-0 text-label font-semibold text-fg-secondary">{label}</span>
      <span className="min-w-0 text-right text-label font-medium">{value}</span>
    </div>
  );
}

/* ─── 改密码 ─────────────────────────────────────────────── */

function ChangePasswordCard() {
  const change = useChangePassword();
  const [oldPw, setOldPw] = useState("");
  const [newPw, setNewPw] = useState("");
  const [confirm, setConfirm] = useState("");
  const [done, setDone] = useState(false);

  const tooShort = newPw !== "" && newPw.length < 8;
  const mismatch = confirm !== "" && newPw !== confirm;
  const valid = oldPw !== "" && newPw.length >= 8 && newPw === confirm;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!valid || change.isPending) return;
    setDone(false);
    try {
      await change.mutateAsync({ old_password: oldPw, new_password: newPw });
      setDone(true);
      setOldPw(""); setNewPw(""); setConfirm("");
    } catch { /* 错误渲染在下面 */ }
  };

  return (
    <Card className="p-7">
      <SectionHead title="修改密码" sub="改完当前登录不会掉 · 其他设备需要重新登录" />

      <form onSubmit={onSubmit} className="mt-4 space-y-4">
        <Field label="当前密码">
          <Input
            type="password"
            value={oldPw}
            onChange={(e) => { setOldPw(e.target.value); setDone(false); }}
            placeholder="••••••••"
            autoComplete="current-password"
          />
        </Field>

        <Field label="新密码" hint="至少 8 位" error={tooShort ? "至少 8 位" : undefined}>
          <Input
            type="password"
            value={newPw}
            onChange={(e) => { setNewPw(e.target.value); setDone(false); }}
            placeholder="••••••••"
            autoComplete="new-password"
            className={cn(tooShort && "border-danger-fg/50")}
          />
        </Field>

        <Field label="确认新密码" error={mismatch ? "两次输入不一致" : undefined}>
          <Input
            type="password"
            value={confirm}
            onChange={(e) => { setConfirm(e.target.value); setDone(false); }}
            placeholder="再输一次"
            autoComplete="new-password"
            className={cn(mismatch && "border-danger-fg/50")}
          />
        </Field>

        {done && (
          <Alert tone="ok" icon={CheckCircle2} title="密码已更新">
            下次登录用新密码
          </Alert>
        )}
        {change.isError && (
          <Alert tone="danger" icon={KeyRound} title="改不了">
            {(change.error as Error).message}
          </Alert>
        )}

        <Button type="submit" className="w-full" disabled={!valid || change.isPending}>
          {change.isPending ? <Loader2 className="animate-spin" /> : <KeyRound />}
          {change.isPending ? "更新中…" : "更新密码"}
        </Button>
      </form>

      <p className="mt-4 text-label text-fg-tertiary">
        忘记密码的找回流程阶段 3+ 才上 · 现在忘了得联系管理员重置
      </p>
    </Card>
  );
}

/* ─── 社交登录绑定 · 阶段 3+ 才做 · 现在占位（灰态按钮·"即将上线"） ── */

function SocialBindingsCard() {
  return (
    <Card className="p-7">
      <SectionHead title="第三方登录" sub="用 Google / GitHub 直接登录 · 阶段 3+ 上线" />
      <div className="mt-4 divide-y divide-hairline border-t border-hairline">
        <SocialRow provider="Google" icon={GoogleG} />
        <SocialRow provider="GitHub" icon={GithubMark} />
      </div>
    </Card>
  );
}

function SocialRow({
  provider, icon: Icon,
}: { provider: string; icon: (props: { className?: string }) => JSX.Element }) {
  return (
    <div className="flex items-center justify-between gap-4 py-4">
      <div className="flex items-center gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-bg-elevated">
          <Icon className="size-4 text-fg-secondary" />
        </span>
        <div>
          <div className="font-semibold">{provider}</div>
          <div className="text-label text-fg-tertiary">未绑定</div>
        </div>
      </div>
      <Button variant="ghost" disabled>即将上线</Button>
    </div>
  );
}

/** Google G 徽标 · lucide 从 v0.469 起移除了品牌图标 · 用内联多色 SVG（官方 G 四色） */
function GoogleG({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 48 48" className={className} aria-hidden>
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/>
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/>
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z"/>
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/>
    </svg>
  );
}

/** GitHub Octocat mark · 单色 · currentColor 跟着容器色 */
function GithubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="currentColor" aria-hidden>
      <path d="M12 .3a12 12 0 0 0-3.79 23.4c.6.11.82-.26.82-.58v-2.02c-3.34.72-4.04-1.61-4.04-1.61-.55-1.38-1.34-1.75-1.34-1.75-1.09-.75.08-.74.08-.74 1.2.09 1.83 1.24 1.83 1.24 1.07 1.83 2.81 1.3 3.5.99.11-.77.42-1.31.76-1.61-2.66-.3-5.47-1.33-5.47-5.93 0-1.31.47-2.38 1.24-3.22-.13-.3-.54-1.52.11-3.18 0 0 1.01-.32 3.3 1.23a11.5 11.5 0 0 1 6 0c2.29-1.55 3.3-1.23 3.3-1.23.65 1.66.24 2.88.12 3.18a4.65 4.65 0 0 1 1.23 3.22c0 4.61-2.81 5.62-5.48 5.92.42.36.81 1.09.81 2.2v3.26c0 .32.22.7.83.58A12 12 0 0 0 12 .3"/>
    </svg>
  );
}
