import { Link, Outlet } from "react-router-dom";
import { PublicControls } from "@/components/PublicControls";
import LogoMark from "@/assets/logo/mark.svg";

export default function AuthLayout() {
  return (
    <div className="relative min-h-dvh bg-bg bg-glow-t">
      <Link
        to="/"
        className="absolute left-6 top-6 flex items-center gap-2.5 sm:left-8"
      >
        <img src={LogoMark} alt="" className="size-7 rounded-lg" />
        <span className="text-body-lg font-semibold tracking-tight">bus-pooling</span>
      </Link>

      {/* 主题 + 语言切换 · 未登录也能用（车主要求） */}
      <div className="absolute right-4 top-4 sm:right-6">
        <PublicControls />
      </div>

      <div className="grid min-h-dvh place-items-center px-6">
        <Outlet />
      </div>
    </div>
  );
}
