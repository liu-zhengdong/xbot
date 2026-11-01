# Agent 通过 CLI 操作 XBot：MCP 接入与 Link 退役路线

档位：**L**（跨 CLI、内核、上下文、运行时与连接器）。状态：**普通资料首刀已合入本地 main、端到端通过；后续批次未实施**。2026-11-01 政东确认方向、要求直接放进 dev，并授权派子 Agent 按路线实施；不另建 Obsidian 设计副本。

## 已确定的方向

- Agent 操作 XBot 自身，通过 Shell 执行 `xbot` 命令：配置 MCP、修改自己的 profile，随后扩展到技能、例程等平台能力。
- MCP 的管理、发现、查看说明和调用都由 `xbot mcp …` 提供；模型按需读帮助和 schema，不常驻注入整套 MCP 或平台管理工具。
- **退役 Link 作为统一能力接入抽象**，包括模型侧 `links` 管理入口；不是只把 `links` 工具改名。
- **保留 Event → Inbox → 唤醒这条线**：外部消息、定时器、Webhook、文件监听、MCP 通知和任务完成仍沿已有路径投递。
- 本文保留整体迁移路线，已实施范围仅批 0/1 普通资料首刀；不改旧 Accepted ADR 正文或 guard 旅程。后续继续先打通最窄端到端，再按能力切换并删除旧入口。

## 问题与边界

已有代理门解决了主线工具 schema 全量注入，但平台操作仍与模型工具协议绑定，原生能力、连接器、MCP 都要经过 Link/工具装配。CLI 已有 MCP 配置、profile、技能、例程等部分入口，尚不是完整且一致的运行时操作接口。

目标是让人、平台 Agent、外置编码 Agent 和脚本使用同一套可发现、可组合的命令。网页继续使用现有 API，与 CLI 复用同一份业务操作；服务端不通过启动 CLI 子进程来处理网页请求。

本路线先覆盖**平台管理与外部能力调用**。Shell、文件、看图、对用户说话、插话、子代理调度等与当轮模型交互紧密相关的基础工具，逐项核对后确定保留集合，不预设“最终只能有一个 Shell 工具”。厂商 hosted search/image 等供应商协议能力也不因本次迁移改写。

## 调用方怎么用（拟议契约）

以下示例是目标用法，**不代表现有二进制已支持**；尽量扩展现有 `profile`、`mcp`、`skill`、`routine` 命令，不再造一套平行命名。

```bash
# 帮助无需服务在线；逐级发现，不一次倾倒所有命令细节
xbot --help
xbot profile --help
xbot mcp --help

# 当前 Agent 自己；显式名字仍可给人和外置脚本使用
xbot profile show --self --json
xbot profile set --self --description '负责 XBot 开发与维护' --json

# 默认管理本实例全局 MCP；Bot 局部覆盖必须显式指定
xbot mcp add docs --url https://example.com/mcp --json
xbot mcp list --json
xbot mcp tools docs --json
xbot mcp describe docs search_docs --json
xbot mcp call docs search_docs --args '{"query":"配置模型","limit":5}' --json

# 复杂参数可从文件或 stdin 读取，不要求展平成大量 CLI flags
xbot mcp call docs search_docs --args-file request.json --json
xbot mcp call docs search_docs --args-file - --json < request.json

# 后续复用已有命令族；参数形态在对应实现批补齐
xbot skill list
xbot routine --help
```

初始上下文只需要一条稳定的 CLI 使用指引，例如“通过 `xbot` 管理平台和调用 MCP；先查看对应命令的 `--help`”。当前实例和身份由运行环境提供，不把全部子命令、工具 schema 或 MCP 清单抄进固定前缀。连接变化若需公告，复用现有环境发布缝，只给必要短提示。

### CLI 的共同约定

- 帮助成功退出 0；业务失败非 0；stdout 为结果，stderr 为诊断。`--json` 输出可解析的结构化结果，不混入启动横幅或进度文字。
- 参数 JSON 与输出 JSON 分开：`--args` / `--args-file` 是 MCP 输入，`--json` 是 CLI 输出格式。
- 回执区分“配置已保存”“已取得工具表”“调用已完成”；失败注明服务、阶段和脱敏原因，不能用空列表冒充发现成功。
- 管理回执说明生效范围和时间。profile 普通资料保存后当前 UI 刷新；模型绑定、人设等沿现有生效边界，不承诺修改已经发出的模型请求。
- 密钥只引用现有保险库名称；缺凭据时明确返回所需名称和用户设置入口。已有设置输入与探测流程保留，不让 Agent 把真值写进参数、输出或上下文。
- 沿用现有大小限制与 blob/文件出口。单次节点调用不超过 1 MiB；大文本给有界预览和可读位置，图片等内容转成当前调用方可访问的文件引用，不在终端倾倒 base64。

