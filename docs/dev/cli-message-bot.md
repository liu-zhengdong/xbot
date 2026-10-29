# CLI 发消息到指定 Bot

## 挂在哪条旅程

进程外脚本（含 Codex ping）能点名叫醒本机正在跑的某个 Bot，并拿到可观测回执。外置 5 分钟健康循环仍属独立候选，本批只做通道。

## 怎样算对

- `xbot [--root DIR] message <bot> <text…>`（或 `-` / `--text`）把主人消息投进**正在跑的** serve/桌面壳，不另起 kernel、不抢 handle 当第二主循环。
- 本机回环默认鉴权：走既有 `GET /api/session` 本机会话或 `web-token.json` + `POST /api/login`；复用 `POST /api/bots/{name}/messages` 的 SSE（`done`/`error`），stdout 打印助手可见正文（或明确失败）。
- `--http` 指 serve 地址，默认 `127.0.0.1:8788`（与 xbot-dev 一致）；`--timeout` 默认够一次短回复；超时/serve 不可达/Bot 不存在非 0 退出，便于脚本。
- 测挂在「CLI → 运行中 serve 主人消息缝」：假 HTTP 或测试 serve 上发一条收到 `done`；禁止以「另起 chat 进程」冒充实作。

## 实现要点

- 唯一装配 seam：`cmd/xbot` 的 `message` 子命令 → 本机 owner HTTP 客户端 → 现有 `postMessage` 流。不新造 inbox 旁路、不复制 kernel。
- `xbot chat` 仍是交互 REPL，本批不改它的语义。

**冒烟用例**：`cmd/xbot/cmd_message_test.go` 的 `TestMessageProcess`（命令分发 → 假 HTTP 会话与消息 SSE）；尚待真实入口和主人日用验收。

## 还没做的

- 外置 Codex for 循环 5 分钟无回再修：仍在候选「重启失败由外置 Codex 兜底」。
