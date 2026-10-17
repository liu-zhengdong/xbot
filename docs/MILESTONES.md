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
| M8 | 开源网关与跨设备 Link（[GATEWAY.md](GATEWAY.md)，G1–G7）；P2P、跨用户通信、模板分享推迟 | 设计已定（2026-10-18）。G1 网关、G2 身份与 `bot@设备` 收发已合入，盒子上两个数据根端到端通过；G3 邀请码、G4 加密待做 |
| 桌面端 | 本地优先桌面端（Windows、Mac），见下文「本地优先桌面端」 | 计划中（2026-10-17 定） |


全局验收：每个里程碑都要报性能数字（交互回复 p95、IPC 开销 p95、后台变慢比例、链路数据量；涉及界面的报帧率），预算见 [AGENTS.md](../AGENTS.md) 的「全局要求：性能与界面流畅」。

## 8 步内核计划（2026-10-17 定）

来自 [Claude Code 内部机制调研](research/claude-code-internals.md)，主人批准。文件检查点和回滚、hooks、规划模式不做（见 [候选清单](CANDIDATES.md)）。这里的 M1–M8 是这份计划的编号，和上面总表的里程碑不是一回事。

| 步 | 内容 | 状态 |
|---|---|---|
| M1 | 提醒通道，熔断与打转检测 | 已合入 |
| M2 | 文件新鲜度 | 已合入 |
| M3 | 待办清单 | 已合入 |
| M4 | shell 后台化 | 已合入 |
| M5 | 环境感知（AGENTS.md 分层加载、持久 cd、token 用量、外部改动 diff） | 已合入 |
| M6 | 搜索与看图 | 已接上 |
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

项目说明：调用方的工作目录（没设工作区时不读进程当前目录）向上读到工作区或共享目录根。每层有 `AGENTS.md` 就用它，否则用 `CLAUDE.md`，近的在后。同一套只注入一次，`cd` 进了另一个子目录才再发。目录和候选文件的 mtime、大小、inode、ctime 没变就只 stat，不再读文件、不再哈希。合计最多 32 KiB，更远的文件先丢掉，截断时正文写 `[truncated]`。关掉标签按 `(?i)<\s*/\s*project_instructions` 拆开，大小写和空白都盖住。文件用 `O_NOFOLLOW` 打开，最后一段是符号链接就不跟。允许的根以外的路径不读。工作区根上、且该目录不是 git 检出、也不是共享目录时，文件是主人放的项目约定，贴在最新一条消息上。共享目录、带 `.git` 的工作区根、子目录或克隆仓库里的是仓库提供的数据，不是主人的指示，只作编码约定，不能当作批准、发送、读密钥或删除的依据；整段包在 `<untrusted_event source="project">` 里，单独成一条，不写进主人那条可信正文，下次组装也不贴回去。权限表不看这些正文，写入提醒前仍过脱敏。稳定规则加了 `ProjectRule`（`DefaultRules`）。新 Bot 的 `rules.md` 带上它；已经写好的 `rules.md` 不变。`evals/` 和 `internal/eval` 没有提示词快照或哈希基线可改，这次稳定前缀变长是有意的。

持久工作目录：前台 `shell` 的 `cd` 留到下一次调用，记在内存和 `bots/<name>/cwd`。每个后台分支按分支 id 另有一份，每个子代理在创建时继承调用方当时的目录；这三份互不影响，只有前台写入 `bots/<name>/cwd`。每次调用确认目录还在、并且在工作区或共享目录里；丢了或不在里面就回到工作区，结果里写明。结果在状态行后面加 `cwd:`。`false` 和显式 `exit` 也留下目录。后台 shell 任务只记下启动时的目录，不改调用方的会话目录。到点转后台的那次也不改。cwd 标记在 `bots/<name>/cwd-markers`，不放进程临时目录。没挂会话的 shell（评测沙箱、节点、命令行）每次仍从自己的目录开始。查项目说明或做权限判断时不把回退写进这份记录，免得把「目录丢了」的说明吞掉。`read_file`、`write_file`、`edit_file`、项目说明、权限判断和后台冲突检测的相对路径按调用方的目录解析；绝对路径不变；新鲜度记下的是绝对路径。评测沙箱的文件工具仍钉在沙箱根上。

