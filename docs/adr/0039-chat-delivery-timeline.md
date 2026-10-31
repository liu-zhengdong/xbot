---
kind: adr
guard: owner
status: Accepted
date: 2026-10-31
supersedes: 0038（主列数据源部分）
---
# 0039 主列读送达时间线，transcript 保留账本职责

## 背景

真实会话稀疏，工具记录会耗尽 ADR 0038 的 raw 条数和字节窗；继续扩大 transcript 窗口无法让主列稳定获得可见消息。政东确认主列与账本分开，并确认完整送达快照在整轮归组定稿后写入。

## 决定

- transcript 仍只追加；轨迹、模型上下文、recall、expand、导出和原 transcript GET/search 保持原职责。
- 原 SQLite 索引库增加可重建的 chat、chat_fts 与投影检查点。唯一追加入口为 `Chat.Record` 的幂等插入实现；`delivery_key` 重复不修改正文、附件或旧快照，不提供编辑 API。
- 主人话、主列卡和来源行确定时记录；成功 say 先留在待完成投影中，最终助手行到达时整批记录。一轮多 say 仍多行，图片、语音集中到最后一条，禁止先写文字后补附件。实时 SSE 不变，持久对账以 chat 为准。
- 主列只用 chat GET 的 before/after/around/limit 与 chat FTS search；游标/key 是 chat ID，transcript_seq 只作回链。默认 40 行、上限 200 行，HTTP 保持约 700 KiB 预算。既有 patch、stick、遮罩和附件组件复用。
- 首次读取/追加以及投影世代落后时按单一 Go 可见规则分块回填；旧无 say 的助手正文保留原截止时间。不改旧 jsonl；checkpoint 与新快照同事务提交。非主列 transcript Window 的可见预算复用同一规则，不再保留前端 spokenLines 主投影。
- peer 原文仍由原 API 负责；chat 中只保留来源展示快照。不引入会话折叠、context_edit、WS 或新的模型上下文机制，沿用 ADR 0021。

## 后果

索引可以重建，正常主列读取不再消费 raw 工具窗口。持久 say 在本轮归组定稿后才可见；此前 SSE 负责实时显示。这是明确的时间边界，不通过回写快照补附件。

ADR 0038 的 transcript 窗口、fork 回放和传输上限仍适用于非主列消费者；其主列使用 transcript/原 FTS 的决定由本文取代。
