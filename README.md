# XBot

XBot 是一个从零写的个人助手（M5）。交互单位是 **Bot**：一个名字、一份 persona、一条只追加的对话。没有 session，也没有 `--resume`。终端和网页是同一条对话的两个入口。

设计决定记在 [docs/DESIGN.md](docs/DESIGN.md)。网页给界面用的 HTTP 接口记在 [docs/API.md](docs/API.md)。后续里程碑以设计记录为准。

## 构建与运行

```bash
go build -o bin/xbot ./cmd/xbot
```

```bash
export XBOT_API_KEY=sk-...
# 可选。没有绑定 provider 时用这两项，默认是 DeepSeek
export XBOT_BASE_URL=https://api.deepseek.com/v1
export XBOT_MODEL=deepseek-chat

xbot provider add deepseek \
  --base-url https://api.deepseek.com/v1 \
  --api-key-env DEEPSEEK_API_KEY \
  --model deepseek-chat --context 64000
export DEEPSEEK_API_KEY=sk-...

xbot new ada --persona "你是一个简短的编程助手。" --provider deepseek --model deepseek-chat
xbot list
xbot chat ada
xbot serve --http 127.0.0.1:8787
xbot webhook add ada github
xbot watch add ada notes --path "$HOME/notes/**/*.md"
# Telegram / 飞书要同名环境变量里已有密钥，缺了就不安装
export TELEGRAM_BOT_TOKEN=1234:abc
xbot link install ada telegram
xbot runner --addr 127.0.0.1:18771
xbot node serve
xbot node add desk --url http://100.x.y.z:18772 --token TOKEN
xbot node call desk info
```

`xbot serve` 在同一个 `--http` 上提供网页。启动时往 stderr 打印登录 token，token 文件在 `~/.xbot/web-token.json`，权限 0600。浏览器打开打印出来的地址，把 token 贴进登录框。登录态是 HttpOnly、SameSite=Strict 的 cookie，不放在 localStorage。

`chat` 接着这个 Bot 唯一的一条对话继续。聊天的时候，这个进程持有该 Bot 的处理锁，并在等待输入的同时处理 inbox 里的事件。`serve` 不管有没有人在聊，都会把到点的 routine、到点的文件变化和已经合批的 webhook 投进 inbox；只有拿到处理锁的那一个进程会执行它。`--interval`（默认 15 秒）是没人在聊时，serve 多久处理一次这些事件。正在聊的进程大约每 200 毫秒就能看到。退出输入 `exit`、`quit` 或 `:q`。

长时间的活用 `spawn` 交给子代理。本机默认是 goroutine。`xbot runner` 是同一套执行器的 HTTP 服务，只听环回地址，要用 token。`xbot chat --runner URL --runner-token TOKEN` 把 Bot 接到它上面。

执行 shell、`write_file`、`edit_file` 之前会问 `y/n`。改 persona、创建或修改 routine 也要确认。`--yes` 跳过询问。`serve` 里没有人点头，这些需要确认的工具会被拒绝。`notify_owner` 不需要确认，它是说给主人听的。

`--root` 放在子命令前面，默认是 `~/.xbot`。

## 配置

| 变量 | 含义 | 默认 |
| --- | --- | --- |
| `XBOT_API_KEY` | 未绑定时的 API key | 空 |
| `XBOT_BASE_URL` | 未绑定时的 OpenAI 兼容 base URL | `https://api.deepseek.com/v1` |
| `XBOT_MODEL` | 未绑定时的模型名 | `deepseek-chat` |
| `XBOT_PUBLIC_URL` | `webhook add` 打印的对外地址 | `http://127.0.0.1:8787` |
| `XBOT_RUNNER_URL` | 子代理和编码任务使用的 runner | 空，本机执行 |
| `XBOT_RUNNER_TOKEN` | runner 的 bearer token | 空 |
| `XBOT_CODE_AGENT` | 编码任务要启动的外部程序，优先于 runner 和本机模型 | 空 |

命名 provider 写在 `~/.xbot/providers.json`。里面只存 `api_key_env`（环境变量的名字），不存密钥本身。Bot 的 `config.json` 只记下 provider 名和 model 名。

```bash
xbot provider list
xbot provider remove deepseek --model deepseek-chat
xbot bind ada --provider deepseek --model deepseek-chat
xbot bind ada --clear
```

`xbot new` 可以带 `--provider` 和 `--model`。两个都空就用上面的环境变量。内核和模型之间是 `provider.ModelSelector`。现在只有固定绑定（`FixedSelector`），以后的自动路由走同一个接口。

