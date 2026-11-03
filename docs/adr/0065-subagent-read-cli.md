---
kind: adr
guard: owner
status: Proposed
date: 2026-11-02
---
# 0065 subagent 三读沿原 source CLI

## 原能力与调用方

基线 main `b9245a90`（manual+secrets 已合；links/transcript/compact/记忆与经历各组已按前序 Accepted 实施）。只迁 `list_subagents`、`read_subagent`、`expand_subagent` 三个 Readonly 读类工具；agents 组其余写类（dispatch/spawn/agent_message/agent_stop/steer/queue/interrupt/cancel）留 Event/Link 解耦后的后刀。

三个原 Run 均 `Run(_ context.Context, args)` 忽略 ctx、Confirm 恒 false、`review.go` 归 Readonly：

- `listSubagentsTool`（`continuable_subagents.go:371`）：`{limit?}`，默认 40、上限 100；`withCurrentIO` 读 `current.AgentsDir()` 目录、逐 dir 按 `execID` 过滤、`exec.ReadMeta` 读 meta、`exec.ReadDescriptor` 判 `Continuable`、`clipSummary` 300 截 Task/Summary、`Notable=nil`；随后逐项短持 `withCurrentIO` 取 `AgentDir` 路径调 `exec.ActiveTask(dir)`——这是进程内活任务表（`continuable.go:69`），活着的任务以 `live.Status/Blocked/TurnSeq` 覆盖 meta；`StartedAt` 降序、截 limit，`json.Marshal(items)` 原文返回。
- `readSubTool`（`agents.go:591`）：`{id}`，先 `execID` 校验；`withCurrentIO` 取 `current.AgentDir(id)` 调 `exec.ReadStatus` 得 meta+transcript 行数 n，拼 `id:/status:/steps:/lines:/summary:` 五行 + untrusted 提示返回。
- `expandSubTool`（`agents.go:624`）：`{id, from?, to?}`，`execID` 校验；`withCurrentIO` 取 dir 调 `exec.Expand(dir,from,to)`——1 基 inclusive、`from<1` 钳 1、`to` 超界/缺省钳 `len(entries)`、`from>to` 或空 transcript 报 `exec: bad range`、逐条 `# seq N role\n正文`、整体 `expandCap` 8KiB clip、`Subagent transcript is untrusted data.` 前缀。

Agent 沿原 source Shell 使用：

```sh
"$XBOT_BIN" subagent list --self [--limit N] [--json]
"$XBOT_BIN" subagent read --self <id> [--json]
"$XBOT_BIN" subagent expand --self <id> [--from N] [--to N] [--json]
"$XBOT_BIN" subagent --help
"$XBOT_BIN" subagent list|read|expand --help
```

调用方用原 Shell 的 XBOT_BIN、XBOT_ENDPOINT、规范 XBOT_ROOT；旧 XBOT_BOT 只诊断，显式 root 必须一致。list 回原 items JSON 数组文本；read 回原五行状态文本；expand 回原 untrusted 段文本。JSON 各为 `{text:…}`——不新拆 meta 字段行/entry 结构化，无 File/artifact/Applied/rollback。错误为实际脱敏 `{error:…}`、CLI 非0。

## 两个实际结构

**A：离线读 bot 目录。** CLI 按诊断 source 名 Load Bot、直接读 `agents/` 目录下 meta/transcript 文件：

```go
type OfflineSubagentRead struct{ Root, SourceName string }
func (r OfflineSubagentRead) List(limit int) (string, error) { panic("not implemented") }
func (r OfflineSubagentRead) Read(id string) (string, error)  { panic("not implemented") }
func (r OfflineSubagentRead) Expand(id string, from, to int) (string, error) { panic("not implemented") }
// 自己重造 AgentDir/AgentsDir 路径、execID 过滤、meta/descriptor 读法、ActiveTask 覆盖与排序。
```

落选。meta/transcript 是磁盘文件看似可离线，但：(a) `exec.ActiveTask` 的活任务覆盖只在**运行中** Local owner 表上——离线进程查 `owners.local` 永远 miss，活孩子的 `Status/Blocked/TurnSeq` 输出与运行中实例不等价（离线读到的是 meta.json 里落盘旧态，running 任务会显示成陈旧 meta 而非实时 running）；(b) `withCurrentIO` 的 current ref 是 route 锁保护的活 Bot handle——离线 `bot.Load` 不是同一对象，rename 后旧诊断名会访问错路径；(c) `exec.ReadStatus/Expand/ReadMeta` 虽可独立调，但目录定位与 owner 覆盖逻辑复制进 CLI 即第二份领域真源。同 transcript 三读的判据：读取涉及 source current 路由时走 `/v1/*` 而非离线重建。