## 两种结构与选择

| 方案 | 主要访问路径 | 状态归谁持有 | 取舍 |
| --- | --- | --- | --- |
| A：独立命令执行 | CLI → 读写配置 / 新建 MCP 客户端 → 返回并退出 | 各 CLI 进程各持一份运行状态 | 适合安装前配置和只读文件操作；反复初始化 MCP，浏览器/登录会话难延续，更新活实例还得另接通知 |
| B：CLI 访问当前运行时（推荐） | CLI → 当前 XBot 的操作入口 → 已有业务逻辑 / MCP 客户端 | 原运行实例持有状态和连接 | 能复用会话并即时生效；必须解决实例定位、独立 chat 的入口和自操作的锁 |

以 **B** 为主路径，借用 A 的离线帮助与已有安装配置能力。运行操作不可达就明确失败，不自动另起 kernel，不偷偷回落到直接改文件。确需离线配置的已有命令，在帮助中明确离线语义，并复用相同存储校验。

```text
Agent → Shell → xbot CLI ─┐
                         ├→ 当前运行时的业务操作 → Profile / 技能 / 例程
网页 → 已有 API ──────────┘                    └→ MCP 会话 → MCP Server

外部消息 / MCP 通知 / 定时器 / 任务完成 → Event → Inbox → 原有 Agent 循环
```

操作入口按现有领域收口（profile、MCP、技能等），优先复用已有 API；缺能力再补相应端点。不先建设“万能 action 总线”、新插件协议或新任务系统。结构骨架是 `CLI → 领域操作`、`HTTP handler → 同一领域操作`；MCP 的边界收纳会话、工具发现、调用及关闭，内核不再依赖它提供 `[]tools.Tool`。

### 必须先解决的运行时边界

1. **实例与调用方**：Shell 获得当前二进制位置、数据根、运行端点和 Bot 身份；本机主线、scoped Shell、子代理及外置编码进程共用语义。优先使用当前实例二进制，不靠系统 PATH 恰好指向某个旧版本；CLI 校验目标实例与数据根一致。环境中的身份是定位信息，不冒充主人认证，也不作为权限隔离承诺。
2. **独立 chat**：现有 `xbot chat` 自己开 kernel，没有默认 HTTP 服务。第一批就确定并验证它如何提供同一操作入口，或复用明确指定的运行实例；不能以“必须改用 serve”隐式砍掉现有入口。只需要最小本机操作入口，不启动第二个 Agent 循环。IPC 传输在第一批定形，避免同时维护多套协议。
3. **自操作不重入对话锁**：profile/MCP 管理直接执行领域操作，不经 `message` / `send` 排队让自己再处理，也不等待当前轮结束后才返回。`message` 仍是主人消息，`send` 仍是不可信外部 Event。同 Bot 的读写按已有资源锁协调，持锁不跨 MCP 网络等待。
4. **MCP 配置与会话分开**：全局安装仍覆盖所有 Bot，现有 Bot 配置同名覆盖规则保持；运行会话先保留现有按 Bot 隔离的行为，不因全局配置共享就共享浏览器状态。客户端从原内核挂载点迁出并交给运行时持有，不长期保留两套 MCP 连接所有者。
5. **慢调用与取消**：CLI 调用共用现有 Shell/Jobs 的短等、输出和完成 Event。先采用“CLI 等待，外层 Shell 按原规则后台化”；不再让 MCP 内层另建一张 Job 表。显式取消沿调用链传到 MCP 客户端；不可取消/取消后副作用未知时如实回报，不自动重试写调用。
6. **改名与热更新**：profile 更新复用既有更新、Jobs.Move 和运行态协调逻辑，不能在 CLI 中重新实现搬目录。改名后的 `--self` 与后续调用仍指向同一个 Bot；普通资料更新不能重建内核并中断任务。
7. **UI 与记录**：当前头像等刷新依赖特定工具结果；改走 Shell 后，要改用业务状态更新驱动现有 UI 刷新。调用和结果仍进原 transcript/轨迹，迟到结果遵守原 turn/reply generation；不解析 Shell 输出关键词来假造旧工具事件，不另存一套 CLI 对话账本。

