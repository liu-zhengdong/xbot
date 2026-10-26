# 生图挂气泡（Bot 主动挂图）

> **结论先行**：厂商 hosted 生图只落盘 `generated/` 并回真实路径；**交付靠 Bot 调 `notify_owner` 的 `images`** 挂进本轮回复气泡。禁止内核在 Save 里自动 `attachOutImages`，禁止引导文案说「自动挂 / 不用额外调方法」。候选：`正在干什么/06-XBot/候选/生图挂气泡热修.md`。

## 背景

上一版（`b389806`）在 Save 钩子里自动 `attachOutImages`，并在 `imageDeliveryGuidance` 教模型「已自动挂本条」。政东手机真聊纠正：出图应是 **Bot 主动挂图**；自动挂版收口作废。公网壳走设备 pipe 单源（ADR 0029 已废弃 sync）；收口不跑 sync-app-from-xbot。

## 关键点（只钉这些）

1. **主动挂图**：生图落盘后，Bot 调现有 `notify_owner`（`images` 参数，工作区路径）把图挂进本轮回复气泡。不要内核 Save 自动 attach；不要教模型「自动挂」。
2. **回执/引导**：工具回执只报真实落盘路径（及保存失败原因）。成功交付话术以「已用挂图方法挂上」为准。改写 `imageDeliveryGuidance`（`internal/provider/image_gen.go`）：引导模型先看回执路径，再用 `notify_owner` images 挂上；禁止自动挂说明。
3. **呈现**：生图仍是普通工具轨迹（`PartHosted` / 轨迹行）；用户只在回复挂了图时看见图。不另起 generate 专工具，不恢复独立「已生图·存工作区」UI 组件。
4. **公网壳**：走设备 pipe 单源；**不要**再跑 `sync-app-from-xbot`（ADR 0029 已废弃）。编码收口=本 feature 分支提交代码与设计短文、写 encode-report 并 botmsg 报 tip；独立复审 PASS 后由执行官合入 main，编码 Agent 不重启 8788。

## 怎样算对

- 正手：用户要画猫 → 生图回执给 `generated/….jpg` → Bot 调 `notify_owner` images → 回复气泡 gallery 有图 + 实话。
- 反手：继续 Save 自动挂，或声称「已挂本条」但气泡无图 / 编假路径。

## 底线

- 不 push；Ultra 仍关；不借机开别的槽
- 不改 CLAUDE.md / AGENTS.md / lock / 发版脚本（除非需求要求）
- 无 UI 文案大改可不喊设计师；只在 feature 分支报 tip；独立复审 PASS 后由执行官合入（勿 sync 公网）

## 实现落点（给编码自补细节）

- 去掉 `internal/kernel/image_gen.go` Save 内对 `attachOutImages` 的调用；Save 只 `WriteGenerated` 回相对路径（可恢复 `func([]byte)` 签名若不再需要 callCtx）。
- 回执文案：去掉「已挂本条回复」；保存成功只报路径；挂图成败由 `notify_owner` 回执体现。
- 重写 `imageDeliveryGuidance`；同步 atlas kernel/providers/ui 与设置脚注里「自动挂」表述。
- 测试：覆盖「Save 不自动进 outImages / Entry.Images」；保留/加强「notify_owner images 后气泡有图」；更新依赖自动挂假设的用例。

## 还没做的

- 独立复审 PASS 后，由执行官合入本地 main 并安排启动 8788、核对运行 tip；公网界面通过设备 pipe 读取内嵌资源。
- 主人用真实模型验收主动挂图话术与气泡；本批代码测试只用 fake/scripted。

**冒烟用例**：`e2e/smoke/image_gen_test.go`（保存无图 / 主动挂图两路），`internal/web/imggen_discover_browser_test.go`（gallery 与普通工具轨迹）。
