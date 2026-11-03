---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0066 shell jobs 三工具 runtime CLI

## 原能力与调用方

基线 main `ad17848a`（subagent 三读已合）。本刀把 `shell_output` / `shell_status` / `shell_cancel` 三工具迁 `xbot jobs … --self` runtime 并整组退 `shell_jobs` kernel 行。盘点依据 remaining-inventory §1.1 shell_jobs 组三行（`shelljob.go:381/411/479`）+ §3 第二刀「`shell_output`/`shell_status` 读侧必须 runtime `--self`；`shell_cancel` 写可同批」。

三个原 Run 均 `Run(_ context.Context, args)` 忽略 ctx：

- `shellOutputTool`（`shelljob.go:395`）：`{id, offset?, tail?}`；`k.shellJobs()` → `jobs.Output(id, offset, tail)`。`Jobs.Output`（`tools/shelljob.go:325`）flush 活写器、按整段流 offset/`next_offset`/`dropped_before`/`held_bytes`/`[truncated]`/`tailLines` 拼装，chunk 走 `lv.redact` + `j.redact` 双层脱敏（`maxJobRead` 32KiB、`maxJobTail` 200）。
- `shellStatusTool`（`shelljob.go:423`）：`{id?}`；`k.shellJobs()`；id 空 → `jobs.List()` 逐条 `id status clipRunes(cmd,80) [cwd …] [held_bytes: N]`、超 32 条 `...` 截断、空表回 `no background tool jobs`；id 非空 → `jobs.Status(id)` 拼 `id status [exit N] [cwd:] [held_bytes:] [output:]`。
- `shellCancelTool`（`shelljob.go:500`）：`{id}`；`k.shellJobs()` → `jobs.Cancel(id)`（running 且本进程 owned：杀进程组/取消调用，回 `cancelled`/`cancellation requested`/`status <s>`；`ErrForeignJob` 兜底 `execSnapshot().(*exec.Remote).CancelToolJob(ctx.Background, jobs.Dir, id)`）。

review 表（`review.go:51-53`）：`shell_output`/`shell_status` Readonly、`shell_cancel` Normal；`NeedsOwner` 对三工具均恒 false（两读落 Readonly 分支、cancel 落 Normal 分支，不弹卡）。`shellCancelTool.Confirm`（`shelljob.go:490`）只产卡摘要 `"cancel <id>"`，不决定是否确认。

Agent 沿原 source Shell 使用：

```sh
"$XBOT_BIN" jobs output --self <id> [--offset N | --tail N] [--json]
"$XBOT_BIN" jobs status --self [<id>] [--json]
"$XBOT_BIN" jobs cancel --self <id> [--json]
"$XBOT_BIN" jobs --help
"$XBOT_BIN" jobs output|status|cancel --help
```

调用方用原 Shell 的 XBOT_BIN、XBOT_ENDPOINT、规范 XBOT_ROOT；旧 XBOT_BOT 只诊断，显式 root 必须一致。output 回原 status/offset/next_offset 拼装文本；status 回原单行/列表文本；cancel 回原 `cancelled|cancellation requested|status <s>` 文本。JSON 各为 `{text:…}`——不新拆 job 字段结构，无 File/artifact/Applied/rollback。错误为实际脱敏 `{error:…}`、CLI 非0。

## 为什么不要 XBOT_CALL_ID（IGNORE ctx 判据）

与 subagent/transcript 三读同一判据（ADR0060/0065），与 compact/links/memory-recall 不同：

- 三个原 `Run` 签名是 `Run(_ context.Context, args)`——下划线形参，原方法体不读 `turnKey`/frozen turn、`CallID`、`routineCallSource`，`Jobs.Output`/`Status`/`List`/`Cancel` 与 `remote.CancelToolJob` 签名本就不要 source ctx。原方法无 turnSeq 归属、无 person/fork 分支、无 approval 链。
- `k.shellJobs()` 取的是**本 kernel 的进程内 Jobs 表**（`k.Resources.Jobs` 或 `liveShell`），不需要 prepared ctx 来选 source；`runtimeSourceCall` 解析出的 prepared ctx 在这条路径上没有任何消费点。
- 必须命中同一运行中 kernel 的理由不是 ctx 而是三点：(a) Jobs 表进程内活写器（`lv.w` span/held）、live status/cancel 回调、`exec.Remote` 远进程兜底全只在本实例；(b) `Resources.Jobs` 是装配绑定的同一张表，离线重建目录读出的 meta 无 live writer 状态、running 任务显示成陈旧记录、cancel 完全不可达；(c) 重启后 running 标 lost 的语义只在 Load 过的本表成立。这三层由 `--self`→XBOT_ENDPOINT→`/v1/*`→当前 listener 承载的 K 天然满足，同 subagent「endpoint 即同实例资格」；CallID/prepared 在这条路径上无消费点，不新增人工门槛。
- HTTP ctx 只用于传输取消；原方法无 ctx 依赖，不宣称新增即时取消。cancel 的 domain ctx 固定 `context.Background()`（同原 Run 字面行为：jobs.Cancel 内部 250ms 等待与 remote fallback 都不随传输取消）。

