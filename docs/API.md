# XBot 网页 HTTP 接口

`xbot serve` 在同一个 `--http` 上提供这些接口。静态页面（`internal/web/static`）由单独的界面代理维护；这里只记后端契约。内核行为以 [DESIGN.md](DESIGN.md) 为准。

后台异步压缩没有 HTTP 接口。消息总量到软目标时内核自己在后台压，压完的摘要在两轮之间换上（见 [atlas/context.md](atlas/context.md) 的两条线）。`GET /api/bots/{name}/transcript` 始终返回只追加的原文，不返回模型看到的压缩视图。

## 约定

- 除 `POST /api/login` 外，请求要带 cookie `xbot_session`（HttpOnly，SameSite=Strict，Path=/，30 天）。值是 `xbot serve` 打到 stderr 的 token。
- 变更类请求（非 GET / HEAD）若带 `Origin`，必须和请求的 Host 一致；`Sec-Fetch-Site: cross-site` 一律 403 `{"error":"cross-origin"}`。没有 `Origin` 的非浏览器客户端可以通过。
- JSON 请求体未知字段会 400 `{"error":"bad request"}`。`POST /api/bots/import` 上限 2 MiB，其余 JSON 上限 1 MiB。
- 成功和失败都是 JSON，`Content-Type: application/json`。HTML 字符会转义（`<` 变成 `\u003c`）。
- 不存在的 Bot，或这个登录用户不是 owner，统一 404 `{"error":"not found"}`。
- 未登录 401 `{"error":"unauthorized"}`。

## 设备静态壳

`GET /api/ui/static/<path>` 要求用户认证（pipe 复用 `AuthorizeOwner`）。与本机静态路由同源：设置 `XBOT_DEV_STATIC` 时只读该配置目录内的真实文件（拒绝符号链接越界），否则读内嵌 `static/`。扩展名允许 html/js/css/svg/png/woff2/webmanifest/txt；非法相对路径、`..`、绝对路径、反斜杠、重复编码和目录均不提供文件。不存在或未列入允许类型回 404。

成功按资源类型返回正文、`ETag`、`Cache-Control: no-cache` 和 `X-Xbot-Tip`。If-None-Match 命中回 304 空正文；tip 为构建版本，未注入版本时为 `dev-<内嵌 UI 摘要>`。文件大于 pipe 的 700 KiB 正文预算回 413，不截断为成功；大字体由公网入口缓存。开发覆盖时 tip 为 `dev-static`、缓存为 `no-store`，仍有正文 ETag / 304；每次重读磁盘，缺文件不回落 embed，实际读取也受 700 KiB 上限约束。

## 登录

`POST /api/login`，body `{"token":"..."}`。成功 200 `{"owner":"<id>"}` 并设置 cookie。token 不对是 401。

`POST /api/logout` 清 cookie，200 `{"status":"ok"}`。以上是旧的登录码路径，保留作调试；本机直接进入，公网入口走账号。

## 账号

都不需要 `xbot_session`；写入要求同源，body 最多 1 MiB、拒绝未知字段。账号会话放在 cookie `xbot_account`（HttpOnly，SameSite=Strict，30 天）。错误一律 `{"error":"<代码>"}`：`bad_input` 400、`credentials` 401、`unverified` 403、`busy` 429、`expired` 410、`session` 401、`unreachable` 502（连不上账号服务）。

- `POST /api/account/register` `{email,password}`：密码 8–128 字节。回 `{challenge,email}`（`email` 已打码）。已有账号的邮箱回同样的形状，改发一封登录信。
- `POST /api/account/login` `{email,password}`：成功同下面的「进门回包」。邮箱还没确认、密码对时 403 `{"error":"unverified","challenge":"…","email":"…"}`。
- `POST /api/account/magic` `{email}`：用邮件里的链接登录，回 `{challenge,email}`；没有这个账号也回同样的话，但不发信。
- `POST /api/account/resend` `{challenge}`：再发一次，30 秒内重复调用不重发也不报错。
- `POST /api/account/code` `{challenge,code}`：用 6 位验证码完成，成功同「进门回包」。
- `POST /api/account/poll` `{challenge}`：没完成 `{"state":"pending"}`，过期或已领过 `{"state":"expired"}`；邮件链接确认后第一次轮询回「进门回包」。
- 进门回包：200 `{"state":"done","account":{email,owner,hosted,gateway,official,gateway_ok,app}}`，并设 `xbot_account`；`owner` 为真时同时设 `xbot_session`。
- `GET /api/account/me`：`{"account":null|{…同上},"app":<bool>}`。账号是用户时顺手续上 `xbot_session`。
- `POST /api/account/logout`：退出账号并清掉 `xbot_account`；本机直连保留 `xbot_session`，远端同时清掉用户会话。
- `GET /api/account/devices`：`{"devices":[{id,name,online,self,bots:[{name,hint,unread,avatar_url,avatar_bg,avatar_ink,initial}]}]}`；没有账号会话 401。

账号服务本身（`xbot account-api`，或 serve 里的替身）在 `/account/v1/`：`register`、`login`、`magic`、`resend`、`code`、`poll`（POST，JSON），`me`、`devices`（GET，`Authorization: Bearer <会话>`），`logout`、`devices`（POST，Bearer），`health`（GET），以及邮件链接页 `link?t=…`（GET 只显示「确认」按钮，POST 才用掉链接，邮件扫描器预取不会把它用掉）。

`GET /api/session` 对无转发头的本机回环连接、回环 Host 且同源的请求签发本机用户会话；远端仍须已有用户会话。已登录时 200 `{"owner":"<id>","app":<bool>,"version":"<版本>","tip":"<构建短号或空串>"}`。`version` 是构建版本去掉开头的 `v` 和 `+` 之后的内容，例如 `0.3.1-beta.2`。开发构建短号单独通过 `tip` 返回，不参与版本比较或自动升级。

`POST /api/app/theme` 只在桌面版存在，body `{"theme":"light|dark|system"}`，让窗口的系统按钮跟随 XBot 自己的明暗主题。成功 204，没有 body；值不对 400；不是桌面版 404。

## 图片生成

- `GET /api/image-gen`：返回 `{enabled,provider,providers:[{id,name,logged_in}]}`。默认关，选择尚未保存时优先已登录的账号，否则 OpenAI；`openai` 对应 ChatGPT OAuth，`xai` 对应 xAI OAuth。只返回登录布尔值，不返回保险箱原文。
- `PUT /api/image-gen`：完整提交 `{enabled:true,provider:"openai|xai"}`，返回当前设置。正文上限 4 KiB，拒绝未知字段、缺失值、非法供应商和尾随 JSON。认证与同源要求复用现有设置接口。写入现有 `web.json`，与搜索共用文件锁，重复值不重写。
- 全局关闭不挂生图；开启后仅所选家的已登录 OAuth Responses 绑定可用。每轮开始固定配置，进行中的一轮保留快照；产物仍进工作区，不自动挂气泡。

## 网页搜索

以下接口均要求登录，写入还要求同源。请求最多 4 KiB，拒绝未知字段和多份 JSON。

- `GET /api/web-search`：返回 `{enabled,mode,provider,providers:[{id,name,needs_key,key_configured}]}`。缺省开、自动、DuckDuckGo；只返回密钥是否存在，不返回值和配置路径。
- `PUT /api/web-search`：完整提交 `{enabled:true,mode:"auto|local",provider:"ddg|deepseek|brave|tavily|serper|zhipu|exa|firecrawl|perplexity"}`，成功返回当前设置。全局共享，不重建 Bot 内核。新一轮对话和新远端任务使用新设置，运行中的远端任务保留启动快照。
- `POST /api/web-search/key`：`{provider:"deepseek",key:"…"}`，空值表示重试本机已有密钥。DeepSeek 复用实际搜索路径探测，15 秒超时；经本机代理的假 IP 也能探测，只向固定官方地址发送。成功即保存，回 `{ok:true}`。无效或连不上回 422，不保存失败值；其它七家目前同样回探测失败。正文和错误不回显密钥。

## 权限总开关

`GET /api/permissions` 要登录，返回 `{"confirm":"off"}` 或 `{"confirm":"table"}`。缺失、不可读或损坏的 `permissions.json` 都按关处理。

`PUT /api/permissions` 要登录，正文为 `{"confirm":"off|table"}`，成功返回当前值；未知字段、非法值、多份 JSON 对象为 400。它与 `xbot permissions on|off` 共用 `internal/permissions` 和数据根的 `permissions.json`，运行中的 Bot 在下一次权限判断时读取新值。权限分类不变。

## Bot 列表与创建

`GET /api/bots` 返回当前 owner 的 Bot。顺序是置顶在前，组内按名字。主 Bot 会被 `SetPrimary` 置顶，所以通常排在置顶组里。

