# XBot 网页 HTTP 接口

`xbot serve` 在同一个 `--http` 上提供这些接口。静态页面（`internal/web/static`）由单独的界面代理维护；这里只记后端契约。内核行为以 [DESIGN.md](DESIGN.md) 为准。

后台异步压缩没有 HTTP 接口。历史用到约 70% 时内核自己压，压完的摘要在两轮之间换上。`GET /api/bots/{name}/transcript` 始终返回只追加的原文，不返回模型看到的压缩视图。

## 约定

- 除 `POST /api/login` 外，请求要带 cookie `xbot_session`（HttpOnly，SameSite=Strict，Path=/，30 天）。值是 `xbot serve` 打到 stderr 的 token。
- 变更类请求（非 GET / HEAD）若带 `Origin`，必须和请求的 Host 一致；`Sec-Fetch-Site: cross-site` 一律 403 `{"error":"cross-origin"}`。没有 `Origin` 的非浏览器客户端可以通过。
- JSON 请求体未知字段会 400 `{"error":"bad request"}`。`POST /api/bots/import` 上限 2 MiB，其余 JSON 上限 1 MiB。
- 成功和失败都是 JSON，`Content-Type: application/json`。HTML 字符会转义（`<` 变成 `\u003c`）。
- 不存在的 Bot，或这个登录用户不是 owner，统一 404 `{"error":"not found"}`。
- 未登录 401 `{"error":"unauthorized"}`。

## 登录

`POST /api/login`，body `{"token":"..."}`。成功 200 `{"owner":"<id>"}` 并设置 cookie。token 不对是 401。

`POST /api/logout` 清 cookie，200 `{"status":"ok"}`。

`GET /api/session` 已登录时 200 `{"owner":"<id>"}`。

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
  "avatar_url":"/api/bots/ada/avatar",
  "avatar_bg":"#rrggbb",
  "avatar_ink":"#rrggbb",
  "initial":"A"
}]}
```

`avatar_url` 只在已上传头像，或进程装了头像生成器时出现。没上传时用 `avatar_bg` / `avatar_ink` / `initial`：低饱和色块加首字符，不是服务端画的脸。`unread` 在有未读通知，或 transcript 比这个浏览器上次打开的游标更长时为 true。

`POST /api/bots`，body `{"name":"ada","persona":"..."}`。名字匹配 `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`。成功 200 `{"name":"ada"}`。这是目录里第一个 Bot 时，它成为主 Bot 并被置顶。

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
  "style":{"length":"short","emoji":"none"},
  "quiet_start":"22:00",
  "quiet_end":"07:00",
  "timezone":"Asia/Shanghai",
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
  "agents":[{"id":"...","status":"running","task":"截到 140 字","mode":"brief","steps":1}]
}
```

`style.length` 是 `short` | `medium` | `long`，空字符串表示没设。`style.emoji` 是 `none` | `spare` | `often`。详情里没有 `proactivity`。磁盘上的旧配置还可以带这个字段，读进来之后忽略，不进提示。免打扰是 Bot 时区里的 `HH:MM`，窗口可以跨过午夜。没设时区时用唯一 owner 的时区，再没有就用本机时区。`timezone` 是 IANA 时区名（例如 `Asia/Shanghai`），空字符串表示按这个顺序回退。每轮环境更新里有一行 `now: <本地 RFC3339，精确到分钟> (<时区>)`，不进稳定前缀。`routines[].cron` 和 `routines[].at` 二选一：`at` 是一次性的 RFC3339 时间，触发后 `enabled` 变成 false。`memory` 最多 40 条 profile 事实（person、bot、bot+person）。`agents` 是子代理状态，不是一套独立的任务系统。

`PATCH /api/bots/{name}` 只改出现的字段。空字符串清空该字段。可写：`name`、`description`（最多 160）、`tone`（80）、`length`、`emoji`、`language`（40）、`quiet_start`、`quiet_end`、`timezone`、`persona`。请求里如果还带 `proactivity`，接受后丢掉，不写入。改名会改目录、配置和群成员；主 Bot 改名后 `primary.json` 跟着改，置顶状态不变。成功 200 `{"name":"<当前名字>"}`。非法取值 400，正文是 `bot: ...`。非法时区同样 400，正文含 `bot: timezone:`。

