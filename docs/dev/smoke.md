# 旅程冒烟用例

2026-10-20 起草，给 [FOCUS.md](../FOCUS.md)「待验收」里的 6 条旅程配冒烟用例：网页对话、忙时插话、动手做事、记住事情、主动唤醒、Bot 之间协作。规矩按技能 `focus`：对着真入口跑，模型换成脚本化替身；用例由不写实现的一方写，实现方不能改期望；断言只对每条旅程的「怎样算对」，不测内部实现。这是开发态的设计；用例全部落地、旅程毕业后，用例路径写回各条旅程，再删掉本文。

## 30 秒版

测试先构建一次 `xbot` 二进制，每条用例起一个真的 `xbot serve`：独立数据目录、真端口、真登录，消息走真 HTTP 和 SSE。只有模型是假的：一个按暗号回固定文字和工具调用的替身，经 `XBOT_BASE_URL` 接进去。替身只凭请求里看得见的内容作答，所以「Bot 答出了某个标记」就证明内核把它送到了模型面前。时间类的断言用替身控节奏；主动唤醒要一个可拨的假时钟。6 条共用一个小框架，加 build tag `smoke` 单独跑，全部串行约 3～5 分钟。

## 框架

放在 `e2e/smoke/`，单独一个 package，和进程内的集成测试分开。

**harness.go：起进程、登录、发消息、读结果**

- `TestMain` 里 `go build -o <临时目录>/xbot ./cmd/xbot`，整包只构建一次。
- `Start(t, opts)`：每条用例一个新数据目录作 `--root`，`HOME` 指到它的上一级，不碰主人的 `~/.xbot`；失败时保留目录并打出路径。环境变量 `XBOT_BASE_URL=<替身>/v1`、`XBOT_API_KEY=smoke`、`XBOT_MODEL=smoke`、`XBOT_AUTO_UPGRADE=0`。命令 `xbot --root <dir> serve --http 127.0.0.1:0 --no-auto-upgrade`，`--interval` 用默认值，和主人实际用的一样。从 stderr 读 `listening on http://…` 和 `登录码（login token）: …` 两行，15 秒读不到就失败并打出 stderr。
- `Login()`：带 cookie jar 调 `POST /api/login`，再确认 `GET /api/bots` 是 200。
- `CreateBot(name, persona)`：`POST /api/bots`；人设里带 `BOTNAME=<名字>` 给替身认。
- `Send(bot, text)`：发消息并解析 SSE，记下发出时刻、每个 `delta` 的时刻和文字、`tool`、`done.text`、`error`。剧本里没有确认卡（[ADR 0019](../adr/0019-no-permission-cards-by-default.md) 默认不弹），流里出现 `approval` 事件就判失败。
- `Transcript`、`Panel`（`GET /api/bots/{name}`，含 `memory`、`agents`、`routines`）、`Notes`、`PeerMessages(a, b)`、`PatchBot`。
- `WaitFor(d, cond)`：轮询到条件成立，超时报出最后看到的内容。
- `Restart()`：SIGTERM，10 秒内必须退出，同一数据目录再起、再登录。
- `Chat(bot)`：起 `xbot --root <dir> chat <bot>`，stdin 用管道，收 stdout；`Say` 写一行，`Quit` 写 `exit`。
- `Stop()`：SIGTERM，超时再 kill。

**stub.go：模型替身**

- `httptest` 服务接 `/v1/chat/completions`，回 SSE；写法抄 `internal/mockopenai`。文字可以分段、段间可设间隔；首包固定延迟 300 毫秒，模拟真实模型。
- 按系统消息认调用方：以 `Summarize the conversation so it can continue` 开头的是压缩；子代理按它的固定开头认（**实现时核对**：预计是 `exec.SubPersona` 那句 `You are a subagent working for the main bot.`，没确认派活出来的子代理是否也用它）；带 `BOTNAME=` 的分清是哪个 Bot；其余是前台。认不出的调用（例如记忆抽取）一律回 `ok`。
- 看最后一条 user 消息里的暗号（`SMOKE_*`）走剧本，按这条消息之后已经调过哪些工具判断到第几步。
- 闸门 `Hold`/`Release`/`WaitEntered`，抄 `mockopenai.Script`。
- 记下每次请求的时刻、调用方、Bot 和原文，供断言用。