```json
{"bots":[{
  "name":"ada",
  "persona":"截到 80 字",
  "description":"一行简介",
  "unread":true,
  "notes":1,
  "pinned":true,
  "primary":true,
  "avatar":"generated",
  "avatar_seed":"ada",
  "avatar_url":"/api/bots/ada/avatar",
  "avatar_bg":"#rrggbb",
  "avatar_ink":"#rrggbb",
  "initial":"A"
}]}
```

`avatar` 是 `generated`（界面按种子画）或 `custom`（已上传图片）。`avatar_url` 只在已上传头像时出现。没上传时界面在浏览器里按 `avatar_seed` 画 Notionists 线稿头像，固定白名单，只有平静或微笑的表情；16 像素用只画脸部的裁切。种子是 `glyph`、图片加载失败或脚本没加载时，界面按名字画黑白几何字形。服务端从不画头像，也不分发画出来的头像。`avatar_bg`、`avatar_ink`、`initial` 仍然返回，界面已不再使用。`unread` 在有未读通知，或 transcript 比这个浏览器上次打开的游标更长时为 true。

`POST /api/bots`，body `{"name":"ada","persona":"...","avatar_seed":"writer#3"}`。名字匹配 `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`。`avatar_seed` 可省略，省了就用名字。种子是去空白后 1–80 个字符，不能有控制字符和「<>"'&」以及反引号。种子不合法 400，这时不会建出 Bot。成功 200 `{"name":"ada"}`。这是目录里第一个 Bot 时，它成为主 Bot 并被置顶。

## 主 Bot、置顶、删除

`POST /api/bots/{name}/primary` 无 body。把主 Bot 转给这个 Bot，并把它置顶。原来的主 Bot 保持原来的置顶状态，不会被自动取消置顶。成功 200 `{"name","primary":true,"pinned":true}`。没点名的入口之后进这个 Bot 的 inbox。

`POST /api/bots/{name}/pin`，body `{"pinned":true}` 或 `false`。成功 200 `{"name","pinned"}`。`pinned` 缺了是 400。

`DELETE /api/bots/{name}` 无 body。成功 200 `{"name"}`。主 Bot 返回 409，正文含 `the primary cannot be deleted`，要先把主 Bot 转走。目录里只剩一个 Bot 时 409，正文含 `the last bot cannot be deleted`。

## 分叉

`POST /api/bots/{name}/fork`，body `{"name":"reviewer","role":"Review pull requests.","group":"ship"}`。`name` 和 `role` 必填。`group` 可省略；有名字时新建一个两边都在的群。

这不是拷贝。源 Bot 写一份交接，成为新 Bot 的 bot 记忆；person 记忆本来就共享，不复制。新 Bot 的 persona 是 `role`，transcript 是空的，provider 绑定继承。两边互相记一条，细节用 `message_bot` 问，不能读源 transcript。源 Bot 正忙时 409 `bot is busy`。成功 200 `{"name":"reviewer","group_id":"g_........"}`，没建群时 `group_id` 是空字符串。

## 档案、头像、模板

`GET /api/bots/{name}` 是详情：

```json
{
  "name":"ada",
  "persona":"全文",
  "description":"",
  "persona_card":{"id":"0123456789abcdef","name":"写作助手"},
  "timezone":"Asia/Shanghai",
  "avatar":"generated",
  "avatar_seed":"ada",
  "avatar_url":"",
  "avatar_bg":"#rrggbb",
  "avatar_ink":"#rrggbb",
  "initial":"A",
  "provider":"",
  "model":"",
  "pinned":false,
  "primary":false,
  "memory":[{"scope":"person","text":"..."}],
  "routines":[{"id":"...","name":"...","cron":"7 * * * *","at":"","enabled":true}],
  "agents":[{"id":"...","type":"dev","status":"running","task":"截到 140 字","mode":"brief","steps":1,"started_at":"RFC3339","current_step":"当前工具的一行摘要","finished_at":null}]
}
```

`persona_card` 是选用的人设卡（没选或卡已删时是 `null`），`persona` 仍是 `persona.md` 全文。没有 `style` 和免打扰：旧配置里的 `style`（language、tone、length、emoji）加载时合成一条 Bot 长期记忆，写入成功后从配置删除，只迁一次；旧配置的 `quiet_start`/`quiet_end` 读进来丢掉；旧模板里的这些字段忽略。详情里没有 `proactivity`。磁盘上的旧配置还可以带这个字段，读进来之后忽略，不进提示。`timezone` 是 IANA 时区名（例如 `Asia/Shanghai`），空字符串表示用本机时区。每轮环境更新里有一行 `now: <本地 RFC3339，精确到分钟> (<时区>)`，不进稳定前缀。`routines[].cron` 和 `routines[].at` 二选一：`at` 是一次性的 RFC3339 时间，触发后 `enabled` 变成 false。`memory` 最多 40 条 profile 事实（person、bot、bot+person）。

`agents[].notable` 是已实际发生的「值得留意」清单（无条目时为空或省略）：`[{"kind":"global_install","text":"全局安装了 ripgrep","objects":["ripgrep"]}]`。`kind` 固定为 `global_install`、`write_outside`、`external_message`、`delete`；按删除、外发、工作区外写入、全局安装排序，同一类每个任务合成一项。`text` 是不带句号的短句，超过两个对象只列前两个并写总数；`objects` 保留完整对象列表，放在任务详情。`calls` 是成功调用数，外发短句用它写消息数量；`unit` 是对象计数单位。`incomplete:true` 表示部分调用没有可靠对象字段，或对象明细达到单任务 64 KiB 上限，此时不声称完整数量。交回 Bot 的报告用同一份清单生成一句摘要：统一以「值得留意：」开头、句号结尾；一项直接写该项，多项写首项及其余项数，并提示详见任务。卡片行本身不加句号。普通工作区内读写不会产生条目。

`agents` 是子代理状态，不是一套独立的任务系统；`type` 为派发时的类型（如 `research`、`dev`），普通 spawn 为空，界面在没有任务标题时用它显示类型名；`started_at` 是实际开始时间（尚未开始时为 `null`），`current_step` 是当前一步的一行摘要，`finished_at` 优先使用实际结束时刻。

`PATCH /api/bots/{name}/routines/{id}`：须登录且属于该 Bot 的用户。请求体可含布尔字段 `enabled` 和字符串字段 `precheck`，至少提供一个；返回更新后的例程。`precheck` 是 shell 命令字符串（不是布尔开关），最多 16 KiB；省略保留原值，空串移除检查。请求体上限 128 KiB（容纳 JSON 转义），详情接口的 `routines[]` 在非空时也返回 `precheck`。和 Bot 的改例程工具共用 `Timer.Update`；不改变提示、时区或计划。未登录 401，Bot/例程不存在 404，字段缺失、类型不对、命令超长或未知字段 400，写入失败 500。有效前置命令（包括省略字段时保留的命令）复用 shell 审核；需要确认或机器 off 时返回 403、整次 PATCH 不落盘，可通过聊天中的 `routine_update` 走现有确认。显式清空仍可使用本接口，不新增 UI。执行规则见 [多 Bot](atlas/multibot.md)。

`PATCH /api/bots/{name}` 只改出现的字段。空字符串清空该字段。可写：`name`、`description`（最多 160）、`timezone`、`persona`、`avatar_seed`；`language`、`quiet_start`、`quiet_end` 已删除，带上是 400。`avatar_seed` 把 `avatar` 设成 `generated`，并删掉已上传的图片；种子非法 400，正文是 `bot: avatar seed ...`。改名不改种子。请求里如果还带 `proactivity`，接受后丢掉，不写入。改名会改目录、配置和群成员；主 Bot 改名后 `primary.json` 跟着改，置顶状态不变。成功 200 `{"name":"<当前名字>"}`。非法取值 400，正文是 `bot: ...`。非法时区同样 400，正文含 `bot: timezone:`。

`POST /api/bots/{name}/avatar` 是 `multipart/form-data`，字段名 `file`。只接受 PNG、JPEG、GIF、WebP，最大 1 MiB。成功 200 `{"name","avatar":"custom","avatar_seed"}`。`avatar_seed` 保持上传前的种子。之后列表和详情里的 `avatar_url` 指向 `GET /api/bots/{name}/avatar`，响应是图片字节，`Content-Type` 按图片内容定，带 `X-Content-Type-Options: nosniff`，`Cache-Control: private, no-cache`。生成的头像不经过这个接口，GET 只返回上传的图。

`DELETE /api/bots/{name}/avatar` 回到按种子画的头像，种子是上传前那个。200 `{"name","avatar":"generated","avatar_seed"}`。

