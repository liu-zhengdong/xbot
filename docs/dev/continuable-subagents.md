# 可继续的本机子代理

档位：**L**。2026-11-01 用户确认 Obsidian「设计/子代理对齐DSH-讨论稿」的五项并授权开子代理实现；本文件是本次实现与独立验收契约。本文只记录设计，产品实现、端到端与日用尚未通过。推荐方案为持久身份 + 有限执行。设计作者仅写本文件与新增 ADR，不改产品代码、旧 ADR、guard 或其他历史 dev，不提交。

## 用户批准范围与怎样算对

挂在既有「忙时插话」「动手做事」旅程，以下沿已确认讨论稿，不改变既有旅程：

1. 完成、失败、停止后，向原 id 发消息继续原子对话；第二轮模型能看到第一轮必要上下文，真实产物随反馈更新。
2. 创建可选 `fresh` / `fork`，默认 fresh；fork 只继承父 Bot 上一个已完成轮边界以前的上下文，当前轮任务所需新信息写入 task。fresh 不带父历史。父子 cd 各自独立。
3. 父子双向消息带真实来源，孩子可提前报告发现或问题；每轮结算仍自动回父 Bot 的原 Inbox，忙闲均可接到。进度只留子记录，不设签收账。
4. 单次创建可选择已配置 provider / model / thinking / reasoning_effort，省略继承父绑定；原 Bot 不被换绑。后续继续沿创建时确定的模型身份，使用孩子实际模型窗口与推理配置，记录不含密钥。
5. 从独立「子代理」Tab 查看孩子原文、执行与结果，直接继续或停止；刷新后归属、轮次与旧结果引用仍正确。2026-11-01 用户明确纠正：子代理单独一个 Tab，「任务」Tab 查看 Routine。仅调整入口归属，已确认的查看/继续/停止行为与验收不变。UI 先按原型规矩定稿，不把孩子加入顶层 Bot 列表。
6. 保留本机 **3 运行 + 8 等待**、同仓可写任务排队；idle 不占 slot / repo 资源。已接受的 steer / queue 在约定边界消费，stop 后不被旧排队消息复活。
7. 实例释放或服务重启后，显式新消息可冷恢复；重启只把失联工作标中断，不自动重放旧工具或消息。旧结果指针不被下一轮覆盖。

本批不做嵌套派生、兄弟任意通信、workflow / dev→review→fix 自动接力、类型默认模型字段、辅助小模型自动路由、runner 等价改造、外部编码 Link 改造或全仓清理。research 的 CLI 只读分类、runner 回连原实例仍是既有缺口。

## 源码核实与真正要改的缝

核对基于本机 main（核对期间父任务继续推进 main，本文不锁定别人改动）。已读根 AGENTS、atlas README/subagents/kernel/context/security/providers/ui、ADR 0021/0028/0035/0043、全部 guard、TESTING、相关 dev 与候选。以下是实现前提，非宣称已实现：

| 源码 | 已核实事实 | 本次处理 |
| --- | --- | --- |
| `internal/exec/exec.go` Start / launchLocked / Control | jobs 只放活执行；runLoop 返回后删除；已结束 id 的 Control 报 not running；Local.Prov 优先 req.Model | 保留活表本义，添加本机可继续能力及 resolver hook，不把全部历史留内存 |
| `internal/exec/loop.go` | queue 目前在一个 runLoop 内继续，重置步数但共享墙钟、只在最后结算 | 新 descriptor 路径中每个 queue 是独立轮，按原 admission 重新排队并分别结算 |
| `internal/exec/transcript.go` | 子记录是 bot.Entry JSONL，seq 为物理行号；meta 是可替换状态文件 | 在同一 JSONL 追加有类型控制记录；不造 run 表或旁支对话库 |
| `internal/exec/result.go` | ResultText 固定覆盖 result.txt，bgturn 也调用 | 保留旧函数；可继续路径新增按开轮 seq 的不可覆盖结果文件 |
| `internal/kernel/agents.go` ensureExec / modelExecutor | wrapper 注入父模型 overlay；subagentBase 除派生外可拿主工具；forwardExec 异步 dropScope | 子模型装配按孩子 route；同步终结回调负责 scope；移除本机终态事件的异步 dropScope |
| `internal/kernel/dispatch.go` | typed dispatch 只 brief；forkMessages 当前 assemble 未限制当前轮；fitProvider 使用父 Kernel 的 lines / compactor | 新创建入口接 fresh/fork、截至已完成边界的投影、孩子独立 fit 预算 |
| `internal/kernel/cwd.go`、`operations.go` | scope cwd 只在内存；dropScope 删除；CLI endpoint 随真实执行延寿 | 终结先采样落 descriptor 再释放 scope；恢复绑定当前 runtime，不恢复旧端口 |
| `internal/web/server.go` | panel 扫 meta；旧 control 拿前台 gate；尚无子 transcript 分页 | 独立子代理 Tab；新消息/停止不等前台 gate，读取有界 |

主线保持全工具（0028）。新增模型工具经 kernelTools 注册表与权限表归类（0035），不向 tools[] 额外私插。父消息与结算沿普通 Inbox / 环境流（0021），不建转告专用账。

## 调用方先行

```text
id = dispatch(type="dev", task="按约定修改模块并报告证据", context="fork")
agent_message(id=id, text="复审第二项遗漏，请沿原修改补齐")
agent_message(id=id, text="当前工作结束后补说明", delivery="queue")
agent_stop(id=id)
list_subagents()  # 包含近期已结束且能继续的孩子

other = dispatch(type="research", task="独立查证", provider="configured-name",
                 model="model-id", thinking="on", reasoning_effort="high")
# 孩子只向自己的直接父 Bot 报信，不接收任意 target：
send_message_to_parent(text="发现接口与实现冲突，建议先暂停接线")
```

fresh 保留现有任务隔离，fork 的当前轮不完整调用从不复制；首次无已完成历史时 fork 仍有孩子前缀和 task。显式发信与自动结算是两条真实输入，允许同时存在，不能把显式消息当作结算已送达。

## 两个结构方案与红线

**A：现有 job 做完后常驻 idle。** Start 创建 job 后永不正常 return，ctrl 唤醒；内存保存 tools / provider / 对话。热继续改动少，但历史与 admission 混在一张表，idle 必须额外管理 slot、repo、runtime 退役；跨重启仍需 descriptor 冷恢复，又长出第二条续聊路径。踩中多状态、资源寿命混乱红线，不采用。

**B：持久身份 + 每次有限执行（采用）。** agents/<id> 是身份与原文；jobs/waiting 只管真实运行与入场。消息统一到 Local.Message：运行中入原控制边界，结束后从同一记录重建，再走原 3+8 admission。Kernel 掌握配置解析、工具作用域、模型窗口和 runtime；exec 掌握记录与调度；web 掌握认证和公开投影。无额外 manager / Bot / Session / event bus。

采用 B，并借用 A 的原 ctrl 通道做运行中 steer。接受每轮冷重建与读取子记录的开销，换取 idle 零运行资源、热/冷同一条恢复路径。descriptor 只保存重建所需事实，transcript 保存原始提示、消息、边界；meta 只作最近状态投影，不在三处同步相同账。

