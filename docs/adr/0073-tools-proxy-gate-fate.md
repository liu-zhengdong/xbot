---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0073 tools 代理门最终去留：门是渐进披露机制，全保

> **Accepted 记（2026-11-02）**：按 24 shape 复核——①`toolDoor.Forward`（`registry.go:264`）`call`→`toolRegistry().find(id)`→返回 `reviewedTool` 交 agent `execTool`（`agent.go:467` 经 `tools.Forwarder` 转发，确认/审核/后台化全按被调工具走）；②`hotTools`+`IsShellTool`→`exposeHot` 直接声明进 `tools[]`，listed 集（`ask_user`/`generate_image`/`listen`/`tts`/`web_search`/`view_image`/`agents` 行/`code_*`/各 link 工具）经 `tools action=call` 到达模型，是唯一通道；③`modelTools()`（`tools.go:23-29`）注册表全量包 `reviewedTool` 交 agent，`byName` 可直调属容错旁路非主路径。零删减判保成立。

## 原能力与调用方

基线 main `2dd0cb65`（ADR0072 Accepted 已合：Event 上移 inbox、`MountLinks` 装配合一、`CollectTools`/`Messenger`/`SendPolicy`/`Deliver`/`hasLink(NameAgents)` razor 已删）。本刀是「去 links」母路线批 5 收口表（`docs/dev/cli-platform-entry.md:102`）的最后一块：「若已无冷工具消费者，删除 `tools` 代理门；否则只保留明确仍需的基础交互消费者。删除空注册组、旧提示、失效断言及无人使用的兼容路径」。

### 先纠任务背景的两处过时表述

- **「tools action=list」**：现码 door 的三动作是 `search`/`describe`/`call`（`registry.go:224` enum），无 `list`。`search` 空 query 时全词命中即事实上的列表（`searchTools` `len(words)==0 → score=1`）；`describe <group>` 列组成员。下文按现码 `search` 写。
- **「模型经 `tools action=call` 调 shell/read_file」**：不对。`modelTools()`（`tools.go:23-32`）把注册表**全部**工具包 `reviewedTool` 交给 agent；`indexTools`（`agent.go:286-304`）只按 `Undeclared()` 跳过 `tools[]` 声明，**byName 里全量可直调**。kernel 行六件基础工具（shell 各壳、`read_file`/`write_file`/`edit_file`、`web_fetch`/`web_search`、`view_image`）按 `hotTools` 或 `IsShellTool` 判 `exposeHot`——`web_search`/`view_image` 不在 hotTools 也不 IsShellTool，是 listed、经门到达；shell/read/write/edit/web_fetch 声明直调。`send_message_to_user`/`todo`/`web_fetch` 在 hotTools；`ask_user`/`generate_image`/`listen`/`tts`/`dispatch`/`spawn`/`agent_*`/`steer` 等控制动词/`code_*`/desktop/browser/connector 各 link 工具全 listed——**经 `tools action=call` 到达的唯一通道**。

### 代理门机制（`internal/kernel/registry.go:206-397`）

- `toolDoor`（`:207`）是注册表外追加的一个元工具（`modelTools` `:29` 单独 append，不经 `Register`）：`Name()="tools"`，`Parameters` 三 action。
- `search`（`Run`→`searchTools` `:298-353`）：按词命中 name+group+description，输出 `id [group] 一行摘要`，hot 工具尾部标 `(in your tool list)`，limit 默认 10 封顶 50；零命中提示读环境流 `tool_index`。
- `describe`（`describeTool` `:357-396`）：工具 id→返回 `{id,group,source,direct,description,parameters}`（`direct` 即 exposeHot）；group id（含 `link.ResolveApp` 店名解析）→列组成员 id+摘要；不识报 `no tool or group`。
- `call`（`Forward` `:264-280`）：实现 `tools.Forwarder`——按 id 查注册表，返回 `reviewedTool{Tool: e.tool, undeclared: e.expose != exposeHot}` + 内层 args；agent `execTool`（`agent.go:467-475`）把调用换成目标工具，确认卡/审核/后台化/工具名全按被调工具走。`Run` 的 `call` 分支（`:292-293`）是防御返回（Forward 已拦截，正常不到）。`search`/`describe` 在 `Run` 内直出，不经转发。
- 披露链（不改 `tools[]` 的三层）：环境流 `tool_index`（`env_replay.go:360`，`toolRegistry().index()` 只列 `exposeListed` 按组）→ `search`/`describe` 按需拿 schema（结果进 tool result，不进 tools[]）→ `call` 转发执行。`TestDoorKeepsToolsBytesStableAndCalls` 守「一轮内 describe/call 后 tools[] 字节不变」。
- `review.Table["tools"]=Readonly`（`review.go:54`）：门本身的 search/describe 是只读；call 的审核按目标工具表项走（`Forward` 返回的 `reviewedTool` 带目标 review）。