`POST /api/bots/{name}/open-workdir` 只在桌面版存在：让系统文件管理器打开这个 Bot 的工作目录。成功 200 `{"status":"ok"}`。浏览器里（`/api/session` 没有 `app:true`）是 404；桌面版里调用文件管理器失败是 500，正文是原因。要登录；不是这个 owner 的 Bot 是 404。

`GET /api/bots/{name}/generated` 返回 `{"path":"<服务端 Bot 工作区的 generated 绝对路径>","can_open":<bool>}`。浏览器与桌面均可用；要登录且为该 Bot 的 owner。目录不存在则幂等创建，只处理固定的 `generated/`，不接收任意路径、不读取图片。`generated/` 为符号链接时拒绝。`can_open` 仅在桌面且配置了系统打开回调时为真；浏览器显示路径并提供复制。

`POST /api/bots/{name}/open-workdir?folder=generated` 复用桌面打开接口，指向该 Bot 的 `generated/`；其余非空 folder 返回 400，符号链接拒绝。无 folder 仍打开整个工作目录。

`GET /api/bots/{name}/template` 下载 JSON，`Content-Disposition: attachment`。模板含档案、persona、例程、`avatar_seed` 和可选的 base64 头像。自定义头像的模板同时带图和种子。不含记忆、transcript、webhook、watch、provider、owner、置顶。

`POST /api/bots/import` 的 body 就是这份模板，可以另带一个不同的 `name`。模板里的合法 `avatar_seed` 跟着模板走，即使改了名字；种子不合法就忽略，用新名字当种子。之后有 base64 头像就照样导入。成功 200 `{"name"}`。这是目录里第一个 Bot 时成为主 Bot。导入失败会删掉新建的目录。

## 对话

`GET /api/bots/{name}/transcript` 返回原文：

```json
{"messages":[{
  "seq":1,
  "role":"user",
  "content":"...",
  "name":"",
  "tool_calls":[],
  "tool_call_id":"",
  "time":"2026-10-15T00:00:00Z",
  "reply_to":4,
  "branch":"bg_7"
}]}
```

`reply_to` 省略表示这行没有引用。有的话，它是本 transcript 里另一行的 `seq`。用户消息上的值是用户发送时带的序号。助手消息上的值是这条回复指向的用户消息序号。

行上还可能有 `images`，是这条消息带的图片文件名（见 `POST /api/bots/{name}/paste`），字节不在里面。`branch` 省略表示主线。有值时，这行属于后台任务 `bg_7`；它是接口按 `fork` 标记重放算出的有效分支：`fork` 标记的 `from` 之后、标记之前的行，以及自己带 `branch` 的行都归这个分支。标记行没有正文，用 `kind` 区分：`"kind":"fork"` 另带 `from`（被分出那一轮的起始 seq）和 `task`；`"kind":"fork_end"` 另带 `status`（`done`、`failed`、`cancelled`、`interrupted`）和 `steps`。标记行的 `branch` 是它自己的分支 id。旧记录没有这些字段，读出来和以前完全一样。

这次 GET 会把该 Bot 的已读游标记到当前长度。`role` 为 `tool` 的行用 `tool_call_id` 对上助手消息里的 `tool_calls`。以 `<environment_` 或 `<memory>` 开头的 user 行是内核上下文，不是用户说的话。Bot 之间的动作如果出现，也是普通 transcript 行，接口没有单独的“系统行”类型。用户聊天里，来自其他 Bot 的一行仍是淡提示。两边来回的原文见下面的「Bot 私聊」。

跨 Bot 阅读不是 HTTP。模型工具 `recall_history` 和 `expand` 有可选的 `bot`。只能读和调用者至少有一个相同 owner 的 Bot。别的 owner、只是同群或能互发消息的 Bot、以及群 id，都是 `not found`（`history: bot: not found`），不说明原因。搜索和展开用对方 Bot 自己的索引。读到的文本包在 `<untrusted_event source="bot:<name>" ... sender="<name>">` 里，没有指令效力。长度上限和读自己的一样（搜索约 4 KiB，展开约 8 KiB）。

`GET /api/bots/{name}/export?format=md|jsonl&tools=0|1` 下载这个 Bot 的 transcript。未登录 401。不是这个 owner 的 Bot 是 404。`format` 缺省是 `md`。`tools` 缺省是 `0`；只有 `1` 才包含工具调用和 `role=tool` 的行。可选 `from` 和 `to`，要成对出现，是包含端点的 seq。

正文按 transcript 段流式写出，不把整份历史装进内存。工具结果如果另存成 blob，写出那一行时读回全文。成功时正文不是 JSON。`md` 的 `Content-Type` 是 `text/markdown`，时间用服务器本机时区，摘要块写成 `## Summary seq <from>-<to>`，后台分支的行标题多一段 `[后台 <branch>]`，`fork` 和 `fork_end` 各写成一行。`jsonl` 是 `application/x-ndjson`。两种导出格式均为公开投影：移除 reasoning 内容块、清除所有保留块的 `vendor_raw`，`tools=1` 也不例外；普通正文、可公开 hosted/citation 与按开关保留的工具块不变。内部 transcript 原始块与同协议回放不受影响。普通行是公开消息记录再加 `seq`，`json.Unmarshal` 进记录时忽略 `seq`，所以能再导入，序号还是原来的。带引用的行多一个 `reply_to`；后台行带 `branch`，标记行带 `mark`（`{"kind","branch","from","task","status","steps"}`）。`{"kind":"summary","from","to","text","focus","at"}` 是摘要块。每一段写出前过 `RedactText`；保险箱还没安装替换函数时，文本原样出去。`Content-Disposition` 是附件，文件名 `<bot>.md` 或 `<bot>.jsonl`。`md` 在该行标题下写 `reply_to: <seq>`。

`GET /api/bots/{name}/transcript` 仍一次返回全部消息，给界面用。很长的历史用上面的 export。Bot 列表上的未读比较用序号计数，不读段文件。

`POST /api/bots/{name}/paste` 收一张要贴进对话的图片。body 是图片原始字节，`Content-Type` 只当提示，类型按字节嗅探。成功 200 `{"image":"<32 位十六进制>.png"}`。只认 PNG、JPEG、GIF、WebP（其他 400 `只认 PNG、JPEG、GIF、WebP`），超过 10 MB 回 413，边长超过 2048 或读不出尺寸回 400 并说明原因。文件存在这个 Bot 的工作区 `paste/` 下，权限 0600。`GET /api/bots/{name}/paste/{image}` 读回这张图，`Content-Type` 按字节定，带 `nosniff`，可长期缓存（文件名不会改）；名字不是一个合法 id 或文件已不在时 404。这两条都要登录，不是这个 owner 的 Bot 是 404。

`POST /api/bots/{name}/messages`，body `{"text":"...","reply_to":3,"images":["<paste id>"]}`。`text` 0 到 16 KiB，`images` 最多 4 个、每个必须是 `POST .../paste` 返回过的名字，否则 400 `图片已失效，重新贴一次`；只有图片、没有文字也可以发。这些图片挂在写入的那行 transcript 上（记录里叫 `images`），回答这条消息的那一轮请求带上图片字节，之后各轮只留一行路径说明，模型要用 `view_image` 自己打开。transcript 和导出里都不带图片字节；导出会用 `image: paste/<名字>`（md）或 `images` 字段（jsonl）写出文件名，纯图片的消息不会变成空行。群聊的发送接口不接受图片。`reply_to` 可省略；给出时必须是这个 Bot 的 transcript 里已有的序号，否则 400 `{"error":"bad reply_to"}`。群聊的发送接口不接受这个字段。拿到处理锁时响应是 `text/event-stream`：

- `event: delta`，`data: {"text":"..."}`
- `event: tool`，`data: {"name","summary"}`。`ask_user` 必填 question、options（2–6 项）、recommended（必须等于某个选项的完整字符串）；summary 是问题，后面每行一个 `1. 选项`。终端会把这段打出来。界面也可以按这个画选择，不必再解析工具参数。
- `event: approval`，`data: {"id","tool","summary","deadline"}`。`deadline` 是 RFC3339，这张卡自动继续的时刻，默认是发出后 30 分钟，可由 `Server.ApprovalWait` 配置。到点没人确认就自动允许原调用并继续，追加 `auto_approved` 和灰字 notice。界面立刻展示确认卡和允许、拒绝按钮；倒计时只读 `deadline`，没有字段就不显示倒计时。
- `event: result`，`data: {"name","output"}`，output 截到 4000 字
- `event: done`，`data: {"text":"这一轮拼起来的正文","reply_to":2}`。`reply_to` 省略表示这条回复没有指向某一条用户消息。整段回复去掉空白后正好是 `SILENT` 时不发 `delta`，`text` 是空字符串。工具调用之前流出来的字只是状态，可以出现在 `delta` 里，但不会留在 `done.text`。界面用 `done.text` 作为这轮的最终回复，不要把工具调用前的 `delta` 拼进那条回复。模型用来标记引用的 `reply_to:` 行不会出现在 `delta` 或 `done.text` 里。
- `event: error`，`data: {"error":"..."}`

