# XBot 里程碑与进度

> 进度天天变，所以单独放在这里；现行设计见 [atlas](atlas/README.md)。每日测试报告不进仓库。

## 总表
| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 核心循环、Link、Event、收件箱、人、记忆接口 | 已完成 `a32d242` |
| M2 | 记忆、定时任务、`serve`、环境快照差异、摘要块、按需找回、供应商 | 已完成 `233aaf7` |
| M3 | 单一事件处理循环、子代理、执行者、工作区、技能 | 已完成 `9d233fa` |
| M4 | Webhook、文件监听、合并与限流 | 已完成 `304fa00` |
| M5 | Web 界面、PWA、Bot 之间通信、群 | 已完成 `caa53a1`；UI 精修中 |
| M5.x | 后台异步压缩、Fork Bot、主 Bot 与置顶、Bot 档案 | 已完成 `db6487e` `4b07ec4` `5a96e75`。网页接口见 [API.md](API.md)，静态界面由单独的代理维护 |
| M6 | 手机后台推送、远程访问（Cloudflare Tunnel 等）、多节点、人的记忆同步、机器开关、桌面操作、浏览器原生 Link、MCP 适配器与连接器目录、编码代理 Link | 手机推送、隧道（公网可达性未测）、多节点、记忆同步、机器开关、桌面、浏览器：已验证。精选目录、Telegram/飞书、MCP（stdio 与 Streamable HTTP，按 Bot 启用，密钥只记名字）、编码代理（确认后启动，输出进对话卡片）：已接上。托管市场未做 |
| M7 | 凭据代理与保险箱、危险操作审核 | 盒子回归（假模型，2026-10-16）。已验证：保险箱 CLI、落盘加密与权限、只向绑定域名注入、跨域跳转不带真值、工具结果/transcript/模型输入里脱敏；删除、外发、带密钥或写方法的 HTTP、`keys:true` 都出确认卡，拒绝后不执行；只读不问；`decisions.jsonl` 留痕。修复：强制 `git push` 不确认；密钥挪到 `<root>/keys/vault/<bot>.key`（旧数据打开时迁移），触及保险箱/密钥路径的 `shell`/文件工具无论机器开关都出确认卡。独立用户守护进程 `xbot vaultd`（[ADR 0005](adr/0005-vault-daemon-user.md)）已在盒子上以 `xbot-vault` 用户端到端验证：迁移后 Bot 读不到密钥和密文，注入、拒绝未绑定域名、跨域跳转不带真值照常；其他 uid 连接被 SO_PEERCRED 拒绝；小请求额外开销 p95 0.22 毫秒（300 次）。遗留：Bot 所在用户若有免密 sudo，隔离形同虚设 |
| M8 | 开源网关、P2P、Bot 地址、跨用户通信、模板分享 | 计划中 |


全局验收：每个里程碑都要报性能数字（交互回复 p95、IPC 开销 p95、后台变慢比例、链路数据量；涉及界面的报帧率），预算见 [AGENTS.md](../AGENTS.md) 的「全局要求：性能与界面流畅」。

## 8 步内核计划（2026-10-17 定）

来自 [Claude Code 内部机制调研](research/claude-code-internals.md)，主人批准。文件检查点和回滚、hooks、规划模式不做（见 [候选清单](CANDIDATES.md)）。这里的 M1–M8 是这份计划的编号，和上面总表的里程碑不是一回事。

| 步 | 内容 | 状态 |
|---|---|---|
| M1 | 提醒通道，熔断与打转检测 | 已合入 |
| M2 | 文件新鲜度 | 已合入 |
| M3 | 待办清单 | 已合入 |
| M4 | shell 后台化 | 已合入 |
| M5 | 环境感知（AGENTS.md 分层加载、持久 cd、token 用量、外部改动 diff） | 待评审 |
| M6 | 搜索与看图 | 待做 |
| M7 | 子代理类型（工具白名单、提示片段、步数上限、防递归） | 待做 |
| M8 | 权限规则（deny → ask → allow，「不再询问」） | 待做 |

两周验收：三个 Bot 从 2026-10-16 连续运行到 2026-10-30。

## 提醒通道与熔断（研究笔记里的 M1）