### 消费面（谁真的用这扇门）

| 消费者 | 路径 | 性质 |
|---|---|---|
| **主线模型调 listed 工具** | `modelTools`→agent `execTool`→`Forwarder.Forward` | **唯一通道**：listed 工具不在 `tools[]`，模型无法按名直调（供应商只认声明名；`byName` 直调是容错旁路不是主路径） |
| **主线模型披露冷工具 schema** | env `tool_index` → `search`/`describe` | 渐进披露本体：schema 按需进 tool result，稳定前缀不动 |
| **懒加载 link（StoreGrant）工具** | `deferred` 标记（`toolEntry.deferred`）→主线仍经门 call（`Forward` 查注册表不看 deferred）→`links load` 后 `markLoaded` 解冻 | 门是主线到 deferred 工具的通道；子代理经 `mountedTools().loaded()` 看不到 deferred |
| **子代理** | `mountedTools`→`scopeTools`/`toolsForScope` 过滤 | **不经门**：子代理工具全量声明，`subagentBase` 负面清单显式排除（continuable_subagents.go 排除 dispatch/spawn/agent_*/steer 等；`tools` 门不在负面清单但因 `mountedTools` 不含 door——door 只在 `modelTools` append——子代理天然无门） |
| **runtime/CLI/人侧** | 全仓 grep：无 CLI 经门调工具；`xbot mcp`/`links`/`subagent` 各 runtime noun 直调领域函数 | **零消费**：门是纯模型侧机制 |

### 「代理门还是基础交互」的判定

任务问「tools tool 是代理门（可删的元层）还是基础交互消费者」。按现码机制核：**它是 ADR0035 定的渐进披露机制的承载体，不是可选代理层**——listed 工具的 schema 不进 `tools[]` 是设计本体（护前缀缓存 + 工具数量无界），「经一门 search→describe→call」是 listed 工具到达模型的唯一路径。删门等价于「把所有 listed 工具改 declared」或「listed 工具不可达」：前者让 `tools[]` 随 Link 装卸变、击穿字节稳定保证（`TestDoorKeepsToolsBytesStableAndCalls`）；后者直接砍能力（`generate_image`/`listen`/`tts`/`ask_user`/全部 `agents`/`code_*`/desktop/browser/connector 工具失联）。两个方向都不是「清理冗余」。

收口表的条件「若无冷工具消费者删除」不成立：**冷工具消费者存在且是主线模型本身**——`tool_index` 里列的每一个 listed id（`agents`/`code`/`image_gen`/`audio`/`desktop`/`browser`/`telegram`/`lark`/`web_search`/`view_image`/`ask_user` 组）都是经门到达的消费者。

## 决策

**tools 代理门全保，零删减。** 逐项：