## Go 契约、状态归属与文件所有权

以下为要实现的签名与字段形状。`exec.Executor` 原接口保留，runner 不被迫实现本机新能力；`modelExecutor` 不得通过嵌入接口把本机能力隐藏掉。

```go
// internal/exec：新增窄能力；Start/Control/Events/Tasks 沿原 Executor。
type Continuable interface {
    Message(ctx context.Context, id string, c Control) (Task, error)
    SendParent(ctx context.Context, id, text string) (Event, error)
}
// KindSteer / KindQueue 是 Message 输入；KindCancel 由已有 Control 接 stop。
// Message 只接受当前 Bot 的 sa_ id，使用 Local.AgentsDir 定位，无客户端路径参数。

type Descriptor struct {
    Version int `json:"version"` // 本版固定 1，不建设迁移框架
    ID string `json:"id"`
    Bot string `json:"bot"` // 归属诊断，目录由当前 Bot 绑定，不信客户端改归属
    Type string `json:"type"`
    Mode string `json:"mode"` // brief 或 fork，公开 fresh 映射 brief
    CWD string `json:"cwd"`
    Provider string `json:"provider"`
    Model string `json:"model"`
    Thinking string `json:"thinking,omitempty"`
    ReasoningEffort string `json:"reasoning_effort,omitempty"`
    Tools []string `json:"tools"` // 创建时类型白名单快照，不是工具实现/schema
    MaxSteps int `json:"max_steps"`
    TimeoutSec int `json:"timeout_sec"`
}
func ReadDescriptor(dir string) (Descriptor, error)
// StartRequest 新增 Descriptor *Descriptor；nil 保持旧一次性语义。
// Task / Meta 新增 TurnSeq int、ResultPath string；Meta / 目录读侧增加 continuable。
// JSON 一律 turn_seq/result_path，不能让 Task、Meta、HTTP 各用一个轮次名。

// Local 新增；只用于本机，所有持久写仍由 exec 完成。
AgentsDir string
Restore func(context.Context, Descriptor) (StartRequest, error)
ProviderFor func(StartRequest) (provider.Provider, error) // 优先于 Prov
CurrentCWD func(StartRequest) string
ReleaseScope func(StartRequest)

// internal/kernel：有实际策略的领域入口，工具与 HTTP 共用。
type SubagentRequest struct {
    Type string `json:"type"`
    Task string `json:"task"`
    Context string `json:"context,omitempty"` // fresh|fork，默认 fresh
    Provider string `json:"provider,omitempty"`
    Model string `json:"model,omitempty"`
    Thinking string `json:"thinking,omitempty"` // on|off|omit
    ReasoningEffort string `json:"reasoning_effort,omitempty"`
}
func (k *Kernel) StartSubagent(context.Context, SubagentRequest) (exec.Task, error)
func (k *Kernel) MessageSubagent(ctx context.Context, id, text, delivery string) (exec.Task, error)
func (k *Kernel) StopSubagent(ctx context.Context, id string) (exec.Task, error)

// 结构化公开读取；before 排他，未换行尾忽略，默认40/最多200。
func ReadTranscript(dir string, before, limit int) (TranscriptWindow, error)
func ActiveTask(dir string) (Task, bool) // 只读实际active owner快照，Web不复制任务状态
// TranscriptWindow: Entries []SeqEntry、HasMore bool、NextBefore int；SeqEntry嵌原Entry并带Seq。
// Task / Meta: Continuable bool、TurnSeq int、ResultPath string（JSON同下划线名）。
// Event 同样带 TurnSeq，不能由异步消费方读取“最新meta”猜该事件属于哪轮。
// ErrNotFound=>404；ErrNotContinuable/ErrActiveElsewhere=>409；ErrQueueFull=>429；输入非法=>400。
```

`StartSubagent` 做唯一外部请求校验、类型/route 确定、fork 截止投影、cwd/resource 装配；`MessageSubagent` 校验所属孩子与 delivery、恢复当前 runtime 装配并交本机 Message；`StopSubagent` 只请求停止，不伪造已经退出。Kernel 不直接写子 JSONL、descriptor、result 或另管 admission。Local.Message 是持久身份、控制与 admission 的深边界，不能实现成「找不到活 job 就重新 dispatch 一个 id」。

| 编码拥有者 | 独占路径 / 职责 | 与其他块的缝 |
| --- | --- | --- |
| exec agent | `internal/exec/{exec,loop,transcript,result,recover}.go` 及其定向测试；可新增 continuable.go | Descriptor/Continuable/回调；不 import kernel/config/web |
| Kernel agent | `internal/kernel/{agents,dispatch,cwd,operations,registry,fit}.go`、`internal/agenttype/`、`internal/review/`、手册提示及自有测试 | 调 exec 能力；配置解析走既有 config/provider，不扩 provider 协议 |
| web agent | `internal/web/` 子接口/公开投影/子代理详情与少量静态资源、API 说明 | 只调用 Kernel 领域入口和 exec 只读；与当前 trace-explorer 的热文件由主管串行接线 |
| 独立验收方 | `e2e/smoke/continuable_subagents_test.go`、相关 testdata，定向浏览器验收 | 不写实现、不降低验收期望；复用真实二进制启动器 |
| 主管 | 装配冲突、相关 atlas/手册/MILESTONES 收尾、审计与合入 | 本设计作者不改这些文件；正式稿独立审后再接线 |

## 一次轮次、只追加记录与调度骨架

所有原始记录仅追加 `agents/<id>/transcript.jsonl`；兼容 bot.Entry 的嵌入式 exec 记录可加 `kind/turn_seq/delivery/sender`，或复用带 OriginInternal 的结构化内部 Entry。实现只选一种，禁止新增独立 runs.jsonl。物理行号即 seq；控制标记与 turn_end 同样占行，模型投影忽略纯控制行，不从用户正文解析控制命令。

- descriptor.json：原子写的重建事实；CWD 是唯一随执行更新的业务字段，policy/route 沿创建快照。初始 persona、类型 prompt、memory、fork 投影只存初始 transcript，不再复制到 descriptor。
- meta.json：最近状态与 turn_seq/result_path 等展示投影；活状态/排位以原 Local.Tasks 为准，不从旧 meta 逆向填活表。
- 第一条任务/下一轮真实输入的物理 seq 作为 `turn_seq`，在入队前确定，等待取消也有稳定身份；steer 不开轮，queue 到轮边界开启下一轮。
- 输入受理、消费与取消在原串追加 `message_received/message_consumed/message_dropped` 内部记录，引用同一输入 seq；真实输入正文只存一次。受理成功须已追加，写失败/满返回错误；只承诺可审计，不承诺崩溃后自动消费。
- 每轮结束追加 `turn_end`，携带状态、turn_seq、result_path、未消费输入去向；不改旧行。新轮投影保留原必要上下文与结果，不再次复制初始前缀和全部历史进 JSONL。
- stop 或恢复发现悬空 tool_call 时由 exec 就地追加占位 tool_result，按调用 id 一一配平；不 import kernel、不重跑工具。请求投影仍按既有工具配对规则组织，原物理行不重排。