用量：流式请求带 `include_usage`。有 usage 对象就按原数记 prompt、completion、cached，0 也算报告过，不用估计值替换。没有对象才按大约 4 字一 token 估计，trace 的 reason 是 estimated，提醒里写 estimated。prompt 跨过该模型 `providers.json` 上下文窗口的 50/75/90% 时，提醒通道发一行；同一档不重复，掉下去再跨过才再发。没有窗口长度就不发百分比，计数仍进 `trace.jsonl` 和 `BotStatus.tokens`。本进程累计，重启从 0 开始，历史在 trace 里。

外部改动：`file_change` 改成不超过 4 KiB 的统一 diff，放不下写 `[diff truncated]`。敏感文件名仍只写 changed。自己 `write_file` / `edit_file` 成功后刷新记录，冷却过了也不再报。

提醒通道这一轮多出来的 token（`compact.Estimate`，大约 4 字一 token，不是供应商分词器）：一份大约 3094 字节的 `AGENTS.md` 第一次注入 3419 字节、855 token（根上的约定，措辞比第一版稍长）；下一轮新注入 0 字节，历史里仍带着那 855 token，直到压缩。再 `cd` 进一个大约 1 KiB 的子项目说明后，新的一套是 4634 字节、1159 token（含不可信包裹）。50% 那一行 133 字节、34 token，只有跨过门槛才发。一小段外部 diff 大约 34 字节。读这一套项目说明的平均耗时 40 微秒（50 次，含第一次读盘）；基准 `BenchmarkProjectText` 38242 ns/op，17868 B/op，218 allocs/op（amd64，200ms，命中按 mtime、inode、ctime 缓存的路径）。远低于本机 IPC 5 毫秒预算。不涉及界面帧率。

评审留下、这次没改的两处：shell 里 `sed -i` 改到的文件仍可能被 `file_change` 报成外部变化（只有 `write_file` / `edit_file` 会刷新新鲜度）。后台段记下的用量可能盖住前台的 prompt 计数（`noteUsage` 写的是同一份内核计数）。`internal/browser` 的 `TestStopChecksCommandLine` 在 main 上也会失败，不追。评审留下的 P2/P3 都记成了 issue：#19–#27，另有 #18（已有 Bot 的 `rules.md` 拿不到 `ProjectRule`）。

假模型回归（评审修复这一轮）：`gofmt -l` 对已跟踪的 Go 文件无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。Not run: 真模型评测，界面浏览器检查（没有界面改动），这五个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。`evals/` 没有提示词基线文件可更新。

再评审：子代理或后台分支的 `cd` 不再改前台目录，前台相对 `write_file` 仍落在前台目录。工作区根自己是 git 检出，以及共享目录上的 `AGENTS.md`，都按仓库提供的数据处理。项目说明缓存键含 inode 和 ctime。回归：`TestSubagentCwdDoesNotMoveParent`、`TestBackgroundCwdDoesNotMoveForeground`、`TestReviewFileConflictUsesCallerCwd`、`TestProjectRootCloneIsUntrusted`、`TestProjectSharedDirIsUntrusted`、`TestProjectCacheSeesSameMtimeEdit`。

