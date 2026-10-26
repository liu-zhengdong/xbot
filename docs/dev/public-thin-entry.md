# 官方薄入口（公网登录 → 同款主界面）

2026-10-26 · 执行落地

## 用户记哪个 URL

**唯一主入口**：`https://account.babelgo.cn:8443/`

手机 Safari / Chrome 打开后：登录 / 注册 → `/app/` 同款主界面；`#devices` 保留作次要入口。

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


## 续作：同款 static + 管道 Do

授权范围：同款 static + 管道 Do，不反代 8788、不做设备列表聊天薄壳。两仓 `feature/public-remote-ui` 从当前 main 开发；主仓不合 main、不起 8788、不 push，由指挥官合入起服。

- 管道沿用批 1 的票、待命、密钥派生和主人 AuthorizeOwner，放行有 Body/Content-Type 的五种 `/api/` 方法。SSE 仍是整段缓冲，单请求/响应 700 KiB，密文帧 1 MiB。
- 设备登录或认领为主人后保存 0600 会话，serve 恢复待命、独立校验/心跳；登出及确定失效清理。Hub 用 `XBOT_PIPE_HUB` 或官方默认。
- 账号仓脚本同步 `internal/web/static` 到 `public/app/`；只补资源路径、bridge、手机主人直接入主界面及禁用远程 Service Worker。账号 API 字段在 bridge 适配，保留原侧栏和对话。
- 入口连接 Web Crypto X25519/HKDF/AES-GCM；同一连接串行请求，连接失败不自动重放写操作。CORS 仅账号来源，CSP 只增固定 pipe 的 HTTPS/WSS。

### 用户验收（主人给定）

手机打开 `https://account.babelgo.cn:8443/` 登录成功后，进入与本机 8788 同款 Web 主界面（侧栏 Bot + 对话），经 pipe.babelgo.cn ADR 0024 加密管道连到已配对设备上的 Bot。

### 还没做的

- 指挥官合入两仓分支，按现有流程更新 Mini 并起 8788；本编码批不操作。
- 政东在公网手机浏览器用真实账号验登录 → 主界面 → 发消息 → 刷新保留对话，以及 Mini 重启后仍能连。未据本机替身测试声称真实公网端到端或主人日用通过。
- 分帧 SSE、主动恶意中枢防护、局域网直连不在本续作；安全边界与现有实现见 [pipe](../pipe.md)。