## 实施路径

这些批次是一条路线的依赖顺序。**2026-11-01 主人认可路线并授权派子 Agent 实施，以本表和下方验证路径作为实施基线。** 先推进批 0/1；实现与独立验证可并行，共享运行态按批串行。每批先补可运行的“怎样算对”，打通真入口后再扩大；后续发现需要改变旅程或可观测标准时再交主人决定。

| 批 | 交付与依赖 | 怎样算对（草案） | 当批退役什么 |
| --- | --- | --- | --- |
| 0 · 定操作契约 | 命令帮助、参数/结果约定、实例定位与身份传递；确定 chat/serve/桌面的运行入口和锁顺序 | 两个隔离数据根同时运行时，调用明确落在发起方实例；帮助离线可读；错误实例/离线操作明确失败 | 不删入口；列出平台工具、MCP 挂载、入站生产者和 Link 依赖清单 |
| 1 · 自改 profile 最窄闭环 | 扩展现有 profile CLI，补 `--self`，接当前运行时与 UI 更新；依赖 0 | 真 Agent 轮次中 Shell 调 CLI，资料落盘，侧栏/顶栏在流尚未结束时刷新，命令不等自己的处理锁；serve 和独立 chat 均通 | 删除已被 CLI 完整覆盖的 profile 模型工具、说明与旧专用刷新判断；头像/改名未覆盖前保留对应旧能力 |
| 2 · MCP 纵向打通 | 全局/局部配置、list/tools/describe/call、现有 stdio 与 HTTP、运行状态、配置变更生效；依赖 1 的运行通路 | 同一会话连续两次调用保留状态；主线和子代理都能发现/调用；失败非 0 且脱敏；通知仍进原 Inbox；慢调用/插话/取消不重跑 | 撤掉 MCP 到模型工具注册表的适配、`links load` 的 MCP 分支及相关 schema 降级；原 MCP 传输实现优先复用 |
| 3 · 平台管理逐族切换 | 技能、例程、Bot/群管理与通信、配置、连接器出站等；有现成 CLI 的先补齐，有运行态依赖的后迁 | 主 Agent、相关子代理和外置编码 Agent 都能走同一命令；技能的 Bot 启停、例程触发、Bot 往来等既有结果不退化 | 每族 CLI 达到等价能力后删除该族旧模型工具；更新跟版规则、手册、环境索引、评测脚本与夹具 |
| 4 · 出入站解耦 | MCP、Telegram/飞书、定时器、Webhook、文件监听等分别按自身生命周期运行；Event 类型/投递从 Link 包中独立出来 | 原来源、身份、信任包裹、路由、去重、唤醒与取消保留；入站启动不再要求实现 `Tools()` | 删除 Link/Emitter/Messenger 接口、Organs/grant 装配及 `links` 工具/命令；仍有职责的目录和安装信息换为相应领域配置 |
| 5 · 收口工具与文档 | 盘点剩余基础模型工具；依赖全部迁移完成 | 模型请求里没有 MCP 和已迁移平台管理 schema/索引；相关旅程及脚本可用；旧记录能读，新记录只走新入口 | 若已无冷工具消费者，删除 `tools` 代理门；否则只保留明确仍需的基础交互消费者。删除空注册组、旧提示、失效断言及无人使用的兼容路径 |

第一刀建议：**普通 profile 简介/时区的 CLI 自操作 + 流中 UI 刷新**。它同时验证身份、实例、锁和回执，范围小于一上来搬完 MCP。随后补齐头像、改名等 profile 行为，再退役相应旧工具。

批 2 和批 4 共享 MCP 生命周期，批 3 和批 4 共享连接器出站与安装配置，不能在同一份装配代码上各自迁一半。独立文档/验证可以并行，核心运行态按批串行落地。开发期只保留切换所需的短暂双入口，不增加长期兼容开关。

## 批 0/1 实施约束（2026-11-01，独立复核通过）

**第一刀**：普通 `description/timezone`，不迁移头像、改名、role/persona，不删除 `bot_update_profile`（它还承担头像/改名）。现有 Event/Inbox、权限默认放行、脱敏及 Jobs 不改。