假模型回归（目录按执行上下文分开这一轮）：`gofmt -l` 对已跟踪的 Go 文件无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/full` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。`BenchmarkProjectText` 38242 ns/op，17868 B/op，218 allocs/op。Not run: 真模型评测，界面浏览器检查（没有界面改动），这五个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

## 搜索与看图（研究笔记里的 M6）

相对环境感知那一版。三个只读工具挂在内置 tools Link 上，权限表里是 `readonly`。工具自己的 `Confirm` 不决定要不要问。`view_image` 算本机工具，机器开关 `off` 拒绝、`ask` 出确认卡；`web_fetch` 和 `web_search` 不走这道开关。本地路径触及保险箱或密钥时，`view_image` 和文件工具一样出确认卡。

`web_fetch` 只取公开 http(s)。解析之后和每一次重定向前都拒绝私网、回环、链路本地、元数据、运营商级 NAT 和其余 IANA 专用段；十进制和 `0x` 字面地址同样拒绝。名字解析两次，第二次不再是公网就当成 DNS 重绑定拒绝。重定向最多 5 次，超时 20 秒，不走环境代理。HTML 收成文字，`focus` 只留含该词的段落。结果包在 `<untrusted_event source="web_fetch">` 里，正文里的结束标签会被拆开。大约 48 KiB 以上截断。再长的工具结果仍按原有 transcript blob 存全文、行内只留预览。

`web_search` 的代码默认仍是 DuckDuckGo 的 HTML 接口，不需要密钥。部署选 DeepSeek 时在 `web.json` 写 `search.backend` 为 `deepseek`（或 `XBOT_SEARCH_BACKEND=deepseek`），`base_url` 可省略，省略就是 `https://api.deepseek.com`。请求是官方 Search API：`POST /v1/tools/search`，正文只有 `query`（1–128 个字符）和 `count`（1–50；工具侧默认 5、最大 10），不传 `need_content`，没有模型，也不走 Responses API。成功且结果非空按次计费（1000 次 5 元），空的 `results` 算成功、不计费。402 是余额不足，429 是超过每秒 5 次。没写 `api_key_env` 只用 `DEEPSEEK_API_KEY`，不回落到 `XBOT_API_KEY`。现场只有 `XBOT_API_KEY` 时，在 `web.json` 写 `"api_key_env":"XBOT_API_KEY"` 显式复用，例如 `{"search":{"backend":"deepseek","api_key_env":"XBOT_API_KEY"}}`。供应商密钥（`DEEPSEEK_API_KEY`、`XBOT_API_KEY`，或 `providers.json` 里的 `api_key_env`）只能发往 `https://api.deepseek.com`。`providers.json` 读失败时也按供应商密钥处理。已经收进内存、但不在这份名单里的搜索密钥仍可以走别的 https 主机。`http` 一律拒绝。单独的变量名会和供应商密钥一样从进程环境清掉；启动之后才写进 `web.json` 的，下次配置工具时再清一次，并进入脱敏。密钥不进日志。旧的 `search.model` 字段还读得进来，但忽略。DuckDuckGo 遇到非 200 或挑战页会报错，让模型告诉主人，不说明怎么换后端；查询词被当成回显，不拿来判断挑战页。付费抓取后端没有默认实现，配了未知的 `fetch.backend` 就失败。结果是标题、URL、摘要，有 summary 和 date 时一并写上，同样包成不可信数据。围栏里的结束标签换成 `[/untrusted_event]`。`web.json` 的 `trusted_egress_cidrs` 只放行 DNS 答案（例如 `198.18.0.0/15`），不放行字面 IP；`::ffff:127.0.0.0/104` 这类映射地址按里面的 IPv4 再对一遍回环和链路本地。

`view_image` 接受本地路径或公开图片 URL，JPEG、PNG、GIF、WebP。文件超过 32 MiB 不读，超过 10 MiB 不送给模型。边长超过 2048 会缩小；WebP 超了就拒绝。读不出宽高就拒绝。边长不超过 2048 的 JPEG、PNG、GIF 原样通过。要缩小的那一路才按 160 MiB 封顶，解码一次只跑一张，缩小后再看有没有超过 10 MiB。WebP 不解码，不占这份额。相对路径用调用方的工作目录；解析符号链接后必须在工作区或共享目录里，沙箱里钉在沙箱根。看图能力写在模型条目的 `vision` 上。`deepseek-flash`（名字里同时有 deepseek 和 flash，含 `group/auto-deepseek-v4-1-flash`）默认能看；`deepseek-chat` 和 `deepseek-v4-pro` 默认不能。显式 true 或 false 盖过名字。没绑定供应商时跟 `XBOT_MODEL`，没设才是 deepseek-flash。不能看的模型得到 `model <id> does not support vision`，图片不会悄悄丢掉。能看的模型收到 `content` 数组：一块 `text`，一块 `image_url` 数据 URL。同一轮只保留最新一批。一轮最多 4 张、合计 10 MiB。图片字节不进 transcript，这一轮结束才从内存里清掉，下一轮没有。子代理连续调用工具时只留最新一批，在没有工具调用的回复之后全部清掉，再接排队或插话，所以连续或跨五轮各看一张时第五张都能附上。16 位彩色 PNG（类型 2、4、6）按 8 字节每像素估计，16 位灰度按 2 字节；渐进 JPEG 按 12 字节每像素，4032×3024 的手机照片会缩小，不会被拒。`xbot provider add --vision on|off` 写入，省略时保留原值。工具执行里的 panic 变成工具错误，不会把 serve 打崩。确认回调里的 panic 会放开锁，panic 文本先脱敏再按字节截断。HTML 扫描用原文字节上的 ASCII 大小写比较，避免 `İ` 这类变长小写把偏移用错。