`POST /api/bots/{name}/approvals/{id}`，body `{"allow":true}` 或 `{"allow":false}`。未登录 401。不是这个 owner 的 Bot，或没有这张待确认的卡，是 404 `no such approval`。已经答过是 409。超过等待时间是 410 `approval expired`。成功 200 `{"status":"ok"}`。默认等 30 分钟（`Server.ApprovalWait` 同时配置权限卡和提问卡）；显示卡片的那次请求断开后，这张卡仍可以用这个接口决定。到时没有回答，按允许执行原调用并继续；transcript 卡追加 `state="auto_approved"` 和 `notice="30 分钟没人确认，已按允许执行"`。挂起的调用沿用一次性放行与决定唤醒恢复。超时后再点权限卡仍返回 410。进程重启时，上一个进程留下的待确认卡追加 `no_decision:restart`。**默认不发权限卡**（[ADR 0019](adr/0019-no-permission-cards-by-default.md)，`xbot permissions on` 打开）：只有 `links` 的 install/remove 和触到保险箱或密钥还发卡；机器开关按 `allow` 算，外部事件开的一轮碰记忆也不发卡。`update_profile` 默认直接更新本 Bot 头像/资料，含 `avatar_seed` 也不出预览确认卡；落盘后回执为 `profile saved`，网页收到工具结果即刷新侧栏与顶栏头像，流式回复期间也生效。打开之后，哪些工具发卡只看内核里那一张权限表，工具自己从不决定：只有对外发送（连接器、`links` 的 install/remove、未知/MCP 工具）、删除、注入 `{{secret:}}` 或非 `GET`/`HEAD` 的 `http_request`、persona/profile 自改（`propose_persona`/`update_profile`，以及 `write_file`/`edit_file` 写自己的 persona/profile 文件）发卡。routine 的增删改暂停、Bot 之间的 `message_bot`/`post_group`、写技能、编码任务都不发。本机工具（`shell`、`read_file`、`write_file`、`edit_file`、桌面、浏览器）再按机器开关：`allow` 按表，`ask` 每次发卡，`off` 直接拒绝；`browser_stop` 在 `off` 下仍可确认。这条链路上根本没有确认人时，工具结果是 `not run: no approver available on this link`，不会写成用户拒绝。用户明确拒绝才是 `denied by user`。

`POST /api/bots/{name}/questions/{id}`，body `{"choice":"选项的完整字符串"}`。未登录 401；不是当前 owner 的 Bot 或没有这张卡 404；choice 不在 options 内 400；已手动选择过 409；成功 200 `{"status":"ok"}`。超时自动推荐后再点选仍返回 200，把选择当作一条新用户消息处理。直接发普通消息走原 messages 接口，并结束未答提问卡的计时。

提问卡与权限卡一样以 `<approval_card>` 用户角色记录追加保存，`tool="ask_user"`，不进入模型上下文。卡体 JSON 为 `{question,options,recommended,choice?}`；属性 `id` 是提交选择用的卡 ID，`deadline` 是截止时刻（RFC3339），`state` 为 pending/approved/auto_approved。自动选择的记录另有 `notice`，默认是「30 分钟没人选，按推荐选了 X」。网页显示推荐标记、自动选中项和灰字事件；超时决定作为用户的选择送进内核并当场唤醒，随后 Bot 的回复仍在 transcript 中出现。注入其他等待时长时，notice 使用实际时长。权限卡的自动允许也使用相同 notice 属性。重启后待答卡沿用 no_decision:restart 处理，不能用失效 ID 再作第一次选择。


卡在等决定时用户又发了消息，这张卡不挡它（[ADR 0018](adr/0018-cards-never-block-the-owner.md)）：卡照样开着、照样能用上面的接口决定，这次请求收到 `done`、`handed_off:true`，插话那条请求马上得到回答。之后的决定送回 Bot：允许时 Bot 用相同参数再调一次，不再弹卡，然后接着做原来的事；拒绝时 Bot 收到拒绝。

`links` 的 `install` 也走这张卡。`event: approval` 的 data 仍有 `id`、`tool`、`summary`，并带 `deadline`。`tool` 是 `links`。`summary` 第一行是 `install <id>`。这条 Link 声明了密钥时，多一个 `secrets` 数组，里面只是名字，例如 `["TELEGRAM_BOT_TOKEN"]`，没有值。界面为每个名字放一个输入框。提交时 body 是 `{"allow":true,"secrets":{"TELEGRAM_BOT_TOKEN":"<用户填写的值>"}}`。只发 `{"allow":true}` 仍然合法；密钥没填齐时安装不会完成。值不会出现在后续的 `event: result`、transcript 或系统提示里。拒绝时不必带 `secrets`。

Bot 正在这个进程里跑一轮时，用户的消息直接插进那一轮（[ADR 0017](adr/0017-owner-words-go-straight-into-the-turn.md)），不进 inbox。响应立刻以 200 开始事件流：那一轮正在等模型就取消重发，正在跑工具就等它返回再接上；之后的 `delta`、`tool`、`result` 和 `done` 都流到这次请求。原来那条请求收到 `event: done`，`data: {"text":"","handed_off":true}` 后结束，界面在这次请求那边等回答。那一轮出错结束、还没读到这句话时，这次请求自己回答它。正在跑的一轮在另一个进程（终端）时，事件放进 inbox，请求保持打开，那一轮在下一步之间读到；锁空出来后发回那一轮的回答，没读到就自己回答。页面断开或 serve 正在重启时，消息留在 inbox，由 serve 回答。只有 `Server.ForkTurns` 打开时，锁被别的进程占着才仍回 202 `{"status":"queued"}`。

## Bot 状态

`GET /api/bots/status` 返回 `{"bots":[BotStatus]}`，同一套登录。含这个 owner 的全部 Bot，不带步骤。没有活内核的 Bot 是 `idle`，`last_message` 从 transcript 尾巴读。`BotStatus` 字段：`bot`、`state`（`idle`、`running`、`awaiting`、`offline`）、`tool`、`tool_summary`（当前仍在运行的工具参数 description 首行，已脱敏，最多 120 字；无 description 时省略）、`since`（RFC3339，本机时区）、`backgrounds`（分叉后台数）、`agents`（正在执行的子代理数，不含排队）、`completed_unread`（用户尚未读到的子代理或分叉完成汇报数，随既有 transcript 已读游标归零）、`last_message`（最多 80 个字符，写前过替换函数）、`todo`（有未完成项时一行摘要，最多 80 个字符）、`node`（本机为空）、`stale_seconds`。`awaiting` 表示这一轮停在等用户确认。同一个 owner 的远端点合并进来，`node` 写上节点名；节点缓存最多每 10 秒刷新，单次调用超时 2 秒，不可达时返回缓存并把 `state` 写成 `offline`、`stale_seconds` 写成缓存年龄。

`GET /api/bots/{name}/steps?n=20` 返回 `{"steps":[{"time","tool","summary","state"}]}`，是这个 Bot 当前这一轮的步骤环。`summary` 最多 120 个字符，写前过替换函数；`state` 是 `running`、`done`、`error` 或 `awaiting`。`n` 缺省 20，最多 20，小于 1 是 400 `{"error":"bad n"}`。不是这个 owner 的 Bot 是 404。远端 Bot 只给状态，不给步骤，空数组。

