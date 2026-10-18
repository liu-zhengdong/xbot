import { defineConfig } from "vite";
import { cloudflare } from "@cloudflare/vite-plugin";
export default defineConfig({ plugins: [cloudflare()], server: { port: 8787, host: "127.0.0.1", watch: null } });
