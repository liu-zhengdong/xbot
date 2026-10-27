---
kind: adr
guard: owner
status: Accepted
date: 2026-10-18
---
# 0012 退役「节点」，多台电脑只靠网关配对

## 背景
旧的「节点」是一套独立的多设备机制：`xbot node serve/add/list/remove/call/pending/approve` 用 bearer token 在 tailnet 上互连（默认端口 18772），Bot 装上 `node` Link 后有 `node_list`/`node_call`，可以在另一台机器上跑 shell，`ask` 时命令进节点待批队列。`internal/nodesync` 在这些节点之间自动同步人的身份和人的记忆，另有 `xbot sync export/merge`。它和 [ADR 0010](0010-gateway-mailbox-relay.md) 定的"不做文件同步，每台安装各管自己的数据"冲突，ADR 0010 把它留成待定项。用户 2026-10-18 定了：整套退役。

## 考虑过的方案
- **保留节点，只去掉记忆同步**：仍然是两套多设备机制，用户还要学一套多余的东西。
- **把远程 shell 搬进网关配对**：网关配对有意不提供远程执行，搬进去等于把攻击面请回来。
- **整套退役，多设备只留配对**：用户选这个。

## 决定
- `xbot node`（全部子命令）、Bot 的 `node` Link 和 `node_list`/`node_call`、网页的 `/api/node/pending` 和侧栏里远端 Bot 的状态汇总、`internal/node`、`internal/nodesync`、`xbot sync export/merge` 都删掉（PR #190 起）。
- 多设备只剩网关配对的「设备」：唯一能力是 Bot 之间发消息（`bot@设备`）；用户给另一台电脑上的 Bot 发消息在 G7 线里做。
- 升级不丢数据：数据根里还有 `nodes.json`、`node-token`、`node-pending/` 时，`serve` 启动只打一行日志说忽略，从不删除；Bot 还挂着 `node` 授权就什么都不装，只打一行日志。
- `xbot node …` 和 `xbot sync …` 直接说明已移除，并指向「设备」配对（`xbot remote invite` / `xbot remote join`）。
- 人的身份和记忆文件（`people/<id>/identity.jsonl`、`people/<id>/memory/sync.jsonl`）留着，就是本机自己的存储；`node-id` 有的话只当这些行上的设备标记读。
- 本 ADR 取代 [GATEWAY.md](../GATEWAY.md) 里"现有 `node` 不动"那条，并给 ADR 0010 的待定项收口。

## 放弃了什么
- 一台机器上的 Bot 远程操作另一台机器：这正是网关配对故意不提供的。
- 人和记忆在多台电脑之间自动对齐，以及手工的 `sync export/merge`。
- 侧栏里同时看到所有节点上 Bot 状态的能力。

## 后果
- 多设备只剩一个概念：设备。界面只用「设备」，现行文档不再把「节点」当功能讲。
- 两台电脑各自记用户的事，会有出入，靠 Bot 互相问来弥补（ADR 0010 已经接受）。
- 没有一条隐式的远程执行通道；要在一台机器上做事，就让那台机器上的 Bot 做。

## 什么时候重新考虑
- 消息之外的跨设备能力（比如同步某个具体文件、在另一台机器上跑一次命令）反复被需要，并且能设计出不给所有设备一个远程 shell 的做法：另开 ADR，在配对的身份上做。