相对 v0.3.0。提醒只追加在最新一条消息末尾，sidecar 为 `reminders.jsonl`，重放不改历史。已接上待确认卡（10 分钟冷却，定时唤醒不带）、打转警告、后台完成；`todo` 与文件改动走同一个注册口。熔断六项：重复输出、同参工具、文件来回改、同一失败轮询、收益递减、连续 3 次确认被拒。收益递减只统计后台因步数上限开的续段（开口那段不算），连续 3 段且最近两段助手新增各少于 500 token 先提醒，下一段硬停；普通短工具步不计入。第一次提醒，第二次硬停并写 `<stop_reason>` 与 `trace.jsonl`。中断、取消、换模型的悬空工具调用收成 `DanglingResult`。常驻规则里的「失败最多再试一次」改到轮询提醒上。DeepSeek 模型提示文件未改。

假模型回归：`go test ./...` 连续三遍。本机基准（`go test -bench`，amd64）：`BenchmarkReminderBind` 287 ns/op，`BenchmarkBreakerObserve` 696 ns/op。一步熔断观察远低于 IPC 5 毫秒预算；不涉及界面帧率。未跑真模型评测。

评审后收紧：同参/轮询只数连续且按命令分计，成功清掉失败计数，只读和 `go test`/`build`/`vet` 放宽；提醒脱敏后落盘，绑到刚写下的序号，段与段之间按序号贴回而不是粘在最新一条；`reminders.jsonl` 缓存并压掉活窗口之外的旧行。缓存只在文件大小和 mtime 都没变时复用，另一个进程写过之后重新装入。

## 文件新鲜度（研究笔记里的 M2）

相对提醒通道。每个 Bot 的活内核记下本进程读过和自己写过的文件（路径、mtime、大小、哈希，最多 64 个，哈希按流计算、最多前 4 MiB）。已经存在但本会话没读过的文件，`edit_file` / `write_file` 报错让先读；大小或哈希对不上就拒绝，并说明自第 N 步之后文件变了。只有 mtime 变、哈希没变，允许写。大小和 mtime 都相同且不是截断哈希时跳过内容哈希，同样大小、同样 mtime 的替换会当成没变。自己写成功后刷新记录，结果里写明不必再读。`write_file` 可以把记录标成完整；`edit_file` 保持改之前的完整或局部。同一轮里没变的完整重读返回短回执，除非要了行范围或 `force`；下一轮重新读正文。外部改动用 `file_change` 提醒，带短 diff，冷却 1 分钟，同一次变化不重复；`.env`、`*.key`、`*.pem`、credentials 一类文件只写 changed。被拒绝的调用在这一轮里不能用相同参数再试，下一轮可以再问。

假模型回归：`go test -count=1 ./...` 连续三遍；`go test -race ./internal/kernel/...`。基准（本机 amd64）：`BenchmarkFileFreshGuard` 7076 ns/op、3272 B/op，未变文件不重新哈希；`BenchmarkSnapshotSmall` 2984 ns/op、576 B/op（6 字节文件，原先按 4 MiB 分配）。都远低于 5 毫秒。不涉及界面帧率。未跑真模型评测。评测脚本里改已有的 `site.css` / `view.html` 之前先读一遍。内核测试把 `TMPDIR` 指到仓库里的 `.test-tmp/kernel/p<pid>`，每个测试进程一份，避免并行的 `go test` 互相删掉临时目录。

## 待办清单（研究笔记里的 todo，本轮称为 M3）

相对提醒通道那一版。工具 `todo` 写一份短清单：content，状态 pending / in_progress / done，放弃用 dropped 加原因，同时最多一项 in_progress。存在 `bots/<name>/todos.json`（0600），一个 Bot 一份。状态接口多一行 `todo` 摘要；全文是 `GET /api/bots/{name}/todos`。步骤环和网页侧边栏不画它。

提醒走已有的 `todo` 种类。活动清单在，并且距上次成功更新已经过 N 步（默认 6，模型 `todo_steps` 或内核 `TodoSteps`）才发；只发相对上次提醒的差异；冷却 2 分钟；刚更新的那一步不发。步数记在内存里，开关关掉时不碰文件，只是计数也不重写。模型配置 `todo` 显式开关，缺省时名字里带 deepseek 的才开（空绑定也算）。除 `links`/`todo` 外全是只读工具的一轮不发。定时唤醒不发待办提醒，也不跑完成门。清单跨回合保留：主人说继续，或前台插话时后台还在跑，都不清掉。每一项都已 done/dropped、写入空清单、或新清单和旧清单没有相同 id 时，旧清单追加进 `todos-archive.jsonl`，活动清单清空。完成门只在这一轮写过或改过清单、模型又要在还有未完成项时停下时，另起一条用户消息注入一次；最后一句是在向主人提问，或还有待确认卡时，不注入。同一轮只一次。步数上限和熔断照旧，不会为了清单一直续。

