# 教训

格式：一行规则；原因；证据（提交或问题）；出现次数「Seen N×」。同一条在 3 个不同日子出现，就升级进 [AGENTS.md](AGENTS.md) 的硬约束。

## 教训

- **脱敏缓存的注册代数要辨认对象替换，不能只数对象个数。** 原因：Shell 现读 sudo vault 时，新旧快照的 store-local generation 和注册数量可相同，轮换密码会误复用旧 forms。守护：`TestShellFormsFollowReopenedSudoVault`（回退注册代数修复后旧红，恢复后绿），以注册/注销 epoch 使缓存失效。Seen 1×（2026-10-31）。

- **连接器：安装配置不等于取得工具；发现失败不能缓存成成功空表。** 启动、握手、列工具错误要沿 load 返回，stderr 有界且脱敏，API 安装状态与运行状态分开。守护：`TestPlaywrightLoadToolsAndFailure`、`TestMCPStartupStderrAndStatus`、`TestMCPHTTPLoadDeadline`、`TestAppsExposeMCPRuntimeWithoutProbing`。Seen 1×（2026-10-27）
- **审核与执行必须共享参数解析，不能用不同 Go 字段类型各解一次 JSON。** 原因：重复 precheck 键末位 null 把审核的 `*string` 清为 nil，却让落盘的 `string` 保留先前敏感命令；修复让 create 两条路径共用解析。证据：`TestRoutineCreatePrecheckDuplicateNull` 在 `914a2f8` 上实际写出未批准 marker，修复后不落命令、不执行。Seen 1×（2026-10-27）
- **复用执行器不等于复用授权，嵌在排期参数里的命令也必须进入原命令审核。** 原因：例程 precheck 在 `59b41d1` 直接复用 runCommand，却把创建/修改仍当成 Normal，绕过保护路径和机器 ask；修复复用 Kernel.reviewCall，定时执行遇 ask/off 静默跳过。证据：`TestRoutinePrecheckUsesShellReview`、`TestTimerPrecheckHonorsChangedMachinePolicy` 旧红新绿。Seen 1×（2026-10-27）
- **构建短号与发布版本分开传。** 原因：`dev+tip` 会被显示版本函数剥掉后缀，关于页仍只有 dev；用同一短 HEAD 注入独立 Tip、写 build-info/xbot.tip，再验证 session 与复制内容。证据：TestSessionBuildTip、TestImggenDiscoverBrowser、test_xbot_dev.py 旧红新绿。Seen 1×（2026-10-26）
- **进度展示必须在 SSE 尚未结束时验收。** 原因：只验收尾后的轨迹，会漏掉 streaming 整段暂停 status 轮询和 live 工具名盖住摘要；浏览器回归应保持流打开、跨两个轮询周期检查预览与节点复用。证据：2026-10-26 `dfaa232` 设计师不 PASS，`TestTodoStreamingBrowser` 在该基线因无 running 进度失败。Seen 1×（2026-10-26）
- **工具名和描述必须对齐默认落盘行为。** 原因：更新资料却写成提议或等待确认，会让模型误判调用结果；不能为头像另加默认确认卡例外。证据：2026-10-26 用户确认 `update_profile` 默认直接落盘，permissions on 仍沿既有 SelfEdit 表；回归同时检查默认无卡和实际磁盘状态。Seen 1×（2026-10-26）
- **响应式抽屉要验证每个断点的开关几何与实际触摸。** 原因：只读第一段 CSS 会漏掉窄屏 translateY 和桌面 display:none 覆盖；长按还要保证松手不选中别的 Bot。证据：`TestBotPanelBrowser` 在旧代码的 390/1440 档因关闭轴失败、820 档因厂头额外动作失败，浏览器触摸补验通过。Seen 1×（2026-10-26）
- **区分 turn 曾经回答与某次调用的回复已经发出。** 原因：同 turn 插话续聊时 `answered` 保留为真，直接用它拒绝挂图会误伤新调用；清掉它又可能放过旧 Job。调用发起时捕获最终回复次数，并在读取前与提交前持锁比较。证据：`TestContinuationCanAttachImages` 旧红新绿、`TestContinuationRejectsOldImageJob` 新旧调用同 turn 并存。Seen 1×（2026-10-26）
- **按去重后的新增内容算剩余预算，身份未知时先限长流式判重。** 原因：读前直接用剩余预算拒绝同图，会让合法大图重挂失败；先整图读完再判重又退回无效大分配。证据：`TestQueuedDuplicateFitsBudget` 的三类引用及内容副本旧红新绿、`TestNewOutImageOverBudgetIsNotLoaded` 的超额新图分配上限。Seen 1×（2026-10-26）
- **OAuth 刷新写回必须与退出删除原子核对，测试要阻塞刷新响应再退出。** 原因：只串行登录回调挡不住 vault 自动刷新，logout 成功后迟到的刷新仍可重建凭证；写回前在 secrets 文件锁内重读并核对旧值，退出无需等待网络。证据：`7589cca` 的 P2，`TestOAuthRefreshAfterRemoveDoesNotRevive`、`TestOAuthLogoutDuringRefresh` 覆盖本地与 daemon。Seen 1×（2026-10-26）
- **流式事件预算必须覆盖厂商在最终事件中重复携带的完整结果集合。** 原因：每张 done 都可接收，不代表包含全部 result 的 completed 能通过同一行限；预算要按单项大小、数量和编码膨胀计算并保留有限开销。证据：`TestResponsesImageGenerationLargeCompleted`，两张各 13 MiB 的真实体积夹具在旧 32 MiB 行限下失败，修后保留最终路径。Seen 1×（2026-10-26）
- **厂商工具的成功要同时核对终态和本地产物，不能只看 result 存在。** 原因：失败或未完成的生图调用可能带残留图片，不能解码后把 failed 强改成 completed；done 与 completed 重复出现或省略 result 时也必须沿本次保存记录回报。证据：`TestImageGenerationTerminalShapes` 的四条终态回归修前失败、修后通过；`TestImageGenerationMissingIDsStillBounded` 守住无 ID 的数量上限。Seen 1×（2026-10-26）
- **流式文字显示成功后，还要守住完成消息和历史重绘。** 原因：Responses 只读取完成事件的 output，忽略单独送达的完整条目，导致已显示的回复落盘为空；历史重绘又跳过空助手行，回复随即消失。证据：`TestResponsesCompletedWithoutOutputRetainsStream`、`TestResponsesStreamPersistence`、`TestChatStreamBrowser`，旧代码均失败。Seen 1×（2026-10-25）
- **测试要等真正的结束事件，不能等「已经没有后台」；阻塞用的测试工具要认 context。并行的 `go test` 不能共用一个会在结束时删掉的临时目录。** 原因：`finishBackground` 先把后台从运行表拿掉，再往收件箱写 `fork_end`。`waitNoBackgrounds` 一看到表空就返回，`endEvent` 读到 0 条。`waitTool` 只等 `rel`、不看 context，分叉没发生时 `runBackground` 能卡满 10 分钟。内核 `TestMain` 把所有进程的 `TMPDIR` 指到同一个 `.test-tmp/kernel`，先跑完的进程 `RemoveAll` 会删掉还在写的进程的目录，分叉写不进去，同样卡在 `rel`。证据：`TestControlRunsInTheSegmentItEarns`，6 个进程并行时挂住；`TestWaitNoBackgroundsWaitsForForkEndEvent`、`TestWaitToolStopsWhenContextEnds`。同一类在 10-18 又出现：结束信号（`done` 通道、`Cancel`/`Shutdown` 返回、`runServe` 返回、回复文字流完）早于最后一次写入，测试一结束 TempDir 清理就撞上还在写的 `OnDone`、例程定时器、发件箱和子代理事件，8 个测试被 CI 按名字跳过。修法是让结束信号排在这个任务的最后一次写入之后，测试等到它再返回；判断「对方是谁」的读数为空时（exec 中的 `/proc/<pid>/cmdline`）不能当成「不是」。证据：`TestStopWaitsForOnDone`、`TestRunServeWaitsForTimerTicker`、`TestChromeOwnedWaitsOutEmptyCmdline`，#222–#226、#144、#51。Seen 2×（2026-10-17、2026-10-18）
- **检查 shell 命令要看命令链里的每一段，强制推送算破坏性操作。** 原因：`git push --force` 不弹确认；命令里第一个 git 不是 clean/push 时，后面的就漏查（`git add . && git push -f`）。证据：`2617bc0`。Seen 1×（2026-10-16）
- **密钥不能和它加密的数据放在一起。** 原因：保险箱密钥放在 `bots/<name>/vault/key`，Bot 的 shell 读出来就能解密。证据：`42cbed0`，[ADR 0005](docs/adr/0005-vault-daemon-user.md)。Seen 1×（2026-10-16）
- **耐久文件不放 /tmp。** 原因：盒子重启清空了 /tmp，丢了 F5 的工作，也删掉了 pnpm 包仓库（dsh 跑不起来）。证据：2026-10-16 约 16:42 重启。Seen 1×（2026-10-16）
- **测试要和外部环境变量隔离（用 t.Setenv 清掉 XBOT_VAULT_SOCKET 等）。** 原因：shell 里留着 XBOT_VAULT_SOCKET，合并前 3 遍测试都挂在 TestSecretMigrateMovesSecretsToDaemon。证据：12602f5 之后的修复。Seen 1×（2026-10-16）
- **自我 re-exec 要把内存里持有的 key 放回新进程的环境。** 原因：v0.1.0 的 serve 升级重启后报 XBOT_API_KEY is empty（启动时 key 已从环境清掉）。证据：v0.1.1 的修复。Seen 1×（2026-10-16）
- **提交前看 git status：本地二进制、日志、cookie 不进仓库。** 原因：Bot status UI 回归顺手留下的 cookie.txt、serve.log、21MB `xbot` 二进制被一并提交。证据：`ac35612`；`783411c` 带进了一份未授权错误响应 `r2.txt`，10-17 才在 `fd66d9e`（#7）里删掉。Seen 1×（2026-10-16）
- **工具 JSON Schema 的 required 用空数组 []，不要 null。** 原因：`json.Marshal` 把 nil slice 编成 null，DeepSeek 拒收，种活三个长跑 Bot 时才发现。证据：`2ed8ca3`。Seen 1×（2026-10-16）
- **改共享规则后，要用真实 DeepSeek 跑一遍认证再合。规则冲突时写明谁让谁。** 原因：10-16 加的判断/务实规则和 DeepSeek 专用提示里「把修复做完」互相打架：root-cause 只复制字体、不改 site.css；propose-plan 擅自改了快照里的配置；avatar-handoff 把给别的 Bot 留言当成「替你发消息」，停下来先问。修法是写清楚适用范围：任务里写明的改动就直接做；快照或问「怎么办」时一个文件都不写；按文档给别的 Bot 留言不算替你发消息。证据：本提交，两遍 eval 都认证。Seen 1×（2026-10-16）
- **发给模型的消息里，tool 结果必须紧跟它的 tool_calls。** 原因：一轮超过前台上限转后台后，后台的结果排在前台回复后面；DeepSeek 官方接口宽容，magpie 等严格网关直接 400，dev Bot 每次唤醒都失败、只能重建。证据：`fix/tool-result-order`，`internal/kernel/toolorder_test.go`。Seen 1×（2026-10-16）
- **归属于某个 Bot 或某个执行上下文的资源（目录、shell 工作目录），要按调用方去取，不能取「第一个」或共用一份。产品保险库已按 ADR 0041 改为整机全局，不再属于这条 per-bot 约束。** 原因：dev 第一版让入站 hub 共用 bots 目录里第一个 Bot 的保险箱验签，review 才发现（2026-10-16）；M5 第一版一个 Bot 只有一个 cwd，前台、子代理和后台分支互相改目录，`review.go` 也按 WorkDir 而不是调用方目录解析路径，第二轮 review 才发现（2026-10-17）。证据：`106f5f6` 返工前 `inboundVault`，修后 `TestConnectorInboundUsesEachBotsOwnVault`（2026-11-01 作用域变更后由 `TestConnectorInboundUsesGlobalVault` 守护共享凭据与 Bot 路由）；`2337be8`（#10）的「keep a cwd per execution context」。Seen 2×（2026-10-16、2026-10-17）
- **一批里每个提交都要能单独编译。** 原因：dev 把 serve 调用点的改动拆进 `b6839ee`，它引用下一个提交才加的 BackgroundMaxTime，单独编不过，bisect 会踩到。证据：v0.3.0 合并说明。Seen 1×（2026-10-17）
- **等待人决定的计时要挂在等待本身上，不能只靠外层流。** 原因：审批卡 60 秒随 SSE 断开失效，dev 第一版改成 24h 却在 ctx.Done 分支没有计时器。证据：`ee240a4`、`6f3ca0c`。Seen 1×（2026-10-17）
- **收益递减只数续段，不数普通工具步。** 原因：把每一步短工具调用都当成续轮后，第 4 步提醒、第 5 步硬停，25 步的状态接口停在 step 5。续段是后台因步数上限开的下一段。证据：`TestToolStepsAreNotDiminishingReturns`、`TestBotStepsEndpoint`。Seen 1×（2026-10-17）
- **同参循环只数连续重复，并按命令分开计。** 原因：失败的 `go test` 输出不以 `error:` 开头，被当成成功；`go build` 三次加 `go vet` 三次共用一个停机计数，第 6 次硬停。中间夹了别的调用要清零，成功一次要清掉失败计数。证据：`TestEditAndRetestDoesNotStop`、`TestBuildAndVetDoNotShareAStop`。Seen 1×（2026-10-17）
- **追加日志的进程内缓存要在文件 size 或 mtime 变了之后重新读。** 原因：两个进程各写一份 `reminders.jsonl` 时，先读进内存的那份一直用旧列表，冷却和去重都看漏。证据：`TestReminderCacheReloadsWhenFileChanges`。Seen 1×（2026-10-17）
- **局部读过的文件，`edit_file` 不能标成已经看全。** 原因：只读了第 1 行再改这一行，下一次整文件读取如果回执，模型就看不到后面的行。`write_file` 可以标完整，因为正文是模型自己给的。证据：`TestRangedReadThenEditDoesNotReceipt`。Seen 1×（2026-10-17）
- **短回执和「第 N 步」只在记下那次读取的同一轮里有效。** 原因：压缩把工具结果挪走之后，回执仍说没变，模型手里没有正文，步号也是上一轮的。证据：`TestReceiptDoesNotCrossTurns`。Seen 1×（2026-10-17）
- **用户拒绝过的调用只挡这一轮，下一轮要重新问。** 原因：进程级拒绝表让用户下一条消息说「改主意了」时，相同参数直接报错，不再出确认。证据：`TestDenialClearsOnNextOwnerTurn`。Seen 1×（2026-10-17）
- **按文件大小决定的缓冲不要按上限预分配。** 原因：6 字节的文件快照分配了 4 MiB 以上。哈希用流式读，缓冲按文件大小封顶。证据：`TestSnapshotSmallFileAlloc`。Seen 1×（2026-10-17）
- **`file_change` 的 diff 不能带出密钥文件的内容。** 原因：`.env`、`*.key`、`*.pem`、credentials 一类文件的提醒如果附上 diff，会把刚写进去的值送进模型上下文。这类路径只写 changed。10-18 补上另一半：保险箱脱敏只认保险箱里的值，别人写进 `config.yaml` 这类普通文件的 token 照样随 diff 进上下文，所以 diff 本身也要过一道通用密钥扫描，命中就不附。证据：`TestSensitiveFileChangeOmitsDiff`、`TestFileChangeWithholdsSecretLookingDiff`（#24）。Seen 2×（2026-10-17、2026-10-18）
- **超时不要杀掉还在出结果的命令；`sleep` 和明显的轮询除外。** 原因：到点杀进程会丢掉长构建和服务器；把 `sleep` 自动转后台又会把空转藏过熔断。证据：`TestShellTimeoutMovesToBackground`、`TestSleepIsNotAutoBackgrounded`。Seen 1×（2026-10-17）
- **后台命令的输出写在 Bot 目录里，并且先脱敏。** 原因：`/tmp` 会在重启时清空；输出文件里的密钥会在下一轮被 `shell_output` 读回来。证据：`TestBackgroundFileRedactsSecret`、`TestShellJobFileUsesKernelRedactor`。Seen 1×（2026-10-17）
- **已经报过的后台任务不能只靠进程内的集合去重。** 原因：重启后集合是空的，旧的已完成任务会每条都再开一轮模型。完成标记要写进任务记录，并且只为这个进程自己结束的任务投叫醒。证据：`TestRestartDoesNotRewakeFinishedJobs`。Seen 1×（2026-10-17）
- **追加的输出不要每次把整段尾部重写一遍。** 原因：尾部满了之后，每写几十个字节都要脱敏并重写 256 KiB。按行追加，只在超过上限时丢掉头部。证据：`TestJobWriterTrickleStaysSmall`。Seen 1×（2026-10-17）
- **未写完的输出只留可能是密钥开头的那一段，不要固定留 1024 字节。** 原因：固定窗口把短于 1 KiB 的启动横幅整段藏住，模型以为命令没有输出；窗口又必须盖住跨写入的密钥。留住的是当前尾巴里最长的、正好是某个已知形式前缀的后缀，留着的字节数写进 `held_bytes`。多个密钥按从长到短替换。证据：`TestShortRunningOutputIsVisible`、`TestRunningShortOutputAndHeldPrefix`、`TestScrubSecretsLongestFirst`。Seen 1×（2026-10-17）
- **提交日志时要按替换区间映射偏移，不能看脱敏结果的后缀还在不在。** 原因：同一段里先出现过一次完整密钥时，后缀对不上，整块连同下一次密钥的前半会被写进文件。没提交的尾部也不能先作为预览落盘。证据：`TestZZR3SecondCopyCommittedRaw`、`TestZZR3PreviewShowsPartialSecret`、`TestJobLogRedactionProperty`。Seen 1×（2026-10-17）
- **换模型名时要显式写出依赖供应商默认值的参数，并检查按模型名挂的东西。** 原因：官方新名 deepseek-flash 默认开思考（旧名 deepseek-chat 被路由到它的非思考模式），XBot 从不发 thinking 字段，开着思考带工具又必须回传 reasoning_content；模型提示也按模型名找文件，换名会悄悄丢掉 `deepseek-chat.md`。证据：deepseek-flash 切换 PR 的 `TestDeepSeekRequestDisablesThinking`，2026-10-17 实测探针。Seen 1×（2026-10-17）
- **加容量上限时，已经超限的旧数据要能往小里改。** 原因：PR #13 让每次写都检查整个 profile ≤ 4KB，线上 person profile 已有 7402 字节，forget 一条也报满，同步合并也卡住，Bot 没法逐步整理。只拦变大的写。证据：`TestForgetOnOverCapProfileSucceeds`、`TestMergeSyncOnOverCapProfile`。Seen 1×（2026-10-17）
- **按影响范围验证，不能用固定三遍代替定位并发问题。** 2026-11-01 用户取消旧“全量一遍 + 修改包三遍”要求，现以 [测试约定](docs/TESTING.md) 为准；相关检查通过即停，同一结果复用，偶发问题做定向同步/隔离检查和 race。旧固定次数门槛已被取代，不再作为硬约束。此次 web 整包三遍耗时约 291 秒，全量又重复执行 web；机械复测扩大了资源开销。Seen 1×（2026-11-01）。
- **atlas 的 verified 戳要指向 main 上真实存在的提交；squash 合并后再统一补戳。** 原因：分支上的提交在 squash 后就不在历史里了，`ba7de7f`、`617c0be` 一类戳全部悬空，每个大 PR 合完都要再开一个补戳 PR。证据：`7e8fdeb`、`2337be8` 的说明，补戳 PR #28、#35、#37。Seen 1×（2026-10-17）
- **不可信内容的围栏不能用内容自己能写出来的闭合标签。** 原因：仓库里的 AGENTS.md 能写出闭合标签跳出项目围栏、混进用户的消息（M5）；网页结果的闭合标签换大小写、加空格、用全角字符也能绕过（M6），最后改成不是标签的围栏才算收口。证据：`2337be8`、`060672d`。Seen 1×（2026-10-17）
- **接外部接口前先用真请求探一次端点、参数和默认值，再写实现。** 原因：M6 的 DeepSeek 搜索先按 Responses API 写，又改成官方 `/v1/tools/search`；默认的 DuckDuckGo 在盒子上直接被挡（202 页面），线上只能改用 DeepSeek 后端。两轮返工都是先写后探。证据：`060672d`（#31）的第 2、4 个提交。Seen 1×（2026-10-17）
- **有额度或上限的资源，要用真实尺寸的输入测上限，并测它在长任务里会复位。** 原因：常见照片尺寸在 2048 直通之前就被拒；子代理一直攥着所有工具图片，4 张的额度整个任务都不复位，前后修了三轮。证据：`060672d`（#31）后半段的图片修复。Seen 1×（2026-10-17）
- **写下的状态要有人读，没人读就删；记忆冲突就按最新的赢，不加信任级别的特例。** 原因：#33 写了每个 Bot 的 `.memory-migrated` 标记却从不读；`newest()` 让旧的可信 Bot 事实压过更新的 person 事实，用户的更正被藏住，第三轮 review 才发现。证据：`966d9f8`（#33）最后一个提交，两条复现测试。Seen 1×（2026-10-17）
- **合并草稿 PR 前先 `gh pr ready`。** 原因：草稿状态下合并会被拒，合并流程卡在半路。证据：2026-10-17 合 #33。Seen 1×（2026-10-17）
- **升级脚本用 flock 拿的锁，要在启动 serve 之前关掉文件描述符。** 原因：serve 会继承这个描述符，一直攥着 `.upgrade.lock`，下一次升级或回退报「another upgrade holds .upgrade.lock」。证据：线上 `backups/rollback-m6.sh` 的 `exec 9>&-`。Seen 1×（2026-10-17）