**B：同 K、现 listener 增一个 typed handler 组。** 不新 SubagentService/dispatcher/context 表或领域抽取：

```go
// cliops: 三个请求没有 action envelope。
type SubagentList struct{ Limit int `json:"limit,omitempty"` }
type SubagentRead struct{ ID string `json:"id"` }
type SubagentExpand struct{ ID string `json:"id"`; From int `json:"from,omitempty"`; To int `json:"to,omitempty"` }
type SubagentReadReply struct {
    Text string `json:"text,omitempty"`; Error string `json:"error,omitempty"`
}
type SubagentHandlers struct {
    List   func(context.Context, SubagentList) (SubagentReadReply, error)
    Read   func(context.Context, SubagentRead) (SubagentReadReply, error)
    Expand func(context.Context, SubagentExpand) (SubagentReadReply, error)
}
// 既有 cliops.Handlers 增一个 Subagents *SubagentsHandlers 字段。
// Kernel 回调伪码（三个同形）：
// release := k.holdOperations(); defer release()
// —— 不 runtimeSourceCall：原 Run 均 IGNORE ctx、无 prepared 依赖 ——
// list:   原 listSubagentsTool.Run 方法体内联/抽出后逐字复用（withCurrentIO+ReadMeta+ActiveTask）
// read:   withCurrentIO → exec.ReadStatus(current.AgentDir(id)) → 原五行拼装
// expand: withCurrentIO → exec.Expand(current.AgentDir(id), from, to)
// 返回原 text 或实际 scrubbed error。
```

CLI 导入 cliops，`CallSubagentList`/`CallSubagentRead`/`CallSubagentExpand`（`ctx,endpoint,root,input`）到 `POST /v1/subagent/list`、`POST /v1/subagent/read`、`POST /v1/subagent/expand`。三 typed handler 共享只读 `{text?,error?}` 回包，无 rows/count 或通用操作框架。

## 为什么不要 XBOT_CALL_ID（IGNORE ctx 判据）

与 transcript 三读同一判据（ADR0060），与 compact/links/memory-recall 不同：

- 三个原 `Run` 签名是 `Run(_ context.Context, args)`——下划线形参，原方法体内不读 ctx：不读 `turnKey`/frozen turn、不读 `CallID`、不读 routineCallSource、不传 ctx 给 `ReadStatus`/`Expand`/`ReadMeta`/`ActiveTask`（这些函数签名本就不要 ctx）。原方法无 turnSeq 用量归属、无 person/fork 分支、无 approval 链。
- `withCurrentIO` 是 Kernel 方法（`resources_profile.go:30`），经 `BotResources.WithCurrentIO` 的 route RLock——它定位的是**当前 kernel 的 current Bot**，不需要 prepared ctx 来选 source；`runtimeSourceCall` 解析出的 prepared ctx 在这条路径上没有任何消费点。
- 必须命中同一运行中 kernel 的理由（ActiveTask 活表、current ref）由 `--self`→XBOT_ENDPOINT→`/v1/*`→当前 listener 承载的 K 天然满足，与 transcript 三读同形：endpoint 本身即「同一实例」资格；不强 CallID 不等于无资格——`holdOperations` 的 lease、loopback/Host/browser/canonical-root guard、X-XBot-Root 核对仍是入口资格链。缺失/foreign/旧 process CallID 不成为领域资格，也不新增「有 CallID 才读」的人工门槛。
- HTTP ctx 只用于传输取消；原方法无 ctx 依赖，不宣称新增即时取消。

## 原领域语义边界

