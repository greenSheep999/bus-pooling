import { useState } from "react";
import type { ReactNode } from "react";
import {
  BadgeCheck, CheckCircle2, KeyRound, Loader2, LogOut, Ticket,
} from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useChangePassword, useLogout, useMe } from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Card, Chip, SectionHead } from "@/components/ui/primitives";
import { avatarColor, avatarLetter, cn } from "@/lib/utils";

export default function Profile() {
  const { data: me } = useMe();
  const nav = useNavigate();
  const logout = useLogout();

  const av = avatarColor(me?.username ?? "?");

  return (
    <div className="space-y-section">
      {/* 这页在 /me 不在 /settings 下 —— 所以不用 SettingsHead，没有「设置 ›」面包屑 */}
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">我的</h1>
          <p className="text-fg-tertiary">你的账号信息和密码</p>
        </div>
        <div className="shrink-0">
          <Button
            variant="ghost"
            onClick={async () => {
              await logout.mutateAsync();
              nav("/login");
            }}
            disabled={logout.isPending}
          >
            {logout.isPending ? <Loader2 className="animate-spin" /> : <LogOut />}
            退出登录
          </Button>
        </div>
      </div>

      {/* minmax(0,1fr)：`1fr` 的 auto 下限 = min-content，会让左列拒绝收缩挤出右列 */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_420px]">
        {/* 账号信息 · 只读 */}
        <Card className="p-7">
          <SectionHead title="基本信息" sub="这些暂时改不了 · 需要改联系管理员" />

          <div className="mt-5 flex items-center gap-4">
            <span
              className="grid size-14 shrink-0 place-items-center rounded-full text-body-lg font-semibold"
              style={{ backgroundColor: av.bg, color: av.fg }}
            >
              {avatarLetter(me?.username ?? "?")}
            </span>
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate text-body-lg font-semibold">
                  {me?.username ?? "-"}
                </span>
                {/* 有邀请码 = 社群成员（decisions §8.20）· 免加价 + 看真名 */}
                {me?.invited && (
                  <Chip tone="brand" icon={<Ticket className="size-3" />}>社群成员</Chip>
                )}
              </div>
              <p className="text-label text-fg-tertiary">{me?.email ?? "-"}</p>
            </div>
          </div>

          <div className="mt-5 divide-y divide-hairline border-t border-hairline">
            <InfoRow
              label="邮箱"
              value={
                <span className="flex items-center gap-2">
                  {me?.email ?? "-"}
                  {me?.email_verified
                    ? <Chip tone="ok" icon={<BadgeCheck className="size-3" />}>已验证</Chip>
                    : <Chip tone="warn">未验证</Chip>}
                </span>
              }
            />
            <InfoRow label="用户名" value={me?.username ?? "-"} />
            <InfoRow
              label="注册时间"
              value={
                me
                  ? new Date(me.created_at).toLocaleDateString("zh-CN", {
                      year: "numeric", month: "2-digit", day: "2-digit",
                    })
                  : "-"
              }
            />
            <InfoRow
              label="加价"
              value={
                me?.invited
                  ? <span className="text-ok-fg">免加价</span>
                  : <span className="text-fg-secondary">默认加价 · 提号时填优惠码可免</span>
              }
            />
          </div>

          {!me?.invited && (
            <Alert tone="neutral" icon={Ticket} className="mt-4">
              注册时填过邀请码的账号免加价、并且能看到上游真实名字 ·
              邀请码只能在注册时填，现在改不了
            </Alert>
          )}
        </Card>

        <ChangePasswordCard />
      </div>
    </div>
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
    } catch {
      /* 错误渲染在下面 */
    }
  };

  return (
    <Card className="p-7">
      <SectionHead title="改密码" sub="改完当前登录不会掉 · 其他设备需要重新登录" />

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

        <Field
          label="新密码"
          hint="至少 8 位"
          error={tooShort ? "至少 8 位" : undefined}
        >
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

        <Button
          type="submit"
          className="w-full"
          disabled={!valid || change.isPending}
        >
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