## 做对了
- 盒子回归用假模型和回显服务，真值只在服务端出现，transcript 和模型输入里 0 处（M7，2026-10-16）。
- Bugfix 带旧代码会失败的回归测试（`2ed8ca3`、`7ff7738`、`61f3395`、`2617bc0`，2026-10-16）。
- 提交说明把没跑的检查列成 Not run（Magpie，2026-10-16）。
- Bot status UI 用假模型 CDP 回归覆盖 running/awaiting/idle/remote/jump-to-confirm/mobile sheet（`ac35612`，2026-10-16）。
- 换默认模型前先用真实接口探路由和思考默认值，再写显式的 thinking 参数（`2062dae`，#29，2026-10-17）。
- 并行挂住的测试追到根因（共用临时目录被 TestMain 删掉、等待不认 context）再修，不加 sleep 或重试（`b252f4e`，#8，2026-10-17）。
- 超限修复只拦变大的写，带 forget、remember、merge 三条回归测试（`3a6409c`，#30，2026-10-17）。
- **定时水位线由所有轮询入口共用，并在停用过期例程前检查漏跑。** 原因：仅 serve 维护会把 chat 已投递的时间点重复列为漏跑，先 Expire 则会漏掉一次性例程。证据：C2 review，`TestChatNoticesBeforeExpiringOneShot`、`TestChatPollsAdvanceWatermark`。Seen 1×（2026-10-18）
- **切换对话必须同步清空旧视图，异步返回要核对发起时的选择。** 对话、详情和流式回复共用这个约束，避免断网时把上一个 Bot 的内容显示到新 Bot 下。Seen 1×（2026-10-18）

