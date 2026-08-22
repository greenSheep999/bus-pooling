import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";
import { router } from "./router";
import { Toaster } from "./components/ui/toaster";
import { initTheme } from "./lib/theme";
import "./i18n"; // 初始化 i18next · 只 import 即可（副作用）
import "./index.css";

// 在 React mount 前先同步应用主题 · 避免刷新时闪一下浅色
initTheme();

// **缓存策略**（issues-log I-15 · 生产延迟排查 2026-08-22）:
//
// 症状:用户切 tab 每次都触发 refetch · 每个 API 跨海 300ms+ · 感知"卡"。
// 分析:staleTime 30s 太短 · 切 tab 40 秒回来所有 hook 都 refetch · 8 个 API 并发。
//
// 调整:
//   - staleTime 30s → 5min:切 tab 回来 · React Query 认为数据仍新鲜 · 不发 request
//   - gcTime 保 default(5min)→ 30min:后台 tab 掉 tab 后 · 缓存留更久 · 回来时立即渲染
//   - refetchOnWindowFocus:false 保留(避免用户切窗口就 refetch)
//
// **哪些数据仍需要实时**:hooks.ts 里单独设 refetchInterval 的(如 stock 60s)不受影响 ·
// 用户主动操作(拉号/建车)后 · 走 invalidateQueries 强制刷 · 不依赖 staleTime。
const qc = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 60_000, // 5 分钟 · 切 tab 回来不 refetch
      gcTime: 30 * 60_000,   // 30 分钟 · 后台 tab 保留缓存
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

async function boot() {
  // VITE_USE_MOCK: "1" = MSW（假数据 · 独立开发前端用）· 默认关，走 vite proxy 到真后端
  const useMock = import.meta.env.VITE_USE_MOCK === "1";
  if (import.meta.env.DEV && useMock) {
    const { worker } = await import("./mocks/browser");
    await worker.start({ onUnhandledRequest: "bypass", quiet: true });
    console.info("[bus-pooling] MSW 已启用 · 走假数据");
  } else if (import.meta.env.DEV) {
    console.info("[bus-pooling] 真后端模式 · fetch /api/* → :8080");
  }

  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <QueryClientProvider client={qc}>
        <RouterProvider router={router} />
        <Toaster />
      </QueryClientProvider>
    </StrictMode>,
  );
}

boot();
