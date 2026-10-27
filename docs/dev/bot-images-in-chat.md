# Bot 出图到聊天（助手气泡挂图）

> **结论先行**：扩增**现有 Bot 发送消息工具**，消息可挂 paste/工作区图；助手气泡真画 gallery。**本批只做挂图 + 展示**。生图另立候选「厂商hosted生图」（未排期）——**禁止**本分支碰 hosted 生图 / generate / 自研出图工具。候选：`正在干什么/06-XBot/候选/Bot出图到聊天.md`。

## 背景

政东对照执行官侧贴图：XBot 助手不能在聊天里出图。入站 paste / 用户气泡已有；助手行不挂 paste id、`botBubble` 不读 `m.images`。

## 怎样算对

1. **真出站到气泡**：助手气泡出现与用户侧同款 gallery（本地 `GET /api/bots/{name}/paste/{id}`），不是公网 Markdown 链冒充。
2. **扩现有发消息工具**：参数可带 paste id 和/或工作区路径；落盘复用 `paste/`（类型/大小/张数沿用 PastedMax*）。**不新建**出图/生图专工具。
3. **transcript**：助手 `Entry.Images` 记文件名；`botBubble` 读 `m.images` → `pastedURLs` + `gallery`。
4. **范围**：本分支＝发消息挂图 + 气泡展示。**不做**文生图、hosted 生图、generate。不 push；不动 xAI 详情 / 前后台 / 人设种子；不改 CLAUDE.md / AGENTS.md / lock。
5. **合入**：本地 main（不 push）→ Mini 8788 → 政东手测。候选保持实现中。

## 现状（main `ca742b4`）

- 用户贴图全链路已通；助手 `botBubble` 不读 `m.images`；`recordTurn` 已能写 `ImageNames`→`Images`。
- 发消息类工具：`message_bot` / `post_group` / `send_message_to_user` / 连接器 `*_send` 等——扩合适的现有工具。

## 不做

- 任何 generate / hosted 生图 / 自研出图工具（另候选「厂商hosted生图」）
- 假公网链冒充出图

## 交付

- 分支 `feature/bot-images-in-chat`；本文件一并提交
- 报告 `~/.xbot-build/bot-images-chat/encode-report.md`

## 实现（本分支）

- 扩的是 `send_message_to_user`：新参数 `images`（pasted id 或工作区图片路径）。没有新工具。
- 落盘：`internal/kernel/outimage.go`。路径经 `os.Root` 在 WorkDir/SharedDir 内读；类型/单张 10 MiB/长边 2048/每条回复 4 张、合计 10 MiB 沿用 `work.PastedMax*` 和 `tools.FitsModel`；先全部校验再写，拒绝时不留文件也不发通知；`work.PastedNameFor` 按内容哈希命名，重复挂同一张无害。
- 挂到气泡：名字挂在本轮（`turn.outImages`），`recordTurn` 写最终回复（无 tool_calls 的助手行）时放进 `Entry.Images`。
- 归属按调用身份：工具 Job 用 `callTurnKey` 里的原轮，普通调用用 `turnKey`，都没有才取当前轮。调用发起时同时捕获原轮已提交最终回复的次数；挂图前及落盘前在原轮锁内核对，次数已变或原轮 `finished` 时整次调用拒绝，不发通知，结果里说明要重新调用；不挂到别的轮。同 turn 插话续聊新发起的调用按当时次数挂到自己的下一条回复，不受粘滞的 `answered` 误拒；没有调用快照时仍按 `answered` 保守拒绝。
- paste id 也按实际内容做 `SniffImage` + `FitsModel` 校验，扩展名须与内容一致。
- 读取阶段就限额：引用先去重，超过 4 个不同引用直接拒，不读文件；每个文件先查单张大小及剩余 10 MiB 预算。放不进剩余额度的工作区路径，先限长流式计算内容 id；只有已排队或本批已验证的同 id 才允许继续读取校验而不再计新增额度，新图不整图加载。已排队 paste id 免新增额度，但仍按实际内容校验类型、像素及扩展名。
- 之后各轮该行说明为「[发给用户的图] 路径」（`withLineNote`），不冒充用户的图。
- UI：`botBubble` 用 `pastedURLs` + `gallery` 画 `m.images`；SILENT 但带图的回复只画图。

## 还没做的

- 用户日用通过（Mini 8788 手测）；手机宽度浏览器未验（本地 headless 被手机账号登录流挡住）。
- 本轮没有最终回复（出错/中断）时，已挂的图不上气泡，只有通知文字。
- 群组 / 连接器 `*_send` / `message_bot` 不挂图（本批不做）。