## cancel 不加 owner 卡与资格门

- 原 `shell_cancel` 归 Normal：`NeedsOwner` 恒 false、无 grant/deniedBefore/parkable 消费点、Confirm 只产摘要。runtime 复现 owner 卡要求 `runtimeApprovalSource` + prepared ctx，与 IGNORE ctx 判据直接冲突——引入 prepared ctx 是为卡而造依赖，原语义本就没卡。
- `jobs.Cancel` 只作用于本 Bot `shell-jobs/` 表内的 job：杀的是本实例起的进程组或请求本表 owned call 取消，`ErrForeignJob` 时转 `exec.Remote` 同名兜底（Remote 场景 Jobs.Dir 仍是本 bot 目录）。作用域天然限在本实例本 Bot，无跨 Bot/跨实例写面，不新增 `--bot`/grant。
- 等价依据：原模型侧 cancel 经 `sh` 语义与 CLI 经 `/v1/jobs/cancel` 语义同为「本表请求取消」，CLI 侧不恢复卡面；研究侧见下「readonly 分类」——research 不放行 cancel，与原 Normal 语义一致收紧而非放宽。

## 两个实际结构

**A：离线读 `shell-jobs/` 目录。** CLI `bot.Load` 诊断名 + `tools.NewJobs(dir).Load()` 直读 meta/log：

```go
type OfflineJobsRead struct{ Root, SourceName string }
func (r OfflineJobsRead) Output(id string, off, tail int) (string, error) { panic("x") }
func (r OfflineJobsRead) Status(id string) (string, error)               { panic("x") }
func (r OfflineJobsRead) Cancel(id string) (string, error)               { panic("x") }
// 离线表 Load 把 running 全标 lost；无 live writer；cancel 无 owned call。
```

落选。(a) 离线 `NewJobs.Load` 会把所有 running 记录标 lost——与原表语义直接冲突：同一 job 经 `--self` 读是 running、经离线读必显 lost；(b) live writer 的 span/held/flush 只在本进程（`freshen`/`Output` 的 `lv.w` 分支），离线读不到 `held_bytes`、offset/dropped 计数失真；(c) cancel 语义整体不可达——离线表无 `lv.cancel` 回调、无 owned call，只能写 meta 不杀进程，与「杀进程组/请求取消」原语义不等价；(d) `k.shellJobs()` 的 `Resources.Jobs`/`liveShell` 定位、redact 闭包（`scrubSecrets(redactor, secretValues)` + `shellForms`）复制进 CLI 即第二份领域真源。同 subagent 判据：活态只在本实例，走 `/v1/*`。

**B：同 K、现 listener 增一个 typed handler 组。** 不新 JobsService/dispatcher/领域抽取：

```go
// cliops: 三个请求没有 action envelope。
type JobsOutput struct{ ID string `json:"id"`; Offset int `json:"offset,omitempty"`; Tail int `json:"tail,omitempty"` }
type JobsStatus struct{ ID string `json:"id,omitempty"` }
type JobsCancel struct{ ID string `json:"id"` }
type JobsReply struct {
    Text string `json:"text,omitempty"`; Error string `json:"error,omitempty"`
}
type JobsHandlers struct {
    Output func(context.Context, JobsOutput) (JobsReply, error)
    Status func(context.Context, JobsStatus) (JobsReply, error)
    Cancel func(context.Context, JobsCancel) (JobsReply, error)
}
// 既有 cliops.Handlers 增一个 Jobs *JobsHandlers 字段。
// Kernel 回调伪码（三个同形）：
// release := k.holdOperations(); defer release()
// —— 不 runtimeSourceCall：原 Run 均 IGNORE ctx、无 prepared 依赖 ——
// output/status: 原 shellOutputTool/shellStatusTool.Run 方法体内联/抽出后逐字复用
//   (shellJobs()→jobs.Output / List+Status 原拼装)
// cancel: 原 shellCancelTool.Run 方法体逐字复用
//   (shellJobs()→jobs.Cancel→ErrForeignJob 时 execSnapshot Remote 兜底)
// 返回原 text 或实际 scrubbed error。
```