评测认证套件没有加新的提示，也没有把这两个工具放进沙箱工具表。假模型用例在 `internal/kernel` 和 `internal/tools` 里，搜索和抓取都用假后端，不访问网络。

假模型回归：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/full` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。本机基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 77.37 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 24843 ns/op，10864 B/op，11 allocs/op（约 177 MB/s，一份重复的小页面）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测。评测认证套件的系统提示没有改；tools Link 的一行摘要也没改，新工具只出现在工具 schema 里。

评审修复这一轮：`1aee130` 上 `HTMLToText(strings.Repeat("İ",200)+"<p>a</p><script>b</script><p>c</p>")` 会 panic `[416:234]`，现在这条回归通过，并留下 `a` 和 `c`、丢掉脚本里的 `b`。工具 panic 收成 `error: tool panic`。结束标签不分大小写、容许空白，全角 `＜／` 也会拆开。`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 76.53 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 18001 ns/op，10864 B/op，11 allocs/op（约 244 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。主人在 deepseek-flash、思考关闭下看过 `view_image` 是对的；网页工具被部署环境的 `198.18.0.0/15` 假 IP 挡住，DuckDuckGo 当时返回 0 条（非 200 的挑战页）。Not run: 这一轮没有再打真模型，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`b1aee62`）：DeepSeek 搜索改成官方 `POST /v1/tools/search`，正文只有 `query` 和 `count`。Responses API、`deepseek-v4-pro` 搜索模型、`search.model` 和散文回退都去掉。402 报余额不足，429 报频率。密钥只用 `DEEPSEEK_API_KEY` 或显式 `api_key_env`，不回落到 `XBOT_API_KEY`；供应商密钥只发往 `https://api.deepseek.com`。`web.json` 和 `providers.json` 要确认卡。启动之后才写上的搜索密钥，配置工具时再收进内存并脱敏。不可信围栏的替换改成 `[/untrusted_event]`，不再是还能被认成结束标签的 `</ untrusted_event`；实体、零宽字符和多出来的斜线、下划线、尖括号也拆开。确认回调的锁用 `defer` 放开，panic 文本先脱敏再截断。DuckDuckGo 的报错只让模型告诉主人，查询词回显不当成挑战页。看图读不出宽高就拒绝，解码按位深限制并串行，缩小后再对 10 MiB。`::ffff:127.0.0.0/104` 不能写进出站白名单。Responses API 的注释偏移随那个解析器一起删掉。子代理工具仍不经过 `reviewedTool`。

