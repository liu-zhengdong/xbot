# 教训

格式：一行规则；原因；证据（提交或问题）；出现次数「Seen N×」。同一条在 3 个不同日子出现，就升级进 [AGENTS.md](AGENTS.md) 的硬约束。

## 教训
- **测试要等真正的结束事件，不能等「已经没有后台」；阻塞用的测试工具要认 context。并行的 `go test` 不能共用一个会在结束时删掉的临时目录。** 原因：`finishBackground` 先把后台从运行表拿掉，再往收件箱写 `fork_end`。`waitNoBackgrounds` 一看到表空就返回，`endEvent` 读到 0 条。`waitTool` 只等 `rel`、不看 context，分叉没发生时 `runBackground` 能卡满 10 分钟。内核 `TestMain` 把所有进程的 `TMPDIR` 指到同一个 `.test-tmp/kernel`，先跑完的进程 `RemoveAll` 会删掉还在写的进程的目录，分叉写不进去，同样卡在 `rel`。证据：`TestControlRunsInTheSegmentItEarns`，6 个进程并行时挂住；`TestWaitNoBackgroundsWaitsForForkEndEvent`、`TestWaitToolStopsWhenContextEnds`。Seen 1×（2026-10-17）
- **检查 shell 命令要看命令链里的每一段，强制推送算破坏性操作。** 原因：`git push --force` 不弹确认；命令里第一个 git 不是 clean/push 时，后面的就漏查（`git add . && git push -f`）。证据：`2617bc0`。Seen 1×（2026-10-16）
- **密钥不能和它加密的数据放在一起。** 原因：保险箱密钥放在 `bots/<name>/vault/key`，Bot 的 shell 读出来就能解密。证据：`42cbed0`，[ADR 0005](docs/adr/0005-vault-daemon-user.md)。Seen 1×（2026-10-16）
- **耐久文件不放 /tmp。** 原因：盒子重启清空了 /tmp，丢了 F5 的工作，也删掉了 pnpm 包仓库（dsh 跑不起来）。证据：2026-10-16 约 16:42 重启。Seen 1×（2026-10-16）
- **测试要和外部环境变量隔离（用 t.Setenv 清掉 XBOT_VAULT_SOCKET 等）。** 原因：shell 里留着 XBOT_VAULT_SOCKET，合并前 3 遍测试都挂在 TestSecretMigrateMovesSecretsToDaemon。证据：12602f5 之后的修复。Seen 1×（2026-10-16）
- **自我 re-exec 要把内存里持有的 key 放回新进程的环境。** 原因：v0.1.0 的 serve 升级重启后报 XBOT_API_KEY is empty（启动时 key 已从环境清掉）。证据：v0.1.1 的修复。Seen 1×（2026-10-16）
- **提交前看 git status：本地二进制、日志、cookie 不进仓库。** 原因：Bot status UI 回归顺手留下的 cookie.txt、serve.log、21MB `xbot` 二进制被一并提交。证据：`ac35612`。Seen 1×（2026-10-16）
- **工具 JSON Schema 的 required 用空数组 []，不要 null。** 原因：`json.Marshal` 把 nil slice 编成 null，DeepSeek 拒收，种活三个长跑 Bot 时才发现。证据：`2ed8ca3`。Seen 1×（2026-10-16）
- **改共享规则后，要用真实 DeepSeek 跑一遍认证再合。规则冲突时写明谁让谁。** 原因：10-16 加的判断/务实规则和 DeepSeek 专用提示里「把修复做完」互相打架：root-cause 只复制字体、不改 site.css；propose-plan 擅自改了快照里的配置；avatar-handoff 把给别的 Bot 留言当成「替你发消息」，停下来先问。修法是写清楚适用范围：任务里写明的改动就直接做；快照或问「怎么办」时一个文件都不写；按文档给别的 Bot 留言不算替你发消息。证据：本提交，两遍 eval 都认证。Seen 1×（2026-10-16）
- **发给模型的消息里，tool 结果必须紧跟它的 tool_calls。** 原因：一轮超过前台上限转后台后，后台的结果排在前台回复后面；DeepSeek 官方接口宽容，magpie 等严格网关直接 400，dev Bot 每次唤醒都失败、只能重建。证据：`fix/tool-result-order`，`internal/kernel/toolorder_test.go`。Seen 1×（2026-10-16）
- **每个 Bot 一份的资源（保险箱、目录），共享入口里要按请求的 Bot 去打开，不能取「第一个」。** 原因：dev 第一版让入站 hub 共用 bots 目录里第一个 Bot 的保险箱验签，review 才发现。证据：`106f5f6` 返工前 `inboundVault`；修后 `TestConnectorInboundUsesEachBotsOwnVault`。Seen 1×（2026-10-16）
- **一批里每个提交都要能单独编译。** 原因：dev 把 serve 调用点的改动拆进 `b6839ee`，它引用下一个提交才加的 BackgroundMaxTime，单独编不过，bisect 会踩到。证据：v0.3.0 合并说明。Seen 1×（2026-10-17）
- **等待人决定的计时要挂在等待本身上，不能只靠外层流。** 原因：审批卡 60 秒随 SSE 断开失效，dev 第一版改成 24h 却在 ctx.Done 分支没有计时器。证据：`ee240a4`、`6f3ca0c`。Seen 1×（2026-10-17）
- **收益递减只数续段，不数普通工具步。** 原因：把每一步短工具调用都当成续轮后，第 4 步提醒、第 5 步硬停，25 步的状态接口停在 step 5。续段是后台因步数上限开的下一段。证据：`TestToolStepsAreNotDiminishingReturns`、`TestBotStepsEndpoint`。Seen 1×（2026-10-17）
- **同参循环只数连续重复，并按命令分开计。** 原因：失败的 `go test` 输出不以 `error:` 开头，被当成成功；`go build` 三次加 `go vet` 三次共用一个停机计数，第 6 次硬停。中间夹了别的调用要清零，成功一次要清掉失败计数。证据：`TestEditAndRetestDoesNotStop`、`TestBuildAndVetDoNotShareAStop`。Seen 1×（2026-10-17）
- **追加日志的进程内缓存要在文件 size 或 mtime 变了之后重新读。** 原因：两个进程各写一份 `reminders.jsonl` 时，先读进内存的那份一直用旧列表，冷却和去重都看漏。证据：`TestReminderCacheReloadsWhenFileChanges`。Seen 1×（2026-10-17）
- **局部读过的文件，`edit_file` 不能标成已经看全。** 原因：只读了第 1 行再改这一行，下一次整文件读取如果回执，模型就看不到后面的行。`write_file` 可以标完整，因为正文是模型自己给的。证据：`TestRangedReadThenEditDoesNotReceipt`。Seen 1×（2026-10-17）
- **短回执和「第 N 步」只在记下那次读取的同一轮里有效。** 原因：压缩把工具结果挪走之后，回执仍说没变，模型手里没有正文，步号也是上一轮的。证据：`TestReceiptDoesNotCrossTurns`。Seen 1×（2026-10-17）
- **主人拒绝过的调用只挡这一轮，下一轮要重新问。** 原因：进程级拒绝表让主人下一条消息说「改主意了」时，相同参数直接报错，不再出确认。证据：`TestDenialClearsOnNextOwnerTurn`。Seen 1×（2026-10-17）
- **按文件大小决定的缓冲不要按上限预分配。** 原因：6 字节的文件快照分配了 4 MiB 以上。哈希用流式读，缓冲按文件大小封顶。证据：`TestSnapshotSmallFileAlloc`。Seen 1×（2026-10-17）
- **`file_change` 的 diff 不能带出密钥文件的内容。** 原因：`.env`、`*.key`、`*.pem`、credentials 一类文件的提醒如果附上 diff，会把刚写进去的值送进模型上下文。这类路径只写 changed。证据：`TestSensitiveFileChangeOmitsDiff`。Seen 1×（2026-10-17）
- **超时不要杀掉还在出结果的命令；`sleep` 和明显的轮询除外。** 原因：到点杀进程会丢掉长构建和服务器；把 `sleep` 自动转后台又会把空转藏过熔断。证据：`TestShellTimeoutMovesToBackground`、`TestSleepIsNotAutoBackgrounded`。Seen 1×（2026-10-17）
- **后台命令的输出写在 Bot 目录里，并且先脱敏。** 原因：`/tmp` 会在重启时清空；输出文件里的密钥会在下一轮被 `shell_output` 读回来。证据：`TestBackgroundFileRedactsSecret`、`TestShellJobFileUsesKernelRedactor`。Seen 1×（2026-10-17）
- **已经报过的后台任务不能只靠进程内的集合去重。** 原因：重启后集合是空的，旧的已完成任务会每条都再开一轮模型。完成标记要写进任务记录，并且只为这个进程自己结束的任务投叫醒。证据：`TestRestartDoesNotRewakeFinishedJobs`。Seen 1×（2026-10-17）
- **追加的输出不要每次把整段尾部重写一遍。** 原因：尾部满了之后，每写几十个字节都要脱敏并重写 256 KiB。按行追加，只在超过上限时丢掉头部。证据：`TestJobWriterTrickleStaysSmall`。Seen 1×（2026-10-17）
- **脱敏窗口要盖住最长的密钥，不能只盖住固定的 1024 字节。** 原因：按整段替换的密钥比窗口长时，每一小段都对不上，明文会进任务日志。留住的长度取 max(1024, 最长密钥+1)，一次写入整块脱敏。证据：`TestLongMultilineSecretOnDisk`。Seen 1×（2026-10-17）
- **提交日志时要按替换区间映射偏移，不能看脱敏结果的后缀还在不在。** 原因：同一段里先出现过一次完整密钥时，后缀对不上，整块连同下一次密钥的前半会被写进文件。没提交的尾部也不能先作为预览落盘。证据：`TestZZR3SecondCopyCommittedRaw`、`TestZZR3PreviewShowsPartialSecret`、`TestJobLogRedactionProperty`。Seen 1×（2026-10-17）

## 做对了
- 盒子回归用假模型和回显服务，真值只在服务端出现，transcript 和模型输入里 0 处（M7，2026-10-16）。
- Bugfix 带旧代码会失败的回归测试（`2ed8ca3`、`7ff7738`、`61f3395`、`2617bc0`，2026-10-16）。
- 提交说明把没跑的检查列成 Not run（Magpie，2026-10-16）。
- Bot status UI 用假模型 CDP 回归覆盖 running/awaiting/idle/remote/jump-to-confirm/mobile sheet（`ac35612`，2026-10-16）。
