# Agent 通过 CLI 操作 XBot：MCP 接入与 Link 退役路线

档位：**L**（跨 CLI、内核、上下文、运行时与连接器）。状态：**profile完整族、MCP、skills与routine已合本地main；Bot四操作五项所测独立端到端分阶段通过并已合main ac519c8d；pin/primary四项所测独立端到端分阶段通过并已合main6291fae1；persona self四项所测独立端到端按精确phase通过并已合mainbddaf2e4；chat-group成员群五项所测独立端到端按精确phase通过并已合main8caacdca；peer-message六项所测独立端到端按精确phase通过并已合main98fba5e8；bot-fork七项所测独立端到端按精确phase通过并已合main06daa397；experience record/search三项所测独立端到端按精确phase通过并已合maind632e7a3；memory-recall三项所测独立端到端按精确phase通过并已合main1053d0a9；下一仅transcript三读Proposed具体合同，其余平台批次继续推进**。2026-11-01 政东确认方向、要求直接放进 dev，并授权派子 Agent 按路线实施；不另建 Obsidian 设计副本。

## 已确定的方向

- Agent 操作 XBot 自身，通过 Shell 执行 `xbot` 命令：配置 MCP、修改自己的 profile，随后扩展到技能、例程等平台能力。
- MCP 的管理、发现、查看说明和调用都由 `xbot mcp …` 提供；模型按需读帮助和 schema，不常驻注入整套 MCP 或平台管理工具。
- **退役 Link 作为统一能力接入抽象**，包括模型侧 `links` 管理入口；不是只把 `links` 工具改名。
- **保留 Event → Inbox → 唤醒这条线**：外部消息、定时器、Webhook、文件监听、MCP 通知和任务完成仍沿已有路径投递。
- 本文保留整体迁移路线；profile 完整族、MCP CLI、skills、routine、Bot四操作及pin/primary已按切片提炼，其余平台族继续推进。不改旧 Accepted ADR 正文或 guard 旅程；后续继续先打通最窄端到端，再按能力切换并删除旧入口。

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

## 批 1 avatar_seed 实施约束（2026-11-01，独立结构审计通过）

**范围与用法**：在既有入口上支持 `"$XBOT_BIN" profile set --self --avatar-seed '周报#3' --json`，可与 `--description/--timezone` 同次提交；`show/set --json` 加 `avatar`、`avatar_seed` 元数据，不返回图片。seed 仍沿 `ValidAvatarSeed`（trim 后 1–80 字符、无标记/引号/控制符）校验，非法组合整次不写。当前 endpoint/root/self、Sink 通知、后台入口寿命与生产放行语义不变。上传/clear 暂无 runtime 协议，旧 `profile avatar <name> --file/--clear` 在存在 endpoint 时明确拒绝，并写明只有无 endpoint 的人用离线入口；不能借旧命令绕过热操作。

**两种结构**：A（选用）把头像元数据纳入已有 profileState，四个真实写口共用私有、有类型的 profile 保存函数；配置/上传文件使用原 config 文件锁协调，调用链仍是 CLI/网页/旧工具 → Bot 领域操作 → 原资源文件。B：另建 avatar service/mutex/缓存，保存后再同步 profile。B 多一份锁与头像状态，跨两 loaded handles 仍需文件锁，还会让 Agent 抄到两套写法，因此淘汰；不加通用 mutator/action 框架。

**拟议形状**：`ProfileUpdate` 增 `AvatarSeed *string`，`cliops.Update` 同名 JSON 字段，`Profile` 增 avatar/seed；profileState 的构造、copyTo、set、RefreshProfile 覆盖这两个字段。`ApplyProfile(u)`、`SetAvatarSeed(seed)`、`SetAvatar(data,mime)`、`ClearAvatar()` 共用私有 `applyProfile(u ProfileUpdate, avatar avatarEdit)`，avatarEdit 是 keep/upload/clear/pinLegacySeed 的小枚举及上传 bytes（MIME 沿既有 SniffImage 判定）/seed；pinLegacySeed 仅供既有离线 Rename 将空 seed 钉为原名字，保持 fresh avatar 模式（legacy custom 仍是 custom），不作为新 runtime 参数。只表达现有真实写意图，没有任意回调、dirty flags 或通用字段 map。SetAvatarSeed 委托 ApplyProfile；网页 PATCH 和旧模型工具把 seed/简介/时区合成一次 ApplyProfile，name/persona 仍沿旧语义，不冒称与它们整次原子。现有离线 `Rename` 在原 primary 锁内、移动目录前，以 loaded 原 Bot 调 `applyProfile({}, avatarEdit{kind:pinLegacySeed, seed:from})`；helper 在 config 文件锁内仅当 fresh seed 为空时钉原名字，保留 fresh avatar（包括 legacy custom）。随后原目录/Name/Jobs 改名路径照旧，删掉原直接 b.cfg.AvatarSeed 赋值；即使后续改名失败，这一步也只将原本按旧名字显示的空 seed 规范为同一旧名字，不改变显示模式。不新增保存头、dirty flags 或异步 rename 协议。

**锁、发布与文件顺序**：所有字段和上传字节先完整校验，再拿 profileState.mu → config.json.lock，锁内读 fresh config、合并显式字段。seed 写 generated+新 seed；clear 写 generated 并保留 filelock 内 fresh seed（不能从过时 handle 的 AvatarState 复制 seed）；upload 写 custom 并保留 fresh seed。普通 SaveConfig 从磁盘保留最新 avatar/seed，不能让旧对象 SetPinned 等设置回滚头像。只有配置元数据保存成功才发布快照；seed/clear 在保存后、仍持上述原资源锁时 best-effort 清除旧 upload/MIME，避免删掉并发新上传；清理失败不伪报未保存。上传文件与 MIME 在同一资源锁中原子各自写入，元数据成功后才发布 custom；接受三文件不是崩溃事务，不加日志。失败不发布新快照。

**读取与 UI**：Config/AvatarState/AvatarBytes/ExportTemplate 不再读取可异步改写的 b.cfg 头像字段，而从同一受锁快照读取；AvatarBytes/ExportTemplate 共用私有 `readAvatar`：拿 profile 快照锁 → config 文件锁后现读 fresh 磁盘元数据，用它判定 custom/seed，再在同锁中读取 custom bytes/MIME，返回同一份元数据与内容；不能以旧 loaded handle 的 custom 快照作判定。于是旧 A=custom、B 已 seed→generated 即使遗留 upload/MIME，A 也不服务旧图或误报 missing file。generated 时不读遗留文件，释放锁后才调用可选 generator。Config/AvatarState 从受锁头像快照读取，ExportTemplate 的头像种子/模式与 bytes 来自同一 readAvatar 结果，避免两次独立读取拼出混合头像。已有 publishProfile 对当前 live.k 的 RefreshProfile 将同步头像快照；原 profile SSE 令侧栏和顶栏刷新，不加组件/事件/轮询，不修改视觉。短 guide 优先 CLI 普通字段和 seed，完整旧 profile 工具及旧即时刷新分支暂留（改名/组合仍未等价）。

**怎样算对与独立观察**：两个 loaded handles：A 换 seed，旧 B 的 SetPinned 不回滚；web/旧工具 seed+非法 timezone 组合均不改头像/简介；同对象上传/clear/seed 与 Config/AvatarState/AvatarBytes/ExportTemplate 的定向 race 无竞争；失败保存不发布、不删除原 upload；generated 有遗留文件也不对外服务。真 CLI 在长流未 done 时替换已上传头像，stdout avatar/seed、列表和编辑资料一致，侧栏与顶栏都已换；当前 root/Bot 外的对象不变。独立验证方拥有新的 `cli_profile_avatar_*` 文件，我不修改期望。

**变更与检查**：预期只改 `cmd/xbot/{main,cmd_bot}.go`、`internal/cliops/profile.go`、`internal/kernel/{operations,profile}.go`、`internal/bot/{bot,profile}.go`、`internal/web/profile_api.go` 与自有针对性单测；沿既有前端代码通知，不重写 UI。先跑自有旧句柄回滚/组合非法/并发读写红回归，审计 PASS 后实现；通过后仅跑受影响 profile/头像/模板/CLI/web 组合检查与必要 race，独立真入口由验证方跑，不全仓/三遍/真实模型。ADR 0043 的入口/生命周期决定继续有效，本刀不改变它或提前移出工具族；改名、上传 runtime 协议、research/runner、MCP 均不纳入。

## 批 2a：MCP core 与进程资源 owner（2026-11-01，已合入本地 main，所测端到端/增量静态通过）

**切片与对照**：2a 只做 core/真实会话 owner、通知 reconcile、原 Jobs 共享和源调用责任；短暂保留只借用的旧模型 adapter 供观察。2b 真 CLI 通过后同刀退 MCP adapter/装配/schema，不提前写 CLI 大接口或文件产物协议。沿已完成 164 metadata/44 相关全文与其余主题筛查，只查新冲突：ADR 0021 消息/环境、0035 剩余工具门、0043 source endpoint、0044 sender 继续有效；新所有权决定见 ADR 0046。不重新做全候选审查，不改旧 Accepted 正文。

**独立机械子步（已单独放行）**：先搬现 client/stdio/http/diagnostics/config 与有效协议测试到 internal/mcp，link 仅留薄模型/事件调用适配；保持现凭据/provider注入、连接寿命、HTTP取消和15s契约，不接进程Manager/共享Jobs/rename-delete。每提交可编译，只跑搬迁相关一次，不合 main、不宣称2a完成；后续HTTP写缝与runtime接线已获整体具体结构PASS，继续按下述形状实施。若无法纯机械拆开，不硬造兼容框架。

**调用方与两种结构**：runServe/cmdChat 各创建一个 `mcp.Manager`，同 Bot/server 唯一真实 Session；web 缓存 K、每次 Pass fresh K 与旧 adapter 都借同一 Bot 资源。A 缓存整颗长期 K 会改变 fresh Provider/role/budget/recovery 与 web gate/chat 装配，淘汰。B（选用）复用原 web kernels/gate 映射提升 `entry{Resources, Kernel, Gate}`，只稳定 Jobs/MCP/通知落点；chat 直接持一份 Resources，不起完整 web.Server。撤回母稿 drain/JobOwners 及上一版 Bot 全局 endpoint：无旧 K/JobID 路由表、持久 UUID/alias、第二任务账本；Pass 仍 fresh 构造。

### 已定：core 与会话 owner

`internal/mcp` 接收现 stdio、client/http/diagnostics 与 config，不依赖 link/bot/kernel/web/exec/tools。产品窄接口如下；配置/通知短 IO 由装配注入，不在 core 引 Event。

