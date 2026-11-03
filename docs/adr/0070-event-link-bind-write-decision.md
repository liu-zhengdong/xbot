---
kind: adr
guard: owner
status: Proposed
date: 2026-11-02
---
# 0070 Event/Link 绑写决策：剩余写类工具全部保留模型侧，零新 CLI 面

## 原能力与调用方

基线 main `ddfb6460`（ToolsLink 六件改 kernel 行已合 `bc20c603`）。批 4「出入站解耦」的最后一块是：剩余写类模型工具里，哪些本质是「发 Event / Post Inbox」（留 Event 线，不动模型注册），哪些是「直接 domain 动作」（候选 CLI 化）。主人要求：「Event 这条线保留住是可以的」——`link.Event` + `inbox.Inbox` append-only log + `Post`/`Ack`/`Wait` + `wakers` 唤醒链是保留线，不解耦。

盘点依据 remaining-inventory §1.1/§1.3 与本刀逐个源码核对。剩余写类工具共六族：

- `send_message_to_user`（`tools.go:105-171`，chat 行 `always`）：`{text?, importance?, images?, audios?}`；`attachOutImages` 先落 paste/ 校验；`imageTurn(ctx).turnSource` 分叉——terminal/web 源时直接出泡回 `sent to the owner's chat`（不写 outbox），其它源走 `deliverNote`→outbox append + desktop 通知回 `notified the owner`；`pushFindings`（`:195-227`）同样按 `turnSource` 判。回执文本是 `pushFindings` 覆盖判据、web SSE `result`/`done.text` 出泡判据、`sentToChat`（`reply_inputs.go:44`）判据、bot.Chat `chatSay` 判据——**「唯一可见通道」语义由回执原文承载**。
- `ask_user`（`peers.go:19-48` + `questions.go` 全文件）：`askOwner` 建 `ownerQuestion` 卡（ApprovalPending 行入 transcript）、30min timer 自动选 recommended；`ChooseQuestion`/`AnswerQuestions`/`deliverQuestion` 回投链——先 `InterjectInternal`，失败再 `link.NewEvent(NameApproval)` → `box.Post` + `wakers` 唤醒。完成事件绑 Event/Inbox 保留线本体。
- `dispatch`/`spawn`（`dispatch.go:50-87`/`agents.go:345-400`）：`StartSubagent`/`spawn` → `ensureExec().Start`（exec.Local 或 Remote）→ `forwardExec`（`agents.go:265-313`）把 `exec.EvDone/EvFailed` 转成 `link.Event{Source: SourceSubagent, Untrusted: true}` → `withCurrentIO` → `box.Post` → `wakers` 唤醒。排队回执「已开始/已排队」告诉模型别自己干；完成结果经 Event 异步回投，是 Event 保留线的第二大生产者。
- `agent_message`/`agent_stop`/`steer`/`queue`/`interrupt`/`cancel`（`continuable_subagents.go:314-368`、`agents.go:505-597`）：`messageSubagent`（prepared turn sender `"bot:"+currentName`）/`StopSubagent`/`control` → `exec.Continuable.Message`/`exec.Local.Stop`/`executor.Control`——进程内 executor 活表操作，descriptor 存在时经 `MessageSubagent`/`StopSubagent` 走 continuable 分支，否则 `ensureExec().Control`。原 `Run` 读 ctx 仅用于传入 executor call。
- `code_start`/`code_steer`/`code_queue`/`code_interrupt`/`code_cancel`/`code_show`（`internal/code/link.go:108-270`）：`code.Link` 是「Link 装配 + 事件生产」双面（inventory §2.4 已列最大残留）；`rec.Start`/`rec.Control`/`nudge` 写 task record，`rec.OnTerminal`→`l.post`→`box.Post` 完成事件（`link.go:80-106`）。`code_show` 是 Readonly 读（`rec.List`/`rec.Get`/`formatTask`/`output` tail），原 Run 忽略 ctx——与已迁 subagent 三读/shell jobs 同档的 runtime 读候选，但本刀不迁（理由见决策表）。
- `generate_image`/`listen`/`tts`/`todo`（image_gen/audio/todo kernel 行）：绑 provider 登录态与工作区落盘（生图/音频）、绑本 bot todo 文件状态与 `UpdatedStep` 当轮提醒（todo）——inventory §3 归「厂商协议能力/当轮计划工具」，母路线归批 5 收口决策，不在本刀判。

## 决策

**剩余写类工具全部保留模型侧注册，批 4 零新 CLI 面。**「发 Event / Post Inbox」的工具不是「把 Event 链路换成 CLI」而是「工具本体就是 Event 链路的一部分」——它们的写动作终点是 `box.Post` 或进程内 executor/record 活表，CLI 化既不换也不简 Event 线，只是把同一动作挪进 `xbot` 子命令 + `/v1/*` handler，与 ADR0069「六件不迁 CLI 是同档判据：模型工具语义绑 kernel 状态，迁走失去 turn 内上下文」同一判定。

按「本质动作」逐族判：

