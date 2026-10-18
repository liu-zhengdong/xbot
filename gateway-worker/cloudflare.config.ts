import { defineConfig } from "cf/config";
export default defineConfig({
  worker: {
    name: "xbot-gateway",
    entrypoint: "./src/index.ts",
    compatibilityDate: "2026-10-01",
  },
});
