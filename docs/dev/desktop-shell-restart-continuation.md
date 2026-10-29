# 桌面壳日常与重启续跑

## 挂在哪条旅程

日常用本机构建验证 XBot；用户授权 Bot 重启后，所有正在工作的 Bot 在新进程醒来时收到共享续跑提示。

## 怎样算对

- `xbot-dev start/restart` 默认启动 `xbot app`，复用 `~/.xbot-dev`、8788 和既有 dev pid/log；只有 `--web` 才启动 serve 并打开浏览器。
- `restart_xbot` 走 kernel 工具注册表和统一权限表，始终需用户确认。确认后先写唯一待续跑记录，再请求 app Relaunch 或 serve reexec。
- 启动只消费一次待续跑记录，给前台锁忙、或含运行/排队子代理的每个 Bot 投递相同提示和“XBot 已重启”；空闲 Bot 不收到。
- 进程退出仍由 app 窗口生命周期负责；服务模式沿用现有 drain。

## 实现

- 桌面默认入口切换在 `scripts/xbot-dev`，浏览器回退由 `--web` 选择。
- 重启提示由 `internal/kernel` 的 `restart_xbot` 工具进入 `cmd/xbot` 服务回调。唯一待续跑文件位于数据根；启动投递使用既有 append-only Bot Inbox，确定性事件 ID 使部分投递后重试幂等。
- 正在工作以 handle lock 和现有子代理 `meta.json` 的 running/queued 状态判定。

## 冒烟用例

`cmd/xbot/restart_continuation_test.go` 覆盖数据落盘后模拟新进程消费：前台忙、运行子代理、排队子代理都收到包含共同提示的事件，空闲 Bot 不收到。`internal/kernel/registry_test.go` 覆盖注册缝与提示参数传递；`internal/review/review_test.go` 覆盖始终确认。

## 还没做的

- 重启失败时由外置 Codex 兜底回复：独立候选，不在本批。
- 自动改代码合入、自评测门与回滚链：明确不做。
- 尚未在本机实际开桌面窗完成端到端人工验收。
