import { lazy, Suspense } from "react";
import { Navigate } from "react-router-dom";
import { useMe } from "@/api/hooks";

const Landing = lazy(() => import("./Landing"));

/** 无文案 spinner · 跟 router.PageFallback 视觉一致 · 中英不闪 */
function GateSpinner() {
  return (
    <div className="grid min-h-[40vh] place-items-center" role="status" aria-busy="true">
      <span className="inline-block size-5 animate-spin rounded-full border-2 border-hairline border-t-brand-strong" />
    </div>
  );
}

/** 根路由分流 · 已登录 → /overview（AppLayout 包裹）· 未登录 → Landing（独立布局）
 *
 *  为什么分流：
 *   - 已登录用户根本用不到 Landing · 直接跳工作台（Overview）省一次点击
 *   - 未登录用户不能进 AppLayout（它内部会拉 /me / 库存 / 钱包 · 全 401 崩）
 *   - Landing 有自己的极简 header · 不需要 AppLayout */
export default function RootGate() {
  const { data: me, isPending } = useMe();

  if (isPending) return <GateSpinner />;
  if (me) return <Navigate to="/overview" replace />;
  return (
    <Suspense fallback={<GateSpinner />}>
      <Landing />
    </Suspense>
  );
}