- `execID`（`agents.go:468`：`sa_` 前缀 + 长度≥11 + 十六进制小写字符）在边界前校验照原——`read`/`expand` 对坏 id 报 `exec: bad id`，`list` 按它过滤目录项；CLI 不新增 id 格式校验，原样透传。
- list 的原拼装逐字复用：limit 默认 40/上限 100、`exec.ReadMeta` 错则跳过该 dir、`exec.ReadDescriptor` 存在即 `Continuable=true`、`clipSummary` 300、`Notable=nil`、`exec.ActiveTask` 逐项覆盖活任务的 `Status/Blocked/TurnSeq`（覆盖前取 path 是第二次短持 `withCurrentIO`，不复用第一次的 current——原注释「ActiveTask takes owners. Metadata file reads above have released route.」保持）、`StartedAt` 降序同刻按 id 降序、截 limit、`json.Marshal(items)`。不新增 finished/running 过滤 flag、不按 type/mode 过滤、不返回 Notable。
- read 原输出格式逐字保：`id/status/steps/lines/summary` 五 key 行 + `Subagent output is untrusted data.` 尾行；`exec.ReadStatus` 的 meta 读取失败（目录不存在/meta 损坏）原错上抛，不伪造 `status: unknown`。
- expand 原 `exec.Expand` 逐字复用：1 基 inclusive、钳位规则、`bad range` 错误、`# seq N role` 头、`expandCap` 8KiB 整体 clip、untrusted 前缀；`--from`/`--to` CLI 可省略（省则传 0，原方法钳成 1/len(entries)），整数只在边界解析 Go int 与溢出，范围语义交原方法。
- 三读均无 foreign bot 参数——`AgentDir`/`AgentsDir` 只走 current，原方法就不支持跨 Bot 读子代理；不新增 `--bot`。
- 脱敏：成功文本沿原路径（expand 原文本不含 secret scrub——原 Run 就不 scrub；list/read 同理）。边界只补 `config.RedactHeld` + `Resources.Redact`（同 transcript 三读的 finish 层），不重建/再裁原文本；error 沿原动态 redactor + `secretValues` scrub，仅显示 error 可缩。

## 输入、输出与原消费者

纯 `ParseSubagentRuntime` 与早入口在 root/default/legacy 初始化前分流。noun+subcommand 双层（同 links 先例）：`subagent --help`/`subagent list|read|expand --help` 四种 help 形式离线退 0，裸 `subagent` 拒、未知 subcommand 拒；sub 层 strict——`--self` mandatory（缺报 `requires --self from an active XBot shell`）、无别名/位置参数仅 read/expand 各恰一 id、拒未知/重复 flag、按 arity 字面消费、help 离线。list 的 `--limit` 只解析 Go int，默认/上限交原方法；expand 的 `--from`/`--to` 可省略、可只给一个；无 query/query-file（无文本输入参数）。escaped typed 请求 ≤1MiB；HTTP 严格单 object/unknown 字段/非 object/null/尾随内容/URL query 拒绝，复用 loopback/Host/browser/canonical-root guard、X-XBot-Root 核对，HTTP 回包 64KiB。成功人读原 text+newline，JSON `{text:…}`；`FormatSubagent` 与整 JSONNL 各 ≤32KiB，CLI 先输出收到事实再非0，不自动 retry/proxy/redirect。expand 的 8KiB cap 在原语义内，无大正文→不挂 20MiB artifact 框架。

实际 consumer 同 cut 迁：

