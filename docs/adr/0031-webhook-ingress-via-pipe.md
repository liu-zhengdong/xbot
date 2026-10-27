---
kind: adr
guard: owner
status: Accepted
date: 2026-10-27
---
# 0031 Bot 入站 Webhook 经账号网关暴露公网地址

## 背景

Bot 的入站 webhook（`POST /webhook/{bot}/{name}`）只挂在 serve 自己的端口上，默认是 `http://127.0.0.1:8787`，外网服务打不到。用户要求：本机登录账号、连上网关后，界面给出的地址就是外网能打到的公网地址（政东 2026-10-27 [t1325u]）。现有 pipe（ADR 0024）只给主人用：领票、端到端加密，设备只放行 `/api/` 并以主人身份执行，第三方没法用它投 webhook。

## 决定

1. 中枢加一条公开入口 `POST /pipe/v1/hook/{device}/webhook/{bot}/{name}`。中枢按设备 id 找到它的待命连接，把请求作为文本控制消息 `{"t":"hook"}` 发下去，等设备回 `{"t":"hook_res"}`，把状态码和短正文还给发送方。
2. 设备只把 `/webhook/{bot}/{name}` 交给本机 inbound 处理器（`Device.Hooks`），**不带主人凭证**，也不进 `/api/`。签名或令牌照旧在本机用 Bot 目录里的密钥校验；中枢不存、也拿不到 hook 密钥（签名校验时密钥从不过中枢；令牌方式下令牌在 TLS 终止后经过中枢内存，不落盘不记日志）。
3. 只转发白名单请求头（`Content-Type`、`Authorization`、`X-XBot-Signature`、`X-XBot-Delivery`、`X-Delivery-Id`、`X-XBot-Event`、`X-XBot-Sender`），正文上限 32 KiB（同 `inbound.MaxBody`），每台设备同时最多 8 条，15 秒没回答 504，设备不在线 503。中枢日志只有 hook id 前 8 位和状态码。
4. 网页 API 的 `url` 优先公网：`XBOT_PUBLIC_URL` 或隧道地址（运维显式配置）> 账号网关（中枢确认待命后才算，断开即撤回）> 本机监听地址；另回 `local_url` / `public_url`。没有公网时不假装有，界面引导去设置·设备登录网关。

## 后果

- 公网地址里带设备 id。知道地址的人能打到设备，但过不了本机的签名/令牌校验；并发上限挡住单设备刷量。
- 中枢要随版本重新部署才有这条入口；旧中枢上这个地址 404。旧设备收到 `hook` 控制消息会忽略，中枢 15 秒后回 504。
- 不做 Cloudflare/frp 教程，也不把 `XBOT_PUBLIC_URL` 当产品解（仍保留给自建部署）。
