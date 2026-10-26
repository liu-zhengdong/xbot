# 远程控制台管道（ADR 0024 批 1）

主人在手机或异地桌面上，经腾讯云中枢 `xbot pipe serve`（`internal/pipe`）读设备上的主人 API。中枢只配对和转发密文帧，看不到 Bot 名和正文。设计见 [ADR 0024](adr/0024-remote-ops-fluent.md)。

## 流程

1. **设备待命**：设备用自己的账号会话开 WebSocket `GET /pipe/v1/ws`，首帧 `{"role":"standby","session":…,"device_id":…}`。中枢调账号服务 `GET /account/v1/me`、`GET /account/v1/devices`，确认会话有效、设备在这个账号下，回 `{"t":"standby"}`。同一设备新的待命连接顶掉旧的。
2. **领票**：入口 `POST /pipe/v1/ticket`，`Authorization: Bearer <账号会话>`，正文 `{"device_id":…}`。同样查账号；会话无效 401，设备不在这个账号下 404，设备没待命 409，同一账号同时超过 8 张票 429。成功回 `ticket_id`、`ticket_secret`（32 字节随机，只给一次）、`expires`（5 分钟）、`hub`。中枢同时经待命连接把 `{"t":"open","ticket_id","secret"}` 发给设备，之后只留密钥的 SHA-256。
3. **配对**：入口和设备各开一条 WebSocket，首帧 `{"role":"entry"|"device","ticket_id","secret"}`。两边到齐后中枢发 `{"t":"ready"}`，之后只转发二进制消息。先到的一方等待时挂断，或票过期还没配上，票就作废。任一方断开，中枢关掉另一方并作废票，一张票只用一次。
4. **端到端密钥**：ready 之后双方各发一个新的 X25519 公钥；共享秘密经 HKDF-SHA256（salt = `SHA-256("xbot-pipe-v1|" + ticket_secret)`，info 含方向和双方公钥）得到每个方向一把 AES-256-GCM 密钥，nonce 是消息计数，丢帧、重放、乱序都解不开。
5. **请求**：明文帧是 JSON：请求 `{"t":"req","id","method":"GET","path":"/api/bots/<bot>/transcript"}`，响应 `{"t":"res","id","status","body"}`。设备把请求交给本机 `xbot serve` 同一个主人 API，带主人会话（`web.Server.AuthorizeOwner`）。批 1 只放行 `GET /api/…`，其余回 403；响应正文上限 700 KiB，超了回 413。

## 限制

- 消息上限 1 MiB；中枢每 30 秒 ping 一次；配对后 10 分钟没有数据帧就断开；首帧 10 秒内必须到。
- 浏览器发起的 WebSocket 要在 `PIPE_ORIGINS` 里；没有 Origin 头的非浏览器客户端照常放行（鉴权靠首帧的票，不靠 cookie）。
- 中枢日志只有票 id 前 8 位、配对/结束和两个方向的字节数。
- 中枢自己签发票密钥，所以只防「看」不防「改」：一个主动作恶的中枢可以做中间人。以后由设备签票、入口公钥绑定进票里再收紧。
- 还没有：分页游标、入口缓存、确认卡、SSE、局域网直连（批 2–5），设备端还没接进 `xbot serve`，手机入口还没有界面。

部署见 [packaging/pipe/](../packaging/pipe/README.md)。
