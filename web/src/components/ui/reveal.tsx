import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

/** 进场揭示 · IntersectionObserver + CSS transition
 *
 *  为什么不用 scroll 监听：scroll 事件每帧都跑、不批处理，列表一多就掉帧。
 *  IO 只在跨越阈值时回调一次，`disconnect()` 后彻底不再占用。
 *
 *  `shown` 是一次性布尔量（不是滚动进度那种连续值），放 state 不会每帧重渲染。
 *  `prefers-reduced-motion` 直接置 true：不做动画，内容立刻在位。
 */
export function Reveal({
  children,
  /** 同组元素错开进场的毫秒数 · 用序号 × 60 之类算出来 */
  delay = 0,
  className,
}: {
  children: React.ReactNode;
  delay?: number;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
      setShown(true);
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          setShown(true);
          io.disconnect(); // 只进场一次 · 不做离场
        }
      },
      // -12% 底部内缩：元素刚探头不算，进来一截才触发，视觉上更稳
      { threshold: 0.12, rootMargin: "0px 0px -12% 0px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);

  return (
    <div
      ref={ref}
      className={cn(
        "transition-[opacity,transform] duration-500 ease-out motion-reduce:transition-none",
        shown ? "translate-y-0 opacity-100" : "translate-y-3 opacity-0",
        className,
      )}
      style={{ transitionDelay: shown ? `${delay}ms` : "0ms" }}
    >
      {children}
    </div>
  );
}