**准确的队列语义：** 每个 child 的已接受待消费消息沿用原 ctrl 容量 **16**（包括 closing 转下一轮的 pending），满则明确拒绝；不另建无界待轮切片。全 executor admission 仍 **8** 个等待身份，一个 child 最多一个当前/待入场激活，不能因多个 queue 占多份身份槽。

1. 普通活动轮接 steer：持原控制口，在下一 step 前追加进模型投影；不打断正在跑的工具。queue 只记后续输入，在当前轮自然结算后开下一轮。
2. 最终回复、失败、取消都在原 admission 锁下进入 finishing；此后 Message 不再投旧 ctrl，改记同一 job 有界 pending。Message 与终结释放原子裁决；返回成功的消息要么本轮消费，要么在下一轮 pending 中，不能掉进死通道。
3. finishing 时先配平悬空调用；在无 executor 锁下同步 `CurrentCWD(req)`，exec 保存 descriptor；写该轮不可覆盖 result 与 turn_end，再同步 `ReleaseScope(req)`。CWD 保存失败按失败结算，不能声称恢复了未保存目录。
4. 此后在原 admission 锁内移除旧 job、释放 slot/repo；有后续输入时按同一 admission 规则重排，原先等着的可运行任务优先。不能原地递归 runLoop 垄断 slot。第一次受理 queue 或 finishing 后的新消息时，在原 admission 容量中预留该身份下一轮的一个等待位置；计数由原 waiting 加活 job 中已有后续输入的身份数推导，不建另一张队列表。合计上限8，满即受理前返回 ErrQueueFull。后续同身份消息仍受16条上限。交接把预留位置转换成同一 waiting 项；不会出现第9个等待身份，也不丢先前成功受理消息。
5. 每轮单独 emit 稳定 terminal，包含本轮 immutable 指针；队列里的下一轮另算预算。等待不计时，BLOCKED 仍计当前墙钟；自然完成后已明确 queue 的后续可自动入场。失败只丢弃失败裁决前已受理的旧轮未消费输入；finishing 后才受理的新消息属于下一轮，不能被旧失败丢弃。stop 按锁先后丢弃此前输入，之后新消息是显式继续；重启不自动消费任何旧待做输入。
6. 无活任务及 admission 队列才在无锁条件调用原 OnIdle。旧终态事件不能再异步 dropScope，否则会删掉同 id 的新轮 scope。

所有 Start / 首次 queue / closing-message 都检查同一等待容量；预留仅由现有 job.pending 是否非空推导，取消或下一轮实际启动后释放。不新增常驻执行、独立调度器或无界内存。

**Stop：** cancel running/blocked；queued 则取消入场。丢弃 stop 之前已受理但未消费的 steer/queue，并在原串标记，之后不会自动复活；stop 之后并发的新消息按原锁顺序是显式继续，可重新入场。已经结束的孩子 stop 幂等。停止 context 不能杀死不响应取消的工具时，状态保持 stopping/running 直到真正退出；已转后台的工具 Job 沿既有政策继续，不因停子代理伪报工具已撤销。

## 多轮结果、事件 id 与父子消息

```text
agents/<id>/descriptor.json
agents/<id>/meta.json
agents/<id>/transcript.jsonl
agents/<id>/results/<turn_seq>.txt
```

结果文件只创建一次、不覆盖；旧 result.txt 只保留给旧一次性任务，不把它更新成 latest 别名。结果 preview 继续最多 256 字节、report 4096 字节，正文与副作用摘要按原政策脱敏。meta.result_path 指最新轮，旧事件永远指自己的轮。

| 输入 / 事件 | 稳定身份 | 路径 |
| --- | --- | --- |
| 每轮 done / failed / cancelled / interrupted | `subagent:<id>:turn:<turn_seq>:end` | Local.Events → forwardExec → 原 Inbox → 唤醒 |
| 主动孩子给父的 message | `subagent:<id>:message:<记录seq>` | exec 先追加孩子记录，分配 seq，再 Events → 同一 Inbox |
| blocked | `subagent:<id>:turn:<turn_seq>:blocked:<记录seq>` | 原阻塞回报；不冒充结算 |
| progress | 沿原临时事件 | 只留孩子记录，主线不收 |

`SendParent(ctx,id,text)` 由 exec 单写孩子记录并返回事件；Kernel 的 scoped 工具只提供当前 id 与文字，无 caller-provided parent/target/sender。只能在该孩子真实执行 context 调用；错误与输出脱敏。返回的 Event 由 Local 自己 emit 一次，调用方不得再次 Post。显式 message 用 `kind=message`，terminal 保留 done/failed 等既有类别。父端一律不可信来源，不能伪装主人授权。

主 Bot 的 `agent_message` 只能寻址自己的孩子。孩子基础工具集显式移除 dispatch/spawn、agent_message/agent_stop/list_subagents、旧 steer/queue/interrupt/cancel，以及 `send_message_to_user`（避免 general 直接给用户挂泡）；只挂 scoped `send_message_to_parent`。research/dev 内置白名单补此名，general 的 `*` 也不能越过基础集边界。既有 message_bot 是不同产品通路，保持旧政策，但不能用它冒充父子信封或访问兄弟子代理记录。

## 冷恢复、模型与上下文

Local 由 Kernel 绑定 `AgentsDir = Bot.AgentsDir()`；Message 校验 id 后在这里读取 descriptor，不信请求的 Dir/JobDir/父名。descriptor 缺失的旧历史只读可查，continuable=false，不猜启动参数迁移；缺失供应商、凭据、模型或类型事实损坏时明确失败并保留旧记录。

**真实执行 owner：** serve.Pass 与 web 可有同 Bot 的不同 Kernel/Local。exec 内只保留 `map[canonical AgentDir]*Local` 的 accepted running/queued 指针路由，claim/release 与原 admission 交接原子裁决，不存任务状态或第二张任务表。Message/Stop 先路由实际 active owner；旧 Kernel 的scope与CLI endpoint随该owner继续存活。idle移除路由，后续由当前入口的Kernel冷装配；不能因“本Local没job”重复执行另一个Local的孩子。跨进程 meta 的PID/ProcStart可证实活着时返回 ErrActiveElsewhere（409），不猜恢复、不发送进程信号。

`Restore(ctx,descriptor)` 只装配新的 StartRequest（原 id/目录/白名单快照/墙钟/步数/当前 cwd/resource）与当前作用域；provider resolver 用现有 config.Resolve/ModelSelector/OAuth/vault，descriptor 中绝不保存 Endpoint、APIKey、bearer、环境值、listener URL 或 provider 对象。ProviderFor 优先于 Local.Prov；Prov 仅作注入 fake 和 descriptor=nil 的旧 fallback。不能只填写 req.Model，因为现行 Local.Prov 会覆盖它。

单次解析规则：provider/model 都省略时固定父当前绑定；只 model 时沿父 provider；只 provider 时取其已配置第一模型；都给则该组合须已登记且可解析。thinking/effort 显式值优先该模型已有设置，缺省沿所选模型设置；只有完全继承父 route 时继承父有效设置。统一调用既有协议校验，非法 effort 不悄悄丢掉，也不新增通用 effort 枚举。创建时固定有效身份与推理值，继续时沿此身份解析当前目录能力、窗口和凭据；不改主 Bot binding、不增加 agenttype.model 第二套优先级。