假模型回归：`TMPDIR` 在仓库内，`go test -count=1 ./...` 连续三遍，`go test -race ./internal/kernel/...`。本机基准（`go test -bench=BenchmarkTodoDiff -benchmem -benchtime=200ms`，amd64）：3569 ns/op，5808 B/op。远低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测，未改网页步骤弹层。

## shell 后台（研究笔记里的 shell 后台化，本轮称为 M4）

相对待办清单那一版。`shell` 默认超时 120 秒，最长 600 秒，模型可传 `timeout_seconds`。到点不杀进程，转成后台，回一条短结果：任务 id 和输出文件路径。`run_in_background` 立刻转后台。输出写在 `bots/<name>/shell-jobs/<id>.log`（目录 0700，文件 0600）。原始字节先留在内存里。最后一段至少 1024 字节、也至少比已知最长密钥形式多 1 字节，这段不进文件、预览或 `shell_output`。每次写入按替换区间把原文偏移映射到脱敏结果，只提交窗口之前已经确定的那段。结束时把剩下的全部脱敏后写入。文件超过 256 KiB 再丢掉头部。已提交的流长度只增不减。`shell_output` 的 offset 按整段流计算，丢掉的前缀会写明。`shell_output`、`shell_status`、`shell_cancel`（杀进程组）在权限表里分别是只读、只读、普通，只有挂了任务表的 shell 才出现。同一 serve 进程里跨回合还在。重启把对不上 `proc_start` 的 running 标成 lost，不向旧 pid 发信号；另一个进程如果对上了仍活着的 pid 和 `proc_start`，就不改这条记录。完成提醒或叫醒会把 `notified` 写进任务记录，同一次完成不会又提醒又叫醒。只有本进程结束的任务才往收件箱投事件。lost 最多提醒一次。结束的任务只留最近 20 个、且不超过 7 天。`Close` 杀掉本进程拥有的进程组。完成提醒走已有的提醒通道，种类 `shell_job`，贴在下一步工具返回之后，不打断正在跑的调用。Bot 空闲时往收件箱投一条来源 `shell` 的不可信事件叫醒它，不另开 `bg_` 段，熔断照旧。`sleep`、`watch` 和带 `sleep` 的 `while`/`until` 不自动转后台，交给熔断；显式 `run_in_background` 仍可把 `sleep` 放后台。评测沙箱仍是 30/120 秒并在超时杀掉，工具名单不含 `shell_output` / `shell_status` / `shell_cancel`。没挂任务表的 shell（节点、命令行）也是到点杀掉。

假模型回归：`TMPDIR=/workspace/.test-tmp` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race` 覆盖 `internal/kernel`、`internal/tools`、`internal/eval`、`internal/review`。未提交的尾部不落盘。提交点按替换区间从原文偏移映射过来。`TestJobLogRedactionProperty` 做 10000 次随机切块。本机基准（amd64，30 次）：`BenchmarkJobWriterTrickle` 1492 ns/op，3712 B/op，3 allocs/op；`BenchmarkJobWriterBulk` 106015 ns/op，179063 B/op，3 allocs/op（一次约 32 KiB）。20k 行按这样一块写，写端大约 4 ms。单块低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测，未改网页。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`，`go test -fuzz`（目标在，正常测试只跑种子）。

## shell 输出少留（M4 跟进）

运行中的任务不再固定留住最后 1024 字节。待写尾巴里只留最长的、正好是某个已知密钥形式（原文、JSON、URL、base64，以及暂存密钥原文）前缀的那一段；对不上时，完整脱敏过的输出立刻进文件和 `shell_output`。还留着的字节数在 `shell_output` 和 `shell_status` 里写成 `held_bytes: N`（大于 0 才写）。`scrubSecrets` 按从长到短替换，一样长按字节序。测试里的 PEM 头尾在运行时拼接，扫描器不会把假私钥当成真的。

