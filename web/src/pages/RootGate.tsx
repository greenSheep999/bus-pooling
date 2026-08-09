import { lazy, Suspense } from "react";
import { Navigate } from "react-router-dom";
import { useMe } from "@/api/hooks";

const Landing = lazy(() => import("./Landing"));

/** 根路由分流 · 已登录 → /overview（AppLayout 包裹）· 未登录 → Landing（独立布局）
 *
 *  为什么分流：
 *   - 已登录用户根本用不到 Landing · 直接跳工作台（Overview）省一次点击
 *   - 未登录用户不能进 AppLayout（它内部会拉 /me / 库存 / 钱包 · 全 401 崩）
 *   - Landing 有自己的极简 header · 不需要 AppLayout */
export default function RootGate() {
  const { data: me, isPending } = useMe();

  if (isPending) {
    return <div className="p-6 text-label text-fg-tertiary">加载中…</div>;
  }
  if (me) {
    return <Navigate to="/overview" replace />;
  }
  return (
    <Suspense fallback={<div className="p-6 text-label text-fg-tertiary">加载中…</div>}>
      <Landing />
    </Suspense>
  );
}