## 1. 网页对话

**替身**：`SMOKE_HELLO_<n>` 回三段「你好，」「这是第 n 次」「流式回复」，段间隔 200 毫秒；`SMOKE_FROM_TERMINAL` 回「终端你好」；`SMOKE_FROM_WEB_LATE` 回「网页后话」。

**步骤**：登录、建 `ada`；发 `SMOKE_HELLO_1`、`SMOKE_HELLO_2`；连读两次 transcript（相当于刷新）；起 `xbot chat ada`，终端说 `SMOKE_FROM_TERMINAL`；终端开着时网页发 `SMOKE_FROM_WEB_LATE`；终端退出。

**断言**：
- 两次发消息，从发出到第一个 `delta` 都 < 3 秒，日志打出实际毫秒数。
- 至少 3 个 `delta`，首尾间隔 ≥ 300 毫秒（边生成边送）；`done.text` 等于三段拼起来。
- 两次读到的 transcript 完全一样；两问两答各恰好一次，`seq` 递增，顺序是问 1、答 1、问 2、答 2。
- 终端说的话和回答出现在网页 transcript 里，排在答 2 之后。
- 终端打开之后网页发的话和回答，出现在终端 stdout 里。终端打开时不回放之前的历史，这里只测「打开之后两边都能看到」。

**测不了**：页面渲染（流式显示、刷新后不重复画）。仓库没有无头浏览器，这里只测接口给的数据。

## 2. 忙时插话

按「前台派活 → 子代理干活 → 前台汇报」。

**替身**：
- 前台收到 `SMOKE_LONG`：调 `dispatch {"type":"general","task":"SMOKE_LONG_TASK"}`，再回「已交给后台」。
- 子代理收到 `SMOKE_LONG_TASK`：挂在闸门 `long` 上，放行后回 `LONG_RESULT_42`。
- 前台收到子代理结果、含 `LONG_RESULT_42`：回「长任务完成：LONG_RESULT_42」。
- 前台收到 `SMOKE_ASIDE`：回「插话的回答」。
- 前台收到 `SMOKE_SLOW`：先送一段文字，再挂在闸门 `fg` 上；收到同时含 `SMOKE_SLOW` 和 `SMOKE_ASIDE_2` 的请求时回「先答插话」。

**步骤**：
- A（子代理在跑）：发 `SMOKE_LONG` 等到 `done`；等子代理挂上 `long`；读详情；发 `SMOKE_ASIDE`；再读详情；放行；最多等 60 秒到汇报出现。
- B（前台正在等模型）：发 `SMOKE_SLOW` 不等结束，等它挂上 `fg`，发 `SMOKE_ASIDE_2`。

**断言**：
- A：`SMOKE_ASIDE` 到第一个 `delta` < 3 秒；插话前后详情的 `agents` 里那一项都是 running；以 `SMOKE_LONG_TASK` 为任务的子代理只启动一次；transcript 里含 `LONG_RESULT_42` 的助手回复恰好一条。
- B：`SMOKE_ASIDE_2` 到第一个 `delta` < 3 秒；替身收到一次新的前台请求，里面两条主人消息都在；`SMOKE_SLOW` 那条流被正常收尾，不是挂到超时（[ADR 0017](../adr/0017-owner-words-go-straight-into-the-turn.md)）。

**测不了**：「任务」面板的样子，只测它读的 `agents` 数据。前台自己在跑慢工具时插话会超过 3 秒（ADR 0017 定了等工具返回），不测，记为已知缺口。

## 3. 动手做事

**替身**：
- 前台收到 `SMOKE_DO dir=<绝对路径>`：调 `dispatch {"type":"dev","task":"在 <路径> 列出文件，写 summary.txt，第一行 files:，后面每行一个文件名"}`，回「交给子代理了」。
- 子代理：`shell` 跑 `ls -1 <路径>`，再 `write_file` 写 `<路径>/summary.txt`，内容 `files:\n` 加 ls 输出，最后回「wrote <路径>/summary.txt」。
- 前台收到结果：回「我列出了目录里的文件，写好了 <路径>/summary.txt」。

