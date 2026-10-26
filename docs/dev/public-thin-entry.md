# 官方薄入口（公网登录页 → 我的设备）

2026-10-26 · 执行落地

## 用户记哪个 URL

**唯一主入口**：`https://account.babelgo.cn:8443/`

手机 Safari / Chrome 打开后：登录 / 注册 →「我的设备」。

## 为什么不是 `https://xbot.babelgo.cn/`

| 选项 | 结论 |
|---|---|
| `xbot.babelgo.cn`（443 / CF） | 现为 Cloudflare 旧信箱网关备用（根路径 404）；橙云 443 与腾讯云国内未备案拦截纠缠，新产品面不扩 CF（ADR 0024） |
| `account.babelgo.cn:8443`（选定） | 账号 API 已在此 HTTPS 直连（灰云 A → shanghaigo）；同域挂静态页，无 CORS、无新证书、无新子域；`:8443` 绕过 ICP 对 80/443 的拦截（与 gateway/pipe 同习惯） |

账号 API 仍为 `https://account.babelgo.cn:8443/account/v1/…`。薄入口静态资源同主机 `/`、`/assets/…`。

## 挂法

1. 仓 `xbot-account` 增加 `public/`（登录/注册/我的设备轻前端）。
2. OpenResty `account.babelgo.cn`：`/account/v1/` → `127.0.0.1:8790`；其余 → `public/` 静态（`try_files` → `index.html`）。
3. 部署机：`/opt/xbot-account` + `/opt/1panel/.../www/sites/account.babelgo.cn/`（现网习惯：rsync/scp，不必 push origin）。

## 产品对齐

- 设计真源：`/workspace/xbot-ui-design/account-auth/`（手机 `#login` `#register` `#devices`）。
- 公网浏览器全屏即手机薄入口屏；不套假手机框；无顶栏 brand；旧登录码不作主路径。
- 注册行为以账号服务为准：**不发邮件、注册即进会话**（ADR 0025）；UI 勿假装等邮件。免密「用邮件里的链接登录」仍可走 magic（outbox），失败时文案诚实。

## 验收

见 `~/.xbot-build/public-thin-entry/accept.md`。