```go
type Options struct {
    ResolveSecrets func(context.Context, []string) (map[string]string, error)
    IsProviderKey func(string) bool
    Redact func(string) string
}
func NewSession(Spec, Options) *Session
func (*Session) Connect(context.Context) error
func (*Session) LoadTools(context.Context) ([]ToolSpec, error)
func (*Session) CallTool(context.Context, string, any) (json.RawMessage, error)
func (*Session) Notifications() <-chan string
func (*Session) Status() Status
func (*Session) Close() error
type Binding struct { Key string; ReadConfig func() ([]Spec, error); Notify func(server, text string) error }
func NewManager(context.Context, Options) *Manager
func (*Manager) Bind(Binding) (*BotRef, error)
func (*Manager) Reconcile(context.Context) error
func (*BotRef) Specs() []Spec
func (*BotRef) LoadTools(context.Context, string) ([]ToolSpec, error)
func (*BotRef) CallTool(context.Context, string, string, any) (json.RawMessage, error)
func (*BotRef) CallToolForSpec(context.Context, Spec, string, any) (json.RawMessage, error)
func (*BotRef) Status(string) Status
func (*BotRef) Pause() error
func (*BotRef) Resume() error
func (*Manager) Rekey(*BotRef, string) error
func (*Manager) Unbind(*BotRef) error
func (*Manager) Close() error
```

Spec 持久字段只有传输配置，现 Dial/Secret 非序列化 probe/fixture 缝暂留；ToolSpec/ToolAnnotations 保 Raw/schema/完整 annotations，CallTool 返回原 json.RawMessage，保 content 未知块、structuredContent、isError。2a 仅旧 adapter 投影 text/isError 兼容，core 不丢内容；2b 再做图片/大结果产物。现 MCPServer/config 可短暂 alias/薄包装编译，但协议/client/config 只留 core 一份。

Manager slot 持 immutable effective spec、实际 Session、短连接完成 gate。global+Bot 同名整条 override，只比较 command/args/url/secret_envs；来源变化或被 local 遮住的 global 改动不重连。ReadConfig 在资源 route 锁中读 global+当前 local 文件，不能捕获旧路径后在改名外读成“配置已删除”。Manager map/slot 锁不包 ReadConfig/Notify/Open/Close/网络；状态 GET 与 adapter 快照不探测。

热变更沿现 Close 旧再连新：短锁阻旧准入，锁外取消/Close 旧 Session+通知泵并等 transport 真退出，再 Open 新 slot。gate 只串关闭/连接，不等在途动作自然 drain；不加 active/desired/draining 队列。已发 tools/call 因替换失败回 configuration_changed/outcome_unknown，永不 retry。失败 spec 阶段明确，不能每 Pass 无限重启同一错误进程。reconcile 同时创建/替换/移除通知泵；Notifications 单消费者、原 32 有界丢弃，不阻塞 RPC。装配将 text 转原 MCP 不可信 Event→Inbox→锁外 waker，不另建完成事件。

initialize/list 保 15s（整次发现含分页）；HTTP WriteContext 去掉所有 POST 统一 15s cap，tool call 随实际 ctx。HTTP 读 max+1 显式拒绝超 1MiB；显式取消对原 request ID 发一次 best-effort cancelled，stdio/HTTP 均不承诺撤销远端副作用、不重发写。Resolver 区分 missing 与 vaultd 无明文能力，不能因 bool lookup 失败降级本地/暗用 env；env 来源仅在装配明确决定。诊断叠启动秘密 snapshot+当前 redactor，先脱敏再 8KiB 截断。商店 probe 保独立有界临时 Session、defer Close 与原鉴权/Playwright 真探测。

**实际凭据写口（补审）**：web/secrets.go 的 globalSecrets POST（Set/SetDomains）、DELETE（Remove）现只写 backend；HTTP 在 openStreamable 时固定 secretHeaders，stdio 也仅 spawn 时读 env。进程唯一会话不能只比 SecretEnvs 名称。沿现写口保存成功后触发同一 reconcile，原 Pass reconcile 同时覆盖外部保存；无新 watcher/secret DB/epoch 服务。每次只解析各有效 slot 引用的凭据输入，用确定顺序的 name/presence/实际值在内存中比较（或私有摘要），值变化/删除仅替换引用 slot，同值、无关 secret、仅 domains 改动不全局重连；值与摘要不进 stdout/JSON/日志。

Manager 比较的输入与实际启动必须同一 snapshot：Resolver 按现 backend/明确 env 策略取得一次输入，Session 的 header/env 与启动脱敏均用这份 frozen snapshot，不为比较、stderr、spawn 三次重读把轮换夹在中间；snapshot 归本 Session、至少保到旧进程真退出，不另存盘或第二份缓存。原启动秘密仍遮旧进程迟到 stderr，再叠当前 redactor。vaultd 无明文不回退本地/env；凭据消失或不可导出明确阶段，不能静默继续旧 header。凭据输入变化也按原 slot Close旧→Open新，受影响在途调用 unknown、无 retry；读到可用输入后沿同 reconcile 恢复，不新增代理密钥产品。机械子步保持原行为，本项只属新 owner 接线。最小 fixture：现 globalSecrets 真 POST/DELETE 与 stdio/HTTP 同 session 计数，引用值变/删换会话，同值/无关名/domains不换，旧 stderr 轮换秘密仍遮。

**实际写缝/取消（补审草图）**：现 client.writeContext 的 wmu 包住 HTTP 完整 POST+readBody，call 又在 write 返回后才 select ctx；共享后不能只删15s cap。HTTP 改成独立 `WriteRequest(ctx, frame)` 传输入口，整个 POST 响应不占 stdio 帧 gate，同 session 多个 POST 各随自身 ctx；原 pushMu 只串入读循环的完整帧。stdio 保完整帧串行，但 mutex 改 ctx 可取消 gate，B 等写权时可立即因自身 ctx 退出，不等 A。stdio 实际阻塞写也须经 ctx-aware WriteContext：原 stdin fd 用写 deadline/取消解除；等 gate 取消或确定零字节的可恢复 deadline 取消不破会话；EPIPE/已关闭等硬 transport 错误即使 n=0 也沿原失败路径关闭，不能仅按零字节复用坏流。实际 Write 返回 n>0 且 err/短写即已损坏完整帧边界，必须标当前 transport 损坏并 Close，不能清 deadline 后在半段 JSON 后续写 RPC/cancel；受影响 call 如实 unknown、不 retry。无可用 deadline 的真实传输才关闭解阻；不宣称这种损坏仅取消 B 而其它流仍完整。

```go
func (c *client) writeContext(ctx context.Context, frame []byte) (mayHaveSent bool, err error) {
    if requestWriter, ok := c.conn.(interface{ WriteRequest(context.Context, []byte) (int,error) }); ok {
        // HTTP: 请求按ctx独立，绝不拿stdio帧gate跨POST/响应。
        return writeHTTPRequest(ctx, requestWriter, frame)
    }
    // stdio: select frameGate / ctx.Done / session.Done，再ctx-aware写完整帧。
    // ctx在等gate时结束则mayHaveSent=false；实际开始写后仅能报可能已发。
}
func (c *client) call(ctx context.Context, method string, args any) (...) {
    // 沿现原ID登记pending、write、等原reply，所有return路径删除pending。
    // tools/call可能已发且ctx取消：一次cancelHint(originalID)，不新分ID、不retry。
}
func (c *client) cancelHint(id json.RawMessage) {
    // 独立短ctx（例250ms，上界同时受session寿命约束），不是已cancel的call ctx。
    // 异步best effort一次notify；caller清pending/返回不等它成功。
    // 不用无界Background；普通控制notify/reply也走短ctx，避免堵读循环。
}
```
任何 manager/route/operations 锁都不包写 gate/POST/提示。最小红必须为同一 HTTP session：A POST gate，B 已到服务端后单独 cancel，A 保持 gate 而 B 有界返回；核对B原requestID的cancel提示至多一次、A没有被B误取消；stdio 等帧gate取消另做必要定向检查，不用CLI外层timeout伪装写缝可取消。此处仍无第二MCP任务队列。

### 已定：装配、原 Jobs 与 per-call 责任

- 原 web entry 同时用于 Pass 和 web；Resources 仅持共享 `*tools.Jobs`、`*mcp.BotRef`、当前 Bot/Inbox 短 route 与关闭状态，不保存第二份 MCP 状态。runServe 同一 entry accessor 构造资源，chat 在入口直接构造；Jobs 只 Load 一次，默认 Redact/Forms/OnDone/Now 绑定一次。openKernel 在 RecoverTasks 前注入，K 借表不 Shutdown；宿主最终关一次，自有独立 K 的私有表仍自关。
- mount/newServeKernel 不再 New 拥有 client 的 MCP；syncMCPLocked 仅同步 borrowed adapter，adapter.Close 不关 Session。attachShellJobs/attachShell 扫 *Shell 时不得经 MCP.Tools 隐式网络；真正 LoadTools 在 host/links/operations 锁外。删除 startEmitters 中 MCP 长期 K，保其它原 Emitter。
- `CallRequest` 增真实 `Redact func(string)string` 与 `Forms func()[]string`；runToolCall 给 source k.redactor+staged secrets、k.shellForms 动态闭包，live/writer 捕获它们，不让每 K 改共享默认。默认只绑定进程 Vault/held/current 已知秘密；Output 仍动态遮新学秘密。实际出口逐处接：RunCall writer/初始 Command；completeCall 的 result.Text **及 Err**、输出、Finalize 后 Result/错误和 writeMeta 失败诊断；runProcess 重设 Command/Description、启动/Wait/读输出错误与格式化结果。writeMeta 本身完全不脱敏，落盘前必须源 call 已遮；保原分块 Forms、输出限额和新学秘密读回。
- Prepare/Finalize/原 turn/replyGen/sender/trust/cwd 闭包继续属 source call，不换成当前 owner K。默认 Resources.DeliverToolDone 复用 Jobs.Deliver→原稳定 shelljob_ID Event→当前 Inbox，所有锁外 wake。request.OnDone 覆盖默认的现有单选保持；exec fallback/forwardExec 用同一 Deliver，不发第二完成 Event。Deliver 实际持 metaMu，故短路由顺序为 Jobs.metaMu→route→Inbox；MCP note 为 route→Inbox。
- 实际运行写口 web/mcp、apps/custom、apps/{id} DELETE、globalSecrets 与 kernel links 成功后立即 reconcile；旧离线 cmd 配置及外部文件由现 Pass 读取，2a 阶段将 runtime MCP CLI 留至2b，现已按下一节完成并合入。snapshotLinks 只读 cached spec；Pass/API、Handle 和显式 load 在资源锁外 reconcile。runtime 摘要/search/load/adapter 统一沿 Manager 保留的有效 spec，不绕过它读坏配置；仅 saved/configured 不冒称 ready。现 Bot MCP API 是 GET/POST/DELETE，同 spec POST 409，无 PUT/update；DEL+POST 仅称移除/新增。真正外部原子替换 mcp.json 沿现 serve pass 频率 reconcile，无新 watcher；独立 fixture 可沿现 --interval 100ms。GET apps/runtime 只看当前 Manager 状态，不拿另一 spec 旧错误作新配置结果。