**调用用法**：Agent 的每种本机 Shell 获得 `XBOT_BIN`（当前可执行文件绝对路径）、`XBOT_ROOT`（规范绝对数据根）、`XBOT_ENDPOINT`（当前活 Kernel 的本机操作地址）、`XBOT_BOT`（用于诊断的启动名字）。模型先读 `"$XBOT_BIN" profile --help`，再执行 `"$XBOT_BIN" profile show --self --json` 或 `"$XBOT_BIN" profile set --self --description '维护 XBot' --timezone Asia/Shanghai --json`。CLI 根缺省采用 `XBOT_ROOT`；显式 `--root` 与 endpoint 的规范根不一致必须失败。`--self` 的权威身份是 endpoint 所绑定的活 Kernel，不能以环境里的名字加载另一个 Bot；因此旧工具改名后 endpoint 仍指同一个 Kernel，后续新 Shell 刷新诊断名。帮助离线退出 0；运行操作不可达非 0，不落回文件写入。已有显式名字的离线 CLI 保留并在帮助中写明；带 endpoint 的普通 profile 操作走活入口，未覆盖 flags 明确拒绝组合。

**两种结构不同的方案**：A：复用 serve 网页 API + chat 新建完整 web.Server。好处是重用登录，代价是 chat 被迫装配用户/网页/对话调度，且容易重入 messages 的处理锁。B（选用）：每个已装配 Kernel 在原进程内开一个仅回环随机端口的窄 HTTP handler，仅有 `GET/PATCH /v1/profile`，无通用 action dispatcher、无新 Agent 循环。绑定当前 Kernel，避免实例注册表/UUID/别名数据库；serve、桌面和 chat 全部经既有 openKernel 装配，Kernel.Close 按下述任务寿命请求退役入口，宿主退出与显式淘汰强制关闭。接受每活 Bot 一个小 listener，换来无路由登记表和跨 chat/serve 同协议；后续批次若出现真正的实例级共享操作再提升所有权，当前不建平台总线。

**最小 IPC**：HTTP JSON 请求/响应上限各 64 KiB（低于 1 MiB）；请求带 `X-XBot-Root`，服务端核对规范根；只监听 127.0.0.1，拒绝浏览器 Origin，不在 URL/输出放密钥。endpoint 是同用户本机定位信息，不作为主人认证或隔离承诺。结果固定为 `{name, description, timezone, scope:"runtime", applied:true}`（show 无 applied 或为 false）；业务失败非 2xx，CLI stdout 仅一条可解析 JSON，stderr 是诊断。关闭或旧 endpoint 必须失败。CLI 保留显式离线入口，不声称它是热更新。

**身份与锁顺序**：handler 直接调用当前 Kernel 的窄 profile 操作，不取 bot 处理文件锁、web gate 或 turnMu，也不投 message/send。普通资料的唯一业务写口为 `bot.ApplyProfile`：校验后在现有 config.json.lock 内读取最新配置、合并显式字段并原子保存，失败不发布新内存快照；活 Bot 以指针式普通 profile 快照（内含 mutex）协调，避免现有 `*b=*fresh` 复制 mutex；只发布受锁字段，不整结构写入 b.cfg。SaveConfig 从稳定快照取值并保留磁盘上较新的普通资料。锁顺序为普通资料快照锁 → config 文件锁，不跨模型/工具/网络等待。`--self show` 现读原配置中的普通字段并刷新快照，因此网页 PATCH 的独立 Bot 对象更新后 CLI 也能读到新值，不建对象登记表。旧 profile 工具、网页 PATCH 和 CLI 共用此写口；不重建 Kernel、Jobs 或任务。改名仍走原工具/既有路径，本刀不扩大改名事务。

**领域共用缝与 UI**：Kernel 暴露读取/更新普通 profile 的领域方法，handler 只做传输边界；网页和旧工具继续调用 bot 的同一 ApplyProfile。保存成功后，经现有活轮次 Sink 增补 `profile` SSE 提示（只带 Bot 名），UI 立即读原列表/详情，并只在当前选择仍相同、不在往来视图时更新现有组件；不经 Inbox、不解析 Shell stdout、不改视觉布局。原 `bot_update_profile` 即时刷新分支保留，直到未来通用状态通知完全覆盖头像/改名；不能用 3 秒轮询替代旧即时行为。短轮也必须在发送 done 前发 profile 提示，长轮在模型仍阻塞时发出；资料面只更新已保存详情，不覆盖用户正编辑的表单草稿。已有列表缓存签名继续包含头像。

