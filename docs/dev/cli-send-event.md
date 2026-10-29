# CLI 外部 Event 到 Bot

## 旅程与怎样算对

外部 Codex/Agent 点名叫醒运行中 Bot，`send --whoami` 经本机会话投递不可信 CLI Event，收件箱可见，模型看到 untrusted_event，主人聊天不出现冒充的输入。调用方得到受理与可见结果、安静或失败；主人 `message` 保持原义。无值得告知的事情可 SILENT，有则用 send_message_to_user。

## 实现与冒烟

唯一装配缝：CLI → owner 鉴权的 Event HTTP 入口 → inbox → 已有 kernel。处理锁空出后才受理，以免忙时事件被吸收而产生无法归属的回执。测试见 `cmd/xbot/cmd_send_test.go` 和 `internal/web/cli_event_test.go`。

## 还没做的

- 真入口端到端、主人日用验收；外置健康循环仍在候选。
