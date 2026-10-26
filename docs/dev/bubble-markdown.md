# 气泡 Markdown 排版（代码高亮 · 引用块 · 表格）

> **结论先行**：消息气泡侧补齐常见聊天排版——**代码块有语法色**、**引用成块且不露源字符 `>` / `>>`**、**GFM 表渲成真正表格**。与「输入框 URL/Markdown 高亮 + 气泡数学」（composer-url-links）**解耦**，不改草稿区行为。候选：`正在干什么/06-XBot/候选/气泡Markdown排版.md`。

## 怎样算对

1. **代码块语法高亮**：` ```python ` / `js` / `go` / `ts` 等常见语言在 **Bot 气泡**里关键词、字符串、注释**可见上色**（不能只有灰底 + 纯黑字）。语言标签与「复制」按钮仍可保留。用户气泡若也渲染代码块，至少不要被 CSS `color: inherit` 整块抹成单色到「看起来没高亮」。
2. **引用块**：行首 `>` / 嵌套 `>>` 渲成带左侧竖条（或等价成块样式）的引用，**源里的 `>` / `>>` 不得出现在可见文字里**。引用内可继续有列表、行内格式、**已有 KaTeX 公式**（`$…$` / `$$…$$`），公式仍正常。
3. **表格**：GFM 管道表（表头行 + `|---|` 分隔 + 数据行）渲成真正 `<table>`（表头、格线/单元格分隔可见），不是 raw `| … |` 纯文本行。格内链接仍可点，协议仍限 http(s)。
4. **安全**：用户稿不当 raw HTML；代码/表/引用内容用 textContent 或等价转义进 DOM；链接协议仍只认 http(s)。唯一允许的 innerHTML 路径仍是既有 KaTeX 转义输出。
5. **范围**：只动消息气泡 Markdown 渲染相关（`appendMarkdown` / `appendBlocks` / `codeBlock` / `highlight` 与配套 CSS、必要时 vendor、测试）。**不改** composer 草稿高亮、不扩进 composer-url-links、不顺手改 CLAUDE.md / AGENTS.md / lock / 发版脚本。
6. **合入**：本地 main（不 push）→ Mini 8788 → 政东手测。候选 status 保持实现中，你不要标已完成。

## 现状（编码前快照，基于 main `492de9b`）

- 路径：`internal/web/static/app.js` 的 `appendMarkdown` → 围栏代码走 `codeBlock`+`highlight`；其余走 `appendBlocks`+`appendInline`（含 KaTeX）。
- `highlight`：自研极简分词，语言匹配 `go|js|ts|java|py`，关键词集偏 Go/JS，**缺 `def` 等**，Python 几乎无色。
- CSS：`.bubble .kw, .bubble .str, .bubble .cm { color: inherit }` 会抹掉用户气泡语法色；Bot 用 `.bot-bubble`，主要矛盾仍是分词过弱。
- `appendBlocks`：**不识别**行首 `>`，也不识别管道表 → 引用露 `>`、表当纯文本（政东手测反例图）。

## 实现方向（Agent 自决，验收为准）

- 高亮：强化现有 `highlight`（按语言补关键词 + 保证 Bot/用户气泡颜色可见），**或**像 KaTeX 一样最小内置成熟高亮库（MIT 等宽松许可，vendor 进 `static/vendor/`，sw 缓存跟上）。禁止假样式壳（只加 class 不上色）。
- 引用：在 `appendBlocks`（或同层）吃连续 `>` 前缀，嵌套层级用缩进/竖条表达；剥掉可见的 `>` 后再把剩余交给既有 inline/列表/数学路径。
- 表格：识别 GFM 表头+分隔行，渲 `<table><thead><tbody>`；单元格走 `appendInline`（链接/粗斜/行内代码/数学）。对齐标记（`:---:`）可忽略或做 text-align，不强制。
- 流式：与现有一样全量重绘即可；未闭合围栏/表保持不炸气泡。

## 不做

- 改草稿区 / composer-url 行为
- 全量 CommonMark、脚注、任务列表、HTML 块
- 改 xAI OAuth 详情、multi-device 等无关面

## 交付

- 分支 `feature/bubble-markdown`；本文件与实现一并提交
- 报告 `~/.xbot-build/bubble-md/encode-report.md`
- 合入与 8788 / accept.md 由执行官收尾