- **一次性邀请码要在同一把锁内先落设备表、再标已用；落盘错误不能确认收件。** Join 的本地配对与网络发送可部分成功，因此同密钥重试必须继续发送。证据：`TestReceivePair/storage`、`TestJoinRetry`。Seen 1×（2026-10-18）

- **本地发件队列的锁不能跨网络调用持有。** 否则重试超时会堵住前台入队；只在容量检查和原子写入、删除时持目录锁，重试另跑限时循环。证据：G5 `flushOutbox`、`TestOutboxRetry`。Seen 1×（2026-10-18）

- **信箱中继的目录发现用主动推送，发送哈希不包含时间戳。** 中继没有同步回复；将时间戳计入变化哈希会每分钟重发未变的目录。证据：G6 `TestDirectorySenderDecision`。Seen 1×（2026-10-18）

- **远端目录只传合法 Bot 名；worker 通知按上次活动网关去重、满时覆盖旧值。** 避免脱敏器注册前泄密、远端自由文本注入，以及 A→B→A 漏通知或慢 worker 阻塞收信。证据：`TestDirectorySenderDecision`、`TestDirectoryIgnoresUnknownFields`、`TestNotifyDirectory`。Seen 1×（2026-10-18）

- **按序列化字节数卡配额的测试要固定时间戳。** RFC3339Nano 会省略尾零，分别取当前时间会让信封长度不同，随机触发 507。证据：`TestGatewaySenderByteQuota` 修正前 300 次复跑中失败 24 次，改用同一时间戳。Seen 1×（2026-10-18）
- **新增 CLI 有值参数，要同时登记参数重排表和 FlagSet。** `provider add --reasoning-effort low` 只加 FlagSet 时，重排把位置参数 `ds` 当成 effort 值；真实二进制回归才发现。守护：`e2e/smoke/thinking_test.go` 的 `TestThinkingEffortCLI`（覆盖 low/high/max、更新保留与拒绝 medium）。Seen 1×（2026-10-26）。

