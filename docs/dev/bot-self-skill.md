# Bot 自装技能通路

> **结论先行**：对话里主 Agent（及需要时的 subAgent）能用 `write_skill` 把技能写进本机 owner 库 `~/.xbot/skills/<owner>/<slug>/SKILL.md`。对齐 ADR 0028：不再藏回前台闸。catalog / grant 文案写清本机路径，禁止「共享目录 / magpie」误导。

## 要干啥

1. **可达**：主线在已 grant `skills` 且本轮 `links load skills` 后可调 `write_skill`（沿用现有 lazy load；`read_skill` 在列表非空时仍可无 load）。禁止再引入 `foregroundHidden` 或同类主线闸藏 `write_skill`。
2. **subAgent**：`agenttype` 的 `dev` 白名单纳入 `read_skill`、`write_skill`（`general` 已是 `*`）。回归：Dispatch 开着时主线仍能拿到已 load 的 `write_skill`。
3. **catalog / grant 文案**：`internal/link/catalog.go` skills Summary、`grants.go` NameSkills、以及 `read_skill`/`write_skill` 工具描述里凡「共享技能目录」类措辞，改为本机 owner 库路径语义（`~/.xbot/skills/<owner>/…` / 「本 Bot 用户的技能库」）。勿再暗示 magpie 或跨产品共享目录。
4. **确认策略**：复用既有 `review` 表——`write_skill` 现为 `Normal`（测试 `TestSkillWriteDoesNotNeedApproval`）。**不要**擅自改成 SelfEdit / 强制确认卡。工具 Description 若仍写「须用户批准再写盘」须改成与 Normal 一致（直接写本机库；危险类仍由 review 表管）。
5. **文档**：本文件；必要时轻改 atlas 里与「写盘前确认卡」矛盾的一句（仅当本批已改代码口径时）。

## 验收

- 对话：grant skills → `links load skills` → `write_skill` → 文件出现在 `skills/<sole-owner>/<slug>/SKILL.md`。
- catalog Summary / grant 文案不含「共享技能目录」误导；点明本机 owner 库。
- `go test`（相关包，不要 `-count=3`）绿；有回归断言：Dispatch 开着时主线在 skills 已 load 后可见 `write_skill`（勿再 hide）。
- `dev` 类型 Allows `write_skill`（及 `read_skill`）。

## 底线

- 真写入本 Bot owner 库；假开关 / 仅 UI 不算过。
- 禁止恢复前台工具闸藏 `write_skill`。
- 不 push；不改 CLAUDE.md / AGENTS.md / lock（除非编译必需且只加一句）。
- 不碰并行特性文件（image-gen-settings、update_profile、fit/compact/env 上下文批）。

## 非目标

设置页导入 UI 重画；跨 owner 共享库；把 `write_skill` 改成默认免 load；自定义确认卡新机制。

## 还没做的

- 独立复审与合入主干。
- 从真实对话入口走通 grant → load → write_skill → 本机 owner 库落盘，并由用户日用验收；当前 scripted 内核回归不代替这一层。