**实际装配签名（已整合并通过相关组合检查）**：`Kernel.Resources *BotResources`；`web.Server.NewResources func(*bot.Bot)(*kernel.BotResources,error)`，`NewKernel func(*bot.Bot,*kernel.BotResources)(*kernel.Kernel,error)`；原缓存只一张 entry map，`ResourcesFor(b)` 复用同 entry，Pass 的 factory 先取它再 `newServeKernel(root,b,resources,restart)`。chat 直接构造同类型，不建 map/web。Resources 构造完成 Jobs.Load/一次默认投递，再注入 openKernel，后者才 RecoverTasks；binding cached/临时构造同传此指针。默认 Jobs 不捕获第一 K 的 Redact/Forms/OnDone；源 call 仍自己的闭包。MCP ReadConfig 在装配过滤原 retired catalog 项，core 不 import link 猜目录规则。Runtime rename/delete 通过 source K 的窄 host callbacks 进入同 Server entry 接缝；chat 仅当前资源闭包，未持资源的其它 Bot 无需造 K/client/table。

### 已定：source listener lease

沿 runToolCall 的 CallRequest.Prepare（Run goroutine 前）识别真实解释器，取得当前 source server 的 calls lease；清理最后 defer release，在原 Status/storeScope/dropScope 的 Jobs/cwd 锁全部释放后执行。记录/准入失败不提前取 lease。完整脚本两次 CLI 中间 gate 仍持着，requested cancel 未真返回不释放；不存 JobID、不按共享表总活数保住空 K。

operationState 只增 calls；closeIdle 快照同 server/calls=0 后解锁查 Local.Tasks/code live，真正摘 server 前在短 operations 锁重查同 server/calls/retiring，HTTP Close 锁外。不得拿 operations 锁调 Tasks/snapshotLinks/网络；release 不等 Jobs.done（cleanup 在 done 前）。原 Local/code OnIdle 保未来 Shell 与队列交接寿命，宿主 ctx/chat 仍强关。profile callback/当前 BIN/root/endpoint/Sink 不提升为进程 profile owner。

### 已审接缝：新增资源触及的 rename/delete/binding

rename 两真实写口必须同一 host 接缝，不能 web 裸 RenameBot→forget 而旧 tool 另走 Move。实际安全锁序：source/current operations → host s.mu/name-index → 整个 code.Move(path/record) → 共享 Jobs.Move(meta/dir) → route → physical RenameBot；更新当前资源 Bot/Inbox、ref key/local 配置并移同 entry/Gate。NewKernel 当前在 s.mu 内 Recover code/Jobs，故不能在 Move 内反取 s.mu。route 内不调用 Jobs/host/operations/网络，所有锁外 wake；物理移动成功后不能因 reload 错误让 Jobs 仍指旧目录。调用/当前 K 的 Bot **与 Inbox** 都需更新，不据此宣称旧 exec/code/liveTurns/hold/所有 K 都已跟名。必要时 retained source profileOperation/CLIEnvironment 在 operations→route 下只解析当前名字/短 profile IO，使用局部 fresh handle而非异步改旧 k.Bot，仍不是新 endpoint owner。profileOperation 保存/读值后必须先放 route 与 source operations，再沿现 publishProfile(b)：它会拿另一 live K.operations，不能在 route 内调它或 Sink，否则与另一 K 的 operations→rename route 成反序。

binding 只清 entry.Kernel，Resources/Gate 保留；cached 与临时 checker 都先借同资源（PrepareBinding 无条件 CheckBindingIdle→shellJobs），防冷路径第二表。CloseOperations 移 s.mu 外，避免 operations→host 的反序；wake 用当前 route，在短 route 锁内 fresh Load 当前路径配置，不让旧 captured b 或 r.current 的长期完整配置 UseBot 回去；绑定 B 后 Provider B 必须配 fresh B 的 prompt/vision/image 能力。TryCurrentHandle 的 Load 与建 handle 在同短 route 内，避免并发改名/删除复活旧目录。

删除按已审三段产品实施：①提取现 bot.CheckDelete 初始存在/primary/last 检查，短 primary 锁即放，拒绝时不动资源；②快照/重验同 entry，临时暂停共享 Jobs 新准入（非 terminal closed）并标资源 deleting，所有 primary/map/route 锁外 cancel+join 真 owned calls（primary→Jobs 会与现 rename 的 Jobs→primary 成环，禁止）；③按现 Delete 约束重验名字/对象/primary/last 后物理删，成功才锁外 Unbind/Close MCP 与移 entry。长等待不承诺原子回滚；对象改名/保护条件改变/删除失败要如实报告，仍存在对象恢复同 entry/Jobs 准入，不 Load 第二表，不假装旧任务未取消。join 阶段保同 client，只停新工具准入，原 OnDone/MCP note 继续投既有仍存在的目录；最后短 route 写 fence 才阻止删后 Post，失败自然保同 ref，不加暂停通知队列。可恢复暂停与宿主永久 Close 分开，恢复不重开已宿主关闭表；最终物理删除在 route→primary 下重核/执行，不得 primary→route，join 则无任何全局/资源锁。具体产品增量交24静态审。无删除账本/marker/UUID，不把 busy 拒绝偷换现 API。

删除三段的实际调用形状：serve 复用现 web.Server entry callback 注入 delete_bot；chat 直接用当前 Resources 闭包，其未持资源的其它 Bot 仍走原删除，不另建 HostResources 映射。
```go
func (s *Server) deleteRuntimeBot(ctx context.Context, name string) error {
    // bot.CheckDelete: 复用现 primary.lock 内存在/last/primary 判断，立即释放。
    // host短锁取得并重验同entry/current name，BeginDelete临时暂停准入。
    // Jobs.PauseAdmission仅置临时paused，不是宿主永久closed。
    // RunCall保持metaMu→j.mu，在任何Mkdir/meta/输出建档前检查paused/closed，
    // 检查与live注册同一准入临界段，暂停后拒绝路径也不能重建目录。
    // Pause调用不在route内；不能持j.metaMu/host/primary做join。
    // 释放全部全局/route锁，Jobs.Shutdown取消并等真实owned calls。
    // 此间rename hook发现closing即拒绝，不在join里等primary/host反锁；
    // 已先完成rename则在BeginDelete前报target_changed，不误删新名。
    // 真call已join，复用原Jobs.Move(sameDir)的元数据锁→route写fence→DeleteWithResult(primary)；
    // 禁止反过来primary→route。join时原OnDone/note照常Post，
    // 仅最后fence阻止删后Post；失败保原ref/原投递，不加暂停note队列。
    // removed成功才终态Unbind MCP/关闭资源/移entry，均不持owner锁等网络。
    // 失败且对象仍存在：EndDelete恢复同Jobs准入/route，不Load新表；
    // 同MCP ref/client在join期间保留，只停新MCP工具准入、通知继续原Post。
    // 已取消工作不承诺回滚；恢复只保证后续可用。
    // 物理目录已删、仅后续vault key清理失败时不能恢复不存在对象；
    // 返回真实partial结果并终态收资源，现Delete的removeVaultKey在RemoveAll后。
}
```
最后的 metadata fence 来自实际写缝：原 Jobs.Deliver 在 callback 放 route 后仍写 notified meta，单 route fence 会与晚到 flush 交错；复用既有 Jobs.Move(sameDir)，不新增锁表/状态机，也不在它里面 join。

Jobs 准入至少区分 open/临时 paused/宿主永久 closed，仅表达资源寿命，不是 active/drain 任务状态；Resume 仅在 shutdown实际完成、host仍活且原对象仍存在时执行。service ctx结束是不可恢复上界，不因失败删除把宿主已关资源重新打开。全段无 primary/map/route→join，既有 rename 的 Jobs→primary 锁序不被反转。

**最小红与验证**：先自有 source call lease fixture，K1 两次 profile 协议调用中间 gate、同表空 K2 退休、requested cancel 未真返回、完整 cleanup 后端口关；该 unit 不冒充真实 CLI。core 搬并复用 stdio/HTTP/diagnostics 断言；补同 Bot 两 K/Pass 单 initialize、另 Bot 独立、热新增 note、override/GET 不重连、换配置 call 计数1、HTTP call ctx/原 ID cancel。共享 Jobs 两源动态 secrets/Forms、不串回调、跨 K 原 ID cancel、单 OnDone、old replyGen/附件边界；保既有 avatar/name/代理门有效断言。独立 ownership/通知/慢 call/跨 K cancel 新文件归验证方，实现方不改。按影响最小 unit/race+入口一次，不全仓/三遍/真模型；仅已单独放行的纯机械 core 子步可先提交；整体结构已经PASS，现正式继续HTTP取消修正与runtime接线；产品完成仍需实际绿证据。

## 批 2b：实例 MCP CLI 与旧模型入口退役

2026-11-01，基线 main `16101e7e`；2a 已合入，本节仅补父2241草图与24预核2749的实际新增接缝。独立最窄真入口已在此基线红：`"$XBOT_BIN" mcp list --self --json` 返回旧 `mcp list <bot>` usage（validation b4f266c6，独立测试9f035622）；不重复2a慢调用/Owner。新决定见 [ADR 0047](../adr/0047-mcp-cli-domain-entry.md)。选定共享领域方法+两个薄HTTP+纯argv parser，不选 web spawn CLI 或每命令自建 K/Session；不重开候选/路线。

### 调用方、argv 与目标

```sh
"$XBOT_BIN" mcp add docs --url http://127.0.0.1:9001/mcp --self --json
"$XBOT_BIN" mcp list --self --json
"$XBOT_BIN" mcp add docs --scope bot --command /absolute/program --arg literal --self --json
"$XBOT_BIN" mcp tools docs --self --json
"$XBOT_BIN" mcp describe docs search_docs --self --json
"$XBOT_BIN" mcp call docs search_docs --args '{"q":"x"}' --self --json
xbot mcp call docs search_docs --http 127.0.0.1:8788 --bot ada --args-file request.json --json
xbot mcp offline list ada
```

`list/add/remove` scope 默认 global、显式 bot 为整条 override；tools/describe/call 总用当前 Bot 的 effective spec和远端原名（可含斜杠），不用旧 sanitize/prefix。无 self 或显式 http+bot 报错，不猜primary/cwd、不静默offline；显式http优先并不得混self，env endpoint只供self。旧裸 `<bot>` 配置语法给迁移提示，文件用途仅 `offline list/add/remove <bot>…`，不接online flags。管理写只保存配置，不接受密钥值。

`cliops.ParseMCP(argv []string)(MCPCommand,error)` 无环境/文件/网络，字段为 Action/Server/Tool/Scope、Self/HTTP/Bot、JSON/ReadOnly、Command/CommandArgs/URL/SecretEnvs、Args/ArgsFile、Timeout/Help。flag arity明确，支持位置参数前后flag；args与args-file互斥且不可重复，cmd再读至1MiB+1（`-`为stdin），以RawMessage保单JSON object且EOF，拒null/数组/多对象；不转any丢大整数。未给args等于 `{}`。timeout默认0只随信号ctx，显式值须正；发现仍core15s。