假模型回归：`TMPDIR=/workspace/.test-tmp` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race` 覆盖 `internal/kernel`、`internal/tools`、`internal/eval`、`internal/review`。`go test -fuzz=FuzzJobLogRedaction -fuzztime=2m ./internal/tools/`。本机基准（amd64，`-benchtime=200ms`，脱敏函数同时认一个短 token 和约 1.7 KiB 的 PEM 形式）：`BenchmarkJobWriterTrickle` 4249 ns/op，432 B/op，3 allocs/op；`BenchmarkJobWriterBulk` 93232 ns/op，253532 B/op，3 allocs/op（一次约 32 KiB）。单次写入远低于本机 IPC 5 毫秒预算。2 万行按 32 KiB 一块写，写端大约 4 毫秒。不涉及界面帧率。在 `a9c1ffa` 上会失败的回归：`TestShortRunningOutputIsVisible`、`TestShellTimeoutMovesToBackground`、`TestBackgroundFileRedactsSecret`、`TestShellJobFileUsesKernelRedactor`、`TestRunningShortOutputAndHeldPrefix`、`TestScrubSecretsLongestFirst`、`TestScrubSecretsStableTieBreak`。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`。

## 环境感知（研究笔记里的 M5）

相对 shell 后台那一版。四件事都走已有的提醒通道，不进系统提示，也不新加工具。

项目说明：Bot 的工作目录（没设工作区时不读进程当前目录）向上读到工作区或共享目录根。每层有 `AGENTS.md` 就用它，否则用 `CLAUDE.md`，近的在后。同一套只注入一次，`cd` 进了另一个子目录才再发。合计最多 32 KiB，更远的文件先丢掉，截断时正文写 `[truncated]`。文件里的 `</project_instructions>` 会被拆开，避免它把提醒标签提前关掉。允许的根以外的路径不读，指到外面的符号链接也不跟。正文是主人写的数据：权限表不看它，写入提醒前仍过脱敏。

持久工作目录：同一个 Bot 的前台 `shell` 里，`cd` 留到下一次调用，记在内存和 `bots/<name>/cwd`。每次调用确认目录还在、并且在工作区或共享目录里；丢了或不在里面就回到工作区，结果里写明。结果在状态行后面加 `cwd:`。`false` 和显式 `exit` 也留下目录。后台任务只记下启动时的目录，不改这份会话目录。没挂会话的 shell（评测沙箱、节点、命令行）每次仍从自己的目录开始。查项目说明或做权限判断时不把回退写进这份记录，免得把「目录丢了」的说明吞掉。

用量：流式请求带 `include_usage`。有 usage 对象就按原数记 prompt、completion、cached，0 也算报告过，不用估计值替换。没有对象才按大约 4 字一 token 估计，trace 的 reason 是 estimated，提醒里写 estimated。prompt 跨过该模型 `providers.json` 上下文窗口的 50/75/90% 时，提醒通道发一行；同一档不重复，掉下去再跨过才再发。没有窗口长度就不发百分比，计数仍进 `trace.jsonl` 和 `BotStatus.tokens`。本进程累计，重启从 0 开始，历史在 trace 里。

外部改动：`file_change` 改成不超过 4 KiB 的统一 diff，放不下写 `[diff truncated]`。敏感文件名仍只写 changed。自己 `write_file` / `edit_file` 成功后刷新记录，冷却过了也不再报。

提醒通道这一轮多出来的 token（`compact.Estimate`，大约 4 字一 token，不是供应商分词器）：一份大约 3094 字节的 `AGENTS.md` 第一次注入 3393 字节、849 token；下一轮新注入 0 字节，历史里仍带着那 849 token，直到压缩。再 `cd` 进一个大约 1 KiB 的子项目说明后，新的一套是 4538 字节、1135 token。50% 那一行 133 字节、34 token，只有跨过门槛才发。一小段外部 diff 大约 34 字节。读这一套项目说明的平均耗时 64 微秒；基准 `BenchmarkProjectText` 53243 ns/op，43668 B/op，249 allocs/op（amd64，200ms）。远低于本机 IPC 5 毫秒预算。不涉及界面帧率。

假模型回归：`TMPDIR=/workspace/.test-tmp/full` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`。供应商超时测试里的重试计数原来没有同步，两个请求叠在一起时 `-race` 会报，改成原子计数。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`，`go test -fuzz`。

