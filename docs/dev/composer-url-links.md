# 输入框 URL / Markdown 高亮 + 消息数学公式（一批）

> **结论先行**：对话**草稿区**（未发送）里 `http(s)://…` URL **高亮可点**（系统浏览器，不误触发送），且 **Markdown 语法有可见高亮**；**已发送/流式消息气泡**侧按常见聊天/Grok 体验渲染数学（行内 `$…$`、块级 `$$…$$`）。同一候选续作，不另开条。候选：`正在干什么/06-XBot/候选/输入框URL高亮可点.md`。

## 怎样算对

1. 草稿区 URL：`http://` / `https://` **高亮**；**点击**用系统默认浏览器打开；不触发发送。桌面壳复用 appshell 离域 → `OpenExternal`；Web 用 `target=_blank` + `noopener`。
2. 草稿区 Markdown：**语法可见高亮**（至少：`` `code` ``、`**粗**`、`*斜*`、标题 `#`、列表 `- ` / `*`、链接 `[text](url)`；URL 裸链与上条一致）。高亮是「看着像语法上色」，不是在草稿里提前渲染成最终 HTML 排版（草稿仍可编辑源码形态）。
3. 消息侧数学：气泡里行内 `$…$`、块级 `$$…$$`（及换行块）按 Grok/常见聊天体验渲染；跟现有 `appendMarkdown` / 气泡约定衔接。无既有约定时用 **KaTeX**（或仓内已有同类）最小接入，CSS/字体随静态资源走，写进本规格。
4. Enter（非 Shift、非 IME composing）仍发送；点 URL / 点在高亮 span 上不 `requestSubmit`。
5. 只动对话 composer + 消息渲染相关；设置浮层等其它 textarea 不动。
6. 合入本地 main（不 push）→ Mini 8788 → 政东手测。

## 现状与最小改动方向

- 现状：`#input` 曾是 `<textarea>`；本批改为 **contenteditable** textbox（若编码已在做，继续这条，勿回退 textarea）。
- 草稿高亮：在 contenteditable 内对 token 包 span/a（URL 用 `<a href>` 可点；MD 语法用 `<span class="md-…">` 上色，**保留可见的 `*` `_` `#` 等源字符**，不要把草稿变成 WYSIWYG 成品）。
- 消息数学：在 `appendMarkdown` / `appendBlocks` / `appendInline` 路径识别 `$…$` / `$$…$$`，交给 KaTeX（`renderToString` 或 DOM API）；失败时回退纯文本，勿炸整条气泡。
- 安全：`href` 仅 http(s)；KaTeX 输出走信任库默认；用户稿勿当原始 HTML 塞 DOM；`maxlength` 16000 仍按纯文本计。
- 不做：mailto、WYSIWYG 工具栏、改设置页、全量 CommonMark、化学式插件。

## 还没做的

- 合入本地 main、Mini 8788 走查与政东手测（本分支交付时未做；不动候选 status）。
- 规格明确不做的：mailto/自定义协议、预览卡片、改已发送消息气泡、拖拽排版。

## 交付

- 分支 `feature/composer-url-links`；报告 `~/.xbot-build/composer-url/encode-report.md`。
- 本文件与实现一并提交。
