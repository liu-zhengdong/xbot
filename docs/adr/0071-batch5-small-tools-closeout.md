---
kind: adr
guard: owner
status: Proposed
date: 2026-11-02
---
# 0071 批 5 小工具收口：剩余模型工具全部保 kernel/link 侧，零新 CLI 面，mcp readonly 不开写

## 原能力与调用方

基线 main `a6ccb891`（批 4 已冻结：ADR0070 Accepted，send/ask/dispatch/code 四族保模型侧、零新 CLI 面；ToolsLink 六件改 kernel 行已合 `bc20c603`）。批 5 是小工具收口——对批 4 留下的收口候选（subagent 控制动词、`code_show`）与 inventory §3 的厂商协议能力、当轮计划工具、mcp readonly 写端逐族定去留。逐条源码核对如下。

**subagent 控制动词六件**（`agents.go:505-585`、`continuable_subagents.go:314-368`）。注册二分（`registry.go:135-144`）：`k.Dispatch` 为真时 `agents` 行产 `dispatch`+`agent_message`+`agent_stop`，为假时产 `spawn`+`steer`+`queue`+`interrupt`+`cancel`——两套不同时出现。动作终点全是本实例进程内 executor 活表：

- `k.control(ctx,id,kind,text)`（`agents.go:424-455`）：`execID` 校验→`withCurrentIO` 读 `current.AgentDir(id)` 的 descriptor——descriptor 存在（continuable 孩子）时 steer/queue→`MessageSubagent`（→`exec.Continuable.Message`）、cancel→`StopSubagent`（→`exec.Local.Stop`）；否则落 `ensureExec().Control`（Sender 硬编 `"owner"`）。`ctx` 只传入 executor call，无 turnKey/turnSeq 消费点。
- `agentMessageTool.Run`（`continuable_subagents.go:326-340`）：`messageSubagent(ctx,id,text,delivery,"bot:"+currentName)`——sender 是 kernel 侧硬编身份，不接受调用方声称；`execSnapshot().(exec.Continuable)` 断言失败回 `ErrNotContinuable`。
- `agentStopTool.Run`（`:352-364`）：`StopSubagent`→`execSnapshot().(*exec.Local).Stop`，回 `subagentReceipt`（task JSON）。

**code_show 与 code 五写**（`internal/code/link.go:108-270`）。`code_show`（`showTool`，`:242-270`）`Run(_ ctx,args)` 忽略 ctx：无 id→`rec.List()` 逐行 `id\tstatus\tgoal`；有 id→`rec.Get`→`formatTask`，`task.Result` 为空时回落 `l.output(id)`（`card.go:21-42`）读 `l.tail[id]`——**活 run 的写器尾在 `l.live`/`l.tail` 两张进程内 map**，record 目录离线可近似、活 tail 离线读不到。`code_steer/queue/interrupt` 走 `control()`（`:272-286`）：`rec.Control` 写 record→`l.nudge(id,kind)` 推活 run；`code_cancel` 同形加队列递进；`code_start` 的 `hasBackend`/`tools.CurrentProcess` 是本实例进程态。五件同样 `Run(_ ctx,args)` 忽略 ctx，但完成事件绑 `rec.OnTerminal`→`l.post`→`box.Post`（`link.go:80-106`）——Event 生产链入口侧，批 4 已定保。

**generate_image/listen/tts**（`image_gen.go:109-152`、`audio.go:185-248`）。三件绑厂商协议与本实例装配：`generate_image` 走 `imageGenCapability` proxy（`resolveImageGen`：web 配置→hosted 判据→provider 登录态→`provider.New`），落盘经 `withCurrentIO`→`work.WriteGenerated`；`listen` 经 `readAudio`（`withMediaIO` 的 scope cwd/roots 解析或 voice id）→`audioBackend`（绑 boundProvider endpoint 或指定已配置 provider）→`a.Listen`；`tts` 同 backend→`a.Speak`→`withCurrentIO`→`work.StoreVoice` 落 `voice/`。回执路径是当轮 `send_message_to_user` 的 images/audios 入参——工具产出绑回复挂泡链。

