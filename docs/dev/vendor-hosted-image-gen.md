# 厂商 hosted 生图

> **结论先行**：在 **OpenAI / xAI OAuth 的 Responses** 路径挂厂商 hosted `image_generation`；`image_generation_call` 的 base64 **落工作区文件夹**，Agent 迭代筛选后再用「发消息挂图」交给用户。**本批不做**发消息挂图 / 气泡 gallery（另分支 `feature/bot-images-in-chat`）。不另造本地 generate。候选：`正在干什么/06-XBot/候选/厂商hosted生图.md`。

## 背景

政东：GPT / Grok OAuth hosted 有生图；生成要单独考虑，不跟「Bot出图到聊天」绑死。样例：多轮生成在文件夹里迭代，最后只交成果——反例是每张都刷进气泡。

## 怎样算对

1. **挂 tool**：ChatGPT OAuth / xAI OAuth 且 protocol=`responses`、模型开启 hosted 生图时，Requests 的 `tools` 含 `{"type":"image_generation"}`（可带厂商允许的 `model` / `action` 等选项）。密钥 Completions 路径本批不强制。
2. **落盘**：解析 `image_generation_call`（含 stream 完成项）→ 解 base64 → 写入该 Bot 工作区子目录（建议 `~/.xbot/work/<bot>/generated/`，随机名 + 嗅探扩展；与 `paste/` 分目录）。工具/Part 文本给出**相对工作区路径**，供 Agent 后续 read / 挂图。
3. **不自动进气泡**：本批不改发消息工具、不改 `botBubble` gallery、不把生成图自动塞进 transcript `Images`。
4. **设置可配**：供应商/模型详情可开关 hosted 生图；OpenAI 可配 tool 级生图模型（如 `gpt-image-2.5-flare` / `gpt-image-2.5-sunburst`）；xAI 以开关为主（Imagine 由厂商选型）。配置进现有 `providers.json` ModelSpec，勿另起配置文件。
5. **失败诚实**：厂商拒权 / 空 result / 解码失败 → PartHosted/错误文案，不假装已出图（ADR 0023）。
6. **测试**：`go test` 相关包绿；合入本地 main（不 push）→ Mini 8788 → 手测。

## 与挂图批解耦

| 本批（hosted 生图） | 另批（Bot出图到聊天） |
|---|---|
| Responses 挂 `image_generation` | 扩现有发消息工具参数挂 paste/路径 |
| base64 → `work/<bot>/generated/` | 气泡读 `m.images` → gallery |
| 不碰 message_bot / botBubble | 不碰 hosted 生图 / generate |

依赖：挂图批合入后，Agent 才能把 `generated/` 里筛好的图挂进气泡；本批可先独立验收「文件夹里有图 + 路径回报」。

## 技术要点（给编码）

### 复用

- OAuth / Responses 传输：`internal/provider/responses.go`（已有 `web_search` / xAI `x_search`+`code_interpreter`）。**不要**重做 OAuth。
- 工作区：`internal/work`（`BotDir`）；新建 `generated` 常量与写文件助手，类比 `paste` 的命名/上限思路，但目录独立。
- 进度：`PartHosted` + `HostedStep{Name:"image_generation",...}`；完成时 Text 含落盘路径。
- 配置：扩展 `ModelSpec`（如 `hosted_image_gen *bool`、`image_gen_model string`）+ Resolve/Endpoint + `providers.js` 详情一行开关/选择；对齐 `hosted_web_search` 模式。

### Responses

- 当 Endpoint/Request 标明开启 hosted 生图时追加 tool（与 hosted search 可并存；勿与本地 fake generate 混淆）。
- OpenAI：`tools: [{type:"image_generation", model: <配置或默认>}]`；默认取配置，空则厂商默认。
- xAI：`tools: [{type:"image_generation"}]`（可选 `action`）；勿塞 OpenAI 专属 size/quality 字段。
- 解析 `image_generation_call`：`result` base64（无 data-URL 前缀）→ 落盘；`prompt` 可进 HostedStep.Query 便于调试。stream 事件对齐现有 hosted 完成路径；未知类型仍降级文字。
- `Request` / binding：新增能力位（勿把生图绑死在 `HostedSearch`/`WebSearch` 上——搜索关时仍可生图）。

### 不做

- 本地 `generate_image` 工具 / 自研扩散
- 改发消息挂图、气泡 gallery（另候选）
- Collections Search；Claude 订阅 OAuth 生图
- push；改 CLAUDE.md / AGENTS.md / lock / 发版脚本
- 重做 xAI/ChatGPT OAuth

## 自检

- [x] 搜索关、生图开时仍挂 `image_generation`
- [x] 生成文件在 `generated/`，不进 `paste/`、不自动进气泡
- [x] OpenAI / xAI 路径各有测试夹具（mock Responses 含 `image_generation_call`）
- [x] 设置开关/模型写入 providers.json 并可读回
- [x] atlas/providers 一句回写 hosted 生图范围（可选薄）

## 交付

- 分支 `feature/vendor-hosted-image-gen`；本文件一并提交
- 报告 `~/.xbot-build/hosted-image-gen/encode-report.md`

## 本批验证

- 独立冒烟：`e2e/smoke/image_gen_test.go` 的 `TestHostedImageGenerationOAuth`，真实 serve / HTTP 设置与绑定 / vault OAuth 传输，厂商为 scripted；覆盖 ChatGPT 与 xAI、搜索关生图开、开关下一轮生效、路径回放、失败诚实及不自动挂图。
- 主人已确认沿用本批设置界面；浅/深色、开关保存和刷新读回已在本机 Chrome 检查，截图留仓外。
- 级别：分支实现与替身端到端通过；本次用户明确要求不合 main、不 push，覆盖上文验收第 6 项的本次操作范围。未达到已合入或主人日用通过，不标候选完成。
## 还没做的

主管复审、审美顾问截图复审、真实账号权限/实际生图、Mini 8788 手测、日用及线上性能验收。dev 保留，不移动 guard；挂图另线推进。
