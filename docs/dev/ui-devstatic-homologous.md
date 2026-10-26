# 前端同源 · DEV_STATIC 与 pipe 同读

2026-10-27 · 执行落地（关键点）

## 结论

权威是「当前 serve 进程实际对外提供的那份 UI」。`static()`（本机页）与 `uiStatic()`（pipe `/api/ui/static`）共用同一解析：有 `XBOT_DEV_STATIC` 时两边读该目录；否则两边读 embed。

## 为什么

ADR 0029 去掉云上业务壳后，本机开发常开 DEV_STATIC 吃磁盘，pipe 仍只读 embed → 未重建时本机与手机漂。同源要的是进程对外那一份，不是「永远只 embed」。

## 钉死

- Tip：DEV_STATIC 开时标明（`dev-static` 或磁盘摘要），勿假装纯 embed tip。
- 测试：删「pipe 永不读 DEV_STATIC」；断言本机路由与 uiStatic 同源。
- ADR 0029 / atlas：开发覆盖开启时 pipe 与本机页同源；关闭时仍二进制内嵌。
- **禁止** `sync-app-from-xbot` 发业务壳；不反代 8788。

## 验收

DEV_STATIC 开着：改 `internal/web/static/app.js` 不重建 → 本机 `/app.js` 与认证后 `/api/ui/static/app.js` 字节一致（或同 tip/摘要）。

## 实现约束

- 复用现有 owner 认证与 pipe 传输；两个入口共用 `staticAsset` 的路径校验、来源选择、读取和响应逻辑。Server 首次使用时固定配置，磁盘正文每次读取；本机的大字体不受 pipe 700 KiB 预算限制。
- 开发覆盖用 `os.Root` 约束路径，禁止通过符号链接读取配置目录外文件；缺失文件回 404。读取前和实际读取量都检查预算，超限回 413。
- `dev-static` 表示开发覆盖模式；资源 ETag 按实际正文生成。未覆盖保持构建版本 / `dev-<digest>`。磁盘多文件编辑不提供跨请求原子快照。

## 验证与级别

回归用例：`internal/web/ui_static_test.go` 的 `TestUIStaticSameSource`（开/关覆盖、同一 handler 下改文件、缓存重验证）与 `TestUIStaticDiskBoundaries`（认证、路径、类型、符号链接、大小边界）。执行证据与逐项自检见 `~/.xbot-build/ui-devstatic-homologous/encode-report.md`。

## 还没做的

- 分支编码交付后，由执行官安排独立复审与合入。
- 使用部署后的真实 pipe 在手机公网验证修改文件后刷新同源，以及主人日用验收；本分支的本机测试不替代这些结论。
