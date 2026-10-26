# 技能设置页五条 UX 热修

> **结论先行**：主路径（对话自装）已 PASS。本批只改设置面文案与列表展示名，让设置页和「跟 Bot 说一声就能装 / 本机 Bot 共享」的体感对齐。不做跨 Bot 假隔离。

## 背景

设计师逛 tip `50d2cb6`：对话装技能同轮能用，但设置页仍像「整台公共库 + 只教导入」；脚注「下一轮才生效」与同轮已可用拧着；列表主名是英文 slug。

证据：`/workspace/xbot-ui-design/review/50d2cb6-self-skill/VERDICT.md`（只读）。

## 要干啥（验收）

改 `internal/web/static/skills.js`（及相关 UI 测试断言），必要时极轻触 `skills.go` / `skill` 仅当展示名要从 API 补字段——优先前端展示逻辑。

1. **导语**：`技能库整台共用…` → `本机 Bot 共享…`（启用仍按 Bot 单独设）。
2. **空态**：除「导入」外，补一句「也可在对话里让 Bot 帮你装」。
3. **列表 / 详情主名**：优先人读标题——`name !== slug` 用 name；否则用 `description`（非空且非 slug）；再退 `slug`。副行 hint 放另一侧信息（描述或 slug），避免主副同文。
4. **导入弹层等**：凡「整台共用 / 所有 Bot 都能看见 / 所有 Bot 都能用」→「本机 Bot 共享」口径；同类措辞一并改。
5. **脚注**：删列表脚「改完后下一轮对话生效」；保留「点开一条，可给某个 Bot 打开或关掉」若仍有用，或整句改成不含「下一轮」。
6. **轻量（可选）**：导入中已有禁用按钮；对话装进度若挡路，记进候选不硬做。**不要改 tip / bin/xbot.tip**。

## 底线

- 真改文案与主名展示；加假「只给你用」开关 / 假装 per-bot 隔离 = 不算。
- 不碰并行分支文件（imggen-ux-hotfix、context-batch-b、生图、头像、tip 脏写）。
- 不 push；不改 CLAUDE.md / AGENTS.md / lock。
- 相关包 `go test -count=1`（不要 `-count=3`）。

## 样例

正：
- 导语「本机 Bot 共享。是否启用，按每个 Bot 单独设。」
- 空态：「还没有技能」+「从本机导入…；也可在对话里让 Bot 帮你装。」+「导入」钮
- 列表：主名「我说记一下你就记笔记」（description），副行 `note-capture` 或短描述
- 无「下一轮对话生效」

反：
- 仍写「整台共用 / 所有 Bot 都能看见」
- 主名仍是裸 slug 而 description 是中文人话
- 新增「仅当前 Bot」开关却不落挂载语义

## 测

- `go test -count=1 ./internal/web/ -run Skill`
- 必要则补 UI 测试断言新文案 / 展示名回退
