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

const qc = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 30_000, refetchOnWindowFocus: false, retry: 1 },
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
