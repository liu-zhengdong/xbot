---
kind: part
parent: README.md
verified: 2026-10-18 @ c1efca4
---
# 网关（设计已定，M8 实现中）

> **30 秒版**：开源可自建的信箱中继。主人自己的几台 XBot 只向外连网关，把发给另一台设备上 Bot 的消息存进网关、由对方取走；网关不存记忆，不同步文件。规格见 [GATEWAY.md](../GATEWAY.md)，取舍见 [ADR 0010](../adr/0010-gateway-mailbox-relay.md)。

## 边界
负责：同一个主人的几台安装之间的连接、身份、配对和消息中继。不负责：Bot 之间消息的语义（还是 [多 Bot](multibot.md) 里的 `message_bot`），机器上的远程 shell（见 [机器与设备](machines.md) 的 `node`），跨用户通信。

## 怎么运转
- 尚未实现，按 [GATEWAY.md](../GATEWAY.md) 的 G1–G7 分批落地，每批落地后更新这一节。
- 网关只转发签名的信封，取走确认即删；离线时代存，有条数、大小和 7 天期限。
- 地址写成 `bot@设备`，设备名是配对时本机给对方起的名字。
- 早先设想的 P2P 优先、Workers 部署、模板分享和跨用户通信，这一版不做，理由见 ADR 0010。
