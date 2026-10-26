# 生图挂气泡热修

> **结论先行**：hosted 生图落盘 `generated/` 成功后，**同轮**经现有挂图通路挂到助手回复气泡；用户只在回复挂图时看见图。生图在时间线里当**普通工具调用**展示，不要「已生图·存工作区」那种独立 UI 组件。勿另起 generate 专工具。候选：Obsidian `候选/生图挂气泡热修.md`。

## 政东纠正（2026-10-27）

1. **生图当普通工具调用**：不要「已生图·存工作区」独立 hosted 步骤组件（状态条专用块 / 「查看 generated/」当交付主路径那套）。时间线按普通 tool 展示即可。
2. **只有回复消息挂图时用户才看见图片**：产物可进 `generated/`；交付靠 `attachOutImages` → `Entry.Images` → gallery。
3. 开发群已删：编码交活用 `botmsg send --name XBot执行官`，不要往群发。

## 背景

月亮猫图在 `generated/`，transcript 无 image 附件，前端不渲图；模型谎称已显示。挂图与生图已分批合入，缺同轮交付桥。

## 怎样算对

1. 生图成功 → 同轮最终助手气泡有真图 gallery。
2. 工具/回执给**真路径**；成功话术「已挂本条」类实话。
3. 时间线：`image_generation` 看起来像普通工具调用，不是独立生图 UI 组件。
4. 不新建生图专工具；不 push。

## 关键点（钉住即可，细节自补）

- `Save` 成功后调 `attachOutImages`（带 turn ctx），图进当前轮 `outImages`。
- 更新成功/引导文案：已挂本条；删「未附加 / 勿声称」。
- **UI**：去掉或降级「已生图·存工作区」独立步骤呈现，改为普通工具调用样式；用户看图只靠回复气泡挂图。
- 测：成功挂上、失败诚实、无独立生图组件冒充交付；改旧「generated 不进聊天」断言。

## 不做

- 新 generate 工具；历史谎称迁移；设置页大改；群组挂图；push；往开发群发消息。

## 交付

- 分支 `feature/imggen-attach-bubble`；本文件一并提交
- 报告 `~/.xbot-build/imggen-attach-bubble/encode-report.md`
- `botmsg send --name XBot执行官`

## 还没做的

- 执行官复审及合入；主人真实账号日用验收。
- 本分支仅以 fake/scripted 验证，不代表真实供应商生成质量或日用通过；本文件按交付要求保留，未迁入 guard。

**冒烟用例**：`e2e/smoke/image_gen_test.go`（真实 serve、HTTP、OAuth 传输，厂商 scripted）；`internal/web/imggen_discover_browser_test.go`（本机 Chrome、真实页面，HTTP scripted）。