假模型回归（Search API 这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 82.58 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 17347 ns/op，10864 B/op，11 allocs/op（253.64 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`3f8db10`）：看图的解码字节上限只加在真正要缩小的那一路，一张位图按 4096×4096×4 估计；2048×1536 原样通过，2560×1440 和 3000×1200 会缩小，16 位 8000×4000 拒绝。WebP 不解码，不占这份额。工具图片留在这一轮剩下的每一步请求里，仍是 4 张、合计 10 MiB，这一轮结束才清掉，不进 transcript，下一轮没有。搜索密钥在 `providers.json` 读失败，或名字属于启动时的 `KeyEnvNames`、或已经收进内存时，只能发往 `https://api.deepseek.com`。现场只有 `XBOT_API_KEY` 时，用 `"api_key_env":"XBOT_API_KEY"` 显式复用。数字字符引用的前导零和超过一个码位的位数会拆开；零宽连接符、不可见运算符和变体选择符也跳过。收进内存的密钥算进 shell 的保留形式，跨两次写入不会漏出去。脱敏用的密钥列表缓存到下次 Hold 或 Forget。`link.Wrap` 仍记在 issue #34，这次没改。子代理工具仍不经过 `reviewedTool`。

假模型回归（这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 79.46 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 17487 ns/op，10864 B/op，11 allocs/op（251.61 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`89848e0`）：子代理在一轮以没有工具调用的回复结束时清掉图片，发生在排队任务和插话之前。跨五轮各看一张时第五张仍然能附上，第一轮的图片不会留在那次回复之后的请求里。主线同一轮只保留最新一批。上下文估算把图片字节按大约 4 字节一个 token 算进去。16 位彩色 PNG（类型 2、4、6）按 8 字节每像素，16 位灰度按 2 字节；渐进 JPEG 再加一块同样大小的缓冲。搜索密钥算不算供应商密钥，只看 `DEEPSEEK_API_KEY`、`XBOT_API_KEY`、`providers.json` 的 `api_key_env`，以及注册表读失败。已经收进内存的单独搜索密钥仍可以走别的 https 主机。`link.Wrap` 仍是 issue #34。子代理工具仍不经过 `reviewedTool`。

假模型回归（子代理看图这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`、`internal/exec`、`internal/compact`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 90.39 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 19150 ns/op，10864 B/op，11 allocs/op（229.76 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`e818aa7`）：子代理在一串工具调用里每次有新图片就清掉之前消息上的图片，连续五次 `view_image` 时最后一次请求只带第五张。渐进 JPEG 改成按 12 字节每像素估计，解码上限 160 MiB，解码仍一次一张；2560×1440、3000×2000、4032×3024 的真实渐进 JPEG 都缩小通过。回归：`TestSubagentImageStreakKeepsNewest`、`TestProgressiveJPEGPhotoPassesCap`、`TestDecodeCapAllows16BitRGBAAndRefusesOversizeSide`，在 `e818aa7` 上失败。修复由本机 dsh 写，独立评审是 zcode。

## 两层记忆（2026-10-17）

主人决定记忆只留 person 和 bot。person 跨所有 Bot 共享，只放每个 Bot 都需要的、关于主人本人的事实（时区、语言、作息、他是谁），写入必须显式带 scope。bot 是这个 Bot 自己的角色、约定和项目决定，也是默认：省略 scope 的 `remember`、自动抽事实、不可信回合的自动写入都进 bot。工作规则和项目决定不进 person。bot-person 不再存储。怎么做事的冲突以 Bot 自己的记忆为准，压过 person。常驻注入从三份 profile（`3×4KiB+512`，12800 字节）改成两份（`2×4KiB+512`，8704 字节）。

迁移每个数据根每进程走一次（`sync.Once`）。`serve` 和 `chat` 打开内核时触发，Remember、Recall、Forget 也会触发，但同一进程里不会重复走。只合并该 Bot `owners` 里的人：Bot 记忆每个发送者都看得见。其他人的目录改名为 `memory.bak`，不并进去。某个目录读失败就记日志、跳过、留在原地，不让别的 Bot 的读写失败，也不让启动失败。合并去重，保留时间戳、来源和原来的 kind（空 kind 不改写成 work）。profile 放不下的改记 log，并写一条 `migration-profile-cap`。log 超过 64 KiB 时，多出来的写到 `log.overflow.md`，`log.md` 本身不超过 64 KiB；检索和 forget 把这份溢出和日志一起读。已经超限的 profile 或 log 上，缩小文件的 forget 仍然成功。人的同步日志仍是 person 通道。不可信回合的 `remember` 和抽事实，不论落在哪一层，都标 `Untrusted` 并加上 provenance。

网页档案不再读已删除的 bot-person 范围，界面上也不再标「你们之间」。`ScopeBotPerson` 常量还留着，存储层拒绝这个范围。

第二轮：包在 `<untrusted_event source=...>` 里的内容，即使这轮是可信的，抽出来的事实也标不可信。`config.json` 缺失、读不了或坏掉不算没有主人，旧目录留在原地；`owners` 为空才把旧目录改成 `memory.bak`，并记日志。迁日志时 `log.md` 留到大约 48 KiB，新的事实留在文件里。没有持久化的迁移标记：每次进程启动都重新走一遍 `bots/*/people/*/memory`，每个数据根每进程一次；迁过的目录已改名为 `memory.bak`，所以重复走是幂等的。同一个键上，最新的那条赢；出处只做标注，不改变取舍。来源后缀在长度检查之后补上，单条仍不超过 1 KiB。

空数据根上 `Migrate` 只做一次目录读取：844.3 ns/op。已有一个 Bot、没有旧的人目录时 7849 ns/op（amd64，`-benchtime=200ms`，一次性基准，没有提交进仓库）。都低于本机进程间通信 5 毫秒。不涉及界面帧率。

评审修复后再跑：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/m33-{1,2,3}` 跑 `go test -count=1 ./...` 三遍。第 1、3 遍全绿。第 2 遍只有 `internal/browser` 的 `TestReleaseTiedLeavesCLI` 失败（`tied chrome still running`，2 秒没等到进程退出）；单独再跑这条和 `TestStopChecksCommandLine` 通过。`TestStopChecksCommandLine`（issue #27）这三遍没有失败。`memory-scope` 的检查放宽成子串 `run the tests` 和 `Asia/Shanghai`。回归：`TestM33UntrustedDefaultScopeRememberIsAttributed`、`TestM33CorruptLegacyIndexDoesNotBreakOtherBots`、`TestMigrateCorruptOwnerStaysAndOtherBotWorks`、`TestMigrateLeavesNonOwnerInBackup`、`TestMigrateLogStaysWithinCap`、`TestForgetOnOverCapLogSucceeds`。Not run: 真模型评测，界面浏览器手点（网页只删了 bot-person 的读取和标签），`-race`，`go test -fuzz`，默认 `TMPDIR`。