**身份传播**：主线、dev/general scoped Shell 共用同一个绑定函数，在最终 commandEnv 加上述环境，覆盖父进程残留值；外部编码命令用其既有白名单环境装配加同一份 caller 环境，内置编码 Shell 同样接入。research 只读包装沿原分类器，首批不开放 profile CLI（不得把任意 xbot 当 readonly），留待按业务动作分类；runner 在其他进程/机器，当前 endpoint 不能直接透传为可达能力。本刀明确保留 research 与远端 runner 缺口，不假报全部执行路径端到端支持，不把回环地址发给远端冒充可用。

**生命周期与发布锁**：入口在 openKernel 全部可能失败的装配完成后最后创建；创建失败关闭该 Kernel 已有资源。提供窄 `CloseOperations`，由 chat 返回、web forget/binding rebuild、service ctx 取消强制关闭。短命 serve.Pass 的 Kernel.Close 仅在已有 exec.Local/code.Link 活任务清空后退役入口：复用现有 live 集合与真正结束后的 OnIdle 回调，不造任务表、不轮询、不常驻空闲 Kernel；服务 ctx 的可撤销回调在入口关闭后移除。不为释放 endpoint 重做 Jobs 生命周期。profile 领域写锁/配置文件锁均在通知前释放，SSE 写出经原 replyStream 的锁；不拿资源锁等网络。活轮次 Sink 在插话时随原路移交，通知归当前持有者，不广播、不追加另一条账本。

**预计变更**：`cmd/xbot/{main,cmd_bot,cmd_serve,cmd_chat}.go`（命令/装配/关闭）；新增窄包 `internal/cliops`（定位契约、handler/client）；`internal/bot/{bot,profile}.go` 及必要配置访问点（共享配置写入与快照）；`internal/kernel/{kernel,shelljob,agents}.go`（caller 绑定与生命周期）；`internal/tools/shell.go`、`internal/code/{link,run}.go`（共用环境）；`internal/kernel/interject.go` 与 `internal/web/server.go`（原 Sink 的窄 profile 通知）；`internal/web/static/app.js`（收到业务变更后立即刷新）；相关定向单测、atlas、多 Bot/UI/生态节点、MILESTONES。决定见 [ADR 0043](../adr/0043-cli-platform-runtime-entry.md)；旧 Accepted 正文不动，仅说明实际切换普通 profile/UI 刷新范围，尚无工具族整体退役。

**独立冒烟可观测接口**：真二进制 serve/chat + scripted 模型发出 Shell 命令，stdout 可直接解析上述 JSON；GET 列表/详情核对磁盘与当前资料；用下一次模型请求的环境时间/指纹核对活 Kernel 已感知；保持 SSE 尚未结束，收到 profile 提示后打开现有编辑资料表单观察新简介，API 核对时区；自然短轮同样立即刷新资料，不仅依赖收尾；双 root 并跑确认另一实例没改变；显式 root mismatch、无 endpoint、非法 timezone 都返回非 0 且不改资料；旧工具仍可换头像/改名。实现方不改独立 smoke 期望。最小验证先 CLIops、bot、CLI、Kernel 的相关测试，随后独立真入口及一个代表浏览器场景；不全仓、不三遍、不真模型。

**复核约束**：后续扩展沿 [ADR 0043](../adr/0043-cli-platform-runtime-entry.md) 的身份、锁和生命周期边界，先复核具体设计，再扩大产品范围；已完成的验证记录见 [MILESTONES](../MILESTONES.md)。独立冒烟为 `e2e/smoke/cli_profile_test.go`、`e2e/smoke/cli_profile_background_test.go` 和 `e2e/smoke/testdata/cli-profile.cjs`，实现方不改期望。

## 对照回顾

回顾范围：候选索引全文、164 份候选 metadata/title 逐条、直接相关 44 篇全文与其余 120 篇主题筛查已由独立研究方核对，未新增阻止批 0/1 的前置；不是宣称全部 164 篇正文逐字读过。人用终端 PATH 的 P3 不等于本批 XBOT_BIN 已完成它；环境实体暂不开、多 Provider/子代理人工排期不阻塞首刀。旧搜索 Link/禁 hosted、expand/load/schema 旧描述和默认写跑先问方向与当前 atlas/ADR 不一致，本批不执行、不改候选状态。头像即时刷新、改名 Jobs 连续性保持；research/runner 缺口明示。下表核对 atlas、ADR、guard 与正在做的设计，不把无关 UI、供应商、评测、设备扩张条目纳入实现。

