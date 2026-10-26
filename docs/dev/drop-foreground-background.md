# 推翻前台后台 · 删限制

> **结论先行**：无前后台之分。主 Agent 拿全套工具；长活直接 `dispatch` 起 subAgent；快速回复靠 system reminder「先回人再干活」，不再靠工具闸 / 前台步数上限硬逼。新 [ADR 0028](../adr/0028-no-foreground-background-split.md) 取代 [ADR 0016](../adr/0016-foreground-dispatches-typed-subagents.md)。

## 要干啥

1. **整块删除**前台工具闸与前台硬上限（假开关 / default-off 不算过）：
   - `foregroundHidden` / `foregroundKept` / `workClass` 过滤
   - `foregroundTools` / `foregroundRefuses` / `errForeground`
   - `foregroundCap` / `cappedTools` / `capTools` / `capNote` / `capGrace` 及 `<foreground_limit>` 提醒
   - `DispatchRule` 里「你是前台、自己不干活」那套措辞（改为「有长活就 dispatch；先回人再干活」的 reminder，见下）
2. 主线 `toolsForScope` / `Kernel` 工具组装：**不再**因 `Dispatch` 砍工具；主 Agent 与挂载目录一致（全工具）。
3. **长活直接 subAgent**：`dispatch` 保留；主 Agent 可自己干短活，也可派长活。不再因「前台到上限只剩 dispatch」强制派活。
4. **快速回复**：加（或改）一条 system reminder / 规则——**先回人再干活**（短答主人，再动手或派活）。可与本批同交或紧随；本批至少要模型侧能看到这条 reminder，旧「前台限死」文案不得残留为现行。
5. **文档**：新 ADR 0028（Accepted，取代 0016）；0016 frontmatter `status` → `Superseded by ADR-0028`；`docs/atlas/subagents.md`（及 README / kernel / ui / fork 里仍把 0016 当前台限死现行的句子）改成新行为。`docs/dev/drop-foreground-background.md` 本文件随合入可留到验收后由执行官删或迁 atlas。

## 验收

- 代码：无 `foregroundHidden`；`foregroundTools`/`foregroundRefuses`/`foregroundCap`/`cappedTools` 及对应测试断言「前台看不到干活工具」删除或改成「主线有全工具」。
- `Dispatch` 开着时主线仍能调用写文件 / 非只读 shell / http 等（守门测反过来或新测覆盖）。
- 模型 rules / reminder 含「先回人再干活」；不再注入旧 `DispatchRule` 的「你不自己干活」。
- `go test ./internal/kernel/...` 及相关 bot/web 测全绿。
- ADR 0028 在仓；0016 标 Superseded；atlas 不再把 0016 当现行前台限死。

## 底线

- **整块拿掉**，不留配置项「default off」、不留空壳函数只 return list。
- 不 push；不动 xAI / 气泡 Markdown / 人设种子并行特性。
- 不改 `CLAUDE.md` / `AGENTS.md` / lock，除非本条编译或入口必需且只加一句。
- **保留**（本批不删）：`dispatch` / 子代理类型 / 慢工具 Job 后台化 / ADR 0017 插话续跑 / 分叉代码若仍服务旧 transcript（`AllowFork` 可仍关）。本批只砍「主线因前台身份被限工具/短等」。
- `ForegroundMaxTools` / `ForegroundMaxTime`：若仅服务前台 cap → 随 cap 删除（配置字段、profile、role 默认、CLI、测试一并）；若还有别的语义先在提交说明写清再决定，默认倾向整删。

## 对照回顾（短）

| 材料 | 关系 |
|---|---|
| ADR 0016 | **取代**：前台只派活 → 无前后台；主线全工具 |
| ADR 0017 | **保留**：插话直接进轮，不分叉 |
| ADR 0014 | **无关**：已删后台续段 |
| atlas `subagents.md` | **重写**「前台只派活 / 工具按类限死」两段 |
| 候选 `推翻前台后台-删限制.md` | 本条真源 |

## 非目标

xAI OAuth 详情 UI、气泡 Markdown、人设卡种子、改候选优先级、push、假开关留壳。
