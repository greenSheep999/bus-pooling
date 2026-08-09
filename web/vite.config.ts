import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  server: {
    port: 3100,
    strictPort: false,
    proxy: { "/api": { target: "http://localhost:8080", changeOrigin: true } },
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
