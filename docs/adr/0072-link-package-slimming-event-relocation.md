---
kind: adr
guard: owner
status: Proposed
date: 2026-11-02
---
# 0072 Link 包瘦身 + Event 上移 + Link 装配合一

## 原能力与调用方

基线 main `94af6e62`（批 4 ADR0070 与批 5 ADR0071 已 Accepted 合入；模型工具面已冻结——`internal/link/tools.go` 与 `NameTools` organ 已随 ADR0069 退役，剩余模型写类工具按 ADR0070/0071 全部保 kernel/link 侧、零新 CLI 面）。本刀是「去 links」母路线（`docs/dev/cli-platform-entry.md` 批 4「出入站解耦」与收尾项「事件结构与投递摆脱 Link 包依赖，删除 Link 装配与旧入口」）的最后一块：**link 包本体的形状**——批 4/5 冻的是「模型工具面不迁 CLI」，本刀定的是「link 包作为 Go 包如何瘦身、Event 移去哪、装配路径怎么合一」。

### link 包现状（4263 行，19 个非测试文件）

| 文件 | 行数 | 角色 | 外部消费者 |
|---|---|---|---|
| `link.go` | 229 | `Link`/`Emitter`/`Messenger`/`SendPolicy`/`ErrNotSent`/`Event`/`NewEvent`/`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent`/`Event.Validate`/`OwnerTrusted`/`Render`/`Wrap`/`UntrustedEventPrefix`/`newID`/`CollectTools` + `Name*`/`Type*`/`Kind*`/`SourceSubagent` 常量 | Event 面非测试消费者遍布 9 个目录 40 个文件（kernel 15、remote 3、inbound 3、web 2、proc 2、bot 1、code 1、eval 1、cmd 1）；Wrap/Render/OwnerTrusted 消费 kernel 4 + tools 1 + remote 1 + peer 1 |
| `timer.go` | 582 | `Routine` 数据模型 + `Timer`（`Name`/`Tools`/`Run`(Emitter)/`Due`/`Fire`/`fire`/`Deliver`/`Expire`/`Commit`/`List`/`listRaw`/`Load`/`read`/`Update`/`routineEvent`/`RoutineBody`/zone index） | `link.Routine`/`link.Timer` 被 proc/web/serve/bot/cmd/kernel 15 个文件 49 处引用 |
| `connector.go` | 558 | `Connector`（telegram/lark organ，`sendTool` `telegram_send`/`lark_send`）+ draft 文件锁读写 + `PostTelegram`/`PostLark` HTTP egress + secret 域绑定 | kernel mount 分支、cmd_serve mount、`inbound/connector.go` 事件生产 |
| `catalog.go` | 99 | `CatalogEntry`/`Catalog`/`Lookup`/`GrantName` + 内嵌目录（telegram/lark/mcp/browser 四行） | kernel `linksTool.search`/`install`/`load`、`installedSection`、CLI links 族 |
| `connectors.go` | 130 | `connectors.json` 内嵌 + `CatalogItem`/`Connectors`/`RetiredServer`/`LookupConnector`/`EnableConnector`/`SecretLookup`/`catalogServer` | kernel `linksTool`、`storeConnector`、web apps、`mcp` 装配 |
| `store.go` | 136 | `App`/`organs` 表（13 行）/`Organs`/`LookupOrgan`/`StoreApps`/`StoreGrant`/`LookupApp`/`ResolveApp` | kernel `syncGrantsLocked`/`catalogRuntime`/`linkSource`/`installedSection`、cmd_serve mount、`ResolveApp` 被 CLI links/connector 安装路径用 |
| `grants.go` | 102 | `Memory`/`Persona`/`Context`/`Agents`/`Skills`/`Runner`/`Webhook`/`Watch`/`Web`/`Peers` 10 个零工具 organ struct + `Blurb`/`blurbs` | kernel `mountGrantLocked` 10 个 case 返回这些 struct、`remote()`/`mountGrantLocked` 用 `link.Runner` |
| `secrets.go` | 106 | `secretFile`/`WriteSecrets`/`DropSecrets`/`ReadSecrets`/`readSecrets`/`writeSecrets` legacy plaintext secrets.json 文件操作 | `inbound/connector.go:120`、`Connector.token()`、`SecretLookup` |
| `global_secrets.go` | 57 | `SetProductSecrets`/`RemoveProductSecrets`/`GlobalSecretLookup`（connector token + MCP env 写全局 product backend） | kernel `storeSecrets`/`dropSecrets`/`search` 的 GlobalSecretLookup |
| `vaultmigrate.go` | 146 | `CredDomains`/`VaultName`/`KindForSecret`/`SetConnectorSecret`/`RemoveConnectorSecret`/`Placeholder`/`MigrateConnectorSecrets`/`pruneEmptySecretFile` | `web/apps.go:465,470`、`inbound/connector.go:111`、`Connector.UseVault` |
| `installed.go` | 57 | `InstalledCap`/`InstalledRef`/`InstalledSection`/`renderInstalled`/`oneLine` 能力框 connector 段渲染 | kernel `installedSection` |
| `routine_migrate.go` | 148 | `MigrateRoutines` + `RoutineMigration`（C1 zone 迁移工具） | `cmd/xbot/routine.go:72`、`cmd_serve.go:894` |
| `terminal.go` | 25 | `Terminal`（`Name`/`Tools` nil）+ `Account()` | kernel mount 分支、eval fixture、`bot/primary.go` 等 |
| `link_test.go` 等 17 测试文件 | ~1450 | catalog/connector/installed/interpreters/link/routine_body/routine_migrate/routine_skills/shell_catalog/store/terminal/timer×6/vaultmigrate | — |

