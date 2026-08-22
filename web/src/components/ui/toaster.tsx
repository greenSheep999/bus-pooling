import { Toaster as Sonner } from "sonner";

/** 全局 toast 容器 · 右下 · 无默认 chrome（卡片样式在 lib/toast.tsx）
 *  挂在 main 一次 · 跨 layout 切换不丢 */
export function Toaster() {
  return (
    <Sonner
      position="bottom-right"
      offset={20}
      gap={10}
      visibleToasts={4}
      expand={false}
      toastOptions={{ unstyled: true }}
    />
  );
}
