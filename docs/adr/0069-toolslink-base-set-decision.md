---
kind: adr
guard: owner
status: Proposed
date: 2026-11-02
---
# 0069 ToolsLink 基础集决策：ToolsLink 整体退役，工具按各 node 能力条件注册

## 原能力与调用方

基线 main `8617fd5e`（cards family 已合 `fa800f10`，本仓 main 已含至 `8617fd5e` 全部工具家族）。盘点依据 remaining-inventory §1.2「ToolsLink 是 namedLink(links,link.NameTools) gating 所有 base 工具」与母路线批 5「盘点剩余基础模型工具」。**先纠背景**：inventory 写的 `namedLink(links,link.NameTools)` gating 实际只剩 `mountGrantLocked` 一处消费——早期 secrets/shell_jobs 两组的 `namedLink` 行判据已在 ADR0064/0066/0068 随整组退役，现码 `internal/kernel/` 内除 `links.go:450` mount 开关外无 `NameTools` 消费点。

ToolsLink（`internal/link/tools.go:6-32`）是名为 `"tools"` 的 organ（`store.go:37` `{ID:"tools", Grant:"tools", Kind:AppNative}`），`syncGrantsLocked` 的 organ 循环（`links.go:377-386`）**每 snapshot 强制挂载**——不进 `apps.json`、不经商店卡、不可授权/收回。`NewToolsLinkAt(work.BotDir(root,name), work.SharedDir(root))` 产 6 个 `*tools.Shell` 解释器（`zsh`/`bash`/`sh`/`powershell`/`cmd`/`git-bash`，按平台探测挂实际可执行项）+ `&tools.ReadFile`/`WriteFile`/`EditFile`/`ViewImage`（`Root` 钉 work.BotDir）+ `&tools.WebFetch`/`WebSearch`。这批工具经 `registry.go:146-155` 的 link 循环注册（`source="organ"`、`group="tools"`、`deferred=false`——organ 非 StoreGrant，不 deferred）。

kernel 侧有三个消费点把 ToolsLink 的工具绑上本实例状态：

- `attachShell`（`shelljob.go:74-94`，每次 `toolRegistry()` 都跑）：遍历 `k.snapshotLinks()` 的 `l.Tools()`，对 `*tools.Shell` 调 `BindJobs`/`BindSession`/`SetMarkerDir`/`bindShellRuntime`（XBOT_* env 装配 + `withShellStartIO` + `BindMarkerIO` + `BindLocalSudo`→vault），对文件/web 工具调 `bindFileCwd`（`BindRoot(k.cwdFor)`、ViewImage 加 `Allowed`）与 `configureWeb`（`Store`/`Now`/`Backend`/`Fetcher`/vision model）。
- `kernel.go:746-757` `memoryEnv` 的 `shown()`：遍历 `l.Tools()` 找第一个 `write_file`，用 `modelPath` 把 memory 绝对路径换成模型可读表示。
- `eval/jail.go:410` `sandboxLink{Name:"tools"}` 与 run.go:278 grant `NameTools`：评测用同名伪 link 注入沙箱工具集，不产 ToolsLink 本体。

其余盘点中的「base 集」实际**不在 ToolsLink 后**：`send_message_to_user`/`ask_user`/`generate_image`/`listen`/`tts`/`todo`/`agent_*`（spawn/dispatch+steer/queue/interrupt/cancel 或 dispatch+agent_message+agent_stop）已是 `registry.go:113-135` 的 `always`/`k.todoToolOn()`/`k.Dispatch` kernel 行；`desktop_*` 走 `NameDesktop` organ（`mountGrantLocked:482`→`desktop.Open`），`browser_*` 走 `NameBrowser` store grant（`mountGrantLocked:484`→`browser.Open`），`code_*` 走 `NameCoding` organ（`mountGrantLocked:486-514`→`code.Open`+`UseProvider`/`UseRunner`/`UseTranscript`），`telegram_send`/`lark_send` 走 connector store grant（`mountGrantLocked:478-480`），`mcp_*` 模型工具已在批 2 全退（`Session` 不再实现 `Tools()`，只走 `xbot mcp` CLI）。`hasLink`/`namedLink` 现存仅剩 `NameMemory`（kernel.go:630 rules 判据）与 `NameAgents`（dispatch.go:40 `dispatching()`）两个消费点，与 tools 无关。

## 决策