CLI 导入 cliops，`CallJobsOutput`/`CallJobsStatus`/`CallJobsCancel`（`ctx,endpoint,root,input`）到 `POST /v1/jobs/output`、`POST /v1/jobs/status`、`POST /v1/jobs/cancel`。三 typed handler 共享 `{text?,error?}` 回包，无 rows/count 或通用操作框架。

## 原领域语义边界

- `shellJobs()` 的 nil 分支原样：`errNoShellJobs`（`shell: background jobs are not available`）原错上抛，不伪造空表；eval jail 无表 shell 同语义。
- output 原拼装逐字复用：`Jobs.Output` 全链（freshen/flush、span/held、`clip.Valid`、`lv.redact`+`j.redact`、status/offset/next_offset/held_bytes/dropped_before/[truncated] 行序）；`--offset`/`--tail` 只在边界解析 Go int，可各给或都不给（原语义 `tail>0` 时 offset 忽略），上限 `maxJobTail`/`maxJobRead` 交原方法；CLI 不新增 id 格式校验。
- status 原拼装逐字复用：无 id 时 List 的 32 条截断/`...`/clipRunes(80)/cwd/held_bytes 行、`no background tool jobs`；有 id 时 `id status [exit N]/cwd:/held_bytes:/output:` 行序；`jobs.Status` 未知 id 原错 `shell: no such job <id>`。
- cancel 原方法逐字复用：`jobs.Cancel` 三分支（finished→`<-lv.done` 回 `status <s>`；无 cancel→`ErrForeignJob`；owned→`cancelled`/`cancellation requested`）+ `ErrForeignJob` 时 `execSnapshot().(*exec.Remote)` 断言与 `remote.CancelToolJob(context.Background(), jobs.Dir, id)` 兜底——Remote 兜底是本实例领域逻辑，留在 handler 内不外移；非 Remote executor 时 `ErrForeignJob` 原错返回。
- 三工具均无 foreign bot 参数——`shellJobs()` 只取本 kernel 表，原方法就不支持跨 Bot；不新增 `--bot`。
- 脱敏：成功文本沿原路径（`Jobs.Output` 的 `lv.redact`/`j.redact` 原链、status/cancel 原文本原样不 scrub），边界只补 `config.RedactHeld` + `Resources.Redact`（同 subagent finish 层）；error 沿原动态 redactor + `secretValues` scrub，仅显示 error 可缩。

## 输入、输出与原消费者

纯 `ParseJobsRuntime` 与早入口在 root/default/legacy 初始化前分流。noun+subcommand 双层（同 subagent/links 先例）：`jobs --help`/`jobs output|status|cancel --help` 四种 help 形式离线退 0，裸 `jobs` 拒、未知 subcommand 拒；sub 层 strict——`--self` mandatory（缺报 `requires --self from an active XBot shell`）、无别名；output/cancel 各恰一位置 id、status 零或一位置 id、拒未知/重复 flag、按 arity 字面消费、help 离线。`--offset`/`--tail` 仅 output 合法、只解析 Go int；无 query/query-file/stdin（无文本输入参数）。escaped typed 请求 ≤1MiB；HTTP 严格单 object/unknown 字段/非 object/null/尾随内容/URL query 拒绝，复用 loopback/Host/browser/canonical-root guard、X-XBot-Root 核对，HTTP 回包 64KiB。成功人读原 text+newline，JSON `{text:…}`；`FormatJobs` 与整 JSONNL 各 ≤32KiB，CLI 先输出收到事实再非0，不自动 retry/proxy/redirect。`Jobs.Output` 自身 `maxJobRead` 32KiB cap 在原语义内——CLI 端 reply text 理论可达 ~32KiB+头部，仍走同一份 32KiB bound 检查，超限如实报 `runtime reply facts exceed 32 KiB Shell output budget`（同 subagent 边界，不另扩预算）。

实际 consumer 同 cut 迁：