`POST /api/bots/{name}/avatar` 是 `multipart/form-data`，字段名 `file`。只接受 PNG、JPEG、GIF、WebP，最大 1 MiB。成功 200 `{"name","avatar":"custom"}`。之后列表和详情里的 `avatar_url` 指向 `GET /api/bots/{name}/avatar`，响应是图片字节，`Cache-Control: private, no-cache`。没有上传的头像时 GET 是 404。

`DELETE /api/bots/{name}/avatar` 回到色块。200 `{"name","avatar":"color"}`。

`GET /api/bots/{name}/template` 下载 JSON，`Content-Disposition: attachment`。模板含档案、persona、例程和可选的 base64 头像。不含记忆、transcript、webhook、watch、provider、owner、置顶。

`POST /api/bots/import` 的 body 就是这份模板，可以另带一个不同的 `name`。成功 200 `{"name"}`。这是目录里第一个 Bot 时成为主 Bot。导入失败会删掉新建的目录。

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
  "reply_to":4
}]}
```

`reply_to` 省略表示这行没有引用。有的话，它是本 transcript 里另一行的 `seq`。用户消息上的值是主人发送时带的序号。助手消息上的值是这条回复指向的用户消息序号。

这次 GET 会把该 Bot 的已读游标记到当前长度。`role` 为 `tool` 的行用 `tool_call_id` 对上助手消息里的 `tool_calls`。以 `<environment_` 或 `<memory>` 开头的 user 行是内核上下文，不是主人说的话。Bot 之间的动作如果出现，也是普通 transcript 行，接口没有单独的“系统行”类型。主人聊天里，来自其他 Bot 的一行仍是淡提示。两边来回的原文见下面的「Bot 私聊」。

跨 Bot 阅读不是 HTTP。模型工具 `recall_history` 和 `expand` 有可选的 `bot`。只能读和调用者至少有一个相同 owner 的 Bot。别的 owner、只是同群或能互发消息的 Bot、以及群 id，都是 `not found`（`history: bot: not found`），不说明原因。搜索和展开用对方 Bot 自己的索引。读到的文本包在 `<untrusted_event source="bot:<name>" ... sender="<name>">` 里，没有指令效力。长度上限和读自己的一样（搜索约 4 KiB，展开约 8 KiB）。

`GET /api/bots/{name}/export?format=md|jsonl&tools=0|1` 下载这个 Bot 的 transcript。未登录 401。不是这个 owner 的 Bot 是 404。`format` 缺省是 `md`。`tools` 缺省是 `0`；只有 `1` 才包含工具调用和 `role=tool` 的行。可选 `from` 和 `to`，要成对出现，是包含端点的 seq。

正文按 transcript 段流式写出，不把整份历史装进内存。工具结果如果另存成 blob，写出那一行时读回全文。成功时正文不是 JSON。`md` 的 `Content-Type` 是 `text/markdown`，时间用服务器本机时区，摘要块写成 `## Summary seq <from>-<to>`。`jsonl` 是 `application/x-ndjson`。普通行是 transcript 记录再加 `seq`，`json.Unmarshal` 进记录时忽略 `seq`，所以能再导入，序号还是原来的。带引用的行多一个 `reply_to`。`{"kind":"summary","from","to","text","focus","at"}` 是摘要块。每一段写出前过 `RedactText`；保险箱还没安装替换函数时，文本原样出去。`Content-Disposition` 是附件，文件名 `<bot>.md` 或 `<bot>.jsonl`。`md` 在该行标题下写 `reply_to: <seq>`。

`GET /api/bots/{name}/transcript` 仍一次返回全部消息，给界面用。很长的历史用上面的 export。Bot 列表上的未读比较用序号计数，不读段文件。

