import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

import { resolve } from "path";

export default defineConfig({
  plugins: [react()],
  build: { outDir: resolve(__dirname, "../internal/web/dist") },
  server: { proxy: { "/v1": "http://localhost:7433" } },
});
