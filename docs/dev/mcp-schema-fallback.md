# MCP schema 被拒时降级

候选真源：主人 Obsidian `正在干什么/06-XBot/候选/MCP schema 被拒时降级.md`。Ultra 2026-10-27 拍板；先复现再改。

## 对照回顾

MCP 工具 schema 被服务商拒绝时，整 Bot 每轮 400，无法继续。要对「schema 被拒」做明确降级，而不是整轮崩掉。

现状线索：`provider.NewStatusError(400, invalid tool schema)` 在 `fit_test` 里被当成「非 overflow、不重试」；MCP 工具 schema 来自 `internal/link/mcp.go` 的 `inputSchema`。

## 实现规格与怎样算对

| 项 | 规格 / 验收 |
|---|---|
| 先复现 | 写出可复现用例（单测或夹具）：某 MCP 工具 schema 触发 provider 400 / invalid tool schema，确认现状行为。 |
| 降级 | 被拒时有明确降级：例如该工具改为无参数 `{}` / 空 object schema 再试，或从本轮工具表摘掉并给模型可读错误；**不得**每轮无差别硬撞同一 400。 |
| 可读 | 降级路径对模型/轨迹可理解（错因或「已降级无参数」），可恢复下一轮。 |
| 范围 | 只处理 schema 被拒类错误；勿把上下文 overflow、鉴权失败误判成 schema 降级。 |

样例正：复现路径红→绿；被拒后同轮或紧邻重试能继续对话。

样例反：吞掉所有 400；无限重试同一坏 schema；误伤 overflow 路径。

## 实现切法

采用临时摘除定义：明确点名时只摘该 MCP 工具；未点名时摘掉本请求 MCP 工具，并向模型说明无法定位的停用范围。只对带 HTTP 400 状态的明确工具 schema 错误增加一次重试，复用模型调用、权限、上下文 fit 与轨迹路径。跨轮使用最多 4096 条的进程内冷却缓存，按 Bot、绑定模型、工具名和 schema 哈希隔离；30 分钟、schema/模型变化或重启允许重试原 schema。实现现状以 [生态](../atlas/ecosystem.md) 为准。

## 非目标

不重做 MCP 协议栈；不 push；不提手测。相关包 `go test` 一遍绿。

## 还没做的

独立复审、合入、8788、docs/dev 毕业。
