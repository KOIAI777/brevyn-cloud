import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/app/",
  plugins: [react()],
  server: {
    port: 5174,
    proxy: {
      "/api": "http://127.0.0.1:4000",
      "/healthz": "http://127.0.0.1:4000",
      "/readyz": "http://127.0.0.1:4000"
    }
  }
});