目录由测试建在数据目录下，放 `a.txt`、`b.txt`，绝对路径写进消息，不依赖 Bot 默认的工作目录。

**断言**：
- 60 秒内 `summary.txt` 出现，内容逐字等于 `files:` 加 `ls -1` 的实际输出。
- 主人看到的最终回复含完整路径和 `summary.txt`。
- 主人看到的 `delta`、`done.text` 和 transcript 里助手正文都不含工具调用原文：没有 `{"command"`、`"path":`、`<untrusted_event`、`tool_calls`。
- 轨迹：主线 transcript 里这一轮有一条调 `dispatch` 的助手消息（派了活），子代理结果那一行含 `summary.txt` 的路径（子代理产出了文件）。

**测不了**：回复是否「说清楚」，属于措辞，靠人看。

## 4. 记住事情

**替身**：
- `SMOKE_REMEMBER 我对花生过敏`：从记忆快照的 `### Person — <路径>;` 标题找到主人记忆文件（抄 `mockopenai.memoryPath`），`write_file` 追加「- (日期) 主人对花生过敏 MARK_PEANUT_1」，回「记住了」。
- `SMOKE_RECALL 我有什么忌口`：把请求里所有含 `MARK_PEANUT_` 的行原样列出作答，没有就回「不知道」。
- `SMOKE_CORRECT 其实我不过敏了`：追加「- (日期) 主人已不再对花生过敏 MARK_PEANUT_2」，回「改好了」。

**步骤**：建 `ada`、`bob`；对 ada 说 REMEMBER；`Restart()`；问 ada；问 bob；读 ada 详情；对 ada 说 CORRECT；再问 ada。

**断言**：
- 重启后 ada 的回答含 `MARK_PEANUT_1`。
- bob 的回答含 `MARK_PEANUT_1`。
- ada 详情的 `memory` 里有一条 `scope` 为 `person`、含「花生」的条目。
- 更正后的请求里有 `MARK_PEANUT_2`，且排在 `MARK_PEANUT_1` 之后（或 1 已不在）；详情里能看到更正那条。

**测不了**：「回答以最新的说法为准」是模型读日期做判断，替身不会判断。这里只证明最新的说法送到了、排在后面。

## 5. 主动唤醒

**假时钟（最小改动）**：serve 的定时器循环、主循环和网页推送的免打扰判断都直接用 `time.Now()`，二进制层面没法拨钟。加一个 `clock.Now()`：平时等于 `time.Now()`；设了 `XBOT_FAKE_CLOCK_FILE` 时，返回真实时间加文件里写的偏移秒数，每次调用重读文件。替换 `cmd/xbot/cmd_serve.go` 定时器循环和主循环里的 `time.Now()`、内核 `now()` 的默认值、`internal/web/push.go` 的免打扰判断。测试改文件就能把钟往前拨。

**替身**：
- `SMOKE_ROUTINE <标签> <RFC3339>`：调 `routine_create {"name":"smoke-<标签>","prompt":"SMOKE_FIRE_<标签>","at":"<时间>"}`，回「定好了」。一次性 `at` 不受 cron 最短 5 分钟的限制。
- 例程事件含 `SMOKE_FIRE_NEWS`：调 `notify_owner {"text":"NOTE_NEWS"}`，回 `noted`。
- 含 `SMOKE_FIRE_QUIET`：回 `SILENT`。
- 错过汇总事件：回 `SILENT`，只记次数。

**步骤与断言**：
- **到点唤醒**（真实时钟）：定一个 30 秒后的 `NEWS`。transcript 出现一条以 `<routine` 开头的 user 行（界面的灰色时钟行由它画出），时间离 `at` 不超过 60 秒；`GET /api/notes` 里恰好一条 `NOTE_NEWS`。
- **没事就安静**：同样定一个 `QUIET`。时钟行出现，之后 30 秒通知不新增。
- **免打扰**：`PatchBot` 把 `quiet_start`、`quiet_end` 设成覆盖当前时刻，触发一次 `NEWS`：时钟行出现，通知不新增。假时钟拨过窗口结束后，通知里出现恰好一条以 `Quiet hours batch:` 开头的汇总。
- **重启不重跑，错过只报一次**：建 `*/5 * * * *` 的例程，触发一次后 `Stop()`，假时钟拨后 2 小时再 `Start()`。60 秒内，已触发过的时段没有第二次；错过汇总恰好一次；没有补跑 24 次。

