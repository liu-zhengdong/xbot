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
  "proactivity":"quiet",
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

`style.length` 是 `short` | `medium` | `long`，空字符串表示没设。`style.emoji` 是 `none` | `spare` | `often`。`proactivity` 是 `quiet` | `normal` | `active`，空字符串按适中处理，并且不会改稳定前缀。免打扰是 UTC 的 `HH:MM`，窗口可以跨过午夜。`timezone` 是 IANA 时区名（例如 `Asia/Shanghai`），空字符串表示用这台机器的本地时区。每轮环境更新里有一行 `now: <本地 RFC3339，精确到分钟> (<时区>)`，不进稳定前缀。`routines[].cron` 和 `routines[].at` 二选一：`at` 是一次性的 RFC3339 时间，触发后 `enabled` 变成 false。`memory` 最多 40 条 profile 事实（person、bot、bot+person）。`agents` 是子代理状态，不是一套独立的任务系统。

`PATCH /api/bots/{name}` 只改出现的字段。空字符串清空该字段。可写：`name`、`description`（最多 160）、`tone`（80）、`length`、`emoji`、`language`（40）、`proactivity`、`quiet_start`、`quiet_end`、`timezone`、`persona`。改名会改目录、配置和群成员；主 Bot 改名后 `primary.json` 跟着改，置顶状态不变。成功 200 `{"name":"<当前名字>"}`。非法取值 400，正文是 `bot: ...`。非法时区同样 400，正文含 `bot: timezone:`。

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
  "time":"2026-10-15T00:00:00Z"
}]}
```

这次 GET 会把该 Bot 的已读游标记到当前长度。`role` 为 `tool` 的行用 `tool_call_id` 对上助手消息里的 `tool_calls`。以 `<environment_` 或 `<memory>` 开头的 user 行是内核上下文，不是主人说的话。Bot 之间的动作如果出现，也是普通 transcript 行，接口没有单独的“系统行”类型。主人聊天里，来自其他 Bot 的一行仍是淡提示。两边来回的原文见下面的「Bot 私聊」。

跨 Bot 阅读不是 HTTP。模型工具 `recall_history` 和 `expand` 有可选的 `bot`。只能读和调用者至少有一个相同 owner 的 Bot。别的 owner、只是同群或能互发消息的 Bot、以及群 id，都拒绝。搜索和展开用对方 Bot 自己的索引。读到的文本包在 `<untrusted_event source="bot:<name>" ... sender="<name>">` 里，没有指令效力。长度上限和读自己的一样（搜索约 4 KiB，展开约 8 KiB）。

`GET /api/bots/{name}/export?format=md|jsonl&tools=0|1` 下载这个 Bot 的 transcript。未登录 401。不是这个 owner 的 Bot 是 404。`format` 缺省是 `md`。`tools` 缺省是 `0`；只有 `1` 才包含工具调用和 `role=tool` 的行。可选 `from` 和 `to`，要成对出现，是包含端点的 seq。

正文按 transcript 段流式写出，不把整份历史装进内存。工具结果如果另存成 blob，写出那一行时读回全文。成功时正文不是 JSON。`md` 的 `Content-Type` 是 `text/markdown`，时间用服务器本机时区，摘要块写成 `## Summary seq <from>-<to>`。`jsonl` 是 `application/x-ndjson`。普通行是 transcript 记录再加 `seq`，`json.Unmarshal` 进记录时忽略 `seq`，所以能再导入，序号还是原来的。`{"kind":"summary","from","to","text","focus","at"}` 是摘要块。每一段写出前过 `RedactText`；保险箱还没安装替换函数时，文本原样出去。`Content-Disposition` 是附件，文件名 `<bot>.md` 或 `<bot>.jsonl`。

`GET /api/bots/{name}/transcript` 仍一次返回全部消息，给界面用。很长的历史用上面的 export。Bot 列表上的未读比较用序号计数，不读段文件。

`POST /api/bots/{name}/messages`，body `{"text":"..."}`，1 到 16 KiB。拿到处理锁时响应是 `text/event-stream`：

- `event: delta`，`data: {"text":"..."}`
- `event: tool`，`data: {"name","summary"}`。`ask_user` 的 summary 是问题，后面每行一个 `1. 选项`。终端会把这段打出来。界面也可以按这个画选择，不必再解析工具参数。
- `event: approval`，`data: {"id","tool","summary"}`。这个工具要主人确认才会跑。界面应立刻展示一张确认卡，写明工具名和 summary，并给出允许、拒绝两个按钮。
- `event: result`，`data: {"name","output"}`，output 截到 4000 字
- `event: done`，`data: {"text":"这一轮拼起来的正文"}`。整段回复去掉空白后正好是 `SILENT` 时不发 `delta`，`text` 是空字符串。工具调用之前流出来的字只是状态，可以出现在 `delta` 里，但不会留在 `done.text`。界面用 `done.text` 作为这轮的最终回复，不要把工具调用前的 `delta` 拼进那条回复。
- `event: error`，`data: {"error":"..."}`

`POST /api/bots/{name}/approvals/{id}`，body `{"allow":true}` 或 `{"allow":false}`。未登录 401。不是这个 owner 的 Bot，或没有这张待确认的卡，是 404 `no such approval`。已经答过是 409。成功 200 `{"status":"ok"}`。60 秒内没有回答，这一次视为不允许，工具不跑。只读的安全命令不发这张卡，包括 `date`、`pwd`、`whoami`、`uname`、`hostname`、`id`、`echo`、`printf`、`true`、`false`、`uptime`、`ls`、`which`、`env`、`printenv`、`cal`，而且命令里不能有管道、重定向、分号或命令替换。这条链路上根本没有确认人时，工具结果是 `not run: no approver available on this link`，不会写成主人拒绝。主人明确拒绝才是 `denied by user`。

锁被别人占着时不跑模型，只把事件放进 inbox，202 `{"status":"queued"}`。

## 子代理、通知、群

`POST /api/bots/{name}/agents/{id}/steer`，body `{"text":"..."}`。`POST /api/bots/{name}/agents/{id}/cancel` 可以没有 body。成功 200 `{"status":"<内核返回的一句>"}`。

`GET /api/notes` 返回 `{"notes":[{"id","bot","text","time"}]}`，只含未读。`POST /api/notes/read`，body `{"bot":"ada","ids":["..."]}`，200 `{"status":"ok"}`。不可信来源（webhook、watch、其他 Bot、定时器）的最终回复如果不是空也不是 `SILENT`，也会出现在这里；主动程度挡下或免打扰暂存的不会出现。同一轮里 Bot 已经调用 `notify_owner` 时，不再把结尾那句回复再推一条。

`GET /api/groups` 返回当前 owner 全部成员都拥有的群：`{"groups":[{"id","name","members","created"}]}`。`id` 形如 `g_` 加 8 位小写字母或数字。

`POST /api/groups`，body `{"name":"...","members":["ada","bob"]}`。成功 200 就是这个群对象。

`GET /api/groups/{id}` 返回 `{"group":{...},"messages":[{"id","time","sender","kind","text"}]}`。`kind` 是 `person` 时这行是主人发的。

`POST /api/groups/{id}/messages`，body `{"text":"..."}`。200 `{"status":"sent"}`。

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
