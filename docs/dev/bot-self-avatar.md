# Bot自改头像（propose_profile + avatar_seed）

> **结论先行**：扩展已有 `propose_profile`，增加 `avatar_seed`（对齐编辑资料「换一批」/`名字#n`），主人确认后写入本 Bot；不造平行工具；自定义上传二期不做。勿与「换头像风格与多一套主题」混。

## 目标

Bot 能提议改自己的生成头像种子；主人确认后侧栏与资料头像更新。

## 验收

1. `propose_profile({"avatar_seed":"Ada#3"})` 走确认卡（与改名同级）；拒绝不写盘；允许后 `SetAvatarSeed`，avatar 回 `generated`（若曾有上传则清除）。
2. 非法 seed（空、控制字符、`<>"'&`、超长）工具层报错，不发卡。
3. 仍拒绝未知字段；name/description/timezone 行为不变；可与 avatar_seed 同卡组合。
4. 只作用于本 Kernel 的 Bot；无「改别人头像」入口。
5. 工具描述/schema 写明可改头像；「换一批」= 提议新 seed（习惯 `名字#n`，与 UI 一致），不另造工具。
6. 相关 Go 测 + review 类冒烟 PASS；合入本地 main（不 push）；8788 可手测。

## 底线

- 只改本 Bot；沿用 propose_profile 确认流；不平行造出图/换头像专工具。
- 不做自定义上传图；不改头像风格算法/主题套装。
- 不 push 远程；不碰无关仓。

## 实现要点（对照）

| 点 | 落点 |
|---|---|
| schema / 解析 | `internal/kernel/profile.go` `known` 加 `avatar_seed`；校验 `bot.ValidAvatarSeed` |
| 写入 | 确认后 `Bot.SetAvatarSeed`（已有：落 seed、清上传、SaveConfig） |
| 确认文案 | summary 含 `avatar_seed "…"`；卡标题仍「修改档案」即可 |
| 测试 | `internal/kernel/profile_test.go`：确认流、非法 seed、应用后 AvatarState |
| 文档 | 本文件；必要时 API/工具说明随实现轻更 |

## 对照回顾

| 项 | 期望 | 实作后勾 |
|---|---|---|
| 仅本 Bot | Kernel 无跨 Bot 目标参数 | |
| 确认流 | 与 name/desc/tz 同卡 | |
| 换一批 | seed 字符串/`名字#n`，无平行工具 | |
| 上传图 | 不做 | |
| 与风格候选隔离 | 不改 DiceBear/主题 | |

## 还没做的

- 合入本地 main，由主管安排；本编码交付只提交 `feature/bot-self-avatar`，不 push。
- 真入口浏览器验收：从对话提议，经确认卡允许/拒绝，到侧栏与资料头像刷新；8788 与主人日用尚未验证。
- **冒烟用例**：独立端到端用例待写。Go 组件回归在 `internal/kernel/profile_test.go`，不代替浏览器旅程。
