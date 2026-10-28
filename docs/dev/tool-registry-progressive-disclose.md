---
created: 2026-10-28
updated: 2026-10-28
status: 已实现（功能分支 feature/tool-registry-proxy）·待合入与日用
来源: 政东 2026-10-28（统一注册中心；渐进披露抄 Pi 默认代理门）；其余由主管定稿
相关: [[原生能力与Links]] [[连接应用]]
对照: [Pi MCP](https://pi.dev/docs/latest/mcp) · [pi-mcp-adapter](https://pi.dev/packages/pi-mcp-adapter)
---

# 工具注册与渐进披露

## 做完长什么样

1. **一张注册表**：本 Bot 本轮能调的每个工具只登记一次（id、来源、分组、说明、schema、expose）。
2. **发给模型的 `tools[]` 固定、短**：元工具 + 少量常热（`direct`）。中途披露 **不得** 往 `tools[]` 加减项——护前缀缓存。
3. **冷工具走一扇代理门（抄 Pi）**：常驻一个 `tools`（或保留名 `mcp` 仅对外接时可拆，产品口径统一叫代理门）工具，三态：
   - `search`：按自然语言/关键词找工具，回短列表（名 + 一行说明）
   - `describe`：按 id 回完整 description + JSON schema（包在 tool result）
   - `call`：按 id + args 执行（即 invoke）
4. **来源刀口不变**（见《原生能力与 Links》）：自己实现 → 原生登记；依赖外部 → Links 安装后再进**同一张表**，冷工具同样经这扇门。

## 政东已拍板

| 点 | 定论 |
|---|---|
| 原生 vs Links | 自己实现 = 原生；依赖外部（MCP、本机 Chrome 等）= Links |
| 注册 | 要有统一注册中心，不要多路 if 拼装 |
| 生效 vs 暴露 | 工具可已注册/可调，但不一定首轮把说明堆进上下文 |
| 模型怎么知道 | 靠短入口/清单 + 代理门 search/describe，不靠猜名字 |
| describe 与 tools[] | describe **只**把 schema/资料放进**工具结果**；**不要**再动态改 `tools[]`（前缀缓存） |
| 形态 | **直接抄 Pi 默认**：一扇代理门 search → describe → call；不抄 Pi 的 `deferred` / `directTools:"search"`（那会改 `tools[]`） |

## 主管定稿（其余）

### 对齐 Pi，不抄什么

| Pi | XBot |
|---|---|
| 默认 `mcp({ search \| describe \| tool+args })` 一扇门 | 同构：`tools({ search \| describe \| call })`（名字实现时定；语义照抄） |
| `exposure: direct` 常驻 `tools[]` | 同：`expose=hot` / direct |
| `deferred` / search 激活后**加进** `tools[]` | **不抄**（破坏前缀缓存） |
| `codemode` 脚本里 searchTools/describeTool | 本批不做；需要时另开候选 |
| 服务器懒连 + 元数据缓存 | Links/MCP：装后登记；调用时再连；元数据可落盘缓存（实现细节） |

### 注册表字段（逻辑）

每条至少：

- `id`：稳定工具名（与今日 `tools.Tool.Name()` 对齐）
- `source`：`kernel` | `organ` | `link` | `mcp:<server>`
- `group`：可选分组 id（如 `bot_profile`、`desktop`、某 MCP 名）
- `summary`：一行人话（进短清单 / search 命中）
- `schema`：完整参数 schema（只经 describe 整份给出；hot 工具可放进 `tools[]`）
- `expose`：`hot`（进固定 `tools[]`，等同 Pi `direct`）| `listed`（短清单有名，经代理门 call）| `quiet`（短清单默认不写，describe/search 仍可查；慎用）

### 固定进 `tools[]` 的集合（稳定前缀）

始终包含：

- **代理门** `tools`（或最终定名）：`search` / `describe` / `call` 三态合一（抄 Pi 的 `mcp` 代理，不拆成两个元工具）
- `send_message_to_user`（产品硬通道）
- `todo`（若该模型开启）

**常热（`expose=hot`）白名单**宜极短，建议默认：`shell`（或壳组入口）、`read_file` / `write_file` / `edit_file`、`web_fetch`。其余（电脑操作细工具、MCP 工具、peers/memory 细项等）默认 `listed`，经代理门。

> API 现实：多数供应商只允许调用出现在本轮 `tools[]` 里的名字。冷工具不进列表 ⇒ 必须经代理门的 `call`；常热例外直接调。

### 代理门契约（抄 Pi）

**search**

- 入参：`query`，可选 `limit` / `group`（namespace）
- 出参：命中列表（id + summary）；勿倾倒全表

**describe**

- 入参：`id`（或 Pi 式 selector）
- 出参：summary + 完整 schema + 可选用法一句；普通 tool result
- **副作用**：不修改本轮或后续轮的 `tools[]`

**call**

- 入参：`id` + `args`（对象；兼容 JSON 字符串若某供应商 schema 弱）
- 仅当注册表存在且本 Bot 已激活；权限/确认走现有 review
- MCP：call 时再 `tools/call`；未 describe 过也允许（鼓励先 search/describe）
- 未知 id：明确错误，可附 search 建议

### 短清单（上下文）

- 环境或系统一块：「可用工具（摘要）」——按 group 折叠，一行一个 summary（可对标 Pi 在 system 里列 `mcp_servers` 摘要）
- 只含 `hot` + `listed`；`quiet` 不出现
- Links 安装/卸载 ⇒ 注册表变 ⇒ **下一轮**短清单与可 call 集合变；元 `tools[]`（代理门+常热）定义仍尽量不变（短清单放可变段，护前缀）

### 与 Links / 器官

- Links 商店：只决定**外接是否装上**；装上后 MCP/浏览器工具**注册进中心**，冷工具经同一代理门，不另开 `mountedTools` 旁路
- 原生器官：进程内实现，启动即注册，不经 `apps.json`
- 废除：靠 `namedLink("memory")` 才 append recall 这类分散注册；改为注册表声明 + 来源模块启动时 `Register(...)`

### 明确不做（本批）

- 不在中途往 `tools[]` 动态加 MCP 全量 schema（含不抄 Pi deferred 激活）
- 不靠 id 黑名单/命令指纹区分原生与 Links
- 不把「前缀缓存」优化成「工具结果里也不给 schema」——describe 就是为给 schema
- 本批不做 codemode

## 现状与怎样算对（2026-10-28）

不再分批，一次做完（政东口径）。现状写在 [内核 · 工具注册与代理门](../atlas/kernel.md#工具注册与代理门) 和 [ADR 0035](../adr/0035-unified-tool-registry.md)。代理门的名字定为 `tools`。

能跑的检查（`go test ./internal/kernel ./internal/link ./internal/web -run …`）：

| 怎样算对 | 测试 |
|---|---|
| 每个工具只登记一次，带 source/group；原生 kernel/organ、Link、`mcp:<服务器>` 分开 | `TestRegistryListsEveryToolOnceWithSource` |
| `tools[]` 只有代理门、send、todo 和 hot（壳、文件、web_fetch），其余登记了但不声明 | `TestDeclaredToolsAreTheShortFixedSet`、`TestToolsComeFromGrantedLink` |
| search 只回短列表；describe 回完整 schema，也能启动 MCP 分组；未知 id 指向 search | `TestDoorSearchAndDescribe` |
| 一轮里多次 search/describe，经代理门 call 壳、call 没启动过的 MCP、call 内核工具，所有请求的 `tools[]` 字节相同；下一轮按 id 直接调 MCP | `TestDoorKeepsToolsBytesStableAndCalls` |
| 经代理门 call 的确认卡写的是被调工具的名字（`bot_update_profile`），不是 `tools` | `TestUpdateProfileAvatar*` |
| 主线请求从不声明 MCP schema | `TestMainLineNeverDeclaresMCPSchemas` |
| 原生不进商店、fetch 不进目录：按 `Organs()`/`StoreApps()`/`retired` 判断，不按 id 黑名单 | `TestStoreAppsCurated`、`TestConnectorCatalogEnable`、`TestAppsStoreListsCuratedCardsOnly`、`TestAppsHideRetiredServer`、`TestAppsInstallBrowserGrantsGlobally` |

性能：同一个 Bot（tools、persona、memory、timer、context、peers、web、skills），声明的工具从 38 个、22.8 KB（约 5.5k token）降到 10 个、9.2 KB（约 2.25k token），能调用的有 46 个；`tool_index` 668 字节，放在环境流里。

## 还没做的

- 用户日用验收：真实模型（DeepSeek 等）会不会主动走 search → describe → call，有没有直接按名字调 listed 工具（agent 会照样执行，但供应商可能拒收）。冒烟用例和真模型认证还没跑。
- 系统提示和规则里还有直接点名 listed 工具的话（如「调用 read_skill」「recall」），要一条条改成「经 tools 调用」，再用真模型认证一次。
- 子代理还没有代理门（会绕开类型白名单，要先做出按白名单收窄的代理门）；现在子代理仍然声明全部已加载工具。
- 设置页没有改：浏览器卡回到商店，界面上还没目视过。
- `quiet` 这一档没有实现；MCP 元数据还没落盘缓存（describe 分组时现启动服务器）。

## 实现批次（原计划，已合并成一次）

| 批 | 通过条件 |
|---|---|
| A · 注册中心 + 代理门 | 新表能列出本 Bot 工具；`tools` 代理 search/describe/call 能跑；call 能跑 shell 与一个 MCP；多轮 describe 前后元 `tools[]` 字节级稳定 |
| B · 迁移动力路径 | 壳/文件/web_fetch/send/todo 走注册；去掉一条典型 `namedLink` 旁路 |
| C · 收口旧 expand | `expand_tool_group` 改为代理门 describe/search 的别名或删除；文档/Atlas/ADR 对齐 |
| D · 商店/器官 | 与《原生能力与 Links》合一：desktop 原生注册；browser Link 安装后注册；fetch MCP 不进目录 |

## 对照回顾（摘）

| 项 | 关系 |
|---|---|
| 每轮工具 ≤10～20 | 靠固定短 `tools[]` + 短清单 + 代理门，不再靠临时黑名单 |
| 经历/资料检索两扇门 | 仍是具体工具；登记进中心，expose 可 listed |
| 连接应用只外接 | 不变；只改「装上之后如何进工具表」 |
| Pi | 默认代理门照抄；deferred 加表不抄 |

## 仓库落地

- 产品真源：本页
- 实现时补 ADR（建议 `0035-unified-tool-registry.md`）+ atlas kernel「工具」节改写；ADR 写明「对齐 Pi mcp 代理，拒绝 deferred 改 tools[]」
- 暂停中的 `feature/organ-always-on`：等本设计开编码时 rebase 或作废重开，勿直接合半吊子特殊匹配
