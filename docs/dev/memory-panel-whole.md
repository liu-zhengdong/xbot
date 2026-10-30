# 记忆面板整篇与经历

M 档。按用户体验设计师 2026-10-30 定稿，统一设计在 Obsidian `正在干什么/06-XBot/设计/记忆面板整篇与经历.md`；视觉与文案真源为 Box `/workspace/xbot-ui-design/memory-panel/{ia,handoff}.md`。本批不改存储、快照、注入，不做编辑器。

## 怎样算对

沿用候选「记忆面板整篇与经历列表」的验收：

1. 记忆 Tab 的常驻以整篇呈现，范围分人 / Bot，可跳到对应 memory.md。
2. 经历逐条列出且必须带时间，新到旧，点开可读摘要 / 提纲。
3. 常驻不改为一条一文件，不恢复专用 remember/forget。

装配缝：`GET /api/bots/{name}` 的 `memory` DTO → `libraryView`。常驻直接读原文；经历复用 `experience.sqlite`，首屏 40 条，更早分页；当月流水只返回路径及空状态，放页脚次要入口。

**冒烟用例**：`internal/web/memory_panel_test.go` 的 `TestMemoryPanelWholeDocumentsAndDatedExperiences`、`internal/web/memory_panel_browser_test.go` 的 Chrome 行为测试，以及真实二进制 `e2e/smoke/memory_panel_test.go` 的 `TestMemoryPanelWhole`。

## 对照回顾

| 相关契约 | 处理 |
|---|---|
| atlas/memory.md：person / bot 常驻原文、经历库独立 | 直接读取原文件与现有经历库，不从 Recall 重组正文 |
| ADR 0030 / 0033：经历归 Bot、资料与经历分开 | 只读当前 Bot 经历，不把 log 或资料索引投成经历 |
| guard/界面.md：气泡同族 Markdown、主题 token、真实浏览器 | 复用 appendMarkdown / prose；检查明暗与窄屏及交互 |
| 候选「一条一文件」与本条冲突 | 按最新定稿不做 |

## 还没做的

- Box handoff 逐字文案与原型对照：本机无 `/workspace`，当前连接无法读取 Box。已按用户消息与 Obsidian 定稿落实整篇、经历及页脚；精确 handoff 尚待核对。
- 独立设计师视觉复审、用户日用验收；截图在 `~/.xbot-build/ui-shots/memory-panel-whole/`。未迁入 guard。
- 全仓和网页整包三次尚未通过，受既有内核 / shell 失败及网页审批超时影响；旧六条旅程也在模型回复阶段失败，本批专用真实 serve 冒烟已通过。验证证据见 MILESTONES。