- `registry.go:131-135` `shell_jobs` 行整行删（组空随退：on 判据 `shellBackgroundOn`/`liveShell` 与 tools slice `shellOutputTool{k}, shellStatusTool{k}, shellCancelTool{k}` 同行退）；`review.go:51-53` 三行删（`shell_output`/`shell_status` Readonly、`shell_cancel` Normal）。
- `shelljob.go:381-528` 三 wrapper 型删（`shellOutputTool`/`shellStatusTool`/`shellCancelTool` 的 Name/Description/Parameters/Confirm/Run + `errNoShellJobs`/`errShell`——errShell 若仅三工具用则随退，否则保）；`jobs.Output/Status/List/Cancel`/`tools.ErrForeignJob`/`execSnapshot`/`exec.Remote.CancelToolJob` 领域函数**保**，由 kernel 侧 runtime handler 直调；`attachShellJobs`/`attachShell`/`shellBackgroundOn`/`bindShellRuntime` 全保（Jobs 装配与工具注册无关）。
- `tooljob.go:36-38` `runToolCall` 的 `case shellOutputTool, shellStatusTool, shellCancelTool` 免 Jobs 槽位分支随三型删——分支引用三工具类型，型删则分支删；runtime handler 本就不经 `runToolCall` 不占 Jobs 槽，原「inspect/cancel 不占用 Job 槽」语义由 handler 不经 Jobs.RunCall 天然保持。
- `internal/tools/shell.go:260` `interpreterDescription` 指引文案 `shell_output reads output (offset or tail), shell_status checks status, shell_cancel requests cancellation` 改指 `"$XBOT_BIN" jobs output|status|cancel --self` 同义句；`internal/tools/shelljob.go:803` `formatBackground` 人读尾行 `Use shell_output to read (offset or tail), shell_status to check, and shell_cancel to stop.` 同步改指 `xbot jobs output|status|cancel --self`（模型经 Shell 拿 XBOT_BIN 回同一 listener），`shelljob.go:121-123` `shellBackgroundOn` 注释的三工具提法随注释对象（on 判据/组行）同行退。
- `internal/kernel/c1_output_test.go:154` `TestC1JobControlsWorkAtCapacity` 对 `shellStatusTool/shellOutputTool/shellCancelTool` 三型包 `reviewedTool` 的免槽位断言随三型删——容量语义由 runtime handler 不经 `runToolCall` 天然不占槽保持，原断言无替代物；`internal/kernel/shelljob_test.go:604` `shellStatusTool{k}.Run` 直接调用断言迁 `jobs.Status` 领域断言（held_bytes 行保核 `st.Held`）。
- `internal/tools/shelljob_test.go:36` `TestShellBackgroundDescription` 的 `shell_output`/`shell_status`/`shell_cancel` 文案断言随 shell.go 文案改指 CLI 提法；`internal/tools/shelljob_prop_test.go:17` 注释 `shell_output` 提法同步改 `jobs output`。
- `docs/atlas/kernel.md:172`（慢工具后台化段 `shell_output/status/cancel` 提法）、`docs/atlas/ecosystem.md:27`（原生内置 Link 段「复用 shell_output/shell_status/shell_cancel」）、`docs/atlas/security.md:185-186`（readonly 表 `shell_output`/`shell_status`、normal 表 `shell_cancel` 三处）同步改指 `jobs … --self` CLI 同形。
- `e2e/smoke` 四处 scripted 生产者随退役断言迁移：`cli_profile_full_runtime_test.go:298` `shell_status`、`cli_chat_group_runtime_projection_test.go:150` `shell_output`、`cli_bot_runtime_projection_test.go:233` `shell_output`、`mcp_runtime_test.go:414-416` `shell_status`/`shell_output`/`shell_cancel`——改 scripted `sh` 调 `"$XBOT_BIN" jobs … --self`（同 subagent cut 的 mockopenai/e2e 迁移先例）。
- `internal/kernel/envaware_test.go:449` `toolCall("b2","shell_status",…)` 断言迁移；`internal/review/review_test.go` readonly/normal 循环列表的 `shell_output`/`shell_status`/`shell_cancel` 三名删（退后落 default，不再需原分类断言）；`internal/kernel/shelljob_test.go:46,59` 的 `shell_status`/工具名提及按现行契约修正。
- readonly 分类：`allowReadOnlyPlatform` 新增 `jobs` 分支——`jobs --help`/`output|status|cancel --help` 与 `jobs output|status --self` 放行，`jobs cancel --self` **不放行**（原 Normal 写类，同 links install/remove 拒档；research 定位「只读不动作」与 output/status 原 Readonly 语义一致、与 cancel 原 Normal 语义一致收紧）。沿原 DirectXBotArgs 分类器，只认 canonicalBIN/受控 XBOT_BIN 单 POSIX 直调+本 parser，不 Contains 放宽。
- `cliProfileGuide`（operations.go:18）如需提 jobs 入口按惯例增补短句（三工具退注册后 tool_index 不再见 shell_jobs 组，指引词指向 `jobs --help`）。
- 旧 native literal/工具配对历史仍可读（`shell-jobs/` 目录与 meta/log 格式不变），无可执行暗 shim/伪 native 配对/新 ENV/Inbox/waker；`Jobs` 表本体、后台化完成回报、`deliverToolDone` 全保。

