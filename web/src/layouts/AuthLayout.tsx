import { Link, Outlet } from "react-router-dom";
import LogoMark from "@/assets/logo/mark.svg";

export default function AuthLayout() {
  return (
    <div className="relative min-h-dvh bg-bg bg-glow-t">
      <Link to="/" className="absolute left-6 top-6 flex items-center gap-2.5 sm:left-8">
        <img src={LogoMark} alt="" className="size-7 rounded-lg" />
        <span className="text-body-lg font-semibold tracking-tight">bus-pooling</span>
      </Link>

      <div className="grid min-h-dvh place-items-center px-6">
        <Outlet />
      </div>
    </div>
  );
}
