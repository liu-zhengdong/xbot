# 远程控制台管道（ADR 0024 批 1 + 公网主界面续作）

用户在手机或异地桌面上，经腾讯云中枢 `xbot pipe serve`（`internal/pipe`）调用设备上的用户 API。中枢只配对和转发密文帧，看不到 Bot 名和正文。设计见 [ADR 0024](adr/0024-remote-ops-fluent.md)。

> **批 1 的安全边界：防看不防改。** 中枢自己签发票密钥，被动的中枢（只转发、记日志）解不开正文；但一个主动作恶的中枢可以伪造 `open`、自己当入口或做中间人，读到用户 API。ADR 0024 要求的「设备签票、绑定入口公钥」留到后续批次，在那之前不要把中枢当成不可信的第三方。

## 流程

1. **设备待命**：设备用自己的账号会话开 WebSocket `GET /pipe/v1/ws`，首帧 `{"role":"standby","session":…,"device_id":…}`。中枢调账号服务 `GET /account/v1/me`、`GET /account/v1/devices`，确认会话有效、设备在这个账号下，回 `{"t":"standby"}`。同一设备新的待命连接顶掉旧的。
2. **领票**：入口 `POST /pipe/v1/ticket`，`Authorization: Bearer <账号会话>`，正文 `{"device_id":…}`。同样查账号；会话无效 401，设备不在这个账号下 404，设备没待命 409，同一账号同时超过 8 张票 429。成功回 `ticket_id`、`ticket_secret`（32 字节随机，只给一次）、`expires`（5 分钟）、`hub`。中枢同时经待命连接把 `{"t":"open","ticket_id","secret"}` 发给设备，之后只留密钥的 SHA-256。
3. **配对**：入口和设备各开一条 WebSocket，首帧 `{"role":"entry"|"device","ticket_id","secret"}`。两边到齐后中枢发 `{"t":"ready"}`，之后只转发二进制消息。先到的一方等待时挂断，或票过期还没配上，票就作废。任一方断开，中枢关掉另一方并作废票，一张票只用一次。
4. **端到端密钥**：ready 之后双方各发一个新的 X25519 公钥；共享秘密经 HKDF-SHA256（salt = `SHA-256("xbot-pipe-v1|" + ticket_secret)`，info 含方向和双方公钥）得到每个方向一把 AES-256-GCM 密钥，nonce 是消息计数，丢帧、重放、乱序都解不开。
5. **请求**：明文帧是 JSON：请求 `{"t":"req","id","method":"POST","path":"/api/bots/<bot>/messages","body":"base64…","ct":"application/json"}`，响应 `{"t":"res","id","status","body":"base64…","ct":"text/event-stream"}`。设备把请求交给本机 `xbot serve` 同一个用户 API，带用户会话（`web.Server.AuthorizeOwner`）。批 1 加本续作放行 `GET/POST/PATCH/PUT/DELETE`，路径必须无 host 且在 `/api/` 下；请求/响应正文各上限 700 KiB，超了回 413。SSE 整段缓冲后返回 Content-Type，本批不分帧流式。

## 限制

- 消息上限 1 MiB；中枢每 30 秒 ping 一次；配对后 10 分钟没有数据帧就断开；首帧 10 秒内必须到。
- 入口的 `Dial`/`Do`（`Get` 是薄封装）认 context 取消：已取消直接返回；进行中取消立即关掉连接，这条会话作废（之后的 `Do` 都报 session ended），以免迟到的响应被当成下一个请求的。写超时取调用方 deadline 和 30 秒中较早的那个。
- 浏览器领票 POST/OPTIONS 与 WebSocket 共用 `PIPE_ORIGINS` 精确来源白名单；领票回显允许的 Origin，允许 `Authorization, Content-Type` 和 `POST, OPTIONS`；没有 Origin 头的非浏览器客户端照常放行（鉴权靠首帧的票，不靠 cookie）。
- 中枢日志只有票 id 前 8 位、配对/结束和两个方向的字节数。
- 中枢自己签发票密钥，所以只防「看」不防「改」：一个主动作恶的中枢可以做中间人。以后由设备签票、入口公钥绑定进票里再收紧。
- 还没有：分帧 SSE、分页游标增强、入口缓存和局域网直连（后续批次）。连接取消或失败后不自动重放写请求。

## serve 与公网入口

`xbot serve` 的用户账号登录/认领后，把账号会话存在 `<root>/account-session`（原子写、0600、勿入 git），经同一个 mux 和 `ui.AuthorizeOwner` 启动 `pipe.Device.Run`。重启会恢复待命；每分钟校验会话与绑定并上报在线设备，无需本机网页常开。新会话轮换时先取消并等旧待命及管道退出；登出或确定会话失效时删除文件并取消。临时账号网络故障不删会话；只取消 serve 不删，供重启恢复。Hub 默认 `https://pipe.babelgo.cn:8443`，`XBOT_PIPE_HUB` 可覆盖；日志前缀 `xbot: pipe:`。

账号仓 `/app/` 只保留登录后的引导、bridge、资源 worker 与字体。业务壳从当前选中的配对设备 `GET /api/ui/static/<path>` 读取，依旧进入现有 Do / owner mux；资源 worker 仅把浏览器原生资源请求交给所属页面的 bridge，不另建网络通道、不缓存业务壳。切设备关闭旧 pipe 并重载；无设备、离线和旧设备不支持出口时显示说明及重试。`/api/account/*` 仍适配云上账号 API；同步脚本不再是业务 UI 发版路径，详见 [ADR 0029](adr/0029-public-ui-single-source-via-pipe.md)。

静态请求帧可带 `inm`（If-None-Match，最多 256 字节且禁止换行）；静态响应帧可带 `etag`、`tip`、`cache`，分别对应 ETag、X-Xbot-Tip、Cache-Control。只回传这几个白名单字段，不转发 cookie。Go `Do` 既有返回签名保持不变，浏览器 bridge 读取可选元数据。

部署见 [packaging/pipe/](../packaging/pipe/README.md)。