main.run保splitGlobal的rawRoot是否显式，在resolveRoot/EnsureDefault/BindLegacy/HoldKeys之前分流MCP help/online；help只stdout不需实例/目录，不安装凭据。offline才沿原本地root/config流程。self取XBOT_ENDPOINT+XBOT_ROOT，显式rawRoot核验沿当前profile边界；human只在显式rawRoot时canonicalize并发X-XBot-Root，不发送默认Root/XBOT_ROOT、不臆造session.Root。human复用cmdMessage的loopback校验、同cookiejar GET /api/session authed与后续请求；不复制其2m/default transport。MCP独立client使用caller ctx、Proxy:nil/noRedirect，不使用profile10s。HTTP请求上限1MiB，回包按下述最终JSON上限64KiB。

### 领域方法与两个薄HTTP入口

新增 `kernel/resources_mcp.go`，不暴露通用route/action bus。低层 `cliops` 定义MCP DTO/具名handlers，不import kernel；Kernel已import cliops，禁止反向循环。

```go
type MCPCall struct { Server, Tool string; Args json.RawMessage; ReadOnly bool }
type MCPArtifact struct { Type, MIMEType, File, Access string } // JSON: type,mime_type,file,access
type MCPContent struct { Type, Text, Preview, MIMEType, URI, File, Access, ProjectionError string }
type MCPReply struct {
    Data json.RawMessage `json:"data,omitempty"` // metadata only; too large goes to Artifact
    Content []MCPContent `json:"content,omitempty"`
    Raw *MCPArtifact `json:"raw,omitempty"`
    Structured *MCPArtifact `json:"structured_content,omitempty"`
    Artifact *MCPArtifact `json:"artifact,omitempty"` // complete large metadata/manifest
    IsError bool `json:"is_error"`
    Saved bool `json:"saved,omitempty"`
    RuntimeError string `json:"runtime_error,omitempty"`
    ProjectionErrors []string `json:"projection_errors,omitempty"`
}
func (*BotResources) MCPList(ctx context.Context, scope string) (cliops.MCPReply,error)
func (*BotResources) MCPAdd(ctx context.Context, scope string, spec mcp.Spec) (cliops.MCPReply,error)
func (*BotResources) MCPRemove(ctx context.Context, scope,name string) (cliops.MCPReply,error)
func (*BotResources) MCPTools(ctx context.Context, server string) (cliops.MCPReply,error)
func (*BotResources) MCPDescribe(ctx context.Context, server,remote string) (cliops.MCPReply,error)
func (*BotResources) MCPCall(ctx context.Context, call cliops.MCPCall) (cliops.MCPReply,error)
```

MCPHandlers逐个持同签名函数；cliops.Start用具名Handlers包含Profile/MCP，在原source监听器挂 `/v1/mcp/servers` GET/POST/DELETE、`/v1/mcp/tools?server=`、`/v1/mcp/describe?server=&tool=` GET、`/v1/mcp/call` POST `{server,tool,args,read_only}`。server/tool用query/body，不能path拆远端原名。所有source路由复用现root/browser/Host guard，不裸挂新路径；MCP调用不持operations锁，handler捕获本source Resources/ref而非XBOT_BOT。source listener的原解释器lease覆盖整个CLI脚本。

human同方法映射 `/api/bots/{bot}/mcp/servers|tools|describe|call`，先require/owned，再在显式header非空时对s.Root核验，最后ResourcesFor；scope global也不跳owner校验。旧web MCP配置GET/POST/DELETE、apps custom/remove与store配置写口保业务，但委托同领域保存/reconcile，不能又旁路创建客户端。

list Data为 `{scope,servers:[{...spec,source:"global"|"bot",shadowed, runtime:{state,tool_count,error?}}]}`；默认读global并在同短route读local名称判遮盖；只有行spec匹配Ref.Specs当前effective才用其Status，否则configured。工具Data为 `{server,tools:[完整ToolSpec...]}`，describe Data为完整原ToolSpec；若最终JSON超限，Data不截schema，改完整json Artifact。add/remove回Data `{scope,name,state:"configured"}` 与Saved:true，reconcile失败保真实保存回执+RuntimeError，不伪ready/回滚。

配置路径的取值和mcp.Load/Add/Remove实际IO在同短route内，Options显式config.ProviderKey；释放route后global→manager.Reconcile、bot→ref.Reconcile，避免ReadConfig再进route死锁。bad config的runtime tools/describe/call仍用Manager保留态；显式配置list如实报告读错。所有网络与JSON/base64 decode均不持route/ops/host。

### 同session readonly 与临时probe

```go
type CallOptions struct { ReadOnly bool }
func (*BotRef) CallTool(context.Context, string, string, any, CallOptions) (json.RawMessage,error)
func Open(context.Context, Spec, Options) (*Session,error)
```

CallTool只选一次slot→wait→若ReadOnly，在同slot.session.LoadTools精确匹配远端名且hint=true→原短锁复核closed/paused/仍当前slot→调用该session。不能组合公开ref.LoadTools和ref.CallTool二次路由；换代失败不打新session，已发unknown不retry。退adapter时删除CallToolForSpec。RawMessage直接传arguments，保数字/未知结构。

Open复用Options.resolveSecrets的同一实现，冻结一次snapshot、lifetime绑probe ctx，Connect失败Close；Manager仍原异步slot启动。现NewSession不消费public ResolveSecrets，不能只传resolver假装完成注入。web.probeApp仍45s envelope，但用此次待保存values的复制map、显式predicate/redactor打开临时core Session，defer Close；不去Manager旧vault。Playwright用原名browser_navigate/browser_close，CallTool后decode isError并检查（transport nil不等于成功），close best effort，保原真探测。

### 有界投影与实际落盘fence

MCPReply最终Marshal（含转义/metadata/tools聚合）<=64KiB；metadata过大完整写json Artifact，不能直接Encode20页工具表。call始终保存脱敏完整Raw json，small text直接Content.text，大text给preview（最多2KiB）+完整txt；known image在锁外真实base64解码、用WriteGenerated的sniff，返回mime/file/access:local；resource_link只保URI不fetch。embedded text/blob为txt/bin，unknown块/structuredContent保完整json；未知type不猜可执行语义。已知图片解码/类型失败仍保原块json并给ProjectionError，不能静默丢。任何projection/落盘失败如实输出，不重放远端写。

新增薄 `work.WriteResult(workspace string,data []byte,ext string)(rel string,error)`：内部固定txt/json/bin suffix、随机exclusive0600、os.OpenRoot、单文件<=20MiB（tools最多20页）、短写/Close失败清残件，返回workspace相对结果路径；不接远端filename、不借Job或第二账。known image复用WriteGenerated的现20MiB/500文件约束。网络、解码、脱敏、预处理在锁外；每次真正写入必须在r.route.RLock内检查closed/ctx、取 `work.BotDir(r.current.Root,r.current.Name())` 并完成writer至Close。work在root/work/name，非bot.Dir；bot.Rename实际moveWork，CurrentBot返回后锁外写会复活旧工作区，不能这样做。

若内容/产物metadata本身使reply超64KiB，把完整投影manifest写json，再仅回小preview+Artifact/Raw引用；保已产出文件的可发现路径，不stdout raw/base64。JSON字段用snake_case，plain输出也只摘要/本机引用。isError:true仍输出Content/Raw后CLI非0；transport失败独立error，projection_errors非空也非0；Saved成功但runtime尚不可用不冒ready。相对file随工作区实际改名移动，access只声明local，不伪远端可达。

### research实际接线与同cut退旧

review新增窄DirectXBotArgs，只认完整literal词、整词单引号/无展开双引号；首词准当前canonical BIN或精确 `"$XBOT_BIN"`，其它变量/拼接/operator/重定向/substitution/wrapper拒绝。原shellWords会抹操作符和引号，不可用于授权。CLI纯parser确认self help/list/tools/describe或call+readonly；禁止http/其它Bot/offline/写动作，显式root仅接受同当前root的literal，未知形式拒绝。仅POSIX解释器，不宣称cmd/PowerShell；普通read分类保留。readOnlyShell取source caller的实际BIN/root，legacy toolsFor和continuableTools都接相同逻辑；不更改Descriptor.Tools持久策略、每Local admission或路由/overflow。

同一2b产品切换退：link.MCP/NewMCP/NewBorrowed/Owns/build/本地工具名投影；cmd.mount两条MCP分支；kernel registry pending/prefix/startGroup/Owns/describe启动；links MCP load/toolsForLoaded/sync/status/newMCP hook；Shell/StartEmitters的过渡skip；主reply/schemaKernel、dispatch.fitProvider的MCP attempt/modelKey和continuable_contract旧MCP fallback用例。保context overflow/fit/calibration、Descriptor provider路由、普通权限/hint真实消费者，历史transcript只读不藏旧执行adapter。

config aliases/callers转core并显式predicate：尤其link.connectors目录Validate/Add/Replace、web MCP/apps CRUD/status、cmd connector/offline；删除SetProviderKey全局前逐口完成，零Options不能放过provider key。Raw ToolSpec/annotations已存在，不造第二结构。混合测试只退adapter/pending/schema断言；保HTTP/session/SSE/原IDcancel、global-local override、目录/密钥/probe真失败、配置状态GET不探测及非MCP Tools原行为。Tools门仍服务memory/routine/context/peers/bots/links/secrets/shelljobs/agents/skills，不可跟MCP一起删除。

### 怎样算对与实施顺序

1. 最窄真实BIN source Shell：self add→list→tools→describe→call，原名/同session counter连续，配置回执不冒ready；此路径独立最小红已固定。
2. 独立方把既有2a central driver改真CLI，原Owner/热note、慢call/gate/计数1、跨K原Job status/output/cancel、单Event、source两CLI/空K/真结束回收断言不改。独立文件只归0d，产品方不改；只因真实受影响路径选择重验。
3. 必要边界：help无root/本地凭据副作用；human显式dev实例/错root；args-file/stdin/大整数/非法或多JSON；isError内容+非0；image/unknown/structured完整本机产物且不stdout base64；research literal/注入拒绝、精确hint与list期间换spec零RPC；主/legacy子/continuable provider请求均无MCPschema而非MCP Tools仍可用。
4. 最小相关unit/race及真实入口各一次，绿即停；不完整矩阵/全仓/真模型/权限UI，不提前完整rename/runner。先交24具体新增DTO/argv/guard/同slot/probe/写fence，PASS后分冲突面实现；最终同cut退旧，不暂留双入口进入main。

## 批 profile 完整等价：组合改名、头像与原运行态连续

2026-11-01，基线main `458dcc94`。沿父1c一页选A，本节只写实际新增接口/IO，不再开路线审。独立最窄红：test `544a808b36626869807cf8ed366027c782500bf2` / validation `ad776eb0` 与该基线树一致，真实source Shell `profile set --self --name FullProfileRenamed --json` exit1，明确仅支持description/timezone/seed，case1.58s/package3.075s；只新`cli_profile_full_runtime_test.go`。旧profile/avatar/MCP绿证据不重跑。[ADR0049](../adr/0049-complete-runtime-profile-entry.md) 待24核本节新增结构。

### 调用与预算

```sh
"$XBOT_BIN" profile set --self --name NewName --description '简介' --timezone Asia/Shanghai --avatar-seed 'NewName#2' --json
"$XBOT_BIN" profile show --self --json
"$XBOT_BIN" mcp list --self --json
"$XBOT_BIN" profile avatar --self --file ./face.png --json
"$XBOT_BIN" profile avatar --self --clear --json
```

