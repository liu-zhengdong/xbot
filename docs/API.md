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

## 本机 CLI profile 操作

这组接口位于当前 Kernel 的独立回环 `XBOT_ENDPOINT`，不是网页 `--http` 端口；serve、桌面和独立 chat 同协议，不经 messages/events 或对话处理锁。调用须无浏览器 Origin/Fetch-Site，Host 为该回环地址，`X-XBot-Root` 为规范数据根；根不匹配 409。self 由该 endpoint 绑定的 Resources 识别；改名后同一 endpoint 继续认新名，`XBOT_BOT` 只作诊断，不接收其它 Bot 作为目标，也不作为主人认证或隔离承诺。

- `GET /v1/profile`：读当前名字、简介、时区和头像元数据，不修改资料。
- `PATCH /v1/profile`：JSON 为可选字符串 `name`、`description`、`timezone`、`avatar_seed`，至少一项，可组合。简介最多 160 字符；简介/时区的空字符串清除，名字必须合法且 portable；seed trim 后为 1–80 字符，不含尖括号、单双引号、&、反引号或控制符。seed 将头像切为 generated。JSON 请求及回包各最多 64 KiB；未知字段、额外 JSON、非法组合或预期目标冲突在业务写入前拒绝。name+fields 合入最终 fresh config 写入，未请求配置保留；persona/group/role/预算不进入这个 JSON。
- `POST /v1/profile/avatar`：原始图片二进制 body，最多 1 MiB。按字节嗅探 PNG/JPEG/GIF/WebP，不信任 Content-Type；不传 base64、服务端文件路径或组合 profile 元数据。超限 413，非法图片非 2xx。CLI 对应 `profile avatar --self --file FILE --json`。
- `DELETE /v1/profile/avatar`：空 body，清除上传状态并回到原 seed；CLI 对应 `profile avatar --self --clear --json`。上传/clear 是独立动作，不与 set 组合。

资料回执保留 `{name,description,timezone,avatar,avatar_seed,scope:"runtime",applied}`，另有 `moved`，按结果提供 `previous_name`、`applied_fields` 与脱敏 `error`。GET 的 applied 为 false，同名且无其它字段不报告物理移动。前置 guard/解析失败且尚无领域结果时回 `{error}`；领域方法已产生结果，即使后期 IO 错误返回非 2xx，也保留实际名字和完成字段，CLI 输出该结果后非 0 退出，不把回执丢成裸错误。上传按实际成功点报告 `avatar_file`、`avatar_mime`、`avatar`；seed/clear 的元数据成功后清旧图 best-effort，清理失败不伪报未保存。多文件操作不承诺全事务，目录实际留在新名时后续路由跟随新名。

