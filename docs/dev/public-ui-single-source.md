# 公网 UI 单源 · 跟设备 tip

Obsidian 真源：`正在干什么/06-XBot/设计/公网UI单源.md`
设计半页：`/workspace/xbot-ui-design/public-ui-single-source/`（README · ia · handoff）
候选：`候选/公网UI单源-跟设备tip.md`

## 心智（一句话）

公网打开登录后 = **当前配对设备上正在跑的那一版**。不是云上另存的旧壳；禁止「记得 sync」当长期方案。

## 要做什么

登录后壳层 tip 耦合静态从**配对设备**经 ADR 0024 **pipe** 取；账号 API 仍云上。废止「sync-app → sync-public」当权威路径。

## 怎么做（主管钉死的主方案）

1. **设备**：`GET /api/ui/static/<path>` 读 tip 内嵌 `staticFS`（路径白名单、防 `..`、ETag、`X-Xbot-Tip` 或等价 tip 标识）。
2. **管道**：静态 GET 走现有 Do；体限仍 `MaxBody`（700 KiB）。大字体 / 少变 vendor 大件**优先仍放云上缓存**（不经 tip 编码进每帧）。
3. **账号仓 bridge**：云上只留登录薄页 + **最小引导壳**（`pipe-bridge` 与必要 boot）。选设备建 pipe 后，壳层业务资源（`index.html` / `app.js` / `app.css` / 面板脚本等 tip 耦合文件）经管道 `GET /api/ui/static/…` 拉；`/app/…` 不再以云上第二份业务 static 为权威。
4. **多设备**：跟**当前选中/配对那台** tip；切换清缓存重拉。
5. **文档**：ADR 记单源路径；`sync-app-from-xbot` / `sync-public` 标**废弃常态**（仅应急允许，不得当验收路径）。

## 怎样算对（对齐设计师 handoff）

1. 配对设备 tip 合入新厂头/新菜单后，公网硬刷登录进主界面能看到同一改动——**不必**再跑手工 sync。
2. 云上第二份 static **不是**权威；临时 sync 只救急。
3. 多设备时画面跟当前选中设备 tip。
4. 登录叙事不变：官方入口 → 同款主界面（侧栏 Bot + 对话）。
5. 无设备：空态引导去电脑开 XBot；不装假主界面。
6. 设备离线：灰态说清「电脑上的 XBot 没开」；不静默塞过期壳。
7. 关于/版本能对上设备 tip（勿公网永远只写 `dev`）。
8. 账号 API 可仍云上；本条只收壳跟 tip。

> 一句话验收：公网打开 = 看到配对设备上那一版。

## 不做

- 反代 8788 整站；设备公网自托管静态作默认。
- 把账号库塞回官方网关。
- 另起简化灰壳 / 另做公网薄聊天壳。
- 把「手工 sync」产品化成设置项。
- 工程词进界面（pipe / tip hash / rsync 路径）。

## 冲突面

`xbot-account` bridge / OpenResty；`internal/web` 静态出口；pipe 体限。与改业务面板的 `app.js` 特性（account-menu / agent-todo / trace / imggen / skills）**错开热文件**：本批优先动出口与 bridge，少改 `app.js` 业务逻辑。

## 验证

相关包 `go test -count=1` 一遍绿。本机 tip 合入后起 8788；公网入口按改后的发布路径验——**勿**再依赖手工 sync 当验收。


## 还没做的

- 两仓合入后，更新账号入口的最小引导与设备构建；本次不 push、不合 main、不起 8788。
- 在真实公网账号和当前配对设备上按上述八条标准验收；本地浏览器跨仓冒烟不替代公网/主人日用。
- 设计师确认连接状态、切换设备入口的浅深色截图；真实公网性能（启动、交互 p95）与帧率待测。

**冒烟用例**：`internal/pipe/browser_test.go` + `internal/pipe/testdata/browser-remote-ui.cjs`（设置 `XBOT_PIPE_BROWSER_APP` 指向账号仓 `public`；真实 Chromium、Go pipe，scripted 模型）。

**级别**：分支实现与本地验证，未合入、未公网端到端验收、未主人日用通过。