`GET /api/bots/{name}/context` 返回这一轮模型实际收到的上下文：`{"bot","target","ceiling","window","requests":[...],"detail":{...}}`。`target` 是软目标（消息到这里后台开始压），`ceiling` 是这次请求的硬上限（含工具定义，到这里轮内同步压），都按当前绑定的模型现算。`requests` 是本轮最近的模型调用，最多 10 次，按发生顺序排列；这是内核在内存里留的快照，进程重启或这个 Bot 还没在本进程里发过请求时是空数组、没有 `detail`，不是 404。每次调用是 `{"step","at","prompt","tools","layers":[{"key","label","tokens","from","to"}],"turn_steps"}`。`step` 是这次唤醒里的第几次调用，从 1 起；`prompt` 是整次请求的估算用量，含工具定义，`tools` 是其中工具定义那一份（用量记录里不含它，所以面板要单独列出）；`layers` 按发送顺序给分层合计，`key` 是 `sys`、`mem`、`block`、`env`、`hist`（早先对话）、`turn`（本轮，从唤醒这一轮的那条消息起）之一，摘要块每块单独一项并带它覆盖的 `from`、`to`；`turn_steps` 是这次调用带上了本轮的几步。`detail` 只描述最新一次调用：`steps` 是本轮的步骤 `{"tool","what","tokens","fail","preview"}`（一次工具调用和它的结果合成一步，`what` 是命令、路径或查询这类参数，`fail` 表示结果以 error 开头或 exit 非 0，`preview` 是结果开头 240 字；`tool` 为空的是回复文字），最多 150 步，再早的丢掉并写进 `skipped_steps`；`turns` 是早先对话每轮 `{"ask","tokens"}`，最多 40 轮，更早的只记 `earlier_turns`、`earlier_tokens`；`blocks` 是摘要块 `{"from","to","tokens","preview"}`；`tool_defs` 是每个工具定义的 `{"name","tokens"}`，从大到小。较早的一次调用的步骤就是 `steps` 的前 `turn_steps` 步。所有文字都过 `RedactText`。只有前台主循环的请求会出现在这里，子代理、记忆抽取和压缩自身的调用不在其中。不是这个 owner 的 Bot 是 404。

`GET /api/bots/{name}/todos` 返回 `{"items":[{"id","content","status","reason"}]}`，是这个 Bot 当前任务的待办清单。没有活动清单时 `items` 是空数组。每一项都已完成或放弃、写入空清单、或换成没有相同 id 的新清单时，旧清单归档，不再出现在这里。用户接着说继续，或前台插话时后台还在跑，活动清单还在。`status` 是 `pending`、`in_progress`、`done` 或 `dropped`。`BotStatus` 多一个 `todo` 字段，有未完成项时是一行摘要（「进度：… · 还剩 N 项待办」，或「待办：还有 N 项」），写前过替换函数，最多 80 个字符；没有未完成项就省略。步骤环不放清单。不是这个 owner 的 Bot 是 404。

## 子代理、通知、群

`POST /api/bots/{name}/agents/{id}/steer`，body `{"text":"..."}`。`POST /api/bots/{name}/agents/{id}/cancel` 可以没有 body。成功 200 `{"status":"<内核返回的一句>"}`。

`GET /api/notes` 返回 `{"notes":[{"id","bot","text","time"}]}`，只含未读。`POST /api/notes/read`，body `{"bot":"ada","ids":["..."]}`，200 `{"status":"ok"}`。不可信来源（webhook、watch、其他 Bot、定时器）的最终回复如果不是空也不是 `SILENT`，也会出现在这里。同一轮里 Bot 已经调用 `send_message_to_user` 时，不再把结尾那句回复再推一条。

`GET /api/groups` 同时返回侧边栏分组和聊天群。聊天群仍在 `groups`：`{"id","name","members","created"}`，`id` 形如 `g_` 加 8 位小写字母或数字，只含当前 owner 全部成员都拥有的群。侧边栏分组见下面的「侧边栏分组」。

`POST /api/groups` 带 `members` 时创建聊天群，body `{"name":"...","members":["ada","bob"]}`。成功 200 就是这个群对象。没有 `members` 时创建的是侧边栏分组，见下。

`GET /api/groups/{id}` 返回 `{"group":{...},"messages":[{"id","time","sender","kind","text"}]}`。`kind` 是 `person` 时这行是用户发的。

`POST /api/groups/{id}/messages`，body `{"text":"..."}`。200 `{"status":"sent"}`。

## 侧边栏分组

这是 Bot 档案上的 `group`，加上这个登录用户自己的一份顺序。不是聊天群。界面按 `order` 画可折叠分组，每组用 `count`；最后一组固定是「未分组」。没有拖拽排序。

`GET /api/groups` 的分组部分：

```json
{
  "order": ["工作", "生活"],
  "folders": [
    {"name": "工作", "count": 2, "bots": ["ada", "bob"]},
    {"name": "生活", "count": 0, "bots": []},
    {"name": "未分组", "count": 1, "bots": ["cara"], "ungrouped": true}
  ],
  "groups": []
}
```

`order` 只含用户建过的分组，不含「未分组」。`folders` 按这个顺序，最后一项永远是「未分组」。组内 Bot 先置顶，再按名字。`count` 是 `bots` 的长度。没有分组时 `order` 是 `[]`，`folders` 只有「未分组」。`groups` 仍是聊天群，上面那一节的对象不变。

`POST /api/groups`，body 只有 `{"name":"工作"}`，没有 `members`。成功 200 `{"name":"工作"}`，名字追加到 `order` 末尾。名字去掉首尾空白，1 到 32 个字符，不能含斜杠、反斜杠或控制字符，也不能叫「未分组」。重名 409，正文含 `group already exists`。坏名字 400。

`PATCH /api/groups/{name}`，body `{"name":"工作台"}`。`{name}` 是现在的分组名，要做 URL 编码。成功 200 `{"name":"工作台"}`。这个 owner 名下、`group` 等于旧名字的 Bot 一起改成新名字。旧名字不存在是 404。新名字已被占用是 409。

`DELETE /api/groups/{name}` 无 body。成功 200 `{"name":"工作"}`。这些 Bot 的 `group` 被清空，于是出现在「未分组」。Bot 目录还在。没有这个分组是 404。「未分组」不能删。

`PATCH /api/bots/{name}` 可以带 `"group":"工作"`，或 `"group":""` 表示挪到未分组。没带 `group` 就不改分组。分组必须已经在这个 owner 的 `order` 里，否则 404。和档案字段可以写在同一次 PATCH 里。

`GET /api/bots` 和 `GET /api/bots/{name}` 多一个 `group`。空字符串或字段省略表示未分组。

命令行：`xbot group list`，`xbot group create <name>`，`xbot group rename <old> <new>`，`xbot group delete <name>`，`xbot group set <bot> <name>`，`xbot group set <bot> --clear`。顺序记在当前终端用户名下。

## Link 目录

M6 的第一片。清单是内置的，不是远程商店。`GET /api/links` 要登录，返回每一条的 `id`、`title`、`kind`（`native` 或 `mcp`）、`capabilities`（`tools`、`events`）、`secrets`（密钥的名字，没有值）、`summary`、`available`。`available` 为 false 的条目还没有运行时。

```json
{"links":[{
  "id":"webhook",
  "title":"Webhook",
  "kind":"native",
  "capabilities":["events"],
  "secrets":[],
  "summary":"接收 HTTP 回调，写成不可信事件。",
  "available":true
}]}
```

`GET /api/bots/{name}/links` 是同一份清单，每条多一个 `installed`。不是这个 owner 的 Bot 是 404。

`POST /api/bots/{name}/links`，body `{"id":"webhook"}`。成功 200 `{"id":"webhook","installed":true}`。这会把对应的 Link 名写进该 Bot 的授权。未知 id 是 404。`available` 为 false 是 409，正文含 `not available yet`，授权不变。已经装过再装一次仍是 200。声明了密钥的条目（Telegram、飞书）也是 409，正文含 `secrets required`，授权和 `secrets.json` 都不变。密钥只能在聊天确认卡里填，或由用户用命令行从同名环境变量安装。

`DELETE /api/bots/{name}/links/{id}` 卸下这条授权。已知 id 成功 200 `{"id","installed":false}`。未知 id 是 404。这条 Link 声明了密钥时，同时删掉这些名字：连接器（Telegram、飞书）的 token 存在保险箱 backend 里，用 `backend.Remove` 删除；只有非连接器的键还留在 `secrets.json`。

连接器 token 的存放：Telegram 与飞书的 token 存在 `vault.Backend` 里（名字 `telegram.token`、`lark.token`，绑定域名分别是 `api.telegram.org` 与 `open.feishu.cn`）。Bot 目录下旧的明文 `secrets.json` 在连接器第一次被打开时自动迁移进 backend，全部写入成功后明文文件被删掉；迁移中途失败则保留明文，下次再迁，重复执行安全。连接器出站请求只携带 `{{secret:telegram.token}}` 这类占位符，真值由 `backend.Transport` 在出口注入。

入站 webhook 的限制：校验 Telegram `X-Telegram-Bot-Api-Secret-Token` 与飞书签名必须拿到明文。当 backend 是本地同用户 Store 时，入站照常用真值校验；当跑着 `xbot vaultd`（backend 是拿不到明文的 Client）时，**Telegram 与飞书的 webhook 入站暂不可用**：请求返回 401，并写一条日志说明是保险箱守护进程不向本进程暴露密钥，不放行、也不做假校验。

`coding-agent` 可以安装。任务记录不走这套登录接口。Bot 用 `code_start`、`code_steer`、`code_queue`、`code_interrupt`、`code_cancel`、`code_show`。完成、失败或取消写成该 Bot 收件箱里的不可信事件。