- **设置开关的浏览器断言限定所在面板，同时核对 class、aria、hidden 和计算样式。** 多个隐藏设置页共享 `.tog`，全局首个匹配可能是手机推送，不能据此判定当前生图开关。头像即时刷新还要把头像字段纳入列表缓存签名。守护：`TestImageGenSettingsBrowser`、`TestProfileRefreshWhileStreamingBrowser`。Seen 1×（2026-10-26）。
- **省略工具结果必须保留同条结果的可回放环境回执。** 大结果可能已变成 512 字节 blob 预览，不能只按预览 token 判断；跨轮占位用 transcript seq，验证 expand 读回与回执回放。证据：`TestBatchBBlobOmissionKeepsReceiptAndExpand`。Seen 1×（2026-10-26）。

- **跟版提示增补也要守原压缩频率门槛。** 常驻前缀多几百字可能跨过长轮折叠阈值；先合并/精简已有规则，不为文案增量放宽频率或硬上限断言。对照行为脚本只验证接线与验收谓词，不能当作真实模型改前改后成绩。证据：上下文批 C 的 `TestLongTurnStaysUnderTheCeiling` 与 `TestBatchCPartnerBehaviorComparison`。Seen 1×（2026-10-26）。

- **保存成功与交付成功要分开回报。** hosted 生图先落工作区，再走同轮挂图；挂图超限时保留真实文件路径和失败原因，当前模型响应看不到的本机失败由程序追加到回复，不能只靠下一轮提示纠正。守护：`TestImageGenerationAttachmentFailureKeepsPathAndReceipt`、`TestHostedImageDeliveryLimitsAndIdempotency`。Seen 1×（2026-10-27）。