### Link 装配现状（四路）

| 路径 | 位置 | 角色 |
|---|---|---|
| `mountGrantLocked` | `internal/kernel/links.go:444-514` | 17 分支大 switch：13 organ struct 直返 + telegram/lark `k.connector()` + desktop `desktop.Open` + browser `browser.Open` + coding `code.Open`+四绑（`bindCurrentPaths`/`boundProvider`/`UseRunner`/`UseTranscript`/`UseCommand`） |
| `syncGrantsLocked` | `internal/kernel/links.go:342-387` | 每 `snapshotLinks()` 同步 `Bot.EffectiveLinks()` grant 的 mount/drop：`catalogRuntime(name)` 过滤、`have` 去重、organ 循环无条件挂 13 个 |
| `snapshotLinks` | `internal/kernel/links.go:328-340` | `linksMu` 锁 + `syncGrantsLocked` + 返回 `k.Links` 拷贝；15 个调用点（`attachShell`、`toolRegistry`、`namedLink`、`linkNames`、`toolsForLoaded`、`StartEmitters`、`memoryEnv`、`person_env` 等） |
| `MountOrgans` | `internal/kernel/links.go:391-394` | 薄封装：new 一个装配-only `&Kernel{Bot, Links}` 调 `snapshotLinks()`；唯一调用 `cmd/xbot/cmd_serve.go:265` |

`cmd/xbot/cmd_serve.go:214-266` `mount()` 是第四条：遍历 `EffectiveLinks()` 跳过 organ、按名 switch 建 connector/`Runner`/`browser.Open`/legacy node 兜底，然后调 `kernel.MountOrgans` 补 organ——与 `mountGrantLocked` 是同一份按名分派逻辑写两遍（connector 重复一次、`Runner` 只在这里、organ 靠 `LookupOrgan` 跳过）。

### link.Event 现状

`link.Event`（`link.go:105-126`）是 inbox 的 wire type：`inbox.Inbox` 的 `pending []link.Event`、`record.Event *link.Event` JSON、`Post`/`Pending`/`Head`/`Ack`/`Wait` 全链。生产者侧 `link.NewEvent`/`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent` + `SourceSubagent`；消费者侧 `Event.Validate`/`OwnerTrusted`/`Render`/`Wrap`/`UntrustedEventPrefix`。`kernel.Deliver`（`kernel.go:863`）用 `link.Messenger` 接口 + `link.SendPolicy`，`kernel.SendPolicy` 字段类型是 `link.SendPolicy`——`Messenger` 唯一调用点是 `k.Deliver` 自身 + kernel_test（无生产实现，`sendTool` 不走 `Messenger`）。`link.Emitter` 唯一生产实现是 `*link.Timer`，消费点 `kernel.StartEmitters:817`。`link.CollectTools` 无生产消费者（ADR0069 退役面已注明「保」但全仓 grep 零非 link/零非 test 引用）。