子模型必须独立作用于：模型 overlay、vision、hosted/local search 互斥、thinking/effort、窗口 T/H、摘要器与工具定义预算。`fitProvider{k: parent}` 不再借父 lines；在原 fit 缝给每个 turnFit 传入子模型的 budget / summarizer / scale，主线默认仍走旧路径，不复制带 mutex 的 Kernel，也不临时改 `k.Budget/k.Provider/k.Compactor`。跨轮冷读历史先按子 T/H 生成有界请求投影，过大先用所选子模型摘要；不能压到 H 内明确失败，不把超长 fork 直接发出去。摘要只追加内部记录（含覆盖 child seq），原文不改；下一轮复用已记摘要，不每次重新摘要全本。子原文回读用 expand_subagent/文件，不错误指向主 Bot expand。

fork 截止以当前父轮开轮 `turnSeqOf(ctx)-1`，不是当前 Count 或「最近一句 assistant」。调用 assemble 的有界投影：只读截止之前未被摘要覆盖的原文和完全结束在截止之前的 summary block，排除当前轮所有工具调用、插话、结果；使用相同工具配对规则。没活轮上下文的受理入口取已完成边界而非猜 Count；本批仅模型 dispatch 创建，HTTP 不另建 fork 创建入口。父固定提示中 persona/内核规则/记忆可读，子执行规则与类型白名单仍以孩子为准；子 overlay 使用所选子模型，不重复继承父模型补丁。现行 initialMessages 的 fork 分支直接绕过 Persona，必须改成明确子前缀（子规则/类型/route overlay）加已完成父 history 再加 task，不能直接沿用旧分支漏掉子规则。

运行时释放顺序见上文同步终结回调。新轮用 descriptor.CWD 建同 id 的 scoped 工具；目录失效按既有 allowedRoots/workspace 回退并写明，resource 随实际入场 cwd 重新计算。正常结束保存最终 cwd；崩溃只保留最近成功落盘 cwd，不承诺捕获进程最后一次未结算 cd。恢复后的 Shell 通过当前实际 Kernel 的 bindShellRuntime 取得 XBOT_BIN/ROOT/ENDPOINT/BOT，旧已退役 listener 从不复用；入口仍由原 Local 活集合/OnIdle 与服务 context 控寿命。

重启不自动消费已受理但未消费的消息、不重放工具。原 recover 对可证明同 pid/proc_start 存活的任务继续观察；否则在同串追加 interrupted/end、丢弃未消费输入，稳定 end id 去重。`internal/kernel/task_recover.go` 的旧 `ev_<id>_interrupted` 对 descriptor 路径替换为同一 `subagent:<id>:turn:<turn_seq>:end`，否则第二次重启会被Inbox已Ack集合吞掉。持久正文已有端到端原路径，原 Inbox 运输语义继续沿用；不增加 acknowledged/notified 第二份回执账，也不承诺任意崩溃点跨文件 exactly-once。

## HTTP / JSON 与模型工具清单

所有路由沿既有登录/owner 校验与同源策略；跨 Bot 或非所属 id 统一 not found，不泄漏存在性。只读响应经公开投影剥 reasoning、vendor_raw 并脱敏。请求 text trim 后 1..16384 bytes；控制 body 最多 64 KiB。错误沿现有 `{error:"..."}`。

```http
POST /api/bots/{name}/agents/{id}/messages
{"text":"请沿原修改补第二项","delivery":"steer"}
200 {"id":"sa_…","status":"running","turn_seq":7}
# 排队：200 {"id":"sa_…","status":"queued","position":2,"turn_seq":19}
# delivery 省略=steer，仅 steer|queue；只表示已受理，不等待模型。

POST /api/bots/{name}/agents/{id}/stop
{}
200 {"id":"sa_…","status":"stopping"}
# queued 已取消返回 cancelled；已终结幂等返回实际终态。

GET /api/bots/{name}/agents/{id}/transcript?before=40&limit=40
200 {"id":"sa_…","entries":[{"seq":39,"time":"…","message":{"role":"assistant","content":[]}}],
     "has_more":true,"next_before":39,"turn_seq":7,"status":"done","continuable":true}
```

transcript 默认尾窗40、最大200条、响应最多700 KiB；before 排他，entries 正序，next_before 为本页最小 seq（无更早数据时省略）。超大单行给公开预览及 truncated 标记，seq 仍连续可翻；不默认全本读取。字段 status 以真实 admission 覆盖磁盘最近状态；公开 entries 保留必要内部轮标记，不能把它们渲染成主人气泡。

已有 `GET /api/bots/{name}` 的 agents 项增加 `continuable/turn_seq/result_path`，原 task/status/steps/notable 等保留；`list_subagents` 复用相同只读目录事实，返回最近40（最多100）个直接孩子，包含完成记录，不调用 Tasks 充当历史列表。任务原文与结果按需获取，不把完整历史塞 panel。可继续资格来自 descriptor 可读，不因 status=done 就隐藏。

新 messages/stop 只操作 executor 与子作用域，不拿主线 gate/turnMu，不 `UseBot` 覆写正在跑的对象；s.kernel 的创建/缓存沿原装配锁，不拿锁等待模型。旧 steer/queue/interrupt/cancel HTTP 路由作为轻兼容接统一入口（interrupt 保留旧控制语义），不保留第二套任务实现。

| 模型工具 | 参数 | 注册 / 权限 |
| --- | --- | --- |
| dispatch | type/task/context/provider/model/thinking/reasoning_effort | 原 agents 组，Normal |
| agent_message | id/text/delivery? | agents 组，Normal |
| agent_stop | id | agents 组，Normal |
| list_subagents | limit? | agents 组，Readonly |
| send_message_to_parent | text，无 target | 仅 scoped 子集，Normal；父主线不挂 |
| read_subagent / expand_subagent | 沿旧 id / from / to | Readonly，能读多轮及轮次结果指针 |

主模型只暴露一套常用名字；typed 模式下不再声明旧 steer/queue/interrupt/cancel 与 spawn。旧 Dispatch=false/spawn 的一次性调用与 descriptor=nil 继续使用旧限制和结果，开发期只轻兼容：不为旧目录补造 descriptor，不引入兼容开关，不顺手删除 bg_/code_ 控制；后续若完整退役旧模式另行删除失效测试。模型工具名单与 registry、review 守门检查同步。

UI 入口为独立「子代理」Tab；「任务」Tab 查看 Routine。子代理页复用现有详情组件和已批准的 trace-explorer 事件投影：选择键为 `(Bot,id)`，事件键为 `(id,seq)`，切 Bot/孩子丢弃迟到回包；轮询保持输入草稿、选择、滚动与详情页签。详情提供继续输入、steer/queue 与停止；不混入主聊天送达投影，不重复造轨迹页。已有 trace-explorer dev 明确未含子代理步骤，本次只补孩子数据源与同一详情入口。视觉由专门 UI 设计方给原型并交用户定稿；本文仅定数据/行为。