请求发到 `{base}/chat/completions`，带 tool calling，流式返回。不依赖 Anthropic。

## 目录

```text
~/.xbot/providers.json
~/.xbot/web-token.json
~/.xbot/web-cursor.json
~/.xbot/peer-rate.json
~/.xbot/groups/<id>/{meta.json,transcript.jsonl}
~/.xbot/people/<id>/identity.json
~/.xbot/people/<id>/memory/{profile,log}.md
~/.xbot/bots/<name>/config.json
~/.xbot/bots/<name>/mcp.json
~/.xbot/bots/<name>/secrets.json
~/.xbot/bots/<name>/drafts/<id>.json
~/.xbot/bots/<name>/connectors/seen.json
~/.xbot/bots/<name>/code/<id>.json
~/.xbot/bots/<name>/persona.md
~/.xbot/bots/<name>/rules.md
~/.xbot/bots/<name>/transcript.jsonl
~/.xbot/bots/<name>/state.json
~/.xbot/bots/<name>/inbox.jsonl
~/.xbot/bots/<name>/outbox.jsonl
~/.xbot/bots/<name>/routines/<id>.json
~/.xbot/bots/<name>/webhooks/id/<name>.json
~/.xbot/bots/<name>/webhooks/seen.json
~/.xbot/bots/<name>/watches/id/<id>.json
~/.xbot/bots/<name>/watches/state.json
~/.xbot/bots/<name>/memory/{profile,log}.md
~/.xbot/bots/<name>/people/<id>/memory/{profile,log}.md
~/.xbot/bots/<name>/agents/<id>/{transcript.jsonl,meta.json}
~/.xbot/work/<name>/
~/.xbot/work/shared/
~/.xbot/skills/<owner>/<slug>/SKILL.md
```

Markdown 是给人改的。索引比 markdown 旧时会按 markdown 重建。

`xbot new` 默认授予 `terminal`、`tools`、`timer`、`memory`、`persona`、`context`、`agents`、`skills`、`webhook`、`watch`、`web`、`peers`，并把当前终端用户记成 owner。shell 和相对路径的文件工具以 `~/.xbot/work/<bot>/` 为工作目录，共享目录在 `XBOT_SHARED`。每个 owner 有自己的技能库。恰好一个 owner 的 Bot 默认看见这个人的技能，一行一个名字和说明；正文用 `read_skill` 读，只在当前这一轮。这个 Bot 可以单独停用某一条。没有技能文件时，前缀和以前一样。

`xbot skill import <path>` 把一份 `SKILL.md` 拷进当前用户的库。`path` 可以是技能目录或文件，也可以是符号链接。`xbot skill list` 和 `xbot skill show <slug>` 看这个库。`xbot skill disable <bot> <slug>` 和 `xbot skill enable <bot> <slug>` 只改这个 Bot 的停用名单。

## 架构

内核只认识收件箱和工具。外面的一切都是 Link。

```text
terminal / web / timer / webhook / watch ──Event──▶ inbox ──▶ kernel ──▶ ModelSelector ──▶ provider
tools / memory / persona / context / peers ──tools──▶ agent loop
bot / group ──untrusted Event──▶ 另一个 Bot 的 inbox
```

Webhook 和文件监听只负责把外部事情变成 Event。它们不调用模型。同一来源在一个短窗口里的多次投递会合成一条再进 inbox，所以一串请求只会唤醒一次。Bot 自己决定处理、`notify_owner`，或者回复 `SILENT`。

**信任。** 主人在终端或网页上的输入是指令，原文进 transcript。除此之外（别的人、别的 Bot、群消息、timer、webhook、文件变化、子代理）都包在 `<untrusted_event>` 里，当成数据。是不是主人由内核判断，不看事件自己带的标记。来源不是 terminal 或 web 时，就算发送者字符串和 owner 相同，也仍然是不可信数据。

**稳定前缀和环境快照。** 每次唤醒的 system 消息只有 persona、rules、以及按名字排序的 link 说明。工具 schema、时间、发送者、记忆都不在里面，这样上游的前缀缓存可以对得上。工具列表按名字排序，schema 变化不会把这段前缀打乱。

易变的部分是环境快照：小时、发送者、工具名、未读通知数、persona/rules/记忆的哈希。和上一次比，没变就不写；变了就在 transcript 末尾追加一条很短的 `<environment_update>`，只含差异。时间只在小时变化，或距离上次唤醒至少 30 分钟时才写。不改历史。persona 文件改完并得到主人确认后，下一次唤醒的前缀换成新文本，同时多一条 `persona: updated`。