`xbot runner` 在环回地址上另有编码任务接口，和子代理一样要 bearer token：`POST /v1/code/start`、`POST /v1/code/message`、`GET /v1/code/events`（SSE）。`start` 的正文是任务字段加可选的 `model`（`base_url`、`api_key`、`model`）。密钥只在这一跳里出现，不进 Bot 的 transcript。

## Bot Webhook

都要用户会话，只能管自己的 Bot。

- `GET /api/bots/{name}/webhooks` → `{"hooks":[{"name","auth","url","local_url","public_url?","created"}],"public"}`（`public` 是读这份列表时的公网 base，没有为空串），按配置顺序，**不带密钥**。
- `POST /api/bots/{name}/webhooks`，body `{"name","auth?"}`。`auth` 是 `hmac`（默认，界面叫「签名校验」）或 `bearer`（「令牌」）。名字必须是 `^[a-z0-9][a-z0-9-]{0,63}$`，否则 400「用小写字母、数字和短横线，不要大写或下划线」；重名 409「已有同名 Webhook」。成功 201，同上字段外加明文 `secret`。第一条会顺带给 Bot 写上 `webhook` Link 授权，不用另装。
- `GET /api/bots/{name}/webhooks/{hook}` → 同上，带 `secret`（详情里「显示」用）。没有这条是 404。
- `DELETE /api/bots/{name}/webhooks/{hook}` → 204。只删这条和它的密钥，`webhook` Link 授权保留。

`url` 是要交给外部服务的地址：有公网地址时等于 `public_url`，否则等于 `local_url`（serve 的监听地址）。公网地址依次取 `XBOT_PUBLIC_URL`、隧道地址、账号网关入口 `https://<网关>/pipe/v1/hook/<设备>`（中枢确认了这台设备的待命才有，握手失败、断开或重连中就没有，见 [pipe.md](pipe.md)）。`GET /api/bots/{name}` 带 `webhook_public`（当前公网 base），任务页随轮询比对，变了就重读列表和打开的详情。Bot 名在地址里做了百分号转义。

外部服务投递：`POST <url>`，签名校验带 `X-XBot-Signature: sha256=<hex(HMAC-SHA256(secret, body))>`，令牌带 `Authorization: Bearer <secret>`；可选 `X-XBot-Delivery`（去重）、`X-XBot-Event`、`X-XBot-Sender`。成功 202 `{"status":"accepted"}`，签名不对 401，没有这条 404，正文超 32 KiB 413。

## 连接器入站

不走登录 cookie。`xbot serve` 在同一个 `--http` 上接受：

- `POST /connector/telegram/{bot}`，请求头 `X-Telegram-Bot-Api-Secret-Token` 必须等于该 Bot 的 `TELEGRAM_BOT_TOKEN`。
- `POST /connector/lark/{bot}`，请求头 `X-Lark-Token`，或正文里的 `token` / `header.token`，必须等于 `LARK_APP_SECRET`。

Bot 不存在、名字不合法、或没有装上这条 Link，是 404 `{"status":"not_found"}`。密钥不对或还没写入，是 401 `{"status":"unauthorized"}`。对上的消息是 200 `{"status":"accepted"}`，收件箱里多一条不可信事件，来源是 `telegram` 或 `lark`，正文是 `chat: <id>` 加文本，没有密钥。同一条 `update_id` 或 `message_id` 再来一次是 200 `{"status":"duplicate"}`，不再写第二份。没有文本是 200 `{"status":"ignored"}`。飞书 `{"type":"url_verification","challenge":"..."}` 在密钥对上之后回 `{"challenge":"..."}`，不写收件箱。

出站不在这个接口上。Bot 用 `telegram_send` 或 `lark_send` 落草稿，用户在确认卡点头之后才发出。

命令行：`xbot link list`，`xbot link list <bot>`，`xbot link install <bot> <id>`，`xbot link remove <bot> <id>`。

目录里的 `mcp` 仍然 `available: false`。服务器不是一条目录授权，按下节单独登记。

## 连接应用安装

`POST /api/apps` 接收 `{"id":"context7","secrets":["密钥值"]}`；无密钥条目省略 `secrets`。密钥按 `GET /api/apps` 每行 `fields`（人话标签，如「API 密钥」）的顺序提交，只在探测通过后保存，不出现在列表。真实探测失败返回 422，未添加；需要密钥却未填齐返回 400。Telegram、飞书的密钥保存于主 Bot（或首个 owner Bot）的保险箱；Context7 写该 Bot 的 secrets。探测方式见 [ADR 0031](adr/0031-store-keys-in-settings.md)；飞书只发一个不存在的消息类型，不会投递到群。同一时间只处理一个添加或移除，单次上限 45 秒；请求取消时在途探测终止。旧的自定义应用接口不在此变更范围。

## 连接应用的运行状态

`GET /api/apps` 沿用 owner 登录校验和全局安装列表。MCP 商店条目增加 `load_id`（如 `mcp:playwright`）；已配置的 MCP 增加 `runtime`，键为当前用户的 Bot 名，值为 `{state, tool_count, error?}`。`configured` 表示当前配置尚未成功取得工具；`ready` 表示该 Bot 的匹配实例取得非空工具表；`error` 附带有界、脱敏的失败原因。没有 Bot 时不返回 runtime。读取不启动 MCP 进程、不发网络请求，不将其他配置实例或其他用户的状态混入。

旧 `state=connected` 和顶层 `connected` 仍表达安装/凭据状态，不代表工具可用；前端本次未接入 runtime。`load_id` 可直接交给 `links` 的 load 动作，成功后返回工具名；错误在原工具结果通路内返回。ready 不保证某个工具的浏览器等外部依赖可用。

## MCP 服务器

M6 的第二片。某个 Bot 的 MCP 服务器记在它目录里的 `mcp.json`，权限 0600。文件里只有名字、命令、参数和密钥环境变量的名字，没有密钥的值。

`GET /api/bots/{name}/mcp` 要登录，而且这个人是该 Bot 的 owner。不是 owner 或 Bot 不存在是 404 `{"error":"not found"}`。未登录 401。没有服务器时 `servers` 是空数组。

```json
{"servers":[{
  "name":"docs",
  "command":"/usr/bin/mcp-server",
  "args":["--stdio"],
  "secret_envs":["API_TOKEN"]
}]}
```

`POST /api/bots/{name}/mcp`，body 同样是 `name`、`command`、`args`、`secret_envs`。成功 200，返回刚写入的这一条。名字是 `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`。命令不能空，也不能含换行或 NUL。`secret_envs` 的每一项是 `^[A-Z_][A-Z0-9_]*$`。未知字段 400 `{"error":"bad request"}`，所以不能在 body 里带 token 或密钥的值。名字重复 409，正文含 `already exists`。坏名字、坏命令、坏的变量名是 400。

`DELETE /api/bots/{name}/mcp/{server}` 无 body。成功 200 `{"name":"docs"}`。没有这台服务器是 404 `{"error":"not found"}`。

命令行：`xbot mcp list <bot>`，`xbot mcp add <bot> <name> --command PATH [--arg A]... [--secret-env NAME]...`，`xbot mcp remove <bot> <name>`。`--arg` 和 `--secret-env` 可以重复。打印和接口都不显示密钥的值。

## 用户技能

元数据可选 `title` 是人读短名，新写技能的 `name` 保持 slug；展示优先 title，再 name/slug，description 只作说明。

还是 `skills` 这条 Link。库由本数据根的本机 Bot 共享（含无 owner 和多 owner），文件在数据根的 `skills/<slug>/SKILL.md`。导入用 `xbot skill import`，设置页用下面的库级接口。

`GET /api/skills/sources/{id}`（`id` 为 `agent`｜`codex`｜`claude`，其余 404）列出本机该来源技能库的一级技能目录：`{"source","skills":[{"id","name","title","description"}]}`，只读，库不存在就是空；不返回路径。`POST /api/skills/import` 也接受 `{"source","id","overwrite"}`：`id` 必须是合法 slug（拒绝 `..`、斜杠和大写），服务端读该来源下 `<id>/SKILL.md`（最多 256 KiB）再走同一条导入，同名 409 与覆盖语义同上。来源根默认在当前用户 home 下（`.agents/skills`、`.codex/skills`、`.claude/skills`）。

`GET /api/bots/{name}/skills` 要登录，而且这个人是该 Bot 的 owner。不是 owner 或 Bot 不存在是 404 `{"error":"not found"}`。未登录 401。返回本机技能库的技能，每条带上这个 Bot 有没有停用它。没有技能时 `skills` 是空数组。停用的仍在列表里，这样界面可以再打开。