## 接受的代价与验证

选 B，接受模型多一次 Shell 调用（与已迁各 runtime CLI 同形），换取领域单一真源与「进程内 Jobs 表、live writer、owned cancel、Remote 兜底」不复制。接受 `jobs` 新 noun（首个 shell_jobs 组 CLI 入口；后续若有批 5 基础集迁移可沿用同一 noun 加 subcommand，不预留现在不用的动词）。接受 cancel 无卡与 IGNORE ctx 的连带后果：CLI 调用方无调用方识别，cancel 作用域本就限本实例本表，与 Normal 无卡原语义一致；research 不放行 cancel 比原模型侧 Normal 更紧，是有意收紧。

最窄验收：

1. `jobs output --self <id>` 与 `shellOutputTool.Run` 同语：status/offset/next_offset/held_bytes/dropped_before/[truncated] 行序、tail 分支、`shell: no such job`/errNoShellJobs 原错；`--offset`/`--tail` 边界与 maxJobTail 上限原样。
2. `jobs status --self [<id>]` 与 `shellStatusTool.Run` 同语：List 32 截断/`...`/clipRunes/cwd/held_bytes、`no background tool jobs`、单 id 五行变体、未知 id 原错。
3. `jobs cancel --self <id>` 与 `shellCancelTool.Run` 同语：`cancelled`/`cancellation requested`/`status <s>` 三分支、`ErrForeignJob`→Remote 兜底（fake Remote 断言 `CancelToolJob` 收到 `jobs.Dir`/id）→`cancellation requested`；非 Remote 的 `ErrForeignJob` 原错。
4. XBOT_CALL_ID 缺失**不**报错（无 CallID 门）；显式 root 与 endpoint 规范 root 不一致报 `runtime data root mismatch`。
5. registry 无 `shell_jobs` 组行、review 三行删、door describe 三工具报 unknown、`runToolCall` 免槽位分支删；`attachShell`/Jobs 装配不退。
6. shell.go 指引、atlas 三处、四处 e2e scripted、envaware/review/shelljob 测试断言同步；`allowReadOnlyPlatform` 的 `jobs` 分支 output/status 放行、cancel 拒、help 放行。
7. strict 拒绝面（裸 jobs/未知 sub/缺 --self/未知或重复 flag/位置参数个数错/`--offset` 给 status）+ 双 32KiB/HTTP 64KiB。
8. research 只读包装：`jobs output|status --self` 放行、 `jobs cancel --self` 拒绝、help 放行——与 readonly 表迁移一致。

复用 subagent 三读的 transport/parser/handler 骨架与已绿族证据，只 new+必要 actual affected 最小 once，不整 longrun/真模型/性能矩阵。完整 caller 合同见 [母路线](../dev/cli-platform-entry.md#批-shelljobs-三工具-runtime-cli-具体合同)。本文保决策边界：不把离线目录读冒运行态等价（live writer/owned cancel/Remote 兜底只在本实例）、把 endpoint 资格冒 CallID 资格（原方法本就不要 ctx）、把新 noun 冒批 5 基础集入口（shell 解释器本体留批 5）。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。

SINGLE ACTUAL CONCRETE SHAPE PASS（2026-11-02）：已核退役面/保留面/验收八点逐条对照现码，design 方向成立无实质修正；退役面补列 `formatBackground` 人读尾行（shelljob.go:803）改指 jobs CLI、`c1_output_test.go:154` 三型 reviewedTool 免槽位断言与 `shelljob_test.go:604` `shellStatusTool` 直接调用随型删/迁、`shelljob_test.go:36`/`shelljob_prop_test.go:17` Description/formatBackground 文案断言随文案同步、`shellBackgroundOn` 注释三工具提法同行退。本 ADR 转 Accepted 实施。