**上下文预算。** 粗算按大约 4 字节约 1 token，分成 stable、memory、env、history 四段。总量超过预算（默认约 4.8 万，或 context 的四分之三；绑定的模型 context 会用来算这个预算）就当场压缩。History 自己的预算是总预算减去另外三段。它用到这份预算的大约 70% 时，后台先压更早的一段，这一轮不等它。Profile 记忆注入最多 4 KiB。

**压缩。** transcript 只追加，序列号就是行号，不会改。摘要是 state 里的块，每块记下它覆盖的 `[from, to]`。更大的块盖住里面的小块。模型看到的顺序是：稳定前缀、当前 profile 记忆、摘要块、还没被摘要盖住的原文。

模型可以调用 `compact(from, to, focus)` 自己选一段已经结束的范围。内核会拒绝：切开未完成的工具调用、碰到最后 N 条原文、和已有摘要块部分重叠。完整包住旧块是允许的，那就是一层更粗的摘要。到了硬上限，内核仍会当场压最早的一段，这一轮会等摘要完成。

`recall_history` 和 `expand` 可以多带一个 `bot`，读同一个 owner 的另一个 Bot 的原文。别的 owner、只是同群、或群 id，都是 `not found`。读到的内容包在 `<untrusted_event>` 里，标明来自那个 Bot，不能当成指令。

侧边栏分组是 Bot 档案上的 `group`，加上当前用户自己的一份顺序。`xbot group` 可以新建、改名、删除分组，或把 Bot 放进去。删除分组不会删除 Bot。这和聊天群不是一回事。

`xbot link list` 是内置的 Link 目录。`xbot link install <bot> <id>` 把已经有运行时的条目加进这个 Bot 的授权。Telegram 和飞书要同名环境变量里已经有密钥（`TELEGRAM_BOT_TOKEN`、`LARK_APP_SECRET`），缺了就不装。装上之后，`xbot serve` 接受 `POST /connector/telegram/<bot>` 和 `POST /connector/lark/<bot>`，对上密钥的消息进收件箱，当成不可信事件。代表主人发出去的话先留草稿，主人确认后才发。`xbot link install <bot> coding-agent` 加上编码任务。工作目录、仓库、步数和时间按这次任务另定，完成时进收件箱。`XBOT_CODE_AGENT` 指向一个外部编码代理时用它；否则交给 `xbot runner` 的 `/v1/code/start`；都没有就用这个 Bot 的模型在本进程里跑。三个都没配时只写记录。MCP 不走 `link install`。网页上的安装接口不收密钥。

`xbot mcp add <bot> <name> --command PATH` 把一个 MCP 服务器记在这个 Bot 的 `mcp.json`。参数用重复的 `--arg`。密钥只写环境变量的名字（`--secret-env NAME`），文件和接口里都没有值。对话里的工具名是 `mcp_<服务器>_<工具>`。`readOnlyHint` 为真的不用确认，其余要确认。服务器可以用通知把不可信事件推进收件箱。

Bot 用内置工具 `links` 搜索目录、安装、卸下和加载。安装要主人在确认卡上点头；带密钥的还要主人在这张卡里填写，模型看不到值。已安装的 Link 在系统提示里每条一行。`load` 只把工具 schema 带进当前这一轮。

```bash
xbot export ada --out ada.md
xbot export ada --format jsonl --tools --from 1 --to 20 --out ada.jsonl
```

`md` 用本机时区，摘要块单独成段。`jsonl` 每行带原来的 seq，普通行可以再导入。默认不写工具调用和工具结果。网页对应 `GET /api/bots/{name}/export`。

软阈值走另一条路。History 过了大约 70% 就写一份 `compact-job.json`，在后台压一段更早的、守同样边界的范围。主循环继续处理事件。这段范围在任务开始时就定死了，压的过程里新到的消息序号在范围外面，不会被卷进去。每个 Bot 同时只有一个任务；第二个进程抢不到运行锁就退出。摘要和抽出来的事实先落在任务文件里。块只在两轮之间换进 state，不会插在模型调用中间。换过同范围的块再换一次是空操作。任务还在 `running`、摘要没写完时崩溃，不会留下半块；下次唤醒接着压同一段。压完如果尾巴里已经没有环境消息，就再追加一条完整的 `<environment_snapshot>`，避免状态被摘要吃掉。抽事实和摘要在同一个后台任务里做，写入记忆时按正文去重。

**记忆。** 三个范围，两档：