## 运行态与后台

- **被调用者接 current IO 后，还要用实际装配后的旧调用方守住 Reload、延迟 Ack 和附属文件。** 原因：Kernel/Timer 方法已接新路由，原 proc 仍拿旧 Inbox 做 Reload/Ack，Web cleanup 保存旧 box，missed-watermark 又直接用旧 Timer.Dir；已成功的新任务随后复活旧目录。短 IO 取当前对象，Handle/SSE/回调与 flock 等待保持锁外，不以删除空旧锁或更新旧 K 指针遮盖。守护：`TestProcRetainedInboxFollowsHandleRename` 的实际 Drain/Run 与 Timer 后半先红后 race1.830s；`TestProfileInboxAckAfterHandleRename`、`TestProfileAbsorbedReplyUsesCurrentInboxAndTranscript` 真 HTTP 先红后 race2.315s；`43e97b41`/`93ff94e1`。继续原 Run 的下一真实 Timer 后，另命中 handle 与 closeDangling.OpenTools 旧存储，`0121ad6d` 同后半 race2.021s；生产 chatConfig 四实际 callbacks 的下一输入/通知/回放也以真红到 race2.005s 修于 `63762ae5`，不以首事件后立刻 cancel 冒领下一事件已验证。Seen 1×（2026-11-01）。

