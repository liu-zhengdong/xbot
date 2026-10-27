---
kind: adr
guard: owner
status: Proposed
date: 2026-10-27
---
# 0030 记忆拆成经历与资料：经历由 Agent 自记，压缩不写记忆

> 草稿。设计稿：Obsidian `正在干什么/06-XBot/设计/经历与资料.md`（副本 `~/.xbot-build/experience-memory/design.md`）。批 B、C 在 `feature/experience-memory` 落地，批 A 的「压缩不写记忆」以 restore-bot-compact 和 [ADR 0021](0021-env-stream.md) 的修订为准。

## 背景

现有记忆只有常驻的 `memory.md` 加日志层（[ADR 0008](0008-memory-v2-dated-log-first.md)、[ADR 0009](0009-memory-as-folder.md)），「长期还能用的事实」和「这段时间发生过什么」挤在一处；压缩前抽事实又让压缩和记忆缠在一起。对照 dsh-recall 的每 turn 一条 Run：密度太高，不照搬。

## 决定

1. **两层**：资料是长期事实、约定和项目知识，同一数据根的所有 Bot 共享 `<root>/memory/`，Bot 用文件工具写，根层索引通过环境流注入；旧 Bot/person 记忆过渡期仍可注入。经历是可检索的「发生过什么」，锚到 transcript。
2. **经历由 Agent 自觉记录**：`experience_record` 写一条摘要，可选 focus 与起止 seq；内核补时间，默认锚从最近一条用户消息到 transcript 尾，并存一段不调模型的原文段提纲（只有发言与工具名，不含工具结果）。没有闲置封口、压缩封口或系统兜底，漏记可接受。
3. **存储**：每个 Bot 一个 `bots/<bot>/experience.sqlite`（`modernc.org/sqlite`）。表 `experiences(id, created_at, summary, focus, from_seq, to_seq, segment)`。
4. **轻推只用常驻规则**：`ExperienceRule` 随记忆链接挂进规则段，与 `DispatchRule` 同一种做法；不往每条消息里塞新的 system-reminder。
5. **环境流提示**：环境采样带 `experience` 字段（总数与最新一条的 id 和截短摘要），新记一条后下一次发给模型时只追加这一项差异，沿用 ADR 0021，不加新机制。
6. **两级披露**：`experience_search` 默认返回经历摘要加原文段提纲，不带大工具结果；`detail` 按 seq（或经历 id 加 0 起下标）读单条消息全文，上限 64 KiB。
7. **压缩不等于记忆**：压缩只动摘要投影，不写资料，也不写经历。

## 为什么不

- 每 turn 自动归档：密度高，噪声压过信号。
- 记录时让模型生成段摘要：多一次调用且慢；提纲由内核从 transcript 现成拼出就够定位，细节靠 `detail`。
- 经历放 owner 共享：经历是这个 Bot 的工作经过，跟 transcript 同归属；共享资料库与经历分开。

## 后果

- 新工具 `experience_record`（Normal）、`experience_search`、`detail`（Readonly）进权限表。
- `expand` 暂时保留；设计里要并进查阅与 `detail`，留到后续清理，同时退役旧 extract 路径；当前分支的旧抽取仍可能写旧 Bot/person 记忆。
- 旧 Bot 不需迁移；第一次记录时才建库。