1. **门本体保**：`toolDoor`/`doorArgs`/`parseDoor`/`Forward`/`Run`/`searchTools`/`describeTool`/`toolSummary` 原样；三 action（search/describe/call）原样。
2. **注册与披露机制保**：`toolRegistry`/`toolEntry`/`expose`（listed/hot）/`deferred` 字段、`hotTools` 名单、`index()`/`loaded()`/`find()`、`modelTools` 的 reviewedTool 包装与 door append、`env_replay.go` 的 `tool_index`/`tools` 两环境字段、`fit.go:117-118` 的 tool_index 裁剪、`review.Table["tools"]=Readonly`、agent 层 `Forwarder`/`Undeclared` 接口与 `execTool` 转发段——全保。
3. **消费者面保**：listed 工具集（`ask_user`/`generate_image`/`listen`/`tts`/`web_search`/`view_image`/`agents` 行全部/`code_*`/desktop/browser/connector 各 link.Tools()）继续经门；deferred/懒加载链（`lazyUntilLoad`/`isLoaded`/`markLoaded`/`loaded()`）原样——它同时服务门（主线可 call deferred）与子代理（`loaded()` 过滤）。
4. **无空注册组**：`kernelTools` 行均按 `on` 条件活注册（批 5 ADR0071 已核全部行有实际产出）；tool_index 组名取 `e.group`，无空组残留；ToolsLink 退役后 `"tools"` group 已消失（`describe tools` 现按 group 查无→报 `no tool or group "tools"`，语义正确——tool id 与 group id 不同命名空间不撞）。
5. **无旧提示/失效断言/无人使用兼容路径可删**：grep 核——`searchTools`/`describeTool`/`Forward`/`parseDoor`/`toolSummary`/`index()` 各有生产调用点；`Run` 的 `call` 分支错误返回是 Forward 拦截后的防御兜底（一行 errors.New，不是兼容路径）；环境流 `tool_index` 被 `fit.go` 裁剪消费、`tools` 字段被 `declared()` 消费。唯一「半死」点是 `Run` 的 `call` 分支——但它是接口要求的 Run 方法兜底，不是可删兼容层，保。

## 退役面

零。本刀是决策冻结：确认门是 ADR0035 机制本体、消费者（主线模型经门调 listed 工具）真实存在且为唯一通道，无可删空组/旧提示/兼容路径。

## 保留面

- `internal/kernel/registry.go` 的 `toolDoor` 全部段（`:206-397`）、`toolRegistry`/`toolEntry`/`expose`/`deferred`/`hotTools`/`index()`/`loaded()`/`find()`/`Register`/`toolSummary` 原样。
- `internal/kernel/tools.go` `modelTools()`（reviewedTool 全量包装 + door append）、`mountedTools()` 原样。
- `internal/kernel/env_replay.go:349-360`（modelTools→declared→`tools` 字段、`tool_index` 字段）、`fit.go:117-118`（tool_index 裁剪）、`env.go:146-152`（`declared()`）原样。
- `internal/tools/tool.go` `Undeclared`/`Forwarder` 接口、`internal/agent/agent.go:294`（indexTools 跳过声明）与 `:467-475`（execTool 转发）原样。
- `internal/kernel/review.go` `reviewedTool`（含 `undeclared` 字段、`Forward` 透传 `:42-48`）、`review.Table["tools"]=Readonly` 原样。
- 懒加载链：`lazyUntilLoad`/`isLoaded`/`markLoaded`/`toolsForLoaded`/`loadedNames`/`links load` 原样——门主线 call deferred 工具与子代理 `loaded()` 过滤共用。
- 子代理负面清单独立性：`tools` 门因子代理走 `mountedTools`（不含 door append）天然不进子代理工具集，无需负面清单条目——此结构事实保。
- `TestDoorKeepsToolsBytesStableAndCalls`、`registry_test.go`/`manage_test.go`/`skills_test.go` 的 door 断言原样。

## 边界

- 不把「批 5 收口表的删除条件」读成「门可疑」——条件前提是「无冷工具消费者」，现码消费者是主线模型本体（listed 工具集全经门），前提不成立。
- 不把 `byName` 全量可查读成「门可绕过故可删」——`byName` 直调是容错旁路（供应商按 `tools[]` 名校验，未声明名多数供应商拒），主路径是门；ADR0035 后果条已写明「模型直接按名调 listed 工具 agent 照样执行」是兜底非设计。
- 不把 `Run` 的 `call` 分支（`errors.New("tools: call was not forwarded")`）当死代码删——`tools.Tool` 接口要求 `Run`；`call` 经 `Forwarder` 拦截不到 `Run`，此行是防御兜底不是兼容路径。
- 不动 `hotTools` 名单、不改 `expose` 判据、不动 listed/hot 边界——「哪些工具该热」是 ADR0035/0069 已定面，本刀只定门的存废。
- 不动 deferred/懒加载机制——它服务门与子代理两消费者，不是门的专属件。
- 不写「门将来可删」的预留——listed 机制是披露设计本体，除非推翻 ADR0035（改全量声明），门无退役路径。