- **跨目录改名的原调用要持真实文件资源，不能让退出闭包再开旧绝对路径。** 原因：组合改名已成功，Shell 的 cwd trap 仍打开旧 marker，stderr 混进同一次 JSON 回执；继承已打开 marker 的 inode、当前路径短段创建/清理，才能同时保 cwd 与严格输出。冷恢复还须在初始化 scope 前按 descriptor 原身份重基；只在最终保存 request 时改路径已经太晚。守护：`TestShellCwdMarkerFollowsRenameThroughOpenFile`、`TestProfileRestoreRebasesDescriptorBeforeScopeInitialization`、`TestProfileChangePersistsCurrentCwdForColdProcess`；独立 RenameEntry 在 `5ed71e5a` 红、`94f34c67` 绿。Seen 1×（2026-11-01）。
- **已提交的事实必须先构成回执，后续 refresh 或重新打开资源失败不能把它抹成纯错误。** 原因：实际 name/config 已保存，坏 Inbox 导致 route closed，原快照读取返回零值，CLI 与原 SSE 都漏掉已发生的移动；先保 name/previous_name/moved/applied_fields，再追加刷新错误并按同 live value 通知。守护：`TestProfileCommittedReceiptAndNoticeSurviveClosedInboxRoute`，`94f34c67` 定向通过；未宣称任意多文件 IO 全事务。Seen 1×（2026-11-01）。

