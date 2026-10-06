// @ts-check
import { defineConfig } from "astro/config";

import tailwindcss from "@tailwindcss/vite";

// https://astro.build/config
export default defineConfig({
  build: {
    format: "file",
  },
  vite: {
    envDir: "../",
    envPrefix: ["PUBLIC_", "WARPSTASH_"],
    plugins: [tailwindcss()],
    server: {
      proxy: {
        "/api": "http://localhost:8080",
        "/f/": "http://localhost:8080",
        "/d/": "http://localhost:8080",
        "/upload": "http://localhost:8080",
      },
    },
  },
});