**测不了**：时钟行和铃铛的样子，只测数据。webhook 入口这次不做。

## 6. Bot 之间协作

**替身**：
- ada 收到 `SMOKE_ASK_B`：调 `message_bot {"bot":"bob","text":"SMOKE_Q 你怎么看方案 X"}`，回「已经问 B 了」。
- bob 收到含 `SMOKE_Q` 的事件：调 `message_bot {"bot":"ada","text":"B_OPINION_77：建议先做 X"}`，回 `sent`。
- ada 收到含 `B_OPINION_77` 的事件：从请求里找到这句复述，「B 的意见是：…」。
- 上限：两边收到 `SMOKE_LOOP` 都给对方发 `SMOKE_LOOP`，永不停。

**步骤**：建 `ada`、`bob`，对 ada 说 `SMOKE_ASK_B`，最多等 60 秒到最终回答出现。另起一个干净的数据目录，对 ada 说 `SMOKE_LOOP`，等 90 秒。

**断言**：
- ada 的 transcript 里来自 bob 的事件行（`source="bot:bob"`）恰好一条。
- `GET /api/bots/ada/peers/bob/messages` 至少两条：`SMOKE_Q` 和 `B_OPINION_77`。
- ada 最后一条助手回复含 `B_OPINION_77`。
- 上限：两个 Bot 之间的消息总数 ≤ `peer.MaxHops`（3）；transcript 里出现 `hop limit`；最后 30 秒替身没有新请求。

**测不了**：「B 发来的内容只当资料」是模型行为，替身不会被说服；这里只确认 bob 的话以 bot 来源事件的形式进入 ada 的对话。

## 验收

- `go test -tags smoke ./e2e/smoke/` 6 条全绿，不加 tag 时 `go test ./...` 不跑它们。
- 每条用例的断言能逐条对上 FOCUS.md 那条旅程的「怎样算对」，测不了的行在用例注释里写明原因。
- 故意弄坏一处（例如替身首包延迟调到 4 秒、去掉一次 `Restart` 后的记忆），对应用例变红，报错说得出是哪一条「怎样算对」没过。
- 6 条的「冒烟用例」都从本文改成指向实际的测试文件和函数名。

## 还没做的

全部没做，下面是实现时的顺序和核对项：

- **框架**：`e2e/smoke/harness.go`、`stub.go`，build tag `smoke`。
- **6 条用例**，按网页对话 → 忙时插话 → 动手做事 → 记住事情 → 主动唤醒 → Bot 之间协作的顺序写。
- **假时钟**：`clock.Now()` 加 `XBOT_FAKE_CLOCK_FILE`，替换上面列的几处。不加的退路是停机时把数据目录里的 `timer-watermark` 改早，但那是改内部文件，不推荐。
- **核对子代理的识别点**：派活出来的子代理系统提示开头是不是 `exec.SubPersona`；不是就找一个稳定的开头，替身按它认。
- **核对默认 Link**：`POST /api/bots` 建出来的 Bot 是否带 web、peers、agents、timer，缺的在用例里补装。
- **免打扰汇总的放出时机**：拨钟之后什么时候放出，以实现为准，用例的等待时间按它定。
- **已知缺口，不在本文测**：前台跑慢工具时插话超过 3 秒；轨迹里看不到子代理用的工具；`xbot chat` 打开时不回放最近几轮。都在[候选清单](../CANDIDATES.md)。

## 相关

- 旅程与「怎样算对」：[FOCUS.md](../FOCUS.md) 的「待验收」
- 长程运行的冒烟和缓存口径：[dev/context.md](context.md)「验收：缓存怎么算」
- 前台派活：[dev/dispatch.md](dispatch.md)、[ADR 0016](../adr/0016-foreground-dispatches-typed-subagents.md)
- 插话与确认卡：[ADR 0017](../adr/0017-owner-words-go-straight-into-the-turn.md)、[ADR 0018](../adr/0018-cards-never-block-the-owner.md)、[ADR 0019](../adr/0019-no-permission-cards-by-default.md)
- HTTP 接口：[API.md](../API.md)