| 范围 | 谁看得见 | 档 |
| --- | --- | --- |
| person | 所有 Bot | profile 每次注入；log 只在 `recall` 时搜 |
| bot | 这个 Bot | 同上 |
| bot-person | 这个 Bot 和这个人 | 同上 |

冲突规则不变：关于人的事实，新的赢；怎么干活，bot 范围赢。`remember` / `forget` / `recall` 是工具。检索是 BM25，没有向量库。Person 事实必须带上写下它的 Bot。来自不可信事件的 person 事实会在正文里标成 untrusted。单条事实最多 1 KiB，profile 文件 16 KiB，log 文件 64 KiB。

**历史。** 原文按大约 8MB 或 UTC 日期分成段，只有尾段可写。SQLite 索引（纯 Go，无 cgo）把序号映射到段和偏移，并用 FTS5 做全文。`recall_history(query)` 走这个索引的 BM25，带回序列号。`expand(from, to)` 按序号把原文拉回来，输出有上限。大段工具输出另存成 blob，上下文里只留预览。索引丢了会自动重建，也可以 `xbot reindex <bot>`。旧的单文件 `transcript.jsonl` 第一次打开时迁进分段。摘要掉的句子还能这样找到。

**定时。** routine 是一段 prompt 加 5 段 cron，最小间隔 5 分钟。`xbot serve` 约每 15 秒看一次。到点就往 inbox 投一条事件，id 是 `rt_<id>_<分钟>`，同一分钟不会投两次。唤醒之后，值得说就 `notify_owner`（写入 outbox，并尽量 `notify-send`）；不值得说就只回复 `SILENT`，不发通知。`xbot chat` 会先把未读通知打出来。

**Webhook。** `xbot serve` 在 `--http` 上听 `POST /webhook/<bot>/<name>`，默认只绑 `127.0.0.1:8787`。每个 webhook 有自己的 secret，模式是 HMAC（`X-XBot-Signature: sha256=<hex>`）或 `Authorization: Bearer`。没签名、签错、或者用了另一种模式，都是 401。正文超过 32 KiB 是 413，不会存下来。带了 `X-XBot-Delivery`（或 `X-Delivery-Id`）的重复投递只收一次，第二次直接 200 `duplicate`。同一个 webhook 在 2 秒窗口里的多次请求合成一条事件再投进 inbox；窗口是从第一条算起的固定窗，连续灌也不会把模型叫醒超过每个窗口一次。合批正文最多 32 KiB，多出来的只计数。事件来源是 `webhook:<name>`，发送者是外部身份，内核会包成不可信数据。

```bash
xbot webhook add ada github --auth hmac --public-url http://127.0.0.1:8787
xbot webhook list ada
xbot webhook remove ada github
```

`add` 打印 URL 和 secret。`list` 只打印名字、模式和 URL，不再打印 secret。secret 在 Bot 目录里，文件权限 0600。配置里的 `webhooks` 名单才算授权；只把文件放进目录不会生效。

**文件监听。** `xbot watch add <bot> <id> --path PATTERN`。PATTERN 必须是绝对路径，不能含 `..`，也不能从文件系统根目录扫。用标准库轮询 mtime 和大小，不读文件内容。第一次看见现有文件只做基线，重启不会把它们全部报成 created。之后的 created / modified / deleted 会防抖（默认约 500 毫秒，从最后一次变化算起）。如果一直在写，最多憋住 10 个防抖间隔，然后仍然发出一条。一个 watch 一轮最多报 200 个路径，一个 Bot 最多 32 个 watch。事件里只有路径、种类、大小和 mtime。指向目录外的符号链接会跳过。

```bash
xbot watch add ada notes --path "$HOME/notes/**/*.md"
xbot watch list ada
xbot watch remove ada notes
```

**网页。** `xbot serve --http` 同时挂网页和 webhook。静态文件用 `go:embed` 打进二进制，没有单独的前端进程。同一条 transcript：终端里说的话，网页上看得到；网页里发的消息，终端下一次处理时也在同一条记录里。网页拿到处理锁就直接跑这一轮，并用 SSE 把增量推回来。锁被终端占用时，网页只把事件放进 inbox，返回 202，由持锁的那边处理，不会两边各跑一次。

网页上可以看 Bot 列表（未读点）、连续对话、Markdown 和代码块、可折叠的工具卡片、`ask_user` 的选项卡、通知、右侧详情（档案、persona、provider/model、记忆 profile、routine、正在跑的子代理，可以 steer / cancel，也可以导出或导入模板），也可以新建 Bot 和群。手机上左侧是抽屉，详情是底部抽屉，composer 避开安全区。支持浅色和深色。

