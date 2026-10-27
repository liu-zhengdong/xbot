# 经历与资料

设计稿在用户的 Obsidian：`正在干什么/06-XBot/设计/经历与资料.md`（副本 `~/.xbot-build/experience-memory/design.md`）；决定见 [ADR 0030](../adr/0030-experience-and-reference.md)（草稿）。分支 `feature/experience-memory`。

## 怎样算对

- 批 B：Agent 调 `experience_record` 能落库，带时间和 transcript 起止 seq；下一次发给模型的环境里能看到新经历。守护：`TestExperienceRecordShowsInNextEnvironment`。
- 批 C：`experience_search` 只给经历摘要加原文段提纲，不带工具结果正文；`detail` 能按 seq、或按经历 id 加下标读出单条全文（含 blob）。守护：`TestExperienceSearchLeavesOutToolBodyAndDetailReadsIt`。

## 还没做的

- 批 A：「压缩不写记忆」以 restore-bot-compact 和 ADR 0021 修订为准，本分支不动 compact。
- 批 D：资料层文案与路径对齐「压缩不写」，旧 extract 路径退役；`expand` 并进 `detail` 与查阅后删除（`ReplyRule`、fit 占位文案里还在用 `expand`）。
- 待定：`detail` 的最终名字；经历库是否 owner 共享（现在每 Bot 一份）。
- 真实模型下的记录密度没验：规则轻推够不够、会不会记太碎，要用户日用后再看。