### 代理门现状（决策点 4 输入）

`Kernel.Dispatch` 二分（`registry.go:135-144`）：`k.Dispatch` 为真时 `agents` kernel 行产 `dispatch`+`agent_message`+`agent_stop`，为假时产 `spawn`+`steer`+`queue`+`interrupt`+`cancel`——批 5 ADR0071 已冻「两套不同时出现、全保 kernel 行」。`dispatching()`（`dispatch.go:40`）的判据是 `k != nil && k.Dispatch && k.hasLink(link.NameAgents)`——`hasLink` 查 `snapshotLinks()` 里 `NameAgents` 的存在；`NameAgents` 是 organ，由 `syncGrantsLocked` 的 organ 循环**无条件挂载**（不读 `EffectiveLinks`）。即 `hasLink(NameAgents)` 在现行装配下恒真，判据实际等价于 `k.Dispatch`——「agents link 存在判」已是死条件。

## 决策

本刀定 link 包本体形状，逐决策点：

### 决策 1：link 包瘦身——连接器/legacy 各文件去留

**connector 目录与 legacy 迁移文件全留 link 包原位**；只删确认死代码。理由：

- `connector.go`（558 行 `Connector`+draft+HTTP egress）是 telegram/lark organ 的实现本体——它不只是「目录」，是生产 `sendTool` 的工具本体与 `UseVault`/`token()`/`client()` 的凭据装配链。瘦它的前提是 connector 改由别的包装配，那是决策 5（link 抽象退役深度）的事，本刀不预先拆。
- `secrets.go`/`global_secrets.go`/`vaultmigrate.go`（共 309 行）是凭据存取与迁移：legacy `secrets.json` 文件操作（`WriteSecrets`/`DropSecrets`/`ReadSecrets`）与 vault 迁移（`MigrateConnectorSecrets`/`SetConnectorSecret`/`Placeholder`）是活跃功能——`Connector.UseVault` 的 first-access 迁移、`web/apps.go` 的填 key、`inbound/connector.go` 的 vault 回退、`kernel.storeSecrets`/`dropSecrets` 全在用。不是可删的 legacy，是 connector 凭据面。
- `routine_migrate.go`（148 行 `MigrateRoutines`）是 `xbot routine --migrate` 与 serve 启动的 zone 迁移工具——活跃 CLI 功能，绑 `Timer`/`Routine`/`cron`。留。
- `catalog.go`/`connectors.go`/`store.go`/`installed.go`（共 422 行）是连接应用目录与能力框渲染——`links` CLI 与 `/api/apps` 的真源。留。
- `timer.go`/`grants.go`/`terminal.go` 是 organ struct 与 Timer 数据模型——决策 5 定它们随不随抽象走。

**唯一确认可删**：`link.CollectTools`（`link.go:222-229`）——ADR0069 保留面写「仍有 desktop/browser/code/connector 等 link.Tools() 消费者」有误：该函数全仓零非 link/零非 test 引用（`toolRegistry` 直接遍历 `l.Tools()` 不经 `CollectTools`）。本刀把它列入退役面（连同其单测），是「找不到成立理由就删」的 razor 动作，不改动任何调用方。

**Event 结构不留在 link 包**——见决策 2。

### 决策 2：Event 上移——移到 `internal/inbox`，不进 kernel

`Event`/`NewEvent`/`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent`/`Validate`/`OwnerTrusted`/`Render`/`Wrap`/`UntrustedEventPrefix`/`newID` + `TypeMessage`/`TypeTimer`/`Kind*`/`SourceSubagent`/`RoutineSkills` 字段，全部上移 `internal/inbox`（新文件 `internal/inbox/event.go`）。

**选 inbox 不选 kernel 不选独立 `internal/event`**：