可以「添加到主屏幕」：有 manifest 和 service worker。通知在页面开着时走页面内列表，并在用户允许后用 Notifications API 弹出。后台 Web Push（VAPID）还没做，留作后续：标准库没有现成的载荷加密。

**Bot 之间。** 发送者是 Principal：人、Bot 或群。同一个 owner 的 Bot 默认可互相看见，别人的 Bot 看不见，也发不了消息。工具：

- `list_bots` 列出同 owner 的其他 Bot
- `message_bot(bot, text)` 往对方 inbox 投一条事件，来源 `bot:<发送者>`，发送者种类是 bot，正文是不可信数据，不附带任何一方的私人记忆
- `create_group(name, members)` / `list_groups` / `post_group(id, text)`
- `fork_bot(name, role, group?)` 分叉出一个新 Bot。这不是拷贝。源 Bot 按新角色写一份交接（项目背景、关键决定、这个角色用得上的上下文），写进新 Bot 的 bot 记忆；person 记忆本来就按人共享，不复制。新 Bot 用这份角色当自己的 persona，transcript 是空的，provider 绑定默认继承。两边各记一条：新 Bot 要问细节就 `message_bot` 问源 Bot，不能读源 Bot 的 transcript。`group` 有名字时，再建一个两边都在的群

```bash
xbot fork ada reviewer --role "Review pull requests for this project."
xbot fork ada reviewer --role "Review pull requests." --group ship
```

网页详情里也可以分叉。交接由源 Bot 的压缩器写成。transcript 和 bot 记忆都空时不调用模型，交接记成 `No transcript yet`。

**主 Bot 和置顶。** 第一次 `xbot list`、`xbot chat`（不写名字）或 `xbot serve` 时，如果目录里还没有 Bot，会自动建一个叫 `xbot` 的主 Bot，并且默认置顶。没点名的入口走它：`xbot chat` 不带名字，以及没有所属 Bot 的事件（进主 Bot 的 inbox）。主 Bot 可以 `create_bot`、`fork_bot`、`delete_bot`、`pin_bot`、`set_primary`。主 Bot 不能直接删，先 `xbot primary set <bot>` 把角色交出去，旧的才能删。最后一个 Bot 也不能删，目录里不会变成零个。任何 Bot 都可以 `xbot pin` / `xbot unpin`；置顶的排在前面。网页列表里主 Bot 有一枚「主」标记。

```bash
xbot chat
xbot primary
xbot primary set ada
xbot pin ada
xbot delete xbot
```

**Bot 档案。** 每个 Bot 有名字（可以改）、一行简介、头像、说话风格和免打扰时段。说话风格是语气、回复长度（短 / 中 / 长）、表情（不用 / 少量 / 常用）和默认语言，写进系统提示，跟在 persona 后面。没填的字段不进提示，空档案不会改变原来的稳定前缀。没有安静、适中、积极三档；通知在任何唤醒上都会送出，想自己约束就写在 persona 里。旧配置里的 `proactivity` 还能读进来，但不生效。免打扰用 Bot 时区的 `HH:MM`（没设时区就用唯一 owner 的时区，再没有用本机时区），时段可以跨过午夜；这段时间里的通知先攒着，结束后合成一条发出。主人在网页右侧「档案」或 CLI 里改，马上生效。Bot 自己用 `propose_profile` 提议，主人确认后才写入，下一次唤醒会看到 `profile: updated`。

头像可以上传 png、jpeg、gif 或 webp。没上传时是一块低饱和的色块加上名字首字母。`GenerateAvatar` 留作以后接生成头像，没设置时不会调用。

模板是一份 JSON：档案、persona、例程，可选头像。不含记忆，也不含 transcript。

```bash
xbot profile show ada
xbot profile set ada --description "项目笔记" --tone "平静、具体" --length short --emoji spare --language 中文 --quiet 22:00-07:00 --timezone Asia/Shanghai
xbot profile set ada --name notes
xbot profile avatar ada --file face.png
xbot template export ada --out ada.json
xbot template import --file ada.json --name ada-copy
```

群有自己的 transcript。一条群消息会扇出到每个成员的 inbox（Bot 自己发的不叫醒自己；人发的会叫醒所有成员）。成员看到的是这条群消息，不是彼此的私人记忆。网页里有群视图。

