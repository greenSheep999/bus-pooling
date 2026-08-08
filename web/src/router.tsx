import { createBrowserRouter } from "react-router-dom";
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
      { path: "/settings/downstream", element: <Downstream /> },
      { path: "/settings/webhook", element: <Webhook /> },
      { path: "/settings/api-keys", element: <ApiKeys /> },
      { path: "/settings/profile", element: <Profile /> },
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