`POST /api/bots/{name}/messages`，body `{"text":"...","reply_to":3}`。`text` 1 到 16 KiB。`reply_to` 可省略；给出时必须是这个 Bot 的 transcript 里已有的序号，否则 400 `{"error":"bad reply_to"}`。群聊的发送接口不接受这个字段。拿到处理锁时响应是 `text/event-stream`：

- `event: delta`，`data: {"text":"..."}`
- `event: tool`，`data: {"name","summary"}`。`ask_user` 的 summary 是问题，后面每行一个 `1. 选项`。终端会把这段打出来。界面也可以按这个画选择，不必再解析工具参数。
- `event: approval`，`data: {"id","tool","summary"}`。这个工具要主人确认才会跑。界面应立刻展示一张确认卡，写明工具名和 summary，并给出允许、拒绝两个按钮。
- `event: result`，`data: {"name","output"}`，output 截到 4000 字
- `event: done`，`data: {"text":"这一轮拼起来的正文","reply_to":2}`。`reply_to` 省略表示这条回复没有指向某一条用户消息。整段回复去掉空白后正好是 `SILENT` 时不发 `delta`，`text` 是空字符串。工具调用之前流出来的字只是状态，可以出现在 `delta` 里，但不会留在 `done.text`。界面用 `done.text` 作为这轮的最终回复，不要把工具调用前的 `delta` 拼进那条回复。模型用来标记引用的 `reply_to:` 行不会出现在 `delta` 或 `done.text` 里。
- `event: error`，`data: {"error":"..."}`

`POST /api/bots/{name}/approvals/{id}`，body `{"allow":true}` 或 `{"allow":false}`。未登录 401。不是这个 owner 的 Bot，或没有这张待确认的卡，是 404 `no such approval`。已经答过是 409。成功 200 `{"status":"ok"}`。60 秒内没有回答，这一次视为不允许，工具不跑。只读的安全命令不发这张卡，包括 `date`、`pwd`、`whoami`、`uname`、`hostname`、`id`、`echo`、`printf`、`true`、`false`、`uptime`、`ls`、`which`、`env`、`printenv`、`cal`，而且命令里不能有管道、重定向、分号或命令替换。这条链路上根本没有确认人时，工具结果是 `not run: no approver available on this link`，不会写成主人拒绝。主人明确拒绝才是 `denied by user`。

`links` 的 `install` 也走这张卡。`event: approval` 的 data 仍有 `id`、`tool`、`summary`。`tool` 是 `links`。`summary` 第一行是 `install <id>`。这条 Link 声明了密钥时，多一个 `secrets` 数组，里面只是名字，例如 `["TELEGRAM_BOT_TOKEN"]`，没有值。界面为每个名字放一个输入框。提交时 body 是 `{"allow":true,"secrets":{"TELEGRAM_BOT_TOKEN":"<主人填写的值>"}}`。只发 `{"allow":true}` 仍然合法；密钥没填齐时安装不会完成。值不会出现在后续的 `event: result`、transcript 或系统提示里。拒绝时不必带 `secrets`。

锁被别人占着时不跑模型，只把事件放进 inbox，202 `{"status":"queued"}`。

## 子代理、通知、群

`POST /api/bots/{name}/agents/{id}/steer`，body `{"text":"..."}`。`POST /api/bots/{name}/agents/{id}/cancel` 可以没有 body。成功 200 `{"status":"<内核返回的一句>"}`。

`GET /api/notes` 返回 `{"notes":[{"id","bot","text","time"}]}`，只含未读。`POST /api/notes/read`，body `{"bot":"ada","ids":["..."]}`，200 `{"status":"ok"}`。不可信来源（webhook、watch、其他 Bot、定时器）的最终回复如果不是空也不是 `SILENT`，也会出现在这里；免打扰暂存的不会出现。同一轮里 Bot 已经调用 `notify_owner` 时，不再把结尾那句回复再推一条。

