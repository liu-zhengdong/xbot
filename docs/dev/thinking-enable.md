# 思考回传与启用

> 来源：Obsidian `候选/思考回传与启用.md` + 主管 2026-10-26「可干」。做完毕业进 atlas / 必要时 ADR / guard，删本文。

## 背景
DeepSeek + 工具时若不回传 `reasoning_content`，开 thinking 会炸，所以现强制 `thinking: disabled`（`thinkingFor` 对 DeepSeek 默认 off；配置只允 `off`/`omit`）。政东要打开思考，但**过程不对用户展示**。

## 目标 / 怎样算对
1. **内核多轮回传** assistant 的 `reasoning_content`（及 Anthropic thinking / Responses reasoning 等价字段），带工具的后续请求不炸。
2. **配置支持** `thinking: on`（或等价启用值）；默认可开（合理默认：DeepSeek 等需要回传的模型可默认 on；其它 endpoint 不因此乱塞未知字段）。
3. **不对用户展示**：聊天气泡 / 前台流式 UI **不渲**思考全文；可进内部 transcript / 调试通道。
4. 假开关不算过：配置 on 时请求侧真正启用，且多轮工具调用实测稳定。

## 推理档位（主管 2026-10-26 补充约束）

- 默认 `reasoning_effort=low`；Flash 的真实可选档只有 `low/high/max`，不接受 medium 等伪档。
- 模型条目支持 `reasoning_effort`，CLI `--reasoning-effort low|high|max`；省略时保留已有配置，未配置的思考请求实际发 low。
- Completions 思考请求用 `reasoning_effort`，Responses 显式 on 用原生 `reasoning.effort`；Anthropic 沿原生 thinking 预算与签名回放，不塞 Completions 字段。
- 思考全文不进入气泡、前台 SSE 或公开 transcript API。

## 底线
- 思考过程不进聊天气泡。
- 不改无关供应商 UI 大改；不改 CLAUDE.md / AGENTS.md / lock。
- 不合 main、不重启 8788（由执行官收尾）。

## 现状锚点（编码起点）
- `internal/config/providers.go`：`validThinking` 仅 `""|"off"|"omit"`。
- `internal/provider/openai.go`：`thinkingFor` 对 DeepSeek 默认 disabled，注释写明因未回传 `reasoning_content`。
- `PartReasoning` 已存在；Anthropic 侧有 thinking 签名回放测试（`internal/provider/anthropic_test.go`）。缺的是 Completions/DeepSeek 工具轮回传与 `on` 开关。

## 样例
- 正：DeepSeek + 工具多轮，thinking on，请求带上轮 reasoning，对话稳定；用户只看到正常回复气泡。
- 反：把思考全文渲进气泡；或配置写了 on 但请求仍强制 disabled；或开了 on 第二轮工具调用 400。

## 验证入口

- `internal/provider/thinking_test.go`：请求开关、low/high/max、omit 回传、缓冲上限、脱敏与跨协议投影。
- `internal/config/providers_test.go`：配置落盘、保留档位、非法值与手改文件校验。
- `internal/web/thinking_stream_test.go` / `thinking_browser_test.go`：SSE、公开 transcript 和本机 Chrome 气泡隔离。
- `internal/web/export_thinking_test.go`：三协议 × tools=0/1 的 JSONL/Markdown 导出均隔离 reasoning/VendorRaw，保留公开正文、步骤、引用与按开关显示的工具；内部段文件逐字节不变、重开后回放数据完整。`internal/bot/export_projection_test.go` 守投影不修改共享消息与幂等。
- `e2e/smoke/thinking_test.go`：真实 CLI / serve，三种协议连续两次工具调用，进程重启后再次回传；上游仅 scripted，按硬约束不调真模型。

## 还没做的

- 真实 DeepSeek 账号验收及用户日用验证；交执行官收尾，本分支不合 main、不重启 8788。
- 窗口估算（`compact.Estimate` / `fit`）只按 `PortableText` 算，回传的思考全文没计入；长思考多轮时仍靠既有溢出缩窗重试兜底。
- 日用确认后再毕业整理；不擅自改 guard 旅程或验收标准。