## 对照回顾：直接相关正文与范围结论

已全量筛候选目录 **165** 份标题及检索到的 priority/status/类型/依赖头（含无 metadata 文件）；仅直接相关正文阅读全文，附录逐项留筛选结论。不是声称读完全部165份正文。依赖的列表正文只对直接相关项展开，无关条目不修改状态、不借本批排期。

| 材料 | 撑得住 / 冲突 / 复用及真实范围 |
| --- | --- |
| 候选「子代理类型」全文 | 类型已落地，旧前台限死/无卡片等描述过时，以0028/源码为准；本批只补续聊、单次选模，类型默认模型/媒体/删fork遗留不做 |
| 候选「子代理工作流」全文 | 复用同id基础；旧建议idle持repo锁、review/max_rounds链不采用，本次每轮正常自动回父，不吞中间结算 |
| 候选「委派当人交流」全文 | 已完成的协作者提示保留，补真实父通信；不强制孩子直接给主人说话 |
| 候选「轨迹显示子代理步骤」全文 | 本次真正覆盖：孩子JSONL接口 + 子代理详情查看步骤；与trace-explorer合数据入口，不额外抄一份轨迹 |
| 候选「前后台思考强度」全文 | 覆盖单次子模型effort选择；「前台不思考」、类型上限/默认/对比真评测均不纳入 |
| 候选「便宜小模型做辅助任务」全文 | 独立：不实现自动辅助路由，仅显式选模 |
| 候选「换小上下文模型前先压缩」全文 | 正文旧“尚未实现”与当前binding.go/atlas不一致；沿现有T/H精神做孩子fork/恢复fit，不借本批改主Bot换绑 |
| 候选「子代理current_step不露工具名」全文 | UI详情不得新增裸工具名预览；旧live current_step回退工具名缺口独立保留，不以新详情宣称已全修 |
| atlas各节点 / 源码 | 类型白名单、3+8、资源队列、Inbox、cwd、runtime可复用；atlas/subagents的旧16步/10分钟和“fork当前上下文”需实现后定向校正 |
| ADR0021 / env-prompt-frame / env-entity stub | 只追加普通消息，状态从原执行器采样，不加签收；现有time Register可用，Proposed环境修订不当Accepted，不迁其余字段 |
| ADR0028 | 主Agent全工具，不恢复前后台闸；子类型约束不等于前台限制 |
| ADR0035 / tool-registry-progressive-disclose | 一张注册表、代理门与权限真名；子集显式通信边界，不新增第二套工具装配 |
| ADR0043 / cli-platform-entry全文 | 复用实际runtime、scope与OnIdle寿命；冷恢复不保留旧endpoint，不宣布research/runner已通 |
| dev/multi-provider全文 | 现状已超过其旧前提；其明确排除子代理选模，本批在既有config/provider上补子route，协议/OAuth不重写 |
| dev/trace-explorer全文 | 该稿明确只改主Bot轨迹，不含孩子；本次补数据源/继续停止，热前端文件由主管整合 |
| guard 六条全文 | 网页回复、插话不取消不相关长活、真实产物、结果转告、Bot信任与原有协作旅程保持；本批不改定义 |
| guard 长程全文 | 主线固定前缀/压缩/原文找回保持；子fit独立预算，若改共享fit触及原路径，选受影响长程一次 |
| guard 界面全文 | 原型先定稿、无任务卡进聊天、原始id仅详情可复制、轮询保选区；不增加审批UI |
| docs/TESTING全文 | 本次纯设计只查diff/链接，不跑产品测试；实现按影响一次，通过即停 |

相关历史 dev 的标题/范围已筛，不逐份清理。当前工作区 `cli-message-bot`、env stub、.playwright-mcp 和 review目录有其他工作改动，本任务不碰。

## Tests 路径、独立验收与最窄顺序

实现前由独立方在 `e2e/smoke/continuable_subagents_test.go` 写真实 `xbot serve` + HTTP/SSE + scripted provider 冒烟（不调用真模型），预期覆盖：第一轮实际文件→terminal→同id第二轮读前次必要事实并改文件→旧/新结果都可读；释放实例及重启后显式继续不重放副作用；主线忙时child message与两次terminal均只到一次；fresh/fork边界；明确选模路由与小窗口拟合；子cwd/runtime自操作；查看/继续/停止真实HTTP与浏览器入口。

exec 自有定向测试预计 `internal/exec/continuable_test.go`，补 admission_test / result_test / recover：终结窗口送消息不丢、3+8与同repo、queue独立预算和逐轮事件、16条满拒绝、stop丢旧消息/显式新消息重开、不可覆盖结果、悬空tool配平、缺descriptor不猜恢复。Kernel 自有 `internal/kernel/continuable_subagents_test.go` 覆盖route/resolver、fork截点、子fit不改父预算、research/dev父通信和general排除；review/registry守门沿既有测试更新有效断言。Web 接口和浏览器用例在 `internal/web/continuable_subagents*_test.go`，定向覆盖owner、分页上限、公开投影、不卡前台gate、草稿/选区和迟到回包归属。

先打通 fresh→done→同id反馈→真实文件变化，再补 fork/路由/消息/停止/重启与UI。实现块可按上表并行，接口接线和共享热文件由主管串行；独立验收不修改实现者同名测试，失败按缺陷归因修复。仅因设计为L不跑全仓或重复三遍；纯设计不跑Go/浏览器/冒烟/性能。

## 还没做的

- 正式稿独立结构审计及其修订；新 ADR 记录决定，旧 Accepted 不改。
- 上述exec/Kernel/web实现、独立scripted端到端及有效回归；所有计划测试当前未运行。
- UI原型定稿、代表场景截图与独立视觉评审。
- 实现后同步atlas子代理/内核/上下文/供应商/UI、手册/API与MILESTONES；不清理无关历史dev。
- 最终端到端与用户日用；当前仅设计交付，未合入、未宣称功能完成。

## 附录：候选标题 / metadata 逐条筛选

此表是2026-11-01本次只读筛选快照，保留磁盘原status（含历史非标准值），不是重排。`—` 表示本次检索未见该字段；依赖头有列表值的直接相关项已在正文全文核对。结论“边界复用”仅指既有能力不得退化，不扩大本次实现。

