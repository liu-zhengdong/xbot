# XBot

XBot 是一个从零写的个人助手（M3）。交互单位是 **Bot**：一个名字、一份 persona、一条只追加的对话。没有 session，也没有 `--resume`。

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
xbot serve
xbot runner --addr 127.0.0.1:18771
```

`chat` 接着这个 Bot 唯一的一条对话继续。聊天的时候，这个进程持有该 Bot 的处理锁，并在等待输入的同时处理 inbox 里的事件。`serve` 不管有没有人在聊，都会把到点的 routine 投进 inbox；只有拿到处理锁的那一个进程会执行它。退出输入 `exit`、`quit` 或 `:q`。

长时间的活用 `spawn` 交给子代理。本机默认是 goroutine。`xbot runner` 是同一套执行器的 HTTP 服务，只听环回地址，要用 token。`xbot chat --runner URL --runner-token TOKEN` 把 Bot 接到它上面。

执行 shell、`write_file`、`edit_file` 之前会问 `y/n`。改 persona、创建或修改 routine 也要确认。`--yes` 跳过询问。`serve` 里没有人点头，这些需要确认的工具会被拒绝。`notify_owner` 不需要确认，它是说给主人听的。

`--root` 放在子命令前面，默认是 `~/.xbot`。

## 配置

| 变量 | 含义 | 默认 |
| --- | --- | --- |
| `XBOT_API_KEY` | 未绑定时的 API key | 空 |
| `XBOT_BASE_URL` | 未绑定时的 OpenAI 兼容 base URL | `https://api.deepseek.com/v1` |
| `XBOT_MODEL` | 未绑定时的模型名 | `deepseek-chat` |

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
~/.xbot/people/<id>/identity.json
~/.xbot/people/<id>/memory/{profile,log}.md
~/.xbot/bots/<name>/config.json
~/.xbot/bots/<name>/persona.md
~/.xbot/bots/<name>/rules.md
~/.xbot/bots/<name>/transcript.jsonl
~/.xbot/bots/<name>/state.json
~/.xbot/bots/<name>/inbox.jsonl
~/.xbot/bots/<name>/outbox.jsonl
~/.xbot/bots/<name>/routines/<id>.json
~/.xbot/bots/<name>/memory/{profile,log}.md
~/.xbot/bots/<name>/people/<id>/memory/{profile,log}.md
~/.xbot/bots/<name>/agents/<id>/{transcript.jsonl,meta.json}
~/.xbot/work/<name>/
~/.xbot/work/shared/
~/.xbot/skills/<slug>/SKILL.md
```

Markdown 是给人改的。索引比 markdown 旧时会按 markdown 重建。

`xbot new` 默认授予 `terminal`、`tools`、`timer`、`memory`、`persona`、`context`、`agents`、`skills`，并把当前终端用户记成 owner。shell 和相对路径的文件工具以 `~/.xbot/work/<bot>/` 为工作目录，共享目录在 `XBOT_SHARED`。技能说明按 slug 排序写进稳定前缀，正文用 `read_skill` 再读。没有技能文件时，前缀和以前一样。

## 架构

内核只认识收件箱和工具。外面的一切都是 Link。

```text
terminal / timer ──Event──▶ inbox ──▶ kernel ──▶ ModelSelector ──▶ provider
tools / memory / persona / context ──tools──▶ agent loop
```

**信任。** 主人在终端 link 上的输入是指令，原文进 transcript。除此之外（别的人、timer、以后的外部 link）都包在 `<untrusted_event>` 里，当成数据。是不是主人由内核判断，不看事件自己带的标记。

**稳定前缀和环境快照。** 每次唤醒的 system 消息只有 persona、rules、以及按名字排序的 link 说明。工具 schema、时间、发送者、记忆都不在里面，这样上游的前缀缓存可以对得上。工具列表按名字排序，schema 变化不会把这段前缀打乱。

易变的部分是环境快照：小时、发送者、工具名、未读通知数、persona/rules/记忆的哈希。和上一次比，没变就不写；变了就在 transcript 末尾追加一条很短的 `<environment_update>`，只含差异。时间只在小时变化，或距离上次唤醒至少 30 分钟时才写。不改历史。persona 文件改完并得到主人确认后，下一次唤醒的前缀换成新文本，同时多一条 `persona: updated`。

**上下文预算。** 粗算按大约 4 字节约 1 token，分成 stable、memory、env、history 四段。总量超过预算（默认约 4.8 万，或 context 的四分之三；绑定的模型 context 会用来算这个预算）就压缩。Profile 记忆注入最多 4 KiB。

**压缩。** transcript 只追加，序列号就是行号，不会改。摘要是 state 里的块，每块记下它覆盖的 `[from, to]`。更大的块盖住里面的小块。模型看到的顺序是：稳定前缀、当前 profile 记忆、摘要块、还没被摘要盖住的原文。

模型可以调用 `compact(from, to, focus)` 自己选一段已经结束的范围。内核会拒绝：切开未完成的工具调用、碰到最后 N 条原文、和已有摘要块部分重叠。完整包住旧块是允许的，那就是一层更粗的摘要。预算不够时，内核仍会自动压最早的一段。压缩前会先把能留下的事实抽进记忆。压完如果尾巴里已经没有环境消息，就再追加一条完整的 `<environment_snapshot>`，避免状态被摘要吃掉。

**记忆。** 三个范围，两档：

| 范围 | 谁看得见 | 档 |
| --- | --- | --- |
| person | 所有 Bot | profile 每次注入；log 只在 `recall` 时搜 |
| bot | 这个 Bot | 同上 |
| bot-person | 这个 Bot 和这个人 | 同上 |

冲突规则不变：关于人的事实，新的赢；怎么干活，bot 范围赢。`remember` / `forget` / `recall` 是工具。检索是 BM25，没有向量库。Person 事实必须带上写下它的 Bot。来自不可信事件的 person 事实会在正文里标成 untrusted。单条事实最多 1 KiB，profile 文件 16 KiB，log 文件 64 KiB。

**历史。** `recall_history(query)` 在原始 transcript 上做 BM25，带回序列号。`expand(from, to)` 把某一段原文拉回来，输出有上限。摘要掉的句子还能这样找到。

**定时。** routine 是一段 prompt 加 5 段 cron，最小间隔 5 分钟。`xbot serve` 约每 15 秒看一次。到点就往 inbox 投一条事件，id 是 `rt_<id>_<分钟>`，同一分钟不会投两次。唤醒之后，值得说就 `notify_owner`（写入 outbox，并尽量 `notify-send`）；不值得说就只回复 `SILENT`，不发通知。`xbot chat` 会先把未读通知打出来。

**子代理。** 主 Bot 自己做短活。长任务调用 `spawn(task, mode)`。`brief` 只有任务说明；`fork` 带上当前主上下文的一份拷贝。子代理有自己的只追加 transcript，步与步之间看自己的 inbox。主 Bot 可以 `steer`（下一步之前插入，已完成的工作保留）、`queue`（当前这次跑完再做）、`interrupt`（停掉当前这一步，用新指令重来，步数预算重置）、`cancel`（停掉，排队的也不做）。子代理把 progress、blocked、done、failed 投回主 inbox。这些事件不是终端输入，内核会包成 `<untrusted_event>`。`read_subagent` / `expand_subagent` 读它的状态和原文，输出有上限。同时最多 4 个，默认最多 16 步、10 分钟。子代理没有 spawn，也不会在后台弹 y/n：`xbot chat --yes` 才自动批准它的 shell；否则需要确认的工具直接拒绝。

**执行器。** `Executor` 有本机 goroutine 实现，也有 HTTP 实现：`POST /v1/start`、`POST /v1/message`、`GET /v1/events`。`xbot runner` 只绑定环回地址，Bearer token 用常量时间比较。请求体里的模型密钥只在这条本机连接上出现。

**并发。** 每个 Bot 只有一个进程持有 `handle.lock` 并调用 Handle。`serve` 拿不到锁时仍然投递 routine，聊天中的进程会收到并处理，同一条事件不会被两边各跑一次。routine 的事件 id 和 inbox 去重兜住重复投递。锁在进程退出时放开。子代理在别的 goroutine 里跑，完成时只是往主 inbox 投事件，不自己调用 Handle。

## 安全

- **shell**：默认 30 秒超时，最长 120 秒。超时杀掉整个进程组。输出默认最多 32 KiB。命令最长 16 KiB。stdin 接到空设备。
- **read_file**：`offset` 从 1 起，`limit` 默认 200 行、最多 2000 行，输出默认最多 32 KiB。
- **write_file / edit_file**：路径先变成绝对路径，并解析已存在前缀上的符号链接。`write_file` 正文最多 1 MiB。`edit_file` 要求 `old_string` 恰好出现一次，文件本身最多 2 MiB。
- 工具结果进 transcript 之前会收成合法 UTF-8，并再卡一道 64 KiB。
- 没有路径沙箱。写和执行靠确认。
- 数据目录 `0700`，配置和身份文件 `0600`。JSONL 追加时加文件锁。
- cron 最短 5 分钟。`* * * * *` 会被拒绝。
- 密钥不进 Bot 目录，也不进 `providers.json`。
- 子代理默认最多 4 个并发、16 步、10 分钟。进度事件默认两秒一条。
- 子代理的输出是不可信数据，不进 system，读出来时也会标明。
- `xbot runner` 拒绝非环回地址。token 为空时会生成一个并打印。空 token 不能连上。

## 测试

```bash
go test ./...
bash scripts/e2e.sh
```

`go test` 覆盖这些 M2 行为，以及 M3：

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
| `cmd/xbot` | `new` / `list` / `chat` / `bind` / `serve` / `runner` / `provider` |
| `proc` | 一个 Bot 的唤醒循环：投递和执行分开 |
| `exec` | 子代理循环、本机执行器、runner HTTP |
| `work` | 每个 Bot 的工作目录和共享目录 |
| `skill` | 共享技能，前缀里只有说明 |
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
