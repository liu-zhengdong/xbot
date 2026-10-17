---
kind: part
parent: README.md
code: [internal/gateway/, internal/remote/]
verified: 2026-10-18 @ c1efca4
---
# 网关（M8，G1–G2 已实现）

> **30 秒版**：开源可自建的信箱中继。主人自己的几台 XBot 只向外连网关，把发给另一台设备上 Bot 的消息存进网关、由对方取走；网关不存记忆，不同步文件。规格见 [GATEWAY.md](../GATEWAY.md)，取舍见 [ADR 0010](../adr/0010-gateway-mailbox-relay.md)。

## 边界
负责：同一个主人的几台安装之间的连接、身份、配对和消息中继。不负责：Bot 之间消息的语义（还是 [多 Bot](multibot.md) 里的 `message_bot`），机器上的远程 shell（见 [机器与设备](machines.md) 的 `node`），跨用户通信。

## 怎么运转
- **网关**（`internal/gateway`，G1）：`xbot gateway serve [--addr 127.0.0.1:8790] [--dir DIR] [--open]`，数据目录默认 `<root>/gateway`。加入口令只从环境变量 `XBOT_GATEWAY_TOKEN` 读，不打印；没有口令又没给 `--open` 就拒绝启动。注册写 `installs/<安装 id>`；信封存 `queue/<收件>/<发件>/<信封 id>.json`；确认过的 id 在 `acked/<收件>/` 留 10 分钟，期间重放同一封只回 duplicate。配额：每对发件和收件 64 封、1 MiB，每个收件 512 封、8 MiB，全网关 256 MiB（不够时先清掉所有过期信封再判），超了回 507。长轮询每个公钥只留最新一个，旧的立刻返回空；一次最多 32 封、768 KiB。HTTP 服务带读写超时。网关自己不写日志。
- **身份与设备表**（`internal/remote`，G2a）：私钥 `keys/remote/install.key`，设备表 `remote/devices.json`，收过的信封 id 在 `remote/seen.json`（保留 7 天、最多 4096 个）。这几处在内核受保护路径里，Bot 读写都出确认卡。`xbot remote id` 打印本机 id 和公钥，`remote gateway URL` 设网关，`remote add <名字> <公钥> [--person ID]` 记下对方（本机只有一个人时默认绑他），`remote list`、`remote remove`。配对目前要两边各 `add` 一次，邀请码在 G3。
- **收发**（G2b）：`message_bot` 的 `bot` 写成 `bot@设备` 就走网关。发出前要求发件 Bot 的 owner 含这台设备绑的人，正文过脱敏，跳数和每对每分钟 8 条的限流照旧。serve（以及桌面端里的 serve）起一个收信循环：没设网关时每 30 秒看一次配置；第一次连某个网关时用口令注册；之后长轮询 25 秒，出错退避 1 秒到 60 秒。收件核对 `to`、配对公钥和签名、7 天时间窗、见过的 id、跳数、目标 Bot 的 owner 和收件侧限流，合格的变成来源 `bot:<bot>@<设备>` 的不可信事件进收件箱，正文里的 `untrusted_event` 标签被中和。不合格的确认后丢弃；本机存储出错的不确认，下次再取。口令读到后从进程环境里删掉，Bot 的 shell 看不到，优雅重启仍带着它。
- **端到端**：`scripts/e2e-gateway.sh` 在一台机器上起假模型、网关和两个数据根的两个 serve，A 上的 ada 发给 `bob@b`，B 上的 bob 回 `ada@a`。盒子上实测（假模型，serve 间隔 1 秒）发出到收到回复约 2.0 秒，大部分是 serve 的轮询间隔。
- 还没做：信封加密（G4，之前网关看得到 Bot 名和正文）、发件侧离线重试和退信（G5）、`list_bots` 看见别的设备（G6）。早先设想的 P2P 优先、Workers 部署、模板分享和跨用户通信，这一版不做，理由见 ADR 0010。

## 坑
- `remote.Identity` 不能再包一层 `keys/remote/install.key.lock`：`LoadOrCreateKey` 已经拿着这把锁，flock 按打开的文件算，同一进程再开一次会永远等下去。
- 同一个数据根上跑两个 serve，两个收信循环会互相顶掉长轮询；不支持这种用法。
