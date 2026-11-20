import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

const backend = process.env.WARPSTASH_BACKEND ?? "http://localhost:8080"

export default defineConfig({
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react(), tailwindcss()],
  resolve: { alias: { "@": resolve(import.meta.dirname, "./src") } },
  server: {
    proxy: {
      "/api": backend,
      "/upload": backend,
      // Page loads of /f/ (the burn interstitial) render the SPA from Vite: the backend's index.html
      // points at its own built /assets, which Vite doesn't serve. Downloads and previews go to Go.
      "/f/": {
        target: backend,
        bypass: (req) =>
          req.headers.accept?.includes("text/html") &&
          !/[?&](raw|inline|view|burn)=1/.test(req.url ?? "")
            ? "/index.html"
            : undefined,
      },
    },
  },
  build: { outDir: "../backend/web/dist", emptyOutDir: true },
  test: { include: ["src/**/*.test.ts"] },
})