- **后台仍在使用的运行入口不能随主轮结束释放，队列交接也必须保持连续持有。** 原因：serve.Pass 结束时关闭 Kernel，仍活的本机子代理随后得到空 XBOT_BIN；编码任务先从 live 删除再启动已排队任务，又会让退役检查误判空闲。复用原执行集合，真正空闲后释放，服务退出强制关闭，不另建任务账本。守护：`TestCLIProfileServeBackground` 在 `07f17164` 真二进制 exit 127、`fe7e1f83` 转绿；`TestRetiredRuntimeFollowsLocalTaskLifetime` 覆盖本机子代理和编码任务。Seen 1×（2026-11-01）。

## 配置与兼容

- **领域回包预算要服从实际下一消费者，HTTP通过不能证明模型拿到完整数据。** 原因：skills按64KiB JSON投影，实际Shell只保32KiB stdout，40KiB普通正文成功exit0却给模型截断JSON且无完整引用。复用现Shell预算只读alias决定整个JSON含换行是否内联，最终artifactreply/metadata也校较小预算；不提高全Shell或让research重定向。守护：独立 `TestCLISkillRuntimeShellOutputBudget` 在`9100c791`真source无redirect红 .94s，`e21224cf`该项 .13s与受影响预算1.10s绿；精确caller边界单位 .979s。Seen 1×（2026-11-01）。

- **绕开有副作用的Load不等于可以丢掉它原本保护的未迁移字段。** 原因：skills只读scope预检需要避免Load触发style→memory；但非迁移开关入口复用整Config marshal后，Config不认识的style被无声删掉。原config锁内只merge请求开关并保其它raw字段，不另建writer或把记忆迁移塞回route。守护：`TestNonmigratingSkillSwitchPreservesLegacyStyle` 在首`1551c71e`实际红3.168s，`9100c791`同缝绿 .375s；保偏好/未知字段与当前fresh配置，同时回执仍按真实成功项。Seen 1×（2026-11-01）。