`GET /api/groups` 同时返回侧边栏分组和聊天群。聊天群仍在 `groups`：`{"id","name","members","created"}`，`id` 形如 `g_` 加 8 位小写字母或数字，只含当前 owner 全部成员都拥有的群。侧边栏分组见下面的「侧边栏分组」。

`POST /api/groups` 带 `members` 时创建聊天群，body `{"name":"...","members":["ada","bob"]}`。成功 200 就是这个群对象。没有 `members` 时创建的是侧边栏分组，见下。

`GET /api/groups/{id}` 返回 `{"group":{...},"messages":[{"id","time","sender","kind","text"}]}`。`kind` 是 `person` 时这行是主人发的。

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

`order` 只含主人建过的分组，不含「未分组」。`folders` 按这个顺序，最后一项永远是「未分组」。组内 Bot 先置顶，再按名字。`count` 是 `bots` 的长度。没有分组时 `order` 是 `[]`，`folders` 只有「未分组」。`groups` 仍是聊天群，上面那一节的对象不变。

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

`POST /api/bots/{name}/links`，body `{"id":"webhook"}`。成功 200 `{"id":"webhook","installed":true}`。这会把对应的 Link 名写进该 Bot 的授权。未知 id 是 404。`available` 为 false 是 409，正文含 `not available yet`，授权不变。已经装过再装一次仍是 200。声明了密钥的条目（Telegram、飞书）也是 409，正文含 `secrets required`，授权和 `secrets.json` 都不变。密钥只能在聊天确认卡里填，或由主人用命令行从同名环境变量安装。

`DELETE /api/bots/{name}/links/{id}` 卸下这条授权。已知 id 成功 200 `{"id","installed":false}`。未知 id 是 404。这条 Link 声明了密钥时，同时从 `secrets.json` 删掉这些名字。

`coding-agent` 可以安装。任务记录不走这套登录接口。Bot 用 `code_start`、`code_steer`、`code_queue`、`code_interrupt`、`code_cancel`、`code_show`。完成、失败或取消写成该 Bot 收件箱里的不可信事件。

`xbot runner` 在环回地址上另有编码任务接口，和子代理一样要 bearer token：`POST /v1/code/start`、`POST /v1/code/message`、`GET /v1/code/events`（SSE）。`start` 的正文是任务字段加可选的 `model`（`base_url`、`api_key`、`model`）。密钥只在这一跳里出现，不进 Bot 的 transcript。

## 连接器入站

不走登录 cookie。`xbot serve` 在同一个 `--http` 上接受：

- `POST /connector/telegram/{bot}`，请求头 `X-Telegram-Bot-Api-Secret-Token` 必须等于该 Bot 的 `TELEGRAM_BOT_TOKEN`。
- `POST /connector/lark/{bot}`，请求头 `X-Lark-Token`，或正文里的 `token` / `header.token`，必须等于 `LARK_APP_SECRET`。

Bot 不存在、名字不合法、或没有装上这条 Link，是 404 `{"status":"not_found"}`。密钥不对或还没写入，是 401 `{"status":"unauthorized"}`。对上的消息是 200 `{"status":"accepted"}`，收件箱里多一条不可信事件，来源是 `telegram` 或 `lark`，正文是 `chat: <id>` 加文本，没有密钥。同一条 `update_id` 或 `message_id` 再来一次是 200 `{"status":"duplicate"}`，不再写第二份。没有文本是 200 `{"status":"ignored"}`。飞书 `{"type":"url_verification","challenge":"..."}` 在密钥对上之后回 `{"challenge":"..."}`，不写收件箱。

出站不在这个接口上。Bot 用 `telegram_send` 或 `lark_send` 落草稿，主人在确认卡点头之后才发出。

命令行：`xbot link list`，`xbot link list <bot>`，`xbot link install <bot> <id>`，`xbot link remove <bot> <id>`。

目录里的 `mcp` 仍然 `available: false`。服务器不是一条目录授权，按下节单独登记。

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