| 现有要求 / 来源 | 关系与处理 |
| --- | --- |
| [生态](../atlas/ecosystem.md)、[原生与 Links](native-vs-links.md) | 本路线会替换“凡能力均 Link/organ 挂载”的形状；保留连接应用设置面、目录、安装/卸载和已有能力，不让内部抽象退役变成产品能力消失 |
| [内核与工具代理门](../atlas/kernel.md)、[ADR 0035](../adr/0035-unified-tool-registry.md) | 现已按需披露，不能把它描述成主线全量注入。MCP/平台工具移出注册表与现决定冲突；实现前新建替代 ADR，写清被取代的条款；本轮不改 Accepted 正文 |
| [ADR 0021](../adr/0021-env-stream.md)、[环境提示框架](env-prompt-frame.md) | 延续尾部公告、不改固定前缀；身份/能力变化复用现有环境注入缝，不借本次迁移决定环境账本争议或加新通知机制 |
| [多 Bot](../atlas/multibot.md)、候选“Bot 自改头像 / 改名后面当面刷新 / 改名打挂工具槽” | 保留资料即时刷新、改名后的任务和路径连续性；CLI 缺 avatar_seed 等能力，不能只因已有 profile set 就删模型工具 |
| [子代理](../atlas/subagents.md)、候选“子代理类型 / 工作流 / 只读工具并发执行” | CLI 身份与 endpoint 需要传到 scoped Shell、本机子代理、runner 和编码进程；research 只读语义不能因套一层 CLI 失效，不借迁移恢复前后台能力闸 |
| [ADR 0028](../adr/0028-no-foreground-background-split.md) | 能力可达性保持，改变的是入口；不规定主 Agent 必须派子代理才能操作平台 |
| [安全](../atlas/security.md)、[ADR 0019](../adr/0019-no-permission-cards-by-default.md)、[ADR 0041](../adr/0041-global-product-vault.md) | 生产继续不弹许可卡；复用认证、全局保险库、域名绑定、脱敏；保留严格测试分类，不借 CLI 迁移新加权限产品或导出密钥 |
| [ADR 0031 设置收密钥](../adr/0031-store-keys-in-settings.md)、候选“连接应用商店 / Playwright 连接器无工具” | 保留鉴权后续、安装前真探测、配置与运行状态区别；撤掉 links 输入卡入口时以已有设置输入承接；缺 key 不假装安装成功 |
| [CLI 主人消息](cli-message-bot.md)、[CLI 外部 Event](cli-send-event.md) | 保持两者的来源/信任和回执语义；管理 RPC 独立于对话投递锁；给自己投递通知需要使用既有非等待方式，不能伪装主人消息 |
| 候选“终端 PATH 上的 xbot / 轨迹可发现与回放” | Agent 能运行当前实例的二进制是批 0 必需配套；不要求用户先做系统全局安装。既有 trace/导出复用，不新造轨迹存储 |
| 候选“搜索后端走 link 协议” | 与退役 Link 的旧实现方向冲突；保留可替换后端这一需求，后续改为 CLI/领域后端表达。本文不自行取消或改写候选，不在迁移中重做搜索策略 |
| 候选“私有连接器目录 / 企业控制面 / MCP OAuth（连接应用后续）” | 保留目录与凭据后端可复用的空间，不提前实现官方 Registry、OAuth、新插件市场、多租户或企业控制面 |
| [六条基本旅程](../guard/六条基本旅程.md) | 网页/终端对话、插话、做事、记忆、主动唤醒、Bot 协作均须保持；工具入口退役时迁移有效行为断言，不改用户的旅程定义 |
| [长程运行](../guard/长程运行.md)、候选“稳定前缀 / 旧工具结果清理 / 小模型裁剪工具结果” | 帮助与 schema 按需进入工具结果，仍受现有 fit/blob/压缩约束；不重写旧 transcript，不用减少声明的 token 数冒充真实总成本收益 |
| 候选其余交互/UI、供应商、记忆与设备工作 | 无需因本路线重新排期；守住原数据/界面入口。远端执行器的定位纳入批 0/2，多设备管理 CLI 的扩张另排，不顺手合并其它功能 |