```json
{"skills":[{
  "slug":"ship",
  "name":"ship",
  "description":"Pack a release",
  "disabled":false
}]}
```

`GET /api/bots/{name}/skills/{slug}` 读这一条。成功 200，有 `slug`、`name`、`title`、`description`、`body`。`body` 是 frontmatter 下面的 markdown，不含密钥以外的限制；这里没有密钥。本机技能库里没有这个 slug 是 404 `{"error":"not found"}`。停用了也能读，读的是用户，不是模型。

`POST /api/bots/{name}/skills/{slug}/disable` 无 body。成功 200 `{"slug":"ship","disabled":true}`。已经停用再停一次仍是 200。库里没有这个 slug 是 404。

`POST /api/bots/{name}/skills/{slug}/enable` 无 body。成功 200 `{"slug":"ship","disabled":false}`。本来就开着再开一次仍是 200。没有这个 slug 是 404。

设置页「技能」按整个库工作，只有开关按 Bot。库级接口都要登录，库是本数据根共享的那一份。

`GET /api/skills` 返回 `{"skills":[{"slug","name","title","description"}]}`，按 slug 排序，没有时是空数组，不带正文，不带停用状态。

`GET /api/skills/{slug}` 返回 `slug`、`name`、`title`、`description`、`enabled_count` 和 `bots:[{"name","enabled"}]`：`enabled_count` 是本数据根全部 Bot 开着这条技能的数量；`bots` 只列这个 owner 名下每个 Bot 是否开着（默认开；顺序同 Bot 列表）。多用户 Bot 同样返回真实启用状态和开关，不再返回 `shared`。不带正文。库里没有这个 slug 是 404 `{"error":"没有找到这个技能"}`。开关仍用上面每个 Bot 的 `enable` / `disable`，重复调用无副作用；服务端把这两个接口串行处理，快速连点不会互相覆盖。

`DELETE /api/skills/{slug}` 把这条技能从库里删掉（整个 `skills/<slug>/` 目录），同时把本数据根所有 Bot 的 `disabled_skills` 里的这个 slug 清掉，以后再导入同名技能时默认开着。成功 200 `{"deleted":true}`；库里没有这个 slug（含不合法的 slug、已经删过）是 404 `{"error":"没有找到这个技能"}`。技能没有选用绑定，所以没有 409/force。和开关接口共用同一把串行锁，并在库写锁内先清开关再删目录。命令行对应 `xbot skill remove <slug>`。

`POST /api/skills/import` body `{"folder","content","overwrite"}`：`content` 是浏览器从用户选的文件夹里读出的 SKILL.md 文本（最多 1 MiB 的请求），`folder` 只是文件夹名，是合法 slug 就当技能目录名，否则取头里的 `name`，仍不合法就 400；请求里不出现本机路径，服务端也不据此读任何路径。成功 200 `{"slug","name"}`。库里已有同名且 `overwrite` 不是 true：409 `{"error":"库里已有同名技能，要覆盖吗？","conflict":true,"name":...}`，库不变。头里缺 `name` 或 `description` 是 400，文件夹名不能用也是 400，错误文字都是给用户看的中文。检查与写入在同一把锁里，并发导入同名只有一个成功，其余得到 409；覆盖不动各 Bot 的开关。

停用名单在这个 Bot 的配置里，不在技能文件里。其他 Bot 不受影响。本机 Bot 运行时均可加载这份库；管理其它 Bot 的开关仍受 `owned` 保护，非 owner 请求该 Bot 的接口返回 404，不泄露其信息。聊天群不挂技能库。

命令行：`xbot skill list`，`xbot skill import <path>`，`xbot skill show <slug>`，`xbot skill disable <bot> <slug>`，`xbot skill enable <bot> <slug>`。list、import、show 用当前数据根，不依赖终端 owner。disable 和 enable 还要求这个 Bot 的 owner 里有这个人，否则是找不到。

## 人设卡

本机一份卡库，全部接口要登录；`id` 给页面用来编辑和选用，界面不展示。

`GET /api/persona-cards` 返回 `{"cards":[{"id","name","preview","search","updated"}]}`，最近改过的在前。`preview` 是正文第一行非空文字，`search` 是正文前 1 KiB，供前端输入时即时过滤，不带全文。带 `?q=` 时只返回卡名或完整正文含这段文字（不分大小写，最多取 200 字）的卡，字段相同；页面以它为准。`GET /api/persona-cards/{id}` 返回 `id`、`name`、`body`、`updated` 和 `bots`（在用的 Bot 数）；id 不合法或没有这张是 404 `{"error":"没有找到这张人设卡"}`。

`POST /api/persona-cards` body `{"name","body"}` 新建，`PUT /api/persona-cards/{id}` 只改出现的字段；成功都返回整张卡。卡名去掉换行、最多 60 字，空名叫「未命名人设卡」；正文去掉首尾空白后不能为空（400「人设卡正文不能为空」），最多 32 KiB；库最多 200 张。

`POST /api/persona-cards/import` body `{"filename","content","confirm"}`：总是新建，不覆盖。卡名取第一行不超过 60 字的非空行（去掉开头的 `#`），否则取文件名去掉扩展名；`filename` 只用来起名，服务端不读任何路径。请求最多 256 KiB。已有同名且 `confirm` 不是 true：409 `{"error":"库里已有同名人设卡","conflict":true,"name"}`，库不变；确认后新卡名加「 (2)」，已有就往上数。检查和写入在同一把锁里。

`DELETE /api/persona-cards/{id}`：有 Bot 在用时 409 `{"error":"N 个 Bot 还在用这张人设卡","conflict":true,"bots":N}`；带 `?force=1` 先把这些 Bot 改回未选用再删，200 `{"deleted":true,"unbound":N}`。查在用、解绑和删除都在卡库锁里；`PUT /api/bots/{name}/persona-card` 也在这把锁里确认卡还在再写绑定，所以删卡后不会绑上已不存在的卡（404）。

`PUT /api/bots/{name}/persona-card` body `{"id"}`，空字符串是未选用。成功 200 `{"name","persona_card":{"id","name"}|null}`；不是这个 owner 的 Bot 404，卡不存在 404，多人共用的 Bot 不能选用（400）。重复选同一张无副作用；和技能开关共用一把锁串行写配置。选用后下一轮固定前缀里的人设就是卡正文。

## 模型

`GET /api/models` 要登录。默认只返回已经配置、并且在认证名单里的模型。`?all=1` 带上未认证的模型。`all` 不是 `0` 或 `1` 是 400 `{"error":"bad all"}`。

```json
{"models":[{
  "id":"fake/scripted",
  "provider":"fake",
  "model":"scripted",
  "context":1024,
  "certified":true
}]}
```

没有可显示的模型时 `models` 是空数组。认证名单是数据根上的 `certified.json`，由 `xbot eval <provider/model>` 在 `proactivity` 和 `proactivity-holdout` 两套都按多数通过后写入。只跑一套不写。默认每条跑 3 次，至少 2 次通过才算这条通过。

## Bot 私聊

`GET /api/bots/{name}/peers/{peer}/messages?limit=N&before=<to>:<seq>`

`{peer}` 也可以写成 `bot@设备`（设备是配对表里的设备名），用来读本机 Bot 跟另一台已配对设备上同名用户的 Bot 的来往：收到的部分来自本机 Bot 自己的 transcript 里 `bot:bot@设备` 事件，发出的部分是它给这个地址调 `message_bot` 的那几行和对应工具结果，退回的通知记为 `kind: "bounce"`。另一半不在本机，看不到。

只读。返回这两个 Bot 之间已经写进 transcript 的私聊，时间从旧到新。调用者必须同时是两边的 owner。不是 owner、Bot 不存在，或者 `{name}` 和 `{peer}` 是同一个名字，都是 404 `{"error":"not found"}`。未登录 401。

每条消息只存在收件 Bot 的 transcript 里，这个接口不另存一份。它读的就是搜索、导出和 `recall_history` 能看见的那几行。群消息不在这里。发送方 transcript 里的 `message_bot` 工具调用也不在这里。还留在收件箱、收件 Bot 还没写进 transcript 的，先不出现。用户跟某个 Bot 的聊天窗口不改：对方 Bot 发来的内容仍是一行淡提示。

`limit` 缺省 50，最大 200。小于 1、大于 200，或不是整数，400 `{"error":"bad limit"}`。

`before` 可选，是排他游标。取客户端已经拿到的最旧一条的 `to` 和 `seq`，写成 `bob:3` 这种 `<收件人>:<seq>`。返回严格排在这条前面的最多 `limit` 条，仍然从旧到新。没有这条消息时 400 `{"error":"bad before"}`。两个 transcript 各自编号，同一个数字可能落在两边，所以游标带上收件人。只写一个数字时，仅当整段线程里恰好一条消息是这个 seq 才接受，否则同样 400。