| 工具族 | 本质动作 | 绑什么 | CLI 化判据 | 结论 |
|---|---|---|---|---|
| `send_message_to_user` | turn 内出泡/通知投递 | `turnSource` 分叉（terminal/web 出泡 vs outbox+desktop）、`attachOutImages` 回执挂 reply generation、`pushFindings`/`sentToChat`/SSE `result`/`chatSay` 五处按回执原文判定 | 回执原文是领域判据本身；CLI 化后 `sh` 回执不再含 `sent to the owner's chat`/`notified the owner`，五处消费者全断——要么伪造回执（与旧工具配对假象同档禁止），要么重写整条说话链 | **保 kernel 行** |
| `ask_user` | owner 选项卡 + Event 回投 | questions 全局表 + `InterjectInternal`/`NameApproval` 事件 + wakers | 卡本体是 owner 交互面（`ApprovalCardTextAt` 入 transcript、UI 决策卡渲染），回投是 Event 保留线——CLI 化是「用 CLI 发 Event」，既不省 Event 依赖又丢卡内 turn 语境 | **保 kernel 行** |
| `dispatch`/`spawn` | 起 executor 任务 + 完成 Event | `ensureExec`/`forwardExec` Event 回投、`adoptScope` cwd、`modelOverlay` persona 装配、`forkMessages`/`completedSubagentHistory` fork 上下文 | fork 模式读 prepared turn 的 `currentState`/`memSnapFor`/`assemble`——prepared ctx 依赖比三读深一层；完成事件本就走 Event 线 | **保 kernel 行** |
| `agent_message`/`agent_stop`/`steer`/`queue`/`interrupt`/`cancel` | 进程内 executor 活表控制 | `exec.Continuable`/`exec.Local` 活任务表、descriptor 二分、`"bot:"+currentName` sender | 活表只在本实例（同 jobs/subagent 判据）；但控制动作无一读 Event/Inbox——它们写 executor，executor 完成才产 Event；属「domain 动作候选」面，本刀不迁（见下条边界） | **保 kernel 行** |
| `code_*` 五写 | coding record + 完成 Event | `code.Record` 目录、`launch`/`nudge` 活 run、`post`→inbox 事件 | 完成事件绑 `code.Link.post`（inventory §2.4 最大残留）；`code_start` 的 backend 探测（`hasBackend`/`tools.CurrentProcess`）是本实例进程态 | **保 organ link** |
| `code_show` | 读 coding record 目录 | `rec.Dir` 本 bot 文件、`output` tail 活写器 | 读 record 目录可离线近似（同 transcript 三读），但 `output`/`tail` 是活 run 的写器尾（`l.live`/`l.tail` map 在本进程）——离线读不到活 tail，runtime `--self` 可达但收益面只一个工具 | **保 organ link**（不迁，理由见下） |
| `generate_image`/`listen`/`tts` | provider 调用 + 工作区落盘 | `imageGenCapability`/`audioBackend` 登录态、`withCurrentIO`/`StoreVoice` | 厂商协议能力，批 5 收口才定（inventory §3 已列） | **保 kernel 行** |
| `todo` | 本 bot 任务清单文件 | `k.todo` 状态、`UpdatedStep` 当轮提醒、`todoPolicy` | 当轮计划工具，迁走失去 turn 步进提醒；inventory §3 已建议保留批 5 定 | **保 kernel 行** |

**关键判定：六族里没有「纯 domain 动作」。** dispatch/spawn 的完成回投、`code_*` 的完成事件、`ask_user` 的卡回投都落在保留的 Event/Inbox 线上——这些工具不是「可以换传输的 domain 动作」，是 Event 生产链的**入口侧**。`send_message_to_user` 更极端：它的回执文本本身就是领域判据（`pushFindings` 覆盖、`sentToChat` 判定、SSE 出泡、chatSay 投影），CLI 化的「等价」只能是伪造回执。`steer`/`queue`/`interrupt`/`cancel`/`agent_message`/`agent_stop` 是唯一一组「写 executor 不直接碰 Event」的工具——但 `control`/`messageSubagent`/`StopSubagent` 全走 `execSnapshot()` 进程内活表 + `ensureExec` 懒建 + descriptor 二分，CLI 化要新 `xbot subagent steer|queue|interrupt|cancel|stop --self` 五个 subcommand + runtime handler 复刻 `exec.Continuable.Message`/`exec.Local.Stop`/`executor.Control` 三分支——收益只是「模型多一次 shell 调用」，不是 Event/Link 解耦的实际增量；`code_show` 同理（活 tail 读不到离线、单工具 runtime 收益面太窄）。两者与 ADR0066 `shell_cancel`「写类可同批迁」不同档：cancel 是独立 noun 的三工具组，这里是同一 noun 的六个控制动词、与三读已迁的 `subagent` noun 同族——批 5 收口时若迁，照 `subagent list|read|expand --self` 先例加 `subagent steer|queue|interrupt|cancel|stop --self` 即可，**本刀不预先开 noun 不写合同**。

## 退役面

零。本刀不动模型注册、不动 Event/Inbox、不动 `link.Link` 装配、不动 `mountGrantLocked`/`snapshotLinks`、不动 `code.Link`/`mcp.Session` 的双面角色。`link` 包的 `Event`/`NewEvent`/`InterjectInternal`/`wakers`/`postCurrentEvent` 全保——`link.go:107-127` Event 型与 §2.2 的 17 个生产者原样。

