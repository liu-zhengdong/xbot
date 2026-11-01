---
kind: adr
guard: owner
status: Proposed
date: 2026-11-01
---
# 0047 MCP 经实例 CLI 进入领域方法，退役模型适配

## 背景

[ADR 0046](0046-process-owned-mcp-sessions.md) 已把真实会话、通知和原 Jobs 延续到进程资源；2a 的独立真实入口通过。MCP 仍经 Link 的工具名/schema/pending/load 与主、子代理 schema fallback 执行，CLI 只有离线配置。要让真实 Shell 使用同一会话，必须提供实例操作入口，并在同一切换中删除旧模型 MCP 入口。

具体调用方、已核源码和验收见 [同份 dev 的 2b 节](../dev/cli-platform-entry.md#批-2b实例-mcp-cli-与旧模型入口退役)。不重新审候选/路线，不扩大为所有平台工具退役。

## 决定

1. `cliops.ParseMCP(argv)` 是 cmd/research 共用的纯 argv parser，不读文件、环境或网络。cmd 负责受限 args-file/stdin、单 JSON object/EOF 和信号/可选 timeout。在线 MCP/help 在 root 默认化、EnsureDefault、HoldKeys 前分流；保留 splitGlobal 原始 root 是否显式。
2. `BotResources` 拥有 MCP list/add/remove/tools/describe/call 领域方法。source endpoint 的 `/v1/mcp/*` 与 human owner-authenticated `/api/bots/{bot}/mcp/*` 只解析/授权/调用同方法；不 spawn CLI、不另建 K/Manager/Session。Agent self 以原 endpoint ref 为权威，不按 XBOT_BOT 查找；human 显式 http+bot 使用现 cookiejar/session.authed，未显式 root 不传默认 root 或 XBOT_ROOT 作认证。
3. MCP HTTP client 按 caller ctx、默认 timeout=0，不复用 profile 10s/message 2m；Proxy:nil、拒重定向，所有新 source 路由复用现 root/browser/Host 边界。默认配置 scope=global，bot override 显式；配置只在短 route 完成实际读写，放 route 后 reconcile，global 刷全部 refs、bot 刷本 ref。保存和 ready 分开。
4. research 仅准当前 BIN/精确受控 XBOT_BIN 的单 POSIX 直调。窄 lexer 保引号语义、拒重定向/串联/替换/未知形式；原 shellWords 不作授权 parser。业务只读为 self list/tools/describe/help；call 必须 readonly，core 从一次选定的同 slot/session 发现精确远端工具 hint=true，重核后仍调用该 session，换代失败不转发新实例。legacy 与 continuable 的 readOnlyShell 同接，保 Descriptor 策略、模型路由/overflow fit 和每 Local admission。
5. 复用现 ToolSpec/annotations Raw 和原始调用结果，不造第二协议结构。网络、解析、base64 解码在 route/ops/host 锁外；结果经有界 MCP 投影，stdout JSON 上限64KiB。已知图片用现 WriteGenerated，resource_link 只保 URI、不取远端；大文本、embedded/blob、unknown、structured/raw 保在本机有界产物，回 preview/file/access:local，不 stdout base64。实际 workspace 取值与写完在同短 route，普通 CurrentBot 返回不作写入 fence。薄 WriteResult 独占0600，不借 Jobs/新增任务账。isError 保内容输出且 CLI 非0，与 transport 失败分开。
6. 此切换删除 MCP adapter/schema/Owns/prefix/pending/load/sync/status 及主、legacy 子、continuable 子的 MCP fallback；保历史 transcript 可读，不执行旧兼容入口。非 MCP 的 Tools 门、Link/目录与其它消费者继续工作。商店 probe 迁 bounded 临时 core Session，注入此次待保存值、显式 provider predicate，精确 browser_navigate/browser_close 并检查 raw isError，保真实鉴权探测。

## 替代范围与未覆盖

[ADR 0035](0035-unified-tool-registry.md) 中 MCP 经模型注册/代理门的部分在本刀实际切换后由本决定替代；其余 native/非 MCP 的工具注册和 Tools 门不退。[ADR 0046](0046-process-owned-mcp-sessions.md) 的唯一会话、通知、未知副作用不重试、共享 Jobs、source listener 与原 Event 路径保持。

完整 runtime rename、远端 runner 回连、人用 PATH 安装、OAuth/Registry、其它平台 CLI 迁移不在本刀。新 CLI 不靠隐藏冷 schema 或失败后再起旧 adapter 兜底。

## 验证级别

先最窄真实二进制 self add→list→tools→describe→call，再由独立验证方把2a业务 driver迁CLI，原会话计数、gate、原Job取消/单Event和source lease断言不变。实现方只做受影响最小unit/race与必要边界；不全仓、不真模型、不改独立期望。当前仅具体接口草图，待24审实际新增切口后实施，不等于2b产品通过。