## 下个版本（v0.3.1 / v0.4.0）
v0.3.0 只发内核修复。下面三项提示类改动做完、过了 review，但都拉低了 DeepSeek 的 give-up 用例（向一个死端点 POST，应当如实报告做不到，最多 4 次工具调用），撤到下个版本重做，每项配自己的 eval 用例，不靠 give-up 调。

- **模糊任务规则**（「一两句计划后同轮开工」）。加上后全量 eval 里 give-up 连续 0/3、1/3、1/3，三轮改措辞没稳住：模型在目标已写明的任务前先翻目录、补细节，超出工具上限。
- **知识问题三档规则**，连同 knowledge-public / knowledge-project / knowledge-fresh 三条用例和 eval 的 `no_tool_calls` 检查。
- **交互回复默认低推理强度**（profile `reasoning_effort`）。

give-up 单测数据（每个构建 6 次，2026-10-17）：7e64ec6 6/6；加上内核修复、没有知识规则 6/6；再加知识规则 5/6；再加低推理强度 4/6。拿掉模糊任务规则后的全量 eval 两遍都只有 give-up 1/3 不过。证据：`/workspace/xbot-test/weekly/v030/`（evalB/D/E/G/H、gu-*.log）。

另：第 9 项（中文回复前的英文开场白；`xbot provider add/list/remove`；`xbot new` 拒绝未知供应商）也留到下个版本。

## 插话时分叉（F1–F5）
设计见 [插话时分叉](atlas/fork.md)。
| 切片 | 内容 |
|---|---|
| F1 | transcript 层：`branch` 字段、`fork`/`fork_end` 标记行、并发安全的 seq、界面历史和导出的重放规则（先做最简显示，不做卡片），旧数据照常读 |
| F2 | 分叉内核：按轮状态拆进 turn 结构，插话时把正在跑的一轮登记成 `bg_` 执行器；可以同时有多个后台，前台提示列出全部在跑的后台；完成事件回收件箱 |
| F3 | 连续插话与资源冲突：前台硬上限，到上限就转成新的后台；每个后台登记自己要碰的资源（文件、仓库、工作目录），和在跑的后台重叠才排队，否则并行；上限 3 个；权限表加「前台写操作交给后台」 |
| F4 | 取消与崩溃恢复：`cancel`/`interrupt`、重启补写 `interrupted` |
| F5 | 网页后台任务卡：审美顾问出原型，政东定稿后实现；按第 8 节验收 |

| 切片 | 状态 |
|---|---|
| F1 | 已完成；遗留项（崩溃修复的占位结果带上所属后台的 branch）已在 F2 分支补上 |
| F2 | 进行中。A：按轮状态挪进 `turn` 结构（完成）。B：内核分叉机制（完成，`Kernel.AllowFork` 开关默认关）：主人插话时把正在跑的一轮登记成 `bg_` 后台（最多 3 个），写 `fork` / `fork_end` 标记和 `agents/<id>/meta.json`，前台提示列出全部在跑的后台，结束时往收件箱投 `fork_end` 事件；崩溃修复能处理多个分支各自末尾的未答调用。C：`serve` 的网页路径已打开分叉（完成）：每个 Bot 一个长期内核，进程内的请求共享处理锁，锁一直拿到所有后台结束；插话作为前台走 SSE。盒子回归：连发 5 句插话，响应 0.01–0.12 秒；后台跑完写 `fork_end`，完成事件由 `serve` 处理并写进通知。F2 完成，遗留见下 |
| F3 | 完成（除前台硬上限，见下）：后台确认走卡片给主人（`BackgroundApprove`）；后台 `steer`/`queue`/`interrupt`/`cancel`，控制消息有 `pending`/`applied` 状态，下一步生效；资源冲突排队（同文件、同目录、同仓库的前台写操作排到对应后台之后，其余并行，后台上限 3）；队列上限 5，排队项带原因和位置、可取消，满了回「队列已满」；后台在跑时网页进程自己处理收件箱里的定时器等事件；删除或改名 Bot 时清理锁记录并取消后台；`GET /backgrounds` 给 F5 的角标和弹层用。盒子回归（假模型）：两个后台并行、插话 0.03 秒回复、steer 下一步生效、冲突的写操作排到对应后台之后、另一个后台的 `fork_end` 在后台还在跑时被处理，两个后台都以 done 结束 |
| F4 | 完成：前台硬上限（4 次工具或 20 秒，可配置；到了就转成后台，槽位满时留在前台）；重启时把有 `fork` 没 `fork_end` 的后台补成 `interrupted`，更新 `meta.json`，只告诉主人一次，不自动续跑；取消路径写 `cancelled`、杀掉正在跑的命令。盒子回归（假模型）：4 次工具后转后台并发提示；20 秒转后台后 `kill -9`，重启后补写 interrupted、事件只投一次、再重启不重复；取消后状态 cancelled、`sleep` 进程已结束 |
| F5 | 完成：按原型做了顶栏「后台 N」角标（只数运行中，没有运行和排队时隐藏）和弹层：运行中（步数、最近工具、追加指示、取消，steer 显示待生效/已生效/已丢弃）、排队（原因、文件名、位置，可取消）、最近结束 3 个；手机是底部弹层。聊天里没有后台卡片，后台的回复和确认卡带「后台」来源标，控制消息和 `fork_end` 事件不显示；插话被拒（429）时显示「队列已满，请稍后再发。」。截图在 `xbot-test/f5/shots/`。验收（假模型）：5 次插话延迟最多 0.02 秒；后台耗时 16.2 秒，基线 16.1 秒（+0.6%）；2 个后台并行加 5 次插话，各 +0.6% 和 +1.9% |

