---
kind: adr
guard: owner
status: Accepted
date: 2026-10-25
---
# 0022 网页出站只留最小底线，不再要地址白名单

## 背景
`web_fetch`、`view_image` 的网址和 `web_search` 过去只拨「公网」地址：DNS 答案落在私网、`100.64.0.0/10`、`198.18.0.0/15` 等保留段就拒绝，要靠 `web.json` 的 `trusted_egress_cidrs` 手写放行。mihomo、Clash 一类代理的 fake-ip 默认把每个域名解析到 `198.18.0.0/15`，于是装了代理的主人在「网页搜索」里试一下 DeepSeek 密钥，得到「密钥无效或连不上」，密钥本身是好的。fake-ip 对代理端很通用，只靠让用户维护 CIDR 是工程视角的长期方案。

## 决定
1. **去掉公网检查和 `trusted_egress_cidrs`。** 设置页、配置和白名单都不再有这个概念；旧文件里的字段读进来忽略，保存时丢掉（`DisallowUnknownFields` 不会让旧文件读失败）。
2. **保留一道最小底线，没有整段拆掉。** 只拒绝回环、未指定、链路本地（含云元数据）和组播，以及 `localhost`、`metadata*` 这几个名字。理由：部署在云盒子上，`web_fetch` 能被页面里的提示词引导；整段拆掉会让它直接读云元数据凭证或打到本机其它服务。底线里的地址不可能是代理的 fake-ip，所以不误伤。私网、`100.64.0.0/10`、`198.18.0.0/15`、`240.0.0.0/4`、ULA 都放行。
3. **其余机制不变**：同名二次解析变成底线地址按改绑拒绝；重定向每一跳重查；POST 不跟重定向；不走环境代理（代理会绕过地址检查，本机 TUN 模式的代理对 Go 的直连透明）。

## 代价
放行私网后，被提示词注入的 `web_fetch` 可以访问主人局域网里的设备（路由器管理页等）。与 [ADR 0013](0013-features-before-security.md) 一致：先让功能顺手，目前只有主人自己在用。如果以后有多用户，要在这里加回按需的内网拒绝，而不是白名单。

## 影响
`tools.PublicIP` 改为 `tools.Dialable`；`tools.NewSearch`、`tools.NewFetcher` 不再收 `trusted`；`config.TrustedEgress` 删除。