## 记忆 v2（2026-10-17 定）
规格见 [MEMORY-V2.md](MEMORY-V2.md)，决策见 [ADR 0008](adr/0008-memory-v2-dated-log-first.md)、[ADR 0009](adr/0009-memory-as-folder.md)。批 1–3 放在晚上的时段做，批 4–6 在 11/30 之前完成，自带技能目录放在桌面端 B 阶段。

| 批 | 内容 | 状态 |
|---|---|---|
| 1 | 快照加差异：会话开始和压缩时拍快照（含最近 8 条日志），中途只追加差异，自己的改动不追加 | 进行中 |
| 2 | 目录布局（`memory.md`、`log/YYYY-MM.md`）和迁移 | 计划中 |
| 3 | 记忆路径上的文件工具把关；拿掉 `remember`/`forget` | 计划中 |
| 4 | 只读的 `recall`（含自由文件） | 计划中 |
| 5 | 经过摘要 | 计划中 |
| 6 | 每晚整理（含索引核对、工作档案） | 计划中 |
| 7 | 导入 Grok 导出、`xbot backup` | 计划中 |

## 下个版本（v0.3.1 / v0.4.0）
v0.3.0 只发内核修复。下面三项提示类改动做完、过了 review，但都拉低了 DeepSeek 的 give-up 用例（向一个死端点 POST，应当如实报告做不到，最多 4 次工具调用），撤到下个版本重做，每项配自己的 eval 用例，不靠 give-up 调。

