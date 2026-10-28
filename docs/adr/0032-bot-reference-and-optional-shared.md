---
kind: adr
guard: owner
status: Superseded
date: 2026-10-28
---
# 0032 资料默认归 Bot，可选跨 Bot 共享

> 共享目录改为始终可用，见 [ADR 0033](0033-bot-and-shared-reference-workspace.md)。

## 背景

政东于 2026-10-28 推翻 [ADR 0030](0030-experience-and-reference.md) 的整机共享资料默认：不同 Bot 的工作资料应各自归属，跨 Bot 的事实才需要共享。

## 决定

默认资料目录为 `bots/<bot>/memory/`，沿用现有 Bot 记忆目录；已有 `memory.md`、`log/`、索引和备份属于系统记忆，不作为资料索引或关键词来源。`<root>/memory/` 是可选的跨 Bot 资料目录，存在时才注入，不主动创建。每个来源独立渲染契约和根层索引，超 256 KiB 整源排除并说明。写入轻推默认指向本 Bot，跨 Bot 事实才写共享库。两源均可关键词提醒，同轮路径去重。

## 后果

经历仍每 Bot，压缩仍不写记忆或经历。旧 Bot/person 记忆继续走原快照和环境流；资料使用相同环境 `reference` 字段及已有提醒 sidecar，不增加写工具或迁移。共享目录已经存在时会被视作可选共享来源；不存在不影响 Bot 资料。