**todo**（`todo.go:603-676`）。`todoTool.Run(_ ctx,args)` 写本 bot todo 文件 + `k.todo` 内存态：`f.Step`/`f.UpdatedStep` 是本实例的当轮步进计数（`noteTodoStep` 按 tool round 递增），写完 `k.liveTurn().todoTouched = true` 标当轮已碰清单（`:670-674`）。`todoQuiet`/`todoNote` 的提醒与完成门消费 `UpdatedStep` 与 `todoTouched`；`todoToolsQuiet` 还回扫 `mountedTools` 判整轮只读。工具本体是当轮计划机制的写入端。

**mcp readonly 写端**（`mcp_readonly_shell.go:97-116`）。research 子代理经 `readOnlyShell` 包壳调 `xbot` 时，`allowReadOnlyPlatform` 的 `mcp` 分支：`call` 只在 `parsed.ReadOnly` 为真放行（即 `xbot mcp call --self --readonly`），写调用拒；`list/tools/describe` 与 help 放行。research 类型定位「只读不动作」（`agenttype.go:55-60`）。

## 决策

**批 5 零新 CLI 面、零新 allowReadOnlyPlatform 分支：六候选全部保现状。** subagent 六控制动词与 `code_show` 不开 `xbot subagent <verb>`/`xbot code` 新 subcommand；generate_image/listen/tts/todo 保 kernel 行；`mcp call` 写端在 research 下继续拒。按「本质动作」逐族判：

| 工具/组 | 本质动作 | 绑什么 | CLI 化判据 | 结论 |
|---|---|---|---|---|
| steer/queue/interrupt/cancel | 写本实例 executor 活表 | descriptor 二分→`exec.Continuable.Message`/`exec.Local.Stop`/`executor.Control`；`execID` 校验 | 活表只在本实例（同 jobs/subagent 判据），`--self` 技术上可达；但动作本身无 prepared ctx/CallID/审批消费点——迁 CLI 只是把同一 map 写挪进 `/v1/*`，领域真源仍在 kernel，零解耦增量 | **保 kernel 行** |
| agent_message/agent_stop | 同上（dispatch 模式对） | 同上加 sender 硬编（`"bot:"+currentName` vs `"owner"`） | 同上；另有 sender 选型问题：`xbot subagent message --self` 要对所有经 sh 的调用方选定一个 sender——现码模型侧固定 `"bot:"+currentName`、人侧 `MessageSubagent` 固定 `"owner"`，CLI 无 turn 判定调用者是人还是 bot 孩子，须新增 sender 决策 | **保 kernel 行** |
| code_show | 读 record 目录 + 活 run tail | `l.tail`/`l.live` 进程内 map | record 可离线近似，活 tail 离线读不到须 `--self`；但单工具开 `code` noun 收益面太窄，且 `code_*` 五写是 Event 入口侧已批 4 保模型——只迁读把同族模型面劈成两半 | **保 organ link** |
| generate_image/listen/tts | 厂商协议调用 + 工作区落盘 | `imageGenCapability`/`audioBackend` 登录态、`withCurrentIO`/`withMediaIO` scope roots | 厂商协议能力（inventory §3）；落盘绑 bot 目录与 scope roots，`--self` 须复刻 `withMediaIO` cwd 解析为第二份真源；回执喂当轮 `send_message_to_user` 挂泡 | **保 kernel 行** |
| todo | 写本 bot 清单 + 当轮步进 | `f.Step`/`UpdatedStep` 计数、`liveTurn().todoTouched`、todoQuiet 扫 mountedTools | `UpdatedStep`/`todoTouched` 是当轮语义：runtime 调用无 turn 归属，`todoTouched` 会标到错 turn 或不标→完成门失效；要保语义须 prepared ctx/turnSeq 归属——为迁而造 turn 依赖，方向相反 | **保 kernel 行** |
| mcp call 写端（research） | readonly 包装下的 MCP 写调用 | `parsed.ReadOnly` 标志 + DirectXBotArgs 分类器 | readonly 是工具 schema 自报标志不是能力证明；research 定位「只读不动作」，开放写端让任意 MCP 副作用绕过文件级只读边界 | **保拒** |