- **模糊任务规则**（「一两句计划后同轮开工」）。加上后全量 eval 里 give-up 连续 0/3、1/3、1/3，三轮改措辞没稳住：模型在目标已写明的任务前先翻目录、补细节，超出工具上限。
- **知识问题三档规则**，连同 knowledge-public / knowledge-project / knowledge-fresh 三条用例和 eval 的 `no_tool_calls` 检查。
- **交互回复默认低推理强度**（profile `reasoning_effort`）。

give-up 单测数据（每个构建 6 次，2026-10-17）：7e64ec6 6/6；加上内核修复、没有知识规则 6/6；再加知识规则 5/6；再加低推理强度 4/6。拿掉模糊任务规则后的全量 eval 两遍都只有 give-up 1/3 不过。证据：`/workspace/xbot-test/weekly/v030/`（evalB/D/E/G/H、gu-*.log）。

另：第 9 项（中文回复前的英文开场白；`xbot provider add/list/remove`；`xbot new` 拒绝未知供应商）也留到下个版本。

评测按四维成绩单出结果（能力、意愿、可靠性 pass^k、守规矩一票否决，另报 Ask-F1），贴用例的 DeepSeek 规则清单和替换原则也在里面：见 [SCORECARD.md](SCORECARD.md)。第一步只给 `xbot eval` 加成绩单输出，不改提示词。

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

## 本地优先桌面端（2026-10-17 定）

主人决定：XBot 装在主人自己的电脑上，Windows 和 Mac 同时做。`serve` 跟着桌面端在本机跑，所有 Bot 数据都在本机，窗口显示本机 `serve` 的现有 Web 界面。设计决定见 [ADR 0007](adr/0007-local-first-desktop-app.md)。

**完成的样子**：主人在 Windows 和 Mac 上各装一个，打开就能直接用本机的 Bot，连续用一周不想换回别的。做出来后先由 UX 设计同事在浏览器里走一遍，再交主人实际使用。

**已定的决定**：
- Windows 要求装 Git for Windows，shell 工具用 Git Bash（`bash -c` 语义，权限表规则不变）。
- cron 按电脑的本地时区解释，不再按 UTC；已有例程升级时迁移，触发时刻不变。
- 关窗口就退出（不做托盘和开机自启）。错过的例程不补跑：下次启动时每个 Bot 收到一条汇总通知，列出错过的时间点，由 Bot 自己决定。

**摸底结论（`d6a90a3`）**：`darwin/arm64`、`darwin/amd64` 的 `CGO_ENABLED=0 go build ./...` 通过，`go vet` 只挂在一处测试代码；`windows/amd64` 编译不过（`fsutil`、`kernel` 的 `flock`，`browser`、`tunnel` 的 `kill` 和进程组，`kernel/project.go` 的 `x/sys/unix`）。编过以后，Windows 上还有三处挡启动或挡核心功能：保险箱密钥的权限检查（Windows 上 0600 读回来是 0666，第二次启动所有 Bot 都起不来）、目录 fsync（对话分段封存失败）、shell 写死 `sh -c`。MyGo 外壳的最小窗口能从 Linux 交叉编译出 Windows 和 mac 版本（每个约 9 MB）；在盒子的 WebKitGTK 里指向本机 `serve`，界面、登录、重开免登录都正常。

每个 PR 约 500 行以内，通用要求：测试三遍全绿、触及包跑 `-race`、每个修复带一条在旧代码上会失败的回归测试、没跑的列成 Not run。

