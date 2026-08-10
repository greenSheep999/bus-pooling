import LogoMark from "@/assets/logo/mark.png";
import LogoWordmark from "@/assets/logo/wordmark.png";
import HyperBadge from "@/assets/logo/hyper.svg";

/** 品牌 Logo · mark（方图标）+ wordmark（"Kiro.bus" 字样图片）+ hyper（星徽 badge）
 *
 *  - <BrandLogo />：mark + wordmark（默认带 hyper 星徽在 wordmark 右上）
 *  - <BrandLogo mark />：只要方标（footer 里目前也这么用）
 *  - <BrandLogo showBadge={false} />：想不显示星徽
 *
 *  Hyper badge 从 kiro-auto 迁过来，绝对定位在 wordmark 右上 · 不占布局宽度 */
export function BrandLogo({
  mark = false,
  showBadge = true,
  className = "",
  markClassName = "size-7 rounded-lg shrink-0",
  wordmarkClassName = "h-5 w-auto",
}: {
  mark?: boolean;
  /** 是否在 wordmark 右上角显示 hyper 星徽 · mark-only 模式下无效 */
  showBadge?: boolean;
  className?: string;
  markClassName?: string;
  wordmarkClassName?: string;
}) {
  if (mark) {
    return (
      <img
        src={LogoMark}
        alt="Kiro.bus"
        className={`${markClassName} ${className}`}
      />
    );
  }
  return (
    <span className={`inline-flex items-center gap-2.5 ${className}`}>
      <img src={LogoMark} alt="" aria-hidden className={markClassName} />
      {/* wordmark + badge · relative 让 badge 绝对定位到 wordmark 右上，不占 flex 主轴宽度
          badge 的 h 跟 wordmark 保持一致比例（约 5:3.5）· mr-4 给 badge 留右侧溢出空间 */}
      <span className={`relative inline-block ${showBadge ? "mr-4" : ""}`}>
        <img
          src={LogoWordmark}
          alt="Kiro.bus"
          className={`${wordmarkClassName} block`}
        />
        {showBadge && (
          <img
            src={HyperBadge}
            alt=""
            aria-hidden
            className="pointer-events-none absolute -right-4 top-0 h-4 w-auto -translate-y-1/3 object-contain drop-shadow-sm"
          />
        )}
      </span>
    </span>
  );
}