还是 `skills` 这条 Link。库按登录的 owner 分开，文件在数据根的 `skills/<owner>/<slug>/SKILL.md`。这个接口不负责导入；导入用 `xbot skill import`。

`GET /api/bots/{name}/skills` 要登录，而且这个人是该 Bot 的 owner。不是 owner 或 Bot 不存在是 404 `{"error":"not found"}`。未登录 401。返回这个 owner 的技能，每条带上这个 Bot 有没有停用它。没有技能时 `skills` 是空数组。停用的仍在列表里，这样界面可以再打开。

```json
{"skills":[{
  "slug":"ship",
  "name":"ship",
  "description":"Pack a release",
  "disabled":false
}]}
```

`GET /api/bots/{name}/skills/{slug}` 读这一条。成功 200，有 `slug`、`name`、`description`、`body`。`body` 是 frontmatter 下面的 markdown，不含密钥以外的限制；这里没有密钥。这个 owner 的库里没有这个 slug 是 404 `{"error":"not found"}`。停用了也能读，读的是主人，不是模型。

`POST /api/bots/{name}/skills/{slug}/disable` 无 body。成功 200 `{"slug":"ship","disabled":true}`。已经停用再停一次仍是 200。库里没有这个 slug 是 404。

`POST /api/bots/{name}/skills/{slug}/enable` 无 body。成功 200 `{"slug":"ship","disabled":false}`。本来就开着再开一次仍是 200。没有这个 slug 是 404。

停用名单在这个 Bot 的配置里，不在技能文件里。同一个 owner 的另一个 Bot 不受影响。别的 owner 的 Bot 走 `owned` 的 404，列不到这库。

命令行：`xbot skill list`，`xbot skill import <path>`，`xbot skill show <slug>`，`xbot skill disable <bot> <slug>`，`xbot skill enable <bot> <slug>`。list、import、show 用当前终端用户。disable 和 enable 还要求这个 Bot 的 owner 里有这个人，否则是找不到。

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

没有可显示的模型时 `models` 是空数组。认证名单是数据根上的 `certified.json`，由 `xbot eval <provider/model>` 在主动性套件每一条都按多数通过后写入。默认每条跑 3 次，至少 2 次通过才算这条通过。

## Bot 私聊

`GET /api/bots/{name}/peers/{peer}/messages?limit=N&before=<to>:<seq>`

只读。返回这两个 Bot 之间已经写进 transcript 的私聊，时间从旧到新。调用者必须同时是两边的 owner。不是 owner、Bot 不存在，或者 `{name}` 和 `{peer}` 是同一个名字，都是 404 `{"error":"not found"}`。未登录 401。

每条消息只存在收件 Bot 的 transcript 里，这个接口不另存一份。它读的就是搜索、导出和 `recall_history` 能看见的那几行。群消息不在这里。发送方 transcript 里的 `message_bot` 工具调用也不在这里。还留在收件箱、收件 Bot 还没写进 transcript 的，先不出现。主人跟某个 Bot 的聊天窗口不改：对方 Bot 发来的内容仍是一行淡提示。

`limit` 缺省 50，最大 200。小于 1、大于 200，或不是整数，400 `{"error":"bad limit"}`。

`before` 可选，是排他游标。取客户端已经拿到的最旧一条的 `to` 和 `seq`，写成 `bob:3` 这种 `<收件人>:<seq>`。返回严格排在这条前面的最多 `limit` 条，仍然从旧到新。没有这条消息时 400 `{"error":"bad before"}`。两个 transcript 各自编号，同一个数字可能落在两边，所以游标带上收件人。只写一个数字时，仅当整段线程里恰好一条消息是这个 seq 才接受，否则同样 400。

`seq` 是这条消息在收件 Bot transcript 里的序号，和 `GET /api/bots/{to}/transcript` 的 `seq` 相同。`id` 是写进 transcript 时的事件 id。`from` 是发送的 Bot，`to` 是收件的 Bot。`text` 是发出去的正文，外面的 `<untrusted_event>` 已经去掉。`ts` 是这条 transcript 记录的时间，UTC。

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