- **依赖方向正确**：`inbox` 是当前唯一必须 import link 只为 `Event` 型的包（`inbox.go` 全文件 `link.Event` 是 wire type）。Event 是「inbox 里那条记录的类型」——它的自然归属是 inbox 包，Event 上移后 `inbox` 不再 import link，link 包也不再被 inbox 反向依赖，两包分层清晰。
- **kernel 不合适**：kernel import inbox 消费 Event；把 Event 放 kernel 则 inbox 反过来 import kernel（inbox 当前不依赖 kernel，依赖方向会倒）。
- **独立 `internal/event` 不必要**：Event 只有一个真正存储宿主（inbox），独立包是给「多宿主共享类型」的解法；Event 的消费者（kernel/inbound/remote/proc/web/code/peer/eval）都是「生产后 Post 进 inbox」或「从 inbox 读出消费」，inbox 是唯一公共依赖点。独立包多一层包边界无收益。
- **import 环**：`Event.Wrap` 用 `tools.NeutralizeUntrusted`——`internal/tools` 不 import inbox/link，无环。inbox 上移后 `import tools` 是新边（inbox 当前不 import tools），单向、无环。

**`link.Event` 在 link 包侧不留别名/不 shim**：按母路线「摆脱 Link 包依赖」的直接读法——全部 40 个非测试消费者 + ~105 个测试文件的 `link.Event`/`link.NewEvent` 等引用点改为 `inbox.Event`/`inbox.NewEvent`。这是机械换 import，类型字段与 JSON tag 逐字不变。

### 决策 3：Link 装配合一——`MountOrgans` 退、`cmd_serve mount()` 并一路

**`MountOrgans` 退**：它只是「new 装配-only Kernel + snapshotLinks」两行，唯一调用点 `cmd_serve mount()`。合一后由 `mount()` 直接 new Kernel 调 `snapshotLinks()`，或——更干净——`snapshotLinks` 的 organ 挂载能力本就在 `syncGrantsLocked` 里，`MountOrgans` 不提供任何 `mountGrantLocked`/`snapshotLinks` 之外的功能。**删 `MountOrgans` 导出函数**，`mount()` 内联 `(&Kernel{Bot: b, Links: out}).snapshotLinks()` 等价两行（snapshotLinks 是私有的，同一包内不可调——`cmd_serve` 在 cmd 包不在 kernel 包；故保留一个导出薄封装或把 organ 挂载提为导出函数，见下）。

**`cmd_serve mount()` 与 `mountGrantLocked` 合一**：两条按名分派并不同源——

- `mount()` 多出的：`NameRunner`（env/参数装配 `link.Runner`）、legacy `node` grant 静默跳过、`work.Open` 校验、connector `NewConnector` 不带 vault（`attachConnectors` 后续补）、`browser.Open`。
- `mountGrantLocked` 多出的：13 organ struct、`timer` 的 `bindCurrentPaths`+Sender 装配、`code` 的四绑、`connector` 的 `k.Vault` 首访迁移。

**合一形状**：`mount()` 保持遍历 `EffectiveLinks()` 的入口，把 `NameRunner`/legacy 各自 case 留下的同时，connector/browser/organ 不再靠「mount() 分派一遍 + MountOrgans 补一遍」——改为：**`mount()` 只建 kernel 无关的 `Runner`，然后调一个导出函数完成 organ + grant + connector + browser 的统一装配**。最窄合一 = `MountOrgans` 语义扩展为「唯一 link 装配入口」：`MountLinks(b *bot.Bot, runner link.Runner, backend vault.Backend) []link.Link`——内部做 `syncGrantsLocked` 的 grant 同步 + organ 挂载 + connector 建 + vault 首访绑定，让 `cmd_serve mount()` 不再自己分派 connector/browser（两者由 `mountGrantLocked` 统一建），`mount()` 只负责 `Runner` env 解析与 legacy 兜底这两个 kernel 无关分支。vault 参数承载原 `attachConnectors` 的「补 vault」职责——合一函数建 connector 后即调 `UseVault`，与现行 `attachConnectors` 同序、同重试语义。

**`snapshotLinks`/`syncGrantsLocked`/`mountGrantLocked` 本体不动**：它们是「每轮同步 grant→link 的活快照」，合一后仍是唯一同步点；合一的是「谁来调用同步」与「外部 link 怎么进 k.Links」——不是把 17 分支 switch 拆掉。

### 决策 4：代理门最终去留——`hasLink(NameAgents)` 改 `k.Dispatch` 直判

