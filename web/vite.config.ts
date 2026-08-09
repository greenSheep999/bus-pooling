import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  server: {
    // 3100 而不是 3000 —— 本机常有别的 next / vite 项目占 3000
    // 覆盖用 --port 或 .env.local 里的 VITE_PORT（vite 会自动读）
    port: 3100,
    strictPort: false,
    proxy: { "/api": { target: "http://localhost:8080", changeOrigin: true } },
  },
});
