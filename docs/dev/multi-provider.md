# 多 Provider 与服务端工具

> **30 秒版**：先把 transcript 里的消息改成可回放的**内容块**，再按**协议**加适配器（Chat Completions / OpenAI Responses / Anthropic Messages），而不是再挂几个兼容 `base_url`。模型能力表写明支持 hosted `web_search` 的绑定走厂商搜索，且**本绑定不再**塞本地 `web_search`；不支持则行为与今天相同（SearchBackend / link 继续演进）。Hosted 结果在模型流里回来、落成内容块，**不进** C1 Jobs。批次：①内容块+加强 Completions → ②Responses+web_search/引用 → ③Anthropic → ④OAuth（OpenRouter→ChatGPT；Claude 订阅 OAuth 默认不做）。

政东已批三板（2026-10-23）：hosted 与本地 search **按绑定互斥**；批次顺序如上；Claude 订阅 OAuth 默认不做。草稿见仓外 `xbot-plans/multi-provider-sketch.md`。

本文写「做完长什么样」和怎么分批验收；不抄实现细节。落地后回写 [providers](../atlas/providers.md)、必要时补 ADR，并更新 guard 冒烟。

## 做完是什么样

贯穿例子：用户有一个 Bot，先绑 DeepSeek（Completions），聊了几轮含本地 `web_search`；再改绑 GPT（Responses，能力表有 hosted `web_search`）；再改绑 Claude（Messages）。旧对话仍可读；GPT 轮次里搜索出引用步骤；Claude 能跑通一轮服务端工具（若该模型有）。

| # | 指标 | 怎样算对 |
|---|---|---|
| 1 | 旧 transcript（纯字符串 Content）在升级后仍可投影给任意适配器 | 不丢行；Completions 路径对话行为与升级前同级冒烟绿 |
| 2 | 新消息以有序内容块落盘；原厂回放需要的字段可挂在块上（如 `vendor_raw`） | 同一绑定重放请求时，适配器能还原本厂认识的块；换厂时不认识的块变成文字摘要块，transcript 行数不少 |
| 3 | 配置能区分协议：`chat_completions` / `responses` / `anthropic_messages`（显式 `protocol` 或由已知厂商推断） | 绑 GPT 走 Responses；绑 DeepSeek 仍走 Completions；绑错协议有清晰配置错误，不静默当 Completions |
| 4 | **按绑定互斥搜索**：能力表写该模型支持 hosted `web_search` → 请求开厂商 hosted，且本绑定工具列表**不含**本地 `web_search`；不支持 → 工具列表与今天相同，SearchBackend/link 照旧 | 假模型/替身能断言工具列表；真 GPT 绑定下 UI 可见搜索步骤与引用（批 2） |
| 5 | Hosted 调用不进 C1 `Jobs`；本地工具仍走 C1（慢工具后台化、插话让出不变） | 一次 hosted 搜索：`shell-jobs/` / Jobs 表无对应项；transcript 有服务端工具块；本地 `web_fetch` 仍可进 Jobs |
| 6 | 换模重算 T/H；工具定义体积计入上下文（ADR 0015） | 换绑后下一轮上下文估算含新工具集；有测试或可观测日志 |
| 7 | 环境流：可用工具列表随绑定/能力变化才推（ADR 0021） | 仅换绑导致工具集变化时 env 更新含工具列表；同绑定重复开轮不重复刷 |
| 8 | DeepSeek thinking 规则仍按模 overlay（ADR 0020） | Completions 路径现有 thinking off/omit 行为不变 |
| 9 | OAuth（批 4）：令牌进现有 vault，可刷新；六条基本旅程在 OAuth 账号下冒烟绿 | OpenRouter→ChatGPT 路径可登录并用；Claude 订阅 OAuth **不提供**入口（默认不做） |
| 10 | Hosted 按次费用有记账字段（可先只写 trace/日志，UI 后补） | `trace.jsonl`（或等价）能区分 hosted 计费条目与本地工具 |

旅程对齐：六条基本旅程在批 1 结束后 Completions 绑定仍全绿；批 2 起对 GPT 绑定加「搜索出引用」专项冒烟；不借机改权限总开关语义（ADR 0019）。

## 现状（设计前提）