**ToolsLink 整体退役（选项 C），工具本体不迁 CLI，注册条件改为 kernel 行 + 按各工具实际依赖的条件。** 三个候选的逐条对照：

- **A 全 CLI 化**：落选。shell/read/write/edit/web/view_image 是模型每轮高频热工具（hotTools 五占四 + IsShellTool 全热）；迁 `xbot` CLI 后每次调用起进程、经 `/v1/*` 回 kernel——jobs/cwd/freshness/IO 绑定全在 kernel 进程内，runtime handler 重造是第二份领域真源（同 jobs/subagent 判据：活态只在本实例）。且 `send_message_to_user`/`ask_user` 的 `turnSource` 分叉与卡回投 `NameApproval` 事件本就不该动（inventory §3 已明示），ToolsLink 六件同档：模型工具语义绑 kernel 状态，迁 CLI 失去 turn 内 IO 上下文。
- **B 部分留 Link**：落选。六件无一依赖外部实体——shell 是 `os/exec` 包装、文件工具是本机 FS、web_fetch/web_search 是 HTTP 客户端、view_image 是本机读+vision 判断；desktop/browser/code/connector 这些真依赖外部 node/link 的族**本就不在 ToolsLink 内**，「留 Link」没有对应残留物。
- **C ToolsLink 退役 + kernel 行注册（选用）**：六件改 kernel 行、group 名各自定（`"shell"`/`"files"`/`"web"`/`"vision"` 或并入现组），`on` 条件按各工具真实依赖——shell 行恒 `always`（`NewShells()` 探测为空即返空 slice，无外部依赖）；`web_search` 行沿用现 `webSearchOn()` 判据（`config.LoadWeb`+`HasHostedSearch` 独占，`registry.go:151-154` 的 link 侧 web_search 过滤随行迁移到 kernel 行 `on` 判据）；`view_image` 行恒 `always`（vision 判据在 `Run` 内：不能看的模型报原错，注册从不按 vision gate）；files 三行恒 `always`。注册后 `attachShell` 不再扫 link，改扫 `kernelTools` 产出的工具集（或 kernel 直接持这批工具句柄统一绑定）；`memoryEnv.shown()` 同换查找源。

ToolsLink 退役连带面：

- `internal/link/tools.go` `ToolsLink`/`NewToolsLink`/`NewToolsLinkAt` 整型删；`link.go:29` `NameTools` 常量删；`store.go:37` organs 行 `{ID:"tools"…}` 删（organ 表不再含 tools）；`mountGrantLocked` `case link.NameTools` 分支删。
- `blurbs[NameTools]`（grants.go:86）删——organ 本就不进 `linksPrefix`（env.go:70 过滤），删后能力框无变化。
- `eval/run.go:278` grants 去 `NameTools`；`jail.go:326-338` `sandboxLink` 保（评测沙箱仍用同名 link 注入夹具工具，不产 ToolsLink——`Name()="tools"` 名与 toolDoor `Name()="tools"` 本就有名撞，退役后只剩 door；评测侧的伪 link 名可保或改 `"sandbox"`，按最小 diff 定）。
- `link.go:224` `CollectTools` 保——仍有 desktop/browser/code/connector 等 link.Tools() 消费者。
- tool_index 的 `"tools"` group 消失——door `describe tools` 原可列组（registry.go:363-385 按 group 名/ResolveApp），退役后六件按新 group 名入索引；door `Name()="tools"` 不冲突（tool id 与 group id 不同命名空间，`describe tools` 现按 `names := []string{id, link.ResolveApp(id)}` 匹配 group 字段，六件离开 `"tools"` group 后 `describe tools` 报 `no tool or group "tools"`，与原 `describe <group>` 语义一致）。
- `review.Table` 六行保——工具本体不变，class/MachineTouching/shellKeys 判据不变。

## 各工具/组决策表