一条链最多 3 跳（人发起是 0，第一次 Bot 投递是 1）。同一对发送者到目标每分钟最多 8 条。超限的发送直接失败，不会继续转下去。

**子代理。** 主 Bot 自己做短活。长任务调用 `spawn(task, mode)`。`brief` 只有任务说明；`fork` 带上当前主上下文的一份拷贝。子代理有自己的只追加 transcript，步与步之间看自己的 inbox。主 Bot 可以 `steer`（下一步之前插入，已完成的工作保留）、`queue`（当前这次跑完再做）、`interrupt`（停掉当前这一步，用新指令重来，步数预算重置）、`cancel`（停掉，排队的也不做）。子代理把 progress、blocked、done、failed 投回主 inbox。这些事件不是终端输入，内核会包成 `<untrusted_event>`。`read_subagent` / `expand_subagent` 读它的状态和原文，输出有上限。同时最多 4 个，默认最多 16 步、10 分钟。子代理没有 spawn，也不会在后台弹 y/n：`xbot chat --yes` 才自动批准它的 shell；否则需要确认的工具直接拒绝。

**执行器。** `Executor` 有本机 goroutine 实现，也有 HTTP 实现：`POST /v1/start`、`POST /v1/message`、`GET /v1/events`。`xbot runner` 只绑定环回地址，Bearer token 用常量时间比较。请求体里的模型密钥只在这条本机连接上出现。

**并发。** 每个 Bot 只有一个进程持有 `handle.lock` 并调用 Handle。`serve` 拿不到锁时仍然投递 routine、webhook 和文件变化，聊天中的进程会收到并处理，同一条事件不会被两边各跑一次。事件 id 和 inbox 去重兜住重复投递。文件监听的游标加了锁，chat 和 serve 一起扫也不会各发一条。锁在进程退出时放开。子代理在别的 goroutine 里跑，完成时只是往主 inbox 投事件，不自己调用 Handle。

## 安全

- **shell**：默认 30 秒超时，最长 120 秒。超时杀掉整个进程组。输出默认最多 32 KiB。命令最长 16 KiB。stdin 接到空设备。
- **read_file**：`offset` 从 1 起，`limit` 默认 200 行、最多 2000 行，输出默认最多 32 KiB。
- **write_file / edit_file**：路径先变成绝对路径，并解析已存在前缀上的符号链接。`write_file` 正文最多 1 MiB。`edit_file` 要求 `old_string` 恰好出现一次，文件本身最多 2 MiB。
- 工具结果进 transcript 之前会收成合法 UTF-8，并再卡一道 64 KiB。
- 没有路径沙箱。写和执行靠确认。
- 数据目录 `0700`，配置和身份文件 `0600`。JSONL 追加时加文件锁。
- cron 最短 5 分钟。`* * * * *` 会被拒绝。
- 模型 API key 不进 Bot 目录，也不进 `providers.json`。Webhook secret 要留在本机才能验签，单独放在 0600 的文件里。
- 子代理默认最多 4 个并发、16 步、10 分钟。进度事件默认两秒一条。
- 子代理的输出是不可信数据，不进 system，读出来时也会标明。
- `xbot runner` 拒绝非环回地址。token 为空时会生成一个并打印。空 token 不能连上。
- Webhook 没有合法的 HMAC 或 Bearer 就拒绝。正文上限 32 KiB。delivery id 在接受时就记下，重复的不再进 inbox。
- Webhook secret 只在 `add` 时打印一次，文件权限 0600。`list` 不打印 secret。
- 文件监听拒绝相对路径、`..` 和从 `/` 开始的扫描。逃出根目录的符号链接不报。事件不含文件内容。每个 Bot 最多 32 个 watch，每个 glob 最多 200 个文件。
- 同一个 webhook 的突发请求会合成一条唤醒，避免把模型调用打满。
- 网页登录 token 在 `web-token.json`，权限 0600。cookie 是 HttpOnly、SameSite=Strict。变更请求拒绝 `Sec-Fetch-Site: cross-site`，带了 `Origin` 时必须和请求的 Host 一致。
- 网页把 Markdown 画成 DOM 文本节点。链接只允许 `http` 和 `https`。接口 JSON 会转义 `<`。
- Bot 发给 Bot 的正文按不可信事件包起来，不复制私人记忆。链长超过 3 跳，或同一对每分钟超过 8 条，会被拒绝。

## 测试

```bash
go test ./...
bash scripts/e2e.sh
```

`go test` 覆盖这些 M5 行为，以及 M2、M3、M4：