- 供应商边界在 `internal/provider/`：只有 Chat Completions（`openai.go`）；`Provider.Chat` 吃 `Message.Content string` + `ToolCalls`。
- `providers.json` 是多**兼容端点**，不是多原生协议；尚无 `protocol`、无模型能力表、无 hosted。
- 本地 `web_search` / `web_fetch` 在 Link / SearchBackend（默认 DuckDuckGo，可切 DeepSeek Search API）；结果包 `<untrusted_event>`。与「模型内置 search」无关。
- 本地工具 + MCP + C1（慢工具转后台）都在；权限表按**本地工具名**归类（ADR 0003）。
- 密钥：env 名 + vault；无 OAuth。
- 注意：内核里已有的 `blocks.go` / `SummaryBlock` 是**压缩摘要块**，不是本设计的消息内容块；命名实现时勿混用。

## 关键取舍（含已批三板）

### A. 消息：内容块 + 原样回放

一条助手/用户消息 = 有序内容块：文本、图片、（本地）工具调用/结果、推理、服务端工具调用/结果、引用……需要原厂回放的块可挂厂商原始片段。

换厂商：适配器只序列化自己认识的块；不认识 → 降成文字摘要块写入投影，**不删** transcript。

**为什么**：不改块模型，Claude thinking 签名和 Responses reasoning / server_tool 永远接不上。

### B. 适配器按协议，不按「又一个 base_url」

| 适配器 | 覆盖 |
|---|---|
| Chat Completions（现有加强） | DeepSeek 及一切兼容端 |
| OpenAI Responses | GPT 原生；部分 Grok 接时再核实 |
| Anthropic Messages | Claude |

`providers.json` 增加 `protocol`（或由已知厂商推断）。Bot 绑定仍是 `provider`+`model` 一对。预留的 `ModelSelector` 仍可 Fixed；自动选模是另一候选。

### C. 服务端工具：能力表；与本地 search **按绑定互斥**（已批）

- 内核能力名稳定：`web_search` / `web_fetch` / `code_exec`（后者若厂商有再映射）；适配器译成各厂 hosted 名。
- **每个绑定对模型只开一条搜索路**：
  - 能力表标明该模型支持 hosted `web_search` → 开厂商 hosted；**本绑定不再**把本地 `web_search` 塞进工具列表。
  - 不支持 → 与今天相同：本地 `web_search` / `web_fetch`（SearchBackend、link 协议继续演进，不废）。
- Hosted 结果在**模型流**里回来 → 落成内容块（步骤 + 引用），**不进** C1 Jobs（不是本地 `Tool.Run`）。
- 本地工具路径仍走 C1。
- 计费：hosted 按次先记账到 trace；UI 后补。

与候选「搜索后端走 link、不用模型内置 search」的张力：本地路径保留给无 hosted 的绑定；有 hosted 的绑定用厂商，避免双重搜索。

### D. 凭证

1. API key（现有）优先贯穿批 1–3。  
2. OAuth 批 4：令牌进现有 vault（ADR 0005/0013），自动刷新；路径 OpenRouter → ChatGPT；Grok 核实复用哪条协议。  
3. **Claude 订阅第三方 OAuth 默认不做**（已批）；文案若提及须写清封号风险，且不提供产品入口。

### E. 与 ADR 的硬约束

| ADR | 约束 |
|---|---|
| 0003 | 若将来把「等价本地工具名」暴露给权限表，必须入表；纯 hosted 在模型侧完成的不进本地工具名表，但 UI/计费要可观测 |
| 0005 / 0013 | OAuth 令牌进现有 vault；不借机新开安全体系 |
| 0015 | 换模重算 T/H；工具定义体积计入 |
| 0017 / C1 | hosted 长调用 ≠ 本地慢工具；掐话续跑仍对「等模型」生效 |
| 0019 | 不暴露机器 ask/off；权限总开关语义本设计不动 |
| 0020 | DeepSeek 规则继续按模 overlay |
| 0021 | 可用工具列表随绑定/能力变化才进环境流 |

## 对照回顾