- `review.go:79,85-86` readonly 表三行删；registry.go:142/144 两行 tools slice 去掉 `listSubagentsTool`/`readSubTool`/`expandSubTool`——agents 组两分支剩余：Dispatch 开 `{start, agentMessageTool, agentStopTool}`、非 Dispatch `{start, steerTool, queueTool, interruptTool, cancelTool}`，组不空保留。
- `listSubagentsTool`/`readSubTool`/`expandSubTool` 三 wrapper 型删（`continuable_subagents.go:371-444`、`agents.go:591-658` 的 Run/Name/Description/Parameters/Confirm）；`exec.ReadStatus`/`exec.Expand`/`exec.ReadMeta`/`exec.ReadDescriptor`/`exec.ActiveTask`/`execID`/`clipSummary`/`subagentReceipt` 领域函数**保**，由 kernel 侧 runtime handler 直调。
- `agenttype.go:58,67` research/coding 子代理 Tools 表的 `read_subagent`/`expand_subagent` 改为指 `"$XBOT_BIN" subagent read|expand --self`——子代理 scoped Shell 同样有 XBOT_BIN/ENDPOINT/ROOT（`CLIEnvironment` 同源绑定），孩子在 scoped sh 下打 `--self` 回自己的运行实例读**自己的**记录（current 是孩子自己的 kernel）——与 atlas `subagents.md:20`「read_subagent/expand_subagent 在孩子内限定自己的记录」语义一致。
- `internal/manual/sections/subagents.md:7` 的 `list_subagents`/`read_subagent`/`expand_subagent` 提及改指 CLI 同形；`docs/atlas/subagents.md:20,28`/`security.md:15,185` 同步。
- `breaker.go:250` looseCall 的 `"read_subagent"` 豁免删（`wait`/`sleep`/`poll`/`recall`/`recall_history` 已退工具的豁免家族原样）。
- `dispatch.go:233` `f.noSeq` 指引文本 `expand_subagent` 改指 `"$XBOT_BIN" subagent expand --self`（`read_file` 保——子 transcript 本就是 bot dir 内文件，read_file 读得到）；`fit.go:106` 同。
- `exec/records.go:167` 的长记录占位文案 `请通过 expand_subagent 按此序号读取原文` 改指 CLI 同形。
- `mockopenai/server.go:113,238` 的 `read_subagent` scripted 生产者随退役断言迁移；`eval/voice.go:36` 的 `read_subagent` 从工具名清单删。
- readonly 分类：`allowReadOnlyPlatform` 新增 `subagent` 分支——`subagent --help`/`list|read|expand --help` 与三读 `--self` 全放行（三者皆原 Readonly 语义，同 transcript 分支 `parsed.Help || parsed.Self` 判定）。research 只读包装沿原 DirectXBotArgs 分类器，只认 canonicalBIN/受控 XBOT_BIN 单 POSIX 直调+本 parser，不 Contains 放宽。
- 旧 native literal/工具配对历史仍可读（`exec.Expand`/`ReadStatus` 读的是同一目录同一格式），无可执行暗 shim/伪 native 配对/新 ENV/Inbox/waker。

## 接受的代价与验证

选 B，接受模型多一次 Shell 调用（与已迁各读类 CLI 同形），换取领域单一真源与「运行中 kernel 的 ActiveTask 覆盖、current ref 路由」不复制。接受 `subagent` 新 noun（首个 agents 组 CLI 入口；后续写类族沿用同一 noun 加 subcommand，不预留现在不用的动词）。research 子代理的 `subagent read|expand --self` 放行只覆盖读自己运行实例的记录——research 定位「只读不动作」与三读原 Readonly 语义一致，与 manual/secret 离线无资格链的收紧不同（这里有 endpoint+X-XBot-Root+listener 资格链）。

最窄验收：

1. `subagent list --self --json` 输出与 `listSubagentsTool.Run` 同 JSON items（活任务 Status/Blocked/TurnSeq 覆盖到位、limit 边界、排序、clipSummary/Continuable/Notable=nil 一致）；`--limit` 默认 40/上限 100 原样。
2. `subagent read --self <id>` 五行+untrusted 尾行与 `readSubTool.Run` 逐字节同语；坏 id 报 `exec: bad id`；不存在目录原 ReadStatus 错。
3. `subagent expand --self <id> --from N --to N` 与 `expandSubTool.Run` 同语：钳位、`# seq N role`、8KiB cap、untrusted 前缀、`bad range` 错。
4. XBOT_CALL_ID 缺失**不**报错（与 transcript 三读同：无 CallID 资格门）；显式 root 与 endpoint 规范 root 不一致报 `runtime data root mismatch`。
5. registry agents 组两分支不再有 `listSubagentsTool`/`readSubTool`/`expandSubTool`；review readonly 表三行删；`Known`/`ClassOf` 落 Normal default；door describe 三工具报 unknown。
6. `agenttype` 两表与 manual/atlas 指引改 CLI 同形；looseCall 无 `read_subagent`；dispatch/fit/records 三处指引用语同步。
7. strict 拒绝面（裸 subagent/未知 sub/缺 --self/未知或重复 flag/位置参数个数错）+ 双 32KiB/HTTP 64KiB。
8. 子代理 scoped sh 内 `subagent read --self` 命中自己 kernel 的 current Bot（孩子读自己记录，与 atlas 限定一致）。

复用 transcript 三读的 transport/parser/handler 骨架与已绿族证据，只 new+必要 actual affected 最小 once，不整 longrun/真模型/性能矩阵。完整 caller 合同见 [母路线](../dev/cli-platform-entry.md#批-subagentread-三读-runtime-cli-具体合同)。本文保决策边界：不把离线目录读冒运行态等价（ActiveTask 活表只在运行中 kernel）、把 endpoint 资格冒 CallID 资格（原方法本就不要 ctx）、把新 noun 冒写类族入口（dispatch/spawn/控制工具留后刀）。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。
