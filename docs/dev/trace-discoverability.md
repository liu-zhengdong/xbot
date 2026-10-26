# 轨迹可发现与回放

> **结论先行**：不另起轨迹账本。统一约定并露出已有落盘——`bots/<name>/trace.jsonl`（用量/熔断）、`bots/<name>/transcript/`（对话+工具步，UI「轨迹」同源）、`bots/<name>/agents/<id>/transcript.jsonl`（子代理）。本批做**路径可自述 + CLI/脚本可搜可回放**；**漂亮 UI 回放后做**。
>
> 设计师半页真源：Obsidian `设计/轨迹可发现与回放.md` · box `xbot-ui-design/trajectory-discover/`（handoff 验收人话）。一句话：**问得到真路径；脚本能列能回放；Tab「轨迹」+空态定死。**

## 背景

政东：希望 XBot 能回放自己的轨迹——至少 Agent 知道轨迹文件在哪，好写脚本搜索/回放/复盘。候选 `轨迹可发现与回放`（P1 / Ultra 轨 A）。与「轨迹显示子代理步骤」「xbot chat 回放」「逐字重放请求」分界，不并入。

## 设计师验收人话（本批 PASS 标准）

1. 问 Bot「轨迹在哪」→ **真路径**，可读到本轮记录。
2. 路径不瞎编；禁止假目录 / 「大概在某某处」。
3. 新跑一轮，落盘符合约定。
4. 脚本按会话/时间能列出并回放工具步（CLI 即可）。
5. 面板 Tab 名固定「**轨迹**」；空态「**没有轨迹**」+「**它每做一件事，都会在这里留下一行。**」（与 step2 同族）。**本批勿改这些文案**——核对仍在即可。
6. 任务卡「在轨迹里看」链保留（切 Tab）；不另起同权入口。
7. atlas 说清与 transcript / `trace.jsonl` / 子代理目录关系——主人问「轨迹」只对上一套说法。
8. 面板没有花式回放器**不挡** PASS。

## 要干啥（实现）

1. **稳定约定（写进 atlas）**
   - 遥测：`{root}/bots/<name>/trace.jsonl`（`Bot.TracePath()`）
   - 回合轨迹（工具步）：`{root}/bots/<name>/transcript/`（UI「轨迹」同源）
   - 子代理：`{root}/bots/<name>/agents/<id>/transcript.jsonl`
   - **扩展现有入口，禁止第二套轨迹文件**。

2. **可自述**
   - 环境注入绝对路径（`paths`：trace / transcript / agents / bot），与 `Paths()`/`TracePath()` 一致。
   - Bot 答路径用人话指路 + 真绝对路径；禁界面堆砌 jsonl/blob/transcript 当主文案（CLI/atlas 可用工程名）。
   - `read_file` 沙箱在 WorkDir；读轨迹用 shell / `xbot trace` / 脚本。

3. **可脚本化（CLI）**
   - `xbot trace paths [bot]`
   - `xbot trace list [bot] [--since|--until|--kind]`
   - `xbot trace replay [bot] [--from|--to]`（复用 `ExportTo`/`TranscriptExport{Tools:true}`）

4. **UI（本批仅守成）**
   - **不要**改 Tab 名、空态、任务卡链；不要做花式时间线/播放器。若发现文案已被改歪，只恢复到上表定稿。

## 底线

- 不做 UI 时间线 / 播放器 / 子代理嵌套展开。
- 不碰 `feature/agent-todo-tool`、`feature/account-menu`、imggen/skills 在飞分支。
- 不另起 `trajectory.jsonl` 主账本；不 push；不改 CLAUDE.md / AGENTS.md / lock。
- 假路径不算过。

## 样例正反手

正：问「轨迹在哪」→ 真绝对路径；`xbot trace paths/list/replay` 通；Tab/空态仍是「轨迹 / 没有轨迹 / 它每做一件事…」。
反：编路径；第二套轨迹文件；只做网页播放器；改掉 Tab/空态文案；动 Todo/账号菜单。

## 测

- `go test -count=1` 改到的包（`./internal/bot/` `./internal/kernel/` `./cmd/xbot/`）
- 可选：grep 静态确认 Tab「轨迹」与空态两句仍在 `internal/web/static/app.js`
- 报告：`~/.xbot-build/trace-discoverability/encode-report.md`；`botmsg send XBot执行官`

## 本批不做

- 面板内打开/回放一段的完整播放器；气泡旁一键回放；子代理步骤嵌时间线；公网发版。

## 还没做的

- 分支合入主干与主人日用验收：按上述正反手确认模型能实际自述路径并用于复盘；独立冒烟验收待执行官安排。
- 获主人认可后，将旅程及独立冒烟移入 guard，再删除本设计。