## 接受的代价

- `tools` 元工具持续占 `tools[]` 一席（约十席之一）与 system 描述字节——这是 ADR0035 已计价的披露成本。
- listed 工具调用比直调多一次 `Forward` 跳转（进程内 map 查 + reviewedTool 包装，p95 <5ms IPC 约束内零网络）——同为已计价成本。
- 模型侧「调用工具」与「runtime CLI」的双入口保持（模型经门、人/CLI 经 noun）——批 4/5 已冻此二分，门是模型侧入口的承载。

## 最窄验收

1. `registry.go` `toolDoor`/`doorArgs`/`parseDoor`/`Forward`/`Run`/`searchTools`/`describeTool`/`toolSummary`/`index()`/`loaded()`/`find()`/`hotTools`/`expose`/`deferred` 全部原样（本 ADR 不改代码，验收=确认无删减）。
2. `modelTools` 仍 append `reviewedTool{Tool: toolDoor{k}}`；`indexTools` 仍按 `Undeclared()` 跳过声明；`execTool` 仍走 `Forwarder.Forward`。
3. `env_replay.go` 仍写 `tool_index`（listed 按组）与 `tools`（declared 名单）两字段；`fit.go` 仍裁剪 `tool_index`。
4. `review.Table["tools"]` 仍 Readonly；`reviewedTool` 的 `Forward` 透传与 `undeclared` 字段原样。
5. atlas `kernel.md` 「工具注册与代理门」段（:188-196）描述与现码一致——本刀无 atlas 同步面（现状不变，仅需确认段落在 ADR0072 合入后仍准确）。
6. `docs/dev/cli-platform-entry.md:102` 收口表「tools 代理门」行标完成——条件是「无冷工具消费者」，结论转为「消费者=主线模型，门全保」。

## 决策复核备忘

- 门的存废判据是「listed 机制是不是设计本体」，不是「门有没有代理语义」——它确有代理语义（一个工具代理所有注册工具），但代理语义是渐进披露的实现形，披露本身是 ADR0035 拍板的产品决定（护前缀缓存、工具数量无界、schema 按需）。批 5 表的「代理门」措辞指「可选中间层」，核后确认它不是可选层。
- 与 ADR0069（ToolsLink 退役）的关系：ToolsLink 是「工具的注册来源」（organ 挂载路径），门是「工具的披露通道」——六件基础工具从 ToolsLink organ 迁 kernel 行只换注册行，披露路径（hot 直调 / listed 经门）不变；`"tools"` group 消失后 `describe tools` 语义正确落空，无残留。
- 与 ADR0072 决策 4（`hasLink(NameAgents)` 死条件）的关系：那刀删的是「agents organ 存在判」死条件，与本刀「agents 组工具经门到达」不同面——`agents` kernel 行的 listed 工具仍经门，organ 存在判只是不再参与 dispatching 判据。
- 唯一观察：`Run` 的 `call` 分支兜底文案 `tools: call was not forwarded` 在生产不可达（Forward 拦截全部合法 call，非法 call 在 Forward 报错）——留作接口兜底，不删不改。

## 开发节（dev）

**形状**：零代码改动。本 ADR 是「去 links」最后一块的决策冻结——docs only。

**顺序**：①本 ADR Proposed 落盘 → ②SINGLE ACTUAL CONCRETE SHAPE PASS 复核 → ③Accepted 后 `cli-platform-entry.md:102` 收口表标完成 → ④atlas kernel.md 代理门段核对（无需改，仅 verified 刷新）。

**还没做的**：无。本刀收口「去 links」母路线批 5 表最后一行；若未来推翻 ADR0035（改全量声明/换披露机制），门的退役随该新 ADR 定，不在本刀预留。
