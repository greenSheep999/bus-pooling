import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "./button";
import { cn } from "@/lib/utils";

/** 打码的密钥显示 + 复制 · 号池 admin key / webhook secret 共用
 *
 *  故意**没有**"查看明文"按钮：这些值存下来后我方只留打码版，
 *  拿不回明文（跟 handoff 一样的一次性语义）。要换就重新生成。
 *  想覆盖成新值走旁边的输入框，不是"看一眼再改"。
 */
export function SecretField({
  masked,
  /** 有明文时（刚生成那一次）显示明文并允许复制 */
  plaintext,
  className,
}: {
  /** 打码版 · 只有明文的场景（刚新建的 key）可以不传 */
  masked?: string;
  plaintext?: string | null;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  const shown = plaintext ?? masked ?? "-";

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <code
        className={cn(
          "min-w-0 flex-1 truncate rounded-xl border px-3 py-2 font-mono text-label",
          plaintext
            ? "border-brand-hairline bg-brand-subtle/40 font-semibold"
            : "border-hairline bg-bg-elevated text-fg-secondary",
        )}
      >
        {shown}
      </code>
      {plaintext && (
        <Button
          variant="ghost"
          size="icon"
          aria-label="复制"
          onClick={() => {
            navigator.clipboard.writeText(plaintext);
            setCopied(true);
            setTimeout(() => setCopied(false), 1600);
          }}
        >
          {copied ? <Check /> : <Copy />}
        </Button>
      )}
    </div>
  );
}
