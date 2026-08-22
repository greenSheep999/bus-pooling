import { toast } from "sonner";
import { AlertTriangle, Check, Info, X } from "lucide-react";
import { ApiError } from "@/api/client";
import { cn } from "@/lib/utils";

/** 写操作反馈 · 4 态对齐 design/gaps G12 + docs/13 §7.4
 *  卡片：左 icon · 中 title/desc/action · 右关闭
 *  位置由 <Toaster> 定在右下 · 3.5s 自动消失 */
export type ToastTone = "ok" | "info" | "warn" | "danger";

export interface ToastAction {
  label: string;
  href?: string;
  onClick?: () => void;
}

export interface ToastPayload {
  title: string;
  desc?: string;
  action?: ToastAction;
  duration?: number;
}

const ICON_WRAP: Record<ToastTone, string> = {
  ok: "bg-ok-bg text-ok-fg",
  info: "bg-info-bg text-info-fg",
  warn: "bg-warn-bg text-warn-fg",
  danger: "bg-danger-bg text-danger-fg",
};

function ToneIcon({ tone }: { tone: ToastTone }) {
  if (tone === "warn") {
    return (
      <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-warn-bg text-warn-fg">
        <AlertTriangle className="size-4" strokeWidth={2.4} />
      </span>
    );
  }
  const Icon = tone === "ok" ? Check : tone === "info" ? Info : X;
  return (
    <span className={cn("grid size-8 shrink-0 place-items-center rounded-full", ICON_WRAP[tone])}>
      <Icon className="size-4" strokeWidth={2.6} />
    </span>
  );
}

function ToastCard({
  id,
  tone,
  title,
  desc,
  action,
}: {
  id: string | number;
  tone: ToastTone;
  title: string;
  desc?: string;
  action?: ToastAction;
}) {
  const dismiss = () => toast.dismiss(id);
  const runAction = () => {
    action?.onClick?.();
    dismiss();
  };

  return (
    <div className="flex w-[min(360px,calc(100vw-32px))] items-start gap-3 rounded-[14px] border border-hairline bg-bg p-3.5 shadow-pop">
      <ToneIcon tone={tone} />
      <div className="min-w-0 flex-1 pt-0.5">
        <div className="font-semibold leading-snug text-fg">{title}</div>
        {desc && <p className="mt-0.5 text-label leading-snug text-fg-tertiary">{desc}</p>}
        {action && (
          action.href ? (
            <a
              href={action.href}
              onClick={runAction}
              className="mt-2 inline-flex items-center text-label font-semibold text-brand-strong hover:opacity-80"
            >
              {action.label}
              <span aria-hidden className="ml-0.5">→</span>
            </a>
          ) : (
            <button
              type="button"
              onClick={runAction}
              className="mt-2 inline-flex items-center text-label font-semibold text-brand-strong hover:opacity-80"
            >
              {action.label}
              <span aria-hidden className="ml-0.5">→</span>
            </button>
          )
        )}
      </div>
      <button
        type="button"
        onClick={dismiss}
        className="grid size-6 shrink-0 place-items-center rounded-md text-fg-tertiary transition-colors hover:bg-bg-elevated hover:text-fg"
        aria-label="close"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}

function show(tone: ToastTone, p: ToastPayload) {
  toast.custom(
    (id) => (
      <ToastCard
        id={id}
        tone={tone}
        title={p.title}
        desc={p.desc}
        action={p.action}
      />
    ),
    { duration: p.duration ?? 3500 },
  );
}

export function errText(err: unknown, fallback: string): string {
  if (err instanceof ApiError && err.message) return err.message;
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}

export const notify = {
  ok: (p: ToastPayload) => show("ok", p),
  info: (p: ToastPayload) => show("info", p),
  warn: (p: ToastPayload) => show("warn", p),
  danger: (p: ToastPayload) => show("danger", p),
  fail: (err: unknown, fallback: string) =>
    show("danger", { title: fallback, desc: errText(err, fallback) === fallback ? undefined : errText(err, fallback) }),
};