name+description/timezone/seed保原同请求组合，avatar上传/clear仍独立，不加set头像文件组合。role/timebudget不扩；persona/group保原独立业务。self用原endpoint/Resources，不取旧XBOT_BOT定位；显式名字与绑定Bot不符/错root照拒，无endpoint保原明确离线用途。

JSON `/v1/profile` GET/PATCH与回包仍64KiB；`/v1/profile/avatar` POST原始二进制body<=1MiB，Content-Type提示后端实际sniff，DELETE clear，复用原Host/browser/root guard。CLI Open文件+有界流式传输，不无界ReadFile、base64、服务端path或临时图像服务；元数据独立profile请求，不塞复杂header。Web原multipart外壳兼容、file仍<=1MiB，薄转同typed头像domain，不把2MiB envelope例外带进source协议。metadata/file非法或超限提交前拒绝。upload现bytes→MIME→config多次原子写，后期IO不冒称全零改，回完成阶段；seed/clear metadata成功后清旧图best-effort仍按saved。

### typed输入、提交与真实回执

```go
type ProfileChange struct { Name, Description, Timezone, AvatarSeed *string }
type ProfileChangeResult struct {
    Name, PreviousName string
    AppliedFields []string
    Moved bool
}
func ValidateProfileChange(ProfileChange) (ProfileChange, error)
func CheckProfileChange(root, from string, normalized ProfileChange) error
func (*Bot) ApplyProfileChange(normalized ProfileChange) (*Bot, ProfileChangeResult, error)
```

外部入口只规范化一次，内部传复制后的typed值。Validate提取现applyProfile的name flat/ValidName/PortableName、description limitText160、timezone flat/LoadLocation、seed Trim/ValidAvatarSeed；完整验证在任何业务写前。Check只读Stat/必要ReadFile+Unmarshal查目标Bot/work/key，不用会迁移的Load，更不ApplyProfile试校验。private checkLocked在primary提交锁内重核依赖；Bot/work/key统一same-file规则，moveWork同步修大小写同文件豁免；peer成员修改前先完成全部预期校验。

无改名仍profile.mu→config.lock fresh merge→WriteAtomic→快照。改名经movers进入route→primary→profile.mu→config锁，持已打开config锁跨目录move，在新路径把name+请求fields+legacy seed pin一次fresh merge写入，保未请求skills/card/role/model。不能改b.cfg后SaveConfig（会回填磁盘profile旧值吞新字段），不在目标Load后嵌套迁移/SaveConfig抢同锁。旧Rename/RenameWithResult薄委托name-only同实现，保RenameBot/peer/primary/work/key职责，不留两套rename。kernel的peer.RenameMember改在提交返回实际moved=true之后执行，错误join入partial；这样primary锁内冲突复核之前不先写groups，不向bot包传通用before/rollback钩子。

actual WriteAtomic成功点才记applied_fields，最终目录落点决定name/moved；同名no-op moved=false，大小写不靠old消失/new出现猜。预期无效/冲突零改。后期primary/work/key/重载IO仍现best-effort回滚：若名回旧而fields已存，回old/moved=false/fields非空；目录留新就回new/moved=true，全部mover/route必须跟新，失败不能复活旧目录。无现成physicalMovedResult或rename failurehook，不冒称已有；验证用真实文件故障/实际move回调结果，不提前造通用故障框架。

cliops.Update加Name；Profile回包增previous_name、applied_fields、moved、可选脱敏error，保name/description/timezone/avatar/avatar_seed/scope/applied。guard前失败保原错误；domain已产结果则HTTP即使4xx/5xx仍带完整有界DTO+error，CLI先输出实际结果再非0，Call不能遇非200丢partial。avatar仍用现私有avatarEdit的typed upload/clear写口，独立回applied_fields中的实际完成阶段（avatar_file、avatar_mime、avatar元数据），无图像bytes入DTO；普通/头像/改名实际变化均统一profile通知，无变化不发假成功。

### 原资源、liveTurn与当前路径

profileOperation仍source operations.mu；短route普通更新，改名释放route读锁再进host/mover，不拿turnMu/web gate。r.Rename改为 `func(*Kernel, bot.ProfileChange) (bot.ProfileChangeResult,error)`；`web.RenameResources(source,change)` / `Kernel.MoveResources(change)`传同typed值到底层写缝，原RenameRuntime(next)/RenameTo(next)作name-only领域委托。host entry/Gate/MCP ref/Jobs同对象，旧holds按实际move释放。

`BotResources.WithCurrentIO(func(*bot.Bot,*inbox.Inbox) error) error`在route.RLock内检查closed并做短文件IO，不能回调exec/Jobs/host、发布或网络。当前metadata可snapshot，实际路径lookup→open/write/close必须同回调。source profile、主线Append/FindEvent/LoadState/SaveState及fork结算、forwardExec父信/完成Post、Code transcript/Post、Timer文件与notify outbox接此落点，不返回旧path锁外写。Timer构造期WithDirectoryIO包装List/Load/Update/Commit实际文件段，emit锁外。code原pathMu/Record锁保，transcript callback用当前route，不捕获k.Bot.Append。不异步给全部旧K写Bot字段，Provider/role/budget快照不重建。

workspace/allowedRoots、scope/命令Prepare+Finalize、父任务cwd从当前r读；只对旧Bot或work前缀内路径用filepath.Rel重基，外部cwd/repo保留。沿原scope/cwd锁在实际读写点重基，不新登记表；文件工具Root、本地进程Start取当前路径短段，Wait/模型/工具循环不持route。source listener lease、调用输出归属、Event ID与Jobs原表不换。

liveTurns只从old bot.Dir找原value且Resources同一才rekey至new；登记key保turn/interject状态。closeInterject用保存key+CompareAndDelete同value，不凭已改k.Bot猜key，rekey/close同interject互斥。rekey固定在整段MoveResources/code/exec/Jobs返回（route/storage/owners全部释放）后、publish前；可仍留host锁封名索引，不能在physical的route.Lock内取inter.mu，close用保存key+CompareAndDelete且不复活closed turn，chat/name-only/cold共用该末段。全部host/mover锁释放后publish，沿现表真实liveTurn刷新普通profile快照再走原Sink；不拿另一K.operations成环。`kernel.ProfileNotice{Name,PreviousName string}` 是通知值，Kernel/Sink.OnProfile改 `func(ProfileNotice)`，web.replyStream.profile接同值并JSON为name/previous_name；已变化的partial失败也发布。

### accepted Local、短存储与冷续

```go
func MoveBot(oldAgents,newAgents string, move func()(moved bool,err error))(bool,error)
// 所有Local构造期注入，空Local也使用。只在末层短IO调用。
WithCurrentIO func(func(botName,agentsDir,workspace string) error) error
```

放现code.Move callback内部，锁序source ops→host→code.pathMu/Record.mu→owners→去重实际Local.mu→job.recordMu(storage fence)→Jobs.meta/dir→route→physical。只从现owners.local oldAgents子key取真实accepted实体。当前表只收continuable；为完整覆盖其它旧K的legacy，本刀legacy Start在同owners→Local准入段登记canonical req.Dir→原Local，terminal真实结算后核原job指针/当前key/owners[key]==l精确删除，queue cancel也等实际结算完。无新表/全K登记/全局capacity；Control按Descriptor区分legacy，不误送continuable.Message。空Local不登记。

moved=true即使后期IO err也先重基Local/job/owners；外code callback沿2a after模式给其mover nil令Record一起重指，再join返回partial。不加drain/newbusy/Join。重基j.dir/request.Dir/JobDir/Bot/workspace/descriptor.Bot+CWD/resultPath与resource中dir:/repo:内部前缀；ID/turnseq/队列/原选模/预算不变。迁既有合法descriptor/meta可变path（含冷完成记录，不登记活job），旧transcript/results内容不写；恢复旧turn_end ResultPath按当前childdir+turnseq投影，不灌旧绝对路径回meta。

扩大现recordMu而不添第二job锁：路径/seq、Mkdir/legacy append、record append、begin/resume读record、seal扫描补tombstone、queued/terminal meta、descriptor、result O_EXCL→resultPath→turn_end→末status完整短段。recordLocked/sealCallsLocked/writeStatusLocked避重入，writeStatus改storage→j.mu。WithCurrentIO只在这些末层取route、按当前路径重基字段并完成open/write/close，fn不反拿exec/Jobs/ops或外部callback。

模型/Tool/ProviderFor/Wrap/Restore/CurrentCWD/ReleaseScope/emit/OnIdle全锁外。每步tools/RunTool/Wrap先storage取当前request深拷贝再锁外调，去掉newJob/legacy refreshTools捕获旧req。finish先锁外快照/CurrentCWD，再storage按当前workspace重基返回cwd并写descriptor/result/end/status，出段才dropBefore/ReleaseScope/emit/Restore；原fail闭包会拿Local，不能搬进storage。restoreNext准备next后在最终owners→Local handoff再按当前旧job重基，不让未入索引准备态越过move。

Message冷续两段：owners→Local→storage→WithCurrentIO读currentdir/状态及私有interruptLocked；放锁Restore/newJob；最终同顺序重取路径/owner/descriptor依赖，按原descriptor选模/ID重基再入场，不能用旧d.Bot/dir盖回。Stop/Control/boundDir同用闭包；不是WithCurrentIO包整个Message（会route→owners）。原允许成功不以新busy躲避。

### UI与同cut退旧

`applyProfileRename(selection,{name,previous_name})`仅原selection仍当前且previous==selection.id时就地rekey同对象，更新id/URL/xbot-selected-bot、sidebar/header、workDescriptions及Bot key的composer草稿/附件。child view加窄rename入口，route/draftKey/chosenKey用当前bot并迁child草稿key，保同root/child/generation/输入。SSE先处理previous_name再refresh，form PATCH成功走同入口，不selectBot/resetConversation，保原reader/abort/text/附件/焦点/scroll。非当前/迟到响应沿原selection/gen拒，不因碰巧同名改另一个视图。无视觉改版。

退profileTool/parseProfileArgs、registry bot_update_profile、review旧执行名、UI result-name刷新hack及手册guide。保personaTool/FlushLegacyHeld/RenameBot领域函数/group原业务；Web persona/group按原规则前置验证再独立保存，不让profile预期冲突先写persona。mixed测试只迁失效tool断言，保有效persona/字段校验/权限默认放行，独立测试只归0d。

### 怎样算对与实施顺序

1. 先独立真Shell最窄红转绿：组合name+desc+tz+seed→原endpoint第二show/MCP及下一步append/父信均新名，错root/显式别Bot拒绝。
2. 冲突/非法组合（含work/key/大小写）零改；真实保存/move后失败如实partial，不复活旧目录；同名moved=false、未请求配置保留。
3. >64KiB有效头像真实HTTP流、1MiB边界/超限/类型拒绝+clear，stdout/Event无image bytes，不冒任意IO全回滚。
4. 活Local gate跨rename原ID steer/父信/结算，新dir唯一记录；冷Message续同ID/原选模/turnseq，旧行不写。原Jobs/code/timer跨move与旧hold释放，不新busy拒成功。
5. 浏览器一项previous_name就地改名、原SSE/草稿/焦点/scroll与child输入保持，切Bot迟到隔离；保persona有效，模型请求无旧profile工具。