| # | 候选标题 | priority | status（原值） | 类型 | 依赖头 | 本次筛选 |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | [ADR 主题索引与定期合并](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/ADR 主题索引与定期合并.md>) | P3 | 候选 | 其它 | — | 标题/metadata筛出；不纳入本批 |
| 2 | [Agent Todo 工具](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Agent Todo 工具.md>) | P2 | 已完成 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 3 | [Bot Owner 可见可配](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot Owner 可见可配.md>) | P2 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 4 | [Bot 互发消息上限提示](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot 互发消息上限提示.md>) | P3 | 候选 | 设备/网关 | — | 标题/metadata筛出；不纳入本批 |
| 5 | [Bot 人设由哪些部分组成](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot 人设由哪些部分组成.md>) | P1 | 已完成 | 上下文 | — | 标题/metadata筛出；不纳入本批 |
| 6 | [Bot 协作地图](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot 协作地图.md>) | P3 | 候选 | 设备/网关 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 7 | [Bot 生成和更新页面](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot 生成和更新页面.md>) | P3 | 候选 | UI | — | 标题/metadata筛出；不纳入本批 |
| 8 | [Bot出图到聊天](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot出图到聊天.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 9 | [Bot控机-MacWin-a11y](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot控机-MacWin-a11y.md>) | P2 | 已排期 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 10 | [Bot自写技能与笔记余量](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot自写技能与笔记余量.md>) | P2 | 候选 | 其它 | [] | 标题/metadata筛出；不纳入本批 |
| 11 | [Bot自改头像](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot自改头像.md>) | P1 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 12 | [Bot自装技能通路](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Bot自装技能通路.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 13 | [C1 非阻塞打磨](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/C1 非阻塞打磨.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 14 | [MCP schema 被拒时降级](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/MCP schema 被拒时降级.md>) | P2 | 已完成 | 协议/Provider | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 15 | [OpenCode-Go供应商](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/OpenCode-Go供应商.md>) | P2 | 已实现待验收 | 协议/Provider | [] | 标题/metadata筛出；不纳入本批 |
| 16 | [Playwright连接器无工具](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Playwright连接器无工具.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 17 | [Shell 按解释器拆工具](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Shell 按解释器拆工具.md>) | P2 | 已完成 | Jobs/执行 | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 18 | [StablePrefix-分段系统提示](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/StablePrefix-分段系统提示.md>) | P1 | 可手测 | 上下文 | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 19 | [Todo与Tasks-主动性引擎](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/Todo与Tasks-主动性引擎.md>) | P2 | 候选 | Jobs/执行 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 20 | [live条裸英文bash与旧shell_闪](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/live条裸英文bash与旧shell_闪.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 21 | [provider请求debug落盘](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/provider请求debug落盘.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 22 | [trace list 与 replay 序号口径](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/trace list 与 replay 序号口径.md>) | P3 | 候选 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 23 | [xAI-OAuth与hosted-tools](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/xAI-OAuth与hosted-tools.md>) | P1 | 可手测 | 协议/Provider | [] | 标题/metadata筛出；不纳入本批 |
| 24 | [xbot chat 回放最近几轮](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/xbot chat 回放最近几轮.md>) | P2 | 候选 | 其它 | — | 标题/metadata筛出；不纳入本批 |
| 25 | [下一版提示词与配套用例](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/下一版提示词与配套用例.md>) | P2 | 已完成 | 上下文 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 26 | [不同人之间 Bot 互发消息](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/不同人之间 Bot 互发消息.md>) | P3 | 候选 | 设备/网关 | — | 标题/metadata筛出；不纳入本批 |
| 27 | [主动唤醒 webhook 冒烟用例](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/主动唤醒 webhook 冒烟用例.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 28 | [主动性用例素材](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/主动性用例素材.md>) | P2 | 候选 | 评测 | — | 标题/metadata筛出；不纳入本批 |
| 29 | [云上账号壳切开](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/云上账号壳切开.md>) | P1 | 已完成 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 30 | [人设卡-AstrBot内置集](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/人设卡-AstrBot内置集.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 31 | [人设卡](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/人设卡.md>) | P2 | 已完成 | 上下文 | [] | 标题/metadata筛出；不纳入本批 |
| 32 | [人设字段标识文案](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/人设字段标识文案.md>) | P3 | 已完成 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 33 | [企业控制面 UI](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/企业控制面 UI.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 34 | [企业自部署：私有连接器目录](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/企业自部署：私有连接器目录.md>) | P3 | 候选 | 协议/Provider | — | 标题/metadata筛出；不纳入本批 |
| 35 | [会话折叠](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/会话折叠.md>) | P2 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 36 | [使用须注册账号](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/使用须注册账号.md>) | P2 | 已完成 | 设备/网关 | [] | 标题/metadata筛出；不纳入本批 |
| 37 | [例程与网关可暂停恢复](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/例程与网关可暂停恢复.md>) | P2 | 已排期 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 38 | [例程免模型前置检查](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/例程免模型前置检查.md>) | P2 | 已完成 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 39 | [例程可挂技能](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/例程可挂技能.md>) | P2 | 后端已合·UI待定稿 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 40 | [例程读 Bot 收件箱](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/例程读 Bot 收件箱.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 41 | [例程账号级与单任务限流](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/例程账号级与单任务限流.md>) | P3 | 候选 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 42 | [供应商唯一实体](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/供应商唯一实体.md>) | P2 | 已完成 | 架构 | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 43 | [供应商展示名统一](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/供应商展示名统一.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 44 | [便宜小模型做辅助任务](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/便宜小模型做辅助任务.md>) | P3 | 候选 | 协议/Provider | — | 全文对照，真实覆盖/排除见上表 |
| 45 | [全局稳定性（7×24 类 Pi Duration）](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/全局稳定性（7×24 类 Pi Duration）.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 46 | [公网UI单源-跟设备tip](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/公网UI单源-跟设备tip.md>) | P1 | 已完成 | 设备/网关 | [] | 标题/metadata筛出；不纳入本批 |
| 47 | [公网UI单源余量](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/公网UI单源余量.md>) | P3 | 候选 | 设备/网关 | [] | 标题/metadata筛出；不纳入本批 |
| 48 | [内核机制自述](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/内核机制自述.md>) | P1 | 已完成 | 上下文 | — | 标题/metadata筛出；不纳入本批 |
| 49 | [内核硬规矩中文](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/内核硬规矩中文.md>) | P1 | 已完成 | 上下文 | [] | 标题/metadata筛出；不纳入本批 |
| 50 | [创建 Bot 先问用途](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/创建 Bot 先问用途.md>) | P1 | 已完成 | 上下文 | — | 标题/metadata筛出；不纳入本批 |
| 51 | [前台说话要对上状态和证据](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/前台说话要对上状态和证据.md>) | P1 | 已完成 | 上下文 | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 52 | [前后台思考强度](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/前后台思考强度.md>) | P3 | 候选 | Jobs/执行 | — | 全文对照，真实覆盖/排除见上表 |
| 53 | [前端同源-DEV_STATIC-pipe](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/前端同源-DEV_STATIC-pipe.md>) | P1 | 已完成 | 设备/网关 | [] | 标题/metadata筛出；不纳入本批 |
| 54 | [协调者模式](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/协调者模式.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 55 | [厂商hosted生图](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/厂商hosted生图.md>) | P1 | 已完成 | 协议/Provider | [] | 标题/metadata筛出；不纳入本批 |
| 56 | [历史生图气泡谎称清理](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/历史生图气泡谎称清理.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 57 | [只给你用默认开关](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/只给你用默认开关.md>) | P2 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 58 | [只读工具并发执行](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/只读工具并发执行.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 59 | [可点击文字](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/可点击文字.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 60 | [合伙人角色定位](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/合伙人角色定位.md>) | P1 | 已完成 | 上下文 | — | 标题/metadata筛出；不纳入本批 |
| 61 | [四维测评成绩单](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/四维测评成绩单.md>) | P2 | 已完成 | 评测 | — | 标题/metadata筛出；不纳入本批 |
| 62 | [多设备本机卡下还没有设备](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/多设备本机卡下还没有设备.md>) | P3 | 已完成 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 63 | [多设备标识与账号配对](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/多设备标识与账号配对.md>) | P1 | 已完成 | 设备/网关 | [] | 标题/metadata筛出；不纳入本批 |
| 64 | [大肥鱼-DSH插件体感五条](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/大肥鱼-DSH插件体感五条.md>) | P3 | 候选 | 产品/体验 | [] | 标题/metadata筛出；不纳入本批 |
| 65 | [委派当人交流](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/委派当人交流.md>) | P1 | 已完成 | 上下文 | [内核硬规矩中文] | 全文对照，真实覆盖/排除见上表 |
| 66 | [子代理current_step不露工具名](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/子代理current_step不露工具名.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 全文对照，真实覆盖/排除见上表 |
| 67 | [子代理工作流](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/子代理工作流.md>) | P3 | 候选 | Jobs/执行 | — | 全文对照，真实覆盖/排除见上表 |
| 68 | [子代理类型](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/子代理类型.md>) | P2 | 候选 | Jobs/执行 | 列表，直接相关项已全文核对 | 全文对照，真实覆盖/排除见上表 |
| 69 | [安全对抗评测](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/安全对抗评测.md>) | P4 | 候选 | 评测 | — | 标题/metadata筛出；不纳入本批 |
| 70 | [官方薄入口-公网登录页](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/官方薄入口-公网登录页.md>) | P1 | 已完成 | 设备/网关 | [] | 标题/metadata筛出；不纳入本批 |
| 71 | [定时熵减-目录整理](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/定时熵减-目录整理.md>) | P2 | 候选 | 记忆 | [] | 标题/metadata筛出；不纳入本批 |
| 72 | [对话区统一插入系统提示](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/对话区统一插入系统提示.md>) | P2 | 候选 | 交互 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 73 | [小模型裁剪工具返回](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/小模型裁剪工具返回.md>) | P1 | 待议 | 上下文/工具返回 | [] | 标题/metadata筛出；不纳入本批 |
| 74 | [展开不见description目的](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/展开不见description目的.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 75 | [工具渐进披露](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/工具渐进披露.md>) | P1 | 已完成 | Agent/工具 | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 76 | [工具调用硬预算](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/工具调用硬预算.md>) | P3 | 候选 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 77 | [常驻终端 PTY](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/常驻终端 PTY.md>) | P3 | 候选 | Jobs/执行 | — | 标题/metadata筛出；不纳入本批 |
| 78 | [干完提议写成技能](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/干完提议写成技能.md>) | P2 | 已完成 | 其它 | [] | 标题/metadata筛出；不纳入本批 |
| 79 | [建议下一句](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/建议下一句.md>) | P4 | 候选 | UI | — | 标题/metadata筛出；不纳入本批 |
| 80 | [开跑侧栏先闪在跑](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/开跑侧栏先闪在跑.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 81 | [思考回传与启用](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/思考回传与启用.md>) | P1 | 可手测 | — | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 82 | [技能列表主名](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/技能列表主名.md>) | P2 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 83 | [技能多源导入](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/技能多源导入.md>) | P2 | 已完成 | 交互 | — | 标题/metadata筛出；不纳入本批 |
| 84 | [技能库去掉主人路径段](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/技能库去掉主人路径段.md>) | P2 | 已完成 | 其它 | [] | 标题/metadata筛出；不纳入本批 |
| 85 | [技能设置-自装五条热修](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/技能设置-自装五条热修.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 86 | [技能设置管理](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/技能设置管理.md>) | P2 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 87 | [换头像风格与多一套主题](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/换头像风格与多一套主题.md>) | P2 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 88 | [换小上下文模型前先压缩](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/换小上下文模型前先压缩.md>) | P1 | 可手测 | 上下文 | — | 全文对照，真实覆盖/排除见上表 |
| 89 | [推翻前台后台-删限制](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/推翻前台后台-删限制.md>) | P0 | 可手测 | — | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 90 | [提醒的不可伪造边界](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/提醒的不可伪造边界.md>) | P2 | 候选 | 其它 | — | 标题/metadata筛出；不纳入本批 |
| 91 | [搜索后端走 link 协议](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/搜索后端走 link 协议.md>) | P3 | 候选 | 协议/Provider | — | 标题/metadata筛出；不纳入本批 |
| 92 | [收敛评测与消融](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/收敛评测与消融.md>) | P3 | 候选 | 评测 | — | 标题/metadata筛出；不纳入本批 |
| 93 | [改名后面当面刷新](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/改名后面当面刷新.md>) | P2 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 94 | [改名打挂工具槽](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/改名打挂工具槽.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 95 | [新建复刻Bot仍写标识](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/新建复刻Bot仍写标识.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 96 | [无title旧技能仍见slug](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/无title旧技能仍见slug.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 97 | [旧工具结果占位清理](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/旧工具结果占位清理.md>) | P1 | 可手测 | 上下文 | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 98 | [本机Obsidian笔记库](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/本机Obsidian笔记库.md>) | P2 | 候选 | 连接/知识 | [] | 标题/metadata筛出；不纳入本批 |
| 99 | [本机卡默认长hostname](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/本机卡默认长hostname.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 100 | [模型上下文窗口跟真值走](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/模型上下文窗口跟真值走.md>) | P2 | 已完成 | 协议/Provider | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 101 | [正聊先回人-system-reminder](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/正聊先回人-system-reminder.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 102 | [每轮轻量自省沉淀记忆](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/每轮轻量自省沉淀记忆.md>) | P2 | 已落地 | 记忆 | — | 标题/metadata筛出；不纳入本批 |
| 103 | [气泡Markdown排版](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/气泡Markdown排版.md>) | P1 | 可手测 | — | — | 标题/metadata筛出；不纳入本批 |
| 104 | [活气泡分段对齐transcript](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/活气泡分段对齐transcript.md>) | P1 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 105 | [测评打分口径](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/测评打分口径.md>) | P3 | 候选 | 评测 | — | 标题/metadata筛出；不纳入本批 |
| 106 | [混合主动权共设计](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/混合主动权共设计.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 107 | [环境实体自注册更新](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/环境实体自注册更新.md>) | P1 | 候选 | 上下文 | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 108 | [环境快照非阻塞打磨](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/环境快照非阻塞打磨.md>) | P3 | 候选 | 上下文 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 109 | [环境更新只发当前值](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/环境更新只发当前值.md>) | P1 | 可手测 | 上下文 | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 110 | [生图历史泡自动挂旧话](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图历史泡自动挂旧话.md>) | P3 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 111 | [生图头像热修余量](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图头像热修余量.md>) | P2 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 112 | [生图对齐搜索-本地代理兜底](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图对齐搜索-本地代理兜底.md>) | P2 | 已完成 | 协议/Provider | [] | 标题/metadata筛出；不纳入本批 |
| 113 | [生图工作区可发现](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图工作区可发现.md>) | P2 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 114 | [生图挂图话术偏工程](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图挂图话术偏工程.md>) | P3 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 115 | [生图挂气泡热修](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图挂气泡热修.md>) | P1 | 已完成 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 116 | [生图状态主题英文截断](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图状态主题英文截断.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 117 | [生图过程条查看generated未现](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/生图过程条查看generated未现.md>) | P3 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 118 | [登录顶无官方入口胶囊](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/登录顶无官方入口胶囊.md>) | P3 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 119 | [真 DeepSeek 改前改后测评](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/真 DeepSeek 改前改后测评.md>) | P2 | 候选 | 评测 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 120 | [空闲压缩](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/空闲压缩.md>) | P1 | 可手测 | 上下文 | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 121 | [终端 PATH 上的 xbot](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/终端 PATH 上的 xbot.md>) | P3 | 候选 | Jobs/执行 | [] | 标题/metadata筛出；不纳入本批 |
| 122 | [网关局域网直连](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/网关局域网直连.md>) | P3 | 已排期 | 设备/网关 | — | 标题/metadata筛出；不纳入本批 |
| 123 | [聊天主列增量刷新](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天主列增量刷新.md>) | — | — | — | — | 标题/metadata筛出；不纳入本批 |
| 124 | [聊天主列窗口与历史搜索](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天主列窗口与历史搜索.md>) | — | — | — | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 125 | [聊天仅工具产物可见](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天仅工具产物可见.md>) | P0 | 已完成 | 产品/内核+UI | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 126 | [聊天图片查看器](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天图片查看器.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 127 | [聊天图片查看器到头左右钮仍显示](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天图片查看器到头左右钮仍显示.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 128 | [聊天图片查看器打开载入闪一下](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天图片查看器打开载入闪一下.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 129 | [聊天图片查看器触控pipe未验](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天图片查看器触控pipe未验.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 130 | [聊天记录与transcript切开](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/聊天记录与transcript切开.md>) | P1 | 设计已齐·可编码 | 架构 | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 131 | [能力用例模型得分表](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/能力用例模型得分表.md>) | P3 | 候选 | 评测 | — | 标题/metadata筛出；不纳入本批 |
| 132 | [自进化](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/自进化.md>) | P3 | 候选 | 其它 | — | 标题/metadata筛出；不纳入本批 |
| 133 | [补齐 magpie 级供应商](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/补齐 magpie 级供应商.md>) | P2 | 候选 | 协议/Provider | [] | 标题/metadata筛出；不纳入本批 |
| 134 | [被动接上旧事](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/被动接上旧事.md>) | P2 | 候选 | 记忆 | [] | 标题/metadata筛出；不纳入本批 |
| 135 | [裸正文不进气泡](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/裸正文不进气泡.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 136 | [认领确认本环境未见](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/认领确认本环境未见.md>) | P3 | 候选 | 设备/网关 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 137 | [记忆v2 批7 导入与备份](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/记忆v2 批7 导入与备份.md>) | P3 | 候选 | 记忆 | — | 标题/metadata筛出；不纳入本批 |
| 138 | [记忆写满硬报错不截断](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/记忆写满硬报错不截断.md>) | P2 | 已落地 | 记忆 | — | 标题/metadata筛出；不纳入本批 |
| 139 | [记忆对主人透明](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/记忆对主人透明.md>) | P2 | 候选 | 记忆 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 140 | [记忆条目一条一文件](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/记忆条目一条一文件.md>) | P2 | 搁置 | 记忆 | [] | 标题/metadata筛出；不纳入本批 |
| 141 | [记忆面板整篇与经历列表](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/记忆面板整篇与经历列表.md>) | P2 | 待验收 | 记忆 | [] | 标题/metadata筛出；不纳入本批 |
| 142 | [详情人设改称职责](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/详情人设改称职责.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 143 | [详情模型选择UI重做](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/详情模型选择UI重做.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 144 | [账号叠层顶栏斜杠双名](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/账号叠层顶栏斜杠双名.md>) | P3 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
| 145 | [账号菜单版本对 tip](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/账号菜单版本对 tip.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 146 | [资料库与置顶](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/资料库与置顶.md>) | P3 | 候选 | UI | — | 标题/metadata筛出；不纳入本批 |
| 147 | [跨设备 Bot 互相询问](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/跨设备 Bot 互相询问.md>) | P3 | 已排期 | 设备/网关 | — | 标题/metadata筛出；不纳入本批 |
| 148 | [轨迹可发现与回放](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/轨迹可发现与回放.md>) | P1 | 已完成 | Jobs/执行 | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 149 | [轨迹总览与点击详情](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/轨迹总览与点击详情.md>) | P2 | 实现中 | UI | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 150 | [轨迹折叠显示思考](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/轨迹折叠显示思考.md>) | P1 | 已完成 | UI | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 151 | [轨迹旧shell与新bash混排](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/轨迹旧shell与新bash混排.md>) | P3 | 候选 | UI | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 152 | [轨迹显示子代理步骤](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/轨迹显示子代理步骤.md>) | P2 | 候选 | UI | 列表，直接相关项已全文核对 | 全文对照，真实覆盖/排除见上表 |
| 153 | [输入框URL高亮可点](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/输入框URL高亮可点.md>) | P1 | 已完成 | 交互 | — | 标题/metadata筛出；不纳入本批 |
| 154 | [输出截断自动续写](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/输出截断自动续写.md>) | P2 | 已完成 | 协议/Provider | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 155 | [过程条只显示 description](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/过程条只显示 description.md>) | P1 | 已完成 | UI | — | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 156 | [连接器失败串mcp前缀略硬](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/连接器失败串mcp前缀略硬.md>) | P3 | 候选 | UX文案 | [] | 标题/metadata筛出；不纳入本批 |
| 157 | [连接器本机缺uvx环境](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/连接器本机缺uvx环境.md>) | P3 | 候选 | 环境 | [] | 标题/metadata筛出；不纳入本批 |
| 158 | [连接器设置页不展示runtime](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/连接器设置页不展示runtime.md>) | P3 | 候选 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 159 | [连接应用商店](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/连接应用商店.md>) | P2 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 160 | [逐字重放请求](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/逐字重放请求.md>) | P4 | 候选 | 其它 | — | 标题/metadata筛出；不纳入本批 |
| 161 | [重启失败由外置Codex兜底](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/重启失败由外置Codex兜底.md>) | P2 | 候选 | 自进化基础 | 列表，直接相关项已全文核对 | 标题/metadata筛出；不纳入本批 |
| 162 | [链接皮站点图标](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/链接皮站点图标.md>) | P1 | 已完成 | — | — | 标题/metadata筛出；不纳入本批 |
| 163 | [限流过久时结束或换模型](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/限流过久时结束或换模型.md>) | P2 | 已完成 | 协议/Provider | [] | 边界复用；正文未逐字重读，以atlas/源码为准 |
| 164 | [顶栏中间可点开详情](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/顶栏中间可点开详情.md>) | P1 | 已完成 | UI | [] | 标题/metadata筛出；不纳入本批 |
| 165 | [默认写跑先问](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/候选/默认写跑先问.md>) | P3 | 候选 | 交互 | [] | 标题/metadata筛出；不纳入本批 |
