# 输入框 URL / Markdown 高亮 + 消息数学公式（一批）

> **结论先行**：对话**草稿区**（未发送）里 `http(s)://…` URL **高亮可点**（系统浏览器，不误触发送），且 **Markdown 语法有可见高亮**；**已发送/流式消息气泡**侧按常见聊天/Grok 体验渲染数学（行内 `$…$`、块级 `$$…$$`）。同一候选续作，不另开条。候选：`正在干什么/06-XBot/候选/输入框URL高亮可点.md`。

## 怎样算对

1. 草稿区 URL：`http://` / `https://` **高亮**；**点击**用系统默认浏览器打开；不触发发送。桌面壳复用 appshell 离域 → `OpenExternal`；Web 用 `target=_blank` + `noopener`。
2. 草稿区 Markdown：**语法可见高亮**（至少：`` `code` ``、`**粗**`、`*斜*`、标题 `#`、列表 `- ` / `*`、链接 `[text](url)`；URL 裸链与上条一致）。高亮是「看着像语法上色」，不是在草稿里提前渲染成最终 HTML 排版（草稿仍可编辑源码形态）。
3. 消息侧数学：气泡里行内 `$…$`、块级 `$$…$$`（及换行块）按 Grok/常见聊天体验渲染；跟现有 `appendMarkdown` / 气泡约定衔接。无既有约定时用 **KaTeX**（或仓内已有同类）最小接入，CSS/字体随静态资源走，写进本规格。
4. Enter（非 Shift、非 IME composing）仍发送；点 URL / 点在高亮 span 上不 `requestSubmit`。
5. 只动对话 composer + 消息渲染相关；设置浮层等其它 textarea 不动。
6. 合入本地 main（不 push）→ Mini 8788 → 政东手测。

## 现状与实现（第二批已落，均在本分支）

- 输入框：`#input` 已是纯文本 contenteditable（`plaintext-only`，不支持时回退 `contenteditable=true`）。
- URL 高亮：`http(s)://` 就地成链，`_blank` + `noopener`，尾标点不吞，紧贴中文不吞，非 http(s) 不亮。
- 草稿 Markdown 高亮（第二批）：行首 `#`/`##`/`###`（要求后跟空格；`####` 及更深不认）、列表符 `- `/`* `（要求空格）、`` `code` ``（内部不再嵌套其它高亮）、`**粗**`、`*斜*`、`[text](url)`。全部保留源字符、非 WYSIWYG；`_斜_` **不做**（snake_case 误判太多）；列表星不误判成斜体（先吃行首标记再扫行内）；`[text](url)` 协议规则与裸 URL 一致，非 http(s) 整段保持纯文本。
- 气泡数学（第二批，复审一修）：`appendMarkdown`/`appendBlocks`/`appendInline` 路径上渲染 `$…$`（行内）与 `$$…$$`（行首成块、居中显示；句子中间的 `$$…$$` 作行内 span，回退仍保留完整源串含 `$$` 定界符）。块级配对按首个闭合 `$$` 切分（`lastIndexOf` 会把同行/闭合行尾的后续公式吞进第一段 TeX）。KaTeX 0.16.11 内置在 `static/vendor/katex/`（未改动的上游 katex.min.css/js + LICENSE，字体仅 woff2 共 20 个 296 KB）；sw SHELL 加 CSS/JS（CACHE 升 v15），字体走运行时缓存。TeX 源从不作为 HTML 进 DOM：唯一 innerHTML 来自 `katex.renderToString` 的转义输出，且以 `throwOnError:true` 调用——库缺失、抛错或真实解析失败（非法 TeX 不再以 `.katex-error` 蒙混）都回退原文（`.math-raw` 等宽字显示含定界符的源串），不炸气泡。
- 数学启发式：金额 `$5 和 $6`（闭侧是空格）不当公式；代码段里的 `$` 保护；流式期间未闭合的 `$` 先显原文，闭合后整段重渲染即正确（流式每 delta 全量重建，无需增量）。
- Enter（非 Shift、非 IME composing）仍发送；点 URL / 点 `[text](url)` / 点高亮 span 不 `requestSubmit`。
- 只动对话 composer + 消息渲染相关；设置浮层等其它输入区不动。
- 安全：`href` 仅 http(s)；用户稿从不 innerHTML。16000 上限同旧 textarea 的 maxlength：超限的用户编辑（中间插字、选区替换、粘贴、拖入、整段替换）在编辑入口（refreshComposerLinks）整体拒绝并恢复光标，禁止事后截尾；程序化写入（发送清空、草稿恢复、失败回填）不截断。
- 撤销/重做由页面自维护（composerHistory，每接受一步编辑一步；⌘/Ctrl+Z、⌘/Ctrl+Shift+Z、Ctrl+Y 与菜单撤销经 beforeinput 拦截；原生 execCommand undo/redo 只发不可取消的 input historyUndo/historyRedo，在 input 入口丢弃原生结果、改走自维护历史）：每次 input 都全量 replaceChildren，原生撤销栈已失效；程序化写入重置基线，同旧 textarea 赋值。
- 不做：mailto、WYSIWYG 工具栏、改设置页、全量 CommonMark、化学式插件、`_斜_`。

## 还没做的

- 合入本地 main、Mini 8788 走查与政东手测（本分支交付时未做；不动候选 status）。
- 规格明确不做的：mailto/自定义协议、预览卡片、改已发送消息气泡的排版模式、拖拽排版、`_斜_`。

## 交付

- 分支 `feature/composer-url-links`；报告 `~/.xbot-build/composer-url/encode-report.md`。
- 本文件与实现一并提交。