## 保留面

- 六族工具本体与注册行原样：`chat` 两行（send/ask）、`agents` 行（dispatch/spawn+控制分叉）、`code.Link.Tools()` 六件、image_gen/audio/todo kernel 行。
- Event/Inbox 保留线全链：`link.Event` 型、`link.NewEvent`、`inbox.Inbox` `Post`/`Ack`/`Wait`、`postCurrentEvent`、`wakers` sync.Map、`InterjectInternal`/`deliverQuestion`/`deliverDecision`、`forwardExec` Event 回投、`code.Link.post`。
- `link.Link` 接口三面：`Tools()`（desktop/browser/code/connector/mcp 供工具）、`Emitter`（timer）、`Messenger`（connector 草稿链）原样——§2.4 的「退役 Link 要动什么」清单整体不启动。
- `send_message_to_user` 的回执契约：`sent to the owner's chat`/`notified the owner` 原文、`attachOutImages` 挂 reply generation、`pushFindings` 覆盖判定、`sentToChat`/`chatSay`/SSE `result`/`done.text` 消费点全保。

## 边界

- 不把「保留 Event/Inbox」读成「所有写工具都迁 CLI 另开一条发消息的道」——send/ask/dispatch/code 的写终点本就是 `box.Post` 或本实例活表，CLI 化是同动作换传输不换依赖；与已迁各组的「领域真源在本实例活表」判据方向相反（那些是「读活表必须 runtime」→CLI `--self`；这里是「动作本身是 Event 入口」→保模型）。
- 不把 `steer`/`queue`/`interrupt`/`cancel`/`agent_message`/`agent_stop`/`code_show` 写成「已决定 CLI」——它们是批 5 收口候选（`xbot subagent <verb> --self` 同 noun 加 subcommand、code 单工具收益面窄），本刀不预留 noun、不写合同、不动 `continuable_subagents.go:443` 负面清单。
- 不把 `generate_image`/`listen`/`tts`/`todo` 写进本刀——厂商协议能力与当轮计划工具按 inventory §3/母路线批 5 收口定，与 Event/Link 无关。
- 不把「Event 上移到 inbox 包」提前——§2.4 列的「生产者不再 import link 只为 Event」是批 4 后续代码动作，本刀只定模型工具面；`link` 包瘦身（Event 移走、Link 只剩 grant/catalog）是另一个实现切片，不预设形状。
- 不动 `code.Link`/`mcp.Session` 的双面角色——「工具注册」与「完成回投」拆分是批 4 后续 Event/Link 解耦切片，本刀决策的是「模型工具面不迁 CLI」，不等于「Link 本体不解耦」。

## 接受的代价

- `xbot` CLI 面不再增长 subagent 写类 subcommand——模型侧控制动词走 `tools` 门直达 kernel 行，人侧 owner 控制（`MessageSubagent` 的 authenticated human entry）仍走 web/API 原入口，不新增 CLI 等价。
- 批 4「出入站解耦」的模型工具面收口为零迁出——剩余 `link.Tools()` 消费者（desktop/browser/code/connector）与 `link` 包瘦身、Event 上移、`mountGrantLocked`/`snapshotLinks`/`MountOrgans`/`cmd_serve mount()` 合一留作后续实现切片，本刀是决策冻结不是代码动作。
- tool_index 的 `agents`/`coding-agent`/`desktop`/`browser`/`telegram`/`lark` 组原样保留——模型仍经 `tools` 门 `describe`/`call` 到达，无双入口。

## 最窄验收

1. `registry.go` kernelTools 的 chat/agents/image_gen/audio/todo 行原样；`linkSource` 循环仍注册 desktop/browser/code/connector 工具；`hotTools` 含 `send_message_to_user`。
2. `sendMessageTool.Run` 的 `turnSource` 分叉与回执原文不变（`sent to the owner's chat`/`notified the owner`）；`pushFindings`/`sentToChat`/`chatSay`/SSE `result` 消费点原样。
3. `questions.go` 的 `InterjectInternal`/`deliverQuestion`/`NameApproval` 事件回投链原样；`forwardExec` 的 `SourceSubagent` Event Post 原样；`code.Link.post` 原样。
4. `continuable_subagents.go` 的 `messageSubagent`/`StopSubagent`、`agents.go` 的 `control`、descriptor 二分原样；`link.Link` 接口、`link.Event` 型、`link.NewEvent`、`wakers` 原样。
5. 无新增 `xbot subagent steer|queue|interrupt|cancel|stop` 或 `code` noun；`allowReadOnlyPlatform` 不新增分支；`cliProfileGuide` 不新增写类指引。

本文保决策边界：不把「Event 保留」读成「写工具都该迁 CLI 发消息」（写终点本就是 Event/Inbox 或活表）、把批 5 收口候选（subagent 控制动词、code_show、厂商协议、todo）提前开合同、把 Event 上移/Link 瘦身写成本刀动作（它们是后续实现切片）。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。
