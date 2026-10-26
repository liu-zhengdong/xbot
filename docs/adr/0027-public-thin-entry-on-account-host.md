---
kind: adr
guard: owner
status: Accepted
date: 2026-10-26
---
# 0027 公网薄入口挂在账号主机

## 背景
手机要打开公网 HTTPS 登录后看「我的设备」，不能依赖本机 `127.0.0.1:8788`。账号 API 已在腾讯云 `account.babelgo.cn:8443`；CF 上的 `xbot.babelgo.cn` 是旧信箱备用，根路径无产品页。

## 决定
用户只记 **`https://account.babelgo.cn:8443/`**。同机 OpenResty 提供静态薄入口，并把 `/account/v1/` 反代到账号服务。不新开子域、不把薄入口扩到 Cloudflare。

## 后果
- 入口带 `:8443`（与 gateway/pipe 一致），因国内未备案域名入站 80/443 被拦。
- 同域无 CORS；会话用 Bearer + `localStorage`（公网页非 HttpOnly cookie 场景）。
- 细节见 [docs/dev/public-thin-entry.md](../dev/public-thin-entry.md)；账号产品决策仍见 [0025](0025-account-is-the-way-in.md)。

## 何时重看
- 域名备案后若可对公网开 443，可再议无端口入口或统一到 `xbot.babelgo.cn`。