| 工具/组 | 现挂载 | 依赖什么 | CLI 化后注册条件 | 结论 |
|---|---|---|---|---|
| shell×6 | ToolsLink organ | `os/exec`+kernel Jobs/session/cwd/IO | kernel 行 `always`，`NewShells()` 空即空 slice | kernel 行，不迁 |
| read/write/edit_file | ToolsLink organ | 本机 FS+`BindRoot(k.cwdFor)`+freshness+selfEditGuard | kernel 行 `always` | kernel 行，不迁 |
| web_fetch | ToolsLink organ | `configureWeb` Fetcher/`Store`/`Now` | kernel 行 `always`（配置错时 `failFetch` 原链） | kernel 行，不迁 |
| web_search | ToolsLink organ | `configureWeb` Backend+`webSearchOn()` 独占 | kernel 行 `webSearchOn()` | kernel 行，不迁 |
| view_image | ToolsLink organ | `configureWeb` vision+`Allowed` roots | kernel 行 `always`（vision 判据在 `Run`：不能看的模型报 `model <id> does not support vision` 原错） | kernel 行，不迁 |
| send_message_to_user | kernel `always` | `curTurn().turnSource`/`attachOutImages`/Desktop 通知 | 不动 | 保 kernel 行 |
| ask_user | kernel `always` | `askOwner`+questions 表+`InterjectInternal`/`NameApproval` | 不动 | 保 kernel 行 |
| generate_image | kernel `always` | `imageGenCapability` proxy+`withCurrentIO` 落盘+provider 登录态 | 不动 | 保 kernel 行 |
| listen/tts | kernel `always` | `audioBackend` provider+`withCurrentIO`/`StoreVoice` | 不动 | 保 kernel 行 |
| todo | kernel `todoToolOn()` | `k.todo` 状态+`todoPolicy`+`UpdatedStep` 提醒 | 不动 | 保 kernel 行 |
| agent_*（spawn/dispatch/steer/queue/interrupt/cancel/agent_message/agent_stop） | kernel `always`+`k.Dispatch` 分叉 | `execSnapshot`/`hasLink(NameAgents)`/`agentTypes` | 不动 | 保 kernel 行 |
| desktop_*×7 | `NameDesktop` organ→`desktop.Open` | 本机 accessibility driver | 不动 | 保 organ link |
| browser_*×4 | `NameBrowser` store grant→`browser.Open` | 本机 Chrome+CDP profile | 不动 | 保 store grant link |
| code_*×6 | `NameCoding` organ→`code.Open` | 本进程 coding runner+`UseProvider`/`UseRunner`/`UseTranscript` | 不动 | 保 organ link |
| telegram_send/lark_send | connector store grant | connector+vault token+ExternalSend 草稿链 | 不动 | 保 store grant link |
| mcp_* | 批 2 已全退 | MCP Session/Manager 进程资源 | 无 model 工具 | 已退，不再注册 |

## 保留面

- 六件工具本体（`tools.Shell`/`ReadFile`/`WriteFile`/`EditFile`/`WebFetch`/`WebSearch`/`ViewImage`）与 `BindJobs`/`BindSession`/`bindShellRuntime`/`bindFileCwd`/`configureWeb`/`modelPath`/`memoryGuarded` 绑定链全保——只换「从 link 集合里找」为「从 kernel 行产出里找」。
- `scopeTools`/`subagentBase` 的子代理工具集构造不动——它本就不经 link，直接 `NewShells()`+`&tools.ReadFile{…}` 自建副本。
- hot 集语义不变：hotTools 表与 `IsShellTool` 判据按 tool `Name()` 判定，与注册源无关；六件进 kernel 行后仍全热。
- organ/store grant 装配其余链路不动：terminal/memory/persona/context/agents/skills/webhook/watch/web/peers/desktop/coding-agent organ 表与 browser/telegram/lark/store connector grant 表原样；`syncGrantsLocked` 的 organ 循环只少 tools 一行。
- `toolDoor` 不动：door 是 registry 代理，六件仍在 registry 内（换 source 从 `"organ"` 到 `"kernel"`）。

## 退役面

