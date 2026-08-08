import { KeyRound } from "lucide-react";
import {
  Dialog, DialogBody, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { PullExtractForm } from "@/components/PullExtractForm";

/** 提取 key 模态壳（次入口 · 从别处触发拉号时用）
 *  主入口 Extract 页顶部直接展示 <PullExtractForm> · 不套 modal
 *  这里 modal 版本给未来其他触发点用（例如快捷键 / 全局 CTA） */
export function PullExtractModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[640px]">
        <DialogHeader>
          <DialogTitle>
            <span className="inline-flex items-center gap-2">
              <KeyRound className="size-4 text-brand-strong" />
              提取 key
            </span>
          </DialogTitle>
        </DialogHeader>
        <DialogBody>
          <PullExtractForm onSubmitted={onClose} />
        </DialogBody>
      </DialogContent>
    </Dialog>
  );
}
