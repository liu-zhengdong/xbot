# 生图工作区可发现 + 生图头像热修余量（同波）

> **一句话验收**：生图后人找得到工作区、嘴不说消息里有图；换头像当面已换就说已换；状态条短、关于对 tip、脚注一行。

设计真源（半页完成标准，已定·够开干）：
- box `/workspace/xbot-ui-design/image-gen-followups/`（README · ia · handoff）
- Obsidian `正在干什么/06-XBot/设计/生图工作区与热修余量.md`
- 候选 `生图工作区可发现` · `生图头像热修余量`；对齐 H11（图进工作区不进气泡）

## 背景

主路径 PASS（`50a5d01` ⊃ `7e436bd`）。设计师再逛轻别扭同波收：工作区入口可发现、交付话术不谎称气泡有图、换头像禁 next-wake、状态条短摘要、关于页对 tip、「用哪家」脚注压短。

## 怎样算对（handoff 原文）

1. **工作区入口可发现**：生图成功后，人能从 UI 一键（或等价清晰路径）打开/看见产物，不必翻磁盘猜。
2. **话术不撒谎**：Bot / 状态条不说「图在消息里 / 气泡已挂图」；说清已存工作区并指向入口。
3. **换头像当面话**：侧栏/顶栏已刷新时，助手不提「下次唤醒」；说已换好一类。
4. **状态条短摘要**：默认不摊整段英文 prompt；扫一眼像回话；需要时仍能看完整 prompt。
5. **关于对 tip**：关于页 / 账号菜单旁版本与运行 tip（或 build-info）对齐，可看见/可复制短号；勿只裸 `dev`。
6. **「用哪家」脚注短**：压到可读一行级（或一行主句 + 可展开）；绑定账号/模型人对齐的人话保留，删说明书腔。
7. **H11 不回退**：图仍进工作区、不进气泡；已 PASS 热修不回退。
8. **挂图另条**：未接通「Bot出图到聊天」前，禁止假装气泡已挂图。

## 文案样例（ia）

| 位置 | 文案 |
|---|---|
| 生图成功回执 | 图已存到工作区（可点入口打开）；**禁**「消息里有图」 |
| 换头像回执 | 已换好 / 侧栏已更新；**禁**下次唤醒 |
| 状态条 | 已生图 · 存到工作区（+ 极短主题）；完整 prompt 可点开 |
| 用哪家脚注 | 一行：画好的图进工作区，不会出现在气泡里。（详可展开） |

## 并行约束

- 错开 `feature/account-menu` 底栏账号菜单热文件。关于页 tip 若大改 `app.js` 底栏/账号菜单结构则先跳过，其余做完；若 tip 注入只动 `xbot-dev`/session API/极小版本文本节点且不碰底栏菜单，可做。

## 底线 / 非目标

- 不重做 hosted 生图主路径 / 设置页结构；不把图塞进气泡。
- 不回退已 PASS：开关同步、用哪家人话、气泡无残片、图进工作区、已登录态、头像当面刷新、tip 文件一致。
- 不碰其它在飞分支；不改 CLAUDE.md / AGENTS.md / lock / 发版脚本（`scripts/xbot-dev` 为 tip 注入可动）。

## 落点提示

- `internal/web/static/providers.js` `step()`；`image-gen.js` 脚注
- `app.js` 关于/账号版本、`openWorkdir`（少碰底栏菜单）
- `displayVersion` / `internal/version` / `scripts/xbot-dev` tip
- `kernel/profile.go` + `link/grants.go`；`provider/image_gen.go` 回执
- `open_workdir*` / `work.WriteGenerated` / generated 入口

## 测试

相关包 go test（勿 `-count=3`）：`./internal/web` `./internal/kernel` `./internal/provider` 等。覆盖：入口、话术、短摘要、tip、脚注、无 next-wake、H11。

## 交付

分支 `feature/imggen-discover-hotfix`；本文件一并提交；`botmsg send` → XBot执行官。

## 还没做的

- 待主管合入、设计师按本批浅深色截图复核，以及真实账号日用验收；编码自测不能替代这两级。
- 本批关于页显示和复制 tip；账号菜单底栏版本文本留给 `feature/account-menu` 合入后衔接（按主管并行约束，未改菜单结构）。
- 真模型措辞、真实桌面文件管理器打开和线上性能未验；本批以 fake/scripted API、Chrome 交互与 OpenPath 回调测试交付。