`dispatching()`（`dispatch.go:40`）的 `k.hasLink(link.NameAgents)` 是死条件：`NameAgents` organ 由 `syncGrantsLocked` organ 循环无条件挂，`hasLink` 恒真。退役 ToolsLink 后 `agents` link 确实仍在（它是 organ struct `link.Agents{}`），但它的「存在」不再携带任何信息——organ 不看 `EffectiveLinks`，恒挂。

**`dispatching()` 改判 `k != nil && k.Dispatch`**：删 `hasLink(link.NameAgents)` 半条件，`hasLink`/`namedLink` 只剩 `NameMemory`（`kernel.go:630` rules 判据）一处消费——`hasLink` 函数保（memory 用），`NameAgents` 消费点删。`Agents` organ struct 保（`mountGrantLocked` 仍返回它、`blurbs[NameAgents]` 保——`stableLinkNames` 含 `agents` 名入提示词 Links 段）。

### 决策 5：link 包退役深度——`link.Link` 接口与 organ struct 去留

**`link.Link` interface、`link.Emitter`、`link.Messenger` 本刀不退**：`link.Link` 仍是 `Kernel.Links` 字段类型与 `snapshotLinks()` 返回类型，desktop/browser/code/connector/timer/10 organ struct 全实现它。要退 `Link` interface 等于把 `Kernel.Links` 改成「具体类型 slice + Name() 接口」——这需要把 14 个 mount case 的产物收成统一类型，或让 `snapshotLinks` 返回 `[]Named`（更弱的 interface）。**这刀不做**——理由：

- `link.Link{Name(), Tools()}` 是最小接口，desktop/browser/code/connector 四类 `l.Tools()` 在 `toolRegistry`/`toolsForLoaded` 真实消费——接口有真实多态点，不是死抽象。
- `link.Emitter` 唯一实现 `*Timer.Run`，但 `StartEmitters`/`proc.DeliverDue`/`serve.Timers` 全按 interface 断言——`Timer` 可退 `Emitter` 改由 proc 直接调 `Run`/`fire`/`Deliver`（Timer 已是具体类型 `*link.Timer`，消费点都判型断言）。**这是决策 5 的候选收窄：`Emitter` interface 退、`Timer` 保具体类型**——`StartEmitters` 的 `l.(link.Emitter)` 断言改判 `*link.Timer`；`link.Link` 的 `Run` 方法本就不在 `Link` interface 上（在 `Emitter`），退 `Emitter` 后 `Timer.Run` 变普通方法。**但这只是省一个接口定义，消费点判型不变——窄收益，本刀不做，记入「还没做的」。**
- `link.Messenger`/`link.SendPolicy`/`ErrNotSent`/`k.Deliver`：`Messenger` 零生产实现、`k.Deliver` 零生产调用方（仅 `kernel_test.go:363-379` 与 `sendTool` 的 Confirm/Run 不经它）——`Messenger`/`SendPolicy`/`Deliver` 整链是「旧 ExternalSend 草稿链」残留，`sendTool` 的实际确认走 `review` 表+工具 `Confirm`。这是**真死代码候选**：`Messenger` interface + `SendPolicy` 字段 + `Kernel.Deliver` + `ErrNotSent` 删，连同 `kernel_test.go` 对应断言。**本刀列入退役面**——它是 razor 直删（无生产实现/无生产调用），与 `CollectTools` 同档。

**connector/desktop/browser/coding link 不改直接 kernel 装配**：`mountGrantLocked` 的 `desktop.Open`/`browser.Open`/`code.Open`/`k.connector` 各返回具体类型 `*desktop.Link`/`*browser.Link`/`*code.Link`/`*link.Connector`——它们实现 `link.Link` 但 kernel 只按 interface 持有。改「直接 kernel 装配不经 Link interface」= 让 `k.Links` 存具体类型 union——Go 无 union，等价做法是各 link 类型提为 kernel 字段或经 `Tools()`-only 更窄接口。**本刀不窄化**：`link.Link` 的 `Tools()` 是真实多态点，`Name()` 是 `namedLink`/`linkNames`/`linkSource` 判据——接口留着比拆掉简单。

## 退役面

