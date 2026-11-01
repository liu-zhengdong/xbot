# 记忆面板整篇与经历

M 档。2026-11-01 用户确认「记忆页单栏与经历搜索」原型后开工，取代 10-30 设计里的 person 展示与双栏。统一设计在 Obsidian `正在干什么/06-XBot/设计/记忆页单栏与经历搜索.md`；确认原型在 `~/.xbot-build/ui-shots/memory-single-column-20261101/记忆页原型.html`。本批不改存储布局、快照、注入，不做编辑器。

## 搜索的调用与结构

调用方：`GET /api/bots/{name}/experiences?q=TTS` 搜当前 Bot 整个经历库；`q=TTS&before=<next_before>` 继续取匹配项；无 q 保持原分页。响应复用 `items/next_before`，每页 40 条。搜索摘要、关注点、提纲，字面包含匹配，英文忽略大小写，保留记录时间新到旧，不让 `%` / `_` 变成通配符。q 在 HTTP 边界 trim 并限制 512 字节。

方案 A：扩展现有 `experience.Store.Page(limit, before, query)`，SQLite WHERE 过滤后沿原游标排序分页。前端 view 持有原列表分页与当前搜索结果，搜索回包核对查询世代与当前 Bot；轮询只更新原列表，不覆盖搜索输入。采用此方案：不增加库或第二套检索协议，旧页外经历同样可搜。

方案 B：前端持续拉完所有经历后本地过滤。不采用：需要为一次搜索加载整个库，首屏和内存开销随历史增长，分页期间结果不完整。另建 FTS/BM25 索引也不采用：此页只需字面搜索，没有新增排名与索引迁移的必要。

常驻仍用原 Markdown 渲染器，预览前两条（手机一条），展开完整原文。搜索输入保留焦点和输入法过程，清除后恢复已加载的普通列表；错误保留输入并可重试。经历行沿现有 keyed 更新保留展开与选区，搜索结果用文本节点和 mark 显示，不把库内容当 HTML。

## 怎样算对

沿用候选「记忆面板整篇与经历列表」的验收：

1. 记忆 Tab 单栏；不显示「关于你」，保留「关于本 Bot」、常驻预览／展开全文及打开原文件。
2. 经历逐条列出且必须带时间，新到旧，点开可读关注点／提纲。
3. 经历搜索覆盖尚未分页加载的记录；搜索可分页、清除可恢复原列表，无结果与失败可辨；快速换词、切 Bot 不显示迟到结果。
4. 常驻不改为一条一文件，不恢复专用 remember/forget；person 资料存储及注入不受页面隐藏影响。

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

- 用户日用验收；现有专项已到真实 serve / Chrome 端到端通过，尚未迁入 guard。
