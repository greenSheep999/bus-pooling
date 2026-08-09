import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

/** Split-flap（翻牌 / 机械翻页）显示器
 *
 * 机场航班牌那种效果：字符变化时上半片往下翻、露出新字符。
 *
 * **实现取舍**：
 *   - 纯 CSS 3D transform（`rotateX` + `perspective`），不用 canvas 也不引动画库
 *   - 只在**字符真的变了**的那一位翻 —— 整排一起翻会晃眼，而且看不出哪位在变
 *   - `prefers-reduced-motion` 时直接切字符不做动画（无障碍 · 也省电）
 *
 * 用在倒计时上：每秒只有秒位翻，分/时位不动，视觉安静。
 */

/** 单个字符牌 */
function Flap({ char, className }: { char: string; className?: string }) {
  const [display, setDisplay] = useState(char);
  const [prev, setPrev] = useState(char);
  const [flipping, setFlipping] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => {
    if (char === display) return;
    // reduced-motion：直接换，不翻
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    if (reduce) {
      setDisplay(char);
      setPrev(char);
      return;
    }
    setPrev(display);
    setDisplay(char);
    setFlipping(true);
    window.clearTimeout(timer.current);
    // 跟 CSS 动画时长对齐（400ms）· 结束后清 flipping 免得残留图层
    timer.current = window.setTimeout(() => setFlipping(false), 400);
    return () => window.clearTimeout(timer.current);
  }, [char, display]);

  return (
    <span
      className={cn(
        // 机场翻牌 · 固定深底白字（不跟随主题）· 机械感来源就是那块深塑料
        // 用固定色不用 --fg/--bg · 否则深色下会变成白底黑字（破坏翻牌语义）
        "relative inline-block overflow-hidden rounded-[3px] bg-neutral-800 px-[3px] text-center",
        "font-mono text-white tabular-nums",
        className,
      )}
      style={{ perspective: "60px" }}
    >
      {/* 静态底层：当前字符 */}
      <span className="relative z-0 block">{display}</span>

      {/* 翻牌层：旧字符的上半片往下翻走 · 只在变化瞬间存在 */}
      {flipping && (
        <span
          aria-hidden
          className="absolute inset-0 z-10 block origin-bottom animate-splitflap"
          style={{ backfaceVisibility: "hidden" }}
        >
          <span className="block bg-neutral-800">{prev}</span>
        </span>
      )}

      {/* 中缝 · 机械感来源（一条极细的暗线） */}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-1/2 z-20 h-px bg-black/40"
      />
    </span>
  );
}

/** 一串字符的翻牌显示 · 每位独立翻 */
export function SplitFlap({
  value,
  className,
  charClassName,
}: {
  value: string;
  className?: string;
  charClassName?: string;
}) {
  return (
    <span className={cn("inline-flex items-center gap-[2px]", className)} aria-label={value}>
      {value.split("").map((c, i) => (
        // key 用位置 —— 每一位是固定的一张牌，字符在牌上变
        <Flap key={i} char={c} className={charClassName} />
      ))}
    </span>
  );
}

/** 倒计时（翻牌样式）· 到点后回调
 *
 * `until` 是 RFC3339 · `serverNow` 是服务端当前时间（也 RFC3339）。
 *
 * **为什么要 serverNow**：客户端时钟不可信（可能偏几分钟甚至几天）。
 * 用 (serverNow - clientNow) 算出偏移，倒计时才准 —— 否则时钟快的用户会提前看到"已结束"。
 */
export function SplitFlapCountdown({
  until,
  serverNow,
  onExpire,
  className,
}: {
  until: string;
  serverNow?: string;
  onExpire?: () => void;
  className?: string;
}) {
  // 客户端与服务端的时钟偏移（毫秒）· 只在 serverNow 变化时重算
  const skew = useRef(0);
  useEffect(() => {
    if (!serverNow) return;
    const s = new Date(serverNow).getTime();
    if (!Number.isNaN(s)) skew.current = s - Date.now();
  }, [serverNow]);

  const target = new Date(until).getTime();
  const [left, setLeft] = useState(() => target - (Date.now() + skew.current));

  useEffect(() => {
    if (Number.isNaN(target)) return;
    const tick = () => {
      const ms = target - (Date.now() + skew.current);
      setLeft(ms);
      if (ms <= 0) onExpire?.();
    };
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
    // onExpire 不进依赖 —— 它通常是内联箭头函数·会导致每次 render 重建 interval
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  if (Number.isNaN(target) || left <= 0) return null;

  const totalSec = Math.floor(left / 1000);
  const d = Math.floor(totalSec / 86400);
  const h = Math.floor((totalSec % 86400) / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  const p2 = (n: number) => String(n).padStart(2, "0");

  return (
    <span className={cn("inline-flex items-center gap-1.5 text-label", className)}>
      {d > 0 && (
        <>
          <SplitFlap value={p2(d)} />
          <span className="opacity-70">天</span>
        </>
      )}
      <SplitFlap value={p2(h)} />
      <span className="opacity-70">:</span>
      <SplitFlap value={p2(m)} />
      <span className="opacity-70">:</span>
      <SplitFlap value={p2(s)} />
    </span>
  );
}
