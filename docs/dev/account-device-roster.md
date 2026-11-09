# 侧栏并进账号设备（同一账号里的其他电脑）

## 授权与级别

M 档。主 Agent 指派，用户要的是「登录同一账号后，把其他设备上的 Bot 也列进侧栏，点开能直接聊」。
现有侧栏只认短码配对的设备，两台都登录同一个账号时那一组是空的，所以先补名单，再单开对话线路。
本设计不含新视觉：分组标题、在线/离线、空态都沿用 `paintSidebar()` 已有写法。

## 现状

- 侧栏 `#remote-roster` 由 `internal/web/static/devices.js` 的 `paintSidebar()` 画，数据来自 `GET /api/devices`，
  即 `remote/devices.json` 里短码配对过的设备（每台带 XKey，对话走加密信箱）。
- 登录后的账号设备是另一路：`GET /api/account/devices`（`internal/web/account_devices.go`），
  每台有 `id/name/online/last_seen/bots[{name}]/self`，现在只用于设置页「账号设备」。
- 远端对话面板 `internal/web/static/remotechat.js`，接口 `/api/remote/{device}/bots/{bot}/messages`
  只按 `remote.Load(root)` 解析设备：账号设备不在那个文件里，也没有 XKey，所以点开必 404。
  账号设备目前唯一的跨机通道是 pipe 的 device-execution（shell/文件/浏览器/桌面），没有对话通道。
- 两台机器都登录同一账号时，谁也不在对方的短码配对名单里，侧栏因此两边都空。

## 打算怎么做

1. 名单：`devices.js` 增加一次 `GET /api/account/devices`（登录后才有，最多一分钟读一次），
   把 `self` 那台排除，其余按设备并进侧栏同一组；同 id 的设备去重（配对那路优先）。
   `bots` 是对象数组，取 `name`。分组标题、在线/离线、`等它的 Bot 列表`、组头折叠、离线变淡都复用原写法；
   账号设备的离线时间写「上次在线」，配对设备仍是「最后联系」。
2. 点开分路：`remotechat.js` 打开面板时问一句 `D.blockedReason(id)`。配对设备照旧走 `/api/remote/...`；
   账号设备当前没有通道，就**不发任何请求**，面板里说明一句、发送也只回这句话（输入框不禁用，沿用「离线不禁输入」的约定）。
   设备行和面板标题都能查到是哪种设备（`D.sideDevice(id)`）。
3. 通道开关：`/api/account/devices` 行上带 `chat` 字段。路由 `/api/account/devices/{id}/bots/{bot}/messages`
   （GET 历史 / POST 发送 / read 空实现）用 `pipe.NewTicket`＋`Dial` 带用户的账号会话代理到目标电脑自己的
   `GET /api/bots/<bot>/chat`、`POST /api/bots/<bot>/messages`，把行映射成面板现在吃的
   `{messages:[{id,role,text,at,state}], device:{...}}`。这条线路由 `XBOT_ACCOUNT_CHAT=1` 打开，默认关；
   `chat` 字段跟着同一个开关走，关着时侧栏不会提供点不开的入口。

## 怎样算做完

- 登录同一账号、另一台电脑报了 Bot 名单时，侧栏出现那台设备分组和它的 Bot；本机那台不重复出现。
- 点配对设备的 Bot 仍走加密信箱；点账号设备的 Bot 不发请求，面板里有一句能看懂的话。
- 未登录/没有账号时行为与改动前一致：不读账号设备，那一组不出现。
- `go vet ./...`、`go build ./...` 干净；`internal/web` 里 devices/remote/account/sidebar 相关用例全绿，
  包括两个新浏览器用例（有账号设备 / 没账号）和代理路由的行为用例。
- `XBOT_ACCOUNT_CHAT=1` 时路由存在、按账号会话转发、本机与未知 Bot 被拒；关着时这些路由不存在。

## 还没做的

- 没有真 hub 上的双机联调：`pipe.NewTicket`/`Dial` 这条真线路只用测试替身验到「票的参数、路径、映射」层，
  真机打通要两台都在线且都在 `xbot serve` 待命。
- pipe 把目标机器的 SSE 整段缓冲后一次交回，所以发送要等这一轮说完；一轮输出超过 700 KiB 会被设备侧上限拒掉，
  那时回复只能靠面板轮询历史补上。冷却/复用会话、断线重试都没有做。
- 账号设备没有未读数和最后一句预览（那是配对信箱的账），所以行上只画名字；read 接口是 204 空实现。
- 离线账号设备不可发信（没有排队这回事），和配对设备「交给网关最多等 7 天」不同，面板文案分开写了。
- 视觉没有新做，也没有审美顾问评审；沿用原分组样式。
