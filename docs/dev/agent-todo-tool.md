# Agent Todo 工具

> **结论先行**：内核已有 `todo` 工具 → `bots/<name>/todos.json` + 提醒/完成门 + `GET /api/bots/{name}/todos` + `BotStatus.todo`。本批**禁止另起账本**；把同一套露清给主 Agent，并让主人在轨迹/状态里用人话看到「待办 / 进度」。不做 Tasks 主动性队列。

## 背景

coding harness（Claude Code / Codex 等）长活靠 Todo 清单。XBot 内核 M3 已落地工具与提醒，但主人侧几乎看不见（atlas：侧边栏和步骤弹层还没画；`TodoSummary` 仍是英文 `in progress` / `pending`；轨迹里工具名是裸 `todo`）。竞品扫后今晚推这条。

候选真源：Obsidian `候选/Agent Todo 工具.md`。与 `候选/Todo与Tasks-主动性引擎.md` 分档：本条单次活；Tasks 不做。

## 要干啥（验收）

1. **同一账本**：继续用内核 `todo` / `todos.json` / archive；工具与提醒同一份。禁止第二套 todo 文件或 Tasks 队列。
2. **主 Agent 真能用**：开关开着时工具可写清单；跨多步工具调用清单仍在（已有行为须保持，假开关不算过）。不要为「露工具」改产品默认模型开关策略（deepseek 默认开等现策保留），除非发现工具根本挂不上——那时只修挂载/描述缺口。
3. **主人侧文案**：凡主人能看见的摘要/轨迹标签用人话「待办」「进度」；藏 `todos.json`、`todo_steps`、`in_progress` 等工程词。建议：
   - `TodoSummary` / 状态预览：如「进度：写测试 · 还剩 2 项待办」
   - 轨迹里 `todo` 工具行：显示「待办」或「更新待办」，展开可见条目状态的人话（进行中 / 待办 / 已完成 / 已放弃），不要只丢原始 JSON/英文 counts
4. **可见位置（最小够用）**：
   - **轨迹**：`todo` 调用用人话标签；打开后能看出在做/已完成（可从工具入参或回包解析，勿另调破坏性接口）
   - **侧边栏或步骤弹层**：把已有 `BotStatus.todo` 画出来（一行摘要即可）；或步骤弹层旁拉 `GET /api/bots/{name}/todos` 列当前清单。守 [guard/界面.md](../guard/界面.md)：对话里不加任务卡；工具原文只进轨迹；不露原始工具名到状态行（用「待办」）
5. **薄测**：相关包 `go test -count=1`；改文案/摘要补单测；UI 测接口与行为，不为未定稿文案写死静态串断言（除非沿用既有断言并更新）。

## 底线

- 不碰 `feature/context-batch-b`、`feature/imggen-ux-hotfix`、技能热修相关脏树/文件集。
- 不做 Tasks 主动性引擎、不做设置页产品面大改、不改压缩/环境快照/技能导入。
- 不 push；不改 `CLAUDE.md` / `AGENTS.md` / lock。
- 假开关不算过：工具真写 `todos.json` 且跨步仍在。
- herdr 临时 session 用完由执行官清。

## 样例正反手

正：
- 主人说「帮我查三处再汇总」→ Agent 调 `todo` 建 3 项并逐项勾；`todos.json` 有真数据；轨迹可见「待办」进度。
- 侧栏/步骤处能看到一行「进度：…」或待办摘要（来自同一账本）。

反：
- 只在回复里列 markdown 清单、工具不落盘。
- 另起 `tasks.json` / 第二套 todo / Tasks 队列。
- 为做 Todo 改 compact / env stream / 技能导入。
- 主人界面仍满屏 `in_progress` / 裸工具名 `todo`。

## 还没做的

- 合入后由执行官在 8788 验收真实入口的三项待办旅程，随后由主人日用确认；本分支保留设计供验收，不迁入 guard。

### 本批不做

- Tasks 主动性引擎（另候选）。
- 子代理是否各自一份 Todo（开放点，本批不拍板：维持现有 per-bot 清单）。
- 大改设置页或独立「待办」产品面。

## 测

- `go test -count=1 ./internal/kernel/ ./internal/web/`（及你实际改到的包）
- 手动：在本 worktree 起服后看轨迹与侧栏（执行官合入后起 8788）