实际资料变化沿当前活轮次 Sink 发送 `profile` SSE `{name,previous_name?}`，网页再读原资料接口；不新增 Inbox 类型。入口随宿主/任务生命周期关闭，不可达时 CLI 明确非 0，不另启 Kernel 或静默离线改文件。无 endpoint 保原人用离线用途。完整命令见 [运行时普通资料](atlas/multibot.md#运行时普通资料)。

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

`GET /api/session` 对无转发头的本机回环连接、回环 Host 且同源的请求签发本机用户会话；远端仍须已有用户会话。已登录时 200 `{"owner":"<id>","app":<bool>,"can_restart":<bool>,"version":"<版本>","tip":"<构建短号或空串>"}`。`can_restart` 仅在桌面运行模式且已有重启回调时为 true，不支持的实例明确返回 false。`version` 是构建版本去掉开头的 `v` 和 `+` 之后的内容，例如 `0.3.1-beta.2`。开发构建短号单独通过 `tip` 返回，不参与版本比较或自动升级。

`POST /api/app/theme` 只在桌面版存在，body `{"theme":"light|dark|system"}`，让窗口的系统按钮跟随 XBot 自己的明暗主题。成功 204，没有 body；值不对 400；不是桌面版 404。

`POST /api/app/restart` 不带正文，要求用户认证与同源，只在 `can_restart:true` 的桌面实例开放，其余返回 404。接收回调失败回 500 `{"error":"原因"}`；成功回 202 `{"status":"restarting"}`，表示请求已交给既有重启循环，后续等待前台轮结束并重新打开客户端，不代表 Relaunch 已经完成。当前 macOS／Linux 装配此回调，Windows 沿现有不支持的 Relaunch 边界不开放；普通 serve 不开放。

## 主人头像

整机一份，唯一存盘为数据根 `owner-avatar`。以下接口要求用户会话，写入沿用同源校验。

- `/api/session` 增加 `owner_avatar_url`：有自定义图时为 `/api/owner/avatar`，否则为空串；`owner_name` 不变。
- `POST /api/owner/avatar`：multipart 字段 `file`，PNG/JPEG/GIF/WebP，按字节嗅探，图片最多 1 MiB，multipart 请求最多 2 MiB。类型不支持 400、图片超限 413、multipart 无效 400；写盘失败 500，不替换旧图。成功 200 `{"owner_avatar_url":"/api/owner/avatar"}`。与 Bot 相同，不额外压缩。
- `GET /api/owner/avatar`：返回原图字节，Content-Type 按字节定，`X-Content-Type-Options: nosniff`、`Cache-Control: private, no-cache`；没有图 404。
- `DELETE /api/owner/avatar`：幂等删除，200 `{"owner_avatar_url":""}`，界面恢复按 `owner_name` 画线稿。

## 图片生成

- `GET /api/image-gen`：返回 `{enabled,mode,provider,providers:[{id,name,logged_in}]}`；`mode` 为 `auto`（默认）或 `local`（总用设置里的家）。默认关，选择尚未保存时优先已登录的账号，否则 OpenAI；`openai` 对应 ChatGPT OAuth，`xai` 对应 xAI OAuth。只返回登录布尔值，不返回保险箱原文。
- `PUT /api/image-gen`：提交 `{enabled:true,mode:"auto|local",provider:"openai|xai"}`（省略 mode 取 auto），返回当前设置。正文上限 4 KiB，拒绝未知字段、缺失值、非法供应商和尾随 JSON。认证与同源要求复用现有设置接口。写入现有 `web.json`，与搜索共用文件锁，重复值不重写。
- 全局关闭不挂生图；开启后仅所选家的已登录 OAuth Responses 绑定可用。每轮开始固定配置，进行中的一轮保留快照；产物仍进工作区，不自动挂气泡。

## 网页搜索

以下接口均要求登录，写入还要求同源。请求最多 4 KiB，拒绝未知字段和多份 JSON。

- `GET /api/web-search`：返回 `{enabled,mode,provider,providers:[{id,name,needs_key,key_configured}]}`。缺省开、自动、DuckDuckGo；只返回密钥是否存在，不返回值和配置路径。
- `PUT /api/web-search`：完整提交 `{enabled:true,mode:"auto|local",provider:"ddg|deepseek|brave|tavily|serper|zhipu|exa|firecrawl|perplexity"}`，成功返回当前设置。全局共享，不重建 Bot 内核。新一轮对话和新远端任务使用新设置，运行中的远端任务保留启动快照。
- `POST /api/web-search/key`：`{provider:"deepseek",key:"…"}`，空值表示重试本机已有密钥。DeepSeek 复用实际搜索路径探测，15 秒超时；经本机代理的假 IP 也能探测，只向固定官方地址发送。成功即保存，回 `{ok:true}`。无效或连不上回 422，不保存失败值；其它七家目前同样回探测失败。正文和错误不回显密钥。

## 权限总开关

`GET /api/permissions` 要登录，返回 `{"confirm":"off"}` 或 `{"confirm":"table"}`。缺失、不可读或损坏的 `permissions.json` 都按关处理。

`PUT /api/permissions` 要登录，正文为 `{"confirm":"off|table"}`，成功返回当前值；未知字段、非法值、多份 JSON 对象为 400。它与 `xbot permissions on|off` 共用 `internal/permissions` 和数据根的 `permissions.json`，运行中的 Bot 在下一次权限判断时读取新值。权限分类不变。

## Bot 运行时 CLI

原source Shell使用 `"$XBOT_BIN" list --self --json`、`info [NAME] --self [--steps N] --json`、`new NAME --self (--persona TEXT|--persona-file FILE|-) --json`、`delete NAME --self --json`；没有bot子命令。self选source endpoint/ref而非目标，XBOT_BOT仅诊断。help离线，按arity解析self/help（旗标值可以恰为--self），main在root/default/legacy初始化前分流；runtime失败不回离线，显式root必须匹配规范XBOT_ROOT。无self原list/new/delete/pin/primary/fork保持离线，info只runtime。

同原loopback/Host/browser/root guard：GET `/v1/bot/list`，GET `/v1/bot/info?bot=NAME&steps=N`，POST `/v1/bot/create` 单对象 `{name,persona}`，DELETE `/v1/bot/delete?name=NAME`。create拒未知字段/非对象/尾对象，persona-file/stdin max+1及最终转义请求≤1MiB。trim name/persona、职责非空；运行new不接provider/model、无默认职责，不以授权取名代替授权编职责。

List小回 `{local:[{name,persona_preview?}],remote:[{name,seen}],count}`：local排self、至少一个非空owner交集，共享有交集可见、foreign/orphan不互见，原80字节persona预览。remote只读既有目录缓存，source Owners匹配设备Person且未unpair；Received非零且不早于7天、每设备先64再ValidName，name=bot@device，Seen沿原directory generation缓存时间，不擅加XKey/NoBotMessaging过滤或网络探测。无项为真实[]；不是全root/用户HTTP列表。

Info小回 `{bots:[BotStatus],steps:[BotStep],count}`。请求时调原k.BotStatuses/BotSteps，不能publish时捕Statuses=nil（Web后挂）。非nil Host整体视图不再按source交集重筛；Web按登录owner OwnerBots含owned self、cached live/其它idle+hold；fallback仅self+VisibleBots，others idle；原nil/skip错误行为保。Bot空取全，指定未出现明确拒；steps>10夹10、<=0不取，只named或结果1个才取；Host只现cached K、fallback仅self，不造remote status或新Kernel。cached K保旧Bot快照时，completed_unread的Count/增量Walk仍经同Resources当前route，host cache查发在route外，读取不复活旧transcript；cold无Resources保原transcript-only增量读取，不依赖Inbox是否有效。BotStatus保bot/state、tool/tool_summary/since、backgrounds/agents/completed_unread、last_message/todo、tokens；tokens保prompt/completion/cached/turn_completion/session_prompt/session_completion/session_cached/window/estimated（session_*仅原计数）。Step仅time/tool/state/summary，不暴露私有callID/description，实际没有node/stale_seconds。

Create/Delete每次复核当前source primary。Create复制source全部Owners/Links（Links空合法），用原global默认provider/首model，不继承source binding/role/budget，不建目标Kernel/wake。回 `{name,applied,stage,error?}`；实际default_binding→config→state→persona→owners，successful config后后段失败applied仍true，原persona32KiB限制仍在WritePersona，不假提前零写；EnsureDefault亦可写全局配置。Delete仅原VisibleBots内本地目标或self，仍受primary/last保护，回 `{name,removed,stage,error?}`；原host同entry暂停Jobs/MCP→锁外owned Jobs join→重核→原fence/FinishDelete，不扩大all-child/code/remote/MCP join。三层回调透真实bool；RemoveAll后vault-key错仍removed:true/stage:vault_key、非0并终态清资源，不err推未删/stat猜/rollback/重开准入。complete仅动作已完成，不是新Bot已wake。

生产permissive不增卡、不强普通Delete原callctx；strict固定Delete恒需确认，不查已退delete_bot Known掉成Normal。只strict时经原Jobs现存Local/running/未finished+同listener的原ctx，用原wrapped Shell approver/原providerID/mutex，main冻结policy+原owner动态fork卡/child原回调保持。missing/completed/foreign/noapprover在Pause前拒。approval-only原Shell args附canonical `_xbot_bot:{action:"delete",name:TARGET}`，现grant/deniedBefore同键，目标A→B不吃A，Run raw不改，无随机nonce/新确认工具或表；原回调后重验primary/可见性。

读HTTP硬限64KiB；完整JSON含newline与同准确FormatBotList/Info生成的人读都须≤现Shell32KiB，任一超限则当前source Resources工作区现results写完整脱敏JSON（20MiB结果限），回 `{count,file,access:"local",bytes}`，inline数组省略；两种引用也fit，bytes=实际文件。Create/Delete小回执同双32KiB；客户端先输出真实partial再非0，无proxy/redirect/自动retry，不截数组/remote/状态伪成功。四旧模型门/schema/index/Known已退，research仅list/info/help；role预算仍原离线配置；交接派生、单聊与成员群分别沿独立CLI，persona见下文自身职责CLI。决定见 [ADR0052](adr/0052-bot-runtime-cli.md)。

置顶和主身份也使用平面source CLI：`"$XBOT_BIN" pin NAME --self [--json]`、`unpin NAME --self [--json]`、`primary set NAME --self [--json]`。同ParseBotRuntime在root/default/legacy初始化前识别，help离线；没有runtime primary查询。无self原pin/unpin恰一参数、primary零参数ensureSeed打印或set NAME恰二参数保持，不因环境有endpoint变runtime。self选source；每次用Resources当前名fresh比root Primary、目标self或原VisibleBots本地owner交集，排foreign/orphan/remote，不能拿XBOT_BOT或publish资格缓存授权。B原已存在endpoint在转交后下一写即可有权；A原endpoint仍A并下一写失权，不新建K/ref/session或wake。

同原guard提供PUT `/v1/bot/pin?name=NAME&pinned=true|false`（显式单个literal bool，unpin=false）及PUT `/v1/bot/primary?name=NAME`。外部query仅一处核字段/重复/arity，内部typed BotPin，无file/stdin/通用patch。Pin回 `{name,pinned,applied,stage,error?}`，pinned是明确请求值，applied仅原SaveConfig成功，保存失败config/false；self完整SetPinned在currentroute，其它目标Load/SetPinned在route外，保原SaveConfig支持字段合并。primary自己可unpin，无强制永久置顶。

Primary回 `{name,primary?,applied,stage,error?}`。原primary.lock内Load目标→WriteAtomic primary.json→仅目标未pinned再SetPinned(true)；保SetPrimary签名/原cause，PrimaryCommitError从实际写点传Applied/Stage，不stat或error字串猜。validation/target/primary之前未成false（lock入口同primary false）；pointer一旦实际写成，后pin失败仍 `{name:"B",primary:"B",applied:true,stage:"pin",error:"..."}` 并非0，A已失权/B已得权。same-primary也本次写pointer，applied不假称changed；stage=complete不代表新wake。不会自动unpin旧A、不改Owners/Links/persona/binding/rolebudget或回滚pointer，原Load潜在初始化不冒全root零写承诺。

两个原Normal动作不加许可卡/CallID/原审批桥，承载Shell原gate仍独立。短ACK只共用FormatBotPin/Primary核JSON含newline与实际human各≤32KiB、HTTP64KiB，partial先输出后非0，不复制List/Info全文artifact或抬全Shell限额。两native wrapper及空bots组/schema/index/Known已退，原isPrimary/错误及其余Tools保留；research只放这些新动作help，pin/unpin/primaryset与离线primary均在解释器前拒。见 [ADR0053](adr/0053-bot-pin-primary-runtime-cli.md)。

## 自身职责运行时 CLI

`"$XBOT_BIN" persona set --self (--text TEXT|--file FILE|-) [--json]`整写原source自己的完整职责。所有Bot包括非primary均可用，不接其它NAME/primary/patch/卡/role预算；没有runtime全文读取或离线setter，profile runtime JSON亦无职责正文。persona/set help离线，同ParseBotRuntime按输入flag arity识别literal --self/help与--，main在root/default/legacy初始化前分流；缺endpoint/root错不回落磁盘，XBOT_BOT只诊断。

PUT `/v1/bot/persona`单JSON对象 `{text}`，拒未知字段/非对象/尾对象，沿原loopback/Host/browser/root guard；text/file/stdin raw max+1及最终转义body各≤1MiB。原Shell命令16KiB，所以完整32KiB职责有file/stdin实际入口，不能靠长argv或先截32KiB再trim。领域仍仅原WritePersona：TrimSpace后1..32KiB字节、写text+newline为0600、单WriteAtomic最终Rename；合法满职责文件可32769字节。handler冻结解码body，审批后在同Resources当前route一次WritePersona，改名跟currentpath，nil直接原source不打开无关Inbox；原Create晚persona检查、config.Persona fallback/rules/记忆/卡与owner HTTP组合applied_fields不变。

短回 `{name,written,effective?,error?}`，仅原write nil时written=true/effective="next_wake"；错误false/非0，不额外造stage/primary式后段partial。FormatBotPersona同server预算和CLI实际打印，整JSON含newline与准确human各≤原Shell32KiB、HTTP64KiB，显示超限也保实际written/effective事实，不回显全文/新artifact或自动重试。原下一Handle assemble→currentComposedPersona读取完整职责并保卡段，active已组装请求不主动重拼。环境沿0021仅职责label+SHA256前6bytes的12hex hash和原卡名/指纹，不额外复制职责到Event/记忆/旧历史/版本账；原Shell调用正常追加，setter不发Event/Inbox/wake。PickedCard首次EnsureSeeds属原初始化，比卡不变须先warm取真实基线。

production Permissive不加许可卡/不强原CallID；strict显式原SelfEdit恒需确认，不查退掉bot_propose_persona Known/Unknown掉Normal、不借Delete或ownerHTTP临时Permissive。可选XBOT_CALL_ID/X-XBot-Call-ID只关联原live Local/running/未finished同listener ctx，原wrapped Shell approver/name/providerID/mutex、冻结policy/动态owner fork卡/actual child原回调保，missing/completed/foreign/noapprover在写前拒。确认在route/Jobs/host锁外，approval-only原Shell args附 `_xbot_persona:{action:"set",text_sha256:64位完整小写SHA256(trim后的解码text),bytes:有效字节数}`；完整body冻结且等待时不重读file，A→B不吃A批准，grant/deniedBefore同canonical，原Shell Run raw不变，无随机nonce/新确认协议/权限账。短hash只环境用，审批使用完整64hex；代码/remote未冒认原strict上下文。personaTool/空bot_profile组/schema/index/Known退役，历史卡片与raw file SelfEdit保持；research仅persona帮助、所有setter在解释器前拒，dev/general沿原source。见 [ADR0054](adr/0054-self-persona-runtime-cli.md)。

## 成员聊天群运行时 CLI

```sh
"$XBOT_BIN" chat-group list --self [--json]
"$XBOT_BIN" chat-group history GROUP_ID --self [--limit N] [--before-id ID] [--sender NAME] [--keyword TEXT] [--json]
"$XBOT_BIN" chat-group create NAME --self --member BOT [--member BOT...] [--json]
"$XBOT_BIN" chat-group post GROUP_ID --self (--text TEXT|--file FILE|-) [--json]
```

chat-group的help离线；纯ParseChatGroupRuntime按flag arity/literal --help值与--消费，只有member可重复。新namespace不接管owner侧栏group或主人message/外Agent send，不加聊天群rename/edit-members/delete。当前source endpoint/规范Root为身份，XBOT_BOT诊断而已；缺self/endpoint或坏ROOT不回落磁盘/newK。post text/file/stdin raw max+1与转义typed请求各≤1MiB，不先截8KiB再trim。GET `/v1/chat-group/list`、GET `/v1/chat-group/history?id=G&limit=N&before_id=I&sender=S&keyword=K`、POST `/v1/chat-group/create`单对象{name,members}、POST `/v1/chat-group/post`单对象{id,text}均沿原listener loopback/Host/browser/root guard；query单值/数字语法、未知字段/非对象/尾对象在边界拒，HTTP回包64KiB。

List仅当前source为member的群，不等同Webowner拥有all members。History在返回metadata前核member，沿原Bus.Transcript：limit≤0默认20、最多50，before_id须存在且exclusive，exact sender/不trim而忽略大小写keyword，从后挑再正序。完整Group{id,name,members,created}、Line{id,time,sender,kind,text}保原值，JSON时间完整，human时间RFC3339/正文Quote一条一行并标不可信；untrusted:true不让成员正文冒指令。空groups/messages真实[]。Create原Bus自动self+其它VisibleBots本地非空owner交集，成员精确值/去重排序共2..8，无primary；name trim后1..48字节且禁正反斜杠/换行，单atomic meta成功回{group,created:true}，不造多阶段partial。

Post原Normal无新确认卡，却须原XBOT_CALL_ID/X-XBot-Call-ID所指actual live/running/未finished同listener Shell Job prepared ctx；missing/completed/foreign/nonShell/无冻结turn在rate/append前拒。共享runtimeSourceCall只解析原调用，审批wrapper原动态owner/child rebind仍供原三严格consumer；Post显式取原prepared冻结turnChain/turnHops，不能用审批重绑后的turnFor/latestTurn，原ctx无approver函数仍能Normal发送。chain空为ch_CURRENT_SOURCE_NAME，hops原值+1，KindBot/原k.now；caller不传sender/kind/chain/hops。其它read/create不强原callctx。trim正文1..8KiB、hops≤3、sender-group每分钟8次均原Bus校验；不冒ownerKindPerson或另造链绕限。

Post短回{id,message_id?,appended,posted_members:[],failed_member?,stage,error?}。stage为validation/rate/transcript/recipient_load/recipient_open/recipient_post/complete；仅原AppendJSONL返回nil才appended:true/message_id=实际群行ID，仅每次原Inbox.Post成功才加入posted_members。原member顺序、skip sender、首次错即停保持，late错误可以群记录已成+B已收/C失败，CLI先输出真实回执再非0，勿盲重试；不rollback群/rate或造重试/签收账。posted只指Inbox已接受，不代表handled/已醒；appended:false不声称全root无写/原rate没消耗或无JSONL尾半行。旧PostGroup(error)仅薄兼容入口至同一PostGroupWithResult writer，owner Web原KindPerson路径亦保。

ListReply{groups?,count,file?,access?,bytes?,error?}与HistoryReply{group?,messages?,untrusted,count,file?,access?,bytes?,error?}按准确共享formatter预算：整JSON+newline与human任一超原Shell32KiB，就把完整脱敏JSON（所选records不截，20MiB cap）写source当前Resources工作区原results文件，省inline数组，回file/access:"local"/真实bytes/count；小引用两格式也fit。两个写ACK同准确双32KiB，显示错误也保created/appended/posted事实，不回正文artifact。Bus/fanout在source route外，artifact实际IO才短持current route，改名沿原RenameMember/当前路径，不复活旧目录。Bus本身无waker，原serve/proc忙时吸收/Inbox waker保持，不承诺所有生产者即时wake。四原模型wrapper/Known/schema/index退役，peers工具组已空并退役，其它native/Link导线保；research仅同窄lexer/parser的list/history/help，写在解释器前拒，dev/general原Shell可用。见 [ADR0055](adr/0055-chat-groups-runtime-cli.md)与 [内核](atlas/kernel.md#成员聊天群与原消息链)。

## Bot 单聊运行时 CLI

```sh
"$XBOT_BIN" peer-message TARGET --self --text TEXT [--json]
"$XBOT_BIN" peer-message TARGET --self --file FILE [--json]
cat FILE | "$XBOT_BIN" peer-message TARGET --self --file - [--json]
"$XBOT_BIN" peer-message --help
```

TARGET 是本地 Bot 或 `bot@设备`。纯 `ParsePeerMessageRuntime` 按 flag arity 消费值，`--` 后只取位置参数；恰一目标、text/file 二选一，拒重复/未知 flag 与缺 self，字面值 `--help` 不冒充帮助。help 离线；main 在 root/default/legacy 初始化前分流。self 取原 endpoint/规范 XBOT_ROOT，XBOT_BOT 仅诊断；缺 endpoint、root 冲突或运行失败不回落磁盘、不新建 Kernel。完整 8 KiB 正文可用 file/stdin 避开 Shell 16 KiB 命令限制；raw 输入 max+1 读取与最终转义 JSON 请求各限 1 MiB，不先截正文再 trim。本入口独立于主人 `message` 的 HTTP/SSE 与外部 Agent `send`。

`POST /v1/peer/message` 只接单 JSON 对象 `{bot,text}`，拒 query、未知字段、非对象及尾随 JSON，沿原 loopback/Host/browser/root guard。`XBOT_CALL_ID`／`X-XBot-Call-ID` 必须关联同 listener 的实际 Local/running/未 finished Shell prepared context；缺失、过期、foreign、nonShell 或无冻结 turn 在 rate/投递前拒绝。Normal 发送不加确认卡，也不要求 approver 函数；承载 Shell 的原 gate 独立。sender chain/hops 显式取原 prepared `turnKey` 冻结值，空 chain 为 `ch_CURRENT_SOURCE_NAME`，hops+1，保 Bot 身份与原时钟；不用审批重绑后的 turn 或 latestTurn。

本地沿唯一 `Bus.MessageWithResult`：目标排 self，双方须至少有一个非空 owner 交集，正文 trim 后 1..8 KiB、hops≤3、from→to 每分钟 8 次。远端沿唯一 `remote.SendWithResult`：精确 ByName 配对设备，未 unpair、允许 Bot 消息、有 XKey 且 source Owners 含设备 Person；不要求目录缓存先发现目标 Bot。正文 trim/脱敏后重验，原 JSON 明文与完整信封 JSON（含加密后的 base64 层）各受 64 KiB cap，和 8 KiB 正文、原 rate 限制共同适用；控制字节转义膨胀仍可在 envelope 阶段拒绝。原签名、加密、独立 remote-out rate、Receive 与持久重试保持。旧 `Bus.Message(error)`／`remote.Send(bool,error)` 只是同 writer 的薄兼容入口。

短回执为 `PeerMessageReply{bot?,route,posted,event_id?,envelope_id?,gateway?,gateway_status?,queued,stage,recorded?,record_seq?,error?}`。route 为 local/remote；`posted:true/event_id` 仅表示原本地 Inbox.Post 返回 nil，非已处理。远端 envelope_id 来自实际建成的信封；gateway 为 `not_attempted|accepted|http_error|unknown`，accepted 仅 HTTP 2xx 且响应体读成功，gateway_status 仅来自 typed HTTPError 的实际状态码，均非对端签收。仅原 retryable 错误才排同一 ENV/ID，原 atomic enqueue 成功才 queued:true；该事实表示源端持久待重试、24 小时过期。响应丢失或排队失败仍可能已被网关接受，rate 也可能已消耗，不能据非 0 退出推断未发送。

stage 按实际位置报告：本地 validation/rate/recipient_load/recipient_open/recipient_post/complete；远端 validation/identity/envelope/rate/gateway/queue/complete，源记录失败为 record。REMOTE 尝试得到实际发送/排队结果后，原 source transcript 追加一次完整冻结请求与结果的普通内部事实，见 [Bot 私聊](#bot-私聊)。只有 AppendSeq 成功才 `recorded:true/record_seq`；记录错为 `recorded:false`、error/非 0，保留原 gateway/queued/envelope 事实，不回滚或重发。本地无需这条源记录。

回执不回显正文、不另存正文 artifact。共享准确 `FormatPeerMessage` 校验整 JSON 含 newline 与实际 human 输出各≤Shell 32 KiB，HTTP 硬限 64 KiB；显示超限仍保发送/排队/记录事实。失败时 CLI 先输出真实回执再非 0，不自动重发或回滚。research 仅放行同窄 lexer/parser 识别的 help；dev/general 用原 source Shell。`message_bot` 的执行 wrapper、Known、schema、index 已退，peers 工具组已空并退役，其它 Link/Event 导线保持。现状核对：2026-11-02；决定见 [ADR0056](adr/0056-peer-message-runtime-cli.md)。

## Bot 交接派生运行时 CLI

```sh
"$XBOT_BIN" bot-fork NAME --self --role TEXT [--group NAME] [--json]
"$XBOT_BIN" bot-fork NAME --self --role-file FILE [--group NAME] [--json]
cat FILE | "$XBOT_BIN" bot-fork NAME --self --role-file - [--group NAME] [--json]
"$XBOT_BIN" bot-fork --help
```

所有 source Bot（含非 primary）都可为明确职责派生一个持久新 Bot。`role` 是 child 的完整职责/persona 及交接 focus，trim 后 1..8192 字节；与配置的运行档位 `config.Role` 不同，child 有效默认 chat/45 分钟，不继承 source 的 dev 预算。NAME trim 后须合法、非 self、未存在；source 要有 Owners 和 Memory；可选 group trim 后最多 48 字节，禁正反斜杠与换行。

纯 `ParseBotForkRuntime` 按 flag arity 消费值，支持 `--` 后的位置参数；恰一 NAME、role/role-file 二选一，重复/未知 flag、坏 arity 或缺 self 拒绝，参数值中的字面 `--help` 不冒充帮助。help 离线；main 在 resolveRoot/default/legacy 初始化前分流。self 由原 endpoint/规范 XBOT_ROOT 识别，显式 root 必须相同，旧 XBOT_BOT 仅诊断；缺 endpoint、root 错或运行失败明确非 0。原有效离线 `fork SRC NAME --role TEXT [--group NAME]` 保持独立。file/stdin 可传完整 8192 字节职责，避开原 Shell 16 KiB 命令上限；raw max+1 读取及最终转义 typed JSON 请求各限 1 MiB，不先截职责再 trim。

原 `BotHandlers.Fork` 在同 listener 提供 `POST /v1/bot/fork`，仅接单对象 `{name,role,group?}`，拒 query、未知字段、非对象和尾随 JSON，复用 loopback/Host/browser/root guard。`XBOT_CALL_ID`／`X-XBot-Call-ID` 必须关联该 listener 实际 Local/running/未 finished Shell prepared context，且有冻结 turn；missing/finished/foreign/nonShell/无冻结 turn 在领域调用前拒绝。Normal 派生不调用 approval intent、不加确认卡、不要求 approver 函数，承载 Shell 的原 gate 独立。沿原 prepared ctx 传取消、用 `turnSeqOf(prepared)` 记压缩用量，实际 seq=0 仍保 0，不找 latestTurn；复用 source Kernel/Memory/Compactor。

回执为 `cliops.BotForkReply`，JSON 字段如下；`created`、`handoff_parts`、`handoff_total`、`stage` 总是输出，其余空值省略。

| 字段 | 含义 |
|---|---|
| `name` | 实际目标名，显示前脱敏 |
| `created` | 仅原 Create 返回 nil 或 typed `CreateCommitError.Applied`；确认原 config 提交，不代表初始化齐全 |
| `create_stage` | 仅原 typed CreateCommitError 的实际阶段：default_binding/config/state/persona；Create 成功时省略 |
| `confirmed` | 封闭集合 owners/binding/rules/handoff/child_note/source_note/group；仅各原调用返回 nil 才加入 |
| `handoff_parts` / `handoff_total` | 前者仅逐次原 Remember 返回 nil 的确认数；后者为实际 OneLine 后按 960 字节切分的总数 |
| `uncertain_memory` | 当前返回错误的 Remember 尝试 `{bot,key}`；Markdown 可能已经写成 |
| `group_id` | 仅原 CreateGroup 返回 nil 时的实际 ID |
| `stage` / `error` | 实际停止位置及可选脱敏错误 |

stage 为 validation/handoff/create/owners/binding/rules_read/rules/handoff_memory/child_note/source_note/group/complete。确认只描述这次调用的 nil ACK，不保证之后正文仍在或未确认阶段没有字节写入。原 Remember 先 atomic 写 Markdown，再写 index，随后 noteSync；index/sync 错误可能发生在 Markdown 已成之后。交接摘要最多 8 KiB，而原 profile 容量只有 4 KiB，故 child 已创建且部分 960 字节事实已确认后，仍可能真实遇到 ErrProfileFull；此时保留 created、confirmed、parts/total 与当前 uncertain_memory。无回滚、自动重试、stat/错误字符串猜提交或容量提升；created:false 也不承诺原默认配置准备、迁移和用量记录全无写入。

handoff 在 source 的短 current IO 内冻结 State migrate、未覆盖行的 projected 历史、ScopeBot/Profile facts 和非空数据分支读取的 source persona。原 Focused compactor 优先、Compact fallback，模型调用在 route/host/Jobs 锁外；`compact.LLM` 调原 Provider.Chat 且不带 tools，HTTP 是否流式沿供应商现有行为。无 projected turns 且无 Bot facts 才走原空源 fallback；该 fallback 保原样，模型摘要则 trim、拒空、按 Unicode 边界 clip 至 8 KiB，再 OneLine/split960。直接 Recall 只读 Bot scope、person store 共享不复制；projected 历史本身仍可带人物内容，不新增历史过滤或人物绝不进模型的保证。

原写序为 handoff → Create → Owners → Provider/Model/ContextLimit binding → source MigrateRules/读规则 → child rules → child handoff 分块 → child `fork-source` → source `fork-NAME` → 可选 source+child 群 metadata。Owners 保 pre-LLM 时点，Links/三项 binding 在 LLM 后沿当前 source fresh 读取。child 目标固定、认识事实作者取后写时当前 source；source note 在一次短 current IO 内以当前 b.Name 同作 BotName/WrittenBy 直接 Remember；源 rules 用原 current helper，group 前取当前名再锁外调用 Bus。改名不重建旧目录，不加嵌套 current helper、CAS 或跨模型快照服务。真实消费者是 child persona/rules/binding/记忆组装、Web 记忆面板、下一正常 wake 及可选群 metadata；不复制 transcript/summary blocks、person 文件、hooks 或预算，不启动 child、投 Inbox 或唤醒。

短回执不回显职责/摘要全文、不另存正文 artifact。准确共享 `FormatBotFork` 与整 JSON 含 newline 各须≤原 Shell 32 KiB，HTTP 硬限 64 KiB；显示问题保实际创建/确认/不确定记忆/群事实。CLI 先输出 partial 回执再非 0，保留回执，勿盲重跑。research 仅同窄 lexer/parser 识别的 bot-fork help，dev/general 沿原 source Shell。`fork_bot` wrapper/Known/schema/index 与已空的 peers 工具注册组已退，其余 Link/Event 导线、旧 native 调用记录和后台 ForkKindStart/End 修复保持；无专用 fork 历史 parser。旧离线/Web `Fork` 仍是错误时空结果的薄兼容 adapter，不提供本运行时 typed partial。见 [内核](atlas/kernel.md#bot-交接派生与原-source-模型链)、[多 Bot](atlas/multibot.md#bot-交接派生)及 [ADR0057](adr/0057-bot-fork-runtime-cli.md)。

## 经历记录与检索运行时 CLI

```sh
"$XBOT_BIN" experience record --self (--summary TEXT|--summary-file FILE|-) [--focus TEXT|--focus-file FILE|-] [--from N] [--to N] [--json]
"$XBOT_BIN" experience search --self [--query TEXT|--query-file FILE|-] [--limit N] [--json]
"$XBOT_BIN" experience --help
"$XBOT_BIN" experience record --help
"$XBOT_BIN" experience search --help
```

`record` 的 summary/summary-file 恰一，focus/focus-file 至多一，两组可任意搭配；`search` 的 query/query-file 至多一。`-` 表示 stdin，一次最多供一个字段。纯 `ParseExperienceRuntime` 按 flag arity 消费值，值中的字面 `--help`、`--` 不当帮助或分隔符；没有位置参数、别名或 `-h`，拒绝未知/重复 flag、错误整数语法和多字段 stdin。noun 与 action 的 help 均可离线使用；main 在 resolveRoot/default/legacy 初始化前分流。每份 raw 输入以 max+1 读取，连同最终转义 typed JSON 请求各限 1 MiB，不先截断再 trim；原 Shell 命令仍限 16 KiB，大输入用文件或 stdin。

self 由原 source 的 `XBOT_ENDPOINT`、规范 `XBOT_ROOT` 与 Resources ref 识别，显式 `--root` 必须相同；旧 `XBOT_BOT` 仅诊断，改名后仍沿同 endpoint。缺 endpoint、root 冲突或运行失败明确非 0。所有 source Bot（含非 primary）均可用，不附加主人、person 或 project scope 资格。两原语忽略调用 context，不要求 `XBOT_CALL_ID`，客户端也不发送关联 header；缺失或过期 CallID 不改变领域许可，header 不是身份或授权。Normal record 不调用 approval intent、不加确认卡、不要求 approver，承载 Shell 的原 gate 独立。research 只允许同窄 POSIX lexer/parser 识别的 help 和 `experience search --self`，record 在解释器前拒绝；dev/general 沿原 source Shell。

原 `Handlers.Experiences` 挂两个 typed callback，直接调用同 Kernel 的 `RecordExperience` / `SearchExperiences`；不新建 Kernel、MemorySession 服务、source registry 或事务框架。原 loopback listener 提供两个 POST：

| 路由 | JSON DTO |
|---|---|
| `/v1/experience/record` | `cliops.ExperienceRecord{summary,focus?,from?,to?}` |
| `/v1/experience/search` | `cliops.ExperienceSearch{query?,limit?}` |

两路均只收单 JSON 对象，拒 query、未知字段、非对象与 EOF 前尾随内容；复用原 loopback/Host/browser/root guard，请求最多 1 MiB，HTTP 回包硬限 64 KiB。DTO 的 summary/focus/query 为 string，from/to/limit 为 int；文件路径、scope、CallID 不进入 DTO。

summary 沿原领域 `TrimSpace` 后非空且≤2 KiB；focus 只 trim，没有独立领域 cap。query 保原输入，仅以 trim 后是否为空选择最新列表分支，非空原文送 BM25，不复制 Web 的 512 字节查询限制。limit 省略、≤0 或>8 均取原默认 8，CLI 接受任何语法有效的 int 后交原方法处理。

from/to=0 使用操作当时的 transcript：to 取 currentCount，from 在截至 to 的最近 200 行里倒找最新非 Internal、非 ForkMark 的 RoleUser，找不到取 to；空 transcript 可 0/0。不限定新的 Owner origin，也不用 prepared turn 的 seq。显式整数仍沿原范围检查，保留旧负 to 宽容和 SQLite nullSeq 的≤0→NULL 语义。segment 由该范围前 200 行生成，每行正文截 160 字节、合计约 600 字节，跳 Internal/ForkMark/Tool result；保事件围栏内正文及 `user:` / `assistant:` / `[tools: names]` 词法，不调模型。

记录回执为 `cliops.ExperienceRecordReply`：

| JSON 字段 | 含义 |
|---|---|
| `id?` | 原返回 Entry 的实际 ID，0 省略 |
| `from` / `to` | 原返回 Entry 的实际范围，含 0，始终输出 |
| `recorded` | 必需字段，Go 为 `*bool`；仅原方法返回 nil 时为 true，任何 error 为 null（未确认） |
| `error?` | 有界脱敏错误 |

即使原方法出错也保留其返回 Entry 的实际 ID/range；`recorded:null` 不证明数据库或行没有副作用。原 Store.Add 只有一次 INSERT，再取 LastInsertId，后一步出错前可能已插入。回执不回显 summary/focus/segment，不增加 stage、stat/错误字符串猜提交、StoreWithResult、去重、回滚或自动重试。

检索回执为 `cliops.ExperienceSearchReply{text?,file?,access?,bytes?,error?}`，没有 rows/count。text 是原 `SearchExperiences` 的完整语义输出：空 query 按最新 ID 列表，非空在最近 2000 条的 summary/focus/segment 上 BM25，最多 8 条；原本地时间、seq 与 detail 提示保持。原 `presentHistory` 先脱敏再按 6 KiB 裁切，无匹配仍为 `no matching experiences`。它不是原始 Entry 或 Web 全库字面分页；产物也不会补回原 6 KiB 已裁掉的记录。

source 与 CLI 共用准确 `FormatExperienceRecord` / `FormatExperienceSearch`，整 JSON 含 newline 与实际 human 输出分别须≤原 Shell 32 KiB。6 KiB 文本含 HTML/控制字符时，JSON 转义可超过 32 KiB，即使 human 仍能放下。任一格式超限，沿原 `work.WriteResult` 将完整该语义 Text 回包 JSON 含 newline 写入当前 source 工作区的 `results/<32hex>.json`（0600，最多 20 MiB），省略 inline text，回 `file/access:"local"/bytes`，bytes 为实际文件字节数。写前按 32hex 文件名长度预留 metadata，检查引用的 JSONNL 和 human 两格式；失败非 0、不返回虚假 file，不截 records 或提高 Shell cap。

callback 持原 operations lease，不外套 currentIO 调用内部已持 route 的 Record/Search；只有产物实际写入在短 currentIO 里取当前 source name/root 和重基后的原 WorkDir，未配置时取当前 Bot 默认工作区，不拿旧目录迟写。领域错误沿原 `k.redactor()` 脱敏，检索结果继续遮盖 held secrets 与 Resources secret；仅显示 error 可缩短，保留实际 ACK/产物事实。CLI 先输出收到的事实回执再非 0，客户端不展开产物、不代理/重定向或自动重试。

SQLite 七列、原 Web 字段与全库字面分页、提纲阅读投影和原始 segment 展示保持；缺库读取经 exists 不建库，已有库 reader 的原 open 仍可能 CREATE TABLE IF NOT EXISTS/WAL，不能承诺全 root 零写。下一自然环境采样仍报 experience count/latest ID+200 字节摘要，读错保已发布值；不额外 Inbox/waker、MemoryOwnReceipt、假 native ToolCall 配对或普通事实账。`detail` 仍是原 context 工具，按 raw seq 或 experience ID+0 起下标读真正完整 blob（64 KiB），不是新 CLI 子命令。两旧 `experience_record` / `experience_search` wrapper、Known、schema、index 与执行指引已退，历史 native 字面记录保持；memory 组仍有 recall，Context 四工具和原 Link.Tools 空壳保留。存储与消费者见 [记忆](atlas/memory.md#怎么运转)，运行装配见 [内核](atlas/kernel.md#经历记录与检索实例入口)。

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

## 全局保险库

要求登录；写请求沿用同源校验。不依赖选中 Bot 或 Bot owner，整机所有 Bot 共用。JSON 正文上限 1 MiB，拒绝未知字段。接口与连接应用 NeedsSecret（含 MCP）、出站注入、askpass、CLI 共用 `vault.OpenGlobal(root)`，没有另一份存储。

- `GET /api/secrets` → `{"secrets":[{"name":"LOCAL_SUDO","domains":[]}]}`。响应 DTO 只有名称、域名，**永远没有 value 字段或值**。
- `POST /api/secrets`：`{name,value,domains}` 添加/覆盖，值仅在请求体；成功 `{ok:true}`。名称/值为空或保存失败 400，不回显提交内容。省略 `value` 时，仅用 `SetDomains` 在原写锁内修改已有条目的域名，不取出、不替换旧值；不存在的名字返回 400，不能复活已删除条目。显式空值仍拒绝。
- `DELETE /api/secrets/{secret}`：按 URL 编码后的名称删除，成功 `{ok:true}`，重复删除幂等。

空 `domains` 是本机用途（包括 `LOCAL_SUDO`），绝不注入 HTTP；非空名单沿既有域名规范化与 HostAllowed 规则。设置的全部/出站/本机计数与名称/域名搜索均在前端处理。列表和写响应不提供查看、复制旧明文的入口。

## 主 Bot、置顶、删除

`POST /api/bots/{name}/primary` 无 body。把主 Bot 转给这个 Bot，并把它置顶。原来的主 Bot 保持原来的置顶状态，不会被自动取消置顶。成功 200 `{"name","primary":true,"pinned":true}`。没点名的入口之后进这个 Bot 的 inbox。

`POST /api/bots/{name}/pin`，body `{"pinned":true}` 或 `false`。成功 200 `{"name","pinned"}`。`pinned` 缺了是 400。

`DELETE /api/bots/{name}` 无 body。成功 200 `{"name"}`。主 Bot 返回 409，正文含 `the primary cannot be deleted`，要先把主 Bot 转走。目录里只剩一个 Bot 时 409，正文含 `the last bot cannot be deleted`。

## 分叉

`POST /api/bots/{name}/fork`，body `{"name":"reviewer","role":"Review pull requests.","group":"ship"}`。`name` 和 `role` 必填。`group` 可省略；有名字时新建一个两边都在的群。

模型侧交接派生使用 [bot-fork 运行时 CLI](#bot-交接派生运行时-cli)，原 fork_bot 已退役；本 owner HTTP 路由仍沿原登录/owned、busy gate 与错误时空结果的 Fork adapter，不提供新 typed partial 回执。这不是拷贝。源 Bot 写一份交接，成为新 Bot 的 bot 记忆；person 记忆本来就共享，不复制。新 Bot 的 persona 是 `role`，transcript 是空的，provider 绑定继承。两边互相记一条，细节用 `"$XBOT_BIN" peer-message TARGET --self --text TEXT` 问，不能读源 transcript。源 Bot 正忙时 409 `bot is busy`。成功 200 `{"name":"reviewer","group_id":"g_........"}`，没建群时 `group_id` 是空字符串。

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
  "memory":{
    "person":{"path":"/data/people/u1/memory/memory.md","markdown":"# 用户记忆\n…","empty":false},
    "bot":{"path":"/data/bots/ada/memory/memory.md","markdown":"# Bot 记忆\n…","empty":false},
    "experiences":{"items":[{"id":43,"created_at":"2026-10-30T00:17:00Z","summary":"…","focus":"…","segment":"…"}],"next_before":43},
    "month_log":{"path":"/data/bots/ada/memory/log/2026-10.md","empty":false}
  },
  "routines":[{"id":"...","name":"...","cron":"7 * * * *","at":"","enabled":true}],
  "agents":[{"id":"...","type":"dev","status":"running","task":"截到 140 字","mode":"brief","steps":1,"started_at":"RFC3339","current_step":"当前工具的一行摘要","finished_at":null}]
}
```

`persona_card` 是选用的人设卡（没选或卡已删时是 `null`），`persona` 仍是 `persona.md` 全文。没有 `style` 和免打扰：旧配置里的 `style`（language、tone、length、emoji）加载时合成一条 Bot 长期记忆，写入成功后从配置删除，只迁一次；旧配置的 `quiet_start`/`quiet_end` 读进来丢掉；旧模板里的这些字段忽略。详情里没有 `proactivity`。磁盘上的旧配置还可以带这个字段，读进来之后忽略，不进提示。`timezone` 是 IANA 时区名（例如 `Asia/Shanghai`），空字符串表示用本机时区。每轮环境更新里有一行 `now: <本地 RFC3339，精确到分钟> (<时区>)`，不进稳定前缀。`routines[].cron` 和 `routines[].at` 二选一：`at` 是一次性的 RFC3339 时间，触发后 `enabled` 变成 false。`memory` 是记忆 Tab 的只读装配 DTO。`person` 与 `bot` 的 `markdown` 为各自 `memory.md` 原文，不经 Recall 拆条；空白或缺文件时 `empty=true`，仍给绝对路径。每份文档上限 256 KiB，超出或读取失败报错，不静默截断。`month_log` 只给本机当月 Bot 流水的路径和空状态，不装入流水正文。

`memory.experiences.items` 来自当前 Bot 的经历库，按 `created_at` 新到旧、相同时间按 id 递减，首屏最多 40 条；`focus` / `segment` 可省略，`created_at` 为 RFC3339 时间。`next_before` 存在时，`GET /api/bots/{name}/experiences?before=<next_before>` 取下一页，响应同 `experiences`；末页不再给游标。读取空库不创建数据库。

同一接口支持 `q`：`GET /api/bots/{name}/experiences?q=TTS` 在当前 Bot 整个经历库的 `summary` / `focus` / `segment` 做字面子串搜索，英文忽略大小写，`%` / `_` 不作通配符；q trim 后最多 512 字节，超出返回 400。空查询沿普通列表；带 `before` 时保留相同 q 继续取匹配项，排序、每页 40 条和响应结构不变，空结果为 `{"items":[]}`。搜索沿登录与 Bot 所有权检查，不使用模型侧 BM25 的最近 2000 条范围。页面隐藏 person 文档只影响展示，详情 DTO 与原文件接口仍保留。

`GET /api/bots/{name}/memory-files/{kind}` 在浏览器以 `text/plain` 打开原文件；`POST /api/bots/{name}/memory-files/{kind}/open` 仅桌面端可用，通过系统默认应用打开。`kind` 只收 `person`、`bot`、`month-log`，不能指定任意路径；两者沿用登录与 Bot 所有权检查，缺文件返回 404。

`agents[].notable` 是已实际发生的「值得留意」清单（无条目时为空或省略）：`[{"kind":"global_install","text":"全局安装了 ripgrep","objects":["ripgrep"]}]`。`kind` 固定为 `global_install`、`write_outside`、`external_message`、`delete`；按删除、外发、工作区外写入、全局安装排序，同一类每个任务合成一项。`text` 是不带句号的短句，超过两个对象只列前两个并写总数；`objects` 保留完整对象列表，放在任务详情。`calls` 是成功调用数，外发短句用它写消息数量；`unit` 是对象计数单位。`incomplete:true` 表示部分调用没有可靠对象字段，或对象明细达到单任务 64 KiB 上限，此时不声称完整数量。交回 Bot 的报告用同一份清单生成一句摘要：统一以「值得留意：」开头、句号结尾；一项直接写该项，多项写首项及其余项数，并提示详见任务。卡片行本身不加句号。普通工作区内读写不会产生条目。

`agents` 是子代理状态，不是一套独立的任务系统；`type` 为派发时的类型（如 `research`、`dev`），普通 spawn 为空，界面在没有任务标题时用它显示类型名；`started_at` 是实际开始时间（尚未开始时为 `null`），`current_step` 是当前一步的一行摘要，`finished_at` 优先使用实际结束时刻。

`PATCH /api/bots/{name}/routines/{id}`：须登录且属于该 Bot 的用户。请求体可含布尔字段 `enabled`、字符串字段 `precheck` 和有序字符串数组 `skills`，至少提供一个；返回更新后的例程。`precheck` 是 shell 命令字符串（不是布尔开关），最多 16 KiB；省略保留原值，空串移除检查。`skills` 为已有技能名/id（最多 32 项，每项去首尾空白后 1–240 字节、单行），省略保留、`[]` 清空；详情与 PATCH 回执非空时返回同一字段。叫醒时按序只读加载；缺失、改名、停用或读取失败会跳过并在本次上下文说明原因。请求体上限 128 KiB（容纳 JSON 转义），详情接口的 `routines[]` 在非空时也返回 `precheck`。与当前实例routine CLI共用 `Timer.Update`；不改变提示、时区或计划。未登录 401，Bot/例程不存在 404，字段缺失、类型不对、命令超长或未知字段 400，写入失败 500。有效前置命令（包括省略字段时保留的命令）沿既有owner专用CheckRoutinePrecheck，production always-allow，不提供source原turn确认链；原source CLI另以实际ShellJob上下文审查。触发时仍单独重读live gate，ask/off跳过并消耗slot。显式清空仍可使用本接口。执行规则见 [多 Bot](atlas/multibot.md)。

`PATCH /api/bots/{name}` 只改出现的字段。可写 `name`、`description`（最多 160）、`timezone`、`avatar_seed`，并保原独立 `persona`、`group` 业务；简介/时区/group 空字符串清除，persona 必须为非空职责正文。`language`、`quiet_start`、`quiet_end` 已删除，带上是 400；旧 `proactivity` 接受后忽略。名字与普通字段先全量规范化、预检，借同一 typed profile 提交；persona/group 的输入也先校验，但仍分别写入，不承诺跨文件事务。seed 切 generated，元数据保存后 best-effort 清上传图/MIME；仅改名保原 seed，必要时把旧名字的 fallback seed 固定下来。主 Bot 改名后 primary 跟随、置顶不变，peer 成员在实际移动后更新。成功 200 返回当前资料与 `previous_name/applied_fields/moved`；领域提交后出错仍返回完整资料回执与 `error`，客户端以实际 name/moved 处理部分结果。前置非法值按原错误状态拒绝。

`POST /api/bots/{name}/avatar` 保 `multipart/form-data` 外壳，字段名 `file`；整个请求最多 2 MiB，图片字节最多 1 MiB，按内容嗅探 PNG/JPEG/GIF/WebP，再转同一 typed 头像领域写口。成功资料回执中 `avatar:"custom"`，seed 保留上传前的值；失败若已有文件/MIME 写入，保留实际阶段与 `error`，不把三次写入宣称全事务。之后列表和详情里的 `avatar_url` 指向 `GET /api/bots/{name}/avatar`，GET 返回原上传图，按内容给 Content-Type，带 `X-Content-Type-Options: nosniff`、`Cache-Control: private, no-cache`；生成头像仍由浏览器画，不经 GET 传图。

`DELETE /api/bots/{name}/avatar` 转同一独立 clear 动作，回执的 `avatar:"generated"`，沿原 seed；元数据保存成功后清理旧图 best-effort。上述 Web 写入仍要求 owner 会话及同源约束，资料部分回执字段与 [本机 CLI profile 操作](#本机-cli-profile-操作) 一致。

`POST /api/bots/{name}/open-workdir` 只在桌面版存在：让系统文件管理器打开这个 Bot 的工作目录。成功 200 `{"status":"ok"}`。浏览器里（`/api/session` 没有 `app:true`）是 404；桌面版里调用文件管理器失败是 500，正文是原因。要登录；不是这个 owner 的 Bot 是 404。

`GET /api/bots/{name}/generated` 返回 `{"path":"<服务端 Bot 工作区的 generated 绝对路径>","can_open":<bool>}`。浏览器与桌面均可用；要登录且为该 Bot 的 owner。目录不存在则幂等创建，只处理固定的 `generated/`，不接收任意路径、不读取图片。`generated/` 为符号链接时拒绝。`can_open` 仅在桌面且配置了系统打开回调时为真；浏览器显示路径并提供复制。

`POST /api/bots/{name}/open-workdir?folder=generated` 复用桌面打开接口，指向该 Bot 的 `generated/`；其余非空 folder 返回 400，符号链接拒绝。无 folder 仍打开整个工作目录。

`GET /api/bots/{name}/template` 下载 JSON，`Content-Disposition: attachment`。模板含档案、persona、例程、`avatar_seed` 和可选的 base64 头像。自定义头像的模板同时带图和种子。不含记忆、transcript、webhook、watch、provider、owner、置顶。

`POST /api/bots/import` 的 body 就是这份模板，可以另带一个不同的 `name`。模板里的合法 `avatar_seed` 跟着模板走，即使改了名字；种子不合法就忽略，用新名字当种子。之后有 base64 头像就照样导入。成功 200 `{"name"}`。这是目录里第一个 Bot 时成为主 Bot。导入失败会删掉新建的目录。

## 对话

### 主列 chat 时间线

`GET /api/bots/{name}/chat?before=&after=&around=&limit=` 需要登录及 Bot owner（401/404）。无参读尾窗，默认40条chat行、limit为1..200；三个游标互斥，before/after排他，around包含命中。非法参数400，不存在的around目标404。返回 `{"messages":[{"id":1,"transcript_seq":7,"delivery_key":"say:call_id","kind":"bot","time":"RFC3339","text":"送达快照","images":[],"audios":[],"reply_to_seq":1,"reply_to_id":1,"meta":{}}],"window":{"head_id":1,"tail_id":40,"total":40,"next_before":1,"next_after":40,"has_more_before":false,"has_more_after":false}}`。kind为owner/bot/card/source/aside；aside 是「心里话」来源行，text 为短正文全文、delivery_key 为 aside:<transcript_seq>，不是正式 bot 回复；附件、引用与来源字段可省略。reply_to_id是chat目标，reply_to_seq仍为账本引用；行上的from_device/relay沿原展示规则。

每个delivery_key只插入一次，正文/附件不能编辑。成功say等最终助手行与媒体归组定稿后整轮提交；多say多行，图片/语音挂最后一条；SSE实时显示不变。旧Bot/世代落后自动分块投影，不改旧jsonl。主列打开、上翻、搜索、轮询与收工对账不再读raw transcript或扩大raw窗。

响应含包络仍≤约700 KiB；大单行带truncated预览并保持chat游标前进。弱ETag由chat尾ID、设备标签和窗口参数组成，无可见变化返回304；工具噪声不使chat失效。默认尾窗/读到尾部的after更新既有transcript已读游标；before/around不标记未见尾部。

`GET /api/bots/{name}/chat/search?q=&limit=20` 只搜chat FTS正文，返回 `{"matches":[{"id":1,"transcript_seq":7,"preview":"短预览","time":"RFC3339","kind":"bot"}]}`，按BM25排名。limit为1..100、q去空白后最多1024字节；空词/无命中为空数组，非法参数400。短中文片段可命中；工具参数、内部正文不进此索引。主列用命中id开around窗；轨迹与Agent继续用下面的transcript接口。

### transcript 账本

`GET /api/bots/{name}/transcript` 返回原文。段存储可用时带弱 `ETag`（现有 transcript 尾段 stamp，并计入远端设备标签文件的 size/mtime）和 `Cache-Control: private, no-cache`；`If-None-Match` 命中返回 304、无正文，不读取历史、不重写已读游标。首次迁移或 stamp 不可用时仍返回完整 200；有变化的 200 仍更新已读游标。返回形状：

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

默认尾窗和读至尾部的 `after` 请求把已读游标记到当前长度；`before`/`around` 浏览历史不标记未见的新消息。`role` 为 `tool` 的行用 `tool_call_id` 对上助手消息里的 `tool_calls`。以 `<environment_` 或 `<memory>` 开头的 user 行是内核上下文，不是用户说的话。Bot 之间的动作如果出现，也是普通 transcript 行，接口没有单独的“系统行”类型。用户聊天里，来自其他 Bot 的一行仍是淡提示。两边来回的原文见下面的「Bot 私聊」。

跨 Bot 阅读不是 HTTP。模型工具 `recall_history` 和 `expand` 有可选的 `bot`。只能读和调用者至少有一个相同 owner 的 Bot。别的 owner、只是同群或能互发消息的 Bot、以及群 id，都是 `not found`（`history: bot: not found`），不说明原因。搜索和展开用对方 Bot 自己的索引。读到的文本包在 `<untrusted_event source="bot:<name>" ... sender="<name>">` 里，没有指令效力。长度上限和读自己的一样（搜索约 4 KiB，展开约 8 KiB）。

`GET /api/bots/{name}/export?format=md|jsonl&tools=0|1` 下载这个 Bot 的 transcript。未登录 401。不是这个 owner 的 Bot 是 404。`format` 缺省是 `md`。`tools` 缺省是 `0`；只有 `1` 才包含工具调用和 `role=tool` 的行。可选 `from` 和 `to`，要成对出现，是包含端点的 seq。

正文按 transcript 段流式写出，不把整份历史装进内存。工具结果如果另存成 blob，写出那一行时读回全文。成功时正文不是 JSON。`md` 的 `Content-Type` 是 `text/markdown`，时间用服务器本机时区，摘要块写成 `## Summary seq <from>-<to>`，后台分支的行标题多一段 `[后台 <branch>]`，`fork` 和 `fork_end` 各写成一行。`jsonl` 是 `application/x-ndjson`。两种导出格式均为公开投影：移除 reasoning 内容块、清除所有保留块的 `vendor_raw`，`tools=1` 也不例外；普通正文、可公开 hosted/citation 与按开关保留的工具块不变。内部 transcript 原始块与同协议回放不受影响。普通行是公开消息记录再加 `seq`，`json.Unmarshal` 进记录时忽略 `seq`，所以能再导入，序号还是原来的。带引用的行多一个 `reply_to`；后台行带 `branch`，标记行带 `mark`（`{"kind","branch","from","task","status","steps"}`）。`{"kind":"summary","from","to","text","focus","at"}` 是摘要块。每一段写出前过 `RedactText`；保险箱还没安装替换函数时，文本原样出去。`Content-Disposition` 是附件，文件名 `<bot>.md` 或 `<bot>.jsonl`。`md` 在该行标题下写 `reply_to: <seq>`。

`GET /api/bots/{name}/transcript` **无参数默认尾窗以25条可见交付为目标**，不再整本返回；这是有意的接口默认改变，需要全本时使用上面的流式 export。不指定 limit 的尾窗与 before 页先取60条原始记录，可见交付不足时向前扩，最多400条原始记录且受256 KiB原始字节预算约束；触顶可能不足25条，has_more_before 如实反映剩余历史。显式 `limit=1..200` 仍限制原始条数；after/around 未指定时仍默认60条。可选 `before=<seq>` / `after=<seq>`（排他）或 `around=<seq>`（包含命中附近），三个游标互斥。非法参数 400，around 目标不存在 404。记录按 seq 升序且连续；条数可能因字节预算更少。空窗 messages 为 `[]`，head/tail/cursor 为 0。追加不改变已有 seq，稳定游标可跨请求使用。

响应另带 `window:{head_seq,tail_seq,total,next_before,next_after,has_more_before,has_more_after}`。head/tail 为本窗含端点范围，total 是快照末 seq；next_before/next_after 分别用于下一页 before/after，有更多时由对应 has_more 标志指明。响应包括 JSON 包络控制在 pipe 的 700 KiB 内（本机与远端统一窗口预算，完整内容用导出）。单超大行公开预览裁短并带 `truncated:true`，仍推进游标；普通附件字段不变。assistant 的 `delivered:[call_id]` 是已成功送达的通知调用，可在成功回包落在窗外时复用原有气泡规则。fork 的有效分支仍包含窗外标记的回溯作用。条件请求 ETag 包括尾部指纹、设备标签和规范窗口参数；不同窗口不能共用 304。Bot 列表未读仍用序号计数。

`GET /api/bots/{name}/transcript/search?q=<关键词>&limit=20` 与 transcript GET 同样要求登录及 Bot owner。直接复用该 Bot 的 `Bot.Search` / FTS5，返回 `{"matches":[{"seq":111,"preview":"短预览","time":"RFC3339"}]}`，沿用 BM25 排名；limit 默认 20、1..100，q 去空白后最多 1024 字节，非法参数 400。空词或无命中返回空数组。预览取存储行（工具 blob 只用已有预览，不解包全文），命中 seq 可用 `transcript?around=<seq>` 定位。工具/内部记录也可被索引命中；本接口给Agent与账本用途，主列搜索不调用它。transcript本身不另建全文索引。

`POST /api/bots/{name}/paste` 收一张要贴进对话的图片。body 是图片原始字节，`Content-Type` 只当提示，类型按字节嗅探。成功 200 `{"image":"<32 位十六进制>.png"}`。只认 PNG、JPEG、GIF、WebP（其他 400 `只认 PNG、JPEG、GIF、WebP`），上传或缩后超过 10 MiB 回 413。PNG、JPEG、GIF 长边超过 2048 时复用 `view_image` 等比缩到长边 2048 后保存（GIF 缩后为 PNG，文件名扩展名按缩后类型）；合规图原字节保存。超边 WebP 无法缩小，回 400 并提示换 PNG/JPEG；读不出尺寸、缩放失败或超过既有解码上限（边长 4096／估算 160 MiB）也回 400 并说明原因。文件存在这个 Bot 的工作区 `paste/` 下，权限 0600。`GET /api/bots/{name}/paste/{image}` 读回这张图，`Content-Type` 按字节定，带 `nosniff`，可长期缓存（文件名不会改）；名字不是一个合法 id 或文件已不在时 404。这两条都要登录，不是这个 owner 的 Bot 是 404。

本机命令行可用 `xbot message <bot> <text...>` 调用此接口，默认连接 `127.0.0.1:8788`，输出最终可见回复。

### 语音附件

`POST /api/bots/{name}/voice` 接收原始音频字节，按内容识别 WebM、MP4/M4A、Ogg、WAV、MP3、FLAC，单文件最多 10 MiB；返回 `{"audio":"<32 位十六进制>.<扩展>"}`。文件存 Bot 工作区 `voice/`，权限 0600；首次录音会建立工作区。空文件／未知类型 400，超限 413，保存失败 500。`GET /api/bots/{name}/voice/{audio}` 返回可播文件，支持 Range（206）、`nosniff` 与 private immutable 缓存。两条均要求登录及对应 Bot owner；非法 id、缺失或越界文件 404。

`POST .../messages` 额外接受 `audios:["<voice id>"]`，最多 4 段、合计 10 MiB，允许只有 audios。名字必须对应本 Bot 已落盘音频；坏附件在启动模型前以 400 拒绝。它与 text/images 同泡，音频排在文字和图片之前。transcript 的 `audios`、jsonl 导出的同名字段和 Markdown 的 `audio: voice/<id>` 只记录文件名。用户语音不会自动 STT，Agent 上下文只收到文件路径，自己调用 listen；TTS 保存与发消息挂泡分开。

`POST /api/bots/{name}/messages`，body `{"text":"...","reply_to":3,"images":["<paste id>"]}`。`text` 0 到 16 KiB，`images` 最多 4 个、每个必须是 `POST .../paste` 返回过的名字，否则 400 `图片已失效，重新贴一次`；只有图片、没有文字也可以发。这些图片挂在写入的那行 transcript 上（记录里叫 `images`），回答这条消息的那一轮请求带上图片字节，之后各轮只留一行路径说明，模型要用 `view_image` 自己打开。transcript 和导出里都不带图片字节；导出会用 `image: paste/<名字>`（md）或 `images` 字段（jsonl）写出文件名，纯图片的消息不会变成空行。群聊的发送接口不接受图片。`reply_to` 可省略；给出时必须是这个 Bot 的 transcript 里已有的序号，否则 400 `{"error":"bad reply_to"}`。群聊的发送接口不接受这个字段。拿到处理锁时响应是 `text/event-stream`：

- `event: delta`，`data: {"text":"..."}`
- `event: tool`，`data: {"name","summary"}`。`ask_user` 必填 question、options（2–6 项）、recommended（必须等于某个选项的完整字符串）；summary 是问题，后面每行一个 `1. 选项`。终端会把这段打出来。界面也可以按这个画选择，不必再解析工具参数。
- `event: approval`，`data: {"id","tool","summary","deadline"}`。`deadline` 是 RFC3339，这张卡自动继续的时刻，默认是发出后 30 分钟，可由 `Server.ApprovalWait` 配置。到点没人确认就自动允许原调用并继续，追加 `auto_approved` 和灰字 notice。界面立刻展示确认卡和允许、拒绝按钮；倒计时只读 `deadline`，没有字段就不显示倒计时。
- `event: result`，`data: {"name","output"}`，output 截到 4000 字
- `event: profile`，`data: {"name":"new","previous_name":"old"}`；普通更新可省略 previous_name。普通资料、头像和改名的实际变更提示；部分提交出错也可带实际新名。当前 selection 匹配 previous_name 时就地更新名字与草稿 key，保原 SSE 和输入；不从工具结果猜资料变更，不串到已切换的其它 Bot。
- `event: done`，`data: {"text":"这一轮拼起来的正文","reply_to":2}`。`reply_to` 省略表示这条回复没有指向某一条用户消息。整段回复去掉空白后正好是 `SILENT` 时不发 `delta`，`text` 是空字符串。工具调用之前流出来的字只是状态，可以出现在 `delta` 里，但不会留在 `done.text`。界面用 `done.text` 作为这轮的最终回复，不要把工具调用前的 `delta` 拼进那条回复。模型用来标记引用的 `reply_to:` 行不会出现在 `delta` 或 `done.text` 里。
- `event: error`，`data: {"error":"..."}`

`POST /api/bots/{name}/approvals/{id}`，body `{"allow":true}` 或 `{"allow":false}`。未登录 401。不是这个 owner 的 Bot，或没有这张待确认的卡，是 404 `no such approval`。已经答过是 409。超过等待时间是 410 `approval expired`。成功 200 `{"status":"ok"}`。默认等 30 分钟（`Server.ApprovalWait` 同时配置权限卡和提问卡）；显示卡片的那次请求断开后，这张卡仍可以用这个接口决定。到时没有回答，按允许执行原调用并继续；transcript 卡追加 `state="auto_approved"` 和 `notice="30 分钟没人确认，已按允许执行"`。挂起的调用沿用一次性放行与决定唤醒恢复。超时后再点权限卡仍返回 410。进程重启时，上一个进程留下的待确认卡追加 `no_decision:restart`。**默认不发权限卡**（[ADR 0019](adr/0019-no-permission-cards-by-default.md)，`xbot permissions on` 打开）：只有 `links` 的 install/remove 和触到保险箱或密钥还发卡；机器开关按 `allow` 算，外部事件开的一轮碰记忆也不发卡。资料改名、普通字段与头像更新经当前实例 profile 领域入口，不再走旧模型工具审批或工具结果刷新分支；实际变更沿 `profile` SSE 提示。打开之后，哪些工具发卡只看内核里那一张权限表，工具自己从不决定：只有对外发送（连接器、`links` 的 install/remove、未知/MCP 工具）、删除、注入 `{{secret:}}` 或非 `GET`/`HEAD` 的 `http_request`、职责自改与 persona/profile 文件自改（persona self CLI显式SelfEdit，以及 `write_file`/`edit_file` 写自己的 persona/profile 文件）发卡。Bot之间的 peer-message/chat-group post 保持 Normal、不额外发卡，承载它们的原 Shell gate 独立；写技能、编码任务不额外发卡。routine纯排期不另加确认，承载它的原Shell仍按机器gate/分类，非空precheck另沿原callctx审核。本机工具（`shell`、`read_file`、`write_file`、`edit_file`、桌面、浏览器）再按机器开关：`allow` 按表，`ask` 每次发卡，`off` 直接拒绝；`browser_stop` 在 `off` 下仍可确认。这条链路上根本没有确认人时，工具结果是 `not run: no approver available on this link`，不会写成用户拒绝。用户明确拒绝才是 `denied by user`。

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

本机可继续子代理归属当前 Bot，在独立「子代理」标签查看；「任务」标签用于 Routine。以下接口沿当前 owner 登录鉴权，只能操作自己的 Bot 和子记录。旧的一次性记录仍可读，`continuable:false` 时不能靠新消息接口猜参数重开。

- `GET /api/bots/{name}/agents/{id}/transcript?before=<seq>&limit=40`：读取孩子的公开记录，不创建模型轮次、不等待主 Bot 对话锁。默认40条、最多200条，`before` 是排他物理行序号，返回正序；响应为 `{id,entries,has_more,next_before,turn_seq,status,continuable}`。`entries` 含 `{seq,time,message:{role,content}}`，可带 `kind,sender,input_seq,delivery,turn_seq,status,result_path,internal,truncated`；工具调用与结果保留块和调用关联，思考块与厂商原始包不公开，文本脱敏。每行最多64 KiB公开预览，超大原行使用带 `truncated:true` 的占位；响应整体不超过700 KiB，收窗仍保留可推进的游标，预览不冒充原始全文。原始子记录不因此截断或改写。
- `POST /api/bots/{name}/agents/{id}/messages`：body `{text,delivery:"steer"|"queue"}`，delivery 默认 steer，text trim 后非空且不超过16 KiB。请求JSON最多64 KiB，拒绝未知字段、额外JSON值及任意自报sender。运行中 steer 在下一步接入，queue 在本轮自然结束后另开一轮；空闲、完成、失败或停止后均按原 id 和记录显式继续。成功200返回 `{id,status,position?,blocked?,turn_seq?}`，表示已接受；不代表已经执行完成。网页直发来源固定为 `owner`，主 Bot 工具发信来源由内核固定为 `bot:<name>`，原文只记一次。
- `POST /api/bots/{name}/agents/{id}/stop`：body `{}`，停止当前执行并丢弃此前未消费消息，保留对话以供以后显式继续。成功200返回同形回执，真实执行未退出时为 `stopping`，未入场取消为 `cancelled`，终态重复停止幂等返回其当前状态；不把尚未停止的工具报告成已撤销。已经转为独立工具 Job 的调用仍沿其原取消规则。

新消息和停止都不等待主 Bot 的生成锁；活动任务可能由例程或其他本机 Kernel 持有，按同 id 的真实活动 owner 路由。3运行+8等待及同资源排队保留原每Local边界，不代表跨Kernel新建不同 id 的全 Bot 总池。不存在/非本 owner 的记录404，旧不可继续或原进程仍存活但本进程无法控制为409，无效输入400，等待/消息队列已满429。`status:done` 只表示当前轮结束，不表示孩子永久不可联系。

Bot 详情的 `agents[]` 补 `continuable`、`turn_seq`、`result_path`。每轮结果保存到该孩子 `results/<turn_seq>.txt`，旧引用不会因下一轮覆盖；恢复中断与正常完成共用稳定的逐轮终态身份。服务重启不自动重放孩子旧工具或未消费输入，新消息到来才继续。

旧 `POST .../agents/{id}/steer` / `queue` / `cancel` 为轻适配：可继续本机孩子转对应新领域入口，一次性/runner 活任务沿原控制；`interrupt` 与旧 `bg_` 控制保留原语义。不把旧记录标为可冷恢复。

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

`DELETE /api/bots/{name}/links/{id}` 卸下这条授权。已知 id 成功 200 `{"id","installed":false}`。未知 id 是 404。这条 Link 声明了密钥时，同时删掉这些名字：连接器（Telegram、飞书）的 token 存在保险箱 backend 里，用 `backend.Remove` 删除；其它 Link 的键也从全局 backend 删除，不回退 Bot 明文文件。

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

`url` 是要交给外部服务的地址：有公网地址时等于 `public_url`，否则等于 `local_url`（serve 的监听地址）。公网地址依次取 `XBOT_PUBLIC_URL`、隧道地址、账号网关入口 `https://<网关>/pipe/v1/hook/<句柄>`（中枢确认了这台设备的待命才有，握手失败、断开或重连中就没有，见 [pipe.md](pipe.md)）。`GET /api/bots/{name}` 带 `webhook_public`（当前公网 base），任务页随轮询比对，变了就重读列表和打开的详情。Bot 名在地址里做了百分号转义。

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

`POST /api/apps` 接收 `{"id":"context7","secrets":["密钥值"]}`；无密钥条目省略 `secrets`。密钥按 `GET /api/apps` 每行 `fields`（人话标签，如「API 密钥」）的顺序提交，只在探测通过后保存，不出现在列表。真实探测失败返回 422，未添加；需要密钥却未填齐返回 400。Telegram、飞书及 Context7 的密钥全部保存于整机全局保险库；连接器授权仍按既有规则保存。探测方式见 [ADR 0031](adr/0031-store-keys-in-settings.md)；飞书只发一个不存在的消息类型，不会投递到群。同一时间只处理一个添加或移除，单次上限 45 秒；请求取消时在途探测终止。MCP 长期会话后的自定义配置回执见下节。

## 连接应用的运行状态

`GET /api/apps` 沿用 owner 登录校验和全局安装列表。MCP 商店条目增加 `load_id`（如 `mcp:playwright`）；已配置的 MCP 增加 `runtime`，键为当前用户的 Bot 名，值为 `{state, tool_count, error?}`。`configured` 表示当前配置尚未成功取得工具；`ready` 表示该 Bot 的匹配实例取得非空工具表；`error` 附带有界、脱敏的失败原因。没有 Bot 时不返回 runtime。读取不启动 MCP 进程、不发网络请求，不将其他配置实例或其他用户的状态混入。

旧 `state=connected` 和顶层 `connected` 仍表达安装/凭据状态，不代表工具可用；前端本次未接入 runtime。`load_id` 保留目录身份；MCP 的 links load 只返回当前实例 CLI 指引，实际发现/调用用下节 MCP CLI，不再返回模型工具 schema。ready 不保证某个工具的浏览器等外部依赖可用。

`POST /api/apps/custom` 保存命令/地址配置后返回 `{id,state:"configured",saved:true}`，保存成功不冒称运行 ready。现 Bot MCP POST/DELETE、全局应用添加/移除与 `/api/secrets` 成功写入都会触发同进程 reconcile；同 spec POST 仍 409，不新增 PUT。外部文件变化按现 serve Pass 读取，坏配置保当前有效会话但报告读错，仍引用的凭据失效单独关闭对应旧输入。

## MCP 运行时 CLI

Agent：`"$XBOT_BIN" mcp list|tools|describe|call … --self --json`，配置为 `add SERVER (--command PATH [--arg VALUE]… | --url URL) [--secret-env NAME]…` / `remove SERVER`。配置scope默认global、显式`--scope bot`整条覆盖；调用使用effective服务器与远端原工具名。human必须显式`--http ADDR --bot NAME`，同cookiejar `/api/session` 的authed会话；只有显式global `--root`才发X-XBot-Root，服务端Root是权威。离线文件命令改为明确 `mcp offline list/add/remove BOT…`。

source在原随机loopback listener提供 `/v1/mcp/servers` GET/POST/DELETE、`tools?server=` / `describe?server=&tool=` GET、`call` POST；全部复用原Host/browser/root guard。human对应 `/api/bots/{name}/mcp/servers|tools|describe|call`，先owner登录/owned再核显式root并借Resources，不另建K/client。servers POST `{scope,spec}`、DELETE query `scope,server`；call `{server,tool,args,read_only}`。args为单JSON对象、默认`{}`，CLI `--args`与有界`--args-file FILE|-`互斥，保大整数；请求<=1MiB。MCP调用ctx/default timeout0，显式timeout正duration；不复用profile10s/message2m，client Proxy:nil/拒redirect。

成功回 `MCPReply`（最终JSON<=64KiB）：metadata在`data`，list `{scope,servers:[spec+source+shadowed+runtime]}`、tools `{server,tools:[完整ToolSpec]}`、describe完整Spec；过大改完整json `artifact`。call用`content:[{type,text?,preview?,mime_type?,uri?,file?,access?,projection_error?}]`，完整脱敏`raw`和`structured_content`为本机artifact；artifact形状`{type,mime_type,file,access:"local"}`。大manifest同样保存引用，stdout不打印binary/base64。图片真实解码和sniff，resource_link只留URI不fetch，unknown/embedded/blob不丢。文件为当前工作区相对路径，URI不等于已下载文件。

配置保存回`saved:true`，`runtime_error`独立且不冒ready；ready只取匹配effective spec的缓存状态。`is_error:true`仍输出保留内容但CLI非0，`projection_errors`也非0，transport失败另报，不自动重试未知写。research `call --readonly`在同一次session检查精确远端tool `readOnlyHint=true`并沿同session调用，换代失败不转发新slot。主与子provider均不再声明MCP schema，非MCP Tools门继续工作。

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

## 例程运行时 CLI

Agent 在原source Shell使用 `"$XBOT_BIN" routine list --self --json`、`routine create --self --args JSON|--args-file FILE|- --json`、`routine update ID --self --args JSON|--args-file FILE|- --json`、`routine delete ID --self --json`。args与args-file互斥；文件/stdin max+1、最终转义请求≤1MiB，单对象+EOF拒未知字段。help离线可用，runtime在本地root初始化前分流，缺endpoint/root或显式root不匹配失败不回落；无--self旧migrate/list --bot/update --bot --skills保原离线。

沿既有loopback/root/Host/browser guard，GET `/v1/routine/list`、POST `/v1/routine/create`、POST `/v1/routine/update?id=ID`、DELETE `/v1/routine/delete?id=ID`。四typed操作只借source actual mounted cached Timer；先bind当前目录再publish，不new Timer/Kernel/调度器。Timer方法自己持route，HTTP不外套route或在Update闭包嵌Load/Save；缺Timer只本族失败。

create对象 `{id?,name,prompt,cron?,at?,precheck?,skills?}`：空白id审批后随机r+8hex，Enabled恒true，同id覆盖，name保原字串且不加必填约束，prompt trim后必需。cron五段/最小5分钟、local；at RFC3339/offset规范UTC，严格早于source now−2min拒，精确边界可用，cron/at互斥。precheck≤16KiB；skills≤32、有序保重复，trim后1–240字节单行，写时不查存在/停用。update仅 `{name?,prompt?,cron?,enabled?,precheck?,skills?}`：nonblank字符串替换、false有效、precheck省/null保持空串清、skills省/null保持[]清，拒at/zone/last_slot。改cron设local但不暗清At；原fresh合并不变量仍校验，不加CAS/重试。

写回 `{id,applied,stage,error?}`，complete只指CRUD完成，不指触发。定义保存/移除（或已缺失）后zone-index失败仍applied:true/stage:zone_index且CLI非0，先保回执，不伪回滚。list小结果 `{routines:[],count}` 带实际定义全字段；完整脱敏JSON含换行和同一纯formatter生成的人读文本均须fit现Shell32KiB，HTTP硬限仍64KiB。任一种超限即当前Resources工作区results保存完整JSON（现20MiB结果限），回 `{count,file,access:"local",bytes}`；引用两格式也须fit，bytes等于实际文件，不截字段/列表伪成功，旧Timer.read未新增文件限。

非空effective precheck（update省仍审原命令）需原source call：ctx-aware Shell最终env给XBOT_CALL_ID，routine写可传X-XBot-Call-ID。HTTP只从该source现Jobs内存表取Local/running/未finished、同listener的prepared ctx及原wrapped approver，放所有锁再审，completion清ctx，无历史/最新turn替代。header仅关联原call，不新认证边界，不强制其它族/无命令CRUD。main审冻结身份、卡片回原owner动态fork；actual child明确untrusted/no-person并保自己的subApprover。code/remote/外部命令缺原ctx时非空precheck明确失败、定义不写。

原process cwd/machine/protected/untrusted-memory及Shell表决定need才问原回调，不借owner API的临时Permissive helper。only nested ToolArgs向原Shell字段附服务端canonical请求与effective命令，真正Run raw不改；现grant/denial key绑定实际内容，args-file或保留命令A→B不能吃A批准。请求routineID稳定、自动ID审后生，不含HTTP/Job/provider随机标识；原Shell名/providerID/park/fork/决定路径保留，无新确认协议。触发live gate另审，Due/Deliver/emit/Inbox/Commit/水印不迁移，CRUD不post/wake。四旧模型门已退；research共用窄lexer/纯parser，仅help/list--self。见 [ADR0051](adr/0051-routine-runtime-cli.md)。

## 技能运行时 CLI

Agent 使用 `"$XBOT_BIN" skill read ID --self --json` 与 `skill write SLUG --title TEXT --description TEXT (--body TEXT|--body-file FILE|-) [--scope self|BOTNAME] --self --json`。read 的 id 按库排序匹配 Slug 或 Name，不匹配 Title；`--self` 定位原 source endpoint/ref，不隐含 scope=self，也不以 XBOT_BOT 定位。help 离线成功，read/write 缺原运行端点明确失败，不初始化客户机配置或回落到离线写。显式 root 必须与 source 的规范 XBOT_ROOT 相同。

原 loopback listener 提供 `GET /v1/skill/read?id=...`、`POST /v1/skill/write`，沿既有 Host/browser/root guard；本刀没有新增人用 owner runtime HTTP。POST 是单个 JSON 对象 `{slug,title?,description,body,scope?}`，拒未知字段与尾随对象，body 必需但可为空串，完整序列化请求最多1MiB（含转义及字段开销）。文件/stdin 读 max+1，超限非0且业务零改；不截断后声称写成功。CLI 随调用方 signal context，source client无代理/重定向，不自动重试写。

read 的每份候选 SKILL.md 原始字节最多20MiB，max+1明确拒绝；HTTP回包仍最多64KiB；为让原Shell消费者取得完整JSON，完整解析正文仅在最终JSON含换行≤现Shell32KiB时返回 `{slug,name,title?,description,body,bytes}`。过大回 `{slug,name,title?,description,preview?,file,access:"local",bytes}`，file为当前 Bot 工作区下 results 相对位置，文件含完整脱敏正文；metadata自身无法满足回包预算则明确错误。不会静默16KiB截断；routine 仍保持原16KiB当轮投影。不存在、当前停用或当前config读取失败均非0。

write 的 title 沿原单行化并安全写frontmatter；scope省略/空白为数据根全部本地Bot（含不同owner/shared/orphan），self为原ref当前名字，其它字符串EqualFold匹配真名，包含名为global的Bot。未知/歧义先失败，服务端不扫描历史推断范围。read沿source配置SkillsDir，write保原root/skills写口；配置和动态脱敏函数在source发布时捕获，不被其它K覆盖。source route→库.write.lock→逐config锁串行文件与默认开关，库锁内重新解析目标；手动enable/disable随后仍权威。只合并开关及必要name降级字段，未请求的legacy style/未知字段保留，不触发记忆迁移。

写回 `{slug,written,scope,target?,updated_bots,error?}`；scope是规范化的显式输入，空串表示全本地，target为已解析实际单Bot。文件已写而后续config失败，written仍true、updated_bots保真实成功项，CLI先输出回执再非0，不称跨Bot事务。两旧模型工具及schema已经退役；research共用窄直调lexer与纯argv parser，仅允许self read/help。既有离线list/show/import/remove/enable/disable和设置页接口保留，show是原SKILL.md。决定见 [ADR0050](adr/0050-skills-runtime-cli.md)。

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

`{peer}` 也可以写成 `bot@设备`（设备是配对表里的精确设备名），只读本机这一半：收到的取本机 transcript 的 `bot:bot@设备` 事件；新 peer-message 发出的取源 transcript 普通内部事实，旧 `message_bot` 调用/配对工具结果仍兼容读取；退回通知为 `kind:"bounce"`。远端历史只核 owned 本机 Bot 与设备 ByName 存在，已标 unpaired 的设备仍可读，不套发送时的 Person/XKey/NoBotMessaging 门。

新远端事实为 OriginInternal、RoleUser、空 EventID，正文精确为 `<peer_message_outgoing>\n` + JSON 对象 + `\n</peer_message_outgoing>`。payload 含完整冻结请求 `bot/text`、gateway/gateway_status、queued、envelope_id、call_id 与 error；经过原 source scrub，file/stdin 不事后重读。`recordTurn` 显式取 prepared `callTurnKey` 的实际 execution owner，记录跟随其动态 branch 和当前 Resources 路径；sender chain/hops 另取冻结 tuple。reader 只采此 origin/role/空 EventID 与精确格式，行 ID 为稳定 `peer_message_<seq>`。全文转义后可超过 CLI 32 KiB，仍受普通 entry 1 MiB 上限，不塞短回执；记录错误不回滚发送或排队。没有伪 ToolCall、新 Entry/Job 字段、独立私聊账或 Inbox/Event/wake。

只读，时间从旧到新。未登录 401；本地线程要求调用者同时拥有两个 Bot，不是 owner、Bot 不存在或同名返回 404 `{"error":"not found"}`。本地 `Conversation` 仍只读双方收件事件，发送方工具调用不计；留在 Inbox 尚未写入 transcript 的消息先不出现。远端 `RemoteConversation` 另读上述源事实与旧 native 历史。两者均不另存会话，群消息不在这里；用户聊天窗口中 Bot 来信仍是一行淡提示。

`limit` 缺省 50，最大 200。小于 1、大于 200，或不是整数，400 `{"error":"bad limit"}`。

`before` 可选，是排他游标。取客户端已经拿到的最旧一条的 `to` 和 `seq`，写成 `bob:3` 这种 `<收件人>:<seq>`。返回严格排在这条前面的最多 `limit` 条，仍然从旧到新。没有这条消息时 400 `{"error":"bad before"}`。两个 transcript 各自编号，同一个数字可能落在两边，所以游标带上收件人。只写一个数字时，仅当整段线程里恰好一条消息是这个 seq 才接受，否则同样 400。

本地行的 `seq` 是收件 Bot transcript 序号，和 `GET /api/bots/{to}/transcript` 相同，`id` 为事件 id。`from` 是发送 Bot，`to` 是收件 Bot。收件事件的 `text` 去掉外层 `<untrusted_event>`；远端新 outgoing 的 `text` 是完整冻结请求经源脱敏的正文，保请求值，不冒充 trim/脱敏后的最终 wire。`ts` 是这条 transcript 记录的时间，UTC。`from_bot`/`from_device`/`to_bot`/`to_device` 是拆开的 `from`/`to`，客户端不用自己解析 `bot@设备`；本机的 Bot 没有设备，`from_device`/`to_device` 省掉，设备取配对表里的 Label，没设就是设备名。跨设备的行 `from`/`to` 仍是完整地址，`kind` 只在不普通时出现（退回是 `"bounce"`）。`status` 只在发出的行出现：新事实优先按 queued:true 为 `queued`，否则 gateway accepted 为 `sent`、unknown 为 `unknown`，其余为 `failed`；sent 仅网关接受。旧 native 历史仍按工具结果读 sent/queued/failed，缺结果默认 sent。queued 保留发送当时事实，不因后来 flush 改旧行；incoming/bounce 保持原路径。跨设备的 `seq` 是本机 transcript 序号，游标 `<to>:<seq>` 照样适用。

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

`GET /api/devices/desktop` 沿现有主人 cookie 鉴权，返回 `{platform,status}`：`status` 是官方 agent-desktop 的原 JSON envelope（权限位于 `status.data.permissions`），不请求系统授权。依赖缺失或探测失败时仍以 200 返回 `{platform,error}` 供设置显示「查不到状态」，不伪造 granted；Linux 返回 `{platform:"linux",legacy:true}`。打开设备页或手动重新检查时调用，不参与设备轮询。动作工具与本机 CLI 的合同见 [机器与设备](atlas/machines.md)。

`GET /api/devices` 返回已配对的设备列表：`{"self","gateway_ok","devices","pairing"}`，每台设备有 `id`、`label`、`name`、`bots`、`bot_avatars`、`seen`、`online` 等字段（`seen`、`added` 是时间，`unpaired`、`online` 是布尔值）。`bots` 照旧是另一台设备上 Bot 的名字数组。`bot_avatars` 是同一批 Bot 的头像，每个列出的 Bot 一项：`{"writer": {"avatar_seed": "writer#2"}, "coach": {"custom": true}}`。种子由对方随设备目录送来，没设种子时取 Bot 名字；`custom` 表示对方用的是上传的图片，图片不过网关，所以界面在能传图之前先画几何字形。对方版本较旧、没送种子时这一项是空的，界面画几何字形，不拿名字当种子（免得和本机同名 Bot 一张脸）。种子先在接收时校验一次，建视图时再校验一次，不合法的丢掉种子、保留 Bot。没有 Bot 时是 `{}`，从不是 `null`。

## 供应商与模型绑定

`GET /api/providers` 返回 `{providers:[{name,protocol,api_key_env,key_configured,models}]}`。只返回环境变量名和配置状态，不返回密钥值。模型项含 `model`、`context`、`vision`、`hosted_web_search`、`available`；`available` 表示协议可执行。每项都是 `providers.json` 里的条目（DeepSeek 是其中名为 `deepseek` 的一项），没有空名的默认端点；`brand` 按 `base_url` 主机名识别，仅供图标和名称展示；未知窗口为 0。`GET /api/models` 也带协议和能力字段，原认证过滤不变。

`GET /api/providers` 的供应商项另含 `base_url`、`custom`、`display_name?`、人话 `title`；`custom` 包括设置创建和原 CLI 非内置条目。模型项带同源 `custom/title` 供绑定摘要显示。

- `GET /api/providers/infer?base_url=<地址>`：需登录，按既有 host 表返回 `{brand,title,protocol}`，未知为 `自定义供应商` / `chat_completions`；只推断，不注册、不请求上游。
- `POST /api/providers/custom`：需登录，接收 `{base_url,key,display_name?,protocol?}`；创建独立供应商（即使 host 匹配内置），返回 `{provider:<稳定配置名>}`，不预取模型。未指定协议则按 host 推断；非法 URL/协议、空密钥为 400；HTTP 可保存。密钥只进入既有 env/held，JSON 配置仅记变量名。
- `PUT /api/providers/custom/{name}/config`：同上，编辑自定义项；空 key 保留已存值，空 display_name 清除展示名，省略/空 protocol 按新地址推断。配置名保持不变；非自定义/未知项 404。
- `DELETE /api/providers/custom/{name}`：删除自定义项和独占密钥；共享密钥保留给其他供应商，Bot 绑定不改写，需重新换绑。
- `PUT /api/providers/custom/{name}/models`：接收 `{model,enabled,context?}`；经 config.Add/Remove 登记/取消模型，未知窗口为 256000；最后模型取消仍保留端点，手动登记后可沿原绑定 API 选用。以上 API 都需登录，401 表示未登录。

`GET /api/providers/models?provider=<配置名>`（需登录）实时拉取该供应商模型名单；配置名必须是表里的项；ChatGPT 订阅按保存的配置名读取 SIWC 账号目录（`models[].slug`、`visibility: list`），凭证由既有 vault 注入；未登录或凭证失效时返回 422 并引导重新登录。返回 `{models:[...]}`，字段同模型能力项。名单来自厂商，能力来自现有 ModelSpec 规则；ChatGPT 未配置模型的窗口取账号目录，其余未知窗口为仓内默认 256000。未配密钥、网络失败、非法或超限名单返回 422 短错误；API 供应商成功但无模型时返回空数组；ChatGPT 账号无可用模型时返回 422。该读取不会增加模型条目、认证或绑定；有效 context_length 元数据会刷新已登记模型的窗口，规则见 [供应商](atlas/providers.md)。

`PUT /api/bots/{name}/binding` 接收 `{provider,model}`，成功返回保存的二元绑定。二者为空时清除绑定。模型尚未登记时，先实时拉一次该供应商名单，名单里有且可用就登记（名称与窗口）后再绑定；更换模型浮层按供应商逐个读 `/api/providers/models`，某家失败只在该组显示「模型名单暂时拉不下来」与重试。名单里也没有的模型、协议不支持或缺少密钥为 400；Bot 正在工作为 409。换到更小窗口时先由内核预检并按需压缩；仍超新 H、无法安全压缩或摘要失败为 409（「上下文仍超新窗口，需先删分支/导出」），旧绑定保留，已成功保存的摘要块继续有效。成功后下一轮重建内核，不重写 transcript。

OAuth 供应商额外返回 `oauth`（`openrouter`、`chatgpt` 或 `xai`）；`key_configured` 此时表示 vault 中已保存凭证，不暴露令牌。保险箱不可用时该项带 `auth_error`，其他 key 供应商仍可列出和绑定。

旧 `PUT /api/providers/models/image-generation` 保留供已有配置调用，设置界面不再提供此入口；其中 enabled 标记不决定全局策略，实际路由由 `/api/image-gen` 的三态与 OAuth Responses 能力决定。该接口（需用户登录）接收 `{provider,model,enabled,image_model}`，更新现有 `providers.json` 中已登记模型的 `hosted_image_generation` 和 `image_generation_model`；只允许 Responses 开启，模型名空串使用厂商默认。成功返回 `{provider,model,hosted_image_generation,image_generation_model}`；未登录 401、无法解析 400、未知绑定/不支持的协议或非法模型名 422。下一轮模型请求读取新配置，无须重建内核。`GET /api/providers`、`GET /api/models` 和实时模型目录携带生图能力字段；`registered` 表示模型已登记、允许保存设置，读取目录不自动登记模型。

`POST /api/providers/oauth/{kind}/start`（需用户登录）返回 `{url}`，供浏览器打开授权窗口。支持 `openrouter`、`chatgpt`、`xai`（xAI 用 Grok CLI 公共客户端 PKCE，回调固定监听 `127.0.0.1:56121/callback`，端口被占用时报人话错误）；重复点击复用十分钟内的待完成尝试；已有另一类登录进行中返回 400。`GET /api/providers/oauth/{kind}/status` 返回 `{state,error?}`，状态为 `idle`、`pending`、`succeeded`、`failed` 或 `cancelled`，错误不含授权响应明文。`POST /api/providers/oauth/{kind}/cancel`（同 start 鉴权）取消进行中的登录：关闭回调监听、状态置 `cancelled`，取消或失败后可立刻重新 start；无可取消时 409。`POST /api/providers/oauth/{kind}/logout`（同 start 鉴权）退出该账号：先停掉进行中的登录，再删除 vault 中的凭证和该 kind 的 OAuth 绑定（同名的 API key 绑定不动），状态回到 `idle`，返回 `{status:"logged_out"}`；什么都没存时也返回 200；不支持的 kind 返回 404，删除失败返回 500 和人话错误。绑在这个供应商上的 Bot 会失去凭证，直到再次登录（重建同名绑定）。退出不等待进行中的自动刷新：刷新写回前会核对凭证仍是它开始时那一份，已被退出删掉就放弃写回（本地与保险箱守护进程同一路径）。设置页只在点击 ChatGPT 订阅卡或 xAI 登录按钮、或详情的「重新登录」时启动授权；轮询成功后刷新供应商列表。回调由短期 loopback listener 接收，不通过对话或 transcript 传递授权码。协议及凭证保存方式见 [供应商](atlas/providers.md)。

Transcript 消息保留原 `content` 字符串投影，另有可选 `parts` 数组，包含 `server_tool` 的 `hosted:{id,name,status,query}` 和 `citation:{url,title,start,end}`；位置按 Unicode 字符计数。助手行另有可选 `thinking` 字符串，是该行可读思考正文（Chat Completions 的 `reasoning_content`、Anthropic thinking 块正文，多段以空行合并），只给「轨迹」用；网页不接收 `vendor_raw`、signature、redacted/encrypted reasoning；Chat Completions 的 `reasoning_content` 也不进 `delta`。消息 SSE 增加 `part` 事件，承载临时 hosted 步骤；最终内容从 transcript 读取。

### Bot 收件箱

`GET /api/bots/{name}/inbox?limit=50` 需登录且为 Bot owner。返回 `{pending_count,items}`，包含待处理与已吸收事件，按事件时间新到旧；主人聊天和确认事件不列入。`limit` 默认 50，范围 1–100；每项含 id、source、type、sender、sender_kind、time、summary 和 status（pending 或 acked）。摘要去除 untrusted_event 标签；Webhook 取首个 delivery 正文，文件监听去除 watch 外壳，定时 routine 只取标签内正文并去掉后续唤醒套话；正文去除首尾空白后最多 1000 字符（截断时加省略号）。接口只读，不执行 Ack。

`POST /api/bots/{name}/events` 接收本机会话认证的 CLI 外部事件，body 为 `{"sender":"codex","text":"..."}`；sender 必填、最长 64 字节，text 非空、最长 16 KiB。等待 Bot 的处理锁后写入 inbox，作为 `source=cli` 的不可信事件处理；SSE 先发 `accepted`（含事件 id），然后发 `done`（`text` 为本轮可见内容，空为安静）或 `error`。断开连接后已受理事件继续执行。此接口不使用主人消息插话路径。