本文服从 [验证约定](../TESTING.md)：规划文档只查 diff、链接和 atlas audit，不跑产品测试。2026-11-01 路线级独立复核通过，已核对上述运行时、迁移与 Event 保留约束；不代表实施细节已定稿。L 档实现前补正式 ADR 和实施设计独立审计。

## 怎样验证这条路线（草案）

正式用例由非实现方按主人定稿标准补写，复用已有真实二进制冒烟框架；模型与 MCP 服务只用 fake/scripted。

1. **自操作**：真 serve + 脚本模型 → Shell → 当前实例 CLI → 改 profile → 流中 UI/持久化结果；另一实例的 profile 未改变。另有独立 chat 真入口用例。
2. **MCP**：真 CLI → 真运行时 → stdio/HTTP 假 MCP；查看说明并调用，第二次调用观察相同会话状态；冷启动失败/缺凭据是明确失败；工具 `isError` 为非 0；通知作为不可信 Event 到达。
3. **忙时操作**：MCP 调用变慢时用户可插话，原调用只执行一次；输出、取消和完成只走既有 Jobs/Inbox。改名时活 Job 与后续 `--self` 正确，原目录不复活。
4. **入口替换**：主线与各执行路径按 CLI 使用能力；旧平台工具、索引和 schema 从请求消失，未迁移基础能力仍可用。跨平台命令参数/文件/错误行为以当前实际支持的平台检查。
5. **回归与收益**：按批选择受影响旅程一次。上下文入口切换时检查前缀稳定和长程约束；测调用额外 IPC p95、首次/复用会话耗时及代表任务总 token，沿用 IPC < 5ms 等现有预算，不预先承诺更快/更省。

**冒烟用例**：独立验证方的 `e2e/smoke/cli_profile_test.go`、`e2e/smoke/cli_profile_background_test.go` 与 `e2e/smoke/testdata/cli-profile.cjs`，实现方不改期望；未移入 guard。

**级别**：普通 description/timezone 首刀已合入本地 main，所测真入口端到端通过（独立 serve 双 root、chat、长/短轮浏览器及 serve.Pass 本机后台）；整条路线仍在实施，主人日用尚未验收。

## 接受的代价与待定细节

- 接受每次命令的进程启动与 IPC 开销，换来统一入口与脚本组合能力；帮助发现也会占 token，收益以代表任务测量。
- 接受运行操作依赖当前实例可达，换来会话连续性；已有离线帮助/配置用途明确保留，不静默切实例。
- 接受模型工具层对 MCP 内部细动作的可见性下降，轨迹首先保留真实 Shell 命令与结果；需要更细展示时复用原 trace，而不是假装旧工具仍被调用。
- 后续具体设计先定：`--self` 跨完整改名的稳定定位、人用终端/外部脚本的显式实例地址；runner 不可直达原实例时如何沿既有链路回接；严格只读子代理如何按业务动作分类 CLI。chat/serve 的最小本机 HTTP 与当前调用方环境沿 [ADR 0043](../adr/0043-cli-platform-runtime-entry.md)，不再重新选择传输。
- 本文不预先锁死包名、socket 路径、所有 JSON 字段与完整基础工具白名单；在第一条真实调用路径上定形，不为将来猜测造框架。

## 还没做的

- 主人日用验收尚未开始。普通资料首刀的独立审计、真入口验证与合入已完成；后续不重复该刀设计审批，按新切片复核实际增量。
- 批 0 的后续实际操作及入站依赖清单、research 按业务只读分类、远端 runner 回连；本批实例/二进制/root/本机当前 Bot 身份和离线帮助通路已建立，不能等同人用 PATH 安装。
- profile 的头像/改名完整运行时 CLI 与组合更新；在真正覆盖前保留旧工具和原头像即时刷新，未整族移除。
- MCP 管理、发现、调用、通知、生命周期与慢调用的 CLI 通路。
- 技能/例程/Bot/群/连接器等平台能力逐族迁移。
- 事件结构与投递摆脱 Link 包依赖，删除 Link 装配与旧入口。
- 基础模型工具集合和代理门的最终去留；同步规则、手册、评测、atlas。
- 各批独立冒烟、相关旅程回归、性能与上下文收益测量、主人日用验收。

落地一批后只把已实现的现状提炼进 atlas；收尾按项目约定处理本 dev 和 guard，本轮不提前把目标写成现状。