| 条目 | 撑得住？ | 冲突 / 校正 | 可复用 |
|---|---|---|---|
| 搜索后端走 link / SearchBackend | 本地路径保留 | 已裁定：hosted 绑定时不双开本地 `web_search` | `web.json`、DeepSeek Search API、`<untrusted_event>` |
| 默认绑定 1M 窗口 / ModelSpec.Context | 要 | 无；能力表字段挂在 ModelSpec 旁路，勿挤爆 Context 语义 | `ModelSpec` |
| 限流换模 | 后接 | 无；本设计只留 `ModelSelector` 缝 | `select.go` |
| 前后台思考强度 | 批 1 块模型后才能做 | 现 DeepSeek thinking 默认关 | Thinking 字段 / overlay |
| 子代理可选模型 | ADR 有、代码薄 | **本设计不顺手做** | agenttype |
| MCP schema 降级 | 弱相关 | 不把 MCP 改成 hosted | MCP 路径 |
| C1 / 环境流 | 要 | hosted 不进 Jobs（已写明）；工具列表变化进 env | tooljob、env stream |
| 权限表 / 设置浮层 | 正交 | 纯 hosted 不入本地工具名；若 UI 展示「搜索」勿暗示可按工具名确认 | ADR 0003、permissions |
| 认证名单 / 主动性评测 | 要 | 新协议绑定进名单前仍跑 proactivity；假模型先绿 | `evals/`、`fake/scripted` |
| 看图 vision | 要 | Completions 已有 content 数组图片；块模型需兼容现有「最新一批、不进 transcript 字节」 | vision 字段 |
| 全局稳定性 7×24 | 候选 P3 | 本设计不展开；适配器超时/重试沿用现 Completions 口径并在 Responses/Anthropic 对齐 | RequestTimeout / StreamIdleTimeout |

## 实现批次

| 批 | 内容 | 可观测通过（最低条） |
|---|---|---|
| **1** | 内容块 transcript + Completions 适配器读写块（对外行为可先仍像今天）；旧串内容迁移/投影 | 旧对话可读；新对话块可回放；Completions 绑定六条旅程/既有 smoke 绿；DeepSeek thinking/vision 回归绿 |
| **2** | Responses 适配器 + 模型能力表 + hosted `web_search` 互斥规则 + UI 最小步骤/引用 | GPT 绑定工具列表无本地 `web_search`；真或录制流可见引用块；Jobs 无 hosted 项；换回 DeepSeek 绑定后本地 search 恢复 |
| **3** | Anthropic Messages + 其服务端工具（若该模型提供） | Claude 绑定可跑通一轮工具；不认识块降级文字；thinking 签名可回放（若启用） |
| **4** | OAuth：OpenRouter → ChatGPT；Grok 核实协议 | vault 中有可刷新令牌；OAuth 账号下六条冒烟绿；**无** Claude 订阅 OAuth 入口 |

批次依赖：2 依赖 1 的块模型；3 可与 2 部分重叠但勿同时大改同一投影核心（并行时用独立 worktree，冲突面错开）；4 不阻塞 2/3 的 API key 路径。

每批合入后：更新 atlas `providers.md`（及涉及的 ecosystem/ui）；有架构取舍再补 ADR；guard 冒烟加上该批「怎样算对」里能自动化的项；余量写回候选。

## 还没做的（相对本设计终态）

- 内容块存储与投影、三协议适配器、能力表配置面。
- Hosted 步骤/引用的正式 UI（批 2 最小可用即可，精致排版可另候选）。
- Hosted 计费 UI；先有 trace 字段即可。
- OAuth 产品流与账号切换 UX。
- 自动选模 / 限流换模 / 子代理自选模型 / 前后台思考强度产品化。
- Claude 订阅 OAuth（明确不做，除非另开候选并写风险）。
- 把「搜索」做成权限表上的新本地工具名（不需要：hosted 不进表）。

## 非目标（v1）

- 不重写内核、不换 thin harness。
- 不做「自动选模路由」。
- 不把 MCP 改成 hosted。
- 不暴露机器 ask/off；不改 ADR 0019 默认关确认的产品默认值。
- 不借机新开安全体系或第二套密钥存储。
- 不废 SearchBackend / link 上的本地搜索演进。
- 不在本设计里承诺 7×24 / 多九可用性工程（另见候选「全局稳定性」）。

## 编码前核对清单

- [ ] 实现前再扫一遍 atlas `providers.md`、`ecosystem.md` 与 ADR 0003/0015/0017/0020/0021 是否有新段落冲突。
- [ ] 批 1 的迁移必须可逆读旧数据；禁止「只支持新块、旧 Bot 打不开」。
- [ ] 命名避开与 `SummaryBlock` 混淆（建议内容块用 ContentPart / ContentBlock 等，文档与代码一致）。
- [ ] 并行开发时：本特性与纯 UI（如设置浮层）可 worktree 并行；勿与另一条同时大改 `internal/provider` 投影核心。
