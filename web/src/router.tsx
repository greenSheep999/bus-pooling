import { createBrowserRouter, Navigate } from "react-router-dom";
import AppLayout from "./layouts/AppLayout";
import AuthLayout from "./layouts/AuthLayout";
import Overview from "./pages/Overview";
import Buses from "./pages/Buses";
import BusDetail from "./pages/BusDetail";
import Extract from "./pages/Extract";
import Prices from "./pages/Prices";
import Dispatch from "./pages/Dispatch";
import Docs from "./pages/Docs";
import WalletPage from "./pages/WalletPage";
import Settings from "./pages/Settings";
import Preferences from "./pages/Preferences";
import Downstream from "./pages/Downstream";
import Webhook from "./pages/Webhook";
import ApiKeys from "./pages/ApiKeys";
import Profile from "./pages/Profile";
import Login from "./pages/Login";
import Register from "./pages/Register";

export const router = createBrowserRouter([
  {
    element: <AppLayout />,
    children: [
      { path: "/", element: <Overview /> },
      { path: "/buses", element: <Buses /> },
      { path: "/buses/:id", element: <BusDetail /> },
      { path: "/extract", element: <Extract /> },
      { path: "/prices", element: <Prices /> },
      { path: "/dispatch", element: <Dispatch /> },
      { path: "/docs", element: <Docs /> },
      { path: "/wallet", element: <WalletPage /> },

      /* 账号本身不是一种设置 → /me（跟 API 的 GET /api/me 同名）
         设置是主入口 → /settings 索引页，下面挂三类设置 */
      { path: "/me", element: <Profile /> },
      { path: "/settings", element: <Settings /> },
      { path: "/settings/preferences", element: <Preferences /> },
      { path: "/settings/downstream", element: <Downstream /> },
      { path: "/settings/webhook", element: <Webhook /> },
      { path: "/settings/api-keys", element: <ApiKeys /> },
      /* 旧路径 · 早期把账号页放在 settings 下，留个跳转别让存过书签的人撞空白页 */
      { path: "/settings/profile", element: <Navigate to="/me" replace /> },
    ],
  },
  {
    element: <AuthLayout />,
    children: [
      { path: "/login", element: <Login /> },
      { path: "/register", element: <Register /> },
    ],
  },
]);