24只核本节新增锁/IO结构。最窄入口先通再补边界，普通profile/seed与2a移动/lease/MCP绿证据封存复用，不全仓/全矩阵/真模型/无关视觉旅程。不合main/push/日用实例；这里是合同，事实记MILESTONES，完成现状再提炼atlas。

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

**级别**：普通 description/timezone 与 avatar_seed 已合入本地 main，所测真入口端到端通过；最终兼容增量有定向红绿/race 证据，版本归属见 [MILESTONES](../MILESTONES.md)。整条路线仍在实施，主人日用尚未验收。

## 接受的代价与待定细节

- 接受每次命令的进程启动与 IPC 开销，换来统一入口与脚本组合能力；帮助发现也会占 token，收益以代表任务测量。
- 接受运行操作依赖当前实例可达，换来会话连续性；已有离线帮助/配置用途明确保留，不静默切实例。
- 接受模型工具层对 MCP 内部细动作的可见性下降，轨迹首先保留真实 Shell 命令与结果；需要更细展示时复用原 trace，而不是假装旧工具仍被调用。
- 后续具体设计先定：runner 不可直达原实例时如何沿既有链路回接；严格只读子代理如何按其它业务动作分类 CLI。当前实例 `--self` 跨完整改名的稳定 ref 与显式本机调用沿 [ADR 0049](../adr/0049-complete-runtime-profile-entry.md)，chat/serve 的最小本机 HTTP 与当前调用方环境沿 [ADR 0043](../adr/0043-cli-platform-runtime-entry.md)，不再重新选择传输。
- 本文不预先锁死包名、socket 路径、所有 JSON 字段与完整基础工具白名单；在第一条真实调用路径上定形，不为将来猜测造框架。

## 批 skills：实例 read/write 与同刀模型门退役（2026-11-01，具体合同）

### Caller 先行与结构选择

```sh
"$XBOT_BIN" skill read ship --self --json
"$XBOT_BIN" skill write ship --title 发布 --description '发布检查' --body-file ./body.md --self --json
"$XBOT_BIN" skill write ship --description '发布检查' --body-file - --scope self --self --json
"$XBOT_BIN" skill write ship --description '发布检查' --body '' --scope global --self --json
```

`--self` 只定位原 source endpoint/ref，不隐含 `scope=self`。scope 省略/空白＝本数据根所有本机 Bot；仅 `self` 是 scope 保留字，显式 `global` 仍匹配真名 global 的 Bot。read 的 id 匹配库排序中的 Slug 或 Name，不以 Title 作为 id。`list/show/import/remove/enable/disable` 保持现离线命令；show 仍原 SKILL.md，不冒充 runtime read。runtime 仅 read/write，要求 --self；help 离线成功，未提供 endpoint 不离线回落，也不在本刀增加人用 HTTP 入口。

| 形状 | 实际调用路径与代价 | 决定 |
| --- | --- | --- |
| A：CLI import 后逐 Bot enable/disable | CLI 掌握库、Bot 枚举与多次提交，正文写后失效/并发交错使默认开关与正文赢家分离，难以如实回执 | 淘汰，不用 CLI import+循环制造原子性 |
| B：source 两个具名 typed 领域操作 | CLI 只组 argv/body；cliops 两路由→捕获原 source 的 Kernel handlers→既有库锁与 Bot config 写口，隐藏默认范围提交细节 | 采用；不加 action bus、第二 owner/Job/会话表或族级框架 |

实际装配草图（未实施的函数明确为本刀新增）：

```go
// cliops: protocol DTO，不导入 Kernel。
type SkillWrite struct { Slug, Title, Description, Scope string; Body *string }
type SkillReadReply struct { Slug, Name, Title, Description string; Body *string; Preview, File, Access string; Bytes int64; Error string }
type SkillWriteReply struct { Slug string; Written bool; Scope, Target string; UpdatedBots []string; Error string }
type SkillHandlers struct {
    Read func(context.Context, string) (SkillReadReply, error)
    Write func(context.Context, SkillWrite) (SkillWriteReply, error)
}
// kernel: 两具名领域函数承接业务；resolver与routine共享匹配规则。
func (k *Kernel) SkillHandlers() *cliops.SkillHandlers
func readRuntimeSkill(dir, id string, disabled []string) (skill.Info, string, error)
func writeRuntimeSkill(root, configuredDir, currentName string, in cliops.SkillWrite) (cliops.SkillWriteReply, error)
// Kernel.StartOperations: 在实际 source 发布前捕获一次。
handlers.Skills = k.SkillHandlers() // 固定 Root、SkillsDir、动态 Redact 函数、原 Resources ref
// source HTTP 只绑定当前已有 K；不构造新 K/Provider，不调用 Tool.Run。
// 新端点 GET /v1/skill/read?id=ship 与 POST /v1/skill/write。
// cmd/research共用纯 ParseSkillRuntime；cmd先早分流，再执行 body IO/HTTP。
```

`SkillHandlers` 的 source Root/SkillsDir/Redact 不被其它新 K 覆盖；每次真正 IO 通过原 `Resources.WithCurrentIO` 取当刻当前 Bot。无 Resources 的测试/旧装配保原 K fallback。read/write 的 self 不取 XBOT_BOT；改名后 source endpoint 仍定位同 ref。原路径差异照实保留：read 在 source 配置 SkillsDir，write 虽要求 SkillsDir 配置，实际写 `skill.Dir(root)`。大正文 artifact 只在当刻当前 Bot workspace 的短 IO 内 `work.WriteResult`，不先复制旧路径再锁外写。

### 参数、预算、完整结果

- 纯 argv parser 只做语法分类：read 恰一 id；write 恰一 slug、必需 description、恰一个显式 `--body` 或 `--body-file FILE|-`，空 `--body ''` 合法；title/scope 可选。重复/未知 flag、缺 body/description、read 带写 flag、其它动作均拒绝。help 不读文件、不加载配置；research 不独立仿写 parser。
- runtime 分流在本地 Migrate/EnsureDefault/HoldKeys 前。read/write 不触碰客户机默认 root；`XBOT_ROOT` 必需且规范化，显式 --root 如有必须相同；loopback/Host/browser guard 复用现 cliops 原 source。默认随 signal context，HTTP no proxy/no redirect；不重试写请求。现 profile.callProfile 与 cmd.runMCP 各有内联 http.Client，并没有已落共享客户端；本刀只在 cliops 抽取二者所需的窄 transport 构造供 source client 复用，profile 原 10s 与 MCP/skills ctx 超时保各自语义，不造 client 框架。
- write 的完整最终序列化 JSON ≤1 MiB；`body-file`/stdin 读 max+1 后判超限，再将完整 DTO marshal 后再次按最终请求字节判预算。JSON 转义也计入，不能以原 body 大小当 wire 大小；超限非 0 且业务文件/config 零改，不能截断后写成功。服务端 MaxBytesReader 同上限、单对象/无尾随对象、拒未知字段。Body 指针区分缺失与显式空串；解码后一次规范化并校验现 ValidSlug、非空 description 和 scope，内部不再多层重复校验。
- runtime 完整 read 新增窄 bounded 打开/解析入口，复用现 frontmatter parse 与 metadata/id 规则；选择候选时也不能先用无界 `skill.List` 读整文件，再声称有上限。按库既有排序逐候选 bounded 读取，单 SKILL.md 原始字节最多现 `work.ResultMaxBytes` 20 MiB、max+1 超限明确报错。现 `skill.Read` 的 16 KiB 策略只留给 routine 引用/旧领域读取，不称 runtime 全文。
- 普通 read 返回完整解析 body 与 metadata 的 JSON；HTTP硬上限仍64KiB，实际完整marshal（含换行）须符合原Shell32KiB输出预算才内联。超预算时返回 metadata、有界 preview、实际 file/access=local/bytes，完整正文落现 results 产物。若 metadata 自身也超预算，回明确 projection error，不能把截过正文标成完整；不无界倾倒 metadata，不重复建 blob 系统。正文和 artifact 按原 source 动态 Redact；普通 body 不静默16KiB截断。CLI --json 只输出 DTO；人读输出正文或 preview+真实产物位置。读写错误都脱敏且非0；存在 Written/UpdatedBots partial 时先保 DTO 再非0。

### resolver、库锁与 partial 的真实提交边界

- 把原 openSkill 的 Slug/Name/Disabled 规则留在领域 resolver，routine 与 CLI 共用规则；runtime 以 bounded 文件打开取完整正文，routine `projectRoutineSkills` 保原引用、缺失/停用提示和16KiB读取，不扩例程正文/触发能力。历史 `redactSkillRead` 继续保护既存旧工具正文，不改旧 transcript。
- write 先完整解码、规范化、只读范围预检：按 root 内全部本地 Bot 配置枚举，含其它 owner/shared/orphan，EqualFold 匹配目标；未知或歧义在 skill 文件/默认开关提交前拒绝。库锁内再解析实际当刻全部目标，避免排队期间旧目标集合决定新正文；没有按 owner 缩窄的新增过滤。
- 实际顺序是 source `route.RLock` → root/skills/.write.lock → 本次全部目标解析 → `skill.Write(root,...)` → 逐 Bot `EnableSkill/DisableSkill` 的 config.json.lock（fresh merge）→ 释放库锁 → 释放 route。只序列化本库文件及默认开关的本地 IO；不反取 source operations/host/Jobs/exec locks，不做网络/模型/外部 callbacks，不为本刀另加全局准入或 cross-Bot 事务表。profile 仍按其既定顺序等 source route；没有库锁→反取 route 的路径。
- 赢家持库锁直至全部 defaults 更新结束，所以并发 scoped writes 的最终正文和默认开关由同一写者决定。库级 ImportData/Remove/Migrate 沿原 .write.lock 串行，不把 CLI import 拼成该操作。之后用户手动 enable/disable 保持权威，read/Prompt 从当前磁盘 disabled 现读，不每次 read 重施 scope。
- 真实 config IO 失败允许文件已写、前若干开关已改：回 `{written:true,updated_bots:[...],scope,target,error}` 和非0，明确 file written but scope update incomplete；不撤回真实成功项、不称跨 Bot 原子事务、不吞失败。目标集合内其它 Bot 并发移除/变更按实际 config 错误给 partial，不新增其它 Bot identity 注册或假定原 source route 能保护所有 Bot。
- 写 skill 本身不自动改 persona、role、routine 引用。Remove 的 before=bot.ForgetSkill 保持；scope 字符串只看显式参数，不扫 transcript 猜“只给你”。主线 HarvestRule 仍仅确认提议或主人明确要求后写，只把执行指引换 CLI。

### 只读分类与同刀退役

