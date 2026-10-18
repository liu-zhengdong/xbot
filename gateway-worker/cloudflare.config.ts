import { bindings, defineConfig, exports } from "cf/config";
export default defineConfig({
  worker: {
    name: "xbot-gateway",
    entrypoint: "./src/index.ts",
    compatibilityDate: "2026-10-01",
    domains: ["xbot.babelgo.cn"],
    exports: { Mailbox: exports.durableObject({ storage: "sqlite" }), Directory: exports.durableObject({ storage: "sqlite" }) },
    env: {
      MAILBOX: bindings.durableObject({ worker: "xbot-gateway", exportName: "Mailbox" }),
      DIRECTORY: bindings.durableObject({ worker: "xbot-gateway", exportName: "Directory" }),
      MAX_INSTALLS: bindings.text("1000"),
    },
  },
});
