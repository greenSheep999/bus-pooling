import { useEffect, useState } from "react";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";
import { usePromos } from "@/api/hooks";
import { SplitFlapCountdown } from "@/components/ui/split-flap";

/** 顶部活动条 · 登录前后共用（AppLayout 和 Landing 都挂它）
 *  多条时 6s 轮播 · 有 countdown_until 才显示翻牌倒计时 · 没条目返回 null 不占位 */
export function PromoBar() {
  const { data } = usePromos();
  const items = data?.items ?? [];
  const [i, setI] = useState(0);

  // 条目数变了（后端下线一条）时把游标夹回范围内·否则会闪空白
  useEffect(() => {
    if (items.length > 0 && i >= items.length) setI(0);
  }, [items.length, i]);

  useEffect(() => {
    if (items.length <= 1) return;
    const id = setInterval(() => setI((v) => (v + 1) % items.length), 6000);
    return () => clearInterval(id);
  }, [items.length]);

  if (items.length === 0) return null;
  const p = items[Math.min(i, items.length - 1)];

  const body = (
    <>
      <span className="truncate text-center">{p.text}</span>
      {p.countdown_until && (
        <SplitFlapCountdown
          until={p.countdown_until}
          serverNow={data?.server_now}
          className="shrink-0"
        />
      )}
      {p.to && <ArrowRight className="size-3.5 shrink-0" />}
    </>
  );

  return (
    <div className="bg-brand text-white">
      {p.to ? (
        <Link
          to={p.to}
          className="page-container flex items-center justify-center gap-2 py-1.5 text-label font-medium hover:opacity-90"
        >
          {body}
        </Link>
      ) : (
        /* 无跳转 · 纯公告（不给 hover 也不给箭头 · 别让用户以为能点） */
        <div className="page-container flex items-center justify-center gap-2 py-1.5 text-label font-medium">
          {body}
        </div>
      )}
    </div>
  );
}