F3/F4 遗留：前台不限制只读工具和单次 10 秒；槽位满时到上限的一轮留在前台；后台之间不按资源互相排队；只读 shell 的判断是一张短名单。

验收：连发 5 句每句 p95 3 秒内回复，后台耗时增加不超过 10%，并测 2 个后台并行。

## M6 切片状态
M6 每片的状态。已验证的是回归对过的；已接上的是代码里有、还没在回归里点过的。托管连接器市场不做。

1. **精选目录和安装**。已接上。一份内置清单，每条标明类型（原生或 MCP）、能力（工具 / 入站事件）、需要的密钥名。已经有运行时的条目可以装到某个 Bot 上，就是给它加上对应的 Link 授权；还没有运行时的条目照样列出来，安装会被拒绝。默认不因为这份清单多装任何东西。
2. **MCP 适配器**。已接上。stdio 与 Streamable HTTP 都把服务器包成这个 Bot 的工具。服务器记在该 Bot 的 `mcp.json`，密钥只记环境变量名，供应商 API key 不传。入站事件可选。不从精选目录一键安装。`xbot connector list/add/remove` 用打进二进制的 JSON 目录，按 Bot 启用，没有托管市场。
3. **连接器**。已接上。Telegram、飞书装上之后，外面的消息变成不可信 Event 进收件箱。代表主人发出去的消息先留草稿，主人确认后才发出。密钥只在确认卡或同名环境变量里填写。
4. **编码代理任务**。已接上。和子代理分开的任务记录：工作目录、仓库、步数上限、时间上限。可以开始、补充、排队、打断、取消。完成、失败或取消时向该 Bot 的收件箱投 Event。
5. **编码代理后端**。已接上。同一份任务记录接到外部命令、`xbot runner` 或本机模型，内核不加新对象。`code_start` 会改文件，但按权限表属于普通动作，不弹确认卡。`XBOT_CODE_AGENT` 优先，否则用 PATH 里的 `cursor-agent`、`codex` 或 `claude`。输出按行流进 transcript 的编码卡片。`code_show` 能看已经流出来的输出，`code_cancel` 停掉进程。仓库非空且目录里还没有 `.git` 时，先 `git clone`。都没有后端时仍只写记录。
6. **手机后台推送**。已验证。每个 owner 一份 VAPID Web Push 订阅，写在数据根的 `push/subs/<owner>.json`（0600），密钥在 `push/vapid.json`（0600）。网页「连接应用」里开启或关掉。订阅地址是公网 https；环回上的 http 只留给本机测试。内网、环回、链路本地、运营商级 NAT 和未指定地址拒绝。主机名解析失败就拒绝，不放行。十进制或十六进制整数主机（例如 `2130706433`）拒绝。地址还要是全球单播，并排除 IANA 专用段，包括运营商级 NAT、基准测试 `198.18.0.0/15`、文档和保留地址，以及 NAT64、6to4 和 Teredo。真正拨号时再用套接字 Control 核对解析出来的地址，避免检查之后被重新绑定到内网。浏览器页面还连着（正在拉接口，或有一条没结束的对话）时，普通通知仍走页面里原来的提示，不发 Web Push。没有标签页连着，并且这条 note 已经写进出件箱时，才按订阅推到手机。免打扰时段不推：时段内 note 只进 held，不写出件箱；即便出件箱里已经有一条，只要这时还在免打扰，后台推送也停。时段结束后合成的那一条写进出件箱，记录上带批量标记，不靠正文开头那几个字来认。页面拉得到。只有页面正有一条没结束的请求时才不推送；仅仅是最近 12 秒有过接口调用时，这条批量通知仍推到手机，避免两边都没人收。页面里的通知列表和提示保持原样。
7. **人的记忆同步**。已验证。人的身份追加在 `people/<id>/identity.jsonl`，人的记忆追加在 `people/<id>/memory/sync.jsonl`。每条记下时间、写下的 Bot、设备。设备用数据根里已有的 `node-id`，没有就记 `local`。`xbot node add` 登记的节点之间自动同步：`xbot serve` 和 `xbot node serve` 每隔 30 秒，以及本机写入或从别的节点合并进新内容之后，用该节点的令牌调用对方的 `sync` 能力，把两份日志换过来再合并。同一条以时间戳较新的为准；时间相同留本机已有的。晚于现在两分钟的时间戳合并时改到这个上限。不合格式的条目（层级、控制字符、长度）丢掉。同步来的账号留在日志里，不授予本机主人身份；主人账号、显示名和时区只由本机操作更换。`xbot person unlink [--force] <link:id>` 去掉一个已关联账号。最后一个本机 `terminal:uid:` 账号要加 `--force`，并先打出警告。记忆放进模型上下文时包在闭合的 memory 块里。不可信记忆的正文、键和设备名里的 `<`、`>` 以及全角和形近尖括号一律转义，块内不能再出现标签。从别的节点来的事实按不可信数据处理，并保留写下的人和设备。只有送达这条记录的已认证对端自己就是所声称的设备，并且该设备已在本机登记，才记成 owner-recorded，仍然是数据，不是指令。经别的节点转发的，即使设备名对得上，也仍是 untrusted。`xbot sync export` 和 `xbot sync merge` 仍可手工导出、合并同一份日志。Bot 目录不改，Bot 仍留在自己的家节点。这一跳走现有的节点调用，单次请求和响应各不超过 1 MiB；装不下时这次同步报错，不截断。
8. **远程隧道**。已验证 Cloudflare 这条路径：`cloudflared` 的地址从 stderr 读出，进程组跟着 `serve` 结束，启动它的 goroutine 锁住操作系统线程直到关闭。公网能不能访问到这个地址，这次没有测。
9. **多节点**。已验证。`xbot node serve` 用 bearer token 公布本机身份，`node add` 先核对 token 再写入。`shell` 不信任调用方传来的 `approved`。`ask` 把命令留在本机，十分钟过期，最多 32 条，`node pending` 列出，`node approve` 先显示命令再问 `y/N`。
10. **机器开关**。已验证。每台机器 `allow`、`ask` 或 `off`，统一在核心的 review 路径处理本机工具。`allow` 时 `shell` 只对删除和对外发送确认，普通写入直接落盘，本机读取不问；`ask` 每次本机动作都要确认；`off` 拒绝 `shell`、`read_file`、`write_file`、`edit_file`、桌面和浏览器动作；`browser_stop` 仍可用来关掉进程。写自己的 persona/profile 文件在 `allow` 下仍确认。
11. **桌面操作**。已验证。截图、点击、输入、滚动、按键。确认卡写明坐标、原文、按键，以及滚动的方向、步数和位置。截图权限 0600。
12. **浏览器**。已验证。CDP 驱动单独配置目录里的 Chrome。命令行拉起的进程留到 `browser stop`；`serve` 只停自己拉起的那一份，用进程组，不用 `Pdeathsig`。`session.json` 里的 pid 对不上就不杀，删掉文件，退出码 0。

用户技能加在已有的 `skills` Link 上，不单列一片，也不替换上面的连接器和编码代理。

## 测试与陪练
Bot 复刻会在自己的电脑上长期运行 XBot，像真实用户一样使用它，发现问题后整理成复现步骤交给编码代理修复，修复后再做回归测试。