`seq` 是这条消息在收件 Bot transcript 里的序号，和 `GET /api/bots/{to}/transcript` 的 `seq` 相同。`id` 是写进 transcript 时的事件 id。`from` 是发送的 Bot，`to` 是收件的 Bot。`text` 是发出去的正文，外面的 `<untrusted_event>` 已经去掉。`ts` 是这条 transcript 记录的时间，UTC。`from_bot`/`from_device`/`to_bot`/`to_device` 是拆开的 `from`/`to`，客户端不用自己解析 `bot@设备`；本机的 Bot 没有设备，`from_device`/`to_device` 省掉，设备取配对表里的 Label，没设就是设备名。跨设备的行 `from`/`to` 仍是完整地址，`kind` 只在不普通时出现（退回是 `"bounce"`），`status` 只在发出的行出现：`sent`、`queued`（网关连不上，排队重试）或 `failed`；还没拿到工具结果的算 `sent`。跨设备的 `seq` 是本机 transcript 的序号，游标 `<to>:<seq>` 照样适用。

```json
{"messages":[{
  "id":"ev_0123abcd",
  "seq":3,
  "from":"ada",
  "to":"bob",
  "text":"ping",
  "ts":"2026-10-15T08:00:00Z"
}]}
```

没有消息时 `messages` 是空数组。没有更旧的消息时，这一页比 `limit` 短。

---

## 设备

`GET /api/devices` 返回已配对的设备列表：`{"self","gateway_ok","devices","pairing"}`，每台设备有 `id`、`label`、`name`、`bots`、`bot_avatars`、`seen`、`online` 等字段（`seen`、`added` 是时间，`unpaired`、`online` 是布尔值）。`bots` 照旧是另一台设备上 Bot 的名字数组。`bot_avatars` 是同一批 Bot 的头像，每个列出的 Bot 一项：`{"writer": {"avatar_seed": "writer#2"}, "coach": {"custom": true}}`。种子由对方随设备目录送来，没设种子时取 Bot 名字；`custom` 表示对方用的是上传的图片，图片不过网关，所以界面在能传图之前先画几何字形。对方版本较旧、没送种子时这一项是空的，界面画几何字形，不拿名字当种子（免得和本机同名 Bot 一张脸）。种子先在接收时校验一次，建视图时再校验一次，不合法的丢掉种子、保留 Bot。没有 Bot 时是 `{}`，从不是 `null`。

## 供应商与模型绑定

`GET /api/providers` 返回 `{providers:[{name,protocol,api_key_env,key_configured,models}]}`。只返回环境变量名和配置状态，不返回密钥值。模型项含 `model`、`context`、`vision`、`hosted_web_search`、`available`；`available` 表示协议可执行。每项都是 `providers.json` 里的条目（DeepSeek 是其中名为 `deepseek` 的一项），没有空名的默认端点；`brand` 按 `base_url` 主机名识别，仅供图标和名称展示；未知窗口为 0。`GET /api/models` 也带协议和能力字段，原认证过滤不变。

`GET /api/providers/models?provider=<配置名>`（需登录）实时拉取该供应商模型名单；配置名必须是表里的项；ChatGPT 订阅按保存的配置名读取 SIWC 账号目录（`models[].slug`、`visibility: list`），凭证由既有 vault 注入；未登录或凭证失效时返回 422 并引导重新登录。返回 `{models:[...]}`，字段同模型能力项。名单来自厂商，能力来自现有 ModelSpec 规则；ChatGPT 未配置模型的窗口取账号目录，其余未知窗口为仓内默认 256000。未配密钥、网络失败、非法或超限名单返回 422 短错误；API 供应商成功但无模型时返回空数组；ChatGPT 账号无可用模型时返回 422。该读取不会增加模型条目、认证或绑定；有效 context_length 元数据会刷新已登记模型的窗口，规则见 [供应商](atlas/providers.md)。

`PUT /api/bots/{name}/binding` 接收 `{provider,model}`，成功返回保存的二元绑定。二者为空时清除绑定。模型尚未登记时，先实时拉一次该供应商名单，名单里有且可用就登记（名称与窗口）后再绑定；更换模型浮层按供应商逐个读 `/api/providers/models`，某家失败只在该组显示「模型名单暂时拉不下来」与重试。名单里也没有的模型、协议不支持或缺少密钥为 400；Bot 正在工作为 409。换到更小窗口时先由内核预检并按需压缩；仍超新 H、无法安全压缩或摘要失败为 409（「上下文仍超新窗口，需先删分支/导出」），旧绑定保留，已成功保存的摘要块继续有效。成功后下一轮重建内核，不重写 transcript。

OAuth 供应商额外返回 `oauth`（`openrouter`、`chatgpt` 或 `xai`）；`key_configured` 此时表示 vault 中已保存凭证，不暴露令牌。保险箱不可用时该项带 `auth_error`，其他 key 供应商仍可列出和绑定。

`PUT /api/providers/models/image-generation`（需用户登录）接收 `{provider,model,enabled,image_model}`，更新现有 `providers.json` 中已登记模型的 `hosted_image_generation` 和 `image_generation_model`；只允许 Responses 开启，模型名空串使用厂商默认。成功返回 `{provider,model,hosted_image_generation,image_generation_model}`；未登录 401、无法解析 400、未知绑定/不支持的协议或非法模型名 422。下一轮模型请求读取新配置，无须重建内核。`GET /api/providers`、`GET /api/models` 和实时模型目录携带生图能力字段；`registered` 表示模型已登记、允许保存设置，读取目录不自动登记模型。

`POST /api/providers/oauth/{kind}/start`（需用户登录）返回 `{url}`，供浏览器打开授权窗口。支持 `openrouter`、`chatgpt`、`xai`（xAI 用 Grok CLI 公共客户端 PKCE，回调固定监听 `127.0.0.1:56121/callback`，端口被占用时报人话错误）；重复点击复用十分钟内的待完成尝试；已有另一类登录进行中返回 400。`GET /api/providers/oauth/{kind}/status` 返回 `{state,error?}`，状态为 `idle`、`pending`、`succeeded`、`failed` 或 `cancelled`，错误不含授权响应明文。`POST /api/providers/oauth/{kind}/cancel`（同 start 鉴权）取消进行中的登录：关闭回调监听、状态置 `cancelled`，取消或失败后可立刻重新 start；无可取消时 409。`POST /api/providers/oauth/{kind}/logout`（同 start 鉴权）退出该账号：先停掉进行中的登录，再删除 vault 中的凭证和该 kind 的 OAuth 绑定（同名的 API key 绑定不动），状态回到 `idle`，返回 `{status:"logged_out"}`；什么都没存时也返回 200；不支持的 kind 返回 404，删除失败返回 500 和人话错误。绑在这个供应商上的 Bot 会失去凭证，直到再次登录（重建同名绑定）。退出不等待进行中的自动刷新：刷新写回前会核对凭证仍是它开始时那一份，已被退出删掉就放弃写回（本地与保险箱守护进程同一路径）。设置页只在点击 ChatGPT 订阅卡或 xAI 登录按钮、或详情的「重新登录」时启动授权；轮询成功后刷新供应商列表。回调由短期 loopback listener 接收，不通过对话或 transcript 传递授权码。协议及凭证保存方式见 [供应商](atlas/providers.md)。

Transcript 消息保留原 `content` 字符串投影，另有可选 `parts` 数组，包含 `server_tool` 的 `hosted:{id,name,status,query}` 和 `citation:{url,title,start,end}`；位置按 Unicode 字符计数。助手行另有可选 `thinking` 字符串，是该行可读思考正文（Chat Completions 的 `reasoning_content`、Anthropic thinking 块正文，多段以空行合并），只给「轨迹」用；网页不接收 `vendor_raw`、signature、redacted/encrypted reasoning；Chat Completions 的 `reasoning_content` 也不进 `delta`。消息 SSE 增加 `part` 事件，承载临时 hosted 步骤；最终内容从 transcript 读取。

### Bot 收件箱

`GET /api/bots/{name}/inbox?limit=50` 需登录且为 Bot owner。返回 `{pending_count,items}`，包含待处理与已吸收事件，按事件时间新到旧；主人聊天和确认事件不列入。`limit` 默认 50，范围 1–100；每项含 id、source、type、sender、sender_kind、time、summary 和 status（pending 或 acked）。摘要去除 untrusted_event 标签；Webhook 取首个 delivery 正文，文件监听去除 watch 外壳，定时 routine 只取标签内正文并去掉后续唤醒套话；正文去除首尾空白后最多 1000 字符（截断时加省略号）。接口只读，不执行 Ack。