- `link.Event`/`NewEvent`/`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent`/`Validate`/`OwnerTrusted`/`Render`/`Wrap`/`UntrustedEventPrefix`/`newID`/`TypeMessage`/`TypeTimer`/`Kind*`/`SourceSubagent` + Event source 名常量（`NameTerminal`/`NameTimer`/`NameWeb`/`NameRemoteOwner`/`NameApproval`/`NameCoding`）整组上移 `internal/inbox`（新 `event.go`），`link.go` 对应段删。
- `link.CollectTools`（`link.go:222-229`）删——零消费者 razor。
- `link.Messenger` interface、`link.SendPolicy`、`link.ErrNotSent`、`errNotSent`、`Kernel.Deliver`（`kernel.go:863-881`）、`Kernel.SendPolicy` 字段（`kernel.go:77`）删——零生产实现/零生产调用 razor。
- `Kernel.hasLink(link.NameAgents)` 半条件删（`dispatch.go:40` 改 `k != nil && k.Dispatch`）；`namedLink`/`hasLink` 函数保（memory 消费点）。
- `MountOrgans`（`links.go:391-394`）退——`cmd_serve mount()` 改为调合一的 `MountLinks`（或等价导出装配函数），`mount()` 不再自己分派 connector/browser/organ。
- `cmd/xbot/cmd_serve.go:226-228`（`NameTelegram`/`NameLark` `NewConnector` case）与 `:246-247`（`NameBrowser` `browser.Open` case）删——connector/browser 由 `mountGrantLocked` 统一建；`mount()` 只留 `NameRunner`/legacy/seen 去重/`work.Open`/`MountLinks`。
- `attachConnectors`（`cmd_serve.go:197-212`）退——**注意时序**：`mount()` 在 `k.Vault` 装配之前跑（`openKernel` `mount→attachConnectors→Kernel{Vault: store}`），所以 `mountGrantLocked` 的 `k.connector()` `UseVault` 分支**在首次装配时 `k.Vault` 为 nil、不触发**——首访迁移实际由 `attachConnectors` 对裸建 connector 补做。合一后「补 vault」职责并入 `MountLinks`：合一函数签名带 `vault.Backend`，建 `*link.Connector` 后即调 `UseVault`，与现行 `attachConnectors` 同序、同「失败留 legacy、下次重试」语义——不新增第三处 vault 绑定路径。

## 保留面

- `link.Link`/`link.Emitter`/`link.Timer`/`link.Connector`/10 organ struct/`link.Runner`/`kernel.Links []link.Link`/`snapshotLinks`/`syncGrantsLocked`/`mountGrantLocked` 17 分支 switch 主体——接口与 organ 装配主体不动。
- `link.Routine`/`RoutineBody`/`MigrateRoutines`/`RoutineMigration`/`timer.go` 数据模型全保。
- `catalog.go`/`connectors.go`/`store.go`/`installed.go`/`grants.go` 的 `blurbs`/`Organs`/`StoreApps`/`StoreGrant`/`ResolveApp` 目录与能力框全保。
- `secrets.go`/`global_secrets.go`/`vaultmigrate.go` 凭据面全保。
- `Name*` 常量按「是 Event 的 `Source` 值 vs 是挂载的 link/organ 名」分两组，**无 alias 无双向 import**——inbox 持 Event 与「Event source 名」常量，link 持「挂载名」常量，二者重叠的按 Event source 归 inbox、link 消费者改读 `inbox` 常量（link→inbox 单向合法，inbox 不 import link）：
  - **上移 inbox**（Event `Source` 值 + `SourceSubagent`）：`NameTerminal`/`NameTimer`/`NameWeb`/`NameRemoteOwner`/`NameApproval`/`NameCoding`——这些是 `NewEvent`/`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent`/timer routineEvent/`code.Link.post`/approval 回投的 `Source` 实参；`link.go` 内 `Timer.Name()`/`terminal.go`/`grants.go` 对应 organ `Name()` 改读 `inbox.*`（organ 名与 Event source 同值的归 inbox）。
  - **留 link**（纯挂载/connector 名，不作 Event source）：`NameTelegram`/`NameLark`/`NameDesktop`/`NameBrowser`/`NameMemory`/`NamePersona`/`NameContext`/`NameAgents`/`NameSkills`/`NameRunner`/`NameWebhook`/`NameWatch`/`NamePeers`——`mountGrantLocked` case、`storeConnector`/`SecretName`/`CredDomains`/`VaultName`/`blurbs` 判据、`remote()` 全在 link/connector 凭据面，不产 Event source 值（telegram/lark 的 `Source` 用 `inbound` 侧 `link.NewEvent(kind, …)` 的 `kind` 实参、`NameWebhook`/`NameWatch` 的 Event source 是 `"webhook:"+id`/`"watch:"+id` 前缀非整名）——留 link。
  - `inbox.NewEvent` 参数是裸 `string`：调用方传 `link.NameTelegram`/`link.NameLark` 等 link 常量值（kernel/inbound 已 import link）或 `inbox.NameTerminal` 等 inbox 常量——`inbox` 对 link 零依赖；`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent` helper 内部用 inbox 自己的 source 名常量。

