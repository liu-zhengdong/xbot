# 工具组渐进披露（expand）

真源：Obsidian `候选/工具渐进披露.md`（主管方案 2026-10-27）。

## 背景

内置工具全量 schema 占上下文。初始只暴露工具组短入口，统一 `expand_tool_group` 展开后再可调。

## 要干啥

1. 引入 `ToolGroup`；MVP 先一组 `bot_profile`（展示名「Bot资料编辑」），挂上现有改资料/头像相关工具。
2. 默认工具列表 = 最小常驻核心 + 各组短入口 + 元工具 `expand_tool_group`。
3. `expand_tool_group`：返回该组完整 schema，并让本会话后续轮可调用；工具列表变更走现有 Env Stream。
4. 未 expand 调组内工具 → 未知/不可用错误。
5. 单测：默认列表、expand 前后、未 expand 调用失败。

## 怎样算对

- 冷启动无组内完整 schema；expand 后可调且测通一条真实写资料路径。
- 测试绿；合入本地 main；起 8788。无 UI 不喊设计师。

## 底线

- 能力渐进发现，不是前台闸；expand 对主 Agent 始终可用。
- 复用现有工具注册与审核；危险写仍走 review。
- 不碰例程 precheck / 网关 / 跨设备 / 环境实体；不 push。

## 本批交付边界

2026-10-27 主人明确：在 `feature/tool-group-expand` 编码、测试、提交、写 delivery-report 并 botmsg 回报；不合入 main、不起 8788、不 push。上文的合入与 8788 验收由父代理另做，本批不据此宣称端到端或日用通过。当前行为以 [内核](../atlas/kernel.md#工具组渐进披露) 为准。

## 命名规矩

政东 2026-10-27 拍板：同类功能统一名称前缀。组名与展开后工具名都遵守。

### 完整前缀表

| 前缀 | 域 | 本批 |
| --- | --- | --- |
| `bot_` | Bot 生命周期 + 人设/资料 | 强制对齐（见下） |
| `group_` | 群 | 规划 |
| `routine_` | 例程（含原 `notify_owner`） | 已基本对齐；`notify_owner`→`routine_notify` 规划 |
| `shell_` | Shell 会话与任务 | 规划补齐 `shell_bash`/`shell_sh`/`shell_zsh` |
| `file_` | 文件读写/搜索等 | 规划 |
| `skill_` | 技能 | 规划 |
| `agent_` | 子代理（原 `subagent_` 议名统一为 `agent_`） | 规划 |
| `context_` | 上下文/会话上下文类 | 规划 |
| `web_` | 联网检索/抓取 | 规划 |
| `memory_` | 记忆检索 | 规划 |
| （例外） | `ask_user` 可保留原名，或迁 `user_ask` | 规划，二选一即可 |
| （元工具） | `expand_tool_group` | 保留；跨组发现入口，不挂业务前缀 |

### Bot（强制，本批要对齐）

一律 `bot_`，**不要** `create_bot` / `delete_bot` / `fork_bot` 这类动词在前的散落名。人设/资料也归 `bot_`。

| 目标名 | 现状（待迁） | 说明 |
| --- | --- | --- |
| `bot_status` | 已是 | 只读状态 |
| `bot_create` | `create_bot` | 建 Bot |
| `bot_delete` | `delete_bot` | 删 Bot |
| `bot_fork` | `fork_bot` | 分叉 |
| `bot_pin` | `pin_bot` | 固定 |
| `bot_list` | `list_bots` | 列表 |
| `bot_message` | `message_bot` | 发消息给同主人 Bot |
| `bot_set_primary` | `set_primary` | 设主 Bot |
| `bot_update_profile` | `update_profile` | 改资料（人设/资料归 bot_） |
| `bot_propose_persona` | `propose_persona` | 提人设（人设/资料归 bot_） |

组名：`bot_profile`（展示名「Bot资料编辑」）。组内展开工具必须用上表 `bot_update_profile` / `bot_propose_persona`。

本批（`feature/tool-group-expand`）：组名与组内展开工具按上表改到位；审核表 / 测试 / atlas 同步改。仓内其它 `*_bot` / 无前缀 Bot 工具按上表迁，能顺手改则改，否则在 delivery-report 列迁移债。

### 例程（已对齐，保持）

惯例已是 `routine_`：`routine_create` / `routine_list` / `routine_update` / `routine_delete` / `routine_pause`。新工具继续此前缀；若将来有例程组，组名也用 `routine_`（如 `routine_manage`）。

对照补齐：`notify_owner` → `routine_notify`（规划迁移，本批不改代码）。

### Shell（规划补齐）

任务类已有 `shell_output` / `shell_status` / `shell_cancel`。交互壳统一：

| 目标名 | 现状 |
| --- | --- |
| `shell_bash` | `bash` |
| `shell_sh` | `sh` |
| `shell_zsh` | `zsh` |

本批不改代码，只定目标。

### 技能（规划迁移）

| 目标名 | 现状 |
| --- | --- |
| `skill_read` | `read_skill` |
| `skill_write` | `write_skill` |

本批不改代码；后续技能组组名用 `skill_`。

### 记忆（规划迁移）

| 目标名 | 现状 |
| --- | --- |
| `memory_recall` | `recall` |
| `memory_recall_history` | `recall_history` |

记忆主要靠文件工具 + `MemoryWriteRule`；只读检索工具迁到 `memory_`。本批不改代码。

### 其它同类（规划，本批不动）

- 群：`group_`（现状 `create_group` / `list_groups` / `post_group`）
- 子代理：`agent_`（现状 `read_subagent` / `expand_subagent`；`spawn`/`steer`/`hold` 等另议；不再用 `subagent_` 作目标前缀）
- 文件：`file_`（读写/搜索等文件工具目标前缀）
- 上下文：`context_`
- 联网：`web_`
- 问用户：`ask_user` 可留，或迁 `user_ask`
- 元工具：`expand_tool_group` 保留

### 原则

1. **同类前缀**：`<domain>_<verb_or_noun>`，domain 在前。
2. **组名 = 同类前缀**：组 id 用该域前缀（如 `bot_profile`），展示名可中文。
3. **展开后工具名 = 同类前缀**：组内成员对外名也带同一前缀，避免展开后仍散落。
4. **先跟惯例再迁**：例程已是 `routine_` 则保持；Bot 本批强制对齐；其余域以完整前缀表为目标，改名要连审核分类、测试断言、atlas 一并改。

## 还没做的

- 其余七项 Bot 管理工具仍用现名；完整迁移债与跨设备/分叉调用约定的影响范围见本批 delivery-report。本批只落地 `bot_profile` 及两项组内工具命名，`bot_status` 保持原名，其余域按上表规划。
- 父代理独立复审、合入本地 main，并执行 8788 真入口验收。
- 主人日用验收；通过后按工作流决定守护旅程归档并删除本设计。