- 错误 token 和没有 cookie 的请求是 401。跨站 Origin 或 `Sec-Fetch-Site: cross-site` 是 403。token 文件权限 0600。
- 网页发一条消息，SSE 里有模型回复；transcript 里主人的原文没有被包成不可信事件。
- 终端再对同一个 Bot 说一句，网页读到的 transcript 里两边都在。
- 终端占着处理锁时，网页消息进 inbox 并返回排队，松开锁之后只处理一次。
- Bot A `message_bot` 给 Bot B，B 回复，A 的 transcript 里看得到回复。B 看到的是 `source="bot:ada"` 的不可信事件，里面没有 A 的私人记忆。
- `fork_bot` 之后，新 Bot 的 bot 记忆里有交接，transcript 是空的，能 `message_bot` 回源 Bot；不传 group 就不建群。源 transcript、webhook 和 person 事实都不拷过去。
- 网页建群并发言，每个成员的 inbox 都有这条，群 transcript 也有，正文里没有成员的私人记忆。
- Bot 之间互相转发 `LOOP` 会在跳数上限停下，成功投递不超过 3 次。

M4：

- 签过名的 webhook 唤醒 Bot，outbox 里有一条通知，正文被包成不可信事件。
- 没有签名或签名错误会被拒绝，inbox 里没有事件。
- 同一个 delivery id 只处理一次。
- 文件连续改动会防抖成一条，事件里有路径和种类，没有文件内容。
- 一个窗口里的多次 webhook 合成一条唤醒。

M3：

- 聊天持有处理锁时，`serve` 仍会投递到点的 routine，聊天里能看到通知，且只处理一次。
- 主 Bot spawn 一个还在跑的子代理时，仍能回答下一条聊天；完成后收到 done。
- 中途 steer 保留已有工作；interrupt 停掉当前步并用新指令重来；cancel 停掉且不再接受控制。
- 同一套控制经 runner 的 HTTP 协议走通。
- fork 的请求里能看到主上下文；brief 看不到。

M2：

- Bot A 记住的 person 事实，新建的 Bot B 能 recall；bot-person 事实别的人看不到。
- `serve` 里 routine 到点：模型选择通知时 outbox 有一条；选择 `SILENT` 时没有通知。
- 主人的终端输入不会包成 `<untrusted_event>`。
- 两次唤醒的稳定前缀字节相同；小时变化只追加一条很小的环境差异。
- `compact` 守住工具对、尾部和重叠；被压掉的句子还能用 `recall_history` 和 `expand` 找到；组装顺序是前缀、记忆、摘要、原文。
- provider 注册表只存环境变量名，Bot 配置只存绑定。

`scripts/e2e.sh` 用真实二进制跑通列目录、写入 `summary.txt`、再给出最终回复。

## 包

| 包 | 职责 |
| --- | --- |
| `cmd/xbot` | `new` / `list` / `chat` / `bind` / `serve` / `webhook` / `watch` / `runner` / `provider` |
| `web` | 登录、网页 API、SSE、嵌入的页面 |
| `peer` | 同 owner 的 Bot 消息、群、跳数和频率上限 |
| `inbound` | Webhook HTTP、合批、文件监听 |
| `proc` | 一个 Bot 的唤醒循环：投递和执行分开 |
| `exec` | 子代理循环、本机执行器、runner HTTP |
| `work` | 每个 Bot 的工作目录和共享目录 |
| `skill` | 每个 owner 的技能库，前缀里只有一行说明 |
| `provider` | OpenAI 兼容的流式 Chat Completions，以及 `ModelSelector` |
| `config` | 环境变量和 `providers.json` |
| `agent` | 模型与工具的循环 |
| `tools` | shell、读、写、精确替换 |
| `link` | Link、Event、终端、工具、timer、routine |
| `inbox` | 每个 Bot 的收件箱 |
| `notify` | 给主人的 outbox |
| `serve` | 后台轮询 |
| `bot` | Bot 目录、绑定、transcript、摘要块 |
| `kernel` | 一次唤醒：信任、环境差异、压缩、组装、调用 agent |
| `person` | 全局身份 |
| `memory` | 三层记忆、markdown 存储、BM25 |
| `compact` | 压缩接口和 LLM 摘要 |
| `cron` | 5 段 cron 和最小间隔 |
| `bm25` | 关键词排序 |

## 家里的电脑，手机调用

还没有自建中继。两条传输都只是把已经在跑的 `xbot serve` 公布出去，内核不变。

**Cloudflare。** 本机要有 `cloudflared`。serve 仍绑回环，隧道进程把这个地址公布成一个临时的 `trycloudflare.com` 主机名：