- `link.SecretName`/`link.ReadSecrets`/`link.VaultName`/`link.Placeholder` 等凭据/connector 名常量全保在 link。
- `inbox.Inbox` 的 `Post`/`Pending`/`Head`/`Ack`/`Wait`/`record` 结构与 append-only JSONL 行为不变——只是 `link.Event` 类型名改 `inbox.Event`，字段与 tag 逐字同。
- `wakers`/`postCurrentEvent`/`forwardExec`/`InterjectInternal`/`questions` 回投链/`code.Link.post`/`proc.DeliverDue`/`serve.Timers` 的 Event 生产/唤醒全链不变——只换类型名。

## 边界

- 不把「Event 上移」读成「link 包消失」——link 包仍是 connector/catalog/store/secrets/organ struct/Timer 数据模型/Link interface 的家；退役的是「Event 结构与 Link 抽象同包」这个历史耦合。
- 不把「Messenger/SendPolicy/Deliver 删」扩到 `sendTool`/`saveDraft`/`review` 链——`sendTool` 的 `Confirm`/`Run` 是 telegram/lark 草稿卡的活跃链，不走 `Messenger`/`k.Deliver`；删的是未被使用的 `Messenger` interface 与 `Kernel.Deliver` 包装。
- 不把「装配合一」读成「拆掉 mountGrantLocked switch」——合一的是 cmd_serve `mount()` 与 organ/grant 装配的入口分叉，17 分支 switch 本体是单一真源保。
- 不把 `hasLink(NameAgents)` 删读成「agents organ 退」——`link.Agents{}` struct、organ 循环挂载、`blurbs[NameAgents]` 全保；删的只是「存在判据」这个死条件。
- 不把 Event source 名常量分两组写成「新 alias 层」——`NameTimer` 等在 link 包删除，消费者改 `inbox.NameTimer`；link 包不保留 alias 转发（避免 link↔inbox 双向 import 或假 shim）。
- `Timer.Run`/`Emitter` 收窄、link 名常量进一步瘦身、`link.Link` 改更窄接口留作「还没做的」——本刀是「Event 上移 + 装配合一 + razor 死代码」三件的决策冻结，实现切片按合同另开。

## 接受的代价

- ~146 个文件（含测试 ~105）的 `link.Event`→`inbox.Event`/`link.NewEvent`→`inbox.NewEvent` 机械换——大 diff、零语义变化；分文件提交或一次换由实现定。
- `internal/inbox` 新增 `import tools`（Event.Wrap 的 NeutralizeUntrusted）与 Event source 名常量——inbox 从「纯队列」变成「队列 + Event 型」家，包职责变重但方向正确（Event 本就是 inbox 记录）。
- `Kernel.Deliver`/`SendPolicy`/`Messenger` 删后，旧 ExternalSend 草稿链的「kernel 侧草稿确认」抽象消失——`sendTool` 的草稿确认走 `review` 工具 Confirm 链已是现行实际路径，删除的是未接线的并行抽象。
- `cmd_serve mount()` 合一后 `attachConnectors` 消失——首访 vault 迁移并入 `MountLinks`，签名带 `vault.Backend`；`UseVault` 失败留 legacy 文件、下次 `syncGrantsLocked` 重挂重试的语义保，不因合一多一次或少一次绑定。

## 最窄验收