- 从现 allowReadOnlyMCP 的 `review.DirectXBotArgs` 到纯 argv 分类抽共同直调判定：仅原 canonical 当前 BIN/精确受控 XBOT_BIN 的 POSIX 单直调，保既有拒绝替换/串联/重定向/错误 root 规则。MCP 分支继续用 ParseMCP；skills 用同 ParseSkillRuntime，只放 runtime read --self/help；write、离线 show/import/remove/enable/disable 与任意 xbot 不放进 research。无新权限 UI、白名单缓存或代理角色。
- 同 cut 删除 skillTool/writeSkillTool、旧 decode/Confirm、registry 两 schema 与 links.toolsForLoaded 的 skills 返回工具分支；主线/legacy/continuable 的实际请求中均无 read_skill/write_skill，dev/general 原白名单/跟版说明切 CLI。保其它非 MCP Tools 门、perLocal/Jobs、历史脱敏和 routine 领域消费者；不删除仍有消费者的 skills 摘要/原生器官装配。
- 更新 skill.ReadWith、HarvestRule、manual/子前缀，初始只给窄 CLI 指引，不把全文注固定前缀。离线旧命令业务、设置页技能开关与导入/删除均保留，不顺手迁下一平台族。

### 对照回顾、怎样算对与推进缝

| 已有需求/决定/守护 | 本刀对应与最小检查 |
| --- | --- |
| 母路线批3/原24 skills库存与1c实际一页 | A/B已对照并采用B；首条真BIN/source Shell write(title+完整body)→read，不调Tool.Run冒充CLI |
| ADR0021环境/ADR0043实例/0046资源/0047只读/0049改名 | 原ref/currentIO/现DirectXBotArgs；无新消息账；改名后scope=self仍同Bot且原source固定目录 |
| 生态/现skills355/444/467 | title/空body/默认全部/BoB/真名global/其它owner/shared/orphan；非法歧义零改；原运行另一K手动开关立即影响read；并发正文与默认开关同赢家 |
| 安全/主线+legacy+continuable工具门 | research read/help允许、write/伪直调拒；真实三个provider请求无两旧schema且其它Tools门仍可用 |
| 原routine/read历史/skill离线+ForgetSkill | routine缺失/disabled/原16KiB引用及原记录脱敏；旧show/import/remove/enable/disable有效行为保留，退役工具断言改当前入口不长期挂skip |
| 节点≤1MiB/既有results20MiB | >16KiB尾哨兵完整、HTTP64KiB硬上限及现Shell32KiB整JSON边界、无redirect实际模型收完整artifact引用、产物真实全文/当前目录、stdin/转义后1MiB边界与超限零改、20MiB max+1拒绝 |