```bash
xbot serve --http 127.0.0.1:8787 --tunnel cloudflare
```

stderr 会多一行 `public url: https://<name>.trycloudflare.com`。手机浏览器打开这个地址，把同一段输出里的登录 token 贴进去。`cloudflared` 不在 `PATH` 上时设 `XBOT_CLOUDFLARED`。进程退出时隧道一起停。

**Tailscale。** 手机和这台电脑在同一个 Tailscale 网络里时，可以让手机直接打到家里的 `xbot serve`。

1. 在家用电脑上装好 Tailscale。
2. 回环地址会改绑到 `tailscale ip -4`。stderr 打印 `public url`。不要用 `--http 0.0.0.0:8787`：那个绑定不会被改写。

```bash
xbot serve --tunnel tailscale
```

也可以自己绑 tailnet 地址，效果一样：`xbot serve --http 100.x.y.z:8787`。`tailscale` 不在 `PATH` 上时设 `XBOT_TAILSCALE`。

3. `xbot webhook add ada github --public-url` 用上面的 `public url`。手机上的快捷指令或其他客户端把请求发到这个 URL，并带上 secret。
4. 网页也在这个地址上。手机浏览器打开 `public url`，把 serve 打印的登录 token 贴进去。token 只在这台机器的 0600 文件里，cookie 不会被别的站点带上。可以添加到主屏幕。
5. 默认 `--http` 是 `127.0.0.1:8787`，只有本机能访问。手机和电脑都要在这个 tailnet 上。
6. 每个 webhook 单独一个 secret。不签名或签名不对会 401。secret 留在 Bot 目录里，权限 0600。

## 另一台机器上的节点

`xbot node serve` 在这台机器上听 `127.0.0.1:18772`，第一次把 bearer token 打到 stdout，并写进 `node-token`（0600）。要给别的设备用，绑 tailnet 地址，不要绑 `0.0.0.0`：

```bash
xbot node serve --addr 100.x.y.z:18772
```

在放 Bot 的那台机器上登记，然后可以直接调 `info`：

```bash
xbot node add desk --url http://100.x.y.z:18772 --token TOKEN
xbot node list
xbot node call desk info
```

`list` 不打印 token。Bot 要先 `links install node`，再 `links load node`，才有 `node_list` 和 `node_call`。`info` 只问身份。`shell` 在那台机器上跑命令，先看它的机器开关。

## 机器开关

每台机器一份，记在这个数据根的 `machines.json`。没设置时是 `allow`。

```bash
xbot machine list
xbot machine set local ask
xbot machine set local off
xbot machine set desk allow
```

`allow`：本机 shell 跟以前一样，`date` 这类只读命令不确认。`ask`：每条 shell 都要主人点头。`off`：shell 直接拒绝。节点进程上的 `shell` 用的是那台机器的 `local`。`ask` 时，调用方要先让主人确认，命令行 `xbot node call desk shell ...` 算主人自己在跑。文件工具不看这个开关。桌面和浏览器也看这个开关。

## 桌面

需要本机有 `xdotool` 和 ImageMagick 的 `import`。装了 `xprintidle` 时，主人 5 秒内还在用电脑，点击、输入、滚动、按键会停下来；没有这个命令就分不出来，照常做。截图不受这个限制。

```bash
xbot machine set local allow
xbot desktop screenshot
xbot desktop click 100 200
xbot desktop type 'hello'
xbot desktop scroll -3
xbot desktop key Return
```

`off` 时这些命令直接失败。Bot 里要 `links install desktop`，再 `links load desktop`，才有 `desktop_screenshot`、`desktop_click`、`desktop_type`、`desktop_scroll`、`desktop_key`。截图写到 `~/.xbot/desktop/`。能用 shell 或 API 做的事不要用桌面。

## 浏览器

用本机 Chrome 的调试端口，配置目录单独放在 `~/.xbot/browser/profile`，不打开主人正在用的那一份。需要 Chrome 或 Chromium 在 PATH 里，或者设 `XBOT_CHROME`。

```bash
xbot machine set local allow
xbot browser open --headless https://example.com
xbot browser snapshot
xbot browser click 'a'
xbot browser stop
```

`open` 打出标题、地址和可见正文，Chrome 继续留着，所以后面的 `snapshot` 和 `click` 还能连上。`stop` 关掉这次拉起的进程。`off` 时直接失败。Bot 里要 `links install browser`，再 `links load browser`。能用 shell 或 API 做的事不要开浏览器。
