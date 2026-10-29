# 聊天仅工具产物可见（dev）

候选：`候选/聊天仅工具产物可见.md`；设计：Obsidian `06-XBot/设计/聊天仅工具产物可见.md`。现状已写进 [ui.md](../atlas/ui.md)「Bot 只通过工具说话」与 [kernel.md](../atlas/kernel.md)「对用户说话」「群历史」。

## 怎样算对

- `TestLiveBubbleSplitBrowser`：流中 `delta` 不出字；两次 `send_message_to_user` 送达 → 两泡、只有一条 `.live`；收工对账后仍两泡、裸正文和工具系统行从未进主列；旧轮（截止时间前）正文仍画，新轮只有正文不画；侧栏预览不含裸正文。
- `TestSendInOwnerChatSkipsTheBell`：终端/网页轮送达不写 outbox，回执 `sent to the owner's chat`；子代理唤醒照旧通知。
- `TestSilentAfterChatSendIsAnAnswer`：用户正聊里已送达后以 SILENT 收尾不算失败。
- `TestGroupHistoryMembersReadAndFilter`：成员可读、非成员拒、默认 20/封顶 50、keyword+sender、before_id、正文一行加引号。
- e2e `HELLO_WEB` 三条：网页流 `done.text` 是送达的消息。

回归：`TestShellCompletionStaysOutOfChatBrowser` 用假 transcript 检查 shell 来源完工不出现在主列，主人和送达消息及 webhook 仍可见。

## 还没做的

- 用户日用：真模型下是否稳定先调 `send_message_to_user`，没调时新轮是空白（只有忙态）；需观察后决定要不要内核兜底提醒。
- 终端 `xbot chat` 仍打印裸正文流，`send_message_to_user` 只以工具摘要出现。
- 旧轮/新轮按固定时间 `2026-10-27 11:00 +08:00` 区分，部署晚于此时的旧内核轮次会显示为空。
- 厂商搜索的步骤与引用角标原挂在助手正文上，新轮不再画进气泡（只在轨迹）。
- 参数级流式（边生成边出泡）没有：送达瞬间出整泡。
- 未在本机浏览器人工看过（只跑了无头 Chrome 的回归）；设计师逛之前不算界面完成。