**关键判定：收口不等于迁移。** 批 5 的职责是给批 4 留下的候选逐族落「留」的决定，与批 4 同一战略边界——写终点在本实例活表或 Event 入口侧的工具保模型侧注册。subagent 六控制动词是唯一「写 executor 不直接碰 Event」的组，技术可达（`--self`→`/v1/subagent/<verb>`→`holdOperations`→原 `k.control`/`messageSubagent`/`StopSubagent` 方法体），但判据上不达：

- **领域真源不移动**。handler 复用同一 `k.control`/`messageSubagent`/`StopSubagent`，executor 活表仍在 kernel——与 transcript/jobs 读的「读必须回同实例」不同，这里迁完模型多一次 sh+HTTP 跳，活表一份也没少。
- **sender 无源可判**。模型侧 sender 是 `Confirm`/`Run` 所在工具硬编的（`"bot:"+currentName`/`"owner"`）；CLI 入口对「经 sh 调用的人」与「经 sh 调用的 bot 孩子」无区分手段，选 `"owner"` 改变现模型语义、选 `"bot:"` 改变人侧语义，新增的是一个不存在的身份判定面。
- **一致性对称在 noun 不在机制**。`subagent list|read|expand --self` 迁出是因为三读是对目录/活表的纯投影；控制动词是驱动 executor 的写动作——`subagent` noun 加写 subcommand 只是字面同族，机制上是把批 4 已冻的「Event/executor 入口侧保模型」再开一道侧门。
- **人侧控制已有入口**。owner 经 web/API 的 `MessageSubagent`/`StopSubagent` authenticated 入口 steer/stop 孩子；`xbot subagent steer --self` 作为「人在终端控制 bot 孩子」是新用户面，不是模型工具迁移——不在收口范围，如要另开切片。

todo 另有一条独立的 turn 归属判据：`todoTouched`/`UpdatedStep` 靠 `liveTurn()` 标当轮；`--self` HTTP 调用到达时 `liveTurn()` 或是别的 turn 或 nil——CLI 写入天然丢失「这轮已写清单」语义，保语义就须引入 prepared ctx/CallID 资格链，为单个工具造一条本没有的依赖。

## 退役面

零。本刀是批 5 决策冻结，不动模型注册、不动 `kernelTools` 行、不动 `code.Link`、不动 `allowReadOnlyPlatform`、不动 `continuable_subagents.go:443` 负面清单。

## 保留面

- `agents` kernel 行原样（`registry.go:135-144` 的 `k.Dispatch` 二分与六件工厂）；`k.control`/`messageSubagent`/`StopSubagent`/`subagentReceipt`/`parseControl`/`execID` 原样；descriptor 二分（`agents.go:431-450`）原样。
- `code.Link.Tools()` 六件原样；`l.live`/`l.tail`/`l.output`/`nudge`/`rec.Control`/`OnTerminal`→`post` 完成事件链原样。
- `image_gen`/`audio`/`todo` kernel 行原样；`imageGenCapability`/`resolveImageGen`/`withImageGen`/`audioBackend`/`readAudio`/`withMediaIO`/`prepareOutAudios`、`k.todo` 状态机与 `noteTodoStep`/`todoNote`/`todoToolsQuiet`/`todoGated`/`todoTouched` 当轮消费链原样。
- `allowReadOnlyPlatform` 的 `mcp` 分支原样：`call` 只在 `parsed.ReadOnly` 放行；无 subagent 写动词、code、image、audio、todo 新分支。
- `xbot` CLI 面不新增 `subagent steer|queue|interrupt|cancel|message|stop`、`code`、`image`、`audio`、`todo` noun/subcommand；`main.go` 早入口分流与 usage 不变。
- `review.Table` 各行原样（steer/queue/interrupt/cancel/agent_message/agent_stop/code_* 各 Normal、code_show/listen Readonly、generate_image/tts/todo Normal）；tool_index 的 `agents`/`coding-agent`/`image_gen`/`audio`/`todo` 组原样。

## 边界

