---
kind: part
parent: README.md
code: [internal/kernel/, internal/inbox/, internal/link/, internal/inbound/, internal/cliops/]
verified: 2026-11-02 @ 2bedef4b
---
# 内核：Link、Event、收件箱

> **30 秒版**：内核只有几个抽象：Link 是 Bot 和外界之间的任何通道，Event 是统一的事件结构，每个 Bot 一个收件箱和一个事件循环。新能力尽量用这几个抽象表达，不加新对象。

## 可继续子代理的领域入口

模型 `dispatch` 和 Web 的查看/继续/停止共用 `StartSubagent`、`MessageSubagent`、`StopSubagent`；新控制不拿主对话 gate、不对活Kernel调用UseBot。真实活动 owner、只追加子记录、逐轮结果及恢复归 exec；Kernel 负责类型/作用域、所选模型/fit和当前CLI runtime装配。父消息和自动结算仍是普通 Inbox 输入，沿环境流/Waker，不新增父邮箱或签收账；来源由调用入口固定，不让HTTP自报。详细行为、容量原边界和兼容见 [子代理](subagents.md)，结构决定见 [ADR 0045](../adr/0045-continuable-local-subagents.md)。

## 当前实例的 CLI 资料入口

`openKernel` 为已有 Kernel 绑定 `127.0.0.1` 随机端口的窄 HTTP 操作入口（`internal/cliops`），serve、桌面和独立 chat 同协议。资料 set 支持名字、简介、时区、生成头像 seed 的组合，头像 upload/clear 是独立动作；MCP 借同 Resources，不另起 Kernel。JSON profile 请求与回包各最多 64 KiB，独立头像原始二进制 body 最多 1 MiB；同用本机非浏览器、Host 与规范 root 校验。命令、字段及部分失败回执见 [多 Bot](multibot.md#运行时普通资料)。

操作入口由实际持有者释放：chat 退出、web 淘汰/换绑或服务 context 取消强制关闭；serve.Pass 的 Kernel.Close 请求退役，有本机子代理或编码任务时复用原执行集合保持入口，实际任务移除并排空原队列后的无锁 OnIdle 回调关闭空闲入口。没有第二张任务表或定期寿命轮询；已关闭入口会撤销宿主 context 回调。主线和可写本机子代理沿短 CLI 指引使用当前实例资料入口，改名后原 endpoint 仍识别同一 Bot；profile 不再登记模型工具或 schema，persona为下文独立self职责CLI，不并资料组合。入口所有权见 [ADR 0043](../adr/0043-cli-platform-runtime-entry.md)，完整资料语义见 [ADR 0049](../adr/0049-complete-runtime-profile-entry.md)。

## 技能实例入口

原 listener 以 typed read/write handlers 捕获该 source 的 Root、配置 SkillsDir、动态脱敏函数与原 Resources ref。self/停用配置和大正文产物写入在当前 route，库文件沿原排序/id解析，runtime 完整读取有20MiB上限，HTTP最多64KiB而整JSON实际投影还服从原Shell32KiB输出预算；routine 仍保16KiB。写入先纯scope预检，再按 route→现库锁→逐config锁配套文件/defaults，返回实际written/updated_bots partial；scope覆盖全部本地Bot且仅self保留，后续手动开关现读。开关写在既有fresh写缝只merge请求字段，保legacy style/未知配置，不Load进route内触发记忆迁移。两旧模型wrapper/registry/load/schema已退役，其它Tools门与原Jobs不变。协议见 [API](../API.md#技能运行时-cli)，决定见 [ADR0050](../adr/0050-skills-runtime-cli.md)。

## 例程实例与原调用审批

原listener的四个typed routine CRUD捕获actual mounted cached Timer；StartOperations先bindCurrentPaths再publish，缺Timer只本族失败。Timer自身经WithDirectoryIO持当前route，handler不外套currentIO，Update闭包不嵌bound IO；list结束后才另在当前Resources工作区存完整JSON产物。写定义后zone-index失败有真实applied/stage partial，排期/slot/Due/Deliver/emit/Inbox/Commit与水印仍原Timer，不加调度器或CRUD唤醒。人读和JSON沿同纯formatter双32KiB预算投影，协议硬限64KiB，完整产物结果限20MiB；字段与空省语义见 [API](../API.md#例程运行时-cli)。

HTTP原本没有调用turn。agent.execTool现把实际已包装approver、原Shell名/providerID/审批mutex接ctx；原Jobs.live只保Prepare后同一ctx，完成清除，CallContext只取内存Local/running/未finished，不读历史。最终Shell env携原JobID/header，listener只认自己原source，放全部锁后review/批准。main保冻结policy，卡片挂明确原owner看动态fork；child在实际Local.RunTool处明确untrusted/no-person，保它原subApprover，不冒父当前turn或new turn。缺原ctx的非空命令拒，code/remote不冒原park支持；普通CRUD及其它族不强制该关联。

仅nested审批ToolArgs附规范typed请求/effective命令，真Shell.Run raw不变。现parkable/argsKey/deniedBefore同canonical，argsfile内容或保留命令A→B不会消费A，自动routineID审后生成且canonical无HTTP/Job/provider随机数；late决定仍指向可重复的原CLI Shell。四旧模型wrapper/registry/review及指引同cut退，保其它Tools门与research纯list/help分类。设计决定见 [ADR0051](../adr/0051-routine-runtime-cli.md)。

## Bot 发现与管理实例入口

原source listener以四typed handler提供平面list/info/new/delete --self，不借离线seed/terminal owner或new Kernel。List原peer.VisibleBots排self/非空owner交集，再接原remote目录缓存7天/每设备64；Info在请求时调用原k.BotStatuses/BotSteps，保整体injected Host/Web owner视图与fallback self+Visible的差异。Web在StartOperations后才挂Statuses，不能捕publish时nil；不新统一目录/remote状态。Host完成未读计数的Count/增量Walk同样沿其原Resources当前route，cache快照/发布在route外；cold nil资源直接读transcript，不借打开/扫描Inbox；不能读retained K.Bot旧快照，segStore只读也会ensure并重建旧transcript。完整字段、steps≤10及空数组见 [API](../API.md#bot-运行时-cli)。

Create/Delete当刻当前source primary复核，Create继承全部Owners/Links（可空）与global默认binding，不继承source active model/role/budget、不新目标K或wake。原config→state→persona→owners后段以CreateCommitError/Unwrap透applied/stage，late persona32KiB仍原阶段，非整事务。资源/host/chat现回调传(removed,error)，保原同entry暂停→锁外owned Jobs join→重核→FinishDelete；removed后vaultkey错也终态清entry/hold/资源，不猜未删/恢复不存在对象，不扩大child/code/remote/MCP join。

只有strict Delete经已存在live source ctx/原wrapped Shell approve，固定Delete语义不借退掉的Known名字；production permissive不增卡、不强普通操作关联。共享runtimeApprovalSource与approveRuntimeIntent服务routine、Delete与自身职责三个真正caller，冻结policy/原owner卡branch/child原回调保持，deniedBefore即使need=false也生效；Bot canonical含delete/target，routine仍typed请求/effective命令，真Run raw不改。原call/header只是关联，不新权限/确认/任务表。JSON与human共用准确formatter双32KiB，大List/Info沿当前source工作区完整结果/真实bytes，HTTP64KiB/结果20MiB。四等价模型门退而peers单聊/fork保，聊天群沿下文独立CLI，research仅list/info/help。见 [ADR0052](../adr/0052-bot-runtime-cli.md)。

原BotHandlers另有typed Pin/Primary小写操作，平面pin/unpin NAME --self与primary set NAME --self沿同parser早分流。每次从当前source ref核fresh primary/self或可见本地目标；B已存在endpoint无需重造即下一写得权，A原endpoint不冒B资格，诊断名不参与。self pin完整短IO在route，其它目标Load/SetPinned与SetPrimary在route外；原primary.lock内pointer→ensurepinned次序保，PrimaryCommitError在真实指针写成后pin错仍Appliedtrue，source下一资格读实际pointer，不rollback/自动unpin旧A或新grant表。same-primary applied是本次指针写成，primary可unpin。两原Normal不借审批桥，短ACK人读/JSONnewline同32KiB而HTTP64KiB，不复制大结果机制；两native wrapper/空bots组/Known已退，isPrimary和其它Tools保，research只放control帮助。原配置合并、typed阶段与离线arity见 [API](../API.md#bot-运行时-cli)，决定见 [ADR0053](../adr/0053-bot-pin-primary-runtime-cli.md)。

## 自身职责与原 SelfEdit 审批

原BotHandlers.Persona为所有source Bot（含非primary）沿同ref提供persona set --self (--text TEXT|--file FILE|-)；同parser早分流/guard，完整职责file/stdin避原Shell16KiB command限，raw与escaped wire各1MiB。解码body冻结，strict在route/Jobs/host锁外沿原runtimeApprovalSource/approveRuntimeIntent显式SelfEdit needtrue，不查退名Unknown或借Delete/ownerPermissive；原actual Local/running同listener ctx、wrappedShell/providerID/mutex/冻结policy-dynamicownerfork/actualchild与deniedBefore/park/grant保持。approval-only原args附set/完整64hex SHA256(trimtext)/bytes，正文不再次复制进审批链，内容A→B不能吃A，原Run raw不变；Permissive普通写不强CallID。

批准后一次currentroute WritePersona，nil直接原source不打开Inbox，currentName/path不取旧诊断。原writer仍唯一trim后1..32KiB字节/0600/textNL/最终Rename，无新阶段/事务；Written/Effective next_wake/Error短ACK双32KiB，显示错也保written事实，不造artifact。active请求不主动重拼，下一正常Handle assemble/currentComposedPersona重新读完整职责和原卡段；环境只职责label/12hex shorthash及卡指纹，不发管理Event/Inbox/wake、改旧历史/记忆/configfallback/rules或另存版本。卡初用EnsureSeeds是原行为，比较前warm。personaTool/空bot_profile组/Known退，历史卡片和raw file SelfEdit及owner HTTP原组合partial保。字段与strict边界见 [API](../API.md#自身职责运行时-cli)、决定见 [ADR0054](../adr/0054-self-persona-runtime-cli.md)。

## 成员聊天群与原消息链

原listener的ChatGroupHandlers提供chat-group list/history/create/post --self，不和owner侧栏group或主人message/外Agent send混义。仅source当前name的member群可列/读，不用Webowner all-members；Create沿原Bus auto-self/VisibleBots本地owner交集/2..8/48字节名/单atomic meta。History原before_id exclusive、20默认50上限/filter/reverse正序与full Group4/Line5字段保，untrusted文本Quote单行；读大结果以准确JSONNL+human双32KiB预算在currentResource原work/results写FULL脱敏JSON（20MiB），不截records。

runtimeSourceCall从原live Local/running/同listener Shell Job只抽出公共lookup/取消，保prepared冻结turnKey；runtimeApprovalSource仍给routine/Delete/Persona按原owner/child重绑动态卡片。Normal群Post不用approvalIntent/新卡，只显式读prepared原tuple，不用审批重绑ctx的turnFor或latestTurn，空chain为ch_CURRENTNAME、hops+1/KindBot/原clock；原ctx无approve函数亦可，missing/completed/foreign/nonShell/无冻结turn在rate/append前拒。read/create不强ctx；source身份仅短currentIO快照，Bus/fanout在route外，artifact实际写才持当前route，不newK/ctx表/跨源事务。

原Bus.PostGroup薄error兼容至唯一PostGroupWithResult。validation/8KiB/hops≤3/8次每分钟rate后，群AppendJSONL成功才appended/message_id，逐member Load/Open/Inbox.Post成功才posted_members，skip sender/原顺序/首错停；latepartial可能群记录+B已成、C错，保failed_member/stage，不error猜没发、rollback/rate退还或自动retry。posted只是Inbox已接受，Bus无waker，原serve/proc、忙时吸收与Inbox/waker导线保持，不承诺全员已醒。四旧wrapper/Known/schema/index退、peers仍有message_bot/fork_bot，其它Link工具/Emitter/Messenger消费者不动。协议见 [API](../API.md#成员聊天群运行时-cli)、决定见 [ADR0055](../adr/0055-chat-groups-runtime-cli.md)。

## MCP 与每 Bot 进程资源

serve/chat 各创建一个 `mcp.Manager`。web 原缓存提升为 `entry{Resources, Kernel, Gate}`，Pass 与 cached/临时 checker 借同一份 `BotResources`；chat 直接持资源。只稳定原 Jobs、MCP ref 和短 Bot/Inbox route，下一 Pass 仍 fresh 构造 Provider/role/budget/恢复。runtime 摘要与CLI tools/describe/call 统一沿 Manager 保留的有效 specs，坏磁盘配置不旁路破坏该保留态；snapshot 只读缓存，reconcile 在 Pass/API/Handle/显式load 的资源锁外。wake 的最终当前名复查也在短 route 内 fresh Load 全配置，不把长期 r.current 的旧绑定盖回新 Provider。借用 K.Close 不 Shutdown 共享 Jobs；进程 context/资源最终 Close 才取消并等待真实退出。所有权决定见 [ADR 0046](../adr/0046-process-owned-mcp-sessions.md)。

默认 Jobs 的 Redact/Forms/OnDone/Now 只绑定一次，每个实际 CallRequest 保 source 的动态脱敏/Forms、Prepare/Finalize、turn/replyGen/cwd；不以最后构造的 K 覆盖。完成沿原 Jobs.Deliver 和稳定 `shelljob_ID` 进入当前 Inbox，再锁外唤醒；request.OnDone 仍覆盖默认一次。没有 JobID 路由表或旧 K 登记表。

真实解释器在 RunCall.Prepare 取 source listener 的 calls lease，完整 cleanup 最后、Jobs/cwd 锁外释放，先于 done 关闭；requested cancel 不当作实际退出。空 Pass 不受同共享表其它 source 的活动影响。原 Local/code live 集合仍保后续 Shell/队列入口；宿主 context 是强制上界。

runtime 改名沿 source operations→host index→code.Move→exec owners/Local/短 storage→Jobs.Move→route→物理移动，保同 Resources、MCP ref、Jobs、Gate 与原 source listener，不重建 Kernel 或等待模型/工具结束。name+fields 的物理提交返回实际名字、已应用字段与 moved；late IO 错误不能把仍在新目录的资源切回旧名。peer 成员仅在实际 moved 后改写，失败合入部分结果；旧 hold 随实际改名释放。

code/exec/Jobs mover 全部返回、route/storage/owners 锁释放后，只把 liveTurns 原表中同 Resources 且仍打开的实际 value 迁到新 key，收尾按保存的 key 与 value 清理，不登记所有旧 K。资料发布在 source/host/route 业务锁外调用当前 Sink，事件带 `{name,previous_name?}`；先从当前 Resources 复制显示快照到活 K，不读其旧路径，不异步替换旧 `K.Bot` 或改写它的 Provider/role/budget 快照。普通资料、头像与部分成功也沿同一通知口，不通过 Inbox 伪装用户消息。

`WithCurrentIO` 与当前目录闭包把路径选择和完整短 IO 放在同一 route 内，已接入 Bot/Inbox、Timer routine、code 记录/卡片/终态投递及 exec 子记录等写口；取出旧路径再锁外写不属于这条保证。原 proc Run/Drain 的 Reload/Head/Ack/Wait 取值、Timer 投递和 Web 共享消息/CLI event 的延迟 Ack 也沿同 ref 取当前 Inbox；原 chat 下一输入、Count/回放与通知读/已读同样接当前存储，打印和交互回调在锁外。下一事件的 handle 协调 inode 在当前 route 内打开、flock 在锁外等；dangling 恢复也只读取当前 transcript，Handle/等待/SSE 保持在锁外。Timer 在实际 StartOperations 发布前重绑当前 K，missed 水印读写及打开锁 inode 同样跟随当前目录，flock 等待与计算/投递在 route 外。WorkDir 的原目录来源不随 UseBot 配置刷新改变；内部 Bot/work 的逻辑与物理同一前缀均可重基，cwd/scope、冷 descriptor 与新进程 cwd 跟随改名，外部自定义目录和相对 Repo 保留。此范围不能推及全部记忆、媒体或提醒接口；网络、Join、Sink 和跨 K 回调不持 route。

删除初始保护检查立即放 primary 锁，暂闭新准入后在全资源锁外取消/join；最终复用原 Jobs.Move(sameDir) 的元数据锁→route→primary 作物理删除 fence。删前失败恢复同对象准入；已 RemoveAll 而后续密钥清理失败仍终态关闭。

## 语音工具与挂泡

原生 `audio` 工具组提供 `listen` 和 `tts`，经既有注册表与 `tools` 门披露。`listen` 按工作区路径主动读取音频并向已配置音频端点请求转写；`tts` 只保存 `voice/` 并返回真实路径，不挂泡、不通知、不播放。供应商路由见 [供应商](providers.md#音频端点)。

`send_message_to_user` 的 `audios` 与 `images` 走同一附件装配入口：各自最多 4 个、合计 10 MiB；同时校验两类附件，成功后一起进入所属 turn/reply generation 的回复队列。迟到调用不能挂到下一轮；最终助手行承接音频文件名，工具结果行不携带音频。纯音频调用可省略 text。这个工具只挂文件，不合成音频。


## 重启续跑（2026-10-29）

服务运行时，kernel 注册 `restart_xbot` 原生工具。它在统一权限表中归为 `Restart`，内核审查直接放行，无论事件来源和权限卡开关都不需用户确认。重启影响同一进程中的所有 Bot。服务回调先将提示与正在工作的 Bot 名单原子写入数据根唯一待续跑文件，再请求重启：桌面 app 经 `appshell.Relaunch`，serve 经现有 `reexecService`。前台处理锁忙的 Bot、或含 running/queued 子代理记录的 Bot 都算正在工作。新进程把“XBot 已重启”与用户提示作为不可信 Inbox 消息投给全部名单 Bot；每个 Bot 的事件 ID 确定且 Inbox 去重，投递完成后删除待续跑文件。部分投递失败会保留文件供下次启动重试。此机制不改 transcript 旧行、不自动修改或合入代码。

连接器的名称解析、渐进加载及错误回传见 [生态](ecosystem.md#连接器发现与加载诊断)；`links` 说明用中文指向摘要中的名称或 id，MCP 失败不标记本轮已加载。工具怎么登记、怎么经代理门披露见下文[工具注册与代理门](#工具注册与代理门)。

例程附带技能名时，`projectRoutineSkills` 在当轮 user message 投影上按序加载正文；初次 Handle 与忙时 eventMessage 吸收共用此缝。固定前缀、原事件与历史不写正文，字段和失败语义见 [多 Bot](multibot.md)「例程挂技能」。

## 边界
负责：事件进出、可信判断、一轮对话的循环、工具调度和确认路径。不负责：具体工具（见 [生态](ecosystem.md)）、transcript 存储（见 [上下文](context.md)）、权限分类（见 [安全](security.md)）。

## 怎么运转
- **解释器命令**：按 [生态](ecosystem.md) 注册具体壳名；新鲜度刷新、保护路径、只读判定、资源冲突、熔断和 Jobs 输出保留覆盖全部解释器。后台完成事件仍沿用 `shell` 来源和 `shell_job` 类型，任务记录的 `tool` 是实际解释器名，`description` 是调用用途。`bindShellSudo` 在主线 `attachShell` 与 `toolsForScope` 两个既有挂载处共用，为所有 Bot 的 Shell 提供现读 `OpenGlobal(root)` 的绑定，密码处理只在 Shell 的进程环境装配缝（见 [安全](security.md#保险箱与出站代理)），不改权限表。
- **Link**：Bot 与外部世界之间的任何一条通道。出站方向把动作暴露成工具；入站方向把外部发生的事变成 Event，投进 Bot 的收件箱。终端、Web 界面、定时器、Webhook、文件监听、MCP、浏览器、编码代理、其他机器、其他 Bot，都是 Link。其他 Bot 走 `message_bot`：同一安装里直接投进对方收件箱；写成 `bot@设备` 时由 `internal/remote` 经网关发给已配对设备上的同用户 Bot；网关连不上时进本机发件队列，工具结果说明已排队；`list --self` 也列出配对设备推送来的 `bot@设备`（只有名字，见 [网关](gateway.md)）。
- **Event**：统一的事件结构 `{source, type, content, sender, time}`。内核根据来源判断是否可信：只有用户通过经过认证的入口发来的消息才算可信，其余一律当作不可信数据包裹在 `<untrusted_event>` 里；正文里能关上或伪造这层包裹的开闭标签先被拆开（规则同网页结果，见 [生态](ecosystem.md)）。
- **收件箱与唤醒**：每个 Bot 只有一个事件处理循环，事件按顺序处理，同一事件不会处理两次，用户正在聊天时事件也会被及时处理。终端只在真正跑一轮时拿着处理锁，停在提示符上时不占锁，网页可以当场回复，也可以 fork。网页写进 transcript 的行，终端下一次刷新时补上。主循环在工具返回和无工具正常答复的下一边界，把这一轮期间新到的收件箱事件按到达顺序接进当前对话，先写 transcript 再 Ack；这些非用户事件不取消正在等的模型，下一步就能看见，transcript 的时间不会倒退。
- **用户的话直接插进正在跑的一轮**（掐话续跑；[ADR 0017](../adr/0017-owner-words-go-straight-into-the-turn.md)，`interject.go`，不分叉的内核）：主线的每一轮从 `Handle` 一开始就在进程里按 Bot 目录登记，网页的 `kernel.Interject` 把用户的话交给它，不写收件箱。正在等模型就取消这次调用、带上这句话重发（`agent.Options.Preempt`），被取消的那次输出不进 transcript；正在跑工具则结束前台等待，给同一调用的后台回执后接新话，不取消或重跑工具；全批结果仍按调用顺序封口。模型正要给出最后回答时有话进来，这一轮再跑一段。用户的原话收到即写进 transcript（[ADR 0021](../adr/0021-env-stream.md) 取代 ADR 0017 原未落盘风险），模型请求重组按统一工具配对规则排列，后面跟一条内部提示 `<owner_interjected>`：先答这句，没被改掉或取消的话再把手上的事做完（真实 DeepSeek 没有这条时只答了插话就收尾）。前台的次数和秒数从这句话重新算。这一轮的流式输出（`Sink`）换给最新说话的那条请求，之前的请求收到 `handed_off`。这一轮出错结束、话还没读到时，交回给发话的请求自己回答。另一个进程里的一轮（终端 `chat`）够不着，网页退回收件箱并保持请求打开。
- **确认卡不挡插话**（[ADR 0018](../adr/0018-cards-never-block-the-owner.md)，`park.go`）：前台在等确认卡时用户插话，等待挂起：卡照样开着，调用返回「还没跑：在等用户确认卡」，这一轮接着答用户。之后的决定也直接插进在跑的一轮，没有就投进收件箱（来源 `approval`，用户身份，内部行）并由网页当场叫醒（`kernel.SetWaker`）。允许留下一次性放行（同一 Bot、工具、参数，10 分钟，进程内存），模型用相同参数再调一次就直接跑；没有确认人的链路上放行照样有效。拒绝就告诉模型不跑；默认 30 分钟没人确认则自动允许并叫醒续跑。提问卡到点按必填推荐项续跑；迟到选择仅属于原卡，不结束其他待答卡，用户直接打字仍结束待答计时。
- **主体（Principal）**：人、Bot、群都是主体，都可以发消息、可以被授权。
- **提醒通道**：条件成立才提醒，冷却期内不重复。正文追加在它绑定的那一条消息末尾，不改写更早的消息，也不写进 transcript 正文。仓库提供的项目说明单独成一条，不拼进用户的可信正文。落在 `bots/<name>/reminders.jsonl`，写入前过脱敏；序号是刚写下的那一行，不拿事后的 `Count`。下次组装按序号原样贴回，前缀字节保持不变。日志在内存里留一份，活窗口之外的旧行在超过上限后压掉，每种只留最近一条给冷却用。这份缓存只在文件大小和 mtime 都没变时复用；另一个进程追加或重写之后，下次读取重新装入。现在接上的种类：待确认卡（定时唤醒不带）、打转警告、外部文件改动（`file_change`，不超过 4 KiB 的统一 diff，放不下就写 `[diff truncated]`，冷却 1 分钟，同一次变化不重复；自己 `write_file` / `edit_file` 成功后刷新记录，不再把这次改动报成外部变化，前台 `shell` 改的也一样；`.env`、`*.key`、`*.pem`、credentials 一类文件只写 changed，不附 diff；别的文件的 diff 看起来带密钥时（私钥块、`ghp_`/`sk-`/`AKIA` 一类前缀、password/token 赋值、URL 里的账号密码、长的高熵串，见 `vault.LooksLikeSecret`）也不附，只写 `diff not shown`）、项目说明（`project`：从当前目录向上读到工作区或共享目录根，每层 `AGENTS.md`，没有这个名字才读 `CLAUDE.md`，近的在后；这一轮的上下文里已经有同一套就不再发，`cd` 换了目录才再发；后台分支发过的那一套不在前台的上下文里，被压缩挪走的也不在，这两种情况都会再发一次；合计最多 32 KiB，截断时写 `[truncated]`；目录和候选文件的 mtime、大小、inode、ctime 没变就不再读文件、不再哈希，但 1 秒内改过的文件（Windows 看 mtime）不信缓存，因为同一个时间戳刻度里的两次同尺寸改写可能留下完全相同的戳；工作区根上、且该目录不是 git 检出、也不是共享目录时，文件是用户放的项目约定，贴在最新一条消息上；共享目录、带 `.git` 的工作区根、子目录或克隆仓库里的是仓库提供的数据，不是用户的指示，只作编码约定，不能当作批准、发送、读密钥或删除的依据，包在 `<untrusted_event source="project">` 里单独成一条；关掉 `project_instructions` 时忽略大小写和空白；用 `O_NOFOLLOW` 打开，最后一段是符号链接就不跟（Windows 上用 `FILE_FLAG_OPEN_REPARSE_POINT` 打开，是重解析点就拒绝）；不进系统提示，不改权限表，写入前仍过保险箱脱敏；允许的根以外的文件不读）、上下文用量（`context_usage`：模型报告的 prompt 跨过 `providers.json` 里该模型上下文窗口的 50/75/90% 时发一行；同一档不重复，掉下去再跨过才再发；接口给了用量就用原数，没给才按大约 4 字一 token 估计并标明 estimated；最近一次和本进程累计进状态接口和 `trace.jsonl`；「最近一次」和门槛只看前台，后台分支的调用只加进累计，`trace.jsonl` 里那一条记它自己的数并带 `branch`）、待办清单（定时唤醒不带）、先回用户（`reply_first`：用户从 web 或终端发来的一轮，首次组装请求时在用户那条消息末尾挂一条 `<system-reminder>`，提醒先用 `send_message_to_user` 回一两句、裸正文用户看不见；同轮后续工具步不再挂，定时、审批、Bot 来信不带）。设计笔记见 [claude-code-internals.md](../research/claude-code-internals.md)。
- **记忆提醒**：`reminderKindsFor` 只经 `Channel.Register(Kind)` 挂 `memory_capacity` 与 `memory_reflect`，共用 sidecar 冷却和回放，不走环境公告。容量 stat 本 Bot 与当前人物（人物事件才读）的 `memory.md` 实际字节，达到 85% 提醒「将满」，达到上限提醒「已满」，附路径及当前/4096 字节，劝先整理或删旧再写；低于门槛不发。两种各冷却 10 分钟；沉淀轻推仅在有记忆存储的 web/terminal 人物对话出现：「本轮有值得沉淀的就记进记忆，没有就跳过。」不另开模型轮、不要求复盘产出。测试：`internal/kernel/memory_reminder_test.go`（经注册/绑定观察正文，删 Register 会红）。容量写入后盾见 [记忆](memory.md#怎么运转)。
- **文件新鲜度**：每个 Bot 的活内核在本进程里记下读过和自己写过的文件，字段是路径、mtime、大小、内容哈希，以及记下它的那一轮。改或覆盖一个已经存在、但这次会话还没读过的文件会报错，让模型先读。大小或哈希和记录不一致就拒绝写入，并说明自第 N 步读过之后文件变了；只有 mtime 变、哈希没变，当成时钟抖动，允许写。大小和 mtime 都相同、且当时不是截断哈希时，跳过内容哈希：同样大小、同样 mtime 的替换会当成没变。模型自己写入成功后立刻刷新记录，工具结果写明不必再读一遍核对。`write_file` 可以把记录标成完整；`edit_file` 保持改之前的完整或局部，局部读过再改一行不会让下一次整文件读取变成回执。没有要行范围、也没有 `force` 时，同一轮里再读一个没变过的完整文件只回一句短回执（大约 30 token，第 N 步是这一轮的工具计数）。下一轮重新读正文。外部改动走上面的 `file_change`。哈希按流计算，最多前 4 MiB，小文件不按这个上限分配缓冲。记录最多留 64 个文件。状态在这个内核的生命周期里，不落盘，换一个进程要重新读。
- **被拒绝的调用**：用户拒绝过的工具调用，相同参数在这一轮里不能再试。再拿来就直接返回错误，不再问一次。新的一轮开始时清掉这份记录，用户下一条消息改口后可以再问。参数按 JSON 值比较，键的顺序不算不同。
- **对用户说话**：`send_message_to_user`（旧名 `notify_owner`，参数 `text` / `images` / `importance`）是 Bot 对用户唯一可见的通道，每个 Bot 都挂（不再依赖 timer 链接）；裸助手正文只给模型看。工具描述要求简洁、人话、好懂。调用所属轮的来源是网页或终端（用户正聊）时只进聊天气泡，回执 `sent to the owner's chat`，不写 outbox、不弹桌面；其余来源（例程、事件、子代理汇报）照旧投递通知，回执 `notified the owner`。`LanguageRule` 要求慢活前的短答和结果都经这个工具发出；`SilenceRule` 的旧措辞保留为 `legacySilenceRule` 供已存规则识别替换。确认摘要就是 `text`，网页流据此在送达时出泡。本轮已送达用户聊天时，以 `SILENT` 收尾不再被 `validateReply` 判为没回答。
- **群历史**：chat-group history沿原peer.Bus.Transcript，member范围/分页/过滤与不可信Quote、full字段和当前完整产物见 [群消息链](#成员聊天群与原消息链)；两个原只读native名已退，不画进用户聊天。
- **待办清单**：工具 `todo` 把短清单写在 `bots/<name>/todos.json`（0600）。一项有 content 和状态 pending、in_progress、done；放弃用 dropped 并写原因。同时最多一项 in_progress，最多 20 项。工具仍接收完整清单；磁盘保留完整值，成功回包只报条数摘要和相对写入前的变更项（id、内容、新旧状态和放弃原因），原样重写只报无变化。这是当前任务的清单，跨回合保留：用户说继续，或前台插话时后台还在跑，都不清掉。每一项都已 done 或 dropped、写入空清单、或新清单和旧清单没有相同 id 时，旧清单追加进 `bots/<name>/todos-archive.jsonl`，活动清单清空。状态接口带一行中文 `todo` 摘要（「进度：… · 还剩 N 项待办」或「待办：还有 N 项」，仍脱敏且最多 80 字）；全文在 `GET /api/bots/{name}/todos`，不进步骤环。空闲侧栏显示这行摘要，运行侧栏显示工具 description，轨迹按原调用与成功回包显示待办进度，展示规则见 [ui](ui.md)。提醒只在活动清单还在、且距上次成功更新已过 N 步时发（默认 6；模型上的 `todo_steps`，或内核 `TodoSteps`）。后续提醒只写相对上次提醒的差异和条数摘要；首次只点名未动的开放项 id、状态与完整清单路径，不重贴内容。冷却 2 分钟，刚更新的那一步不发。步数记在内存里，开关关掉或只是计数时不重写文件。模型配置 `todo` 为 true 或 false；缺省时供应商或模型名含 deepseek 才开，空绑定按默认的 deepseek-chat 算开。除 `links` 和 `todo` 外全是只读工具的一轮不发。定时唤醒不发这条提醒。完成门只在这一轮写过或改过清单、模型又要在还有未完成项时停下时，另起一条用户消息注入一次 `<todo_gate>`。模型最后一句是在向用户提问，或还有待确认卡时，不注入。注入的这条注记带 `origin: internal`，界面不显示。
- **慢工具后台化**：所有实际启动的工具共用原 `tools.Jobs`，前台默认最多等待 2 秒（待实测参数）；用户插话立即结束该批尚在等待的调用，不打断运行。`shell.timeout_seconds` 只能缩短等待，`run_in_background` 立即交回；shell 不再有 sleep/poll 例外。完成、等待到点、插话与取消在同一调用上裁决：只返回一次正常结果或一次含任务号和输出路径的后台回执。工具 Job 不占子代理的 3+8。确认、文件新鲜度拒写和资源排队仍先于启动。输出沿 `bots/<name>/shell-jobs/<id>.log`（目录 0700，文件 0600），最多 256 KiB；短结果普通回报，长结果只给预览与原输出路径，图片复用工作区粘贴文件。`shell_output/status/cancel` 共用这些任务，名称暂不变；取消 shell 杀进程组，其他工具请求取消，不响应 context 的调用在真正退出前仍是 running，`Close` 等实际退出。运行完成不论忙闲都以稳定事件 ID 投原 Inbox，再唤醒；无独立 shell 提醒或签收账，`notified` 只表示运输已投递，不代表用户已收到回复。runner 沿已有事件与控制接口接同一 Jobs；新进程仅观察仍存活的原执行进程，不能确认存活则标 lost，一次普通回报，不重跑。标 lost 时若记录里的 output 已不在当前目录（改名搬走目录留下的旧路径），先把 output 收束回本目录再写，仍写不进就丢弃这一条，不拖垮整张表；改名搬目录时同步改写记录里的旧路径；通过当前实例 `profile set --self --name` 改名时，内核沿用同一张任务表原地换到新目录（`Jobs.Move`：搬目录在表的写锁里完成，建档、完成写回和 Finalize 也持这把锁、按 live 路径写，所以改名窗口里不会写旧目录，也没有调用能把表指回旧名），正在跑的调用（包括触发改名的 Shell）连同 output 路径一起跟过去，在新目录收尾，不重建旧目录、也不另建第二张表；网页编辑资料改名与 CLI 共用上述 typed 领域和资源 host/mover 接缝；记录正文里的 id 必须和已校验的文件名一致，否则不读，正文 id 不用来拼路径。待写尾巴里，只有可能是某个已知密钥形式开头的那一段留在内存里，不进文件、预览或 `shell_output`。已知形式包括保险箱里的原文、JSON、URL 和 base64，这一轮暂存的密钥原文，以及收进内存、至少 8 字节的供应商密钥。保险箱变了才重新列出保险箱形式；暂存密钥和内存里的供应商密钥每次都附上，所以跨两次写入拆开的密钥也会被留住。留住的是当前尾巴的最长后缀，且这个后缀必须是任一形式的前缀；对不上时，已经完整脱敏的部分立刻写入。还留着的字节数在 `shell_output` 和 `shell_status` 里写成 `held_bytes: N`（大于 0 才写）。每次写入对整段待写原文脱敏，按替换区间把原始偏移映射到脱敏结果，只提交这段前缀之前的那一段。一个形式如果跨在提交点和这段前缀上，整段形式继续留着。结束时把剩下的全部脱敏后写入。多个密钥同时出现时按从长到短替换，一样长就按字节序，避免短的先替换、把长的剩下一段。流的 offset 只增不减。文件超过上限才丢掉头部；`shell_output` 的 offset 按整段流计算，丢掉的前缀会写明。普通回报成功入 Inbox 后写 `notified`；失败留待下次投递，稳定事件 ID 去重，lost 最多通知一次。结束的任务只留最近 20 个、且不超过 7 天。`Close` 杀掉本进程拥有的进程组，同样等它们写完才返回。记录里有 `proc_start`，重启不凭它发信号；另一个进程如果对上了仍活着的 pid 和 `proc_start`，就不会把这条改成 lost。评测沙箱仍是 30/120 秒并在超时杀掉，也不出现 `shell_output` / `shell_status` / `shell_cancel`；没挂任务表的 shell（节点、命令行）也是到点杀掉。挂上会话之后，前台 `cd` 留在这个 Bot 上，记在内存和 `bots/<name>/cwd`。后台分支按分支 id、子代理按创建时各有一份目录，继承当时调用方的目录，之后互不影响，分支结束释放；可继续本机孩子先把最终cwd同步保存到descriptor再释放scope，新轮重新绑定当前runtime；只有前台写入 `bots/<name>/cwd`。文件工具、项目说明、权限判断和后台冲突检测用调用方这一份。每次调用确认目录还在、并且在工作区或共享目录里，否则回到工作区并在结果里说明。结果在状态行后面写 `cwd:`。后台任务记下启动时的目录，不改这份会话目录。没挂会话的 shell 每次仍从自己的目录开始。cwd 标记写在 `bots/<name>/cwd-markers`（0700），不放进程临时目录。到点转后台的那次不改会话目录。`read_file`、`write_file`、`edit_file`、`view_image` 的相对路径按当时的 shell 目录解析，新鲜度记下的仍是绝对路径；`view_image` 解析符号链接后还必须落在工作区或共享目录里。评测沙箱不改根。前台跑完的解释器命令（`sed -i`、格式化工具、重定向）改了读过的文件、mtime 落在这次调用的时间窗里时，刷新记录（不算看全）并在结果里列出这些文件，不再报成外部变化；转后台的命令照旧按外部变化报。
- **熔断**：同一轮里数五件事：重复输出、同工具同参数、文件来回改、同一失败反复轮询、连续 3 次确认被拒。同参和轮询只数连续重复，中间夹了别的调用就清零，并且按命令各自计数；成功一次就清掉失败计数。只读、`go test`/`go build`/`go vet`、后台轮询放宽到 8 次。排队的控制消息另起一轮时清掉这些连续计数。收益递减这一项随续段机制一起删掉了：没有续段就没有「又开了一段却什么都没干」这种形态，后台的时间由墙钟预算管。第一次只注入打转提醒；第二次硬停，让模型总结，并把 `<stop_reason>` 写进 transcript，同时往 `trace.jsonl` 追加 `{kind:stop, reason}`。
- **悬空工具调用**：中断、取消、切换模型都走 `DanglingResult`，给没返回的调用补一条占位结果，配对不会断。

有用户话或任务结果的一轮必须给正常回复，不能以 `SILENT` 吞掉；只有没有这些输入的定时唤醒才允许静默。环境与任务结果都是原有消息，不另建转告请求或签收机制。接收原话和公布该输入的环境共用原环境发布锁；请求发送前，经 `promptinject` 的环境 Source 现采 Register 字段；time 在外置 Env 内比较已告知语义全文，有变才追加，不从对话回放时间。其余字段暂沿有效对话的 JSON 基线采样；压缩后两路一同补尾部全量快照，格式与生命周期见 [上下文](context.md)。

输出截断的合并规则见 [供应商](providers.md#输出截断)。内核把 prepare、fit、环境公布和用量记账保留在每次实际请求上，输出截断不改变 overflow 的裁剪重试。逻辑回复拼接完成后才统一剥离 `reply_to`、校验 SILENT 和工具协议文本，并沿原助手记录入口追加一次；中间片段及续写指令不进 transcript。取消和插话沿原模型请求的 context；已正常完成的回复仍优先于迟到取消。

## 工具注册与代理门

`link.Organs()` 是原生能力的唯一装配清单，CLI 启动与内核快照共用；内核原生工具始终登记，旧原生 grant 不参与开关。折叠后的请求会补完整环境快照；该请求投影从旧环境消息移除被快照取代的工具索引，保留其他字段与 transcript 原文，给最新工具结果留出空间。能力框只列外接 Link。浏览器、Telegram、飞书和 MCP 仍按安装状态挂载。

见 [ADR 0035](../adr/0035-unified-tool-registry.md)。`toolRegistry()`（`internal/kernel/registry.go`）是本轮能调的工具的唯一来源：已挂载 Link 的工具和声明式表 `kernelTools` 的每一行各登记一次，记下 source（`kernel` / `organ` / `link`）、group 和 expose。`modelTools` 把全部登记的工具包进 review 交给 agent，但只声明代理门 `tools`、`send_message_to_user`、`todo`（开着时）和 hot 名单（各个壳、`read_file`、`write_file`、`edit_file`、`web_fetch`）；其余标成 `Undeclared`，可以调用但不进 `tools[]`。

代理门的 `search` 返回 id、分组、一行摘要；`describe` 把完整说明和 schema 放在工具结果里，传分组 id 时列出成员；`call` 由 agent 层转发（`tools.Forwarder`），之后的审核、确认卡、拒绝表和后台化按被调用的工具名与参数走。三种动作都不改 `tools[]`，一轮里按字节比稳定。环境流的 `tool_index` 按分组列出 listed 的 id，`tools` 字段只列已声明的工具。资料更新与 MCP 分别走当前实例 CLI，不在模型工具表、describe 或旧前缀路由中提供执行兼容；persona沿原self CLI，空bot_profile组已退；peers余单聊/fork，聊天群四动作亦沿CLI。

子代理没有代理门：`mountedTools` 返回注册表里不是 `deferred` 的工具（懒加载 Link 在 `links load` 之前算 deferred），全部声明。`expand_tool_group` 已删除。

## 轨迹路径与脚本回放

扩展现有入口，禁止第二套轨迹账本。三处各有职责，`{root}` 是当前安装的数据根：

| 内容 | 路径与来源 |
|---|---|
| 用量、熔断及 fit 等遥测 | `{root}/bots/<name>/trace.jsonl`，`Bot.TracePath()`；不是工具步骤全文 |
| 对话与工具调用、结果 | `{root}/bots/<name>/transcript/`，`Bot.TranscriptDir()`；原有 seq 分段与 blobs，网页「轨迹」是同源投影 |
| 子代理步骤 | `{root}/bots/<name>/agents/<id>/transcript.jsonl`，`Bot.AgentsDir()` / `Bot.AgentDir(id)`；不复制进主 Bot 账本 |

环境流的 `paths` 与 `xbot [--root DIR] trace paths [bot]` 共用 `Bot.Paths()`：`bot`、`trace`、`transcript`、`agents` 均为真实存储位置的绝对路径，另有 `access` 说明访问方法。路径不变不重复公布，压缩后的完整环境快照会恢复它们；不修改固定前缀。路径可在首条遥测或任务写入前存在于约定中，对应文件/目录按原有规则延迟创建。`read_file` 沙箱在 WorkDir，读取这些路径使用 shell、`xbot trace` 或脚本。

- `trace paths [bot]` 输出一条 JSON 对象；省略 bot 使用已存在的主 Bot，空安装报错，不创建 Bot。
- `trace list [bot] [--since RFC3339] [--until RFC3339] [--kind KIND]` 将遥测按文件顺序逐行输出 JSONL。时间窗包含两端，支持时区偏移；缺文件为空结果，未知 kind 无匹配。只读打开时的文件大小，未换行的追加尾巴留给下次读取；单行上限 1 MiB，完整坏行报行号而不打印原文。
- `trace replay [bot] [--from SEQ --to SEQ]` 将 Markdown 投影流式写 stdout，序号窗包含两端；from/to 必须成对，省略则全量。直接复用 `bot.ExportTo` 与 `TranscriptExport{Tools:true}`，还原工具 blob，沿用摘要、脱敏与公开边界，不输出 reasoning 或 vendor_raw，不执行历史工具。它回放主 Bot transcript，子代理全文按上述路径另读。

list/replay 使用全局产品保险库的既有脱敏出口；命令不追加业务记录。加载仍可能触发既有配置或保险箱迁移；replay 的读取沿用原存储锁、旧格式迁移和派生索引维护，不能理解成底层文件系统绝对零写入。设计师用户视角已 PASS（tip `f16c9a6`）；本约定即 atlas 真源。

## 厂商生图

每轮开始从 `web.json` 固定全局生图开关和供应商选择；默认关。`resolveImageGen` 在装配工具目录前固定一份能力：关则两路都关；自动优先当前绑定的已登录 OpenAI/xAI OAuth Responses hosted，否则注册代理 `generate_image`；总用设置里的家则只注册代理。代理须全局所选账号已登录且目录有 Responses 模型，经 `kernelTools` 的真实 `image_gen` 分组登记为 listed，再由 `tools` 门调用；没有空组或 `expand_tool_group`。同一轮续段保留快照，设置变化下一轮生效。与搜索策略独立，完成和失败沿 `PartHosted` 进入进度与原对话。Save 只将文件保存到 `generated/` 并返回真实工作区相对路径，不写出图队列或 `Entry.Images`。Bot 读取回执后，主动调用既有 `send_message_to_user` 的 `images` 挂图；相对路径按调用时的目录解析，目录改变时需按工作区根定位回执路径。挂图沿既有 `attachOutImages` 的工作区边界、类型/尺寸校验和每条回复 4 张 / 10 MiB / 长边 2048 上限，内容相同的图只挂一次。通知调用携带原 turn 与 reply generation，迟到调用不能挂到另一条回复，同一长轮的后续回复可正常挂图。最终回复沿既有队列写 `Entry.Images`；保存成功不代表交付成功，挂图成败以 `send_message_to_user` 回执为准，拒绝不污染队列，原文件仍保留。传输、配置、保存上限及回放规则见 [供应商](providers.md)，代理复用相同保存上限和回执，权限表归 Normal，不增加工具内确认分支，不改 transcript 旧行。

MCP已移出主/legacy子/continuable子模型schema，原MCP重试/冷却退役；普通overflow的fit重试、预算校准与Descriptor实际模型路由保持。CLI/ref同session只读与结果产物见 [生态](ecosystem.md#mcp-cli只读与旧入口退役) 和 [ADR 0047](../adr/0047-mcp-cli-domain-entry.md)。

## 全局搜索策略

模型入口、工具目录、作用域工具、本机子代理和内置编码任务读取同一份网页搜索设置。策略包装保留底层模型与授权传输；远程执行端点在新任务启动时继承当前搜索能力，运行中的远端任务保留启动快照。厂商与本机的选择规则见 [供应商](providers.md)，持久化和本机搜索见 [生态](ecosystem.md)。修改设置不重建内核，不丢失正在运行的任务控制。

## 机制手册

`internal/manual/sections/*.md` 通过 go:embed 跟版，包含[记忆](../../internal/manual/sections/memory.md)、[上下文与压缩](../../internal/manual/sections/context.md)、[例程与唤醒](../../internal/manual/sections/routines.md)、[子代理](../../internal/manual/sections/subagents.md)、[插话与排队](../../internal/manual/sections/interjections.md)、[连接器](../../internal/manual/sections/connectors.md)、[文件与工作区](../../internal/manual/sections/workspace.md)。`KernelRules` 只注入短目录和先读要求；`kernel_manual(section)` 始终挂载且属于 readonly，读取已知 section 或 index，未知值报错，不读任意路径或实时状态。用户用 `xbot manual [section]` 读取同一正文。

## 共享规则

通用执行原则统一在本层，DeepSeek 内嵌 overlay 为空：`VerifyRule` 把任务材料写明的结果做完整，按结果验收；`JudgmentRule` 对齐提问与改动请求，对不运行的系统给建议，非关键细节不全时说明推断后同轮做安全部分；`ResourcefulRule` 补齐成品必需配套，标明推断。两个确认模式共用交接原则，指定渠道和既定 Bot 的交接属于执行，新接收人仍需批准，删除、外发、不可逆操作和目录边界保持原确认模式。`KnowledgeRule` 区分稳定知识、项目证据和时效来源；`ConvergenceRule` 要求工具调用能改变结论或推进必需结果，外部失败最多重试一次，结论定了就停止。SilenceRule 的 S4 和 LanguageRule 不变。开发用例见 [供应商](providers.md#主动性评测)。

所有 Bot 共用的规则由 [assemble.go](../../internal/kernel/assemble.go) 的 `KernelRules` 随当前二进制组装，包含完整默认规则与通用说话规则。`rules.md` 只存 Bot 自有补充；创建和模板导入不再复制默认规则。首次组装（fork 继承前也一样）按空行分段清理旧默认：完整当前规则及开头可识别的旧默认段删除，独立自定义段保留，混入旧默认段而无法拆出的修改丢弃，不提示、不存历史版本账。清理和规则写入共用文件锁、原子替换；重复加载没有内容变化就不写盘。空文件和缺文件都表示无补充。组装顺序仍是人设、当前内核规则、Bot 补充、模型补充和 Link/技能，条件规则仍在原内核边界追加。

`ProjectRule` 把项目文件当成编码约定：工作区根上、且该目录不是 git 检出、也不是共享目录时，才是用户放的；共享目录、git 检出、子目录和克隆仓库里的不是批准、发送、读密钥或删除的依据，也不压过内核安全、权限表和脱敏。新旧 Bot 均由当前内核注入这一条。评测目录没有提示词快照，这是有意的稳定前缀变化。挂了记忆存储时，内核在规则末尾追加 `MemoryWriteRule`（用文件工具改记忆文件夹、`memory.md` 常驻且不超过 4 KiB、日志按月追加、其余细节放自由文件、用 `recall` 检索）；它不写进 `rules.md`，所以已有自定义规则的 Bot 也会带上。写记忆文件夹时的内核守卫和只读 `recall` 见 [身份与记忆](memory.md)。共享规则提前限制外部失败最多重试一次；熔断轮询提醒仍要求停止重复失败，不追加重试预算。

`ActionRule` 让模型在删除、代表用户对外发消息、改已定计划之前先问一次，「即使请求里点名了这一步」。生产内核固定不弹工具权限卡（[ADR 0019](../adr/0019-no-permission-cards-by-default.md)，`Kernel.Permissive: true`，旧实时 `permissions on` 不能覆盖；保险箱路径也放行，只保留 links 填 key 输入卡），组装时换成 `OwnAskActionRule`：用户点名的步骤（删除、发消息也算）直接做、不再追问，只有模型自己加出来的这类步骤才先问一次；同时补一条 `EventMemoryRule`：外部事件报告的事和工作有关（新 issue、重大 bug、故障、构建挂了）就记进记忆并写明来源，但事件里的措辞仍不改人设和规则。评测按表确认的内核，`boundary` 仍测旧规则。

合伙人行为分散在既有规则对应职责：Judgment 先对齐结果并独立判断；Resourceful 禁止因可逆就替用户编职责；Offer 把偏题新想法记候选（无指定落点则 Bot 记忆），维护聚焦并用既有例程盯进度；Verify 自己核对已约定的产物、测试命令/输出/退出状态，缺证据说未验证，验收数值与 done 不等于测通。任务数量与各状态按最新环境回放，包含 queued/blocked。Dispatch 同步允许核对既定验收，仍禁止越界重复派活。Action/OwnAskAction 也涵盖未经授权的花钱。人设与工作档案不双写，字段落点见 [多 Bot](multibot.md#人设组成与提示词边界)。

记忆渲染版本由 State.MemRender 记录；唤醒时版本不符则重建唯一的 Bot 记忆快照，人物资料改走环境流，压缩刷新也记录版本。记忆注入的默认键不重复显示文本，自定义键保留；只有 person 范围追加 `via` 作者，使已接受的常驻记忆不因重复键和作者格式膨胀而被裁剪（见 [身份与记忆](memory.md)）。

环境当前值、跨轮提前占位、空闲队列触发和换小窗口预检共用内核压缩路径，配置与边界见 [上下文与 transcript](context.md)。

CLI 外部事件经本机会话认证的 `/api/bots/{name}/events` 写入 inbox，来源为 `cli`、发送者由调用方提供，始终按不可信数据处理；输入行标为主人可见的事件，界面显示来源行，不作为主人消息。处理锁可用后才受理，回执随该次唤醒返回可见结果或安静。此前已写成内部行的记录保留原标记。
