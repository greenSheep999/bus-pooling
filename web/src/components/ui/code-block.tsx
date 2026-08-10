import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/utils";

/** 代码块 + 复制 · 对接文档用
 *  不上语法高亮：多引一个 highlighter 就为了几段 JSON 不值当（KISS） */
export function CodeBlock({
  code,
  lang,
  className,
}: {
  code: string;
  /** 右上角标签 · 例 "bash" / "json" */
  lang?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  const { t } = useTranslation();

  return (
    <div className={cn("relative rounded-xl border border-hairline bg-bg-elevated", className)}>
      <div className="flex items-center justify-between border-b border-hairline px-3 py-1.5">
        <span className="font-mono text-[10px] font-semibold uppercase tracking-wide text-fg-tertiary">
          {lang ?? "text"}
        </span>
        <button
          type="button"
          onClick={() => {
            navigator.clipboard.writeText(code);
            setCopied(true);
            setTimeout(() => setCopied(false), 1600);
          }}
          aria-label={t("action.copy_code")}
          className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-label font-medium text-fg-tertiary transition-colors hover:bg-bg hover:text-fg-secondary"
        >
          {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
          {copied ? t("action.copied") : t("action.copy")}
        </button>
      </div>
      <pre className="overflow-x-auto p-3 font-mono text-label leading-relaxed text-fg-secondary">
        {code}
      </pre>
    </div>
  );
}