- 不把「收口」读成「能迁的都迁」——批 4 已冻的战略边界（写终点在本实例活表/Event 入口侧保模型）覆盖本批全部候选；`--self` 技术可达不是迁移理由，领域真源不移动才是判据。
- 不把 `xbot subagent steer|queue|interrupt|cancel|message|stop --self` 写成「已否决永不做」——它们是「人在终端控制孩子」的潜在新用户面，如开按人侧新功能另行设计（sender/身份判定单算），与模型工具迁移无关；本刀不预留 noun、不写合同。
- 不把 `code_show` 写成「部分离线等价」——record 目录离线可读的近似与 live tail 缺失并存，单工具 noun 不划算；若 coding 族后续整体重估，`xbot code show --self` 是候选形，现在不开。
- 不把 mcp `call` 写端的 `parsed.ReadOnly` 扩成「research 放行 MCP 写」——readonly 是自报标志非能力证明；research 写面只经 `ShellReadOnly` 白名单与 `allowReadOnlyPlatform` 逐 noun 放行，不在本刀扩。
- 不把批 5 写成「剩余工具盘点结束」——desktop/browser/code/connector 的 link.Tools() 消费者与 `link` 包瘦身、Event 上移、mount/snapshot/mount() 合一仍是批 4 后续实现切片，本刀只收口模型工具面。
- Root4USERdocs/OwnerSSE/guard/main/push 日用不动。

## 接受的代价

- `xbot` CLI 面在批 5 后不再增长模型工具 subcommand——至此除 `profile/skills/routine/bot/pin/primary/persona/chat-group/peer-message/bot-fork/experience/memory-recall/transcript/compact/links/manual/secret/subagent(三读)/jobs/restart/http/mcp` 已迁各组外，模型工具面与 CLI 面的边界定形：**runtime CLI = 读投影 + 无 turn 依赖的领域动作（cancel/install/remove/http/restart）；模型工具 = turn 语义、Event 入口、厂商协议与活表写**。后续新工具按此二分入位，不再逐批开决策。
- `subagent` noun 保持三读不写——模型侧 `steer` 等动词与 CLI `subagent list|read|expand` 并存是接受的双入口（同一 noun 不同面），不一致代价已由「机制对称不在字面同族」判据计价。
- research 下 `mcp call` 写保持拒——与 `jobs cancel`/`links install|remove` 同档收紧保留；如某 MCP 工具确需 research 可达，按 agenttype 新类型或显式 grant 另行设计。

## 最窄验收

1. `registry.go` `image_gen`/`audio`/`todo`/`agents` 四行原样；`k.Dispatch` 二分（dispatch+agent_message+agent_stop vs spawn+steer+queue+interrupt+cancel）原样。
2. `code.Link.Tools()` 六件原样；`showTool.Run` 的 `rec.List`/`rec.Get`/`formatTask`/`l.output` 回落原样。
3. `allowReadOnlyPlatform` 无新增分支；`mcp` 分支 `call` 仍只放 `parsed.ReadOnly`；research 下 `xbot subagent steer --self`、`xbot code show --self` 因 noun 不存在天然拒（unknown subcommand/无分支），`xbot mcp call --self` 不带 `--readonly` 仍拒。
4. `continuable_subagents.go:443` 负面清单原样（dispatch/spawn/agent_message/agent_stop/steer/queue/interrupt/cancel/send_message_to_user 不注入孩子）。
5. `main.go` usage 与早入口分流无新 noun；`cliops` 无新 typed handler/transport；`cliProfileGuide` 无新增指引。
6. atlas kernel/ecosystem/security 三处模型工具表与 readonly 表原样——本 ADR 无 atlas 同步面（现状不变）。

## 决策复核备忘

- 批 4「剩余写类全保」的战略边界在批 5 落地为「候选逐族否决」：六控制动词技术可达但领域真源不移动（executor 活表一份也没少）+ sender 无源可判；`code_show` 单工具 noun 收益窄且劈同族；厂商三件与 todo 分别绑登录态/落盘与当轮步进；mcp readonly 写端保持拒。
- sender 判据是六动词否决的独立依据：`k.control` 非 descriptor 分支硬编 `Sender:"owner"`、`agentMessageTool` 硬编 `"bot:"+currentName`——CLI 入口无 turn/身份判调用者，任选一值都改变现语义。
- todo 判据独立于其他五族：`UpdatedStep`/`todoTouched` 是当轮语义，runtime 调用无 turn 归属，保语义须造 prepared ctx 依赖——方向与「为迁而造依赖」相反。

本刀 docs only；产品代码、测试、e2e 零改动。Not run: 产品测试（无代码面）。