- **改名的路径前缀要匹配同一目录的逻辑与物理拼写，不能只比 name 和词法 Rel。** 原因：合法 `/var` 或用户 symlink root 下，原 Shell `pwd -P` 已保存物理目录；mover 用原 root 拼写，未匹配就把 atName 置新，后续原 project 永久留旧前缀并误退工作根。UseBot 的新配置也不能充当原 WorkDir 来源。复用现有路径 resolver 识别同一前缀；保外部/相对 Repo 与权限 roots，不改输入去绕过合法 alias。守护：`TestProfileAliasedRootKeepsPhysicalLiveProject` 先红 .659s、race 绿1.794s，`TestProfileChangeColdCwdUsesPhysicalRootIdentity` 先红 .634s、绿 .488s；独立 ONLYResources 在 `0004ad0f` 命中同 `/var`→`/private/var` 漏迁，不将其它通过组算作该 case 已过。Seen 1×（2026-11-01）。

- **新增 fresh 读取时，消费到的字段要延续已有 Loader 的归一化。** 原因：头像读取直接解磁盘 config，遗漏 Load 对缺 name 使用目录名的既有回退，使受支持旧 Bot 的模板名字、seed 和生成器输入变空。复用归一化规则，不扩大到未使用字段。守护：`TestOldConfigWithoutAvatar` 的缺 name、模板与生成器断言在 `0053145b` 红、`3816c3bb` 定向 race 绿，并保留改名不换脸检查。Seen 1×（2026-11-01）。

- **执行入口、发现和环境摘要必须读取同一个运行态有效配置。** 原因：Manager 保留了可用 MCP 会话，installed 摘要却重新读非法磁盘配置，导致模型轮次在调用前失败。守护：`TestMCPRuntimeReconcile` 在 `aaa66c49` 真入口红，`704f584d` 统一 mcpSpecs 后该项绿；离线配置仍按文件语义读取。Seen 1×（2026-11-01）。
- **长期路由 handle 不能充当完整 Bot 配置快照。** 原因：换绑清缓存后，唤醒先构造新 Provider，又用长期旧 handle 覆盖 Bot，使 prompt/vision 与 Provider 不一致。守护：`TestResourceCurrentRouteKeepsFreshBindingAfterWakeRecheck` 在旧实现命中旧绑定，`704f584d` 在短 route 内 fresh Load 后定向 race 绿。Seen 1×（2026-11-01）。

## 界面交互

- **滚动回归要比较可见文字的屏幕位置，并真正结束 SSE。** 只检查 scrollTop 或文本存在，会漏掉控件增高与临时回复收工后补头像／名字的位移；本次长回复逐字 Range 坐标在旧布局实测下移 34px。守护：`TestChatStickSettlementBrowser`、`TestChatStickHistoryNoticeBrowser`、`TestChatStickQuoteBrowser`，分别验证持久历史／临时正文收工保位、轮询控件／失败提示保锚和引用解除跟随，均有命中缺陷的先红证据。Seen 1×（2026-11-01）。

- **新增前端模块必须接真实Server资源路由，组件夹具成功不能替代真入口。** 原因：子代理模块已嵌入并被index引用，组件fixture直接载入脚本因此通过；真实serve没有/subagents.js与/subagents.css路由，Tab选中后create undefined且空白。修复复用现有s.static，不新增第二资源来源。守护：`TestContinuableSubagentBinary` 用真实二进制+Chrome读原孩子工具结果并发反馈，`62dfada8` 诊断命中、`113c8a2f` 补路由后配合完成态可见控件驱动于 `2595697f` 转绿；只修夹具定位或补静态文件文字断言不能守住本缺口。Seen 1×（2026-11-01）。

- **消息投影里的头像和装饰必须稳定，分页锚点不能依赖会重播的位移动画。** 头像 SVG 的递增 ID 会让未变消息比较失败并替换播放器；display 切页重播 4px 动画会使历史位置偏移。守护：`TestTranscriptIncrementalBrowser`、`TestVoiceChatBrowser`、`TestChatTabsPreserveChatBrowser`、`TestTranscriptWindowVisibleTailBrowser` 与真实 serve 的 `TestChatTabsJourney`。Seen 1×（2026-11-01）。

- **后端扩窗必须一起验前端合并上限，至少用两页扩展后的 raw 规模测保尾和锚点。** 110 raw小夹具未触300行裁切，真实约200 raw两页把打开时的最新泡裁掉；控件高度也必须计入锚点校正。守护：`TestTranscriptWindowVisibleTailBrowser`（两页各300 raw、25泡、旧裁切先红）及 `TestTranscriptWindowLargeMergeBrowser`（800行触顶仍保尾并提示搜索）。Seen 1×（2026-10-31）。

- **图片手势要用真实浏览器输入验证。** Pointer capture 会把 click/dblclick 重定向到容器；触屏双击还可能附带合成 dblclick。按图片实际边界判点击、避免重复缩放，并验证取消手势后下一次空白点击仍能关闭。守护：`internal/web/testdata/chat-image-viewer.cjs`。Seen 1×（2026-10-27）。
