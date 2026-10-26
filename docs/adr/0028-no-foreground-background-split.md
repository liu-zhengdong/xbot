---
kind: adr
guard: owner
status: Accepted（取代 0016）
date: 2026-10-26
---
# 0028 无前后台之分：主 Agent 全工具，长活直接 subAgent

## 背景
[ADR 0016](0016-foreground-dispatches-typed-subagents.md) 把主线定成「前台」：工具集在代码里限死，只能理解、派活、转告和改记忆；干活交给预制类型子代理。它解决了「主线上下文被长任务撑爆」和「模型自觉放后台不可靠」，但代价是：

- 主人眼里 Bot 经常「只会派活、自己不动手」，短改也要冷启动子代理。
- 前后台两套身份（工具闸、`foreground_limit`、Dispatch 强制短等）渗进内核、规则文案、测试和 atlas，认知负担大。
- 快速回复本可用 reminder「先回人再干活」解决，却靠砍工具硬逼，假开关式折中也不够。

2026-10-26 主人定稿：**无前后台之分**；限制代码全部删除。

## 考虑过的方案
- (a) 保留 0016，只把前台白名单放宽：半套身份还在，文档和测试继续分叉。不采用。
- (b) 配置项 default-off 关掉闸：假开关，代码壳还在。主人明确不算过。不采用。
- (c) **整块删除前台闸与前台硬上限**；主 Agent 全工具；长活仍用 `dispatch` 起 subAgent；快速回复改 system reminder「先回人再干活」。采用。

## 决定
1. **删除** `foregroundHidden`、按类砍工具、`foregroundRefuses`、前台步数/时间硬上限及其 cap 工具集与 `<foreground_limit>` 提醒；不留空壳。
2. **主 Agent 全工具**（与挂载目录一致）。`dispatch` 仍在：长活、并行调研、类型预算等直接起 subAgent，不再「必须因前台短派」。
3. **快速回复**：规则 / system reminder 要求先简短回主人，再自己干或派活；不靠工具集强制。
4. **ADR 0016 标 Superseded**。子代理类型、慢工具 Job、插话续跑（0017）保留。

## 后果
- 主线可以自己改文件、跑 shell、发 HTTP；上下文再次可能被长任务撑大——用「长活请 dispatch」的 reminder 与既有压缩/子代理缓解，不靠再砍工具。
- atlas / 测试 / 文案去掉「前台看不到干活工具」现行表述。
- 旧 transcript 与分叉只读路径不因本条强制清掉。

## Revisit when
- 主线上下文再次被长任务系统性撑爆，且 reminder + 子代理不够用。
- 主人要求恢复某种「主线只读」产品形态。
