---
kind: adr
guard: owner
status: Accepted
date: 2026-10-18
---
# 0011 官方网关跑在 Cloudflare Workers 上，客户端默认连它

## 背景
[ADR 0010](0010-gateway-mailbox-relay.md) 定了跨设备靠信箱中继，代价是"需要一台公网可达的机器跑网关"。普通用户没有这台机器，装好两台 XBot 也配不上对。主人 2026-10-18 定了：我们自己部署一个官方网关，XBot 默认连它，装好就能配对；网关仍然开源，想自建的人在配置里改地址。

## 考虑过的方案
- **共享电脑上跑 Go 网关，用 Cloudflare Tunnel 暴露**：依赖这台电脑一直开着，单点。
- **Workers + Durable Objects**：不依赖任何一台机器，TLS 和全球接入由 Cloudflare 负责，Free 计划够个人用量起步。
- **只给 Go 网关，不做官方部署**：自建门槛留给每个用户，配对体验差。

## 决定
- 官方网关是 `gateway-worker/` 里的 Worker，部署在 `https://xbot.babelgo.cn`。每个收件安装一个信箱 Durable Object，全局一个目录 Durable Object 记安装和配额。
- Go 的 `xbot gateway serve` 保留，作为自建方案。两份实现说同一套 HTTP 协议，必须都通过 `internal/gateway/conformance_test.go`（`XBOT_GATEWAY_URL` 指向被测地址）。协议以 Go 为准。
- 客户端没配网关时用官方地址；配了就用配置里的。
- 端到端加密不变：网关只看到安装 id、时间、大小和密文。仍然不同步文件，每台安装只管自己的数据。

## 后果
- 换来：装好就能跨设备配对，不需要自己的公网机器。
- 代价：Free 计划每天 10 万次请求，一台一直在线的安装每天长轮询约 3,456 次，用户多了要换付费计划或改成更省请求的等待方式。官方网关的安装上限是 1000（Go 默认 64）。
- 官方网关是主人运营的服务，安装 id 和通信时间会经过它。

## 什么时候重新考虑
- 用量接近 Free 计划上限，或 Cloudflare 的条款、价格变了。
- 需要不依赖单一云厂商的官方网关。