### A 线：跨平台核心
| PR | 交付 | 验收 |
|---|---|---|
| A1 | 文件锁抽象（unix `flock`，Windows `LockFileEx`），`kernel` 的两处改用它；`windows`、`darwin` 的编译和 `go vet`（含测试代码）修好；交叉编译脚本；GitHub Actions 加 Windows 和 macOS 的 `go test`（先不挡合并） | 三个目标 build 和 vet 全绿；Linux 行为不变；CI 上能看到 Windows/mac 的测试结果 |
| A2 | 进程控制：Windows 用 Job Object 杀整棵进程树、查存活和启动时间；mac 用 `sysctl` 取启动时间；编码代理和浏览器复用 | 取消后不留孤儿进程；`serve` 重启后还在跑的后台任务不被标成 lost |
| A3 | Windows shell：找 Git Bash（`XBOT_SHELL`、注册表、常见路径），找不到时给出安装指引；系统提示注入当前系统 | Windows 上 shell 工具和后台任务能跑 |
| A4 | Windows 存储语义：保险箱密钥权限检查、目录 fsync、原子写遇到共享冲突时重试、只读分段；ID 按大小写不敏感查重、拒绝 Windows 保留名、Unicode 规范化 | Windows 上重启后 Bot 照常起来，对话分段能封存；`Dev` 和 `dev` 不会落进同一个目录 |
| A5 | 重启和升级：Windows 上启动新进程再退出，替换正在运行的 exe 时先改名 `.old`；发布加 Windows 和 `darwin/amd64` | Windows 上实际升级一次 |
| A6 | 浏览器工具按平台找 Chrome，所有权判断不读 `/proc`；桌面操控工具在 mac/Windows 上标成不可用 | mac/Windows 上能开一次 Chrome |
| A7 | 编辑工具保持文件原来的换行（CRLF）；子进程的 `HOME`/`USERPROFILE` | CRLF 文件能正常编辑 |

### B 线：桌面外壳
| PR | 交付 | 验收 |
|---|---|---|
| B1 | `internal/appshell` 加 `xbot app`：同进程起 `serve`，固定端口 `127.0.0.1:8787`，单实例，一次性启动码自动登录，日志不含 token；`go.mod` 升到 Go 1.27 | 盒子上能开 Linux 窗口并截图；交叉编译出三个平台 |
| B2 | 界面适配：标题栏留空位和拖动区域，全局字体加微软雅黑和苹方，原生通知从 Go 端发，外链用系统浏览器打开 | UX 同事在浏览器里走一遍；Windows 标题栏、双击最大化、吸附布局，mac 红绿灯 |
| B3 | 打包：GitHub Actions 出 mac `.app` 和 dmg、Windows 安装器、Linux `.deb`，附 SHA256SUMS 和不带版本号的固定下载地址；Gatekeeper 和 SmartScreen 放行说明（不签名） | 真机上装得上、放行后能打开 |
| B4 | 应用内自更新：复用 `internal/upgrade`，后台预下载，「重启以更新」，更新说明只列几条中文要点；mac 整体替换 `.app`，Windows 用 `.old` | 真机上完整升级一次 |

### C 线：定时任务
| PR | 交付 | 验收 |
|---|---|---|
| C1 | cron 按本地时区解释，已有例程迁移（触发时刻不变）；错过窗口的一次性例程不再永远保持启用 | 回归测试在旧代码上失败；线上例程迁移前后触发时刻一致 |
| C2 | 错过时间点的汇总通知：每个 Bot 记上次轮询的水位线（不放进例程目录），停机或睡眠超过 2 分钟时发一条汇总事件，ID 确定、可去重，最多看 7 天 | 停机 3 小时后正好收到一条汇总；重复投递只有一条；首次运行不发 |

**顺序**：A1 → C1 → C2 → B1 → B2 → B3，到这里 mac 就能装上用。接着 A4 → A3 → A2 → A5，Windows 能用。最后 B4、A6、A7。A1 和 C 线互不依赖，可以并行。

**要主人的真机才能验的**：WebView2 和 WKWebView 的实际渲染、字体、标题栏、拖动和吸附、原生通知；Windows 上 Git Bash、Job Object、`LockFileEx` 的强制锁、目录 fsync、rename 共享冲突、只读文件删除；dmg 和 Windows 安装器、Gatekeeper 和 SmartScreen 放行；mac 整体替换 `.app` 和 Windows `.old` 的自更新；合盖一晚后的汇总通知。盒子上能验的：交叉编译和 `go vet`、Linux 窗口（WebKitGTK）、单元测试；GitHub Actions 上能跑 Windows 和 macOS 的全量测试。

## 测试与陪练
Bot 复刻会在自己的电脑上长期运行 XBot，像真实用户一样使用它，发现问题后整理成复现步骤交给编码代理修复，修复后再做回归测试。
