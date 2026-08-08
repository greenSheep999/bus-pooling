import { Link, Outlet } from "react-router-dom";

export default function AuthLayout() {
  return (
    <div className="relative min-h-dvh bg-bg bg-glow-t">
      <Link to="/" className="absolute left-8 top-6 flex items-center gap-2.5">
        <span className="grid size-7 place-items-center rounded-lg bg-brand font-semibold text-white">
          K
        </span>
        <span className="text-body-lg font-semibold">bus-pooling</span>
      </Link>

      <div className="grid min-h-dvh place-items-center px-6">
        <Outlet />
      </div>
    </div>
  );
}
