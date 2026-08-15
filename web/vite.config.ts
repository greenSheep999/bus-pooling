import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  server: {
    port: 3100,
    strictPort: false,
    // 用 127.0.0.1 显式走 IPv4 · localhost 在 macOS 会优先 IPv6 · 但后端只绑 IPv4(BP_ADDR=127.0.0.1:8080)
    // 别的进程占了 IPv6 8080(如 visualgo)时 localhost 会打错服务 · 返 404 迷惑
    // 8080 被别的项目占了(visualgo · macOS opcon-xps 冲突) · 换 8090
    proxy: { "/api": { target: "http://127.0.0.1:8090", changeOrigin: true } },
  },
  build: {
    // Vite 8 底层 Rolldown · codeSplitting API 拆 vendor
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: "react-vendor",
              test: /node_modules[\\/](react|react-dom|react-router-dom|scheduler)[\\/]/,
            },
            {
              name: "query",
              test: /node_modules[\\/]@tanstack[\\/]/,
            },
            {
              name: "form",
              test: /node_modules[\\/](react-hook-form|@hookform|zod)[\\/]/,
            },
            {
              name: "radix",
              test: /node_modules[\\/]@radix-ui[\\/]/,
            },
            {
              name: "charts",
              test: /node_modules[\\/](recharts|d3-.*|victory-vendor)[\\/]/,
            },
          ],
        },
      },
    },
    chunkSizeWarningLimit: 500,
  },
});