1. `internal/inbox/event.go` 含 `Event`/`NewEvent`/`TerminalEvent`/`WebEvent`/`RemoteOwnerEvent`/`Validate`/`OwnerTrusted`/`Render`/`Wrap`/`UntrustedEventPrefix` + `TypeMessage`/`TypeTimer`/`Kind*`/`SourceSubagent` + Event source 名常量（terminal/timer/web/remote-owner/approval/coding）；`internal/link/link.go` 这些段删。
2. `inbox.Inbox` 全链 `link.Event`→`inbox.Event`，inbox 不 import link；全仓非 link 文件 `link.Event`/`link.NewEvent`/`link.*Event`/`link.OwnerTrusted`/`link.Wrap`/`link.Render` 引用清零（grep 验证），改 `inbox.*`。
3. `link.go` 剩 `Link`/`Emitter` interface 与留 link 的 `Name*`（telegram/lark/desktop/browser/memory/persona/context/agents/skills/runner/webhook/watch/peers）常量；Event 段、`SendPolicy`/`ErrNotSent`/`Messenger`/`CollectTools`、上移 inbox 的 source 名常量（terminal/timer/web/remote-owner/approval/coding/`SourceSubagent`/`Type*`/`Kind*`）全删——`link.go` 减到常量 + `Link`/`Emitter` 两 interface 约 90 行。
4. `kernel.go:863` `Deliver`/`kernel.go:77` `SendPolicy`/`kernel_test.go:363-379` 删；`dispatch.go:40` 改 `k != nil && k.Dispatch`；`kernel.go:630` `hasLink(NameMemory)` 保。
5. `cmd_serve.go` `mount()` 去 connector/browser case + `attachConnectors` 删 + `MountOrgans` 改 `MountLinks(b, runner, backend)`（或等价合一导出）；`links.go:391` `MountOrgans` 删——合一函数内部 `syncGrantsLocked` 走 `mountGrantLocked` 统一建 organ/connector/browser/timer/coding，connector `UseVault` 在合一函数内做、与 `attachConnectors` 同序同语义。
6. `CollectTools` 删后 `go build ./...` 与受影响包测试过——Event 上移是全仓机械换，按 `docs/TESTING.md` 最小有效范围（link/inbox/kernel/inbound/remote/proc/web/code/peer/eval/cmd 包 build + 相关包测试），不整 longrun/真模型。
7. atlas `kernel.md` Link/organ/装配段、`ecosystem.md` connector 段同步 `link.Event`→`inbox.Event` 与装配入口描述；`docs/dev/native-vs-links.md` 现状段刷新。

## 决策边界备忘

- Event 上移是本刀唯一「跨包搬家」；其余是 razor（CollectTools/Messenger/SendPolicy/Deliver/hasLink(agents)/MountOrgans/mount() connector 分派）与常量归位。
- 批 4/5 冻的「模型工具面」与本刀「link 包面」正交——本刀不新增/不退役任何模型工具，`toolRegistry`/`kernelTools` 行不动；`linkSource`/`lazyUntilLoad`/`stableLinkNames` 行为不变。
- 实现切片若发现 `NameTimer`/`NameCoding` 上移让 `Timer.Name()`/`code.Link.Name()` 需 `import inbox`——确认无环（link→inbox 单向）即可；`inbox` 不 import link。

## 开发节（dev）

**形状**：inbox 增 `event.go`（Event+helper+source 名常量）；link.go 删 Event/Messenger/SendPolicy/CollectTools/被上移常量；dispatch.go 改判；links.go 删 MountOrgans/改 MountLinks；cmd_serve.go mount() 去 connector/browser/attachConnectors。

**顺序**：①inbox event.go 落（类型+常量+NeutralizeUntrusted import）；②link.go 删 Event 段+CollectTools+Messenger/SendPolicy/ErrNotSent；③全仓 `link.Event`→`inbox.Event` 机械换（含 inbox 自身）；④kernel Deliver/SendPolicy/hasLink(agents) 删；⑤links.go MountLinks + cmd_serve mount() 合一；⑥atlas/dev 同步。

**还没做的**：`Emitter` interface 收窄（`Timer.Run` 直调化）、`link.Link` 更窄接口、`link` 包内 organ struct 是否随「退役 Link 抽象」整体归位（desktop/browser/code 各包自持 organ struct 或 kernel 直装）、`sendTool`/`Connector` 是否随 Event source 名上移后解 link 依赖——均留后续切片，本刀只定决策不开合同。