- `internal/link/tools.go` 整文件删（`ToolsLink`/`NewToolsLink`/`NewToolsLinkAt`/`Name()`/`Tools()`）；`link.go:29` `NameTools` 删；`link.go:223-230` `CollectTools` 保（其它 link 仍用）。
- `store.go:37` organs 表 `{ID:"tools"…}` 删；`grants.go:86` `blurbs[NameTools]` 删。
- `mountGrantLocked` `case link.NameTools` 分支（links.go:450-451）删——`work.BotDir`/`work.SharedDir` 的 import 按其它分支存活情况定去留。
- `registry.go` kernelTools 加六行（shell/files/web/vision 各组），link 循环自然不再见六件。
- `attachShell`/`attachShellJobs` 的遍历对象从 `l.Tools()` 换为 kernel 行产出的工具集（实现细节：kernel 行工厂返的 `[]tools.Tool` 里筛 `*tools.Shell`/文件/web 类型，同现码判型逻辑）。
- `kernel.go:746-757` `shown()` 的查找源从 `l.Tools()` 换为 kernel 行工具集。
- `eval/run.go:278` grants 删 `NameTools`（bot.Create 不再记该 grant）；`jail.go` `sandboxLink` 保但名改 `"sandbox"`（避免与退役后语义混淆——原 `"tools"` 名指 ToolsLink，伪 link 名改后 `linkSource`/`LookupOrgan` 判 `"kernel"` 与 organ 同结果，不引行为差）。
- 各 `NewToolsLinkAt`/`NewToolsLink` 测试夹具（web/park/approval/paste/dispatch_web/preamble/memory_snapshot/conversation_result/dispatch_test/link_test/interpreters_test/shell_catalog_regression_test 等）改注 kernel 行等价工具或直建 `&tools.ReadFile{…}` 集——**测试夹具不建伪 ToolsLink 冒注册面**，与 ADR0066「不整 longrun」同档：fixture 迁最小等价。
- `docs/atlas/ecosystem.md:26-27`「原生内置 Link」段、「按解释器执行命令」段的 ToolsLink 提法同步；`docs/atlas/kernel.md` registry/link 装配段同步；`docs/API.md` links 表 tools 行同步。

## 最窄验收

1. `internal/link/tools.go`/`NameTools`/`mountGrantLocked` tools 分支/organs tools 行/blurb 删；`syncGrantsLocked` organ 循环不再挂 tools。
2. `kernelTools` 加六行：shell 组 `always`（`NewShells()` 产出）、files 组 `always`（read/write/edit）、`web` 组 `webSearchOn()`（fetch+search 同行或 fetch `always`/search `webSearchOn()` 分行）、`vision` 组 `always`（`Run` 内 vision 判据不动）；注册后 tool_index 六件按新组名出、shells 与 hot 五件仍 exposeHot。
3. `attachShell` 遍历 kernel 行工具集完成 BindJobs/BindSession/bindShellRuntime/bindFileCwd/configureWeb；`memoryEnv.shown()` 换查找源后 memory 路径显示不变。
4. 模型 `tools` door `describe shell`/`describe files`/`describe web` 按新组名列六件；`describe tools` 报 `no tool or group "tools"`。
5. `stableLinkNames()` 不再含 `"tools"`；能力框 `linksPrefix` 本无 organ 行，无变化。
6. eval `sandboxLink` 名改 `"sandbox"` 后 `linkSource` 落 `"kernel"`、评测面不变；各 `NewToolsLinkAt` fixture 迁 kernel 行等价或直建工具集。
7. atlas ecosystem/kernel/API 三处同步；`cliProfileGuide` 不新增（六件本就是模型工具，非 CLI）。

## 接受的代价

- tool_index 的 `"tools"` group 名消失，改为 `"shell"`/`"files"`/`"web"`/`"vision"`（或并入现组名）；`describe tools` 从「列组」变 `no tool or group "tools"`——与原 links 组退役同形，模型经 `search`/`describe <新组>` 到达。
- `attachShell`/`shown()` 的查找源换为 kernel 行集——实现上 kernel 行工厂的 `[]tools.Tool` 与 link `Tools()` 同型，判型逻辑（`*tools.Shell`/`*tools.ReadFile` 等）逐字复用。
- 六件的 `source` 从 `"organ"` 变 `"kernel"`——`linkSource` 函数仍在（desktop/browser/code/connector 用），tool_index 的 source 字段按现行 JSON 原样吐 `"kernel"`。

## 决策边界

不把「ToolsLink 去掉后哪些工具还走 Link」误读为「六件里有必须留 Link 的」——六件零外部依赖，留 Link 没有对象；不把「整体退役」误扩到 desktop/browser/code/connector（它们各依赖自己的 node/grant 链路，与 tools organ 无关）；不把六件误迁 CLI（kernel 态绑定：Jobs/session/cwd/IO/freshness/selfEditGuard/vision 全在 kernel 进程内，CLI 化是第二份真源且失去 turn 语义）；不把 `mcp_*` 写成「需 mcp link 保留」（批 2 已全退成 `xbot mcp` CLI，无 model 注册面）。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。
