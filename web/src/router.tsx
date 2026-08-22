import { lazy, Suspense } from "react";
import { createBrowserRouter, Navigate } from "react-router-dom";
import AppLayout from "./layouts/AppLayout";
import AuthLayout from "./layouts/AuthLayout";
import RootGate from "./pages/RootGate";

// 每页独立 chunk · 首屏只加载路由匹配到的页
// 注：Landing 在 RootGate 内 lazy 加载 · 这里不再重复 import
const Overview = lazy(() => import("./pages/Overview"));
const Buses = lazy(() => import("./pages/Buses"));
const BusDetail = lazy(() => import("./pages/BusDetail"));
const Extract = lazy(() => import("./pages/Extract"));
const Prices = lazy(() => import("./pages/Prices"));
const Dispatch = lazy(() => import("./pages/Dispatch"));
const Docs = lazy(() => import("./pages/Docs"));
const WalletPage = lazy(() => import("./pages/WalletPage"));
const Settings = lazy(() => import("./pages/Settings"));
const Preferences = lazy(() => import("./pages/Preferences"));
const Downstream = lazy(() => import("./pages/Downstream"));
const Webhook = lazy(() => import("./pages/Webhook"));
const ApiKeys = lazy(() => import("./pages/ApiKeys"));
const AccountSettings = lazy(() => import("./pages/AccountSettings"));
const Profile = lazy(() => import("./pages/Profile"));
const Login = lazy(() => import("./pages/Login"));
const Register = lazy(() => import("./pages/Register"));
const JoinByLink = lazy(() => import("./pages/JoinByLink"));
const Community = lazy(() => import("./pages/Community"));
const Invite = lazy(() => import("./pages/Invite"));
const Legal = lazy(() => import("./pages/Legal"));
const StatusPage = lazy(() => import("./pages/Status"));
const Notifications = lazy(() => import("./pages/Notifications"));

// 页级 Suspense fallback · 无文案 · 一个轻量 spinner 避免中英不一致
// （单个页面自己会拿 skeleton 骨架显示 data loading · 这里只是 chunk 加载态）
function PageFallback() {
  return (
    <div className="grid min-h-[40vh] place-items-center" role="status" aria-busy="true">
      <span className="inline-block size-5 animate-spin rounded-full border-2 border-hairline border-t-brand-strong" />
    </div>
  );
}

function withSuspense(el: React.ReactNode) {
  return <Suspense fallback={<PageFallback />}>{el}</Suspense>;
}

export const router = createBrowserRouter([
  /* 根路径独立分组 · 已登录 → Overview（AppLayout 包住）· 未登录 → Landing（自带 header）
     RootGate 内部按 useMe 分流 · 未登录不套 AppLayout（否则头栏会去拉 me / stock 崩） */
  { path: "/", element: withSuspense(<RootGate />) },
  {
    element: <AppLayout />,
    children: [
      { path: "/overview", element: withSuspense(<Overview />) },
      { path: "/buses", element: withSuspense(<Buses />) },
      { path: "/buses/:id", element: withSuspense(<BusDetail />) },
      { path: "/extract", element: withSuspense(<Extract />) },
      { path: "/prices", element: withSuspense(<Prices />) },
      { path: "/dispatch", element: withSuspense(<Dispatch />) },
      { path: "/docs", element: withSuspense(<Docs />) },
      { path: "/wallet", element: withSuspense(<WalletPage />) },
      /* 通知全部页 · 铃铛"查看全部通知" 落地页 */
      { path: "/notifications", element: withSuspense(<Notifications />) },
      /* promo 跳转目标（config.promo.items 里配的 to） */
      { path: "/community", element: withSuspense(<Community />) },
      { path: "/invite", element: withSuspense(<Invite />) },

      /* 账号本身不是一种设置 → /me（跟 API 的 GET /api/me 同名）
         设置是主入口 → /settings 索引页，下面挂三类设置 */
      { path: "/me", element: withSuspense(<Profile />) },
      { path: "/settings", element: withSuspense(<Settings />) },
      { path: "/settings/preferences", element: withSuspense(<Preferences />) },
      { path: "/settings/downstream", element: withSuspense(<Downstream />) },
      { path: "/settings/webhook", element: withSuspense(<Webhook />) },
      { path: "/settings/api-keys", element: withSuspense(<ApiKeys />) },
      { path: "/settings/account", element: withSuspense(<AccountSettings />) },
      /* 旧路径 · 早期把账号页放在 settings 下，留个跳转别让存过书签的人撞空白页 */
      { path: "/settings/profile", element: <Navigate to="/me" replace /> },
    ],
  },
  {
    element: <AuthLayout />,
    children: [
      { path: "/login", element: withSuspense(<Login />) },
      { path: "/register", element: withSuspense(<Register />) },
      /* 邀请链接落地页 · 未登录也能打开（引导登录后回跳继续加入） */
      { path: "/join/:code", element: withSuspense(<JoinByLink />) },
    ],
  },
  /* 法务页 · 未登录也能看 · 自带 layout（不套 AppLayout） */
  { path: "/legal", element: withSuspense(<Legal />) },
  { path: "/legal/:slug", element: withSuspense(<Legal />) },
  /* 上游状态页 · 公开 · 自带 layout */
  { path: "/status", element: withSuspense(<StatusPage />) },
  { path: "/status/:anonId", element: withSuspense(<StatusPage />) },
]);
