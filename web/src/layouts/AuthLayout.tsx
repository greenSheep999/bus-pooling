import { Link, Outlet } from "react-router-dom";
import { PublicControls } from "@/components/PublicControls";
import { BrandLogo } from "@/components/BrandLogo";
import { DocumentMeta } from "@/components/DocumentMeta";

export default function AuthLayout() {
  return (
    <div className="relative min-h-dvh bg-bg bg-glow-t">
      <DocumentMeta />
      <Link
        to="/"
        className="absolute left-6 top-6 flex items-center sm:left-8"
      >
        <BrandLogo />
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
