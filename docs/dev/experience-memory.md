# 经历与资料

设计稿在用户的 Obsidian：`正在干什么/06-XBot/设计/经历与资料.md`（副本 `~/.xbot-build/experience-memory/design.md`）；决定见 [ADR 0030](../adr/0030-experience-and-reference.md)。分支 `feature/memory-reference-complete`。

## 怎样算对

- 经历记录：Agent 调 `experience_record` 能落库，带时间和 transcript 起止 seq；下一次发给模型的环境里能看到新经历。守护：`TestExperienceRecordShowsInNextEnvironment`。
- 资料：每个 Bot 看到自己的资料契约和索引，空库有契约；可选共享目录存在时两个 Bot 均能看到共享段。磁盘变化后刷新，不变不重发。守护：`TestReferenceEnvironmentPerBotAndOptionalShared`。
- 经历查阅：`experience_search` 只给经历摘要加原文段提纲，不带工具结果正文；`detail` 能按 seq、或按经历 id 加下标读出单条全文（含 blob）。守护：`TestExperienceSearchLeavesOutToolBodyAndDetailReadsIt`。

## 还没做的

- `expand` 并进 `detail` 与查阅后删除（`ReplyRule`、fit 占位文案里还在用 `expand`）。
- 待定：`detail` 的最终名字。经历库按现行决定每 Bot 一份。
- 真实模型下的记录密度没验：规则轻推够不够、会不会记太碎，要用户日用后再看。
