# 上下文一批 · 批 B

设计真源：主人 Obsidian `正在干什么/06-XBot/设计/上下文一批.md` §4–§5；2026-10-26 编码授权。本批是同一工程切片，中间批不找主人验收。

## 对照回顾

沿用真源对候选的统一回顾：环境当前值、旧工具结果占位、空闲压缩、换小窗口四条同属本批；轮内先摘要是旧结果清理的可选子项。ADR 0021 的对话串、环境流、插话口不变；ADR 0015 的 T/H、atlas/context 的追加式 transcript、现有摘要队列与 expand 全部复用。guard/长程运行和六条基本旅程不能退化。context-dedup 的 overlay/todo/fork_end、批 A 的 StablePrefix 和批 C 的人设规则不在本批。

## 实现规格与怎样算对

| 项 | 规格 / 验收 |
|---|---|
| ① B1–B3 | 模型请求中的环境投影以字段当前值为准；同分钟重复唤醒不重发 time；变化只追加；待批卡清空显式 null；摘要或轮内折叠后重采样完整快照，之后继续按差异发。 |
| ② B5 | 跨轮组装时，新的真实输入之前的大工具结果（≥512 估算 token）提前换 expand 序号占位，不等 H，不调模型；工具配对、写记忆的环境回执和 transcript 原文保留。 |
| ③ B7 | serve 已有巡检在持有空闲工作锁时检查，达到 token 门槛和闲置时长就进入既有后台摘要队列；摘要工作不持工作锁，不挡新对话。默认最小 16000 token；XBOT_IDLE_COMPACT_MIN_TOKENS 支持 false/off/0 关闭。XBOT_IDLE_COMPACT_AFTER 默认 50m，可用 Go duration 配置或 false/off/0 关闭。这是部署策略预算（给一小时缓存预留约十分钟），不是供应商 TTL 保证。 |
| ④ B8 | 换绑在工作锁内用新窗口 T/H 预检；超过新 T 时复用自动压缩范围及摘要，压完复测；T 是目标，H 是拒绝边界；没有可压范围但低于 H 允许切换。无法承载则 409 明确提示「上下文仍超新窗口，需先删分支/导出」，不写新绑定。压缩失败保留原绑定，已提交摘要仍有效。 |
| ⑤ B6 | XBOT_FIT_SUMMARY_BEFORE_OMIT 默认 false；true 时过 T 且即将省略大工具结果，先用现有 foldTurn 从原始内容生成 turn_progress；失败回落已有 omit/硬上限路径。默认关闭与原轮内路径一致。 |

## 非目标与待决取舍

实体自注册 §4.2/B4 属批 D，不加接口桩；不改前缀分段、人设、协议适配器、UI、手动压缩入口。跨轮只改变请求投影。空闲阈值与等待时间可关闭；轮内先摘要保守默认关闭，真实任务质量/费用对比后才考虑默认开启。换绑优先安全拒绝不能压到新 T 的上下文，不强截尾部；输出预留沿用 compact.Lines。官方 [Anthropic 缓存文档](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) 同时有默认 5m 与可选 1h，50m 不是通用 TTL：短缓存部署须调整 AFTER；本批不修改供应商缓存协议。

**冒烟用例**：`e2e/longrun_test.go::TestLongRunJourney`（B9）；B1–B8 定向行为测试见交付报告。


跨轮占位只在 `Kernel.assemble` 请求出口调用 `omitPastResults`，不进 `projected()`；fork handoff / 环境回放仍看原文（见 `TestBatchBForkHandoffKeepsPastToolBodies`）。

## 还没做的

- 独立复审、合入 main、主人日用与真实供应商缓存/摘要质量对比；本批交付不声称这些已完成。