本切片已按单一phase合同落地，现状提炼至 [生态](../atlas/ecosystem.md)、[API](../API.md#技能运行时-cli) 与 [ADR0050](../adr/0050-skills-runtime-cli.md)；独立固定五项按首read/write及退门完整候选的精确版本封存，实际级别见 [MILESTONES](../MILESTONES.md)。不重跑已绿profile/MCP或本刀整批；此节保留同母路线已定调用合同，后续仅按新族实际增量推进，不重新开概念审批。

## routine 切片已提炼

3e具体合同及0051窄核后，routine当前实例CLI/actual Timer/原调用审批桥与四模型门退役已提炼至 [API](../API.md#例程运行时-cli)、[内核](../atlas/kernel.md#例程实例与原调用审批)、[ADR0051](../adr/0051-routine-runtime-cli.md)。六项独立所测业务与实现方严格上下文单位的精确phase、fixture修正及当前合入级别只记 [MILESTONES](../MILESTONES.md)；不把它们抬成末docs SHA重跑。以下其它族仍沿母路线推进，不再保留已完成routine合同的重复副本。

## Bot四操作切片已提炼

ccdc具体合同单次窄核后，原source平面list/info/new/delete、动态状态、primary管理、实际partial及原审批/四模型门退役已经提炼至 [API](../API.md#bot-运行时-cli)、[内核](../atlas/kernel.md#bot-发现与管理实例入口)、[ADR0052](../adr/0052-bot-runtime-cli.md)。首链d7、完整519增量与作者/独立各自精确phase及当前验收/合入级别只记 [MILESTONES](../MILESTONES.md)，不留完整合同重复副本。pin/primary另在下节提炼，fork/persona/rolebudget、peers消息/群及以下其余路线未借本刀迁移；主人cli-message-bot/SSE/send设计不在本刀内。

## pin与primary切片已提炼

f609具体合同单次窄核后，原source平面pin/unpin/primary set、primary.json真实commit后pin失败partial、原A失权/B预先endpoint得权与两模型门退役已提炼至 [API](../API.md#bot-运行时-cli)、[内核](../atlas/kernel.md#bot-发现与管理实例入口)、[ADR0053](../adr/0053-bot-pin-primary-runtime-cli.md)。586首A→B链与cc46三个边界的四项独立所测精确phase/当前合入级别只记 [MILESTONES](../MILESTONES.md)，不在母文档复制已完成合同。persona全Bot self/full职责file-stdin/原SelfEdit/nextwake另在下节提炼，fork、卡、role预算、消息群和用户独立owner-SSE设计仍原边界，不借两小写动作扩展。

## 完整self职责切片已提炼

2d9具体合同单次窄核后，所有原source Bot的persona set self完整text/file/stdin、原WritePersona单atomic提交、下一真实wake正文/hash与SelfEdit实际Shell原调用审批已提炼至 [API](../API.md#自身职责运行时-cli)、[内核](../atlas/kernel.md#自身职责与原-selfedit-审批)、[ADR0054](../adr/0054-self-persona-runtime-cli.md)。260首file→nextwake与546完整边界的作者/独立精确phase、当前合入级别仅记 [MILESTONES](../MILESTONES.md)，不留完成合同重复副本。卡/role预算/fork/消息群/vault连接器/用户owner-SSE原边界保；后续remaining平台与Event/Link母路线按实际下一切片继续，不借persona扩展或提前抢步骤。

## 成员聊天群切片已提炼

7bd具体合同单次SINGLE ACTUAL CONCRETE SHAPE PASS后，原list_groups/get_group_message_history/create_group/post_group四动作沿新chat-group list/history/create/post --self提炼至 [API](../API.md#成员聊天群运行时-cli)、[内核](../atlas/kernel.md#成员聊天群与原消息链)、[ADR0055](../adr/0055-chat-groups-runtime-cli.md)。source MEMBER范围/auto-self原Bus、Post原prepared冻结chain-hops及Normal无额外审批、真实群append→Inbox逐投的partial、双32KiB与current FULLartifact边界在实际接缝一处维护，不留完成合同副本。dfe首RO与2bed后四项各精确phase/当前合入级别只记 [MILESTONES](../MILESTONES.md)，五项所测独立端到端分阶段通过，非末SHA整组重跑；owner侧栏group与用户独立owner message-SSE/externalAgent send各原义不混。

单聊message_bot（含bot@device/remoteoutbox≤24h）和fork及其它平台继续按新具体切片推进，仍有native消费者；不借群切片删Link.Tools/Emitter/Messenger/SendPolicy/DeliverDraft，Event/入站Link装配待逐族等价后独立切。用户root四dirtydocs/owner-SSE设计只读避冲突，guard步骤/日用边界保。

## 批 peer-message：单聊切片已提炼并合main98fba5e8

5cdc具体合同单次SHAPE PASS、[ADR0056](../adr/0056-peer-message-runtime-cli.md) Accepted；首40fcf22d LOCAL、完整8472a2a5 REMOTE与唯一message_bot退门的能力已提炼 [API](../API.md#bot-单聊运行时-cli)、[多Bot](../atlas/multibot.md)、[网关](../atlas/gateway.md)，不在母dev保第二份协议/状态说明。六项所测独立端到端为40f首LOCAL与847后五项的精确phase，合同/首/末全部必要静态通过，证据见 [MILESTONES](../MILESTONES.md)；已合本地main98fba5e8787b733f0f45e1b5804f557bdd7e2b89，未主人日用。

本母路线继续保fork/其它尚未迁native与Event/Link入站装配；不因单聊已迁就删其它Emitter/Messenger/SendPolicy/DeliverDraft。下一刀bot-fork以真实main98fba5e8为基线按下节另定具体合同，结构通过前不开实现；owner message-SSE/externalAgent send仍用户独立设计边界。

## 批 bot-fork：交接派生已提炼并合main06daa397

本刀合同、首模型链与末退门必要静态均通过；七项所测独立端到端按实际source/validation phase通过，已合main06daa39768e27d3068b57365602c285f5380d143、未主人日用。实际协议已提炼 [API](../API.md#bot-交接派生运行时-cli)、[内核](../atlas/kernel.md#bot-交接派生与原-source-模型链)、[多Bot](../atlas/multibot.md#bot-交接派生)、[记忆](../atlas/memory.md)，[ADR0057](../adr/0057-bot-fork-runtime-cli.md) Accepted；参数、partial字段和实际写序不在母路线复抄。逐phase与夹具修正归属见 [MILESTONES](../MILESTONES.md)。

本刀仅退fork_bot与确已空的peers工具注册组，保Link/Event其它职责、oldnative历史与bg_插话分支修复；原Memory/Compactor/ctx resolver、oldoffline/WebFork与OwnerSSE边界不扩。source997冻结，父已收最终docs与纯独立commits集成本地main，复用原七有效phase未重已绿首链、六后项或旧族。其余native/platform/EventLink沿未完成路线继续，下一族仅以下经历record/search具体合同，未开代码。

## 批 experience：经历两操作已提炼并合maind632e7a3

仅经历record/search两门；三项所测独立端到端及全部必要SHAPE/增量静态通过，已合maind632e7a372b0418de592e89c4b51dd5045596782、未主人日用。协议已提炼 [API](../API.md#经历记录与检索运行时-cli)、[内核](../atlas/kernel.md#经历记录与检索实例入口)、[记忆](../atlas/memory.md)，[ADR0058](../adr/0058-experience-runtime-cli.md) Accepted；精确source/validation phase、私夹具失败因果与作者证据留 [MILESTONES](../MILESTONES.md)，不在母路线复抄参数/原cap/partial字段。

原Record/Search/SQLite、操作时range/Env/Web/detail与忽略ctx语义保持，不强CallID/owner-personproject资格、不新MemorySession/SQL进度事务框架。仅两wrapper/Known/index/guide退，memory组recall、context四工具及Link/Event装配仍后刀；source815冻，e506仅必要liveblurb一行，父已收doc与pure独立commits整合，复用原三有效phase未重三已绿或旧族。下一Recall仅下方具体合同，未开代码。

## 批 memory-recall：单一文件检索已提炼并合main1053d0a9

仅recall一个能力；三项所测独立端到端与作者最小有效检查、必要SHAPE及首十一/退门十一/eval二source三轮实际静态全部通过；已合本地main1053d0a9bb735e5de90ba75fcd3681c5742b3bf0、未主人日用。实际协议提炼 [API](../API.md#可见记忆文件检索运行时-cli)、[记忆](../atlas/memory.md)、[内核](../atlas/kernel.md)、[权限](../atlas/security.md)，闭合评测接缝见 [模型与评测](../atlas/providers.md)；[ADR0059](../adr/0059-memory-recall-runtime-cli.md) Accepted，源/validation/实际phase与夹具错误因果留 [MILESTONES](../MILESTONES.md)，不在母路线复抄参数/cap/人物选择表。

原source prepared非nil人物ctx与唯一FS scanner、精确scope/date/2000files4M/20hit200runes保持；一次currentIO directcurrentb.Name保护原文件访问，无新scope许可或Memory服务。只退recall wrapper/Known/index及确空memory TOOLgroup，保原Link-Event/Memory器官/文件写守卫/context四工具/detail。闭合eval仅限定可信BIN argv恢复原只读host记忆能力，其余Shell隔离不变；source4722后只test/doc，不为最后SHA重三有效业务场景或旧审批族。原三业务phase与eval按各实际版本封存，TRUE main仅compile/diff确认不重跑；下一Context三读仅下方Proposed合同，具体SINGLE SHAPE通过前不开实现，不并compact或第二代码面。

## 批 transcript-read：三读具体合同PASS，实施中

基线 TRUE Recall main1053d0a9bb735e5de90ba75fcd3681c5742b3bf0（已FF，产品与独立验证差异0封存，不重原三phase/作者二十二项）。本刀仅原transcript READ3：recall_history/expand/detail；compact模型与State写、Link/Event去留另刀。现有方法/附件/权限证据已对照context/kernel/security、ADR0021与1c冻结全清单，不增环境账/ctxMetaFrame。具体结构决策 [Accepted ADR0060](../adr/0060-transcript-read-runtime-cli.md)，c51fb3d7两docs已获24 SINGLE ACTUAL CONCRETE SHAPE PASS，现按原导出方法实施。独立入口红purea670f223/sourcec51-validation79cb6b40 ONLYEntry FAIL1.06/package20.815s（含build启动非p95）为actual非primarySource NativeSH规范BIN未知transcript noun、无FTS/owner/Detail领域假红；首compiled即早交，不等全部单位/退门/首反馈。

### 实际caller与请求

```sh
"$XBOT_BIN" transcript search --self [--query TEXT|--query-file FILE|-] [--bot NAME] [--json]
"$XBOT_BIN" transcript expand --self --from N --to N [--bot NAME] [--json]
"$XBOT_BIN" transcript detail --self --seq N [--experience ID] [--json]
"$XBOT_BIN" transcript --help
"$XBOT_BIN" transcript detail --help
```

原source Shell拿search返回的真实RoleTool seq，再expand同条配对工具结果，不重执行原工具。Detail保普通seq1基与experience>0下seq0基offset两个锚、source-only，无bot/target参数；原experience<=0仍普通seq。新纯ParseTranscriptRuntime早于resolveRoot/default/legacy，help离线，其余self；无alias/位置参数，拒未知/重复flag、按arity、字面--help/--作值。search query可缺/空、与query-file互斥且file路径不可空，raw文本/文件/stdin max+1及escaped typedJSON各1MiB；expand必须from/to、detail必须seq，只解析Go整数/溢出，实际正数/range交原方法，不改历史<=0规则。bot原样交原trim。sourceEndpoint/canonicalRoot权威，显式root同一、旧XBOT_BOT仅诊断；不离线Load旧source或新K。

### 两个实际shape与原方法直用

A `cmd→offline TranscriptReader{globalStore,sourceName,owners,workspace}→自查Search/Range/Blob`必须重造owner/currentRef/目标附件/secret/fence和原读取维护副作用，rawBlob也不等原4/8/64KiB语义文本，淘汰。B `cmd→cliops三typedRPC→现K.TranscriptHandlers→原导出API`，原RecallHistoryBot(query,name)/ExpandBot(name,from,to)/Detail(seq,id)直接复用，不再extract domain/通用operationEnvelope/新StoreService或target registry。在既有Handlers挂一个Transcript *TranscriptHandlers，Search/Expand/Detail各具体callback；DTO分别`{query,bot?}`、`{bot?,from,to}`、`{seq,experience?}`，Detail编译类型里无Bot。CallTranscriptSearch/Expand/Detail(ctx,endpoint,root,input)分别POST `/v1/transcript/search|expand|detail`，无CallID参数/要求，普通共享只读回包`{text?|file?,access?,bytes?,error?}`，不rows/count；FormatTranscriptRead生成准确human。三原Run忽略ctx/API无ctx，不runtimeSourceCall/nonnilprepared/审批owner重绑/primary-personproject资格，缺/旧/finished/foreignCallID不新拒；ctx仅传输取消，承载Shell原gate独立，无新卡或approver。

self search/slice、expand count/slice/hydrate已有一次currentIO，Detail几次currentIO跟原sourceRef；不能外套route再递归currentName/lineNote，否则有writer等待下RLock死锁风险。foreign原trim目标名，空或CURRENTsourceName self，别名无自动旧名映射；否则bot.Load(k.Bot.Root,name)一次，原k.Bot.Config().Owners（不改用显式fresh Resources.current）∩目标加载时盘Owners非空。同群/可发消息不越权，missing/loadErr/foreign/orphan统一原history ErrNotFound，授权后读盘ERR保真；foreign无稳定ref，不承诺跨其rename跟随，不加cache/CAS。原readSegStore.with/ensure会mkdir/lock/迁旧串/修SQLite，读非物理零IO；Detail也不扩成跨多次IO一致快照事务。

### 原语义文本、输出与退门

History原FTS/BM25最多8hit storedPreview/nohydrate、snippet实际240BYTES非runes、forkskip/正文空才toolBlob、whole4KiB/noMatch原词法。Expand inclusive1≤from≤to≤COUNT、summary覆盖seq仍可取、每条hydrate8KiB+whole8KiB、seqrole与目标workspace附件line-note、正文空才toolBlob保。Detail sourceExperience正id范围/普通seq、本地格式时间、HydrateLimit64KiB+WHOLE64KiB、正文非空也toolBlob保；不是无限Blob/附件base64导出。原presentHistory先k.Redact/fallbackBot再cap，foreign原Neutralize+完整Wrap(sourcebot:target/typeTranscript/sendertarget)，膨胀缩body重Wrap及原极端fallback不顺修；self不新fence。

新边界持原operations lease，方法调用不外包currentIO；仿experienceSearch仅补held/Resources secret scrub，不重建/二次裁原Text。actualJSONNL与准确human各32KiB/HTTP64KiB，超任一复用work.WriteResult当前SOURCE工作区0600/20MiB FULL这份semanticText JSON含附件/fence/newline，非target写区/无限原Blob。32hex metadata双格式写前预检、Bytes实际，真正写才短currentIO按currentRootName+rebasedWorkDir；error动态脱敏可缩显示、失败非0无假File，CLI先回实际事实再报错不retry。三请求严格object/unknown/null/尾随/URLquery拒，沿原Host/browser/loopback/root guard，不新增通用产物框架或scope服务。

fit.omittedResult原seq→expand、无seq→history→expand替成这份实际CLI，保DO_NOT_RERUN/customnoSeq；ExperienceSearch末detail提示、manual/context、liveLinkContext及readonly/Registry/Known/schema/describe按actualcaller迁。research同DirectXBotArgs当前BIN/精确XBOT_BIN单POSIX直调+本parser只self/help，不Contains/操作符。breaker只迁原recall_history对应search的宽循环阈值，expand/detail不额外放宽。只退三wrapper/Known/index，ContextTOOLgroup保Compact与LinkContext器官/Event及4K记忆写guard；旧native历史literal/toolpair兼容读，无暗可执行shim/伪native配对/ENV/Inbox/waker。实际mock/eval精确grep无三读producer，不机械改MemoryRecall专用evalC桥。

### 最小验收与EARLY交付

SINGLE具体shape通过后才0d固定最窄真实入口红；首可编译candidate即交source Shell search→Expand原paired tool-result/full hydrated语义，无需重执行，不等所有边界与末文档。私有query-file预置needle（不能把needle写进SH命令造成FTS自索引淹掉仅8hits），精确从返回结果定位RoleTool真实seq；原唯一payload与counter证实原工具只执行一次，不任意命中/seq存在冒消费成功。无CallID/旧CallID不新拒在作者typed源单位覆盖，不借Recall冻结ctx门。

同cut继续sharedOwner交集/foreign-orphan-group统一mask、授权后真实diskerr、foreign长文本Neutralize完整fence/目标附件路径，sourceDetail rawseq与experienceoffset两锚原FULL10K、64K semanticText双32K artifact、sourceRename后self/blob/产物当前路径与旧BOTS-WORK缺席。0d owned Experience chain194–203一行两个liveDetail司机只由0d窄迁、全文与两锚断言保；八处readonly describe无关control由0d换实际仍live控制，不改旅程要求/整Experience族重跑。作者ExperienceRuntimeTest170两锚保，原方法仍live则不机械改直调测试。必要new+actualaffected按docs/TESTING最小once，旧FTS/Memory/Context全族/经验三-Fork七-Message六-Group五与原群三审批/未变sourceResolver证据复用，不提前重跑。Root4USERdocs/SSE/guard/main/push日用不动，当前仅这一合同代码面。

## 还没做的

- 主人日用验收尚未开始。普通资料首刀的独立审计、真入口验证与合入已完成；后续不重复该刀设计审批，按新切片复核实际增量。
- 批 0 的后续实际操作及入站依赖清单、尚未迁族的 research 业务只读分类、远端 runner 回连；本批实例/二进制/root/本机当前 Bot 身份和离线帮助通路已建立，不能等同人用 PATH 安装。
- profile 完整组合、头像原字节上传/clear、原运行态连续及模型门退旧已提炼 atlas，固定五项独立所测端到端按各自精确版本通过，证据见 MILESTONES；原 chat loop 下一输入/Timer handle 及其 recover、回放、通知实际消费者也已以局部红绿和 reviewer24 至 `63762ae5` 连续增量 PASS 收口，已合入本地 main，尚未日用；合并时与主线持久选择的交叠补单项真实浏览器红绿，不重跑固定五项。已通过的普通字段、avatar_seed、MCP 和本刀五项不重复整批验收，母路线其它族继续在本 dev 中推进。
- 后续各平台族继续按实际增量迁移；已合入的 MCP CLI 不重复本刀审批或精确已绿证据。主机日用仍待用户实际使用，现状见 atlas。
- skills read/write、routine、Bot四操作/control/persona与chat-group已提炼现状并合main；peer-message已合main98fba5e8。bot-fork已合main06daa397；经历record/search已合maind632e7a3；memory-recall已合main1053d0a9，下一仅transcript三读待具体SINGLE SHAPE；连接器及剩余目录管理等平台能力继续逐族迁移。
- 事件结构与投递摆脱 Link 包依赖，删除 Link 装配与旧入口。
- 基础模型工具集合和代理门的最终去留；同步规则、手册、评测、atlas。
- 各批独立冒烟、相关旅程回归、性能与上下文收益测量、主人日用验收。

落地一批后只把已实现的现状提炼进 atlas；收尾按项目约定处理本 dev 和 guard，本轮不提前把目标写成现状。
