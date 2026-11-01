# XBot 里程碑与进度

## 2026-11-02 Bot 交接派生沿原 source CLI（七项所测端到端分阶段通过，待父集成）

- L档，TRUE Message main98fba5e8后仅迁fork_bot{name,role,group?}，不是bg_插话分支。fecc5b79具体合同经reviewer24 SINGLE ACTUAL CONCRETE SHAPE PASS，[ADR0057](adr/0057-bot-fork-runtime-cli.md) Accepted；A旧offline TryLock/openKernel/Backgroundctx与Web owner gate淘汰，B原source Kernel/Memory/Compactor/currentRef与preparedctx/turnSeq获选。独立bot-fork noun保旧有效fork SRC NAME arity/literal值及ownerSSE/externalAgent边界。0091b583首compiled原模型链，997baf85只退唯一wrapper/Known/空peers TOOLgroup及help/guide，原Domain/CLI首逻辑不改；现状提炼 [API](API.md#bot-交接派生运行时-cli)、[多Bot](atlas/multibot.md)。其它Memory/Context/Link/Event导线不拆。
- 唯一ForkWithResult原写序保，旧Fork error-empty兼容；Created仅原Create成功/typed Applied，Confirmed只各原nil确认，handoff_parts是原Remember nil次数/total真实960B split。原8KiB handoff与4KiB profile-cap并存，MD先atomic、index后atomic等错误可能正文已成，UncertainMemory保当前实际bot/key，不写MemoryWrittenfalse暗示零写、不stat/error串猜提交/回滚重试/抬cap暗剪摘要。源currentIO冻结State/Uncovered/projected/BOTfacts与原非空persona分支，原Focused优先/LLM无tools Chat+prepared turnSeq usage在route/hostJobs锁外；真实seq0不借latest补。Owners原pre模型、Links/三binding原post模型fresh；source note短currentIO直接当前Name/WrittenBy，group Bus在route外，不新Snapshot/MemoryTxn服务/ctx表/CAS。role为职责persona，child默认chat45min，不继承sourceDev预算/不复制person store与transcript/blocks、不额外InboxPost或启动wake。
- 最窄purebd9587dcee0cb437f8ab2df965388ec57676ef18在fecc/fullvalidation69968a75d15c3a1eff80577d320214ff6d09f5bd ONLY TestCLIBotForkRuntimeEntry FAIL.93s/package2.134s，真实API SourceForkEntrySource NativeShell exactBIN bot-fork child --self --role --json，unknown command；无fakeDomain断言，sourceDiff0/jobs私服已收封。首非空链pure13b714f13cedad812c20114dbb678d51895aa077先compile1.326s，加sourceInboxNoPost后compile1.102s无cases。开跑前按实际OpenAI Chat wire修私脚本SSE/usage：nil onDelta并不等于HTTP非stream，原Stream=true；修原Memory fixture为canonical dated格式，均PRErun准备不是业务红或产品fix。
- source0091b583/fullvalidation723d6e3d96e67aaaff836e152ab58a37179850f7 ONLY TestCLIBotForkRuntimeNonemptyHandoff首跑 PASS.51s/package4.555s，无fixturefail。source Dev2h/nonprimary/stalePrimary诊断、真实raw+已有summary+BOTfacts，经原compact.LLM ONE NoTools request实收persona/roleFOCUS/projected历史，非empty fallback；约1.7KiB handoff实际memory确认2+条及双侧认识事实、child FULL职责/OwnersLinks/三binding/RULES/defaultchat45/emptyTranscriptBlocks/sharedPerson MD EXACT不改/未copy。Web childMemoryPanel与child下一真实provider compose读到handoff；可选group sorted精确两成员/meta，双方Inbox无Post、group transcript不存在。直接Recall仅BOT不等于projected历史绝无person输入，不新过滤保证。首绿与entry红封不重，不抬997末state重跑。
- 作者首compile-only cliops.682/cmd.962s PASS；kernel初BUILDONLY因作者宣freeze后临时cause-preservation补丁的import/body非原子窗口unused clip，无case；READ当前clip确已使用，不删除或逻辑fix，只重blocked kernel.811s PASS。新CLI once cliops.632/cmd.737s，review仅RetiredBotToolsAreUnknown.529s PASS。统一新kernel14+五affected mixed race package2.832s仅三夹具失败：Rename.08s把GrantLink(timer)合法organ no-op当后配置变更；Index.03s错要求PathError而真实Rename是LinkError；ActualNextWake.07s手造Event漏Sender，在消费前即原Validate拒。产品0改，真实nonorgan/capturedpostConfig严格逐值且pre!=post、LinkError.New对应真实index路径/保MD全文与uncertain、完整NewEvent后ONLY三失败 race package1.592s PASS，其余16首绿复用。test owner在该run期间又把完整Revoke fixture切到Telegram+postLinks捕获，不能确证编哪完整版本，故仅最终Rename race package1.907s确认，另两与整组不重。四直接相关包vet/gofmt/diff0。
- 新单位实际覆盖compact.LLM no-tools8KiB Unicode handoff→真实4KiB latecap/960B ackparts、MD已成index实际FS错、typed Create state/persona及rules/source-note/group故障、legacy成功Name/Group与失败empty。实际Agent→Jobs→HTTP测missing/finished/foreign/nonShell/cancel、noapprover/allBot/Localchild、原turnSeq0/另turn、模型中真实HTTP改名仍原frozeninput/post当前rules-memory-group与原ctxusage、child provider消费及不copy person；这些producer UNIT不混独立或生产strict声称。源resolver0改，旧群三exact审批证据复用不跑。五旧author混合司机仅迁过期native索引与direct工具调用到actualSourceCall，原scriptCompactor/记忆隐私/Owners-provider-hooks与随后peer-message业务保；没有扩大原fork-bg守护/Memory/Compactor族。
- 所有必要静态fecc合同、ONLYfecc→0091首11production、ONLY0091→997末五production均PASS，无必要新问题。原SourceDomain/Handler/CLI/Memory/Compactor/oldOffline首证复用，末仅native34LOC+JSONimport/空peers TOOLregistry/Known/research genuinehelp/sourceguide；Link/Event与历史repairForkKindStartEnd不变。0d四旧describe owned仅窄迁仍live工具，产品不改其文件。997完整候选后六项见下方精确phase，不复核首Domain或抬末SHA整七。当前级别为七项所测独立端到端、全部必要静态与上述作者证据通过，父集成待收，未main/未日用。Not run：末SHA首链整跑/七项整组重跑、旧Message6/Group5/Persona4/control4/Bot5/profile5/routine6/skillsMCP/wholefork-bg-MemoryCompactor/原三approval/整包整仓/多OS矩阵、真模型/外网/性能/截图/日用/rootmain/push/release或guard；文档齐后唯一atlas审计exit0、无断链，当前25项原存类型非致命提示，无本次verified格式新警告，不扩修/不重审；Root4USERdirtydocs/ownerSSE设计不动。
- 997/fullvalidation a54ed1d994a57b647f253fa7c08570d1c53d3721后六首次包5.581s为混合结果：LateSourceAndGroup .24s、MemoryCapacityPartial .16s、MemoryIndexPartial .16s首过封（原Mac真实权限执行无skip）；CurrentDuringArchive .44s、RoleFileAndStdin .34s、ReadOnlyAndSchemas .19s仅fixture/observer失败，不能称整组六绿。Current在执行rename之前误以原projected transcript含同Shell未来rename/rule命令为冻结输入泄漏，cleanup stream错同case不冒已改名；只把冻结persona/BOTNAME old/newnameLateRule检查限于prior_summary frame，保真正模型release前profile完成及后写全断言。Input两次8192 FILE/STDIN完整职责/实际focus与child memory-personnoCopy已过，仅invalidRole把stdout typedJSON+stderr整流Unmarshal错；改Decoder读首JSON对象，非0/validation/Createdfalse/精确无archive及cfg-memory原样均保。Readonly首实际fork ACK/configpersona已过，失败因为canonical dated Markdown正文不存索引key；改真实MD全文handoff/peer-source正文及PUBLIC Recall查actualkey/WrittenBy/隐私，不mockMemory或松业务。
- pure00b4f6450320e8052ca84eca0e4924470b4a2bb6只纠上述三个owned observer，source997无改，ONLY三失败 CurrentDuringArchive .78s、RoleFileAndStdin .40s、ReadOnlyAndSchemas .46s/package2.391s exit0。Current为同REAL Shell fork PID与同Job source profile rename在LLM返回前完成，原prior persona/BOTNAME old/rawsummaryBotfacts冻结；后CURRENT rules/双认识事实WrittenBy/group成员当前名，真实usage Prompt20/Complete10/Turn原ownerSeq且currentTrace仅1，旧BOTS+WORK均缺席。Input完整8192B角色FILE/STDIN及20K trim合法职责、文件改删后冻结请求仍完整，两次真实NoToolsLLM焦点与BOTfacts；短100B模型摘要留真实4K容量，并非宣8K摘要必全成。empty/8193在typedvalidation非0/无archive，原cfg-memory不改。Readonly main/research/dev实source两archive、help/literalhelp/缺self在ops解释器前拒，dev前源-child记忆/群exact，唯一native/空peers退；ctxChild/noapprover/otherturn/seq0仍归UNIT不冒独立。
- 真实晚失败：原8KiB摘要遇原4KiB容量，child与若干part ACK已成、当前key不确定且后note/group未确认；真实MD已成index目录冲突仍保Created/parts/uncertain。LateSourceAndGroup一case两实际故障：source memory目录0500但既有MD-index-lock有效/read-exec可用，只最后source note写失败后恢复；另group目录0500仅群meta晚WRITE失败后恢复，前全部notesMemory真实已成、不rollback，原Mac非skip。与三observer失败之前可达阶段分别叙述，不拿首次Current当已完成rename，不把Input之前完整role成功或Readonly已ACK当源领域bug。
- 有效七项明确是0091/723d首Nonempty .51s/package4.555s、997/a54d后三late各首绿（包5.581s含三fixture失败，不报全组绿）、997/00b4后三ONLYfailed复验/package2.391s；不写997整七/后六重跑或合成总耗时。0d纯六commits为bd9587dcee0cb437f8ab2df965388ec57676ef18、13b714f13cedad812c20114dbb678d51895aa077、584f4a477b1211240a43e9ec142a88ed778547ed、c404ec9bcec2bb7a94a5303d6578d9cf45a08d71、ec2078e00c1266b01a2c34dcd170d94e638e52ac、00b4f6450320e8052ca84eca0e4924470b4a2bb6：八新owned及两旧readonly四describe→recall_history窄迁，无harness/产品镜像修改。准备ALLstable compile1.300s无cases，源/验证均sourceDiff0，相关后台jobs私服preparer已清；父收pure不收validation镜像。

## 2026-11-02 本地与配对设备 Bot 单聊沿原 source CLI（已合入本地 main，六项所测端到端分阶段通过）

- L档，TRUE main8caacdca后仅迁message_bot的LOCAL与bot@device两分支。合同5cdc0151由reviewer24 SINGLE ACTUAL CONCRETE SHAPE PASS，[ADR0056](adr/0056-peer-message-runtime-cli.md) Accepted；独立peer-message namespace避开用户owner message-SSE/externalAgent send与侧栏group。40fcf22d首compiled LOCAL，8472a2a5接完整REMOTE原writer结果+普通source事实并退唯一模型门；现状提炼 [API](API.md#bot-单聊运行时-cli)、[多Bot](atlas/multibot.md)。原Bus/Remote签名薄适配至唯一WithResult，原rate/owner可见范围/配对加密/重试与Receive保持，不冒local-only完整；fork/其它native/EventLink仍后刀。
- 真正的远端历史消费者先纳设计：RemoteConversation以旧native args/result恢复发送全文，Shell FILE/STDIN短ACK不能替代它。舍弃改rawShell调用/塞全文>32K结果/新Jobmetadata聚合，选原source transcript一次OriginInternal RoleUser普通事实，完整冻结请求正文经原scrub；recordTurn显式prepared.callTurnKey实际execution owner动态branch/currentref，发送则prepared.turnKey冻结chain/hops，不跟latest或approval-rebound。精确origin/tag/JSON reader与原seq派生ID保同Shell多attempt；queued优先、未知不冒sent、旧native pending/sent/queued及incoming/bounce兼容，原unpaired设备历史读门不加发送策略。无Entry新字段/假tool配对/Inbox/Event/waker/新DM账；record错误保实际Gateway/Queued/Envelope facts并Recordedfalse/non0，不回滚重发或承诺崩溃事务。
- 独立最窄pure46d4786faddb80fe89cbb548e8cdcb701dbe9491在5cdc/validation0a4fd9262a8f9933b75f74559e0724e7daaa9d5c ONLY Entry FAIL.96s/package2.220s，真实API SourceA/TargetB同owner、NativeShell frozen caller unknown command peer-message；sourceDiff0，私服/jobs已收封。首完整LOCAL在40f/validation2732898b78b57ab83c411db5e575783e1d32fe01 FAIL1.08s/package2.648s时，完整两Inbox body/KindBot/Untrusted/time/chain/hops实际已到，停于recipient provider观察器；原model.requests(ownerCue)按lastUser筛，忙时吸收的真实BotEvent排在ownerCue后导致漏选。仅私有observer3329f1cfa775696c02b96b988e7a085029344ea6改按actualTarget system BOTNAME查raw provider requests，不改共享harness/source或弱化全文，ONLY失败LocalSource PASS1.55s/package2.761s。
- 该首LOCAL实际达：nonprimary/stale primary XBOT_BOT不冒名，完整20K trim合法输入及8192B UTF8 FILE/cat STDIN两真实ShellJob→typed Posted/EventID→durable recipient Inbox→真正Target provider FULLbody；source bot/KindBot/Untrusted/time/ch_SOURCE/hops1逐值保。extraOwnerOnly共享可见，self/foreign/orphan/empty/8193/missingID拒且本次rate/target-sharedInbox EXACT不改；LOCAL Recorded=nil不加source事实。首红与首绿封不重，不抬847末SHA重跑。
- 作者首唯一统一compile-only cliops.534/kernel.683/cmd.753s无cases。新CLI一次cliops.674s PASS；cmd因并行handler临时先import后body未编译、零cmd case，fullfreeze后只补cmd新组.730s PASS，不重cliops。Remote+peer新组race2.112/1.966s PASS，真签名/加密/gateway.Handler/response-read-loss-queuefault/sameENV重试/Receive去重为单位证据，不冒子serve独立。LOCAL新实际Inbox.Post ACK/晚失败耗rate与旧Message适配单case race1.661s PASS。统一新kernel11+五受影响原driver race2.633s仅RemoteFullFact夹具FAIL.07s（近全8192控制字节JSON+密文base64超过原64KiB信封）；产品0修，保原cap，只改exact8192B混5200控制字节+ASCII、JSON仍>32KiB/实际信封合法，ONLY失败case race package1.963s PASS，其余15首过复用。
- 新source单位包含真实Agent→Jobs→HTTP、frozen sender与actual execution dynamicbranch/anotherturn、actual Local child、noapprover Normal、missing/finished/foreign/nonShell/cancel、当前rename/nilResources、fullbody/privacy、after-accept或queued后真实record FS错保facts、新unknown与not_attempted/entry1M界；不借成独立或新增strict生产测试。原runtimeSourceCall/resolver0改，群已封三exact审批consumer证据复用不重。五旧author mixed司机仅迁退役断言与fork原directnative调用到actualSourceCall，保handoff/provider/owners/hooks/记忆私密与双向Inbox业务；fork产品只改四指引文本、不迁算法。公开mock六旧产出变实际sh BINpeer-message，识别原Shell并精确忽略新增普通fact作fixture cue，两新HTTP场景.562s PASS；review三affected.645s PASS，七相关包vet/gofmt/diff0。
- reviewer24 ONLY5cdc→40f首13production STATIC PASS封，ONLY40f→847实际八production末增量STATIC PASS，无必要新问题；原CLI/WithResult/reader/common resolver0改复用首证，不重旧三approval/旧族。0d拥有七newpeer files与两授权旧GroupReadonly/stub退役迁移，产品0改其测试。后五cohesive只compile1.306s无cases准备后，source847完整validation先为0799004d998682dbe8b7efeb201127b9b3ac3aef；pure b2f25b95d537c8725c76b5d6443ab88fc6eec3ad在实际开跑前静态纠正同控制字节64K信封夹具，精确8192B混5200controls+ASCII/JSON>32K并加实际完整env≤原gateway.MaxBody断言，Notrun准备不是第二个独立红或产品修。
- source847/独立b2f后五首跑全PASS：ReadOnlyAndSchemas1.07s、RecordFailureCurrentRoute.39s（ordinary Mac真实执行非skip）、UnknownQueueFailure.27s、RemoteAcceptedBodyConsumer.35s、RemoteQueuedSameEnvelope10.14s/package13.612s exit0。真实私有TCP gateway/Ed25519Identity-X25519Identity/inviteJoin HMAC/请求与信封双签/AES配对：B首STOP且目录cache无目标仍可接受，再Receiver Run收原stableEventID/KindBotUntrusted/原chain-hops1/实际Receive时间。8192B混控制字节原请求→短ACK双32K→原source完整冻结脱敏fact/actualseq→WebSENT，再改变删除原文件也不丢正文。gateway真实accept后故意ContentLength短响应，使原同ENV原子0600 outbox durable/Gatewayunknown+Queuedtrue+Recorded全文Webqueued；原5s worker重试同ID/body/signature，seen只收一次/队列移除。不是parent假DefaultClient罩child，也无新生产flags或外网。
- UnknownQueueFailure真实gateway已accept、原outbox目录被FILE占路径使enqueue失败，回unknown/Queuedfalse/EnvelopeID/Recordedtrue/非0stageQueue，Webunknown全文、Receiver仍真收一封；不称零发送/新env确认/退rate。RecordFailureCurrentRoute同原Shell改名、stale诊断env仍写当前source普通fact/actualseq/Websent、旧BOTS与WORK均不复活；把已初始化source transcript tail设0400后，真实gateway accepted才触原Append新open失败，Recordfalse/Seq0/StageRecord非0仍保EnvelopeID/Gateway事实，故障期tail BYTES EXACT、恢复mode在原toolresult前。Receiver真收两封而source仅一合法fact，无假record函数；真实public UnpairNotice后设备mark，Web仍读旧史。只前台真实executionbranch，动态BG/anotherturn/child/noapprover/hop4/rate9与旧native历史兼容归上述UNIT，不混作独立。ReadOnly主/research/dev退唯一native、fork/read_file保，LOCAL无source事实，literalhelp不冒写。
- 本地集成：完整产品 merge `83ca8cf3`，七个纯独立提交仅收 `0a457319/2c53c3b6/4403bd37/3beb6672/a3c2e0df/3566618e/4ff2d377`，含两处授权旧依赖窄迁，不收 validation 产品镜像。CLI 只编译 .694s、smoke 只编译与 diff 通过；复用唯一 atlas 无断链结果。暂存树先精确等于已验证集成树，再仅更新本段 main 事实；四份并行文档原修改未覆盖、未代为提交，绿色业务未重跑。
- 六项有效业务phase明确是40f/3329首LOCAL1.55s/package2.761s，加847/b2f后五package13.612s各自首过；不写末SHA整六重跑或合成总时。七pure独立commits由父收46d4786f/18dd/f685/94b/3329f1cf/129a57f4/b2f25b95，不收validation产品镜像，jobs/Source-Receiver-gateway私服children已清。当前级别为六项所测独立端到端、合同/首/末必要静态和上述作者单位通过，已合入本地 main、未日用。Not run：旧群5/Persona4/control4/Bot5/profile5/routine6/skillsMCP/wholefork-outbox-collab-gatewayloops/原三approval/末SHA整六/整包整仓/多OS矩阵、真模型/外网/性能/截图/日用/rootmain/push/release或guard变更；API/atlas唯一审计exit0/无断链；初31项非致命提示含本次五node误写date-only的no-verified-commit，已按实际源码补2026-11-02 @ 8472a2a5，复用该无断链结果不重审；余26原存提示不扩修。用户原四dirtydocs/ownerSSE设计不动。

## 2026-11-02 成员聊天群四动作沿原Bus CLI（已合入本地 main，五项所测端到端分阶段通过）

- L档，Persona TRUE mainbddaf2e4之后仅迁list_groups/get_group_message_history/create_group/post_group。7bd3f3d1具体caller/typed形状单次reviewer24 SINGLE ACTUAL CONCRETE SHAPE PASS，[ADR0055](adr/0055-chat-groups-runtime-cli.md) Accepted。A ownerHTTPgroups/Load-newK淘汰，B原source typed→原Bus/currentResources，独立chat-group namespace不抢ownerFolder group或用户ownerSSE message/externalAgent send。dfe22040首compiled只读，2bedef4b完整Create/Post+退四门，现状提炼 [API](API.md#成员聊天群运行时-cli)、[内核](atlas/kernel.md#成员聊天群与原消息链)。message_bot/fork/其余native及Event/Link导线保，不借刀删总装配。
- 初独立pure68e65d837e97e1c210793933a28954aabab857f4在7bd/validationd1eb81da901adf8620a7f05c282521ea59ba81b0 ONLY TestCLIChatGroupRuntimeListEntry FAIL.93s/package2.182s，真实API source NativeShell BIN chat-group list --self --json unknown command；sourceDiff0，原日志/私服jobs已收封。纯adfcff91b7c18eb505d60188bb6742970a7fe5b9首成员链只compile1.553s无cases，完整Group4/Line5/date+readcase固定后才交产品；所有独立owned文件仅0d维护，产品0改。
- 产品dfe/validation5d001b03527d26e5c2e023b34a4e62e32fa81888 ONLY TestCLIChatGroupRuntimeMemberListHistory PASS.90s/package1.861s无fixturefail：actualSourceA MEMBER两群而Webowner allMembers不见，Webowner C-D群A非member；owner配置持久后FIRSTCLI前Restart真实装配/Links[]合法，staleXBOT_BOT=C不冒名。完整Group4/Line5/time/default20/cap50、before55 exclusive+sharedBsender+needle得25/35/45正序，45多行Quote不伪记录/untrusted true，foreign history非0且metadata/messages nil，empty arrays[]。60line仅明确historical完整NL fixture，不冒runtimePOST或wake。首业务绿封不重。
- 产品FULL2bed/validation7e0f3083a0c1657e1c7f4649d5ad2ca259e5903e ONLY remaining4：PostFanoutPartial PASS.18s、ProjectionCurrentRoute PASS.31s、ReadOnlyAndSchemas PASS.42s；CreatePostActualSource FAIL1.01s在ONEboundedRESULT复合guard，package3.443s包含失败，不能称末整四绿或产品预算bug。actualModelharness358-361 map遍历+Contains(lastUser)首匹配，初cue CLI_CHAT_POST_CREATE是三个invalidCreate cue子串，原已finished初脚本可被误选；私有纯修3e01485b4f2a68d2734c45951baf4290bac2efc2仅改初cue为互不包含CLI_CHAT_CREATED_ROOT并加splitcount/empty/outputBytes安全诊断，严格32KiB/实际业务断言保、不改共享harness/source。复验实际恢复才确认fixture归因，不据首复合guard猜source修。
- 产品FULL2bed不变/validation3e01485b4f2a68d2734c45951baf4290bac2efc2 ONLY TestCLIChatGroupRuntimeCreatePostActualSource PASS1.49s/package2.685s：非primary source env-uCallID Create可用、autoSELF/sortedDedup/fullmeta/原Visible交集与foreign拒；NativeJob实际CallID POST两次完整8192 UTF8 bytes file/stdin，实际gmLine5/time/fullbody/B,C Inbox KindBot/Untrusted/group source/current sender/chain ch_SOURCE/hops1、无self fanout，missingID/empty/8193拒typed validation非0/appendedfalse且transcript/rate/config EXACT不改。私cue修后实际业务全达，noApprover/actualchild/anotherturn/hop4/rate9仅下述UNIT，不冒独立。
- 其它三束所测：真实群append→B Inbox.Post→C完整坏JSONL NL导致Open错，message_id/postedB/failedC/stage与原ratehit保持、非0，不用未换行坏行/清路径/回滚；81群大List和8条U+0085 history整JSON约24K fit但准确Quote human>32K，原Shell同改名后四个JSON/human FULL current artifacts/实际bytes/0600及旧BOTS+WORK均缺席；原research list/history/help放行、write解释器前拒/dev真正Job post/四旧schema index退而message_bot/read_file保。原serve/proc/忙吸收/waker保持，不泛称Bus即时唤醒全员。
- 作者首唯一统一compile-only cliops.621/kernel.671/cmd.753s无cases；newCLI一次cliops.663/cmd.615s PASS。Full compile首次因我退wrapper后漏删time/link两个dead imports，kernel/cmd BUILDFAIL零业务，另cliops.563/peer.555/review.510s compile-only成功复用；只删死import后ONLYkernel/cmd3.178/3.250s compile PASS，不当业务红/编译日志新普规。统一race新kernel14组（domain/realctx/research）+四affected旧driver+三个实际受影响审批consumer中，仅新Research错把真实--help flag当写而FAIL，kernel2.160s，其余20case首过；只改TEST为--member消费--help/--后literalName，产品0改，ONLYfailedResearch race1.767s PASS，peer五新fact单位race1.793s、review四affected race1.062s PASS，五相关包vet/gofmt/diff0。未把整组初失败写成全PASS/末重整组。
- Unit实际身份与边界：Agent→Jobs→HTTP原prepared冻结tuple对比改了owner/当前另turn，actual Local child/nilapprover Normal、missingfinishedforeignnonShell与取消；old wrapper动态owner branch/child审批身份分别由TestRoutineSourceCardUsesOriginalOwnerBranch、TestBotSourceDeleteStrictApproval、TestPersonaRuntimeStrictOriginalApproval三exact受影响单位复核，不整旧族/生产strict E2E。原8KiB/hops4拒/第9次8-per-minute限、晚partial/rawerror/rate消耗/legacyKindPersonadapter，同source nil直接b无Inbox、当前rename完整artifact/tailbytes/两read humanOnly，均保持真实业务。旧五author driver只迁退役native引用/索引，保主资格/权限/所有字段等mixed要求；独立无旧owned断言命中，不造迁移或重跑旧族。
- 本地集成：完整产品 merge `fb1da631`，四个纯独立提交仅收 `22cb0c3f/71114b5c/e32d2a62/c6bb2771`，无旧 owned 文件变更，不收 validation 产品镜像。CLI 只编译 .701s、smoke 只编译与 diff 通过，最终文档 atlas 无断链结果复用。提交前暂存树精确等于已验证集成树，再仅更新本段合入事实；四份并行文档原修改未覆盖、未代为提交，业务绿色未重复运行。
- reviewer24 ONLY7bd→dfe十二production首增量STATICPASS、ONLYdfe→2bed七production末增量STATICPASS，全部必要静态齐，无必要新问题；首RO/Bus/commonresolver逻辑末0改复用。四纯独立commits68e/adfc/d5de37aa29aaca6fd62fe2cfea1dc5c013016127/3e01485b归父收六newownedfiles，不收validation mirrors。精确五phase为dfe/5d首.90/pkg1.861+2bed/7e三绿.18/.31/.42（原包3.443含CreatePost fail）+2bed/3e唯一CreatePost1.49/pkg2.685，不抬末SHA整五/估总时。当前五项所测独立端到端与全部必要static通过，已合入本地 main，未日用；API/atlas/0055提炼、母路线去完成合同副本。唯一atlas审计exit0/无断链，27项已有非致命提示size1/stale5/dev4/unabsorbed3/unmapped14，不为提示扩修或重审。Not run：末docs SHA产品/已绿/旧Bot5-control4-persona4-profile5-routine6-skills-MCP/全包全仓矩阵、真模型/远端/跨平台/性能/截图/日用/rootmain/push/release/guard变更，父原四USER dirtydocs/用户独立ownerSSE全文保。

## 2026-11-02 完整self职责沿原source CLI（已合入本地 main，四项所测端到端分阶段通过）

- L档，TRUE main6291fae1之后仅迁bot_propose_persona{FULLtext}。合同2d9d6926先核persona set --self text/file/stdin caller、A离线Load/newK与B source typed两结构、原WritePersona单提交点/下一wake/explicit SelfEdit审批，reviewer24 SINGLE ACTUAL CONCRETE SHAPE PASS；[ADR0054](adr/0054-self-persona-runtime-cli.md) Accepted。260a5bfa首compiled，5464f159完整退一个persona门/空bot_profile组/Known/readhelp+guide，原writer/Create晚检查、compositor/env与共享approval primitive完全未改。现状见 [API](API.md#自身职责运行时-cli)、[内核](atlas/kernel.md#自身职责与原-selfedit-审批)，卡/role预算/fork/消息群/用户owner-SSE不借刀迁移。
- 独立entry红7977a4dfaf3ace04cefeb9ec995d54f2b730165f在2d9/validation4dd707521c892214ea42b94cab32073e3011421c：ONLY TestCLIPersonaRuntimeEntry FAIL.98s/package2.166s，真实API非primary source先nativewarm EnsureSeeds，再BIN persona set --self --text --json触unknown command，sourceDiff0，jobs/私服收；封存不重。首file链纯425d043afa534b13d7c39f489cfe1d71d059847a与授权旧三文件迁移9471ec8d67eaa66f12b1057e3912c8a8fba64942只compile1.202s无业务；仅替旧Bot/control readonly persona describe为仍live recall_history、旧profile rename中的nativepersona写迁真实CLI Written/Effective+原持久化，原SSE/MCP/CWD/资料组合有效要求保，不重旧五/四/五族、不称更新断言已新测旧业务绿。
- 产品260/validation60b4219bf721e75829cb82a02bae48bcfb4cc4ad ONLY TestCLIPersonaRuntimeFileNextWake PASS1.04s/package2.658s，没fixturefail：真实API NONPRIMARY source先WARM默认卡/配置基线；24K级UTF8职责全文超过原Shell16KiB argv限，经实际FILE/staleXBOT_BOT=primary仍回Name SELF/Writtentrue/Effective next_wake、文件全文NL0600。写入当wake至少两次真实provider请求system全OLD、不主动重拼且正文不在NONsystem消息；NEXT真实Handle system含newFULL一次/OLD不在、原卡名+FULL卡正文保，envPersona精确职责SHA256前6bytes12hex+原卡指纹；configfallback与卡库ALLfiles EXACT保持。无新K/ctx hook/wake/getter，首绿封存。
- 首唯一统一compile-only cliops.524/kernel.681/cmd.757s，无cases；新CLI十case一次cliops.610/cmd.802s PASS，覆盖grammar/help/arity/literalflag/早root-env-file、typed PUT原header/guard、raw+wire1MiB/max+1转义、full32KiB file/stdin UTF8/raw>32Ktrimvalid、短JSONnewline32K/HTTP64K及显示错保真实written/effective。原WritePersona不复制职责校验。
- 作者新测试落盘曾两类编译误用：不存在的exported WithSourceCallID、freshBot第二返回*scripted误当Bot；初两kernel选择尝试均BUILD FAIL、零kernelcases，不当产品红。只修author tests为真实HTTP header路径及k.Bot，review三受影响case race1.591s已PASS封不重；统一ONLY十二新domain/approval+一research+十三受影响kernel driver race3.975s全PASS，四相关包vet/gofmt/diff0。原子FS失败保旧职责/0600、byte/trim边界、warm卡/compositor/hash/currentrename/nilInbox/receipt实效、严格actual Agent→Jobs→HTTP与actualLocalchild allowdeny/deniedBefore/parkA→B/sameTrim/frozenbodyrename/冻结policy-dynamicownerfork/missingfinishedforeignnoapprover/近1MiB无效体摘要bound均实际单位，非生产strict E2E；无额外strict开关/故障身份hook。
- 只迁九个受影响旧author driver：typed当前self保存原FULL正文/改名路径，或仍live recall_history/context/query保原Tool门search/describe/call/index bytes/权限等有效业务；native-only分类断言移除而rawfile SelfEdit/historicalCardText不动。未改独立owned files，不整包/整旧family复验，已有Links[]/actualsource与PickedCard首次EnsureSeeds准备基线按实际保。writer/store/source/审批未因测试误用改产品。
- reviewer24精确ONLY2d9→260十production STATIC PASS、ONLY260→546五production退门/help/guide增量STATIC PASS，无必要新问题；首260逻辑0新改复用。完整546/validation5356b2d2a224b663f517a0c318c3287519778851 ONLY remaining3首跑：InputBoundaries PASS.65s、ReadOnlyAndSchemas PASS.46s；AtomicFailureAndCurrentRoute FAIL.94s/package3.513s在BEFORE_FS old-personaEquality setup，尚未到真实writer FS失败点，不是产品bug/不能称FS已测。实际harness.CreateBot把调用fragment后追加双换行BOTNAME=name，旧persona完整文件正确存在；独立初draft错拿fragment比较。唯一纯fixture修b2cd83b9cf4784342305abefab11879bf5b262ba仅4增2删，把expected改为完整oldDuty+marker+NL，保FULL精确断言、不用一次setter替setup、不改源/clear路径/削弱FS-currentRename-nextWake与旧BOTS/WORK缺席；ONLY失败case在b2cd复验FAIL1.06s/package2.356s已到真实daemon dirpermission FS错：原WriteAtomic失败、typedName/Writtenfalse/Effective空/Error与旧完整persona/config EXACT及mode恢复全过，同Shell profile改名+Persona实际保存exit0/两JSON ACK/SAMEendpoint也过；仅observer localProfileDTO.PreviousName漏json:"previous_name"，导致后续比较空。源cliops/profile.go与accepted旧cliFullDTO标签正确。第二唯一纯fixture修9844a68f8c9e8586939b6c52d217a37553abad16仅补observer tag，不改业务字段/断言或source。产品FULL546不变/validation9844 ONLY TestCLIPersonaRuntimeAtomicFailureAndCurrentRoute PASS1.12s/package2.333s，ordinary Mac实际执行非skip：daemon CreateTemp denied、Writtenfalse/Effective空/Error+旧helper完整职责/config EXACT及mode恢复；同原Shell/endpoint profilemove+Persona ACK NameCURRENT/Writtentrue/next_wake、全文NL0600，NEXT真实Handle system新职责/OLD absent，final旧BOTS与WORK均缺席、rootPrimary不变，无清目录workaround/领域改动。两次失败分别是不同到达阶段的fixture错误，不产品bug；四项phase为260/60b首1.04/pkg2.658+546/5356两绿.65/.46（包3.513含setup失败不称整组三绿）+546/9844单Atomic1.12/pkg2.333，不抬末SHA四例重跑/合成总时。六纯独立commits7977/425d/9471/e8f4/b2cd/9844由父收，不收validation产品mirror。已绿两束与首260封不重。原首绿/entry红及旧Bot5/control4/profile5/routine6/skills/MCP证据复用，不抬末SHA四项重跑。
- 本地集成：完整产品 merge `e9f619ff`，六个纯独立提交仅收 `4e839d9b/be43facc/04447e02/8d78263a/ed7d3a8c/54427c68`，保三处授权旧预期窄迁移，不收 validation 产品镜像。CLI 只编译 1.033s、smoke 只编译与 diff 通过，最终文档 atlas 无断链结果复用。提交前暂存树精确等于已验证集成树，再仅更新本段合入事实；四份并行文档原修改未覆盖、未代为提交，业务绿色未重复运行。
- 当前级别：四项所测独立端到端按各精确phase、全部必要静态及所列作者证据通过，已合入本地 main，未用户日用。API/atlas0054提炼实际单写与nextwake/SelfEdit，母路线保后续Event/Link单独推进。按LESSONS分类复核实际source/默认卡初始化/真实ctx/既有fixture签名及并行kernel TMPDIR约束；没有新增产品缺陷教训，编译误用保史不扩全局规则。唯一atlas审计exit0/无断链，27项已有非致命提示（size1/stale5/dev4/unabsorbed3/unmapped14），不为提示扩修/重跑；Not run：末docs SHA产品/旧绿/四项整组、whole packages/repository/matrix、真模型/远端/跨平台/性能/截图/日用/rootmain/push/release或guard变更，父保原四dirtydocs/用户独立owner-SSE设计。

## 2026-11-02 pin与primary转交沿原source CLI（已合入本地 main，四项所测端到端分阶段通过）

- L档，mainac519c8d之后仅迁pin_bot/set_primary。合同f609f3ba先核flat caller/旧有效离线精确arity、A离线Load/newK与B原source typed两形状、实际primary pointer→pin写点partial及资格，reviewer24单次CONCRETE SHAPE PASS，[ADR0053](adr/0053-bot-pin-primary-runtime-cli.md) Accepted。586435ab首compiled，cc46ba7c完整退两wrapper/emptybots注册组/Known+help-only与必要driver迁移；原isPrimary/managementerrors/peers/persona/fork保，不新K/ref/事务/审批桥/wake。现状见 [API](API.md#bot-运行时-cli)、[内核](atlas/kernel.md#bot-发现与管理实例入口)。persona/卡/role预算/消息群等不借刀迁移。
- 独立最窄pin红b860b672dd2070c854f247d93ba3b6fdaa663f59在TRUE mainac/validation4ea58a066dc36293074f4f3783af20f0be89f71b：ONLY TestCLIBotControlRuntimePinEntry FAIL.68s/package2.050s，Root actualPrimary+APIpeer owner交集、真实Native Shell BIN pin Peer --self触旧singlearg usage，sourceDiff0；合法空Links不设前提，封存不重。首链test8f27196d41e361698c5ecc167c75cb2197cfb54a在合同f609/validationc0f6f45669c46ed6642abb8d0934fe017146dbcb仅compile1.185s无业务。旧独立readonly原保native pin的失效两断言仅由0d用cf9b907392afdbe084ba228973983fa4326f82e9迁source pin/仍live persona describe，不重原Bot五项、不把迁断言算新业务绿。
- 产品586/validation3c9edca6c18fb5f060c523d5993bd1f8457a817c ONLY TestCLIBotControlRuntimePrimaryTransferEntry PASS1.38s/package2.926s，没fixture修/新hook：B先nonprimary真实nativeprintf热装配原endpoint；A原endpoint pinB true配置→unpinB false→primarysetB回Name/Primary=B Appliedtrue/complete、actualpointer B/target pinnedtrue/旧A pin不改；A同endpoint并staleXBOT_BOT=B下一unpinB非0/validation/Appliedfalse，请求false及Error正确、目标config EXACT bytes未改；B预先存在的同endpoint且staleXBOT_BOT=A下一unpinA成功、actualfalse/pointer仍B。source identity不随pointer迁移、资格每次fresh复核已达；首绿封存不重。
- 首唯一统一compile-only四包cliops.554/kernel.662/bot.636/cmd.754秒，无case；新CLI控制parser/format/query/guard/实际partial先输与双模式精确32KiB once cliops3.142/cmd3.250s。actualPrimaryCommitError/原source-domain新组once bot.106/kernel.744s：真实primary.lock入锁失败vs实际pointer写成后config.json.lock目录导致SetPinned失败、同primary也Appliedtrue、原preexisting A/B revocation/gain、self/primary可unpin/旧A true或false都保、本地owner交集/foreign-orphan-remote拒、retained旧K.Bot改名currentroute、自定义binding与支持config字段合并、SaveConfig错时请求Pinned不冒已持久化、nil Inbox坏行不扫描与脱敏/短回执预算。无production故障hook、error字串或stat推提交/回滚。
- 只退役两个相关driver断言到typed selfPin及两个旧native负断言，保其余创建/persona/history/当前路径等有效业务。ONLY新research帮助guard/退Known+上述两affecteddriver race kernel2.124/review1.723s PASS；五相关包vet/gofmt/diff0。没有重旧Bot5/profile/routine6/skills/MCP族；受驱动变化的单case单位不冒旧独立族末SHA重跑。所有作者新组均一次通过，无产品或夹具失败需选择重验。
- reviewer24精确ONLY f609→586首九production STATIC PASS；ONLY586→cc46实际两门/空组/Known/readhelp/guide五production增量STATIC PASS，无必要新问题；原586 domain/CLI写点没有新增改变。完整cc46/validationc2484d9ea7ee74d3373cf4ab1f7f8bd62a9be76d ONLY remaining3一次全PASS：SelfFieldsAndLocalVisibility1.28s、PrimaryPinPartial.33s、ReadOnlyAndSchemas.39s/package3.467s，无失败/fixture修。字段在FIRSTsource前按实际配置Restart装配ALL Owners/Links0/supportfield，self原primary unpin/same-primary本次pointer原子替换+pin均到、persona/role/skills/card等保持，extraOwnerOnly共享可pin而foreign/orphan/remote字符串拒且配置/指针EXACT不改。partial沿WARM B原endpoint、实际config.lock regular观测后仅换DIR造成原SaveConfig FS错：A setB非0而typedAppliedtrue/PrimaryB/stagepin、actualpointerB、B仍unpinned/旧A保，A下一失权拒且cfg EXACT不改，B同原endpoint下一unpinA存盘，不rollback/补Bpin。freshmain/research/dev均无两schema，main-dev真实CLIpin与其它persona describe/readfile仍可用；research control help通过，三写/离线primary/operators解释器前拒且cfg/pointer零管理副作用。jobs/私服/children收，无密钥/日用动作；四个纯独立commits b860/8f271/cf9/8f2e，不收validation产品mirror。四项证据是586首+cc46三边界，不抬末SHA四例重跑/合成p95总时间。原Bot四操作、首pin红/首A→B绿、所有旧family证据复用。
- 本地集成：完整产品 merge `345120be`，四个纯独立提交仅收 `d38b0e66/92e5171c/b4bbabcd/df523984`，其中只迁受退役影响的旧独立只读断言，不收 validation 产品镜像。CLI 只编译 .797s、smoke 只编译与 diff 通过，最终文档 atlas 无断链结果复用。提交前暂存树精确等于已验证集成树，再仅更新本段合入事实；四份并行文档原修改未覆盖、未代为提交，业务绿色未重复运行。
- 当前级别：四项所测独立端到端按各自精确phase与全部必要静态、所列作者证据通过，已合入本地 main；未用户日用。API/atlas0053已提炼实际接口、主指针真实commit及两端下一资格，不把回执当永久grant；母路线仅删完成合同重复，保persona等下一族。按LESSONS类别复核了actualsource装配/Links[]合法、Loader原副作用、partial写点与双格式预算，本刀无新实证错误，不为普通成功日志新增教训。唯一atlas审计exit0无断链，27项已有非致命提示（size1/stale5/dev4/unabsorbed3/unmapped14），不为提示扩修或重复审计。Not run：末docs SHA重跑产品/旧绿、whole packages/repository/matrix、真模型/远端/跨平台/性能/截图/日用、root/main/push/release或guard变更；父保原四dirtydocs/用户独立owner-SSE设计。

## 2026-11-02 Bot发现、状态与创建删除经source CLI（已合入本地 main，五项所测端到端分阶段通过）

- L档，同母路线最窄四操作。ccdc3f72具体合同先核flat caller/两不同结构/typed DTO及实际接缝，reviewer24单次CONCRETE SHAPE PASS；[ADR0052](adr/0052-bot-runtime-cli.md) Accepted。d7a0954d首可编译 source list/info/new/delete；519bb8aa共用原审批primitive、strict显式Delete及同cut退四模型门，保pin/primary与peers消息/群/fork。原List/Info不同范围、晚挂Statuses、原Create阶段/removed事实与双格式预算均按实际，不另造Kernel/registry/任务表/事务。现状见 [API](API.md#bot-运行时-cli)、[内核](atlas/kernel.md#bot-发现与管理实例入口)，其它母路线保留。
- 独立最窄红89cea90bcf7ed5e5e9312cb9810dd0b111f61aa3在main2f/validation4a8bfd14dd8f8484c5151ecdd31d6f0e9f3a3cbf：ONLY TestCLIBotRuntimeListEntry FAIL.93s/package2.129s，真实Shell BIN list --self --json exit0却忽略flag走旧seed/全本地人读，不把exit0当runtime已通。该红封存不重。
- 产品d7/validationf3f0e95cb59e7a131a935c2233911c1bff543d30 ONLY PrimaryCreateDeleteEntry PASS1.52s/package2.731s：读actual primarypointer与APIownerintersection，真sourceflatlist/info named+steps2/new trim明确职责/list发现80bytepreview/info cold目标动态Host，actual config/state/persona/ALL Owners与Links（空也合法）/globaldefaultbinding/默认rolebudget，原source Delete removed/complete、最终目标目录与list缺席/Peer和primary保。sourcebinding恰同globaldefault，此case不冒已区分customsourcebinding，另有作者单位区分。
- 首链fixture历史保真：d7/843a.84s/package2.468s是owners-or-links复合setup断言未到runtime，不能据此断定缺owner；独立89dfbc55987ba3c99b224fa9859606127fe0710b再.85s/package1.571s明确APIpeer Links空合法。最终只去不在合同的非空Links前提，必要实际owner缺失才读后补并保旧全部字段/primarypointer，原Links[]仍严格比较；不是产品bug，不改域默认/DTO/预算。
- 首编译五包仅无case：cliops3.144/cmd3.260/kernel3.179/bot3.148/web3.262；CLI子更早重叠compile.551/.758同为编译非业务。必要pureparser限定只有new识别原persona/provider/model值arity，ONLY新parser.644s；其余新transport/formatter/input/CLI一次cliops.706/cmd.795，完整typed状态/token/step、onlyDelete CallID、64KiB newline/guards/no redirect、max+1/转义1MiB、>32KiB职责不偷提前检查、partial先输出与两种最终32KiB guard通过。
- 实际bool host补缝新测试ONLY RemovedFact/FailureFact web.290s：direct资源、同host entry及cold path/成功与真实post-RemoveAll target.key移除错误，removed=true/原PathError/终态准入与entry/hold清理，不stat猜；primary保护/cancel/原迁移失败/错target返回false。旧mutation MCP测试仅签名适配未重跑。CreateCommitError两新case bot3.159s：default_binding/config/state/persona真写点、晚32KiB前config/state已提交与ErrExists/Unwrap保。
- 新runtime域六项首kernel.684s有五PASS，仅Human-only预算fixture200条不足32KiB在assertion止，之前该case原route/List/Info完整artifact已达；只调256使实际JSON≤32K、人读>32K，ONLY failed TestRuntimeBotArtifactsCurrentRoute .681s绿，未重五绿。覆盖原owner/remote7天inclusive/每设备先64/无额外filter、动态Host全部字段/private排除/锁外回调、custom sourcebinding不继承/globaldefault/allOwnersLinks、owners故障partial、rename当前workspace及完整脱敏产物/20MiB拒绝。先前错过共享符号移动/未用import与一个新test编译类型笔误都未执行业务，不当红或产品bug。
- 十个旧driver迁typed/actual Handle→真实Shell Jobs→源env/parser→CallBotCreate，原persona dedup/历史tool pairing/明确用途保；其它Tools门和忙时absorption改实际recall_history standin。13项affected race3.796s中十PASS、三个新迁移夹具失败（provider缺APIKeyEnv名字、forwarded c4工具结果仍Name=tools、obsolete fakeMCP人为挂载）；只修三夹具并ONLY三race3.056s绿，不声称整13末SHA复跑全绿。故意保历史fake type供其它原断言，不恢复MCP模型策略。
- 必要strict/shared单位race kernel1.939/review1.585全PASS：Bot allow/deny、missing/nilapprover/completed+permissive无需origin、park delete target A→B不误消费/A同意grant保/B拒复试不问/同A消费，research在解释器前仅list/info/help；新routine denial在need=false仍拒。仅共享接缝影响的原routine三个单case重验（park跨providerID/dynamic original-owner card/actual Local原child allow+deny）通过，非整个routine族重验。strict/park/child、阶段与vaultkey fault为作者单位，不称独立生产strict E2E或全部children join。七相关包vet、gofmt/diff通过，末尾仅去孤立旧4KiB renderer常量。
- reviewer24精确ccdc→d7首产品STATIC PASS；ONLY d7→519实际共享ctx/explicitDelete/四门退/readonly增量STATIC PASS，无必要新问题。原Jobs/listener live/owner分支、HTTP取消、denied先于need、原full approver、Host序列未动及scope保留均逐项核。完整519/validation9d7da14cc469db96b9f15fdbaa086902d80e51b5余四首跑：ReadOnlyAndSchemas.23s与ListAndHostInfoVisibility.21s PASS封存；CreatePolicyAndHostDelete.94s在复合继承断言止、ProjectionCurrentRoute2.75s在首Info读取粗generic断言止，package5.702s，最初不猜产品/夹具。519/afb521fe ONLY这两项有界诊断.99s/2.73s/package5.001s后确定：binding deepseek/smoke、role空/budget0/persona/Links0对0全正确，仅owners实际1/预期2；serve启动ALLcfg reconcile已先ResourcesFor造原entry，后直接disk改2owners不能代表runtime，须持久完再真server Restart正确装配。Projection实际是原Shell合法background sh_* callcontinues回执，不是CLI非0；应等原Job实际done，再仅原shell_output(id)一次完整≤32K/noDropped/noOffset取DTO继续全文产物断言。两处均仅fixture调用/装配修正，不改产品/预算/Root/严业务。同519/修fixture11eaa21922f9b1a7674d2123ae98ffcb239f1e71 ONLY2复验：CreatePolicyAndHostDelete PASS1.35s封存，证明FIRSTruntime前Restart正确装配后的ALL2 owners/Links0、customsource provider/roledev/budget1234不继承而global deepseek/smoke/role空预算0、实际persona-file/nonprimaryCRUD拒/blank零创建/late32KiB applied:true stage:persona cfg+state已在而persona缺席/primary保护；WARM真实Native PID+Job running后，原source HostDelete removed/complete时该owned进程停止、目标不复活/Info无，不泛称所有children/code/remote/MCP均join。Projection2.78s/package5.458s已达四输出（真实background等done再native shell_output）、完整typed artifact/currentpath/双格式32K/20M等业务，末尾旧source PATH ABSENCE仍失败未分bots/work，独立只增强最后安全kind/entry诊断再ONLY此case，不清目录规避、不改no-resurrection期待；Remote独立NOTRUN（devices会触5秒remoteLoop真实网关，不为此造hook/外网），原Person/7d/64准确实现方TestRuntimeBotListSourceVisibility已在.684s那组PASS复用。最终结果按实际phase追加；首d7/List红/routine6/skills/profile/MCP旧绿不重。
- 唯一实际产品缺口已定位到Host未读completion reader：519/独立438eac1d6d98e1c76a044f4ac4f4696513139104 ONLY Projection FAIL3.60s/package4.812s，旧work确实无，旧bot只有transcript与lock无config，其它四读/完整artifact/currentpath均到。该形状单独不能断言Append/异步；segStore所有Count/Walk等也WithLock→ensure建目录。只读核原独立纯HTTP/FS/helper没有旧Bot读，产品链为实际Info→动态Web statusSource→botStatuses先k.Status(current)再unreadCompletions(k.Bot旧快照)→Count旧store初始化；same source/entry rename原设计保旧K.Bot，故不是Job/SSE/wake writer。不散改这些路径。
- ddda18e6只修同reader：unreadCompletions接原k.Resources，Count和增量Walk各完整短IO经现withInboxIO/当前route，name取当前Bot，原cache s.mu查/发全部route外，不反转host→route、不换K.Bot或新registry/cache表，cold nil资源/cursor-through保。新增samecachedsource/entry RenameResources→原BotHandlers.Info(realstatusSource) case在519先RED.03s/package.880s，正确completion count与rawoldpath缺席均失败；修后ONLY此case.05s/原签名受影响ownercompletioncursor.08s，race package2.033s全PASS，webvet/diff/gofmt0。ddda/独立validation99ef30dfb1a57a2f8b5be402b4ba4fb16852494f SAME438e严断言ONLY Projection一次PASS3.67s/package5.051s，未修改fixture/删目录/调预算；旧WORK和旧BOTS同时缺席，原4JSON/human完整typed artifacts/currentresults/实际bytes/20MiB/full identities/preview/coldSince/state/Count/Hostscope与Native后台实际done/exit0/noDropped/CURRENTshell_output完整offset各断言全到。job/私服/children清理，独立八个纯test commits不变。四已有绿不重；reviewer24仅核实际reader增量。
- 最后reader增量reviewer24确认资源路径修复形状正确，但指出必要cold P2：ddda复用withInboxIO在resources=nil/box=nil时会Open/全fold unrelated Inbox，cache-hit/cursor-complete也新增IO，坏Inbox可把合法transcript未读变0，违反原fallback。9b013438只让nil直接原b.Count/Walk，非nil直接同r.WithCurrentIO（与旧helper非nil原转发相同），cache锁与currentname保，不动holder或其它生命周期。新TestColdCompletionReadHasNoInboxDependency首author fixture少newline被原JSONL当半尾忽略，.02s/package.772s未命中不冒旧bug已绿；完整坏Inbox行后ONLY新case在ddda RED.03s/package3.288s。修后ONLY新cold.06s/原owner-cursor.08s，race package1.478s全PASS，webvet/gofmt/diff0。原资源case和ddda独立Projection复用不重/不抬9b末SHAE2E；reviewer24仅ddda→9b这项nil增量正式STATIC PASS、唯一必要P2闭合；nonnull先前成立资源路径与cold原fallback均保，无其它必要问题。
- 本地集成：完整产品 merge `35131922`，八个纯独立测试提交只取为 `27d097f9/bcbfadc7/e0949f70/2f180c66/9b35be22/20991147/7cd200c1/04617345`，未收 validation 产品镜像。CLI 只编译 .256s、smoke 只编译与 diff 通过；最终文档 atlas 无断链结果复用。提交前核暂存树精确等于已验证集成树，再仅更新本段合入事实；四份并行文档原修改未覆盖、未代为提交，业务绿未重跑。
- 当前级别：五项所测独立端到端在各自精确phase通过（d7首链、519只读/可见/生命周期、ddda Projection）；不是五项在末SHA整组重跑。所列作者证据与全部必要静态增量（d7、519、reader/cold9b收口）通过，已合入本地 main；未用户日用。API/atlas/0052与母dev已提炼，教训限定本项目source夹具合法空集合；唯一atlas审计exit0无断链，27项已有非致命提示（size1/stale5/dev4/unabsorbed3/unmapped14），不为warning扩修。Not run：末docs SHA全组重跑、whole packages/repository/matrix、真模型、跨平台/性能/无关旅程、日用实例；不push/发版/重启或改guard，root/main及用户dirty文档原样由父持有。

## 2026-11-02 routine actual Timer CLI 与原调用审批（已合入本地 main，所测端到端通过）

- L档，同母路线下一具体族。合同3e3067d4先列实际caller/typed DTO/原ctx连接、A离线new Timer与B原listener actual cached Timer两结构，reviewer24单次SHAPE PASS；[ADR0051](adr/0051-routine-runtime-cli.md) Accepted。43f21bbf首可编译CRUD，3c1b7d8e修唯一新消费者P2，6983ac09完成原call审批/同cut退四模型门；正常merge父main a389后31a0a0a9只收MILE事实、无产品差异。现状见 [API](API.md#例程运行时-cli)、[内核](atlas/kernel.md#例程实例与原调用审批)；母dev其它族保留，已完成routine合同不再两处重复。
- 最窄真红封存：base c5605860/validation c4046a7f9399ca14979c9942feb3b210c462c782，ONLY TestCLIRoutineRuntimeListEntry .93s/package2.730s，真实source BIN不识别--self。首产品43f21bbf、fixture/validation1228f3e4006470df640ebc82f77ff81e28e982c9的ONLY CRUDEntry一次PASS1.14s/package2.323s：实际六Shell create args/list/update argsfile/list/delete/list，typed applied/stage/id/count、defaultenabled/cronlocal、有序重复missing refs、false/[]、未请求At/Cron/Zone/LastSlot保持与最终definition缺席全到。首次validation4629f398的1.13s/package2.127s只命中新fixtureannual cron在原MinGap固定窗口不足两次，作者三处改下一本地小时daily，不改产品validator或业务严断言；首绿随后封存。
- 唯一产品必要P2是合法no-json实际输出。43f/validation1228新ONLY PlainListBudget红.99s/package2.281s：290合法disabled once规范JSON27287B可fit，第二实际Shell人读exit0但32781B被clip，无完整引用。3c1b7d8e仅让server/CLI共用准确纯FormatRoutineList，JSON+newline与人读、最终artifactmetadata同时核现32KiB；HTTP64KiB、原小列表格式保持。3c/validationb9d8f324cfaa3bc48df0efa8c2f58707c13bb5c0 ONLY该项一次PASS1.07s/package2.578s，JSON提前artifact合法；两实际caller完整current results/Bytes=文件字节、290id/name/prompt/AtUTC/ZoneUTC/disabled及空字段逐个精确、无dup/clip。独立作者另纠自己的错误永久inline前提，保canonical JSON≤32KiB和完整业务，产品方未改独立文件。
- 作者预算证据独立于上述E2E：精确43f归档+仅新增handler case红.07s/package3.250s（300项JSON25227/human35111/File空），只三个新case绿kernel.217/cliops.118/cmd.235，完整300产物25227B/人读引用87B。并发退役期间两次compile block没有跑到业务断言，不当红或产品bug，归档已清。
- 完整产品31a0a0a9、validationd34df765d2004f41f490a776e4ef2aee48b394e0 ONLY余四：LargeListCurrentRoute.18s、ReadOnlyAndSchemas.23s PASS封存。CurrentTimerAndGate1.25s、TypedOnceLifecycle.14s/package3.254s首失败仅两个新fixture前提：机器off观察后未恢复就调用受原Shell gate的clear，及将AtUTC误推once.Zone必须UTC。作者各移/改一处fixture，产品/原业务不变；validationa44390a4239e980f7286938927be7e6825f379e6 ONLY这两项PASS1.45s/.79s/package3.564s。六项按43f/3c/31a精确phase封存，不称末docs HEAD重跑全六。
- 余四实际覆盖：原Job非空precheck创建→rename原endpoint/retained命令update→缺CallID拒且定义exact不变→live machineoff Due/Deliver消耗once slot/disable、无proof/Inbox→恢复allow→坏CallID清空命令仍成功→sameID rearm与actual allow proof仅一次/唯一timerInbox/Commit/dedup/旧目录不复活；At offset+白字规范UTC、原空Zone/default/trim/duplicate skills/null省/blank保持/false/[]、once+cron冲突及未知At/Zone/LastSlot/缺schedule/过期拒零改/sameID覆盖；40KiB无redirect当前artifact与改名旧BOT诊断；实际main/research/dev三wire无四旧schema/index/指引、read/help允许/write和operator零副作用、其它Tools Peer及read_file保留。定时E2E只量proof/唯一Inbox/slot提交，未单数模型出字或waker次数；waker源码保原不能冒E2E已量。
- 实现方必要单位：CLI首次parser/input/transport/entry定向cliops.192s、cmd.645s；domain七项初link.442s/kernel.781s仅missing-Timer夹具误以有Bot不会自动mount，改Kernel{} ONLY该项3.214s绿，产品不变。原Agent/真实Jobs/Shell三新race agent1.574s/tools1.572s，保原wrapped callback/name/providerID/Run raw、prepared ctx/运行态/历史拒/complete/cancel清除/最终env。12个受影响Timer/旧driver迁移/注册表与actual Local child回归race5.522s；child真实StartSubagent→Local→Jobs→HTTP，main可信但nested明确untrusted/no-person，用原BackgroundApprove允许/拒绝，不借main callback，CRUD不执行marker。
- 原source严格审查/同表/duplicate-null/kept-command/foreign-completed/原owner动态fork/readonly在解释器前拒组首kernel6.056s中仅park fixture原sender为空导致既有Inbox Event.Validate拒迟到消息，review1.645s PASS，其余选中均绿。仅补真实sender，ONLY TestRoutineSourceParkGrantBindsEffectiveFile .03s/race package1.797s绿：停放不写、argsfile A→B不消费A、B拒后二试不再问、同A跨新providerID消费原grant、自动routineID审后生成。zone-index postdefinition typed partial为域单位，strict/park/child/zonefault均非独立生产strict E2E；没有新strict开关、身份hook、Job表、调度器、CAS/新确认协议。七相关包vet、gofmt/diff通过。
- reviewer24仅实际3c→698原ctx桥与退门增量正式STATIC PASS，无必要新问题；早期43f的cachedTimer/bind-beforepublish/锁/typed/partial/输入静态已PASS，43f→3c准确双formatter/真实reference长度/metadata预算增量PASS。追到原web审批只decision/timer闭卡、不因Job完成抹掉park后迟到卡，原owner/fork、child回调、canonical denial与complete ctx均闭合。atlas/API/0051/dev/LESSONS同步，唯一atlas审计exit0无断链，27项已有非致命提示（size1/stale5/dev4/unabsorbed3/unmapped14）；不为提示扩修或新开matrix。独立只收f29761cd/70b1272e/9b033c70/1228f3e4/28209abc/3e3a390d/a44390a4七纯fixture提交，六newfiles，不收validation产品镜像或merge；本次只提交本刀变更，保留并行 main 文档未提交修改。
- 本地集成：完整产品 merge `8ab632ad`，七独立测试只取为 `7c95f9d3/66ff3cf7/819482bd/034d87a1/c4a3b9d4/922592aa/0bac7adb`，未收 validation 产品镜像。集成 CLI 只编译 .695s、smoke 只编译和 diff 通过；最终文档 atlas 无断链结果复用，业务绿未重跑。提交前核集成树精确一致，选择性暂存保四份并行文档原修改未提交。
- 当前级别：已合入本地 main，六项独立所测端到端与实际新增静态通过，尚未用户日用；所有作者/独立job与私服均已收。Not run：已绿首CRUD/Plain/余项重复、skills6/profile/MCP旧组、whole package/repository/完整矩阵、真模型、跨平台FS、无关旅程/性能、实际用户日用；不push/发版/重启日用实例，不擅改guard或其它族范围。

## 2026-11-01 skills 经当前实例 CLI，退役双模型门（已合入本地 main，所测端到端通过）

- L 档，沿同一母路线进入下一具体族。`ca6a8530` 先提交实际caller、A import+loop/B typed domain两个结构及参数/预算/锁合同，reviewer24窄结构PASS；[ADR0050](adr/0050-skills-runtime-cli.md) Accepted。`1551c71e` 首次可编译 source read/write --self，不新建Kernel/Job/client框架；`9100c791` 同cut退read_skill/write_skill执行wrapper、schema/registry/load/dev名单/review项，保其它Tools门、perLocal/Jobs、persona和例程领域。现状见[生态](atlas/ecosystem.md)、[API](API.md#技能运行时-cli)。
- 独立真红封存：main `ae35213b` / validation `bf964228` 的 ONLY ReadEntry .96s/package2.631s，预置技能后真source Shell调用BIN read明确exit2 usage。首实现 `1551c71e` / validation `3f3b325cdc48a25583fda0a1e2df2945b97ab148` ONLY WriteReadEntry一次PASS1.00s/package2.707s：bodyfile→真BIN写title/description→原endpoint第二read，metadata、完整正文尾哨兵、实际SKILL.md、Worker+Peer两个UpdatedBots及真实config启用都到达，--self不等于scope=self。首绿不重跑。
- 完整 `9100c791` / validation `f6d27ba62268fde64d74f02d5f9185ccdebb9159` ONLY剩余四项一次全PASS：BodyBudgets1.07s、BodyRefusals .38s、ReadOnlyAndSchemas .26s、ScopeLifecycle .74s，package4.214s、exit0。覆盖>16KiB全文、file/stdin、转义整JSON64KiB产物全文、改名后当前workspace与旧诊断Bot名、显式空正文成功/缺正文拒绝、转义后1MiB超限零改、原文件20MiB+1拒绝。大stdout沿原Shell限制实际重定向文件再核JSON预算，不放宽输出契约。
- Scope真实覆盖所有local含other-owner/shared/orphan、真global Bot、EqualFold BoB、改名原ref的self、已运行Peer手动离线开关后立即read和未知目标零改。本机大小写拼写的samefile只读事实为True，BoB/bob歧义conditional本次NOTRUN，不另造FS矩阵；实现方歧义/零文件与config变化已有单位守护。实际main、continuable research/dev三wire请求均无双schema/index/旧指引，read/help允许、write/操作符无副作用，Peer的剩余Tools门与dev read_file保留；legacy为下项真实Provider单位证据，不称独立legacy E2E。
- 本地必要证据复用：新parser .103s、CLI四项 .362s；初次compile-only skill/kernel .580/.673s与cliops/cmd .538/.499s。bounded候选组 .868s、runtime五项 .433s；fresh当前config错误增量仅两受影响项 .622s。旧业务driver迁typed后保title/全local范围/实时手动开关/12竞争写同赢家、routine16KiB、历史redact和其它Tools门；race2.668s首次仅新legacy fixture错误断言失败，其余所选项通过，修正后ONLY真实Dispatch=false spawn→Local→Provider请求 race1.592s；agenttype race1.318s。初始历史fixture编译错无业务执行，不当产品失败或整包绿。
- 唯一必要实现P2：非迁移Setter仍委托原整Config marshal，旧Bot.style会无声丢失；`TestNonmigratingSkillSwitchPreservesLegacyStyle` 实际红3.168s后，仅原config锁写缝改raw字段merge，保style/未知字段且不回Load/记忆迁移，绿 .375s。退役review项 .546s；相关七包vet、gofmt/diff通过。`TestResearchSkillDirectCommandsUseBoundSource` .904s在实际readOnlyShell缝计数，拒绝写/离线/错误root/操作符/任意xbot先于解释器执行，作为本地guard证据，未冒称独立RPC计数。一次atlas audit exit0无断链，27项非致命提示（size1/stale5/dev4/unabsorbed3/unmapped14），不为清提示扩修或重审。reviewer24 首1551及后续1551→9100→e212连续必要增量静态全部PASS，无剩余必要问题；未为最后单guard计数单位重开审查。
- 五项后24另抓到实际消费P2：HTTP/CLI64KiB预算高于原Shell32KiB输出，40KiBASCII正文在模型处截断JSON。精确 `9100c791` / validation `f6d27ba6` 新增 ONLY ShellOutputBudget实际红 .94s/package2.486s，未到完整file/fulltail断言。`e21224cf` 只导出现有Shell预算只读alias、skills整JSON含newline按两者较小值投影，finalartifactreply/metadata也检查；HTTP仍64KiB，不提高Shell、不让research重定向。仅受影响精确caller预算单位及真实全文产物 .979s、kernel/tools vet通过。精确 `e21224cf` / validation `2601e232fb04c2b398e27a2ac7615a9450b2ba46` ONLY ShellOutputBudget+BodyBudgets一次双PASS，新40KiB无redirect消费 .13s、原预算1.10s，package3.069s、exit0；模型所收strict完整JSON、当前results全文/精确尾哨兵、22KiB仍内联、file/stdin/转义产物与改名旧ref全到。其它三项旧绿及首链封存不重跑，协议、harness与独立断言未改。
- 父方在保留并行重启功能 `fca3a61f` 的隔离分支集成：产品 merge `8c26e5b7`，独立四提交仅取为 `e1fc4bdc/2b0bb31e/73898460/06d55230`，未取 validation 产品镜像。CLI 编译1.083s、smoke只编译与 diff通过，集成 atlas audit退出0无断链，非致命提示未扩修；固定业务绿未重跑。本次以选择性暂存合入 main；并行文档修改继续保持未提交，未覆盖或代为提交。
- 当前级别：已合入本地 main，原固定五项skills与新增实际消费接缝均独立端到端通过，六项按首1551、其余9100、消费补缝及相关预算e212的实际SHA封存，不把它们抬成最终docs HEAD重跑；尚未用户日用。所有独立job/私服均已收，未改独立期望。Not run：已封存profile五项、旧MCP/长调用、全包/全仓/完整矩阵、真模型、无关旅程与日用实例；不push/发版/重启日用实例，母dev其它族继续保留。

## 2026-11-01 聊天收消息与回复收尾保阅读位置

- S 档，按用户日用反馈修复突然跳位，产品提交 `7e2f9ec5`。SSE 送达就用正式气泡布局，避免结束时补头像／名字；收工单独对账，不被上翻时的后台轮询冻结跳过；分页／轮询控件和失败提示变高保可见消息锚，引用跳转明确解除 stick。沿用已有回到最新入口，不新增未读悬浮组件。连续窗口合并保留历史，有缺口沿旧规则不静默拼接。
- 三个新增 Chrome 回归先红：上翻收工留下临时行、轮询失败提示移动阅读锚、近底引用被布局跟随拉回。追加逐字 Range 检查另复现临时长回复收工下移 34px，统一送达布局后绿。`TestChatStick*Browser` 四项最终通过（7.81/12.37/6.38/1.03s），包含贴底跟随、历史和刚到长正文的阅读位置、扩窗回短尾、返回最新后继续收主动消息；既有追加／prepend 锚偏移实测 0px，20帧墙钟样本 p95 18ms，仅作有限样本，不代表稳定60fps。
- 相关 ChatStream、TranscriptIncremental（含无 validator）、TranscriptWindow、VoiceCluster 已通过；ChatTabsPreserve 与 LiveBubbleSplit 初跑暴露夹具未持久化已送达回复和旧 `.trace` 定位，按真实持久契约／现行 explorer 修正后仅两项复跑通过（1.841s），原行为断言保留。共11项浏览器场景通过；独立只读增量复审 PASS，未重复测试。
- 真实二进制、serve、HTTP、磁盘与 Chrome 的 `TestChatTabsJourney` 13.78s通过，模型为 scripted；截图在 `~/.xbot-build/ui-shots/chat-scroll-20261101/`，目视查看深色桌面与浅色390px聊天场景。JS语法、相关包vet、diff与atlas审计通过（审计保留既有非致命提示）；运行8788的 app.js/app.css 与工作区内容一致，来源 dev-static，刷新载入，无需重启服务。
- 级别：本地 main 已合入，相关自动端到端通过，待用户日用。Not run：全仓、真实模型、跨平台原生WebView、物理60fps和用户日用；不push、不发版、不自动刷新或重启日用实例。

## 2026-11-01 重开窗口恢复上次 Bot

- S 档。原先选择只写 sessionStorage，关闭窗口后丢失；打开 Bot 现在同时持久保存到 localStorage，刷新仍优先用本窗口选择，新窗口恢复上次打开的 Bot。已删除 Bot 沿主 Bot、列表首项回退，账号退出清除持久记录；没有新增设置或视觉变化。现状见 [网页界面](atlas/ui.md)。
- 独立浏览器回归先在旧实现复现：选 Focus、关闭页面再开，实际回到 Home。修复后 `go test ./internal/web -run '^TestSelectedBot.*Browser$' -count=1 -v` 通过，覆盖关闭新开、同 profile 浏览器进程重启、多窗口刷新、缺失 Bot 的 primary/first 两级回退；断言侧栏、标题和实际聊天内容。首次绿测仅因测试观察脚本在 about:blank 访问 storage 产生错误，限定测试 HTTP origin 后原业务断言全部通过（10.087s）。
- 级别：浏览器真实页面与进程生命周期验证通过；运行中的 8788 桌面服务所发脚本与工作区一致，刷新即可载入。未重启用户日用进程，原生 WebView 重启与用户日用待验。Not run：全仓、真模型、无关旅程、跨平台和原生窗口实机重启；不 push、不发版。

## 2026-11-01 Bot 控机 · Mac / Windows 无障碍树

- L 档，按本轮用户直接授权自行设计、实现及设置自检，不等设计师或用户代合。唯一装配仍为 `link.Organs → kernel desktop.Open(root) → Link.Tools`，Mac/Windows 薄适配官方 CLI，Linux 旧像素驱动保留；没有平行 Link、商店项、应用名单或 UI 指纹。Windows npm shim 按官方安装布局定位原生 exe，不经过 shell。决策见 [ADR 0048](adr/0048-desktop-a11y-adapter.md)，合同见 [机器与设备](atlas/machines.md)。
- 自动验证（均 `go test -count=1`）：desktop、review、link 相关整包；kernel 的器官装配/注册、机器 allow/ask/off、生产无卡定向用例；web 授权 API/状态浏览器与原设备配对 8 场景；cmd 的非法按键回归；`-tags smoke ./e2e/smoke -run TestDesktopAccessibilityBinary` 真 CLI/serve/HTTP 冒烟通过。CLI 子进程夹具覆盖 snapshot→click/type→snapshot 变化、原样文字、错误 disposition、无重试、超限与缺依赖；npm global/local 的 amd64/arm64 路径解析通过。浏览器仅 chrome-headless-shell。初次 cmd 回归命中 bad-key 错误文案变化，恢复原错误语义后定向通过。
- Windows amd64/arm64 与 Linux 相关包交叉构建、相关 vet、JS 语法、diff、atlas audit 通过（audit 有仓库既有非致命提示）。实际官方 1.0.2 / envelope 2.5 无权限路径：真 xbot snapshot 非零退出，保留 `PERM_DENIED / not_delivered / safe`，未伪报操作成功。
- 设置沿白纸透明行，实际隔离 serve、浅色 1280×900 DPR 2 截图由主 Agent 看图通过：`~/.xbot-build/ui-shots/bot-desktop-a11y/page-@2x.png`。已有设备初始化在会话建立前 GET 的一次 401 记入截图报告，最终真实授权状态成功读取；未扩修旧初始化时序。没有新 CSS/动效、未独立视觉评审。证据摘要在 `~/.xbot-build/bot-desktop-a11y/`。
- 级别：代码 `cc521eb3` 已合入本地 main，自动化的 scripted 桌面端到端通过；真实 Mac App 正向链因系统无障碍 denied 未通过，Windows 无实机。不宣称双平台实机完成或用户日用通过；剩余验收留在 [dev](dev/bot-desktop-a11y.md)。Not run：全仓、真模型、无关旅程、Windows 实机、已授权 Mac App 正向操作、原生壳及完整主题/响应式矩阵、专项性能；不 push。

## 2026-11-01 profile 完整 CLI 与原运行态连续（已合入本地 main，所测端到端通过）

- L 档，沿同份 CLI dev 补完整 profile 合同并获 reviewer24 结构通过，[ADR0049](adr/0049-complete-runtime-profile-entry.md) Accepted。产品 `5ed71e5a` 接组合 name/description/timezone/avatar_seed、fresh config/实际 partial 与既有 Resources/Jobs/exec/host；`94f34c67` 修原 Shell cwd marker 和已提交事实/通知丢失；`748ea940` 接原始头像传输、cold scope/恢复、Timer/code 当前路径、主源短 IO 和 profile 模型门退旧；`0feadf1c` 收有限媒体/提醒/experience/history/skills/reference/compact 路径、cold cwd 持久化、无缓存 Web peer partial 与相对 Git repo 兼容。未新建 UUID/别名、资源/任务账、缓存框架或全局 Local 准入，未迁其它平台族业务。
- 实现方定向证据复用：Bot 新组合六项 .687s、头像四项 .646s；cmd Name/partial 两项 .698s、头像三项 .820s；cliops partial race1.696s、头像三项 race1.799s 与 vet。exec 新移动组 race4.247s、受影响准入/closing 四项1.235s、cold/boundary1.215s；安全 cold interrupt 新单项 .131s。Web 新组合/partial/大头像三项 race1.555s、reserved-group4.395s，JS 两文件语法检查通过。只编译失败的跨写面窗口不计业务执行，也未因此重复旧绿组。
- 缺陷定位：独立 `5ed71e5a` RenameEntry 真失败于旧 marker 路径 stderr，`94f34c67` 的 FD trap 回归 race4.195s 后独立 validation `48e6c44b` ONLY RenameEntry PASS1.69s/package2.674s；严格 JSON/API 新名断言保留，该 SHA 证据封存。坏 moved Inbox 仍保 commit facts/notice 的新回归 race1.277s；先 rename 后 Restore 的 P2 与 safe recovery race1.743s，父信已绿结果复用。新 scope fixture 最初缺独立 checkout 边界，按实际仓库内 TMPDIR 建 `.git` 后仍精确断言新 project Resource，未改生产上溯策略。
- 其余新增定向：原 source persona/rules/通知/trace race1.729s；保留旧 K 的本地路径一组 race2.051s、Kernel vet；cold foreground cwd .597s、无缓存 Web peer partial .789s。Timer emit 里同步 rename 后提交/CRUD race1.662s；code 旧 Link 与 flock 等候 race4.288s、执行 cwd/预构造 Shell4.334s、实际 Repo env 受改用例1.781s。24 增量发现相对本地 Repo 因新 cmd.Dir 错找，真实 git 定位先红 .763s，最小兼容修后 race1.699s，保原 Repo 值和远端 locator。退旧模型门、peer partial、原 Persona/door 定向通过，review .568s；混合测试保有效业务。
- 一次 atlas audit 退出 0、无断链，31 项非致命提示（size1/stale9/dev4/unabsorbed3/unmapped14；stale 含 4 个新切片后待刷新戳的当前节点），不为清提示扩修；当前相关 verified 已同步至产品 `63762ae5`，未把这些提示冒称阻塞或另开无关审计。实现方未运行全仓或重复已绿矩阵。
- `0feadf1c` 独立剩余四项首次执行（validation `eccdb437`）中 Avatar PASS3.06s、Browser PASS5.04s（Chromium153），包含 exact1MiB PNG、64KiB JSON/超限零改/clear、原 SSE、两对 previous_name、真实 child 输入/原 ID、草稿焦点滚动及迟到切 Bot 隔离，证据封存不重跑。Resources 抓到实际 cached Timer 捕获 MountOrgans 临时 K；`9060537c` 以实际 StartOperations 发布前重绑，真实构造链先红 .717s、race 绿4.303s。Atomic 后续抓到 cached UseBot 换新 Bot 而 WorkDir 原前缀丢失，Resources 又抓到 scoped Start 取主线目录；`0004ad0f` 保不可变目录来源并对齐 Session/Start scope，新真实 project proof/strict main JSON race1.963s。24 指出的媒体 cwd fallback/继承语义回归以三必要分支先红后 race1.772s 修复，同增量 Kernel vet 通过。
- `0004ad0f` 的 ONLY Atomic（validation `6baa2de2`）完整 PASS1.11s，最终实际模型请求无 profile 入口、组合/冲突零改/persona/原 MCP 会话/同名 strict JSON 均到达，证据封存。Resources 原 child 已可启动，但实际 `/var` root 与 `pwd -P` 的 `/private/var` project 前缀漏迁而回根，未宣称该项通过。`27f23a8f` 补同一目录逻辑/物理身份的现 resolver 匹配：Kernel 真 alias/root 项先红 .659s 后 race1.794s，Bot 冷 cwd 红 .634s 后 .488s；原 Local live gate+Code.Record.Move→exec.MoveBot 与 cold descriptor/meta/code project 单项红 .673s 后 race1.699s。外部路径和相对 Repo 值不变，未宽化 roots/usableDir、未改原 transcript。
- alias 修复后的独立 ONLYResources（validation `682d3f9a`、有界 basename 诊断 `97bf6f3a`）已真实通过 live project 新 cwd 与精确 `before\nafter\n` 文件证明；Restart 前旧 Bot 目录只复活 `inbox.jsonl.lock`，仍不算通过。实际 caller 是 proc 旧 Inbox Reload/Head/Ack 与原 HTTP 延迟 Ack。`93ff94e1` 已补 Web current ref 短 Post/absorbed read/late Ack，真实 shared-message/CLI-event 红 .20/.22s、HTTP/absorbed race 绿2.315s；原 SSE、200 done、EventID 与单 Post/Ack 保留。`43e97b41` 再闭合 proc Run/Drain current Inbox 与原 Timer watermark 实际读写/open inode，flock 等待/模型/投递在 route 外。新同组先报旧 Inbox，再到实际 Timer 后半只报 watermark 两文件，双口修后 race1.830s 与 proc vet 通过，未改 handle/feed 或其它业务。
- 最后 ONLYResources 在精确产品 `43e97b41` / validation `5d263d31d1b257aba3c76f528482ad70a447928b` 一次 PASS1.14s/package5.420s、exit0：live 原 Job/同 ID steer/held Timer wake/父信和单完成事件，物理新 project cwd 与精确 before/after proof，旧 Worker 目录无复活后真实进程 Restart，ANY Local Restore 前真实 CLI 第二次冷改名，原 child ID cold Message accepted/done 与更高 turn_seq，真实 `child-original` 选模、原 task/结论、ColdWorker/full-project、旧 proof 不重放、新轮结果只追加到新路径且旧结果首 bytes/旧 raw 前缀不变、两个旧 bots/work 不复活及最终无旧 profile 模型项，均实际到达。其余四项按各自精确 SHA 证据封存；没有把含失败的 9.947s/3.531s 原包耗时改称全绿，也没有重标为末 SHA 重跑。
- 五项封存后，24 在原 chat Run 下一事件发现旧 Bot handle 口；原同组继续实际处理第二 Timer，先红旧 handle+transcript，current route 内 open inode、锁外 flock 后只剩 transcript。`0121ad6d` 再把 closeDangling 的实际 OpenTools 读接当前存储，同 run 后半 race2.021s 与 proc vet 绿，保原旧 K/Bot/Inbox 对象、第二事件 Ack、无重放和 Timer 不重投。`63762ae5` 收原 chat Accept/Idle/CatchUp/After 必到调用口：第二输入、通知和新名、回放与 seen、After count/显示顺序四分支先红后 race2.005s，实际短 IO 与打印/回调分离。两增量没有重跑或重标固定五项。
- 本地 main 以 `daee7d54` 合产品、`23fb8a98` 合最终事实文档；七个独立测试提交仅取本批四 Go 与一 CJS，落为 `0a6319f9/878ecd86/840baf13/54277bc9/ff17abc1/d322a367/3f1ef57e`，未取 validation 产品镜像。保主线 `50b2c2b0` 的滚动收尾、持久选择及桌面权限分类，两处文档冲突保双方事实；profile ADR 因桌面已有 0048 顺延为 0049。
- 新交叠只命中改名遗漏 localStorage 选择：真实编辑 Focus→RenamedFocus 后关页面，新页仍持久 Focus 并回 Home，单浏览器红11.75s/package12.253s；仅在持久值仍为旧名时迁移，不重开 SSE/selection、不覆盖其它窗口后选值；同单项绿1.92s/package2.627s。shipped app、真实页面生命周期与 scripted HTTP，未注入 selected storage 或调内部 rekey。CLI 合入编译0.510s、smoke只编译、JS语法及 atlas audit退出0/无断链，既有非致命提示未扩修。固定五项及旧选窗整套没有重跑。
- 当前级别：已合入本地 main、本地定向 unit/race 与固定五项独立所测端到端全部通过，reviewer24 连续增量静态至 `63762ae5` 全部 PASS，无剩余必要问题；末 Run/chat 局部结果按实际版本记载，未冒领五项在末 SHA 重跑。独立测试只由 0d 维护，实现方未改其期望。Not run：全仓/完整矩阵、普通 profile/seed/MCP 已绿组重复、真模型/外部服务、跨平台、日用/专项性能；未 push、发版或更改运行实例。当前 atlas/API 已同步，路线与其它平台族仍未完成。

## 2026-11-01 MCP 2b 实例 CLI 与旧模型入口退役（已合入本地 main，所测端到端通过）

- L档，在main16101e7e上沿已定CLI路线落具体合同29668efc；24窄结构PASS后 [ADR0047](adr/0047-mcp-cli-domain-entry.md) Accepted，替代0035的MCP模型门部分，0046进程owner/通知/Jobs/sourcelease保持。产品7c5d5766退Kernel旧注册/schema并接领域产物，16be6d78接argv/双HTTP/早分流/core只读/probe和移除Link adapter/aliases。未新增通用action bus、会话/任务账、权限UI或完整rename。
- CLI self以原endpoint/ref为准；human显式http+bot复用owner cookie，未显式root不发默认目录。纯ParseMCP供cmd/research，同session hint精确查验后执行，不二次slot路由。args-file/stdin单对象、RawMessage保大整数、默认ctx/timeout0。tools/describe/call统一最终64KiB投影，完整Raw/unknown/structured/图片/embedded产物在当前work路径短route写完，isError保内容而非0；临时probe冻结此次待保存值并检查raw isError。
- 本地选定绿：CLI/profile/literal/Known检查.543/.514s；cmd help/root/cookie/stdin大整数/isError/offline四项.771s；统一Kernel27case race3.190s（非MCP门/Descriptor真实HTTP路由/overflow/secret与资源4case），research源绑定单项race4.289s（初版新fixture少Tool接口方法仅编译失败，修fixture后单项绿）。提取owner HTTP后原message三项.373s通过。work3case.650s、human/domain API.808s；新core readonly/Open4race1.668s、迁协议3race1.744s、目录4race1.666s/provider拒绝1.726s；probe/runtime5race2.598s首轮实际执行通过。相关vet/diff通过，未因并行写中编译失败重复执行已绿组。
- 精确16be6d78独立CLIEntry（validation4bb126e3）PASS.58s/package1.378s：真实source Shell add/list/tools/describe/原名counter及remote.v1 call1/2同session、envBot decoy无效、完整Spec/unknown annotations、主tools无MCPschema、DoorPeer非MCP Tools门保留。初轮只因独立fixture把list_bots误要求当前Worker失败，作者创建DoorPeer后仅Entry复跑，无产品改。24对16101..16be静态PASS，无P0/P1；同产品16be的validation4bb126e3还通过Owner1.32s、Calls22.86s（新CLI唯一长调用15.601727s，原Job跨web控制/sourcelease/单Event）、Boundaries.12s、Results.16s、Research.45s；完整Raw/structured/unknown/PNG/schema Artifact/isError坏图非0、主+continuable无MCPschema、同sessionhint与换spec两端零RPC均到达。Reconcile产品热换已正确done+exit1/configuration_changed/outcome_unknown/noRetry，独立fixture修为读取原Job.exit_code后仅该项在validation c530b805一次PASS10.57s/package11.844s，所有credentials后续均过。产品无fix、其它组/15.602s不重复，后续docs版本不冒领全组重跑。独立文件提交9f035622/4d130a95/4bb126e3/c530b805仍归0d。
- 本地 main 合入 `35abccda`，独立四提交依次取为 `5aca7fcb`、`595aa436`、`76e0599e`、`0856164f`；未取验证分支产品镜像。合入后 CLI 编译通过（0.729s），smoke 只编译，未重复独立组；atlas audit退出0、无断链，40项非致命提示未扩修。原未提交设计和临时文件保持。
- 级别：2b 已合入本地 main、本地定向unit/race、独立所测端到端与静态全部通过，尚未日用。Not run：全仓/完整矩阵、真模型/外部服务、无关UI/头像/continuation浏览器旅程、跨平台；独立CLI慢15.5秒仅由验证方必要重验一次，实现方不跑。旧2a精确aaa/704证据封存，2b不把其冒用成新CLI全部通过。整条平台CLI迁移与其它族仍未完成。

## 2026-11-01 可继续本机子代理，对齐 DSH 主旅程

- L档，用户确认五项并授权并行实现；原型后进一步指定独立「子代理」Tab，「任务」Tab查看Routine。已落地同id完成/失败/停止后继续、fresh/已完成父轮fork、双向来源消息与每轮自动结算、单次provider/model/thinking/effort、公开子记录与真实继续/停止。采用持久身份+有限执行，决定见[ADR0045](adr/0045-continuable-local-subagents.md)，现状见[子代理](atlas/subagents.md)、[界面](atlas/ui.md)、[API](API.md#子代理通知群)。已完成dev提炼撤下，跨入口新id共享入场仍在仓库外候选，原每Local 3+8/resource边界保持。
- 执行器、Kernel、Web与UI分块实现，另有独立设计/代码审计、独立用例作者和视觉顾问。核心提交为exec `9bcaa85f`/`86f77156`、Kernel `67aba3b1`/`1f44abf4`、Web `26ca38b2`、UI `03e119d9`/`45938570`。使用已独立验收的轨迹explorer `f8f88322` 作为共享详情依赖，没有复制第二套详情renderer。最终静态复审无剩余P1/P2；修复重复queued Stop、BLOCKED已消费steer、拒绝冷入场scope清理、工具预算早返、跨模型MCP拒绝缓存及看图实例复制锁。
- exec核心定向0.706s、整包race5.834s、后续生命周期增量race1.755s及scope清理race1.206s通过。Kernel受影响包仅旧注入Local夹具缺AgentsDir，迁到实际记录根后相关两例0.256s通过；实际模型搜索/vision双向、子窗口/工具定义、摘要route、overflow隔离、空历史fork与MCP跨route定向3.299s通过。agenttype/review定向与相关包vet通过。Web11项1.540s通过，最终exec读帽增量后4项1.277s通过。通过即停，日志按具体代码状态复用，没有再扫全仓。
- 独立二进制验收先在 `f80d53cd` 取得红：真serve/HTTP派发、实际v1文件与父自动回报成功，子transcript GET返回404。集成 `908dab62` 后，Context/Delivery/ParentMessages/Model/StopRestart五组Binary通过，覆盖默认fresh/fork边界、cwd隔离、父忙闲消息、显式选模与重启route、stop/failed/真正kill后的显式继续及Shell副作用不重放。3个旧入口与TraceExplorer浏览器回归通过。
- 真Chrome主链路抓到新脚本/样式未注册本机route的产品装配缺陷，由 `113c8a2f` 补齐。独立夹具另修正了选区误命中正文/textarea焦点，以及完成态隐藏delivery控件与未承接Promise；保留全部业务断言，不为实现放宽期望。`2595697f` 的 `TestContinuableSubagentBinary` 真二进制+HTTP+Chrome同服务主链路2.85s通过：原孩子看首轮工具/结果、反馈实际写v2、两轮turn_seq2/8、旧结果字节和旧记录前缀不变、主Bot草稿不被提交。
- 对照已有界面guard，补回孩子迁Tab后遗失的notable短事实与完整对象，复用原renderer。`e1ea53cf` 独立代表浏览器最终0.38s/包1.307s通过，含真实终态profile事实、路径选择、草稿/输入焦点、工具正文选区跨两poll及相同childID跨Bot迟到隔离。新真实Chrome主链路截图桌面/390均DPR2，主管与独立顾问实际看图PASS，未冒称深色或原生窗口。完整日志、截图及独立 `ACCEPTANCE.md` 在 `~/.xbot-build/continuable-subagents/`；原型视觉复核在 `~/.xbot-build/continuable-subagents-ui/REVIEW.md`。
- 级别：已确认本机主旅程端到端自动验收通过，随本次提交集成本地main，待用户日用。Not run：真实模型/外部服务、全仓/全smoke/全Web、无关旅程、Windows/原生壳、深色新增截图和专项性能；没有将局部通过写成整仓或所有后续SHA重跑。未push/发版，不自动重启日用实例。

## 2026-11-01 轨迹总览与点击详情

- M 档，用户确认紧凑布局、无斑马纹与 DSH 配色原型后授权实现。正式轨迹改为三轨总览、类型／错误／当前范围搜索、34px 单行事件与右侧概述／预览／原始内容／来源；手机 44px，详情覆盖内容区并可返回。工具结果按调用关联，孤立结果在加载更早调用后归并；颜色只点在类别标记和标签。实现 `f8f88322`，现状见 [网页界面](atlas/ui.md)。
- 实现与独立验收由不同子 Agent 承担，主管复核并统一执行。累计 7 条定向 Web 浏览器用例通过，覆盖多调用同 seq、显式 internal、工具结果搜索、详情与剪贴板完成回执、两阶段输入／正文选区保态、分页锚点及迟到回包隔离；保留 thinking／todo 回执／hosted 生图与聊天状态行为。初跑新夹具因中文 ETag 不满足 HTTP ByteString 修正编码，旧迟到资料用例触发从已独立加载的轨迹迁到上下文，保留全部隔离期望；只重跑失败 3 项，通过后停止。
- 真二进制、HTTP、磁盘账本及 Chrome 的 `TestTraceExplorerJourney`（14.94s）与 `TestChatTabsJourney`（11.54s）通过；实际读回系统剪贴板，执行搜索／筛选、三种总览模式、四页签、轮询追加、before 历史、390px Escape／关闭返回和切 Bot。第三方仅为 scripted 模型，未用真供应商。定向 vet、JS 语法与 diff 检查通过。
- 主管与独立视觉复审对照用户定稿原型，正式桌面浅／深色和手机详情三张 DPR2 截图 PASS，无截图实证 findings；手机列表行高和未截图状态不冒称目视验证。证据、日志和评审位于 `~/.xbot-build/trace-explorer-20261101/validation/`。级别：端到端通过，待用户日用。Not run: 全仓、真实模型、无关旅程、Windows／原生窗口与专项性能测试；本次只改 Web 轨迹投影和静态资源。

## 2026-11-01 MCP 2a 运行态底座（本地 main 已整合，所测端到端通过）

- 整体具体结构经24 PASS，`76f6df46` 在原编号上标 Accepted，整合为避让 continuation 决定迁到 [ADR 0046](adr/0046-process-owned-mcp-sessions.md)，仅本刀 owner 决定；0035 MCP schema 退役留2b。产品 `3f8e076d` owner/通知/冻结凭据，`b08afda9` HTTP/stdio取消/完整Raw/陈旧spec拒绝与Ref暂停，`c799d8ae` 逐call脱敏/Jobs准入，`aaa66c49` 共享原Jobs/sourcelease/rename-delete与真实入口接线。
- `3f8e076d` 独立Owner真入口 PASS（validation `69e78b70`，仅21093183+9e5f3d99独立文件），1.53s/package2.675s；24对同SHA增量静态PASS。热新增在web/describe/status前收到note并启动真实轮；三Bot共3次init（旧10），Worker web/note/web计数1/2/3同session、Other独立1，untrusted Inbox单事件。
- 精确 `aaa66c49` 的独立最终组（validation `781db024`）：Owner PASS1.60s，CallsAndSourceLease PASS22.78s。真实HTTP15.514s、调用count1；跨web原Job status/output/cancel、sourceShell两次CLI端口保留和真结束回收、空K关闭、单原event均过。该15.5s证据封存，不重复。日志由协调方保存在 `~/.xbot-build/ui-shots/cli-platform-mcp/mcp-final-781db024.log`。
- 同组Reconcile在 INVALID_KEEPS_WORKING 实际红：installedSection现读坏磁盘绕过保留spec；24另命中正常换绑B后wake以r.current旧整配置覆盖fresh B的P1。`704f584d` 用同一mcpSpecs覆盖runtime摘要/search/load/adapter消费者，无resources才读文件；CurrentBot在短route内fresh Load当前名。最小定位先红（deepseek旧绑定覆盖replacement），修复后单项race PASS4.287s，cmd/kernel编译通过；只交独立Reconcile和修复增量静态，Owner/Calls证据复用。
- `704f584d` 独立Reconcile复验（validation `2d59185f`）一次PASS10.96s/package12.022s：非法spec保有效会话、热换unknown不retry/单event、真实secret定向变更/删除/恢复均过；24对 `aaa66c49..704f584d` 定向修复静态PASS，无剩余2a阻塞。Owner/Calls保留上面的精确SHA，不重跑15.5s。
- `58b2d81c` 仅导出原dirMu保护的 Directory() getter，供协调方整合新main continuation restore的实际Jobs.Dir读口；Dir兼容保留、不共享Local executor。tools仅编译通过0.708s，没有为getter另写测试或重跑旅程；本分支未拉main continuation。
- 自有绿证据：owner/adapter5项race；取消/Raw/stale/pause11项race（半帧新fixture预读场景纠正后仅重跑该项）；tools定向race1.920s；kernel6项race2.862s含原lease红翻绿、跨K同表cancel/单完成/源mask；mutation7项race kernel1.235s/web1.339s；9项受影响旧rename/Jobs/delete/endpoint通过。最终cmd/web/pipe/e2e编译通过(.749/.899/.893/.894s)，所有job已收。相关一次绿即停，不以全仓测试代替实际入口。
- 本地 main 合入 `66ef3022`，保留同期可继续子代理与专属 Tab；合并后仅将新增 Kernel fixture 接资源参数、restore 读目录接 Directory()。组合 Kernel 的实际子模型搜索/vision、MCP 跨 route schema 回退、续聊与父信通过（0.896s），Web 完成态继续与绕前台 gate 控制通过（0.935s），CLI 编译通过（0.757s），smoke 只编译。独立三个测试提交依次取为 `458c6179`、`ab6860bb`、`97c32bb5`，未取验证分支产品镜像。相关新日志在 `~/.xbot-build/ui-shots/cli-platform-mcp/integration-*-7efc0dc1-eda8616c.log`，不改独立报告的历史 SHA 归属。
- 合入工作区 atlas audit 退出0、无断链，40项非致命提示（size1、stale5、dev5、unabsorbed3、unmapped26，含现有未提交设计与临时文件）；未为清提示扩修。
- 级别：2a 已合入本地 main、所测端到端与增量静态通过，未日用。Not run：全仓/三遍、机械19项重复、15.5秒独立组重复、Avatar/无关浏览器旅程、真实模型/服务与跨平台；未push/发版/改运行实例。2b runtime MCP CLI/完整产物与旧adapter/schema退役未做。

## 2026-11-01 MCP 2a 设计与独立 core 机械子步

- L 档，沿已确认 CLI 路线在同份 [dev](dev/cli-platform-entry.md) 补 2a 调用方、两种结构与实际锁/回调/关闭接缝，新增 [ADR 0046](adr/0046-process-owned-mcp-sessions.md)（Proposed）。完整进程 owner/共享 Jobs/通知/取消接线仍待整体具体结构 PASS；没有提前做 2b CLI/退 schema 或完整 runtime rename。
- 已单独获准的纯机械子步提交 `e4313fc3`：现 client/stdio/HTTP/diagnostics/config 与有效协议测试移到 internal/mcp，Link 保模型与 Event 投影/薄委托；core 仅 stdlib+fsutil。Provider predicate 由 Link 动态注入，lazy owner/Close、Secret fallback、HTTP 15s 与同步取消行为未改。不把此子步描述为 MCP 2a 完成。
- 一次定向 19 项 core/Link 测试通过（11+8，3.324/3.124s）、两包 vet/格式/diff 通过；额外 `go test ./cmd/xbot -run '^$'` 编译通过（0.230s，不执行产品用例）。仅复用这批证据，不全仓重跑。
- 实施前自有最小红分别命中旧实现：source 解释器调用尚在 gate 内而退休关闭端口（3.215s）；同 HTTP session A 长 POST 阻塞时，B cancel 等 A 响应（1.838s）。只新增自有 fixture，不改独立 E2E 文件；它们是待 owner/取消实施的红，不冒称真实 CLI/独立入口已通过。
- 级别：仅 feature 分支机械提交/设计草图与红证据；未合 main、未独立验收、未日用。Not run：新 runtime 生命周期绿验收、完整 suite/三遍、真实模型/服务、跨平台与无关旅程；未 push/发版/改运行实例。

## 2026-11-01 移除 Tab 内重复大标题

- S 档，按用户截图要求删除记忆、轨迹、任务、上下文、收件箱等主视图内重复的页面标题和标题占位，页面名称由顶部标签表达；内容分区标题和操作保留。同步删除无用的标题 CSS 与字号/行高 token。实现 `b519fffb`。
- 真实二进制、HTTP 与 Chrome 的 `TestChatTabsJourney` 通过（13.88s），覆盖浅深色、桌面/窄屏、切页保持、搜索、经历展开和消息送达。首次验证遇到既有提纲正文/原文双节点导致 strict selector 失败，选择器限定可读正文后通过；保留原行为断言。JS 语法与 diff check 通过，atlas audit 无断链、保留既有非致命提示。
- 端到端通过，已查看相关 DPR 2 截图，证据在 `~/.xbot-build/ui-shots/tab-headings-removed/after/`。核对 8788 正在提供的三个静态资源与工作区一致，刷新即可生效；未更改或重启后端。Not run: 全仓、真实模型、无关旅程、Windows/Linux 原生窗口；局部删除不新增视觉评审或性能测试。

## 2026-11-01 环境发送者正式姓名

- L 档（内核环境来源语义），范围限用户要求的 `sender=u501` 改为正式姓名。环境 `sender` 现读 person 显示姓名，`sender_id` 保留稳定 ID；记忆、认证与快照关联继续按 ID，旧环境可回放，原 transcript 不改。方案及对照回顾见 [ADR 0044](adr/0044-environment-sender-name.md)，独立设计与代码复核无剩余问题。实现 `c599679f`。
- `TestEnvironmentSenderUsesPersonName` 在旧实现实际先红，修复后与 `TestEnvironment*` / `TestEnvStream*` 一起通过（4.743s），覆盖首请求姓名/ID、同人改名、旧环境和人物记忆。独立编写的 `TestSenderNameBinary` 经真实二进制、HTTP、scripted 模型验证姓名/ID与回复送达，通过（0.45s）；初次执行的测试解析器误读纯文本时间公告，修正后单项通过。kernel vet、diff check 通过。
- 本机正式与开发数据根的姓名经 person.Record 追加授权记录更新为「刘政东」；运行中 8788 会话接口确认 `owner_name=刘政东`。级别：本地 main 已合入、专项真实入口端到端通过，待用户日用。Not run: 全仓、真实模型、无关旅程与 Linux/Windows；本次按影响选择定向检查。

## 2026-11-01 经历展开区阅读优化

- S 档，响应用户对真实提纲中工具回报、绝对路径和截断提示的视觉反馈。正常对话改为「你／Bot 名」角色与短段落，限制阅读行宽；工具完成回报及截断标记只放在默认折叠的「原始提纲」，完整原文可查看复制、搜索命中保留。不改存储、API 或生成新摘要。提交 `a8558329`、`f45f0e46`。
- 3 条 Chrome 记忆回归通过（5.302s），覆盖原文保真、技术内容默认折叠、混合角色阅读、展开保留和原文搜索。实际记录的全角括号／冒号补入夹具后先红（工具回报被当对话），修复后同用例通过（1.881s）。JS 语法、diff check 通过；无需重跑后端／全仓。既有开发实例直接读取更新的静态资源，已在 8788 用小鲸原记录核对；实际截图在 `~/.xbot-build/ui-shots/memory-detail-reading/final/`。截图工具报告设备接口 401 与旧 favicon 端口拒绝连接，未影响本页渲染，未扩修其他接口。
- 级别：本地 main 已合入，专项浏览器及真实页面核对通过，待用户日用。Not run: 全仓、真实模型、无关旅程及跨平台原生窗口。

## 2026-11-01 CLI profile 第二刀：生成头像 seed

- L 档，沿已授权路线在同份 dev 补调用用法、两种结构、真实写口与读缝；独立结构审计在加入 legacy Rename 空 seed 钉脸和旧 custom 句柄 fresh 判定后 PASS，才改产品。实现 `f5b6702e`，补充测试/文档 `0053145b`，旧配置兼容修正 `3816c3bb`：`profile set --self --avatar-seed SEED [--description/--timezone] --json`，回执与 show 加 avatar/seed，不传图片。沿首刀当前实例身份、原 Kernel、Sink、任务寿命和 UI；不改视觉、Event/Inbox、权限默认放行、persona，不新增组件/事件/任务表。
- profile 快照覆盖头像元数据；生成、上传、clear 与 legacy pin 通过私有 typed 写缝、原 profile→config 锁、fresh merge 保存。无关旧句柄 SaveConfig 保留磁盘最新头像；clear 保最新 seed。全部先校验，失败不发布或清旧图；生成元数据成功后锁内 best-effort 清理，失败不伪报未保存，不承诺上传三文件崩溃原子。AvatarBytes/ExportTemplate 共享锁内 fresh metadata 判 custom 与读取，generated 不服务遗留 custom 文件；可选 generator 锁外调用。
- 自有旧红：旧 B SetPinned 回滚 A 新 seed，旧 handle clear 覆回旧 seed，旧 custom handle 服务 generated 后遗留图，同对象头像读写 race，web 非法组合先改头像。窄 bot/kernel/web/CLI 头像、模板、旧改名与 profile 回归已绿（0.394/3.638/0.519/0.521s）；新增保存失败保旧快照/图、cleanup 失败仍报告已保存与跨 live 头像元数据检查通过。必要定向 race：bot 4.317s、kernel 4.582s，相关五包 vet、格式与 diff 检查通过。分支 atlas audit 退出 0、无断链，30 项既有非致命提示；合入工作区审计同样退出 0、无断链，43 项非致命提示（含现有未提交设计、review 和测试临时文件），未扩修。
- 独立方持有新的 `cli_profile_avatar_*` 测试（测试提交 `445a50bc`）；旧版真实 chat 已取得 `flag not defined: -avatar-seed` 红。`f5b6702e` 首跑真 Chat/Unicode、ServeAtomic（非法组合旧文件不变、runtime file 拒绝、合法组合保存并清旧文件、双 root）和 Browser（长短轮、两处头像真实来源与像素变化）均绿（5.429s，短轮 672ms）。实现方未改期望或重复 E2E；代码主体独立审计无 P0/P1，其指出的受支持旧配置缺 name 在 readAvatar 沿目录名补齐：复用 `TestOldConfigWithoutAvatar` 扩导出/生成器行为，旧红后 `3816c3bb` 定向 race 绿（4.227s）、bot vet 绿，不重跑 E2E。级别：头像 seed 已合入本地 main、所测端到端通过，最后兼容增量定向通过；未日用。合入 `8e6a4684` 保留同期环境发送者、经历详情及 Tab 标题改动，独立测试随后合入为 `90e5918b`；在此组合版本仅补 `TestRuntimeProfileRefreshesOtherLiveHandleBeforeNextRequest` 与 `TestEnvironmentSenderUsesPersonName`（0.870s）确认新头像快照和正式姓名环境同存。Avatar E2E 继续精确对应 `f5b6702e`，未描述为在每个后续 SHA 全部重跑。
- 保留完整旧 profile 工具与即时刷新（改名及其组合未等价）；旧 `profile avatar <name> --file/--clear` 在 endpoint 存在时拒绝，help 标明仅无 endpoint 离线，尚无 runtime 上传/clear。Not run：全仓/三遍、真模型、Linux/Windows、生产性能、日用；runtime 改名、上传协议、research/runner 与 MCP 后续批次未实施。未 push/发版或改日用运行实例。

## 2026-11-01 记忆页单栏与经历搜索

- M 档，按用户确认原型实现：隐藏「关于你」，「关于本 Bot」预览前两条、手机一条，可展开完整 Markdown；经历在下方单栏，搜索框直接可见。流水为空时不占页脚。
- 搜索复用原经历接口与 SQLite 分页，在整个 Bot 库过滤摘要、关注点、提纲，字面匹配、英文忽略大小写，保留时间/id 顺序；不新增索引、不改模型侧 BM25 与 person 注入。查询保留输入法过程、焦点和展开态，清除恢复已加载列表，旧词与旧 Bot 的回包不覆盖新页面，失败可重试。
- 后端回归在旧实现上命中「q 被忽略」先红，修复后专项 HTTP 通过；6 条相关 Chrome 回归通过（7.357s），覆盖单栏、折叠、搜索、分页、失败、IME、迟到回包与切 Bot。独立代码复审发现 Enter 会重置完成搜索的游标，去掉冗余重查后定向 Chrome 通过（3.894s）。
- 真实 serve + Chrome 的 `TestChatTabsJourney` 与 `TestMemoryPanelWhole` 通过（16.286s），包括首屏之外的旧记录、45 条匹配的分页、清除和提纲；为修复截图捕获到动画中间态，现有截图 helper 关闭截图期间动画，再跑该旅程通过（14.954s）。浅深色及 1440/768/390 的 DPR 2 证据在 `~/.xbot-build/ui-shots/memory-single-column-20261101/product/after/`，已目视对照确认原型。
- scoped vet、JS 语法、gofmt、diff check 与 atlas audit 通过；audit 保留既有文档长度、旧验证戳、未归档文档等提醒。Not run: 全仓及无关旅程、真实模型、Windows/Linux 原生窗口、生产性能专项。实现提交 `16f9ab0c`；已构建并通过既有 SIGHUP 路径让 8788 桌面开发版优雅重载，运行 tip 核对一致；本机小鲸的真实库 TTS 查询返回 5 条且无匹配查询为空。级别：本地 main 已合入，隔离真实入口端到端通过，待用户日用验收。

## 2026-11-01 CLI 平台批 0/1：当前 Bot 普通资料

- L 档，沿用户已认可路线完成一页实施设计、候选依赖复核与独立设计审计，新增 [ADR 0043](adr/0043-cli-platform-runtime-entry.md)，不改旧 Accepted 正文。首刀仅 description/timezone；头像/改名旧工具和原即时刷新保留，Event/Inbox、生产永远放行及脱敏不变。产品提交 `07f17164`，实际任务寿命修复 `fe7e1f83`，增量 `c3cd3f0c` 修复 coding 原队列交接空窗及跨活 Kernel 资料快照同步。
- serve、桌面和独立 chat 在原进程提供窄 profile HTTP 入口；Shell、本机可写子代理和本机编码进程获得同一当前二进制/root/endpoint/Bot 语义。`profile show/set --self --json` 真调用原运行时，root mismatch/不可达非 0，不另启 Kernel 或伪装文件写入。ApplyProfile 文件锁内合并显式字段，受锁普通快照不整份改写 cfg；当前活轮 Sink 发 profile 提示，现有 UI 在长轮和自然短轮立即读回资料，保留编辑草稿与迟到请求保护。
- 独立代码复核命中 serve.Pass 收尾早关后台入口：已取得真二进制旧红，修复后独立后台用例绿。本机 Local/code 原 live 集合持有入口，实际退出并排空原队列后无锁 OnIdle 释放；code 移除旧 live 项延到原 wake 启动继任之后，服务 context 为寿命上界。两个 Bot 真句柄的定向用例确认旧 Pass 保存后，当前活轮下一次模型请求已含新时区/profile 指纹；已成功落盘后刷新失败不会伪报保存失败。
- 实现方检查：普通 profile 合并实际旧红新绿；协议/root/JSON、同对象并发保存、持处理锁自改、model request 短指引、服务未停时淘汰/重建旧 endpoint 均通过。bot/kernel/web 定向 race 通过；Local 与 coding（含已排队第二任务）寿命、跨活句柄同步的定向 race 通过。既有头像流中刷新浏览器、sudo 环境/后台 helper、编码进程流式与密钥剔除回归通过；受影响八包 vet、JS 语法、diff/格式检查通过。分支 atlas audit 退出 0、无断链，30 项既有非致命提示；合入工作区审计同样退出 0、无断链，共 43 项提示（size 1、stale 8、dev 5、unabsorbed 3、unmapped 26，包含现有未提交设计、review 与测试临时文件），未扩修无关文档。
- 独立验证方拥有并运行 `TestCLIProfile*`：serve 双 root、非主身份、web PATCH 后 show、失败/root mismatch/非法时区、独立 chat、离线帮助与无运行时失败、真实浏览器长/自然短轮即时资料刷新，以及真实 Webhook→serve.Pass→dev 主轮收尾后 CLI。实现方未修改这些用例期望、未重复执行独立 E2E。独立证据按增量归属：Offline 在 `07f17164`，serve/chat/Browser 在 `fe7e1f83`，最终后台用例在 `c3cd3f0c` 再绿；不称全部用例在每个提交重跑。合入 `59526c85` 保留同期单栏记忆/搜索改动，独立用例三个测试提交随后合入；在组合后的 `b1bd9c9b` 仅补受影响的真实 Browser 验证，通过（5.661s，脚本短轮 691ms，长短轮资料读取均先于 SSE 结束），证据在 `~/.xbot-build/ui-shots/cli-platform/`。
- IPC 小样本：100 次本机 GET，fresh connection、含 root 检查，额外 p95 **0.325 ms**，直接 callback p95 42 ns；排除 CLI 进程启动，非模型/生产 p95。级别：普通资料首刀已合入本地 main、所测端到端通过，未日用；未 push/发版/改用户运行实例。Not run：全仓/三遍、真实模型、Linux/Windows、生产性能与 CLI 进程启动专项、research 只读 CLI 分类、远端 runner 回连、头像/改名完整运行时 CLI、MCP 后续批次。

## 2026-11-01 全仓减法清理

- 按用户的奥卡姆剃刀要求扫描全仓引用。删除 3 个无调用方 Go 私有函数、11 个闲置 JS 函数、旧主列过滤缓存和工具气泡 CSS；删除只测旧前端投影的两条单测。三份已取代的开发设计删除，atlas 去掉失效设计链接，性能约束只在 AGENTS 维护。审计忽略代码示例中的伪链接，仍检测真实断链。
- 六条旅程保留有效行为，淘汰裸正文、三段 delta、固定模型调用数等旧假设；改用真实送达结果、chat 时间线及 read_file 结果指针。供应商切换用有效绑定；顶部标签旅程复用共享模型替身，删除额外 HTTP 包装服务器。
- 私有 smoke 服务改用现有 --interval 100ms，产品默认仍为 15s；唤醒拨已有测试时钟，静默检查等回合完成，协作等跳数上限终态，取消固定 90 秒等待。唤醒用例从本轮初始约 126s 降到约 10s。
- 验证：六条旅程与 Responses 绑定分组通过（18.404s + 19.666s），真实 serve/Chrome 顶部标签通过（10.717s），定向 kernel/link/web、JS 语法、格式、依赖 tidy 与 atlas 链接检查通过。go.sum 去掉旧校验项并补当前依赖图校验，不改 go.mod 或依赖版本。审计仍有既有长度/验证戳提示。未跑全仓、三遍或无关性能测试。证据：`~/.xbot-build/retire-tests/`。

## 2026-11-01 验证范围与资源预算

- 按用户要求取消固定三遍、每次全仓及全守护冒烟。统一约定见 [TESTING](TESTING.md)：按影响一次，通过即停，复用日志和缓存；并发问题定向复现或 race，截图和性能按实际影响选取。同步 AGENTS、LESSONS、思想节点、守护执行说明及现有设计中的旧门槛。
- CI 纯文档变更跳过产品测试，同分支新提交取消旧 CI；不按 Markdown 后缀跳过运行提示。现有全仓、race、跨平台构建与头像检查保留。
- 本次只检查 diff、链接、YAML 结构和过滤场景，并跑 atlas audit；未跑 Go、浏览器、smoke 或性能，按新的范围约定执行。

## 2026-11-01 聊天主区与顶部标签（feature/chat-tabs）

- L 档，用户确认原型后授权整体UI改版与旧布局测试迁移。身份操作、可扩展标签、全宽聊天/记忆/轨迹/任务/上下文/收件箱；聊天固定第一、更多管理显隐，资料/模型/群成员入口保留。正式内容复用原API，不增内核或第二套聊天与轮询。决策 [ADR 0042](adr/0042-chat-workspace-tabs.md)，设计与逐项删测理由见 [chat-tabs](dev/chat-tabs.md)。
- 实现和迁移分派两Agent，负责人独立用例与宿主验收；独立设计复核、三轮代码复审最终无剩余 finding。修复消息头像ID导致节点重建、4px动画导致历史偏移、记忆空数据与分页边界丢失、跨页搜索/操作反馈、错误刷新损坏任务草稿、模型目录迟到后资料缓存不重绘。侧栏拖动、语音、换绑、更新、群组及往来业务断言保留。
- 全量 `go test -count=1 ./...` 通过；web整包三遍通过（290.788s）。既有CLI队列测试清理竞态改为等待实际写完，30遍及race3遍通过。真二进制 `TestChatTabsJourney` 最终三遍通过（34.729s），浅深色、1440/768/390、真实存储与工具投递、六页和资料编辑；最后60样本各轮切页p95 67–69ms，仅本机脚本化数字。
- 六条旧smoke在本分支与原main同样因旧 done.text/delta 契约失败，未放宽guard；长程旅程通过。DPR2原型对照、同内容改前改后像素差、取色及桌面壳点击验证留在 `~/.xbot-build/ui-shots/chat-tabs/`，日志在 `~/.xbot-build/chat-tabs/`。
- 级别：本地新UI真入口端到端通过；未push、未发版、未变更日用服务。用户日用、Windows/Linux原生窗口、真模型及生产性能未测；不宣称旧六旅程门禁或用户日用通过。

## 2026-11-01 设置内容区滚动（feature/settings-content-scroll）

- S 档。沿用设置双栏壳，右栏 `.setcnt` 内缩独立滚动，滚动条不再贴弹层外缘；主标题行吸顶，侧栏和关闭按钮不随内容滚动。各设置页共用规则，窄屏保留原固定顶栏，没有新增动效或应用页特判。
- `go test -count=1 ./internal/web` 通过（90.005s）。新增真实 headless-shell 行为回归覆盖连接应用、通用长页：内容滚动 300px，侧栏/关闭按钮/标题位置偏移 <1px，滚动区上下右均内缩。旧 CSS 上先红（apps 标题随滚动移动），修复后绿。
- 1440×900、DPR2 浅深色前后与滚后截图在 `~/.xbot-build/ui-shots/settings-content-scroll/{before,after}/`，已本机自检，数据为 scripted 应用目录；未独立设计评审或用户日用验收。仅 CSS 改动，无新增请求/事件处理；未测持续帧率或生产 p95。
- 按用户范围只跑相关 web 包一次，不跑全仓或三遍。Not run: 全仓测试、go vet、Linux/Windows、独立复审、用户日用、生产性能专项。本条级别：分支浏览器行为通过；按用户后续授权合本地 main、重启 8788，不 push。

## 2026-11-01 保险库全局单库、Bot 共享（feature/vault-global-shared，本地分支）

- L 档，在 6bd6c31f 上改装配缝，不重做皮。唯一产品入口 `vault.OpenGlobal(root)` 不接收 Bot 名；本地和 vaultd 各按既有后端选择使用固定产品 namespace，OAuth namespace 不动。设置 `/api/secrets` 的 metadata DTO 永无 Value；连接应用 NeedsSecret（含 Context7/MCP 环境键）、CLI、出站 HTTP、LOCAL_SUDO askpass 都接同源。CLI 暂留存在性校验的 Bot 参数，但语义全局，帮助明确说明。
- 安全合并旧 Bot 密文及产品 secrets.json：目标原写锁内检查全部冲突、一次提交后才删源。非空同名值或域名不同显式失败、保留源；同值同绑定可重入，空值让位非空；本地迁往 daemon 时任何远端同名均拒绝覆盖。删除旧 Bot 前也先迁移，已迁全局库不随 Bot 删除/改名。没有新增依赖、模型工具或明文 daemon 出口。
- UI 去掉当前 Bot 标记，切 Bot 不换列表、不清草稿；导语和删除确认说明所有 Bot 共用。按政东中途纠偏，主钮纯「添加」直接开 nestcard，删除下拉/箭头及其样式；sudo 仅保留卡内快捷条和空态次钮。卡行/管理、chip、搜索及浅深皮沿现有实现。
- 缝上测试：全局 Open/API 写入后跨 Bot 的真实出站注入、NeedsSecret 写入及 MCP 读取、askpass 同源；另守密文/明文合并、值/域名冲突保源、幂等不复活、OAuth 隔离、daemon 合并及冲突显式失败、Bot 生命周期和 CLI 跨 Bot。临时回退 Bot 的旧 per-bot 路径投影，`TestGlobalAPIAndNeedsSecretCrossBotConsumers` 实际因 askpass 读不到 API 值而红，恢复后绿；不是只看 URL 有没有 Bot 名。
- 真二进制 `TestVaultSettingsSudoBinary` 最终三遍通过（29.358s）：真实 serve/HTTP/浅深 Chrome、两 Bot 的 HTTP 注入及脚本化 sudo -A，模型请求/transcript 无值、删后 askpass 失败。截图 `~/.xbot-build/ui-shots/vault-global-shared/after/{light,dark}/`；before 复用上一条已留存的截图，已本机对照原型查看。三遍 settings-open 127–185ms，secrets GET 每遍 30 样本 p95 2–7ms；既有 daemon overhead 300 次 warm GET p95 0.127ms。均为本机 fake/scripted 数字，不是生产交互/持续 60fps 或后台减速指标。
- gofmt 无输出、全仓 vet、diff check 通过；atlas audit 退出 0，保留既有链接/陈旧/未索引告警。cmd/xbot、bot、inbound、kernel、link、vault 整包三遍通过；web 本条缝上/API/应用/审批及 Chrome 专项三遍通过（17.451s），整包三遍在 600s 超时前出现 DevicesPairBrowser/remote、CLIEventQueue 清理竞态、QuestionTypedReplyStopsTimer 计时失败，未改用例或扩修。全仓 `go test -count=1 ./...` 已完整跑过，最终仅 OAuth 回调端口占用和旧 DevicesPairBrowser/remote 未绿；OAuth 单项三遍复跑通过，设备配对在未修改 6bd6c31f 归档复现相同失败；三遍另两类失败未重新归因。六基础旅程 smoke 在当前分支和原 tip 归档均因旧 done.text/delta 契约同样失败；长程测试随全量 e2e 通过。
- 级别：分支专项真入口端到端通过；未合 main、未 push、未重启日用服务，不宣称整体旅程门禁或用户日用通过。设计保留，未改 guard/Agent 指令/发版文件；不安排合入后逛验收。Not run: 真实密码/系统 sudo 策略、真模型、Linux CI、独立设计/代码复审、设计师回签、用户日用、生产 p95/后台减速/持续帧率专项。日志 `/tmp/vault-{all-final,repeat3,web-seams3,binary3,old-red,baseline-device,guard-smoke,baseline-guard,atlas-final}.log`。

## 2026-11-01 浏览器测试默认 headless-shell（feature/chrome-headless-shell）

- S 档。只改测试启动与取证脚本：Go / Node 共用各语言的 `internal/browsertest` 选择器，优先 `XBOT_TEST_CHROME`，否则 PATH 的 `chrome-headless-shell`，再找已有 Playwright headless-shell 缓存；不回退完整 Chrome，不安装依赖，不改产品浏览器、UI 或日用服务。
- 绿条验证选中的 executable 与真实启动路径：本机实际启动 `~/Library/Caches/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-mac-arm64/chrome-headless-shell` 并读 DOM（0.61s）；选择回归覆盖 PATH / 缓存 / 显式覆盖，Go 与 Node 结果一致。把共享页面启动点回退成旧 Chrome.app 选择后，启动点回归实际失败；恢复后通过，未为验红启动完整 Chrome。
- `go test -count=1 ./...`、`go vet ./...`、gofmt、diff check 通过。改动包整包 `-count=3`：browsertest / pipe 通过，web 唯一失败为既有 `TestCLIEventQueue` TempDir 清理竞态（上一条头像里程碑已有 HEAD 复现）；不扩修。新路径上的真实滚动帧测试通过，20 样本帧间隔 p95 19ms、锚偏移 3.66px，仅本机测试数字，不作 60fps 保证。
- 真二进制自定义供应商 / 历史窗口 / 保险库三组 smoke 均连续三遍通过，启动日志明确记录 shell 路径；自定义目录 p95 0.71–1.77ms、保险库 GET p95 2–4ms（本机 scripted）。另显式启用外部 `xbot-account/public` 的 pipe 浏览器用例三遍均在脚本第 60 行失败：导航销毁 evaluate 上下文；已确认启动的是 shell，但这条外部资产旅程未绿，未扩修或声称是基线问题。
- 级别：本地分支启动路径与三组真二进制专项端到端通过；整体门禁未全绿，未合 main、未 push。atlas audit 退出 0，保留既有链接 / stale / unmapped 告警。Not run: Linux / Windows CI、用户目视 Dock 日用验收、生产性能专项、独立评审；未调用真实模型。日志 `/tmp/xbot-headless-{target,red,all,vet,repeat,smoke,pipe-live,atlas}.log`。

## 2026-11-01 设置主人头像（feature/owner-avatar-settings）

- M 档。通用「我的头像」复用现有 setpane/按钮/字体，无新 CSS、颜色或动效；data root `owner-avatar` 是唯一真源，GET/POST/DELETE 与 session 字段串起预览、侧栏即时更新和刷新读回。类型/大小对齐 Bot，无 localStorage 图片副本。
- 专项 `TestOwnerAvatarSeam` / `TestOwnerAvatarBrowser` 连续三遍通过：认证/同源、上传/字节读回/重开服务对象、拒绝类型/超限不损旧图、清除幂等、预览与 me 同源、失败保留原图。deletion test：删除 POST 路由后 seam 红；恢复 me 原先无 URL 的渲染后浏览器红（me did not use saved URL）；恢复实现后三遍绿。删除旧 `avatar_owner_ui_test.go` 的源码/颜色文本断言，以接口和浏览器行为替代。
- 真实二进制隔离 serve + Chrome 的 `internal/web/testdata/owner-avatar.cjs` 三遍通过：真上传、整页刷新读回、清除、再刷新回原线稿，不调用模型。初三遍 session GET 每遍 20 样本 p95 1.46–2.40ms，上传到 me 14.1–36.7ms；最终含浅深切换/真实图片的三遍 GET p95 1.55–58.78ms、上传到 me 21.8–47.0ms；仅本机接口/页面数字，不是生产、IPC 或持续帧率保证。浅深 1440×900 DPR2 默认/悬停/自定义/清除证据在 `~/.xbot-build/ui-shots/owner-avatar/after/{light,dark}/`，浅色改前在 before/light；局部自检，未独立设计师验收。shoot 的旧设备列表 401 是基线已有，未扩修。
- gofmt、go vet、diff check、atlas audit（退出 0，保留既有告警）通过。全量 `go test -count=1 ./...` 未全绿：providerauth/TestLogoutStopsPendingLogin 端口占用，单独三遍通过；web 全量一遍通过，但整包三遍在 TestCLIEventQueue 临时目录清理失败，当前单独三遍和 HEAD 归档单独三遍均复现。日志 `/tmp/owner-avatar-{tests,web3,cli-baseline,vet,deletion-api,deletion-ui}.log`；不为头像扩修已有 CLI 生命周期。
- 级别：本地分支头像专项端到端通过；未合 main、未 push，整体门禁未全绿。Not run: Linux CI、Windows 字体实测、独立评审、用户日用、生产 p95/IPC/持续帧率专项；已有旅程 smoke 的结果包含在全量测试中，不另声称全局日用通过。

## 2026-10-31 设置保险库与本机 sudo（feature/vault-settings-sudo，本地分支）

- L 档，按更新后的 Grok 风格 handoff，废弃旧安静行 UI。设置「保险库」位于权限后、搜索前，per-bot 全部/出站/本机计数、名称/域名搜索、轻底圆角卡与管理；添加下拉、LOCAL_SUDO 预填、管理只改域名/重设值、D5 删除、全空与筛选/搜索无结果。没有迁移供应商/搜索密钥，没有改并行权限卡、AGENTS/CLAUDE、发版或 sudoers。
- 唯一存储缝为 OpenBackend；API DTO 永不含 value，SetDomains 在原写锁内保留旧值，本地/daemon 同契约。Shell.Run 的进程环境缝装配临时 0700 SUDO_ASKPASS；主线与子代理同一绑定，现读固定 LOCAL_SUDO，密码不入 env/助手文件，后台保留至实际退出。vaultd 不开明文出口。注册身份 epoch 修复重开 vault 快照的脱敏形式缓存碰撞，回退此修复后 TestShellFormsFollowReopenedSudoVault 实际红，恢复后绿；空域不出站与非 ASCII 域名拒绝同守。
- 专项三遍全绿：vault Set/HTTP URL、头、体拒注入，API 元数据、同连接器 backend、域名保值/重设/删除，CLI 入口，mounted/scoped Shell，前后台助手清理、删除后失效，Chrome 增删/管理/过滤/搜索/迟到响应。vault、tools、link、cmd 整包三遍通过；web/kernel 整包三遍完整跑完但旧组未绿。全仓 gofmt、vet、diff check 通过，atlas audit 退出 0，保留既有告警。
- 真二进制 `TestVaultSettingsSudoBinary` 对真实 serve/HTTP/页面、浅深 Chrome 1440×900 DPR2，脚本化模型与 sudo（不碰本机密码/系统策略）最终三遍通过（24.945s）：管理改域名/重设值、切 Bot、sudo -A、模型请求/transcript 无密码、删除后 exit 4。最终三遍设置打开 126–135ms，secrets GET 每遍 30 样本 p95 2–3ms；是本机脚本化交互数字，不是生产/IPC/60fps 指标。截图 `~/.xbot-build/ui-shots/vault-settings/after/{light,dark}/`，含列表、添加、sudo、管理、重设、删除、过滤、无匹配、空态，已本机对照新 shots/ref 查看。
- 全量一遍未绿：kernel 长轮/记忆日志、memory 月日志、web 时间/供应商/审批/粘贴/人设/提问、work description，在 HEAD 2a5f3044 归档复现；初跑的 vault 非 ASCII 校验回归已修且整包三遍绿。web 三遍另有 CLIEventQueue/VoiceRealBrowser/DevicesPairBrowser 偶发失败，baseline 单次未复现，不扩修。既有六旅程 smoke 均卡旧 done.text/delta 契约，HEAD 归档同样复现；本条新 smoke 绿，不声称全局旅程无退化已证明。日志 `/tmp/xbot-vault-{tests,repeat,baseline,baseline-extra,smoke,baseline-smoke,binary3,atlas}.log`。
- 级别：本地分支专项真二进制端到端通过，未合 main、未 push、未重启日用服务；整体门禁未全绿，不宣称用户日用通过。设计保留，未经用户确认不移 guard。Not run: 真实系统 sudo/真实密码、真模型、Linux CI、独立设计/代码复审、用户日用、生产 p95/IPC/后台减速/持续帧率专项；不安排合入后逛验收。

## 2026-10-31 全仓测试修绿（feature/fix-full-suite，基线 f743221d）

- S 档测试维护集合；未改生产代码或 UI、保险箱、权限策略。未 push、未合 main。
- A：routine precheck API 的 403 期望过时，改为经 review 后 200 且命令落盘，覆盖保护路径、ask/off、删除、外发和旧权限开关。park 四例仍用退役的 `shell`、缺 description、用裸助手正文交付；改用 `sh` 与用途、say 工具，明确只在严格测试内核测挂起/允许/拒绝/超时/断线。另加 permissive 经真实工具 review 执行、无审批事件/卡的 HTTP 回归，旧权限开关开着且机器 ask 也不出卡。ask 两例的替身错把独立 system-reminder 当成选择；跳过提醒并用 say 交付，设 permissive 验证问句卡仍保留推荐超时与手动选择语义。
- B：长轮的固定 T/H 夹具被增长的共享前缀挤占；按实际组装前缀补回工作负载余量，仍守 40 步最多 10 次折叠、最新结果完整、原文保留，并加强检查消息加工具定义不超 H。kernel/memory 月日志夹具用月末减一月，10 月 31 日归一化成 10 月 1 日；改从月首减月，kernel 固定月末重现边界，保留回滚和 64 KiB 守卫断言。work shell 夹具补必填用途。
- B/UI：时间分隔测试仍放入已不进 chat 的定时原始记录，改测送达回复；供应商测试把内部配置名当展示名，fixture 补显式 custom/display_name，仍测原型属性名与迟到响应；人设卡删旧工具栏结构断言，保留导入/搜索/保存/删除行为。按仓库规则删除查 JS/HTML/CSS 文本的 paste_ui_test，已有图片 HTTP 行为守护保留，新增当前 chat 重载图片 ID 回归。不改界面资源。
- 基线归档独立运行逐条重现上述 15 个顶层失败（`/tmp/xbot-fix-baseline.log`）；修复后四包整包 `go test -count=3 ./internal/kernel ./internal/memory ./internal/web ./internal/work` 通过，分别 169.346s / 0.769s / 433.687s / 0.778s。最终 `go test -count=1 ./...` 全绿（`/tmp/xbot-fix-full.log`，web 146.111s、e2e 72.307s）；gofmt 无输出、全仓 go vet、git diff --check 通过。atlas audit 退出 0，保留既有链接、陈旧和设计索引告警，未扩修。
- 级别：本地分支自动化回归全绿，未合入、未宣称用户日用或整体旅程验收。以上为测试耗时，非生产性能数字。Not run: 真模型、Linux CI、用户日用、交互 p95/IPC/后台减速性能专项。
## 2026-10-31 OpenCode Go 缺少会话头（feature/opencode-go-session）

- S 档：`provider.New` 为客户端生成独立 UUID，Completions 在官方 `opencode.ai` 的 `/zen/go/v1` 与 `/inference/go/openai/v1` 请求上发送 `x-opencode-session`，普通重试、工具续步、多轮及共用客户端的辅助调用复用标识；其他端点不加头。客户端重建后换新标识，不宣称 Bot 跨重启持久会话。UUID 依赖原已存在，仅改为直接依赖。
- 新单元回归在旧实现实际返回 HTTP 400 / MissingSessionID，修复后通过；两个端点、尾斜杠、502 重试、后续轮次、独立客户端与非目标 host/path 均覆盖。provider 整包最终三遍绿（12.093s）；新增真实 serve / registry 绑定 / API / SSE / `send_message_to_user` 续轮 / 双 Bot 隔离冒烟三遍绿（3.978s），9 次 scripted 回复约 78–180ms；不是生产模型延迟指标。上游全部为本地模拟代理，无真实模型调用。独立只读代码复核无阻塞/重大问题。
- gofmt、diff check、全仓 vet 通过；atlas audit 退出 0，仍有仓库原有 stale/broken link/unmapped 告警。全仓测试完整跑完未绿：kernel 长轮/记忆守护、memory 月容量、work shell 描述，以及 web CLI 清理/时间分隔/供应商/配对/审批/粘图/人设/提问等；与本日既有基线记录同组，未扩修或放宽期望。日志 `/tmp/xbot-opencode-go-{all,vet,provider,atlas}.log`；已有归档基线 `/tmp/rate-limit-base-all.log` 可对照。完整 smoke 跑完（255.967s）仍未绿：六条基础旅程的空正文/无 delta 失败与 `/tmp/rate-limit-base-smoke.log` 同组；另有 busy、Responses 与 thinking 断言失败，未逐条重新归因；本次 `TestOpenCodeGoBinary` 未失败。日志 `/tmp/xbot-opencode-go-smoke.log`。
- 已重编并重启本机桌面开发实例 `http://127.0.0.1:8788/`（pid 69461，23:11:03 构建），真实 `/api/session` 返回 authed=true、version=dev、tip=2a5f3044。级别：本次专项端到端通过并在本机生效；全仓守护未全绿，尚未提交，不宣称整体旅程或用户日用通过。Not run: 真模型、Linux CI、生产 p95/IPC/后台性能专项、主人日用。

## 2026-10-31 永远放行、无工具权限卡（feature/always-allow-no-permission-cards，本地分支）

- 按用户明确要求固定 serve/chat 的 `Permissive: true`；旧 live permissions on 不能覆盖。保护路径不再绕过 permissive 硬弹卡，子代理与例程 API 同口径；links 填 key 输入卡保留，严格测试/评测内核与权限分类保留。同步 ADR 0019 和 atlas，未动界面、代理或运行中的服务。
- 两条回归先在旧实现实际红（保护路径出卡、live on 重开卡），新实现专项三遍通过（kernel 0.845s、cmd/xbot 0.735s，测试总耗时而非交互 p95）；cmd/xbot 整包三遍通过（16.491s）。gofmt、diff check、全仓 go vet 通过；atlas audit 跑过，仍有已有链接/陈旧/设计索引告警。
- 全仓一遍与改动包三遍已尝试但未全绿：kernel 长轮/overflow/记忆日志失败，memory 月日志、work description、web CLI/时间分隔/供应商失败；90s 包级超时涉及 e2e/kernel/tools/web。未逐条重新归因，不扩修。日志 `/tmp/always-allow-{full,repeat}-final.log`。
- 级别：本地分支专项回归通过，未合 main、未发布、未重启；不宣称端到端或用户日用通过。Not run: 真二进制/浏览器联合旅程、真模型、Linux CI、用户日用、交互 p95/IPC/后台性能专项；全仓及 kernel 整包三遍未完整通过。

## 2026-10-31 限流等待上限与一次备用（feature/rate-limit-end-or-switch）

- M 档，仅 Provider 共用重试、供应商配置与启动 selector 装配。`config.ModelSelector → RateLimitSelector → FixedSelector` 为唯一装配 seam；每次唤醒累计额度等待默认 180 秒，可用 providers.json 的 `rate_limit_wait_seconds` 配置。三协议读取 Retry-After/reset 建议，长等立即以 `ErrRateLimitWait` 结束；显式 `fallback: {provider, model}` 最多切一次，后续工具步保持备用、共享剩余等待预算；无备用不声称换模。普通 5xx/超时维持两次短重试，已有输出不重试/切换。不碰 UI、主列窗口、控机、公式、AGENTS/CLAUDE、发版脚本或 lock。
- seam HTTP 测试覆盖三协议、累计无建议等待、2 秒短重置、usage-limit 分类、reset 解析、无备用、一次备用与备用失败，断言错误类型/请求数/模型目标而非日志名字。临时回退共用重试层修复，新增 seam 测试实际失败，恢复后通过。config/provider/cmd 三包 `-count=3` 全绿（10.497s / 1.539s / 4.041s）。
- 新增 `TestRateLimitBinary` 真 serve / registry / API / SSE 与真实到期 Timer，上游仅 scripted。Timer 的 slot 被消费、trace 写 failed、现有 serve 错误出口记录原因；备用经真实工具步发消息成功，主模一次、备用两步、不回跳。真二进制专项三遍通过（5.941s）；并行全量检查时交互观察 135–216ms（6 次样本上限约 216ms），Timer 含重启观察最大 248ms；不是生产 p95 或 IPC/后台性能测量。
- gofmt、diff check、全量 vet 通过。最终 `go test -count=1 ./...` 未绿：kernel 长轮/记忆守护、memory 月日志、web 时间分隔/供应商/审批/粘贴/人设/提问、work shell description；未改动 HEAD b04e79fb 归档全量复现同组失败。既有六条 smoke 也未绿，HEAD 归档复现相同输出契约失败（done.text 为空 / 无 delta）；不扩修。最初全量另有一次 CLIEventQueue 临时目录清理失败，baseline 单项三遍与最终全量均未复现。日志 `/tmp/rate-limit-{all-final,base-all,smoke,base-smoke,repeat,binary3,before}.log`。
- 级别：分支专项真二进制通过；未合 main、未接 8788、既有旅程守护未全绿，不宣称整体端到端或用户日用通过。开发设计保留供主管验收，未经确认不移入 guard。Not run: 真模型、Linux CI、主人日用、生产 p95、IPC/后台减速专项。

## 2026-10-31 公式复制同槽对勾（feature/formula-copy-check，本地分支）

- S 档，仅按 handoff [t741u] 调整 formulaBlock：删除 copied-tip，可见成功态为右缘同槽绿 check，约 1.6 秒恢复 copy；aria-label 可报「已复制」。默认透明、hover/focus 与成功态 fill-2，普通 hover 图标透明度 .72、成功 1；不改公式分流、逐行拆块、普通代码或美元公式。
- 浏览器回归覆盖浅深 copy/check 互换、绿色、无 tip/文字、恢复、整行及键盘复制；最终测试回退旧 JS/CSS 实际红（copy/check icons missing），恢复后 Formula/Math/Markdown 专项三遍通过（10.701s）。虚拟时间驱动关闭该槽 transition，真实 Chrome 截图保留原动效。1440×900 DPR2 浅深 before/after 的 default/hover/copied 在 `~/.xbot-build/ui-shots/formula-copy-check/`，已查看；focus/blur 到下一帧 40 次采样 p95 19.6ms，非真实服务交互或持续帧率指标。
- gofmt、全仓 go vet、JS 语法、diff check 通过；atlas audit 通过并保留已有告警。最终全仓一遍、web 整包三遍均完整跑完但未绿；公式回归不再失败，其余 kernel/memory/work、时间分隔/供应商/审批/粘贴/人设/提问等与上一条已记录失败同组；全仓另见 OAuth TestLogoutStopsPendingLogin，未逐条重新归因或扩修。日志 `/tmp/formula-check-final-{all,web3}.log`。
- 级别：本地分支专项 Chrome 通过，未合 main、未 push、未重启日用服务，待设计师复审与用户日用。Not run: 真服务联合旅程、真模型、Linux CI、用户日用、IPC/后台减速性能专项。

## 2026-10-31 围栏公式块（feature/math-formula-block，本地分支）

- S 档展示修复，按新版 handoff [t740u]：只在 appendFenced 分流，空/text Unicode 等式逐行独立透明公式块；显式 TeX 成功整段一条，失败拆行。整条 click/Enter/Space 复制当前原文，成功提示 1.6 秒；悬停/聚焦软底，icon 仅提示。普通 Python 等代码路径与皮肤、输入草稿、美元公式路径不变。
- 最终单测在 HEAD 归档上红（公式仍是 code-block），新实现 Math/Markdown/Formula 专项三遍绿（7.549s），Chrome 覆盖按行复制、键盘、反馈消失、浅深默认透明与字色、KaTeX 成功/失败、持久消息和 Python 高亮。1440×900 DPR2 前后浅深截图含默认/hover/copied：`~/.xbot-build/ui-shots/math-formula-block/`，本机查看。focus/blur 到下一帧 40 次采样 p95 19.5ms，非真实服务交互 p95 或持续帧率。
- gofmt、go vet、diff check、atlas audit 通过（audit 保留已有告警）。全仓一遍与 web 整包三遍均已跑完但未绿，见 `/tmp/formula-tests.log`、`/tmp/formula-web3.log`：kernel/memory/work 与旧 UI/审批/提问等既有失败，web 三遍另见 DevicesPairBrowser；HEAD 归档定向复现时间分隔/供应商/审批/粘贴/人设五项（`/tmp/formula-baseline.log`），其余未逐条归因或扩修。
- 级别：本地分支专项单测/浏览器通过，未合 main、未 push、未重启用户服务，不宣称真服务端到端或用户日用通过。Not run: 真二进制旅程、真模型、Linux CI、用户日用、IPC/后台减速性能专项；按用户要求不再邀设计师复审。

## 2026-10-31 心里话来源行（feature/aside-thoughts，本地分支）

- M 档，按政东指定的 chat-timeline 扩展：整轮 `deliveries` 在存在 landed say 时选最后一条合格无 tool_call 裸正文，经 Record 同一幂等插入实现追加 `aside:<seq>`。非空非 SILENT、120 Unicode 字符上限（超长整段丢）、近重复/协议垃圾/路径 dump 过滤；媒体仍挂最后一条 say。投影世代升为 2，重建不改 transcript；不加模型轮或已说出口上下文记录。
- 前端复用 `addSourceEvent` 的 `sysline link` 来源行，灰字「查看心里话」，`setTip` 悬停全文、点击展开纯文本抽屉；现成 identity 合并计数，未绑定正式回复菜单，不进侧栏预览或引用摘要。未碰图片、缩图、提示词与守护层旅程。
- `TestChatAsideRecordWindow` 先在旧实现红，再覆盖 Record→窗口/幂等重开/过滤；last-eligible 与分支独立完成、媒体独立保留有专项。真实 POST messages→scripted Kernel→chat 验证 say 后的 aside；Chrome 验证 tooltip 可见全文、展开/收起、原文转义、无回复菜单、轮询保态。最终 bot 整包三遍通过（70.585s），chat/aside/来源行/stick/live/增量/语音专项三遍通过（68.986s）。单轮 say+aside HTTP 在三遍专项中约 0.42–4.90ms、约 723 字节；非公网/真模型/IPC p95 指标。
- gofmt、go vet、JS 语法、diff check 通过；atlas audit 退出 0，保留既有告警。最终全仓一遍完整跑完但未绿：kernel 长轮/MemoryFileLogGuard、memory 月容量、work description，及 web CLI 清理/时间分隔/供应商/审批/图片/人设/提问共 15 项，均在未改 HEAD 归档复现。Web 整包三遍尝试中另遇 DevicesPairBrowser 偶发失败，进程在约 316s 被终止，不能称整包三遍完成；没有放宽断言或扩修。
- 1440×900 DPR2 浅深默认/悬停/展开截图：`~/.xbot-build/ui-shots/aside-thoughts/{before,after}/`，本机已查看；日志 `~/.xbot-build/aside-thoughts/`。级别：本地分支专项 HTTP/Chrome 通过，未合 main、未 push、未重启日用服务。Not run: 真二进制联合旅程、真模型、用户日用、设计师独立复审、Linux CI、物理帧率/IPC/后台减速专项；未委派 Agent。

## 2026-10-31 粘贴超边图缩后落盘（feature/paste-downscale，本地分支）

- S 档，按用户指定的行为修正。`postPaste` 经导出的 `tools.FitImage` 复用原 `fitImage`，PNG/JPEG/GIF 超 2048 等比缩后保存（GIF 转 PNG），合规图原字节保存；超边 WebP 明确拒绝。原上传/缩后 10 MiB、每条 4 张/合计 10 MiB 及解码预算不变；不改 outimage、浏览器显示尺寸或 UI 资源。
- `paste_downscale_test.go` 在真实 paste HTTP 缝覆盖横竖 2340×1682 PNG/JPEG/GIF 返回 200、磁盘尺寸与比例、GIF 扩展名转换、GET 字节一致、合规图不变，以及超边 WebP 400 且不落盘；在旧实现上实际先红，再修绿。旧测试的整张拒绝断言由上述落盘行为回归替代，符号链接/体积/张数守门保留。专项三遍通过，墙钟 4.894s（含 fake 服务及其它 paste 回归，非交互 p95）。
- gofmt 无输出、go vet 全仓、diff check 通过；atlas audit 跑过，保留已有告警。全仓测试完整跑完未绿：kernel 长轮/月日志、memory 月日志、work description 和 web CLI/时间分隔/供应商/审批/粘贴静态 UI/人设/提问等失败（同日既有里程碑已记录此组失败），未扩修。改动包三遍：tools 通过（333.160s），web 出现同组失败后 600s 超时；paste 专项另跑三遍全绿。日志 `/tmp/paste-{red,focused,all,repeat,vet,gofmt,atlas}.log`。
- 级别：本地分支已实现、paste HTTP 回归通过，未合 main、未 push，不宣称整体验收或用户日用通过。Not run: 本机浏览器手动粘贴、真模型、Linux CI、用户日用、交互 p95/IPC/后台减速专项。

## 2026-10-31 删除 Bot 确认（feature/delete-bot-confirm，本地分支）

- S 档展示修复：仅删 Bot 确认改用技能同族 nestcard；移除通用 modal 的删 Bot 文字危险钮特判。点名标题、一句灰字 lead、取消与实心删除；保留原删除接口、主 Bot／最后一只的人话拦截和删后回主 Bot，不改其它更多菜单项。
- `TestDeleteBotConfirmBrowser` 浅深各三遍通过，覆盖实心按钮、无正文灰框／身份行、取消、Esc、焦点循环、两种拒绝及删除成功刷新；回退旧 app.js 后因缺少 nestcard 失败。Chrome 1440×900 DPR 2 前后与 hover 截图：`~/.xbot-build/ui-shots/delete-bot-confirm/`。20 次打开到下一帧 p95：浅 16.6ms、深 17.5ms（本机 scripted HTTP 页面，非真模型）。
- gofmt、go vet、diff check 通过；atlas audit 仅已有告警。全仓一遍未绿：kernel 两条、memory 月容量、OAuth 端口占用、work description 以及 web 十条失败；web 十条已回退旧 app.js 定向复现，日志 `/tmp/delete-bot-{tests,baseline-web}.log`。web 整包三遍未绿并在总计 10 分钟时超时（当时运行 `TestTranscriptWindowHTTP`）；另见 CLI event queue、设备配对、语音失败，未扩修或逐条归因。日志 `/tmp/delete-bot-repeat.log`。
- 级别：本地分支实现，浏览器 scripted 删除路径通过；未合 main、未 push，待设计师复审和用户日用。Not run: 真服务删除冒烟、Linux CI、用户日用。

## 2026-10-31 记忆提醒（feature/memory-reminder，本地分支）

- L 档，按用户指定设计，仅在 `reminderKindsFor` 经 `Channel.Register(Kind)` 接容量与轻量沉淀两条 reminder；不动 promptinject/envprompt、写入守卫、UI 或额外模型轮。容量读 Bot/当前人物 memory.md 实际大小，85% 起劝整理，已满明确提示，附当前/上限字节；两种各冷却 10 分钟，沉淀限有记忆存储的 web/terminal 人物对话。
- `TestRegisteredMemoryCapacity` 覆盖门槛两侧、已满/超限、Bot/person、冷却与整理后消失；`TestRegisteredMemoryReflection` 覆盖轻量文案、冷却、重建 Kernel 仍冷却、timer/无 memory 不发；`TestMemoryReminderPreservesProfileFull` 断言 ErrProfileFull 且拒写前后原文一致。三条专项三遍通过（2.668s，测试墙钟，非交互延迟指标）。deletion：临时删除两处 Register，实际正文缺失使容量/沉淀两测试变红；恢复后通过。
- gofmt 无输出、go vet、diff check、atlas audit 通过（audit 保留已有告警）。全仓一遍完整跑完未绿；kernel 整包三遍完整跑完（202.653s）仅 LongTurnStaysUnderTheCeiling / MemoryFileLogGuard 两条既有失败。全仓另有 memory MonthlyGuardAndRoundTrip、work ShellUsesBotDir，及 web 时间分隔/供应商设置/4 个审批/粘贴图片/人设/2 个提问共 10 条失败；全部在未修改 HEAD 归档定向复现，未放宽断言或扩修。日志 `/tmp/mem-rem-{target,deletion,all,kernel3,baseline,baseline-web,vet,atlas}.log`。
- 级别：本地分支已实现、注册缝专项通过；未合入、未 push、未声称真入口端到端或日用通过。Not run: 真二进制端到端冒烟、真模型、用户日用、独立复审、Linux CI、交互 p95/IPC/后台减速性能测量。不增加新 ADR（复用既有 reminder、未推翻现行决定），主管复审待交接。

## 2026-10-31 聊天图片尺寸（feature/chat-image-size，本地分支）

- S 档展示修复：单图按聊天内容栏 ⅓、最高 320px、contain 展示；多图全部保留为 84px 缩略格并换行。只改 gallery 装配与图片 CSS，不改 lightbox、语音或生图设置。
- 回归先红：旧实现单图 84px／内容栏 680px，不满足 ⅓；修复后图片查看器与 bubble Chrome 测试三遍通过。可信鼠标／键盘／触屏／reduced-motion lightbox 冒烟通过；1440／800／390px 视口下内容栏 680／448／326px，单图宽 226.66／149.33／108.66px，多图均 84px、窄屏换行；竖图限高 320px。暖本地夹具各 30 次点图至载入加两帧，p95 浅色 33.7ms／深色 34.1ms，不代表真模型或公网性能。
- 1440×900、DPR 2 浅深前后截图：`~/.xbot-build/ui-shots/chat-image-size/{before,after}/{light,dark}/`，包含主列、悬停与打开大图；已本机自查，设计师复审及用户日用待验。
- `gofmt -l .`、`go vet ./...`、diff check 通过；atlas audit 已运行，仍报已有 broken link／stale／unmapped 告警。全仓 `go test -count=1 ./...` 与 web 整包 `-count=3` 未绿：kernel 长轮／记忆日志、memory 月日志、work shell 描述、web CLI 事件／时间分隔／供应商／审批／粘图静态断言／人设／提问。所有失败项在未修改 HEAD 的独立归档逐项复现，未扩修。日志：`/tmp/xbot-chat-image-size-{all,web,baseline,baseline-other,playwright,red}.log`。
- 级别：浏览器展示缝端到端通过；仅本地分支提交，未合 main、未 push。Not run: Linux CI、设计师复审、用户日用。

## 2026-10-31 环境提示 Register 光框架与语义时间（feature/env-prompt-frame，本地分支）

- L 档，落实已落仓设计批 1 + 2。新字段唯一缝为 `envprompt.Env.Register(Field{Key, Sample, Prompt})`；上层 `promptinject.Registry.Register(Source{Name, Order, Render})` 首先挂环境。time 从钟现采，独立尾部语义公告；同分钟不重复，外置 Env 只在成功追加后提交已告知全文，预算预览不提交。压缩/拟合后强制补语义全量快照，计入原 T/H；不改 StablePrefix。Env 生命周期跟 Kernel，重启后重采重公告一次，无新增磁盘账本。
- `sampleEnvironment` 不再采 time，遗留回放忽略历史 JSON time。person/tasks/memory 等保留旧路；公告与写记忆回执共用 `bot.FrameEnvironment`，存储 splitter 不再限定 JSON 载荷。未迁提醒/插话、未加 UI；Accepted ADR 0021 与 Proposed 提议状态均未改。
- `TestRegisteredTimeRequests` 经真注册缝验证同分钟/变分钟、请求无 time JSON、注册额外字段、压缩/拟合恢复。实际 deletion test：临时删 time 的 Register，首请求缺语义公告而失败；恢复后通过。新增真二进制用例也在未改 HEAD 上先红（首请求语义公告数为 0），不是静态文字指纹。Env/Source 注册、预览不提交及语义/JSON 工具回执均有行为测试。
- 新增真实二进制 HTTP/scripted 冒烟 `TestEnvironmentPromptBinary` 连跑三遍通过（2.575s 含构建），覆盖登录、模型传输、跨轮、跨分钟、重启、固定前缀；12 次完整 HTTP 往返 37.7–104.8ms，近邻法 p95=104.8ms，非真模型/公网/IPC或物理性能。长程守护单独通过：573 轮、3 次后台压缩、恢复最差 98.6%、整段 K=0.491、压缩成本 0.424%，前 200 轮 K=1.004；无硬上限越界。
- `gofmt -l .`、`go vet ./...`、diff check、atlas audit 通过（atlas 保留已有告警）。改到四包 `-count=3` 已完整跑完：bot/envprompt/promptinject 绿；kernel 只有原有 LongTurn（19 folds/40 steps）与 MemoryFileLogGuard 失败，未修改 HEAD 归档同样复现。未放宽这两条断言。vision 测试夹具窗口改 64K，避免其 1000-token 夹具因新增公告误入压缩路径，vision 行为断言保留。
- 全仓 `go test -count=1 ./...` 完整跑完但未绿：kernel 上述两条、memory 月日志守门、work description、web 旧时间分隔/供应商/审批/图片/人设/提问断言。旧六条旅程 smoke 全跑失败于 done.text/deltas 原口径，未修改 HEAD 基线六条同样复现；未改守护断言或扩修。日志和回退证据：`~/.xbot-build/env-prompt-frame/`。
- 级别：本功能专项真入口端到端通过，但全仓/旧旅程守门未绿，不能宣称整体验收通过。未合 main、未 push、未移动 guard。Not run: Linux CI、真模型、用户日用、独立复审、IPC/后台减速专项。

## 2026-10-31 聊天送达时间线（feature/chat-timeline，本地分支）

- L 档，按政东确认的选 1：transcript 继续只追加；原 index.sqlite 增加 chat/chat_fts 和世代检查点。主人话/卡/来源行追加时投影，成功 say 待最终助手行与媒体定稿后整批插入，多 say 多行、媒体挂最后一条；delivery_key 幂等且不回写正文/附件。回填分块读取、快照/检查点同事务；旧无 say 正文沿原截止。单一 Go 可见规则同时供回填和非主列 Window 旧预算使用。
- 主列打开、上翻、搜索、轮询、收工与预览改读 chat；40 行默认/200 行硬顶，700 KiB 帽保留，key/游标为 chat ID，transcript_seq 为引用回链。复用 keyed patch、stick、打开遮罩与媒体组件；前端 spokenLines/skipMessage 主筛退出。轨迹独立读 transcript，并用条件请求/结构相等跳过无变重画；peer 原文、recall、expand、导出和模型上下文主责不变。ADR 0039、atlas、API 已同步，dev 留待用户日用/设计师确认，不改 guard。
- 新存储/API 回归覆盖不可变快照、Record 幂等、整轮多 say/媒体、重开、分支独立完成、显式 internal、世代回填、短中文 FTS、分页、鉴权、ETag、单行和聚合体限，以及真实 POST messages→scripted Kernel→chat。Bot 整包最终三遍通过（60.244s）；Chat/stick/live/全部窗口/搜索/增量/语音 Chrome 专项最终三遍通过（112.749s）。真 serve 二进制 `TestTranscriptWindowJourney` 通过，包含浅深尾窗→上翻→chat FTS→around→回最新，并确认主列无 transcript 请求、轨迹仍请求 transcript。替换旧 skipMessage 静态源码测试为 Go 字段行为守护，不修改旧旅程验收。
- 稀疏夹具含600条工具/say记录，尾窗40个可见行仅6037字节；三轮40次本机 HTTP p95 为1.6/2.0/2.0ms，prepend 锚点偏差-3.609px。stick 墙钟20帧样本p95为20.7/22.2/21.3ms，不宣称稳定60fps；原生语音Range每轮25次p95为2.8/2.7/2.9ms。1440×900 DPR2浅深前后截图位于 `~/.xbot-build/ui-shots/chat-timeline/{before,after}/`，已看并对照；视觉组件/样式未改，设计师本人复审待验。
- gofmt、JS语法、go vet、diff检查通过；atlas audit退出0、保留仓库已有告警。全仓一遍、Bot/Web整包三遍均完整执行，但全仓与Web整包未绿：kernel长轮/MemoryFileLogGuard、memory月容量、work description与10项Web时间/供应商/审批/图片/人设/提问断言均在干净基线归档定向复现。Web整包另有CLIEventQueue清理、DevicesPairBrowser与一次VoiceReal的bot playback超时，基线单独三遍未复现这些偶发项，不归因为本改动或声称已修；最终功能专项三遍含真实语音全过。六条旧guard smoke本次和基线均失败（旧done.text/工具对话口径），未扩改断言。初次新trace轮询导致无变重画的问题已在本批修正，并由LiveBubbleSplit守住。
- 级别：本功能本机端到端通过，未合main、未push、未重启日用8788。完整测试/基线日志 `~/.xbot-build/chat-timeline/`。Not run: Linux CI、真实跨设备pipe、真实模型、设计师独立复审、用户日用、物理帧率/IPC/后台减速专项；未委派Agent。

## 2026-10-31 主列 stick-to-bottom（feature/chat-scroll-to-tail，本地分支）

- M 档，按用户合同收拢 `threadStick`：持有 stick 状态与唯一底部阈值、rAF 合帧、ResizeObserver、prepend 视觉锚；打开/latest 贴底，上翻解除，主动下滚到底可重新跟尾。applyTranscript、群渲染、poll、SSE/live、临时发送、历史控件统一调用；异步增高不再用瞬时距离冻结 poll。stick 时尾窗断层直接换新尾，历史阅读仍保位置。未动服务端 Bot.Window 与800 raw保尾。
- 新增墙钟 Chrome + scripted HTTP 回归 `TestChatStickBrowser`，覆盖首次、迟到高度突增、追加/poll、离底观察者不拉回、回真尾、prepend、重新跟尾、临时发送和流式离底；旧 app.js 确实在首次贴底断言失败。沿用原断言，将受 ResizeObserver/rAF 影响、虚拟时间下偶发8px几何误差的 `TestChatStreamBrowser` 改走同一墙钟驱动；不放宽验收。
- 最终专项三遍全部通过：ChatStick、ChatStream、TranscriptWindow各浏览器用例、TranscriptIncremental、LiveBubbleSplit、Voice各浏览器用例（含真实音频）；最终锚点偏差3.007–3.148px，20帧墙钟样本p95为22.4–22.5ms，不宣称稳定60fps。模型/HTTP内容均scripted，媒体与ResizeObserver/rAF为浏览器原生。
- gofmt、JS语法、diff check、vet通过；atlas audit退出0、保留仓库现有告警。全仓一遍与Web整包三遍已完整跑完但未绿：kernel长轮/记忆文件守门、memory月容量、work命令description，以及Web时间分隔/供应商/parked审批/贴图/人设/提问；Web三遍另出现CLIEventQueue临时目录清理和Interjection顺序偶发失败。不将这些失败归因于本改动，也不宣称全仓绿。日志 `~/.xbot-build/chat-stick/`。
- 浅深1440×900 DPR2截图 `~/.xbot-build/ui-shots/chat-stick/{before,after}/`；已看after浅深。级别：本机专项浏览器通过，实际Air/小鲸、设计师复审与用户日用待验。未合main、未push、未重启8788。Not run: Linux CI、实际Air/跨设备/真实模型、物理帧率与IPC/后台减速专项。

## 2026-10-31 忙碌描述同源粘性与切换过渡（feature-busy-desc-sticky，本地分支）

- S 档：沿现有忙碌行接入按 Bot 保存的 last-description，SSE／status 共源；空 summary 不清名、不回「在处理」，新非空描述才替换。送达消息后的新活泡沿用描述，空闲后清理，不串下一轮；未动 stick 滚动、接口、内核或 transcript。
- 侧栏预览与主列标签保留原节点，opacity 淡出后换字再淡入，复用 token：普通 120ms、reduced-motion 100ms。真实 Chrome/scripted 浅深采样两侧同帧换字，约 145／146ms 恢复 opacity=1；未测物理 FPS、IPC 或后台减速。
- BusyLine／Todo 四条 Chrome 测试最终连跑三遍通过（15.197s），覆盖首个空描述、连续空描述与轮询、首行过滤、新描述过渡／节点保留、送达后延续、下一轮清理。新回归在旧实现先红；未修改旧滚动或底边测试。
- 1440×900、DPR 2 浅深前后 default／hover／steps 及 after reduced-motion 截图与逐帧采样：`~/.xbot-build/ui-shots/busy-desc-sticky/`，已看浅深 after，设计师复审和用户日用待验。
- gofmt、JS 语法、diff、go vet 通过；atlas audit 退出 0，保留已有告警。全仓一遍已跑但未绿：kernel 长轮／MemoryFileLogGuard、memory 月容量、work description，以及 Web 旧时间分隔／供应商／审批／图片／人设／提问断言、CLIEventQueue 清理、Interjection 顺序。ChatStream 的回复结束底边断言偶发失败，在干净 HEAD 单跑十遍也复现三次，未扩修。Web 整包三遍首次运行 300s 超时，另一次超过 6 分钟后停止，未取得完整结果；不称全包绿。全量日志在 `~/.xbot-build/busy-desc-sticky/`。
- 级别：本机专项端到端通过，未合 main、未 push。Not run: Linux CI、真实模型／跨设备、设计师复审、用户日用与物理性能专项。

## 2026-10-31 更早消息保尾回归修复（feature/older-keep-tail，本地分支）

- S 档，修设计师对 `6186bb3d` 的 FAIL：前端合并改为保最新尾部、从旧端裁；缓存800 raw，与默认两页各400 raw对齐。满窗后继续上翻无进展时沿原提示行说明用搜索定位，不新增分页。保锚移到加载态/最老页控件更新之后；最老页提示不再定时消失而二次抖动。
- `Bot.Window` 默认尾窗/before仍按25条真实交付扩展，将硬帽调到400 raw/256 KiB；显式 limit仍1–200，HTTP含包络700 KiB上限不变。只改既有读取缝，不改原始历史、投影计数、模型或权限。
- 真实HTTP+Chrome回归改为每页300 raw（250条各600字节隐藏工具记录＋25次成功say），打开25泡→点更早50泡，打开时25个节点全部保留；三轮含控件稳定后的锚点 Δtop=-3.609px（门槛绝对值<4px），响应181,944字节、40次fetch p95分别0.6/1.5/0.8ms。回退旧300行裁切后该测试确实失败；原128KiB/200 raw帽下该夹具甚至无真泡。不是实际小鲸、跨设备或模型性能数据。
- 全部 `TestTranscriptWindow*` 连续三遍通过，最终控件状态同步后又三遍通过；包含800行触顶提示/保尾、失败、切Bot、搜索、reduced-motion和真实HTTP。Bot整包三遍通过（42.646s）；真serve二进制 `TestTranscriptWindowJourney` 冒烟通过。gofmt、vet、JS语法、diff和atlas audit通过（已有atlas告警保留）。
- 全仓一遍、Web整包三遍已跑但未绿：kernel长轮19 folds/40 steps、MemoryFileLogGuard、memory月容量、work description；Web时间分隔/供应商/parked审批/贴图/人设/提问断言失败，Web达120s包超时，与既有里程碑记录的失败相同，本次未重新做无关基线归因。完整日志 `~/.xbot-build/older-keep-tail/`。
- 1440×900 DPR2浅深打开/更早截图 `~/.xbot-build/ui-shots/older-keep-tail/{before,after}/`，before专门回退前端裁切（后端使用扩窗，隔离丢尾回归），已看浅深after。级别：本机专项端到端通过，未合main、未push、未重启8788；实际小鲸软目标与设计师复审/用户日用待验。Not run: Linux CI、真实box/Air与跨设备、真实模型、物理帧率/IPC/后台减速专项。

## 2026-10-31 移除重复回复文案（feature/drop-replying-label，本地分支）

- S 档：只移除发送开始时的「正在回复」状态及其对应清理；主列三点、工具进度、排队和错误逻辑保留。未 push、未合 main。
- 本地 Chrome/scripted 端到端通过：ChatStream、BusyLine、TodoLiveTools 连跑 3 次通过（合计 10.130s）；回归在旧实现上先红。回复状态切换前后主列底边变化 < 0.1px；未测 p95/FPS。
- 浅深色 1440×900、DPR 2 前后截图：`~/.xbot-build/ui-shots/drop-replying-label/`，待设计师/用户日用验收。
- gofmt 与 go vet 通过；全量 go test 运行 120 秒后超时，期间 kernel 的 TestLongTurnStaysUnderTheCeiling、TestMemoryFileLogGuard 及 memory 的 TestMonthlyGuardAndRoundTrip 失败；web 全包 -count=3 运行 300 秒超时，无结果。未扩改无关包。

## 2026-10-31 可见交付尾窗修复（feature/visible-tail-window，本地分支）

- S 档：唯一扩窗装配缝仍为 `Bot.Window`。默认尾窗和未指定 limit 的 before 页先取60条 raw，再按25条可见交付向前扩，保留200条/128 KiB硬帽、连续 seq、真实 has_more_before、原有 fork 回放；显式 limit 与 after/around 原语义保留。HTTP 仅透传未指定 limit，不增加前端自动续拉或另一套计数，不改旧 transcript、Agent 指令或发版。
- Window 回归覆盖尾部 tool/SILENT/timer、双页各25条 say、用户/legacy/旧 notify_owner/图片/语音、失败送达不充数、同轮 say 抑制旧正文、窗外成功回包、条数/字节触顶。删除扩窗循环后，同一测试回到0条 say/60条 raw而失败；恢复后 Bot 整包 `-count=3` 通过（86.273s）。
- 全部 `TestTranscriptWindow*` 三遍通过（162.410s），包括真实 HTTP+Chrome 首包直接25泡、无自动 before，单次上翻另增25泡且保留已有消息节点；既有真实 serve 二进制窗口/搜索冒烟通过（9.686s）。新稀疏夹具110条 raw，响应15,724字节；每轮40次本机浏览器 fetch，三轮 p95 为0.6/1.0/0.9ms。不是跨设备、真实网络或模型性能验收。
- gofmt、go vet、diff 检查通过，atlas audit 退出0。全仓一遍与 Web 整包三遍未绿：kernel 长轮/MemoryFileLogGuard、memory 月容量、work description，以及 Web 的既有断言失败；全仓 kernel/tools/web 与 Web 整包达到120s包超时。上述长轮/记忆/work及8条Web断言在原tip归档复现；CLIEventQueue清理、Interjection顺序、DevicesPairBrowser本次基线单跑未复现，包超时未归因。保留日志，不把专项绿称为全仓绿；基线对照与完整结果在 `~/.xbot-build/visible-tail-window/`。
- 级别：本机 Window/HTTP/Chrome 专项通过，既有真 serve 冒烟通过；未合 main、未 push。Not run: Linux CI、真实 Air/跨设备日用、真模型、物理设备帧率/IPC与后台减速专项。

## 2026-10-31 聊天主列窗口、历史搜索与打开遮罩（feature-transcript-window，本地分支）

- L 档：同一 `GET /api/bots/{name}/transcript` 默认尾部最多60行，before/after排他翻页、around定位命中，返回双向范围/游标/总seq。无参数不再全本；完整原文仍走现有流式export。搜索出口直接复用Bot.Search/FTS5，返回seq、短预览与时间，没有另建全文索引或Bot名字特例。
- `Bot.Window` 按已有lines索引长度/偏移读命中段，原始窗口128KiB预算、HTTP正文含包络留在700KiB内；大单行明确预览并推进连续游标。新增可重建fork_spans/tool_results关系索引保留窗外fork回溯与成功送达投影；旧index首次升级重建、成功后提交版本，可重试，原段字节不变。
- 主列最多缓存300行；chip/上滑prepend保留节点与锚点，搜索浮层开附近窗/高亮、双向续翻与回最新。历史位置不被轮询抢走；退出Bot往来恢复原历史窗口/视口。首屏为半透明遮罩+当前Bot圆头像（默认/上传沿原路径）叠随机闲话，长等换句、轻弹入/晃动/字抖，整包一次置入后320ms散去；失败留遮罩内诚实提示并重试，reduced-motion全静态。侧栏签名只含选择身份，分页加载状态不再触发侧栏重建。
- 专项HTTP/存储、独立Chrome、真实HTTP+Chrome通过；`TestTranscriptWindowJourney` 用真serve二进制/真实登录/FTS/浏览器连续三遍通过（17.848s合计，不调用模型），覆盖chip与真实wheel、命中跳窗/回最新及浅深640/390几何。旧代码同样本默认2,799,808字节，大小回归失败；新代码4460行/2,916,860原始字节默认响应37,834字节（小于716,800），三次40请求暖handler p95为0.846–1.052ms；条件空轮询p95约48–61µs，body为0。这些是本机受控读取数字，不代表公网或模型延迟。
- 1440×900 DPR2浅深截图（等待/失败/尾窗/更早/搜索/命中与窄屏）在 `~/.xbot-build/ui-shots/transcript-window/`；前态由干净56e6fda8资源生成。移除四条依赖旧JS文字/函数切片的过时UI测试，既有浏览器等待条件同步到新遮罩完成；未改守护旅程的步骤或怎样算对。独立最终复核PASS_WITH_SOFT，设计师本人视觉复审、真实跨设备管道及用户日用待验。
- gofmt、JS语法、go vet、diff检查通过；atlas audit退出0，保留已有告警。全仓一遍与web整包三遍未全绿：kernel长轮19 folds/40 steps、MemoryFileLogGuard、memory月容量、work description，以及web时间分隔/供应商/旧审批/提问/人设/贴图测试，在干净HEAD archive定向复现；CLIEventQueue目录清理偶发也在基线三遍复现。全仓另有OAuth固定56121端口占用，基线单跑未复现。旧六条smoke当前与干净基线均失败于原脚本期待done.text/deltas的旧对话口径；未改断言或扩修。日志/基线/回退证据在 `~/.xbot-build/transcript-window/`。
- 级别：本功能本机端到端通过；未合main、未push，未动AGENTS/CLAUDE、changelog、lock与guard。
## 2026-10-31 语音簇 v5（feature-chat-voice-cluster-v5，本地分支）

- S 档，依据用户拍板的 `~/agent-tools/chat-voice-cluster/{handoff.md,ia.md,hifi/v5-*.html}`，只改现有 `appendAudios` 播放态和 voice CSS/tokens；继续经 `appendMessageBubbles` 装配，合文一泡、尾注 `.v-caption`、10 竖条和簇 gap 6 保留。短音波形 56px、play 32px、行内 gap 8px，Chrome 实测短音泡约 158–160px；随时长增长，不齐宽、不画细横轨。麦克风仍为原 36px，transcript／spokenLines／存盘未改。
- 播放中数字为剩余秒数（向上取整），已播竖条按 `floor(currentTime / duration * 10)` 左→右填实；暂停／结束回总时长及静态波形。复用原生 audio 事件，无新增轮询、计时器或动画。
- 新回归在原 v4 先红（短音泡约 198px、缺播放态），新实现语音簇／播放倒数与前后跳转／暂停续播与结束／录音交互／增量刷新浏览器测试连续三遍通过；原生 Chrome MediaRecorder／播放器与真 serve 的 scripted 语音冒烟也各三遍通过。并发检查期间原生浏览器曾一次 bot playback 超时，单独复跑三遍通过；未改测试放宽门槛。
- 1440×900、DPR 2 浅深色前后、hover／playing／paused 和 390px 短长音截图在 `~/.xbot-build/ui-shots/chat-voice-cluster-v5/`，已对照原型自查。每色 30 次播放事件更新到布局 p95 浅 0.4ms／深 0.3ms；投影到两帧绘制 p95 26.1／33.6ms。原生 Range GET 每轮 25 次，三轮 p95 5.0／3.6／6.8ms。均为本机受控样本，非真实供应商／物理设备／60fps 或用户日用验收。
- gofmt、JS 语法、go vet、diff 检查通过；atlas audit 退出 0，保留既有告警。全仓 `-count=1 -timeout=90s` 与 web 整包 `-count=3 -timeout=90s` 未全绿：kernel 长轮 19 folds/40 steps、MemoryFileLogGuard、memory 月容量、work description，以及 web 旧头像断言与 AskOnly 静态测试 panic，均在原 tip `56e6fda8` 干净归档复现。全仓另在 e2e LongRunJourney、tools JobWriterManySecretsIsFast 达到 90 秒包超时，本轮未作基线归因，未扩修。证据与完整日志在 `~/.xbot-build/chat-voice-cluster-v5/`。
- 级别：本地浏览器行为通过，既有语音真入口 scripted 冒烟通过；设计师对生产截图复审、真实供应商／物理麦克风扬声器、Linux CI 和用户日用未验。只提交本分支，未合 main、未 push，未移动 guard。

## 2026-10-31 设置自定义供应商（feature/custom-provider，本地分支）

- L 档，沿 `config.Add / Remove / Resolve`、`providers.json` 文件锁与现有 `SaveEnvKey` / held 装配。Add 允许先存零模型端点；custom/display_name 是原表内元数据，host 匹配内置仍是独立配置和密钥，CLI add 调用及旧协议默认规则保留。host 默认来自既有 Brands/Vendors，显式保存协议；HTTP 灰字警告可存，决定见 ADR 0037。
- 设置网格末 ghost 入口、扁平表单、失焦推断芯片和展开三态、保存后目录/勾选、融合试态 icon、底栏危险删除。主标题人话、通用 glyph、fill-2；模型目录失败可手动登记，并在绑定浮层明确显示已保存模型及目录错误/重试。删除仅清独占 env/held，保留其他条目共享的凭据。未改 OAuth、搜索、生图路径。
- 独立作者的 `internal/web/custom_provider_test.go` 在原 HEAD 归档先红（缺路由/元数据），新实现三遍通过；覆盖真实 API/config 注册后可读可绑、无模型保存、host 同厂独立、编辑删除、共享密钥、URL/key 校验和模型勾选。旧供应商浏览器中的固定卡数断言随新增入口删除，其余行为回归保留。
- `e2e/smoke/custom_provider_test.go` + `internal/web/testdata/custom-provider.cjs` 用真 serve 二进制、实际页面、Chrome、scripted /models 连续三遍通过：从 account 设置入口到注册、拉目录、改协议/地址/名称、测试 icon、绑定 runtime 模型、目录失败手动模型绑定、危险删除。最终复跑另通过既有 `TestThinkingEffortCLI`，验证原 CLI provider add 的实参路径。浅深截图在 `~/.xbot-build/custom-provider/screenshots/`；选中失焦后 fill-2、透明 border、outline none、点击命中，绿点距右 17px。每色 25 次目录 HTTP 的最终复跑 p95 为浅色 1.298ms、深色 0.633ms，三轮最高 3.281ms；不是实际厂商/模型时延或日用性能验收。无新增轮询或动画。
- gofmt、JS 语法、go vet、diff 检查通过；config 整包三遍、web 本批及既有供应商/绑定/OAuth/首启/表面定向三遍通过。已跑全仓一遍和改动包三遍，但 web 整包被旧静态测试 panic 中断；全仓未绿的长轮 19 folds/40 steps、MemoryFileLogGuard、memory 月容量、web 头像/审批静态断言、work description 均在原 HEAD `31594dff` 干净归档复现。旧模拟 DOM 的 constructor 标题断言也在原 HEAD 单独复现；不改这些无关失败。日志在 `~/.xbot-build/custom-provider/checks/`。atlas audit 退出 0，保留既有告警。
- 级别：本条真入口端到端通过（scripted），未合 main、未 push、未调用真实供应商/模型、未用户日用验收。实现设计已吸收进 atlas；未迁入 guard。

## 2026-10-31 聊天气泡纯渲染拆分（feature/bubble-split，本地分支）

- S 档单点渲染修复：`userBubble`、`botBubble`、`optimisticMessage` 共用 `appendMessageBubbles`。每个 audio id、每张图片、每个空行分隔段落各成一泡；单换行同泡，代码围栏和块级公式内部空行保留，引用只挂列首。复用 `.bubble-col` 的 gap，没有改 CSS、transcript／存盘、录音逻辑或 `spokenLines` 归组；大图仍可在同条消息的图片间切换。
- 生产静态资源 + scripted fetch 的本机 Chrome 回归覆盖混合消息、旧 Bot 文本记录、CRLF／连续空行、代码／公式、引用、乐观文本发送；旧代码混合消息用例因只有一个合并泡失败，新代码通过。既有语音／图片查看器／Markdown／数学／增量刷新／live 拆分专项连续三遍通过；图片及正文断言改为检查对应消息范围，继续守原有导航和内容行为。
- Chrome CDP 在真实私有 `optimisticMessage` 入口传入两段语音、两图及三段文字，浅深色均当场为 7 个独立内容泡。1440×900、DPR 2 前后及音频悬停／大图打开／混合乐观态截图在 `~/.xbot-build/ui-shots/bubble-split/`。30 次受控混合乐观渲染到两帧绘制 p95：浅色 36.3 ms、深色 36.9 ms；不含真实网络／模型或日用性能验收。
- gofmt、JS 语法、go vet、diff 检查通过；atlas audit 退出 0，保留既有告警。全仓 `-count=1 -timeout=90s` 与 web 整包 `-count=3 -timeout=90s` 未全绿：内核长轮 19 folds／40 steps、MemoryFileLogGuard、memory 月容量、work description 失败，web 审批测试挂起；五项均在干净 HEAD `05a829ae` archive 定向复现。完整日志在 `~/.xbot-build/bubble-split/checks/`。
- 级别：本地浏览器投影行为通过；真实 serve 全链路、设计师新稿／截图复审、用户日用未验。只在本功能分支本地提交，未合 main、未 push，未移动守护层。

## 2026-10-31 语音分支同步例程技能 main（本地整合）

- 将语音提交 rebase 到本地 main `e58b1834`；Event 同时保留 audios 与 RoutineSkills，忙时输入先装配音频路径再投影例程技能。两侧 API、任务行技能编辑、录音播放器及 atlas 说明均保留，未合 main、未 push、未重启 8788。
- 语音 HTTP／kernel／work、原生 Chrome 录音播放、既有挂图／增量刷新和例程技能后端／API／CLI／Chrome 行为定向连续三遍通过，无跳过。真 serve 的语音与例程技能冒烟各三遍通过；语音完整 scripted 链 95.9–142.6 ms（三次样本），原生浏览器 Range GET 各 25 次的 p95 为 3.0／3.3／3.3 ms，非真实供应商或物理设备性能验收。
- gofmt、vet、JS 语法与 diff 检查通过；atlas audit 退出 0、有告警。全仓 `go test -count=1 -timeout=90s ./...` 未全绿：kernel 长轮 19 folds／40 steps、MemoryFileLogGuard、memory 月容量及 work description 失败，web 审批超时；与已有记录一致，本轮未重做基线对照。整包三遍中 bot／link／provider／review／cmd 通过，kernel 仍有长轮／记忆失败且达到 90 秒总上限（当时运行 ProjectCacheDistrustsRecentChange，未归因），web 审批超时，work 同一 description 失败。完整日志在 `~/.xbot-build/voice-merge/`。真实供应商、物理麦克风／扬声器、Linux CI、旧六旅程 smoke 与用户日用本轮未跑。

- 2026-10-31：语音作为 `audios` 一等附件，沿 images 装配：录音上传到 Bot 工作区 `voice/`，消息／插话／transcript／上下文路径／导出保留音频引用，GET 支持 Range。原生 audio 工具组提供 listen 与只落盘的 tts；挂泡仅由 send_message_to_user 扩 audios，混合附件校验成功才一起入回复队列。Composer 支持点按、持按、取消、Esc、草稿保留；双方手动点播互斥，失败可重试，附件在同泡文字／图片上方，增量刷新保留未变播放器。
- 级别：功能分支端到端通过（真 serve + 本机 scripted 音频／模型），独立视觉复审通过。语音二进制冒烟连续三次通过（本轮 408–459 ms，3 次样本，非真供应商 p95）；专项 HTTP／kernel／work／Chrome 与既有挂图、增量刷新回归连续三次通过。Chrome 另验原生 MediaRecorder、浏览器测试麦克风、原生解码播放，Range GET 25 次 p95 2.9 ms；浅深 mic 可见响应 p95 38.2/37.5 ms。录音态各 180 帧平均 60.09/60.02 fps，帧间隔 p95 22.6/23.3 ms（同机基线 idle p95 22.8/21.7 ms），非物理设备／厂商性能验收。截图：`~/.xbot-build/ui-shots/voice-chat/`；完整日志和文件清单：`~/.xbot-build/voice-chat/`。
- gofmt、vet、diff 检查通过；atlas audit 退出 0，仍有既有告警。全仓与改动包整包三次未全绿：内核长轮 40 步 19 次折叠、记忆文件／月容量、work description 失败均在干净 dbda0b60 复现；web 审批挂起同样在基线复现，最终全量以 90 秒超时退出。整包三次另在 QuestionChoices 用例到达总超时，未归因。旧六旅程冒烟仍有忙时任务／协作门等待及旧 done.text/delta 期望失败，代表性的 WebChat、Remember 在基线复现；新 voice 真二进制冒烟独立通过。未合 main、未 push；真实供应商、物理麦克风／扬声器及用户日用未验，设计保留在 dev，未移动守护层。

## 2026-10-31 例程技能任务面板 UI（feature/routine-skills-ui，本地交付）

- 生产 UI 只在 `routineCard` / `updateRoutineCard` 的原 `.task-routine` 下挂技能摘要、添加入口和逗号编辑；取消／Escape 丢弃，保存仅 PATCH `skills`，失败保留输入。沿用原行头和开关，无新选择器、Webhook 或 CRUD 改动。
- Chrome 使用生产静态资源与受控 fetch：有序引用、添加、空数组、失败重试、取消、两个轮询周期的输入／焦点／选区保持、原开关和行头节点保留均连续三遍通过；例程 skills API、开关 API 与例程运行投影同批三遍通过。新行为用例在旧 UI 上因缺少技能入口失败。级别：本地浏览器行为与 API 检查通过；真实 serve 界面全链路、设计师生产截图复审和用户日用待验，未合 main、未 push。
- 1440×900、DPR 2 浅深色前后及默认／悬停／编辑／错误／添加状态截图在 `~/.xbot-build/ui-shots/routine-skills-ui/`；另有 390px 窄屏。Chrome 实测行头 36px、icon 14px、toggle 34×20px、pad 6×10px、副行 12px，与正文同起点，副行右缘命中按钮；窄屏页面宽 390px、输入与按钮均在视口内。
- 本机无头 Chrome 50 次展开到下一帧 p95 22.7ms；新增后台请求、轮询、动画均为 0，保存每次 1 个 PATCH。此数字是受控浏览器交互采样，不代表真实网络延迟或 60fps 验收。
- gofmt、JS 语法、go vet 和 diff 检查通过；atlas audit 执行，保留既有告警。已跑全仓 `go test -count=1 -timeout=90s ./...` 与 web 整包 `-count=3`，未全绿：内核 `TestLongTurnStaysUnderTheCeiling`（40 步 19 folds）、`TestMemoryFileLogGuard`、memory `TestMonthlyGuardAndRoundTrip`、work `TestShellUsesBotDir` 失败，web `TestWebApprovalAllowAndDeny` 超时；这五项均在干净 `b67fe44d` archive 定向复现，未扩修。检查日志与几何数据在 `~/.xbot-build/routine-skills-ui/checks/`。

## 2026-10-30 例程挂技能（feature/routine-skills，本地分支验收）

- Routine 有序 skills 引用、工具/API/CLI 读写与 Kernel 唯一正文投影已落地；覆盖初次唤醒和忙时吸收，不改调度/precheck，不写技能库。
- 独立真实 CLI→serve ticker→Inbox→scripted 模型冒烟通过，现有 liu-dev-workflow 技能样本 12013 字节，16.61s；默认可移植 fixture 16.73s。顺序、清空、缺失说明、前缀/历史无正文及库不变均有断言。
- 目标回归先红后绿；两份约 14 KiB 正文的本机装配平均 0.224ms、p95 0.321ms（200 次）；不等同于模型回复延迟或 UI 帧率。
- gofmt、go vet、功能涉及四包的目标回归三遍通过；link/cmd 完整包三遍通过。全量及 kernel 完整包三遍遇 TestLongTurnStaysUnderTheCeiling（40 步 19 folds）；全量另有 work.TestShellUsesBotDir 缺 description，web 完整包卡在 TestWebApprovalAllowAndDeny 的连接清理后主动停止。三者均在原始 tip dbda0b60 隔离 archive 复现；未修改既有测试。atlas_audit 执行成功，无新增非 stale 告警；全量 e2e 包通过（61.618s）。
- 级别：CLI→serve→scripted 模型端到端通过；未合 main，真实模型及用户日用未验。UI 定稿与剩余工作只在 [设计](dev/routine-skills.md) 维护。

- 2026-10-30：聊天 transcript 首次加载／轮询／收工对账收敛到 `applyTranscript`，按 seq／送达序号与卡片 id 做 keyed DOM patch，未变旧行、选区与展开态保留；复用尾段 stamp 的弱 ETag，304 无正文且不解码历史，不再 stringify 整份消息。仅主列与 transcript 条件请求，未改侧栏刷新或发送 SSE；有变化时仍计算完整离屏投影。级别：功能分支 Chrome/scripted HTTP 行为通过，未合 main、未 push；真实 serve 与用户日用、独立视觉复审未验。
- 新增接口与 Chrome（有／无 validator）回归旧代码红、新代码连续三次绿，现有 live 分段、transcript 来源／分叉／转义定向三次通过。条件请求 handler 三轮各 100 次，p95 32.833 / 32.958 / 31.125 µs，正文 0 字节；隐藏工具追加的主列 DOM mutation 为 0；未测真实模型延迟／60fps。1440×900、DPR 2 明暗前后截图在 `~/.xbot-build/ui-shots/chat-transcript-incremental/`。gofmt、vet、diff 检查通过，atlas audit 有既有告警。全仓测试遇既有内核长轮 40 步 19 次折叠失败，600 秒内未完成；web 整包三次在审批测试 180 秒超时。浏览器批量三次另遇 `TestChatStreamBrowser` 布局高度偶发差 8px，未修改 HEAD 的重复运行也复现；未扩修这些路径。

- 2026-10-30：记忆 Tab 在 Bot 面板装配缝改为整篇 person / bot `memory.md`、带时间经历及页脚本月流水入口；经历首屏 40 条、更早分页、点开提纲，常驻复用气泡 Markdown。原文 / 经历分页 / 打开文件 / 空数据 / 读取失败的接口与 Chrome 行为定向三次通过，整篇 DTO 回归旧代码失败。真实 `xbot serve` 专用冒烟三次通过；Chrome 1440×900、DPR 2 明暗及 390px 窄屏无横向溢出。未合 main、未 push；独立视觉复审、Box handoff 逐字核对、原生打开文件与用户日用待验收（本机无法直读 Box）。
- 本机面板 HTTP 三轮各 30 次样本 p95 为 0.797 / 0.775 / 0.771 ms，payload 8,318 字节；沿用现有面板请求，新增轮询 / 动画为 0，更早分页每次 1 个请求；真模型响应及帧率未测。gofmt、vet、diff 检查通过，atlas audit 有既有告警。全仓测试复现内核 `TestLongTurnStaysUnderTheCeiling`（40 步 19 次折叠）、work `TestShellUsesBotDir` 缺 description、web `TestWebApprovalAllowAndDeny` 在 90 秒超时；web 整包三次同样被审批超时阻断。旧六条旅程冒烟均未通过模型回复阶段；干净 HEAD 的 `TestRemember`（空 done.text）与 `TestWebChat`（0 个非空 delta）同样失败，本批未改模型或工具链路。

- 2026-10-30：OpenCode Go 经 `config.Vendors` 成为内置密钥供应商（Chat Completions、Go base、`OPENCODE_API_KEY`），设置页复用品牌卡和密钥详情，引导订阅后从 Console 复制 API key；无 OAuth、Zen 按量、Messages 第二品牌或配额 UI。登记缺失时回归红，保存 key → 实时 models → 同步 → 绑定 → scripted Completions 通过；相关 config/web 和本机 Chrome 行为连续三次通过，浅深色截图在 `~/.xbot-build/ui-shots/opencode-go/`。级别：功能分支 HTTP/scripted 与浏览器检查通过，未验真实 serve 全旅程、独立视觉评审或用户日用；未合 main、未 push。新增后台请求、轮询、动画均为 0，交互 p95/帧率未测。
- 本批 gofmt、vet 通过；全仓与改动包整包三次未全绿：内核长轮失败、网页审批挂起，跳过后另遇旧 UI/park/question/work 用例失败或超时。干净 HEAD 复现长轮、审批挂起、work description 及 avatar/message grouping/provider catalog/persona UI 失败；不扩修本批外路径。

- 2026-10-30：E2-final 非 maskable 图标的面板外画布改为透明，覆盖 web PNG、SVG 内嵌 PNG、appshell、ICNS 和 ICO 全部尺寸；maskable 字节不变。以既有 1024px 图像抠图，保留面板内 RGB；透明度回归旧资源失败、新资源连续三次通过，appshell 整包三次通过，Chrome 浅深色资源截图与 alpha 检查通过。尚未合入、原生 Dock/安装包和用户日用未验收；新增请求、轮询、动画均为 0，512px PNG 增加 3,885 字节，交互 p95/帧率未测。
- gofmt、vet、diff 检查通过，atlas audit 有既有告警；全仓测试复现内核 `TestLongTurnStaysUnderTheCeiling`（40 步 19 次折叠）、work `TestShellUsesBotDir` 缺 description，以及 web `TestWebApprovalAllowAndDeny` 在 90 秒超时；网页整包三次也被同一测试的 90 秒超时阻断。
- 2026-10-30：生图统一为关 / 自动 hosted 优先、代理兜底 / 总用设置里的家（只代理）；本地 `generate_image` 经统一注册表与 `tools` 门调用全局 OpenAI/xAI OAuth，复用 Responses、generated/ 及既有挂图。设置增加「怎么画」，详情能力只读。功能分支 scripted 真实 serve 端到端三次通过，六次代理完整旅程 86–103 ms（样本 p95 103 ms，非真厂商性能）；本机 Chrome 浅深色截图与行为通过，未获独立视觉复审或用户日用验收；未合 main、未 push。
- 本批 gofmt、vet、atlas audit（既有告警）通过；config/provider/review 包三次及 kernel 跳过既有长轮失败后三次通过，web 生图与供应商浏览器相关测试三次通过。旧绑定/所选家相等门控恢复后回归红，修复绿。全仓检查 600 秒未完成：kernel 长轮 40 步 19 次折叠失败，web 审批连接挂起；两项在未修改 HEAD 复现。web 跳过审批后重跑仍遇人设卡锁用例挂起，未继续扩修。移除挡住供应商页测试的既有选中底色静态值断言，不改成新色值断言。

- 2026-10-30：原生能力统一由 `link.Organs()` 驱动 CLI 与内核装配，旧原生 grant 忽略；真实 Link 仍可装卸。编码任务随 Bot 改名迁移记录、卡片和完成事件；折叠后的请求去掉被新快照取代的旧工具索引，保留 transcript 与其他环境字段。改动包连续三次通过（内核跳过既有失败的长轮测试，网页只跑相关 API），编码任务改名通过 race 检查；真实入口和用户日用尚未验收。长轮折叠测试既有基线为 40 步 12 次，本次为 19 次，均超过测试门槛；交互 p95 未测。全仓检查另遇已知网页包超时、`TestShellUsesBotDir` 失败，以及 OAuth 测试报告本机 56121 端口被占用。

- 2026-10-29：`restart_xbot` 的 Restart 类审查对所有事件直接放行，无确认卡，复用原进程重启和续跑链；Timer、webhook 和两种权限卡状态的脚本化内核回归旧代码失败、新代码通过。尚未进行真实进程重启或用户日用验收；新增网络请求、轮询和动画均为 0，交互 p95 未测。
- 重启专项与续跑专项各连跑三次、gofmt、vet、atlas audit（有既有告警）通过。全仓内核 `TestLongTurnStaysUnderTheCeiling` 失败，干净基线同样 40 步 12 次折叠；网页 httptest 连接未退出，约 90 秒中断，内核整包三次也未完成。

- 2026-10-29：桌面壳为裸二进制启动设置内嵌品牌图标，macOS 用 Dock 图标，其他平台用窗口图标；允许调用方覆盖，失败写日志。默认 PNG 和覆盖选项的回归在旧代码失败、修复后 appshell 包三次通过。尚未做原生窗口目视或用户日用验收；新增网络请求、轮询和动画均为 0，交互 p95 和帧率未测。
- 提交前 gofmt、vet、atlas audit 通过（audit 有既有告警）；全仓测试失败：kernel `TestLongTurnStaysUnderTheCeiling`、work `TestShellUsesBotDir` 失败，web `TestWebApprovalAllowAndDeny` 在 90 秒超时。此前里程碑已记录这些失败，本次未复测基线。

- 2026-10-29：CLI Event 新写入的 transcript 行标为主人可见，保持不可信包裹，修复来源行入口；事件入口及 transcript API 回归旧代码失败、定向连跑三次通过。全仓测试中内核折叠测试失败、OAuth 测试端口占用，网页包未结束时中断；改动包全量三次也未完成。真实入口和主人日用尚未验收；新增请求、轮询和动画均为 0，交互时延与帧率未测。

- 2026-10-29：CLI Event 接入通用可点击来源行；Bot 往来保留既有入口。浏览器行为回归覆盖分组、展开和来源隔离；真实入口与日用验收待做。

## 2026-10-29 隐藏聊天中的后台工具完工（功能分支）

- 聊天 transcript 投影跳过 `shell` 来源完工事件；事件仍保留。无头 Chrome 回归旧代码失败、修复后通过，覆盖主人消息、工具送达消息和 webhook 对照；尚未用户日用验收。新增请求、轮询、动画均为 0；帧率与交互 p95 未测。
- 定向测试连续三次、gofmt、vet、atlas audit（有既有告警）通过。全仓测试复现内核 `TestLongTurnStaysUnderTheCeiling` 失败，另有 `internal/work` 的 `TestShellUsesBotDir` 缺 description 失败；网页测试 HTTP 连接未结束，约 70 秒中断，网页包三次及全仓均未完成。

## 2026-10-29 CLI 消息通道（功能分支）

- `xbot message` 经本机 owner 会话和现有 messages SSE 给运行中的 Bot 发消息；脚本化 HTTP 测试覆盖命令分发、正文与失败路径。未合 main，尚待真实入口及主人日用验收。真实二进制对假 HTTP 服务单次 632 ms；新增每次调用 2 次本机 HTTP 请求，交互 p95 尚未测。命令包三次、go vet、gofmt 通过；全仓测试遇既有内核 TestLongTurnStaysUnderTheCeiling 失败，网页包未结束时中止，全仓未完成。

## 2026-10-28 统一工具注册中心与代理门（feature/tool-registry-proxy）

- 一张工具注册表（`internal/kernel/registry.go`，`kernelTools` 声明表替掉 `namedLink` 拼装）。主线 `tools[]` 只声明代理门 `tools`、send、todo、壳、文件和 `web_fetch`，其余经 `tools` 的 search/describe/call 调用（抄 Pi 的 mcp 代理，不抄 deferred）。`expand_tool_group` 删除。环境流加 `tool_index`。见 [ADR 0035](adr/0035-unified-tool-registry.md)。
- 原生和 Links 分开：电脑操作是 organ，开机即挂；浏览器回到商店（带探测）；fetch 标 `retired`，不进目录、不挂载、不算已连接。
- 性能：同一 Bot 声明的工具从 38 个、22.8 KB（约 5.5k token）降到 10 个、9.2 KB（约 2.25k token），能调用 46 个；`tool_index` 668 字节。一轮里多次 describe/call 之后，`tools[]` 按字节比不变。新增请求、轮询、动画都是 0。
- 级别：端到端通过（假模型，scripted）；未合 main，未 push，没有用户日用，没有真模型认证。

## 2026-10-28 商店目录真源去掉原生器官（功能分支）

- `StoreApps()` 不再列电脑操作、浏览器（改为 `link.LookupOrgan`，提示词「连接器」与 `links load` 按标题照旧认）；`fetch` 目录行 `hidden`，`/api/apps` 不再出现「网页抓取」；前端 `apps.js` 的 `NATIVE` 滤掉删除。商店 POST `desktop` 返回 404。

## 2026-10-28 设置页扁平面举一反三（功能分支）

- 通用、权限、关于、技能、人设卡、已连接清单、设备本机行、图片生成开关与引导去掉 `--x-fill-1` 灰壳，清单去分隔线；关于头区不再复读短号；供应商卡绿点/「登录」贴右缘；连接应用去「本机」芯片，电脑操作、浏览器、网页抓取不进商店、推荐、已连接和计数。表面回归在旧样式上失败、修后通过；无头 Chrome 截图核对通用、关于、设备、技能、图片生成、连接应用；尚未在真实设置入口目视或用户日用。新增请求、轮询和动画均为 0；帧率和交互 p95 未测。

## 2026-10-28 网页搜索去灰垫（功能分支）

- 网页搜索开关、未选行与列表容器透明，选中及展开密钥区保留 fill-2；其他设置页不变。明暗主题 Chrome 样式回归旧样式失败、修复后连续三次通过；尚未目视验收真实设置入口或用户日用。
- gofmt、go vet、atlas audit 通过（audit 有既有告警）。全仓测试出现已有的 `TestLongTurnStaysUnderTheCeiling` 失败；全仓和 web 全包三次测试因 web 长时间未结束而中断，均未跑完。新增请求、轮询和动画均为 0；帧率和交互 p95 未测。

## 2026-10-28 供应商网格透明回修（功能分支）

- 供应商及订阅网格、换绑和网页搜索列表容器不铺整块底色；选中仍为 fill-2，未选供应商卡保留 fill-1。明暗主题 Chrome 渲染回归连续三次通过，旧 tip 样式回退时失败；尚未在真实设置入口目视验收，也未日用验收。
- gofmt、go vet、atlas audit 通过（audit 有既有告警）。全仓测试复现 `TestLongTurnStaysUnderTheCeiling` 失败；web 全包三次测试在约 100 秒时仍未结束，两项中断。新增请求、轮询和动画均为 0；帧率和交互 p95 未测。

## 2026-10-28 供应商卡选中底回修（功能分支）

- 供应商及订阅卡选中用 fill-2，网格缝与未选卡同为 fill-1；换绑行和网页搜索选项使用较浅选中底。Chrome 在现有本机设置入口加载本 worktree 样式，浅深主题点选返回并失焦后查看邻卡竖边，未见描边感；键盘焦点环保留。这是脚本化自检，尚未用户日用验收。
- 新增渲染回归旧代码失败、新代码连续三次通过；gofmt、vet、atlas audit 通过（audit 有既有告警）。全仓测试出现既有 `TestLongTurnStaysUnderTheCeiling` 失败，web 长时间未结束；全仓和 web 全包三次运行约 100 秒后中断，未完成。浅色选中底与缝 RGB 差之和为 23，深色为 25；新增请求、轮询和动画为 0，帧率与交互 p95 未测。

## 2026-10-28 供应商选中卡只留底色（功能分支）

- 选中卡保留中性 `--x-fill-3` 底，边框与未选卡相同；焦点轮廓保留。Chrome 设置入口脚本覆盖明暗主题，回归在旧样式失败、修复后连续三次通过，并查看了脚本化截图。gofmt、vet 和 atlas audit 通过（audit 有既有告警）。
- 全仓测试中 kernel 的 `TestLongTurnStaysUnderTheCeiling` 失败（40 步折叠 12 次）；全仓和 web 全包三次运行因 web 测试长时间未结束而中断，未完成。新增请求、轮询和动画为 0；未测帧率或交互 p95，未做用户日用验收，未合 main、未 push。

## 2026-10-28 私有资料与共享工作空间（功能分支）

- 私有资料继续使用 `bots/<bot>/memory/`；整机共享工作空间保留 `<root>/memory/`，采样时创建并始终注入契约与索引，空库也给路径。每轮轻推列出两条绝对路径，默认写私有、跨 Bot 才写共享；沿用环境流与 sidecar，关键词和预算覆盖两源。见 [ADR 0033](adr/0033-bot-and-shared-reference-workspace.md)。
- 空库、跨 Bot 写读、关键词、去重、预算与压缩定向 kernel 测试三次通过，分叉/重试/跨 Bot 写入 race 通过；`TestSpawnWhileChatKeepsAnswering` 三次通过，手册和命令包三次通过；gofmt、vet、atlas audit 通过（既有告警）。全仓测试 e2e 包通过；kernel 长轮、work shell 失败和 web 审批超时均在上一 tip 分别复现。kernel 全包三次运行在 90 秒总超时前两次遇到同一长轮失败，第三次未完成；全仓运行后仅调整了私有契约措辞，定向与入口测试重新通过。
- 20 个深层文件关键词扫描微基准约 0.20 ms/次；未测交互 p95、真实模型或主人日用，未合 main、未 push。

## 2026-10-28 资料默认归 Bot 回修（功能分支，之后共享策略由 ADR 0033 更新）

- 按政东新决定，资料默认使用 `bots/<bot>/memory/`，可选 `<root>/memory/` 保存跨 Bot 事实；Bot 库跳过系统记忆文件、日志、索引和备份。两源沿用环境流及提醒 sidecar，按来源独立预算，旧 extract 不恢复。决定见 [ADR 0032](adr/0032-bot-reference-and-optional-shared.md)。
- 定向 kernel 测试连续三次通过，覆盖私有隔离、共享可见、两源关键词、每轮轻推、系统条目排除、独立预算、压缩和分叉；提醒相关 race 测试通过。`TestSpawnWhileChatKeepsAnswering` 连续三次通过，手册与命令包三次通过；gofmt、vet、atlas audit 通过（既有告警）。全仓 e2e 包通过，kernel 长轮折叠失败、work shell 描述失败、web 审批测试 90 秒超时，三项在旧 tip 分别复现；kernel 全包三次运行受总超时影响未完成，前两次均遇到同一长轮失败。
- 20 个深层文件的单源关键词扫描微基准约 0.20 ms/次；交互 p95、真实模型和主人日用未测，未合 main、未 push。

## 2026-10-28 经历与资料收口（功能分支）

- 资料库增加关键词路径提醒和每轮写入轻推，通过提醒 sidecar 绑定并回放；原文不写入提醒，根层已有索引不重复提示。旧压缩抽事实代码及接线退役；资料索引超 256 KiB 整源排除并说明。
- 脚本化入口 `TestSpawnWhileChatKeepsAnswering` 连续三次通过，原 tip 在 `pong` 超时；脚本模型现能识别附有既有 reply-first 提醒的输入，旧 tip 仅带此脚本修正也通过。关键词、写入轻推、预算、压缩、分叉、溢出重试及稳定前缀等定向 kernel 测试三次通过，相关 race 检查通过。全仓测试 e2e 包通过；kernel 长轮折叠失败、work shell 描述失败及 web 确认测试超时均在原 tip 单独复现。kernel 全包三次运行在 90 秒总超时前两次出现同一长轮失败，未完成第三次。gofmt、vet、atlas audit 通过（audit 有既有告警）。
- 20 个文件的关键词扫描微基准约 0.41 ms/次；未测交互 p95、真实模型及主人日用，未合 main、未 push。
## 2026-10-28 日巡检界面修复

- 供应商卡选中皮与焦点改中性，主标题用供应商展示名；技能 YAML 块说明正确解析，缺失或无效说明不展示符号和 slug。相关浏览器与 skill 回归见本批测试；未合 main，尚待日用验收。

## 2026-10-27 对话顶悬浮卡紧凑回修（功能分支）

- 复审 P1 回修：底部状态行预留高度，发送和回复结束时不再推动消息列；浏览器回归覆盖两次切换，断言在修复前失败，修复后连续 3 次通过，线程底边变化均小于 0.1px。gofmt、vet 和 atlas audit 通过（audit 有既有告警）；全仓及 web 全包三次检查再次在 `TestWebApprovalAllowAndDeny` 超时，未完成三次；全仓还报 e2e、kernel、work 既有失败，以及 OAuth 固定端口占用（并行检查时发生）。未测帧率或交互 p95。
- 各断点身份卡只显示 28px 头像和名字，状态及 Bot 往来提示移到对话底部；消息顶垫和会遮挡卡的抽屉回到顶栏下沿。Chrome 脚本在 390–1440px 检查尺寸、命中及开合，另检查回复状态；相关测试三次通过，新增回归在旧代码失败，1200px 脚本截图已查看。gofmt、vet 通过；atlas audit 退出 0，有既有告警。
- 全仓测试报 e2e `TestSpawnWhileChatKeepsAnswering` 等待超时、kernel `TestLongTurnStaysUnderTheCeiling` 和 work `TestShellUsesBotDir` 失败；全仓和 web 全包三次均在 `TestWebApprovalAllowAndDeny` 以 180 秒超时退出，未做基线对照。新增请求、轮询和动画均为 0；未测帧率或交互 p95，未做用户日用验收，未合 main、未 push。

## 2026-10-27 技能收成提议（功能分支）

空库也注入提议与确认后写入的模型规则，沿用现有写工具和 Normal 权限；脚本化内核对话、Dispatch、skill、review、agenttype 定向测试和 vet 通过，atlas audit 退出 0（有既有告警）。未测真模型、主人日用、交互 p95；新增网络请求和后台任务均为 0。未合 main、未 push。

## 2026-10-27 任务记录折叠同头（功能分支）

- 已结束的运行记录开合共用紧凑行头，正文在下方显隐；标题节点在运行和结束状态间复用，例程配置行未改。
- Chrome 脚本从任务 Tab 验证开合，另验证标题节点、轮询及运行转完成；相关回归连续三次通过，旧代码的同头回归失败。gofmt、vet 通过；atlas audit 退出 0，报告既有告警。全仓单次出现 e2e 等待超时及 kernel 长轮折叠失败，web 尚未结束时中止；web 全包三次也未结束时中止，均未做基线对照。新增请求、轮询、动画均为 0；未测帧率或交互 p95，未做主人日用验收，未合 main、未 push。

## 2026-10-27 主列忙态单行（功能分支）

- 活泡复用一行三点胶囊和 description；状态及 SSE 的文案来自工具参数 description 首行，确认摘要仍在步骤中。侧栏运行预览同规则；厂商搜索流式提示不再另占一行。
- 新浏览器回归旧代码先失败，所选定向 agent/kernel/web 测试连续三次通过，但未覆盖 TestReplyToRoundTrip；该测试在复审时确认因本提交回归失败；本机 Chrome 脚本化截图核对同行。gofmt、vet、atlas audit 通过（audit 有既有告警）。全仓单次失败：e2e 聊天测试（复审确认是送达正文回归）、kernel 长轮折叠、work shell 描述，以及 web 审批测试 180 秒超时；受影响包三次中 agent 通过，kernel 长轮折叠失败，web 同一审批测试超时，未完成三次。未做基线对照；并行有其他工作区测试。未测帧率、交互 p95 或主人日用，未合 main、未 push。

- 复审回修：恢复送达消息记账，`result.text` 与 `done.text` 带正文，忙态仍只显示 description；删除无调用的 progress 和闲置参数。新 result 断言在旧 tip 失败，修复后 `TestReplyToRoundTrip`、忙态与活泡浏览器回归各连续三次通过，kernel 定向测试三次通过；gofmt、全仓 vet 通过，atlas audit 有既有告警。全仓单次失败：e2e `TestSpawnWhileChatKeepsAnswering` 超时、kernel `TestLongTurnStaysUnderTheCeiling`、work `TestShellUsesBotDir`，web `TestWebApprovalAllowAndDeny` 在 240 秒包超时；kernel/web 全包三次同样分别因长轮失败和审批超时未通过（长轮复审已在基线复现）。未测交互 p95、帧率和主人日用；未合 main、未 push。

## 2026-10-27 收件箱定时摘要修复（功能分支）

定时 routine 摘要只显示标签内正文，去掉后续唤醒套话；接口回归在旧代码失败、修复后专项通过。级别：本分支，尚未端到端验收；未测性能数字。

## 2026-10-27 收件箱来源和摘要修复（功能分支）

- Webhook、文件监听按真实来源前缀显示；摘要提取首个 Webhook delivery 正文、去掉 watch 外壳。真实格式接口及 Chrome 脚本回归在旧代码失败、修复后专项连续三次通过。
- gofmt、vet 通过；atlas audit 退出 0，存在既有告警。全仓测试失败于 e2e 等待、kernel 长轮折叠、work shell 描述及 web 审批 180 秒超时；web 全包三次在同一审批测试超时，未完成三次，与复审记录现象一致，未重新核验基线。未测真实入口端到端、用户日用、交互 p95 或帧率；新增请求、轮询和动画均为 0。未合 main、未 push。

## 2026-10-27 技能删除确认计数（功能分支）

- 删除确认的 N 统计本数据根所有 Bot，开关列表仍限当前用户可管理的 Bot；对齐设计的文案和危险按钮已存在。回归先在旧代码失败，定向 API、界面与 Chrome 脚本三次通过；gofmt、vet 通过，atlas audit 有既有告警。
- 全仓测试失败：e2e 等待超时、kernel 长轮折叠、work shell 描述、OAuth 回调端口占用，web 审批测试三分钟超时；web 全包三次亦在同一审批测试超时，未完成三次。未测交互 p95、帧率或用户日用，未合 main、未 push。

## 2026-10-27 网页搜索标题说明（功能分支）

- 「本机用哪家」标题旁的 ⓘ 复用全局悬停和聚焦提示，收起原先两段说明。
- 指定网页搜索测试单次和三次、Chrome 脚本悬停与聚焦、gofmt、vet 通过；atlas audit 有既有告警。全仓测试失败：e2e 等待超时、kernel 折叠次数超限、work shell 描述缺失，web 的审批测试 180 秒超时；web 全包三次也在同一审批测试超时，未完成三次。未做旧版本对照，未测交互 p95 和帧率，未验收用户日用。

## 2026-10-27 连接应用商店卡密度（功能分支）

- 商店卡标盒 48px、图形 30px，替换品牌形标；同行等高、说明最多两行，保留密钥范围说明与单格末卡。Chrome 回归旧代码失败、新代码与商店定向测试三次通过；gofmt、vet 通过，atlas audit 有既有告警。全仓测试早一轮报 e2e 等待超时和 kernel 长回合失败，web 包久未结束；最终版本全仓和 web 全包三次均中止，未完成。未测交互 p95 或帧率，未合 main、未 push，用户日用待验收。
## 2026-10-27 人设卡工具条单行（功能分支）

- 有卡时「全部」与搜索、新建、导入共用工具条；空库和筛选行为保持不变。当前布局见 [ui](atlas/ui.md)。
- 浏览器回归旧版失败、新版连续三次通过；1440×900 截图确认单行，搜索、分段和空库用脚本化数据验证。gofmt、vet 和 atlas audit 通过（audit 有既有告警）；全仓及 web 全包三次因并行资源竞争按要求中止，全仓中止前 e2e 和 kernel 有失败，未做基线对照。新增请求、轮询、动画均为 0；帧率与交互 p95 未测。未合 main、未 push。

## 2026-10-27 碎嘴群友本机主人残留升级（功能分支）

- 本机「碎嘴群友」比 `legacy-owner/1.txt` 多一段【跟人交流】，不逐字相等而被跳过；`legacyOwnerBodies` 改为一名多快照，新增 `legacy-owner/1-talk.txt`，仍走库锁内精确匹配，1.txt 照旧匹配，改过的卡不覆盖。
- 回归在旧逻辑失败；人设卡包三次、gofmt、vet 通过；全仓单次仍有既有失败（web 600 秒超时、work `TestShellUsesBotDir`，与人设卡无关）。对本机卡库的拷贝跑 EnsureSeeds：碎嘴群友逐字等于现行用户版，四张均无「主人」；live 库未动。note-capture「当主人说」本批未改。未合 main、未 push、未操作 8788。

## 2026-10-27 人设卡旧正文升级（功能分支）

- 复用库锁内的种子升级，四张未改过的旧「主人」完整版与原有三张薄稿按读盘正文精确匹配后升级；改过的卡不覆盖。
- 回归在旧逻辑失败；人设卡包三次、gofmt、vet、atlas audit 通过（audit 有既有告警）。全仓测试失败：e2e 聊天超时、kernel 长回合折叠次数、web 确认测试 3 分钟超时、work shell 描述缺失；此前里程碑也记录了这些问题，本次未重做基线对照。未测交互 p95、用户日用或 live 库，未合 main、未 push、未操作 8788。

## 2026-10-27 xAI 账号与密钥分离（功能分支）

- 订阅 xAI 和 API 密钥分别绑定；账号登录期间可创建或复用让名后的密钥供应商，退出账号保留密钥。订阅详情和密钥详情均可保存密钥。
- HTTP 回归在旧代码上复现 409；最终代码下密钥保存和脚本化 Chrome 回归连续三次通过。gofmt、vet 通过；全仓单次和 web 全包三次运行约两分钟仍未结束，已中止，不能判全绿。atlas audit 有既有告警。未测真实 OAuth、主人日用、交互 p95 或帧率；未合 main、未 push、未操作 8788。

## 2026-10-27 关于页版本点串复制（功能分支）

- 版本行左标签、右贴版本串；点击串复制完整展示并短显「已复制」，头区短号只展示。复用已有复制能力和配对码式行内反馈。
- 本机 Chrome 脚本回归连续三次通过，旧代码先失败；截图自查。gofmt、vet 通过；全仓测试报 e2e `TestSpawnWhileChatKeepsAnswering` 超时、kernel `TestLongTurnStaysUnderTheCeiling` 失败，web 全包久未结束后中止，全仓亦未完整结束；atlas audit 有既有告警。未测交互 p95 和帧率；未合 main、未 push，用户日用待验收。

## 2026-10-27 本机可选账号设备页复审修复（功能分支）

- 已登录设备页通过同源账号接口单独展示账号关联设备，保留配对列表和本机认领；未登录不拉账号目录。浏览器脚本回归以账号服务替身覆盖登录、展示、认领和退出。
- 账号、会话、配对定向测试连续三次通过；回归浏览器测试在旧 tip 失败、新代码通过。全量测试中 e2e 聊天超时、kernel 长轮失败，等待 web 包后中断；web 包三次运行在审批测试超时。gofmt、vet 通过。尚未合入、未做真实多设备端到端及用户日用验收；未测交互 p95 或帧率。每次已登录打开设备页增加一次同源账号设备请求。

## 2026-10-27 侧栏头像菜单收敛

- 按定稿保留头像开关菜单与唯一「设置」行；设备、关于仍在设置里，侧栏拖拽仍可切换窄条。详见 [界面](atlas/ui.md)。
- 本机 Chrome 脚本回归覆盖展开侧栏、窄条、设置和设备/关于；浅色和深色截图已自查，设计师复审及用户日用尚待验收。

## 2026-10-27 更换模型能力图标（功能分支）

- 浮层行保留横铺，在右侧用看图、搜索 outline 图标和短数字窗口替代能力句；语义通过 title / 无障碍名称提供。
- 本机 Chrome 脚本连续三次覆盖手机、桌面及桌面壳尺寸的能力语义和布局；未进行用户日用验收。新增网络请求和轮询为 0；未测帧率或交互 p95。全量测试已报 e2e 并发聊天超时、kernel 长轮折叠次数失败；全量及 web 包三次测试在 web 包等待超过三分钟后中断，未完成。

## 2026-10-27 xAI 旧密钥占用态修复（功能分支）

- xAI OAuth 绑定占用同名条目时，旧密钥详情改为说明先退出登录才能修改密钥，隐藏不能保存的表单；订阅入口、跳转及 OAuth 详情未改。
- 脚本化 Chrome 相关回归单次及三次通过，回归在旧代码上失败；gofmt、vet 通过。全仓检查在 e2e `TestSpawnWhileChatKeepsAnswering` 超时、kernel `TestLongTurnStaysUnderTheCeiling` 失败后仍未结束；web 全包三次亦未结束，两者约两分钟时中止，不能判全绿。atlas audit 有既有告警。未测真实 OAuth、用户日用或性能 p95/帧率；未合 main、未 push、未操作 8788。

## 2026-10-27 xAI 订阅入口（功能分支）

- 订阅区并列 ChatGPT 与 xAI；xAI 详情未登录引导回订阅，图片生成 Grok 未登录跳回 xAI 卡，复用既有 OAuth 与密钥绑定。账号占用同名条目时保留密钥折叠说明，不提供会被后端拒绝的密钥写入。
- Chrome 脚本化界面与相关测试 `-count=3` 通过；gofmt、vet、atlas audit 已跑。全仓单次与 web 全包三次未通过：e2e `TestSpawnWhileChatKeepsAnswering`、web `TestWebApprovalAllowAndDeny` 超时，work `TestShellUsesBotDir` 失败；三条在 `f4a54cd` 归档复现。未测真实 OAuth、生产性能或用户日用；未合 main、未 push、未启动 8788。

## 2026-10-27 provider 请求原文 debug

- 三种协议共用请求 body 落盘函数；热开关、路径与原文约定见 [供应商](atlas/providers.md)。按用户纠正，含原始密钥、未 redact，仅 body，不额外采集 header。实现与测试仅改 provider；不改权限、vault、UI 或 transcript。
- 复审 PASS（Pi prd-rev，P0/P1/P2=0）。功能 tip `1c94e90` 已合本地 main；`docs/dev/provider-request-debug.md` 已毕业删除。未 push；禁 sync-app；纯工程无 UI，不喊设计师。

## 2026-10-27 连接器 Agent 侧可用

- 沿现有 `links load` 补全名称/id 解析与商店类摘要筛选；MCP 失败回传阶段与有界脱敏 stderr；`/api/apps` 提供按 Bot runtime，读取不拉进程；前端未改。见 [生态](atlas/ecosystem.md)。
- 复审 PASS（Pi pwm-rev，P0/P1=0，P2=4 不阻塞）。功能 tip `5c04f8e`（rebase 自 `de23bd0`）已合本地 main；`docs/dev/playwright-mcp-tools.md` 已毕业删除。未 push；禁 sync-app；前端未改，待设计师逛后再 →已完成。

## 2026-10-27 云上账号壳切开

- 账号仓 tip `26a0067`：云静态只留登录 / 注册 / 查收邮件；删 `#devices` / `#gateway`；有会话一律 `/app/`；无设备/离线用设备侧 boot。见 [界面](atlas/ui.md#公网登录后的设备壳)。
- 复审 PASS（Pi cas-rev，P0/P1/P2=0）。`docs/dev/cloud-auth-split.md` 已毕业删除。未 push；禁 sync-app；登录/注册壳待设计师逛后再 →已完成。

## 2026-10-27 生图挂气泡重开（Bot 主动 notify_owner）

- 上一版内核 Save 自动 `attachOutImages`（`b389806`）作废。Save 只落盘 `generated/`；交付改由 Bot 调既有 `notify_owner` images 挂进本轮回复。引导禁「自动挂」话术。见 [内核](atlas/kernel.md#厂商生图)、[供应商](atlas/providers.md)、[界面](atlas/ui.md#图片生成设置)。
- 复审 PASS（Pi，无 P0/P1；两则 P2 备案）。功能 tip `07d6592` + atlas `b64ff8e`；已合本地 main。`docs/dev/imggen-attach-bubble.md` 已毕业删除。未 push；**未**跑 sync-app-from-xbot（ADR 0029）。设置脚注文案有改，待设计师逛后再收口已完成。

## 2026-10-27 生图同轮挂回复

- hosted 生图保存后接入既有挂图通路；普通工具轨迹展示、成功/部分失败的真实回执和轮次归属见 [内核](atlas/kernel.md#厂商生图)、[供应商](atlas/providers.md)、[界面](atlas/ui.md#图片生成设置)。按用户纠正移除独立生图步骤块，保留原 gallery 与可选工作区菜单。
- kernel 全包单次通过（41.609 秒），provider 全包通过；最终图片与长轮定向、定向 race、本机 Chrome 五项行为回归通过。ChatGPT/xAI scripted 冒烟跑真实 serve、HTTP 设置、OAuth 传输、保存与图片下载；不调真模型。新 kernel/UI 回归在 `77c0ee6` 归档分别因缺少挂图和仍有独立 hosted 块失败。
- 全仓包分批单次执行：work 的 `TestShellUsesBotDir` 失败；web 原审批测试卡住后主动终止诊断，跳过该条继续跑其余用例，另四条 parked approval 用例失败。这五条 web 和一条 work 均在 `77c0ee6` 复现；其余包通过，本次未扩修审批/解释器测试。gofmt、vet、diff 检查通过，atlas audit 保留既有告警。
- 本机 1024×1024 PNG（28,629 字节）保存＋挂图 100 次 p50 0.393 ms、p95 0.865 ms，最大 3.573 ms；仅小图文件链路，不是厂商耗时、IPC 或线上交互 p95。真实 serve 的两家 scripted 首个可见回复耗时和完整日志、浅深色截图见 `~/.xbot-build/imggen-attach-bubble/encode-report.md`。
- 已合本地 main（功能 tip `b389806`）；复审 PASS（无 P1）；`docs/dev/imggen-attach-bubble.md` 已毕业删除；atlas kernel/providers/ui 覆盖同轮挂图与普通工具轨迹。未 push；无 UI 大改未喊设计师。未使用 `-count=3`。

## 2026-10-27 例程 create 重复键末位 null 回修（功能分支）

- 审核与创建落盘共用 `parseRoutineCreate` 和 `Precheck *string`，消除 `914a2f8` 中“审核为空、string 落盘保留先前命令”的不一致；保护路径仍走原 reviewCall，未改 Confirm 决策、执行器、HTTP/update 或 P2。解析约定见 [安全](atlas/security.md)。
- 正式 `TestRoutineCreatePrecheckDuplicateNull` 用真实 agent 审批、落盘与 Timer.Fire 覆盖九种输入；旧实现的末位 null 与大小写变体均实际执行了未批准 marker，修后不落命令、不执行。缺字段、null、空串、末位敏感命令需审批和单字段 true 保持一致。
- kernel/link 全包 `go test -count=1` 通过（49.700 / 2.917 秒），web 例程单次通过（0.605 秒）；相关 vet、gofmt、diff 检查通过，atlas audit 已执行。未跑 count=3、race 或全仓；本改动不增加进程、网络或文件调用，未测生产 p95。
- 证据：`~/.xbot-build/precheck/fix-p1-dupnull-report.md`。仅分支提交，待独立复审 PASS 后合入；未合 main、未 push、未起 8788，P2 串行等待/取消残余未改。

## 2026-10-27 例程前置检查 P1 审核回修（功能分支）

- 有效 precheck 的创建/修改转回现有 shell 的 `Kernel.reviewCall`，更新省略字段也审保留的命令；保护路径使用执行时的进程 cwd。HTTP 修改复用同一判定，需确认时 403、整次不落盘。定时执行遇有效 machine ask/off/读取错误静默跳过并 Commit slot。约定见 [安全](atlas/security.md)；P2 串行等待/取消不传仍为已知残余、不阻断。
- link/kernel/tools/review/proc/serve 全包 `-count=1` 通过；web 例程用例单次通过。新增敏感路径、机器 ask/off、permissions on 删除/外发、创建与修改拒绝/确认、实际 cwd、HTTP 拒绝写入、运行中策略变化用例，放回 `59b41d1` 归档均能检出旧绕过。未使用 `-count=3`。
- link/tools/review 全包 race、web 例程与 kernel 例程/权限定向 race 通过；kernel 全包 race 报既有 configureWeb 竞争（四个子代理/环境用例），其中环境用例在 `59b41d1` 同样复现，未扩修。相关包 vet、gofmt、diff 通过，atlas audit 保留既有告警。完整证据 `~/.xbot-build/precheck/fix-p1-report.md`。
- 100 次 Timer 微基准均值：缺字段 0.637 ms/op、true 4.436 ms/op、false 3.084 ms/op；仅本机文件/进程链路，不是交互或 IPC p95。P2 多条检查仍可能累计约 2×N 秒，生命周期取消问题本轮未改。
- 仅分支提交，待独立复审；未合入 main、未启动 8788、未 push，未声明真入口或用户日用通过。

## 2026-10-27 例程免模型前置检查（功能分支编码交付）

- `Routine.precheck` 使用可选 shell 命令字符串；三处日常投递入口共用检查，非零、超时、启动失败静默消费 slot，成功沿用原事件。工具及现有 HTTP 接口可读写，未增加 UI。执行边界与幂等约定见 [多 Bot](atlas/multibot.md)。
- link/kernel/tools/proc/serve 单次通过；link/tools/proc/serve 三遍通过，例程 HTTP 接口三遍通过，六个相关包的新增用例 race 通过。旧 tip `714cfbe` 归档反证：非零检查仍投递 1 个事件，新实现为 0 且 LastSlot 落盘。全仓 vet、gofmt、diff 检查通过。
- 全仓测试未全绿：web 审批用例阻塞、work 旧 shell 测试缺 description，均在上述基线归档复现；首轮三遍 kernel 测试出现一次 shell 通知时序失败，专项基线 100 次未复现，当前 kernel 全包隔离三遍通过（118.894 秒）；保留首轮失败证据。atlas audit 的既有告警未扩展修复。
- macOS/arm64，100 次 Timer.Fire 微基准：无检查 0.644 ms/op，true 3.602 ms/op，false 7.288 ms/op（含 slot 文件读写，测试并行期间采样）。这是局部均值，不是交互/IPC p95，也未验证生产后台变慢比例。超时用例在 5 秒断言范围内返回，执行预算为 2 秒。
- 证据与报告：`~/.xbot-build/precheck/delivery-report.md`。本批仅分支提交；未合入 main、未起 8788、未 push；未做真模型、真入口端到端或用户日用验收，保留 dev 供父代理收尾。

## 2026-10-27 下一版提示词与配套用例（功能分支编码交付）

- 按 SCORECARD 的 D1–D5 收拢共享原则，清空四份 DeepSeek overlay；S1–S3 去重，S4 不动。新增知识三档、模糊任务同轮开工、桌面端主动配套和中文开场六条开发用例；复用 eval/scorecard，增加零工具调用检查与带工具开场白的 fake 回放。现状见 [供应商](atlas/providers.md) 与 [内核](atlas/kernel.md)，未完成验收见 [设计](dev/next-prompt-scorecard.md)。
- fake 每题三次：原 14 题能力 7/7、意愿 5/5；原提示与最终提示在同样 20 题上均为能力 11/11、意愿 7/7、守规矩 2/2、可靠性 pass³=1.00、违例 0，give-up 3/3。真实 DeepSeek 改前改后均因未配置 provider/密钥而未启动；fake 不证明真实提示效果，真实四维与独立留出验收待复审侧完成。
- eval/kernel/prompts 三遍通过；最终全仓单次运行中这三包及 e2e 通过，web 审批测试超时、work 旧 shell 测试缺 description，均在原 tip 归档复现。gofmt、vet、diff 检查通过；atlas audit 已执行，既有提醒未扩展修复。零调用检查与开场白回放有旧实现失败反证。
- 同夹具共享前缀 6328→6247 字节；StablePrefix 三次微基准中位数 1095→1086 ns/op，均 10516 B/op、20 allocs/op；40 步最多 10 次折叠的门槛不变，短措辞十次专项通过。未测真实回复/IPC p95 或帧率。
- 证据与交付报告：`~/.xbot-build/nprompt/`。仅功能分支提交，未合入、未 push，未声明真实模型端到端或用户日用通过；保留 dev 的剩余验收项。

> 这里只记**已经发生的事**：做完了什么、验了什么、实测数字、提交号、踩过的坑。**正在做什么、怎样才算做完写在 [dev](dev/) 里那份设计**；还没开始的想法在候选清单（不在仓库，在用户的 Obsidian：`正在干什么/06-XBot/XBot 候选清单.md`）。现行设计见 [atlas](atlas/README.md)。每日测试报告不进仓库。

## 2026-10-27 编辑资料名字标签（分支验证）

- 编辑资料名字行由「标识」改为「名字」，复用原字段与保存逻辑；人设卡库、选用、导入和已删除字段保持原行为。当前用语见 [ui](atlas/ui.md)，复审与合入待办保留在 [设计](dev/persona-label-name.md)。
- 全仓 `go test -count=1 ./...`、web 包 `-count=3`、全仓 vet、gofmt、JS 语法与 diff 检查通过；新增 Chrome 回归按可见标签编辑并保存名字和简介，旧产品代码因找不到「名字」输入框失败。atlas audit 已执行，既有文档告警详见交付报告。
- 1440×900、DPR 2 的前后浅深色默认／选中截图在 `~/.xbot-build/ui-shots/persona-label-name/`；自检与日志在 `~/.xbot-build/persona-label-name/delivery-report.md`。产品 app.js 前后均 334719 字节，新增请求／轮询为 0；未测模型、IPC p95 或帧率。分支验证通过，未合入、未 push、未做设计师验收。

## 2026-10-26 公网 UI 单源

- 设备新增受限 `GET /api/ui/static/<path>`，只读内嵌 staticFS；保留 MIME / ETag / no-cache，补设备 tip 与 700 KiB 超限 413。pipe 保持 owner `/api/` 边界，复用 Do / AuthorizeOwner，仅扩展静态元数据白名单。
- 配套账号分支删除业务 static 副本，保留 boot / bridge / scoped resource worker 与字体；完整页面、脚本、样式来自当前设备，切设备关闭旧 pipe、重载，无设备/离线/旧设备不支持时不回落旧壳。账号 API 仍在云上，sync 脚本退出业务 UI 发版路径。决策见 [ADR 0029](adr/0029-public-ui-single-source-via-pipe.md)；atlas 真源见 [界面](atlas/ui.md)「公网登录后的设备壳」。
- 相关 Go 包 `internal/web`、`internal/pipe` 单次测试及账号仓 `internal/account`、`internal/fsutil`、19 条 Node 测试、smoke-public 通过。新增出口测试在旧 xbot 归档上因路由缺失失败；新 bridge 行为测试在旧账号归档上失败。gofmt、相关包 vet、diff 检查通过；atlas audit 执行，既有陈旧、链接、映射提醒未顺手修改。
- Chromium 使用真实 Go hub / owner API 与 scripted 模型，验证完整桌面/手机界面、发消息、原生图片、只改设备端后刷新、Mini → Laptop 切换、关于版本、无业务缓存、无设备重试、账号退出及 CSP。完整相关包运行时启动 207ms，20 次 Bot API 读取 p95 0.40ms；仅为本机 fixture，不代表公网启动/交互 p95、IPC 或 60fps。
- 已合本地 main（合入 tip `a02e3f3`，现 tip 含其后批次）；设计师 PASS：设备侧 + 公网引导壳（pipe 薄引导，非另部署业务拷贝）；登录后主壳缺会话不挡收口。`docs/dev/public-ui-single-source.md` 已毕业删除。证据 box `/workspace/xbot-ui-design/review/a02e3f3-public-ui-single-source/`；encode `~/.xbot-build/public-ui-single-source/encode-report.md`。未 push。

## 2026-10-26 上下文一批 · 批 C

- 接入四项：合伙人导向对齐既有跟版规则；create_bot 用途不明先问、空职责拒绝但不加审批；七节机制手册 go:embed，模型按需 kernel_manual 与用户 CLI 同源；任务数和验收话术复用环境与结果证据。简介退出人设正文，工作档案复用记忆，人设卡/职责数据不迁移，CLI 区分运行档位与职责；无网页 UI 改动。约定见 [kernel](atlas/kernel.md) 机制手册/共享规则与 [multibot](atlas/multibot.md) 人设边界。
- 新增三组外在行为正反脚本、创建澄清/直接创建、七节先读再答、五任务状态和无测试证据两案；脚本是验收夹具，不是真实模型改前改后分数。旧基线隔离归档上的空职责、简介注入、手册接线和规则约束回归均失败。已有长轮折叠频率门槛保持，缩短规则后连续十次通过。
- 独立长程 450 轮、3 次压缩、峰值 64460 token，压缩开销 1.760%，缓存恢复最差为压缩前中位数的 97.6%，K=0.520。同一升级夹具前缀 6033→6716 字节；手册读取微基准 1036ns/op。真 CLI 七节同源与离线读取通过；其启动到结束延时见 `~/.xbot-build/context-batch-c/cli-smoke.json`，不代表模型回复/IPC/帧率。
- 普通全仓 go test、六个相关包三遍复测、批 C 专项 race 三遍、vet/gofmt 通过，六条 guard 冒烟通过。扩展 smoke 另有忙时回报、OAuth 生图、Responses 恢复三项失败，均在 f16c9a6 隔离基线复现；全工具 configureWeb 既有 race 也有基线反证，本批未修这些模块。
- Pi glm-5.3 review1 **PASS**；已合本地 main（tip `d052a38`）；`docs/dev/context-batch-c.md` 已毕业删除（本批无网页 UI，不经设计师；此前误留/revert 已纠正）。完整检查见 `~/.xbot-build/context-batch-c/encode-report.md` / `review1-report.md`。四维真实模型对照、C8 与手册面板后置；五条候选已完成。未 push。

## 2026-10-26 Agent Todo streaming 热修

- 针对 `dfaa232` 设计师不 PASS 的四处：流式回复期间继续既有 3 秒 status 轮询；侧栏和 live 优先同一 `st.todo`，无摘要时 todo／shell 用「待办／后台命令」；未完成轨迹用「进度 n/m 项」；running 预览恢复后可点击打开中文待办步骤。不改账本、工具执行、模型默认或 Tasks。
- `go test -count=1 ./internal/web/` 通过（30.797 秒，含本机 Chrome）；`go vet ./internal/web/`、JS 语法检查通过。`TestTodoBrowser`、`TestTodoStreamingBrowser`、`TestTodoLiveToolsBrowser` 均新绿；仅回退 app.js 至 `dfaa232` 后分别因 0/3 完成口吻、streaming 无 running 进度、live 裸 todo 而失败。流式测试还覆盖摘要连续变化、shell 事件不盖摘要、摘要消失后的工具人话兜底和两个轮询周期节点复用。
- 1440×900、DPR 2 的浅深色前后截图（流中默认/悬停、步骤打开、轨迹折叠/展开）在 `~/.xbot-build/ui-shots/agent-todo-hotfix/`，日志和夹具在 `~/.xbot-build/agent-todo-hotfix/`。两次非虚拟时间 Chrome 采样：status 变化到进度可见为 2993/3004 ms，符合既有 3 秒轮询；不是模型回复、IPC、60fps 或线上 p95 验收。
- 编码期未改 kernel、未跑全仓三遍；其后已合本地 main（tip `a02e3f3`，祖先含热修与 `949d542`）；设计师用户视角 Ultra 四块 PASS（含本热修）；`docs/dev/agent-todo-tool.md` 已毕业删除。未 push。

## 2026-10-26 Agent Todo 工具可见性

- 复用内核 `todo`、`todos.json` 和既有归档/提醒，将 TodoSummary 改为中文；侧栏显示待办进度，步骤弹层和轨迹使用「待办」及中文状态。轨迹成功回包才展示已保存条目，失败/未返回单独标明，原文折叠留在轨迹；全部完成归档后仍可看历史。未改模型默认开关、写盘锁或原子写路径，没有第二账本、Tasks 或对话任务卡。约定见 [ui](atlas/ui.md) 状态/轨迹段与 [kernel](atlas/kernel.md) 待办清单。
- kernel/web 的 `go test -count=1`、相关 vet、gofmt 和 diff 检查通过；新增摘要边界与浏览器行为测试，API 测试改为真实工具连续建三项/更新后读取同一磁盘账本。旧实现隔离归档上的摘要、API、浏览器三条回归均失败。浏览器验证成功/失败/未返回/放弃原因、原文折叠、归档清空预览和未变节点跨两次轮询保持；1440×900、DPR 2 浅深色截图经审美顾问复审通过。
- 五 Bot 状态接口 500 样本 p95 1.205ms；本机 Chrome 30 次轨迹切换至下一帧 p95 浅色 19.7ms、深色 20.2ms。前者为测试 HTTP handler 耗时，后者为 scripted HTTP 页面交互耗时，不代表真模型回复、IPC、后台负载或 60fps 达标。证据：`~/.xbot-build/agent-todo-tool/`，截图：`~/.xbot-build/ui-shots/agent-todo-tool/`。
- 已合本地 main；设计师 PASS（tip `a02e3f3`）；`docs/dev/agent-todo-tool.md` 已毕业删除。未 push。按本批范围未跑用户日用。

## 2026-10-26 轨迹可发现与回放

- 环境流与 `xbot trace paths` 共用真实绝对路径 helper；`list` 逐行过滤已有遥测，`replay` 直接复用 `ExportTo(Tools:true)`，无新账本和 UI 改动。约定见 [kernel](atlas/kernel.md#轨迹路径与脚本回放)。
- 编码验证：全仓 `go test -count=1 ./...`、vet、gofmt 与六条 guard 旅程冒烟通过；fake HTTP 模型经真二进制收到路径，shell 结果落盘，CLI 时间筛选与 seq 4–5 工具回放通过。证据 `~/.xbot-build/trace-discoverability/encode-report.md`。
- 已合本地 main（tip `f16c9a6`）；设计师用户视角 PASS；`docs/dev/trace-discoverability.md` 已毕业删除。未 push。
- 本机并行测试负载下各 30 次 CLI 进程启动至结束：paths/list/replay p95 为 13.438/15.470/35.565ms；本次路径字段 JSON 483 字节，约 121 估算 token。长回合测试按实际路径长度补足环境及快照预算，仍检查原折叠频率与请求上限；生产压缩不变。未测线上响应、IPC、后台负载或真模型；未 push。

## 2026-10-26 生图工作区可发现与头像热修余量

- 生图步骤与 Bot 菜单可进入 generated 路径浮层；浏览器可见/可复制服务端路径，桌面复用 open-workdir 打开固定子目录，拒绝符号链接。状态主题最多 18 字符，完整 prompt 可点击展开。模型生成请求及保存回执明确工作区交付，头像回执说已换好，图片设置脚注压短。关于页显示并复制独立构建 Tip，xbot-dev 用同一短 HEAD 注入并写两份已有记录；未改账号菜单底栏结构。约定见 [ui](atlas/ui.md#图片生成设置)。
- 编码验证：相关包单次完整测试通过（web/kernel/provider/link/work）；Chrome 覆盖目录入口/复制/失败、prompt 展开、关于短号复制。证据 `~/.xbot-build/imggen-discover-hotfix/encode-report.md`。
- 已合本地 main（tip `949d542`，本切片 `e7da2c5`）；设计师用户视角 PASS；`docs/dev/imggen-discover-hotfix.md` 已毕业删除。未 push。
- 隔离 Chrome 1440×900、DPR 2，浅深色截图在 `~/.xbot-build/ui-shots/imggen-discover-hotfix/`。假 API 下点击目录入口到两帧后（各 30 次）p95：浅色 33.3ms、深色 33.7ms；不是模型/IPC/后台负载或 60fps 结论。未测线上响应、真模型日用；未 push。

## 2026-10-26 图片生成设置（分支实现，待合入/验收）

- 设置在人设卡后新增图片生成，复用网页搜索密度与 web.json 写锁；全局默认关，OpenAI / Grok 单选，账号状态只读现有 OAuth 元数据，未登录同浮层深链至 ChatGPT / xAI 详情。新轮固定配置，只有所选家的已登录 OAuth Responses 绑定挂生图；图片继续进 generated/，不挂气泡。设计与待验收项见 [image-gen-settings](dev/image-gen-settings.md)。
- 全仓 Go 单次测试、vet、gofmt 通过；新增配置并发/幂等、API 输入边界、门控与浅深色 Chrome 交互回归。旧门控 overlay 上全局关/错家/未登录/API key 的回归失败；原工作区落图与无气泡断言继续通过。截图在 `~/.xbot-build/ui-shots/image-gen-settings/`，编码报告在 `~/.xbot-build/image-gen-settings/encode-report.md`。
- 本机 Chrome、1440×900、DPR 2、模拟 API，每个主题 30 次开关点击至下一帧：浅色 p95 35.7ms / 最大 35.9ms，深色 p95 34.9ms / 最大 35.0ms；零页面脚本错误。这是设置交互耗时，不代表真模型响应、IPC、后台负载或 60fps 达标。未合 main、未 push、未重启服务；待设计师截图复核及合入后真实账号工作区验收。

## 2026-10-26 update_profile 改名与默认直接落盘文案（分支提交，未合入）

- 基于 revert 后的 `2217ce2`，既有 profile 工具统一命名为 `update_profile`，不保留别名；工具/schema、Link 引导、权限表、cardtext、eval 与文档同步。默认 permissions off 含头像也直接落盘，无预览确认卡例外；SelfEdit 分类和 permissions on 审核不变，未改 propose_persona 流程。
- 单次相关套件通过：kernel 40.422s、bot 5.770s、review 0.702s、eval 4.591s；头像/资料 API 0.524s。补齐 Link 文案后重跑 link 0.761s、kernel 34.603s。gofmt、vet、diff 检查通过；atlas audit 执行完成，仍有文档陈旧、链接与映射等提醒。
- 回归覆盖模型工具列表、默认无卡（含无审批入口）、落盘重载、组合字段、上传清除、仅本 Bot、重复调用与非法参数。overlay 恢复旧工具实现后工具列表断言失败；恢复 `27f1970` 的头像例外后默认无卡和无审批入口落盘断言失败。
- 首轮三个默认模式 scripted 头像回合为 32.028–41.052ms（含落盘），不是线上交互/IPC p95 或浏览器帧率。日志与逐项交付在 `~/.xbot-build/update-profile-rename/`。按本次范围未跑全仓测试、三遍复测、真模型与浏览器；未 push、合入或重启服务，日用验收仍由主管安排。
## 2026-10-26 上下文一批 · 批 B（分支实现，未合入/未日用验收）

- 实现提交 `b160417`：环境补待批卡当前计数与清空 null；跨轮旧大工具结果提前变 expand 指针并保留回执；serve 空闲巡检复用后台摘要队列；换小窗口在保存绑定前压缩/拒绝；轮内先摘要开关默认关闭。没有 UI 或手动压缩入口，实体注册留批 D。规格见 [context-batch-b](dev/context-batch-b.md)。
- 最终 `go test -p 1 -count=1 ./...` 46 个测试包、kernel/compact/web/serve 三遍复测、专项 race、vet、gofmt 通过。基线 `f22b708` 隔离归档上的请求投影、空闲 serve 与换绑 HTTP 回归实际失败；B1/B3 同时复用已有环境回归。初次并行全量遇到 OAuth 固定端口冲突和插话顺序断言；未改这些断言，最终串行全量与三遍复测均绿。
- 独立 LongRunJourney：453 轮、3 次后台压缩，峰值 64552 token；压缩与抽取占比 1.755%，缓存恢复最差 97.3%，加权成本比 K=0.518。1001 行历史的未变待批卡查询微基准从 1.705ms 降至 0.051ms；阻塞 fake 摘要期间前台 Handle 单次 33.59ms。不是线上 p95、IPC 或 UI 帧率证明。
- 未合 main、未 push、未碰 8788；Linux CI、真实供应商/缓存质量、用户日用未验。完整自检、开关、已知限制与命令日志在 `~/.xbot-build/context-batch-b/encode-report.md` 与 `accept.md`。

## 2026-10-26 Bot 面板与窄屏、rail 修正（已合本地 main，待手测）

- 满宽厂头仅留编辑；模型整栏、人设满宽；更多复用原菜单，改由本机 Bot 行和置顶头像右键／长按进入，动作绑定被操作的 Bot，旧菜单及子菜单统一关闭。信息面板各断点统一右进右出，手机全高近全宽；汉堡按 52px 内容带加安全区、16px 左距、8px 间距落实。72px rail 分区头仅留 10px 透明间隔，设备头同样清掉灰底和圆角，壳间距保留。编码 tip `1765587`（rebase onto main 后 tip 见合入提交）；Pi glm-5.3 review1 **PASS**（`~/.xbot-build/bot-panel-detail/review1.md`）。
- `internal/web` 单次及新增三档浏览器回归通过；旧提交归档上的同一回归在手机/桌面关闭轴和中宽厂头额外动作处失败。Chrome 真实触摸长按通过，浅深色截图在 `~/.xbot-build/ui-shots/bot-panel-detail/`。模拟顶部安全区 44px 时内容带仍为 52px；24 次点击至动画结束 p95 196.3ms，最大 198.6ms（CSS 动画 180ms，含浏览器事件调度；不是 60fps、模型回复或 IPC 测量）。
- gofmt、vet 通过；atlas audit 的已有提醒未增加。全仓单次末轮有 `internal/code/TestLocalModelStepCapIsTheTasks` 的 TempDir 清理失败（directory not empty），本批未改该包。已合本地 main（不 push）；8788 手测见 `~/.xbot-build/bot-panel-detail/accept.md`。无独立候选（热修）。

## 2026-10-26 Bot 自改头像（已合本地 main，待手测）

- 扩展既有 `update_profile` 的 `avatar_seed`，复用 ValidAvatarSeed / SetAvatarSeed，仅作用于本 Kernel Bot；与名字、简介、时区组合更新，默认直接落盘；permissions on 按 SelfEdit 表审核。无上传能力、风格或主题变更。tip `0752c11`；Pi glm-5.3 review1 **PASS**（`~/.xbot-build/bot-self-avatar/review1.md`）。
- 当时的 permissions on scripted Kernel 覆盖拒绝、允许、非法 seed、非字符串、未知/跨 Bot 字段、同卡组合、上传清除、重载 AvatarState 与重复 seed；仅回退工具实现的 overlay 实际复现旧代码 unknown field、零确认和 schema 缺字段。指定三包与全量 Go 测试 count=1、既有头像/资料 API 与 vet 通过；三遍复测按政东要求取消。
- 首次专项四个 scripted 回合（含一拒绝）为 32.882–36.081 ms，含立即确认与落盘；不是线上回复 p95、IPC 或浏览器帧率。命令、结果与逐项自检在 `~/.xbot-build/bot-self-avatar/encode-report.md`。
- 已合本地 main（不 push）；8788 手测见 `~/.xbot-build/bot-self-avatar/accept.md`。保留 dev 设计待验收。

## 2026-10-26 StablePrefix 分段与可读环境人设（分支实现，待合入/验收）

- 实现提交 `e64d62a`：S0 人设/内核规矩/可选模型补丁、S1 能力框各用固定 `###` 标题和槽说明，保留 S2 技能边界；环境 persona 改为卡名或稳定标签加正文指纹，全文仍只进前缀。复用 PersonaText 的读取路径，由 PersonaSource 一次返回卡名与正文，保留 JSON 差分与快照回放。设计见 [stable-prefix-sections](dev/stable-prefix-sections.md)。
- `go test -count=1 ./...`（46 包，含 e2e 长程）、kernel 定向、kernel/bot `-count=3`、vet、gofmt 和 diff 检查通过；四项最终回归在 e164b7a 隔离归档中失败、当前实现通过。缓存专项确认 link 改动保留 S0 7062 字节，skill 改动保留 S0/S1 7336 字节。Atlas audit 已跑，既存告警没有新增项目。
- 扩展 smoke 套件未全绿：唯一失败 `TestNewTaskWhileBusy` 缺子任务验收值转告、relay-s3 超时（当前 124.64s）；e164b7a 原实现隔离复跑同样失败（124.80s），属基线已有失败，本批未改调度或冒烟期望。其余冒烟未报失败。完整日志与交付：`~/.xbot-build/stable-prefix-sections/encode-report.md`。
- Apple M6 / Darwin arm64 五次微基准中位数：StablePrefix 1494→1226 ns/op、14309→8595 B/op；完整分段组装 1510→1664 ns/op、12460→11612 B/op（约 +154ns，+10.2%）；四槽齐全固定增加 332 字节。未测真实模型缓存收益、交互/IPC p95、后台负载或帧率，不以微基准代替这些指标。
- Pi glm-5.3 review1 **PASS**（`~/.xbot-build/stable-prefix-sections/review1-report.md`）；真模型自述与用户日用尚未验。dev 保留供手测。

## 2026-10-26 官方薄入口续作（分支交付，本机 scripted 浏览器通过，未合入）

- 复用同款 static、账号 Bearer、ADR 0024 票/待命/AuthorizeOwner；加五种 `/api/` 方法、双向 Content-Type、正文限额与领票 CORS。serve 保存/恢复用户账号会话并独立报在线，轮换/登出/失效取消旧管道。
- 本机 Chromium 经真实 JS Web Crypto → Go Hub → 用户 API，用 scripted Provider 完成桌面/手机主界面、发消息、原生图片取回、离线重试和退出。连续 3 次首屏样本 48–54 ms，每次 20 次 `/api/bots` 的 p95 0.6–0.8 ms；不是公网、真模型、IPC 或帧率测量。完整 Go 套件与前端 15 条测试通过，详细命令、部署和余项见 `~/.xbot-build/public-thin-entry/encode-remote-ui-report.md`。
- 扩展 `smoke` 套件唯一失败为 `TestNewTaskWhileBusy`（子任务缺结果汇报、relay 等待超时），在隔离归档的 main `e164b7a` 上同样复现；六条基础旅程未出现失败。本批没有修改忙时任务逻辑，不能把扩展冒烟报为全绿。
- 不合入 main、不 push、不重启 Mini 8788。真实账号手机公网验收交指挥官与用户，尚不报用户日用通过。

## 2026-10-26 hosted 生图 P2：多图完成事件预算（分支回修，未合入）

- SSE 生图行预算按既有 20 MiB/图 × 8 张的 base64，加有限输出元数据与事件封装计算，修复 completed 重复携带多张大图使整轮报 token too long、最终路径丢失的问题。单图/张数/落盘上限、未启用生图的 1 MiB 行限与清理后的 1 MiB 输出预算不变。
- 新增两张各 13 MiB 的实际字节夹具，completed 行 36350890 字节；覆盖 done 已保存后重复 completed、仅 completed 两种路径，检查文件内容、去重、最终 Message 和模型回放中的相对路径。修前两场景分别在保存 2/0 张后报 token too long，修后均保存两张并保留路径。
- 专项命令、耗时与提交见 `~/.xbot-build/hosted-image-gen/fix-p2-report.md`；本批不碰 shelljob、消息挂图或 gallery，不合 main、不 push，不再启动全量长测。

## 2026-10-26 厂商 hosted 生图（分支实现，替身端到端通过，未合入）

- 沿用已有未提交实现，完成 Responses `image_generation` 的独立开关、OpenAI tool 级模型、xAI 默认 Imagine、工作区 `generated/` 落盘及相对路径回放。设置写入现有 ModelSpec；搜索关仍可生图。复用 OAuth/vault、PartHosted、BotDir，不新增本地工具，不改消息挂图或气泡 gallery，不自动写 transcript Images。
- 单图 20 MiB、每个响应 8 张、目录 500 项；流式图片行最多 32 MiB，移除 base64 后保持 1 MiB 输出预算。补终态失败、空结果、解码/落盘失败、重复完成省略 result、无 ID 限额及并发目录限额检查。四条失败终态回归和原实现边界回归有修前失败证据；独立复审发现的残留 result 假成功已修复，独立 overlay、四包专项 race 及真实 serve 双 OAuth 冒烟通过。
- 六条基本旅程与 Responses 搜索兼容冒烟通过；hosted 冒烟使用真实二进制、HTTP 设置/绑定、vault OAuth 注入和磁盘，上游 scripted。独立复测首个可见回复 ChatGPT 83.7 ms、xAI 101.1 ms（各一例）；本机 Chrome 30 次开关保存 p95 69.7 ms，含驱动开销，不代表线上回复 p95、IPC 或帧率。1440×900、DPR 2 的浅深色默认/悬停/开启截图已留存，保存和刷新读回通过；用户确认沿用界面。
- 按用户最终收束要求以 hosted 专项作为本批门禁；此前全量/三遍有 shell 通知测试失败，未修该测试或生产机制。vet、格式、构建、atlas audit 与完整交付见仓外 `~/.xbot-build/hosted-image-gen/encode-report.md`。未合 main、未 push、未重启日用服务；未测真实账号权限与生图、Mini 8788、用户日用、60fps 或后台负载。dev 保留，候选不标完成。

## 2026-10-26 xAI 退出与自动刷新竞态 P2（分支实现，未合入）

- 保留 `b2e586f` 的刷新写回核对：与删除共用 secrets 文件锁，重读后只有旧凭证仍匹配才保存；退出无需等待 token 端点。没有用第二次 logout 清理。
- 回归覆盖本地独立 Store 与 daemon 的 Remove，以及两种后端的真实 logout HTTP 路由：刷新阻塞 → 退出成功且凭证不存在 → 放行刷新 → 凭证仍无、绑定为 0、status 为 idle。HTTP 用例限制退出最多等待 3 秒，失败时也释放假端点。
- 用 overlay 仅换回 `7589cca` 的刷新实现，四个子用例全部重现凭证 configured；修复版各三遍通过。本机假服务的六次 logout 往返为 0.440–0.749 ms，端点全程阻塞；这是局部样本范围，非线上 p95、IPC p95 或帧率结论。
- 指定抽测、四包三遍、race、Chrome/Node、gofmt、vet 通过。全量首跑遇到未改动的 kernel 通知用例失败，该例单跑三遍通过后，全量复跑通过；保留首跑日志。atlas audit 退出 0，仍有既有文档告警。
- 级别：分支实现与 fake/scripted 局部验证；未合 main、未 push、未重启 8788、未做真人 xAI 或用户日用验收。门禁与交付证据见仓外 `~/.xbot-build/xai-oauth/detail-ui-logout-fix1.md`。

## 2026-10-25 网页搜索设置（本地实现，未合入）

- 设置浮层在权限后、设备前增加网页搜索。全局开关、自动 / 总用本机和九家单选按定稿接入；默认 DuckDuckGo，DeepSeek 已实现，其余七家后端占位。选中为中性灰；密钥「试一下」成功即保存，失败保留输入，不回显密钥，不增加保存主按钮和供应商详情开关。
- 搜索策略覆盖已有内核、作用域工具、本机子代理、内置编码任务和新远端任务的执行端点；修改不重建内核，运行中的远端任务保留启动快照。密钥沿用本机凭证写入与脱敏路径，serve 重启前置载入；探测沿用可信出口网段并固定官方地址。
- 本机 Chrome 1440×900、DPR 2 的六态与浅深色均有截图。读回、空密钥禁用、通 / 挂、失焦加点击单次探测、改字时迟到响应、关闭取消及保存失败保持状态通过。30 次自动化切换至选中态更新 p95 68.7 ms，含浏览器驱动开销；不是网络搜索时延或帧率结论。与同视口改稿做了像素差和取色，三种禁用紫色均未出现。状态接口顺序三轮 p95 为 1.207 / 1.183 / 1.156 ms；并行跑两组门禁时曾测得 8.987 ms 超过 5 ms，未改阈值。
- 最终全量 Go 测试、cmd/xbot / config / provider / kernel / web 三次、go vet、gofmt、前端语法检查通过。审阅发现的启动脱敏、可信出口、编码任务绕过、旧凭据绑定丢失、界面状态同步、地址与密钥混配、旧响应覆盖新校验结果与密钥轮换脱敏竞态已修复；回退修复后，对应回归实际失败，恢复后通过。
- 没做真供应商密钥 / 真实搜索网络验收、Windows 原生窗口、60fps 测量和设计师复审。未合 main、未推送、未发版，未重启日用服务。代码审阅、门禁与交付证据在 `~/.xbot-build/web-search/commit.md`。

## 2026-10-25 流式结束后回复消失（本地实现，未合入）

- Responses 保留流中完整输出条目；完成事件没有条目时，不再把已流出的回答落盘为空。真正空回复走已有错误与失败通知。历史空助手行显示可读提示，当前 Bot 和用户的消息保留；不改 OAuth、模型能力或设置布局。
- 旧代码反证复现回复消失和空落盘。浏览器覆盖正常、空回复、轮询、切换 Bot 与迟到流；HTTP 集成覆盖落盘及错误。两包三遍、全量 Go 测试、vet、gofmt 通过。只读 `codex review --base d39a8d0` 未发现明确缺陷；审阅沙箱不能跑 HTTP/浏览器，主 Agent 本机已跑通。
- 1440×900、DPR 2 的浅色和深色前后实图已自检。20 次脚本化流结束到历史 DOM 重绘的 p95 为 3.5 ms，最大 4.7 ms；不代表真模型回复、IPC 或后台负载性能。现场完整页面消失和供应商原始事件未复现，已复现的是回复气泡消失；未做真实账号或 8788 日用验收。
- 证据与手测步骤：`~/.xbot-build/chat-vanish/commit.md`。未 push、未发版、未合 main、未重启服务。

## 2026-10-24 ChatGPT 新模型能力（本地实现，未合入）

- OpenAI 看图与 Responses 搜索共用默认名单，补齐 GPT-5.6 luna / sol / terra、GPT-6 astra / luna / sol、GPT-6.1 sol 及家族别名；显式能力覆盖仍优先。设置详情与绑定运行时复用同一规则，SIWC 仍只提供模型名单与窗口。
- 相关回归通过；回退旧推断后，看图、Resolve 与订阅详情回归均失败。本机替身 SIWC 名单 HTTP 往返 1.225 ms，非线上 p95。完整门禁与审阅结果见 `~/.xbot-build/chatgpt-caps/commit.md`。
- 级别：本地实现；未做浏览器、真实模型或用户日用验收。未合 main、未 push、未发版、未重启 8788。

## 2026-10-24 ChatGPT 订阅登录入口（本地实现，待主管复审）

- 放开 A-magpie 订阅区 ChatGPT 卡。点击后复用既有 OAuth start/status 与 vault；等待时可重开授权窗且不重绘，失败可重试，成功刷新供应商并显示绿点。详情支持重新登录，模型仍用能力芯片，Bot 绑定路径不变。
- 模型名单接口复用登录时的 SIWC 目录解析，按账号可见模型返回实时名单；无凭证引导重新登录。不增加 OpenRouter 列表登录入口，不做 Claude 订阅 OAuth。
- 全量 Go 测试、web / config / providerauth 三遍、vet、gofmt、node 语法与行为测试通过；旧代码回退后两条回归失败。Chrome 已走脚本化登录与详情，浅深色截图已查看。SIWC 本机替身名单往返 0.683 ms；未测真人授权、线上 p95 或原生桌面壳。证据在仓外 `~/.xbot-build/chatgpt-oauth-ui/commit.md`；未合 main、未 push、未重启服务。

## 2026-10-24 模型与供应商：A-magpie 返工（本地实现，未合入）

- 统一设置浮层保留。列表改成订阅 / 供应商分区、右侧搜索、两列圆角卡。卡片只放透明品牌标、短名和已配置绿点；当时的 ChatGPT 订阅占位已在本页「ChatGPT 订阅登录入口」批次放开。详情用协议标签和 hosted / vision / 窗口芯片行，删除表格及原型制作说明。
- 详情按需拉供应商模型名单，复用配置中的能力规则，未知窗口使用仓内默认 128000。名单不写回配置，不改绑定选择器。拉取有超时、分页和 1 MiB 输入 / 输出限制；密钥未配、失败和空名单均有下一步。返回保留搜索条件，迟到响应不会改写新页面。
- 全量 Go 测试、web / config 三遍、vet、gofmt、前端语法检查通过。回退路由至 base 后，三协议动态名单回归均因 404 失败。审阅发现的特殊供应商名称已修复；当时的 ChatGPT 名单限制已在本页「ChatGPT 订阅登录入口」批次解除，两个回归均有修复前失败证据。Chrome 用实际设置壳和脚本化 HTTP 数据通过搜索、详情、重试及 2048 行检查；浅色、深色和窄屏截图已对照定稿。九次本机替身 catalog HTTP 往返为 0.52–1.35 ms；这不是线上 p95 或 60fps 结论。
- 完整命令、截图、差异和提交见仓外 `~/.xbot-build/mp-capability/commit.md`。atlas audit 退出 0，保留既有告警。未测真实供应商、Windows 或原生桌面窗口；未合 main、未 push、未发版、未重启 xbot-dev，待主管与设计师复审。

## 2026-10-24 多 Provider 批 4：OAuth（本地实现，未合入，验收未全过）

- OpenRouter PKCE 换取可重新授权轮换的 key，复用 Completions；ChatGPT 官方 Sign in with ChatGPT 动态注册、身份与 scope 校验、自动刷新，复用 Responses。令牌原子保存在现有 vault 的共享命名空间，本地和 daemon 都用原域名校验及注入。共享命名空间采用 Bot 名称不允许的前缀，避免 Bot 删除或改名影响密钥。API key 绑定保留；没有 Claude 订阅 OAuth 入口。
- 假授权服务验证 PKCE、nonce/签名/scope 拒绝、令牌加密与重启、并发单次轮换、上游实际 Bearer、重新登录保留模型配置；真实 Unix socket daemon 通过。模型正文中的工具占位示例不替换；编码折行与重复前缀缓冲边界脱敏均有修复前失败证据。桌面登录用系统浏览器，返回空句柄仍持续查询状态，脚本化宿主回归通过；未做原生窗口实机登录。
- 两条 OAuth 绑定各跑完整六旅程：5/6，分别 312.934 s、313.409 s。动手做事已产出文件并报告完整路径，但守护用例仍要求完成事件内联 summary.txt，当前事件只含 result.txt 指针；纯 key 同一断言失败。没有改旅程、断言或内核事件来换绿。替身沿既有 mockopenai 做法读取子代理结果，插话旅程通过。
- ChatGPT 替身网页首字 350/333 ms，插话 320/322 ms。带长身份令牌的 race 流式测试本地/daemon 首段约 9.0/9.7 ms；未测线上 p95、IPC 额外开销 p95、后台变慢比例或真人账号。API key Responses 搜索与换回 Completions 专项通过。
- 全量 Go 测试、相关五包三遍、vet、build、gofmt、atlas audit 有本次通过记录；较早一轮 web 插话顺序测试偶发失败，保留日志，未扩范围修内核。三轮 `codex review --base 2052ee1` 的实际问题当次处理并补回归。完整命令、限制、提交与审阅证据在仓外 `~/.xbot-build/multi-provider/commit-b4.md`。未 push、未发版、未合 main；整批与用户日用验收均未通过。

## 2026-10-23 上下文 A″：缓存前缀分段（本地实现）

- S0（人设、跟版 KernelRules、自有补充、overlay）、S1（Links）、S2（installed 与技能目录）用连续 system 消息组装；空目录省略，记忆与环境位置仍在后面。复用 A′ 规则入口、既有目录渲染与安全包裹，不改 Provider。
- 专项假缓存：技能变化时完整消息边界缓存从 0 到 6797 字节；Link 变化从 0 到 6616 字节。细粒度公共前缀分别为 6955→6951、6723→6720 字节，差异来自分隔换行；该口径无实质提升。总估算含消息开销 1748→1756 token。以明确分界收益和微小开销保留拆段，不承诺真实供应商收益。
- Apple M1 前缀组装基准 1000 次：3794 ns/op、12164 B/op、30 allocs/op。长程替身 455 轮通过，3 次压缩、prompt 峰值 64687、压缩成本 1.758%、缓存恢复通过，成本比 K=0.516（压缩对不压缩，非拆段对照）。未测真实供应商、线上 p95、IPC、后台变慢比例与用户日用。
- 首轮目录测试因只读取第一条 system 失败，读取器改为读取全部连续 system，原技能隔离与停用检查保留。只读 `codex review --base c938d1b` 未发现明确可操作缺陷；其 kernel/provider 测试受沙箱限制，compact 测试通过。`go test -count=1 ./...`、`go test -count=3 ./internal/kernel ./internal/skill`、专项缓存测试、`go vet ./...` 通过；`gofmt -l .` 无输出。atlas audit 退出 0，仍报既有陈旧/未挂接文档及测试临时文件告警，未扩范围处理。完整门禁和提交证据见工作区 commit.md。未推送、未发版、未合 main。

## 2026-10-24 多 Provider 批 3：Anthropic Messages（本地实现，未合入）

- Claude API key 绑定接入 Messages，沿用内容块、协议槽位与既有工具执行路径。显式启用的 hosted 搜索与本地搜索互斥；签名、搜索结果与引用可同协议回放，未知块降成文字摘要。无 OAuth 接线，无静态界面或前缀组装改动。
- 合成流与内核替身覆盖工具往返、GPT → Claude → GPT → DeepSeek 换绑、窗口和环境工具集更新、hosted 计次及 Jobs 隔离。单次 Claude 合成工具轮约 32 ms；未测真模型回复 p95、IPC p95 或后台变慢比例。
- build、vet、gofmt 和 Provider/kernel 三次测试通过。默认全量曾通过，最后一轮 web 插话顺序用例失败；四包三次检查也有同一失败。六条旅程 smoke 中忙时插话、动手做事缺少最终报告，Responses 搜索和其余旅程通过。审阅发现的工具图片、无工具总结、参数精度、重定向凭据、图片限制、搜索状态及续请求用量问题均有修复前失败的回归证据。
- 检查、审阅及提交证据见仓外 `~/.xbot-build/multi-provider/commit-b3.md`。未 push、未发版、未合 main；未做真钥或用户日用验收。

## 2026-10-23 上下文去重：overlay、todo 与完成事件（本地实现，未合入）

- 验收 #1–#5 已落地。Overlay 正文只在固定前缀，环境差异和压缩快照只公布模型键与短哈希；todo 保留完整磁盘清单，回包只报变更项和条数；完成事件改为结果文件指针与短开头。create_bot 既有短回执和 persona 环境哈希保持，补齐真实请求回归。
- 实际载荷：2030 字节 overlay 在环境中只留键和 12 位哈希；8 项 todo 初次回包 1051 字节，两次单项状态更新均 184 字节。1363 字节 fork 结论的完成事件 526 字节，全文可通过已有 read_file 读取。20KB 未读控制留在同一结果文件，完成事件守住 4096 字节上限。
- 回退五个生产文件到 25caf8f 后，新增 overlay、todo、fork 与子代理完成回归均失败；修复已恢复。旧派活断言改为核对指针和完整文件，e2e 假模型用已有 read_subagent 取回结论，原异步响应和记忆验收保留。
- gofmt 无输出，go vet ./...、相关包 kernel/exec/mockopenai 三次、go test -count=1 -v ./... 通过。长程假模型 455 轮、3 次后台压缩，prompt 峰值 64684（硬上限 107200），压缩成本 1.759%，缓存恢复检查通过。M1 本机已有基准各 1000 次：环境回放 30895 ns/op，todo 差异 2827 ns/op。未测线上回复 p95、IPC p95 和后台变慢比例；无界面改动。
- 首轮只读审阅的 P2（长未读控制绕过完成事件整体上限）已修复并补回归，最终只读复审未发现明确可操作缺陷。atlas audit 仍有既有告警，未扩范围清理。未跑真模型、Linux CI、线上长对话和用户日用；未推送、未发版、未合 main、未重启 8788。

## 2026-10-23 上下文 A′：规则随版本组装（本地提交，未推送）

- 当前 `KernelRules` 每次从二进制组装，`rules.md` 只留可映射的 Bot 补充；创建、导入不复制默认规则，fork 继承补充。迁移不提示、不保留历史目录。范围与异议见 [短计划](../plan.md)。
- 新增升级回归通过；回退旧 `kernel.go` 后同一回归失败。实际组装前缀 5635 字节，包含当前 Silence 的 routine fallback 与中文补充。并发迁移、重复迁移和空补充均覆盖。复审发现并修复 CRLF 段落、旧重试残句及 CLI 创建遗漏；新增初始化回归在旧 CLI 上失败。
- `gofmt -l .` 无输出，`go vet ./...` 与 `go test -count=1 ./...` 通过（包含 e2e）。`go test -count=3 ./internal/kernel ./internal/bot ./internal/web`：kernel/bot 通过；web 出现状态接口 p95 6.058ms 与插话多一条 answer 的失败。只复跑这两项各三次均通过，状态接口 p95 1.002–1.163ms；修复迁移边界后，完整三次包测试 kernel/bot/web 均通过，命令行包 `go test -count=3 ./cmd/xbot` 也通过。
- atlas audit 有既有的文档大小、陈旧基准、未挂接文档等告警，未扩大范围清理。最终只读 `codex review --base 6ff19e6` 未发现明确可操作缺陷。未跑真实模型 Silence 六次对照、线上升级、Linux CI 和用户日用验收；不宣称端到端升级已通过。

## 总表
| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 核心循环、Link、Event、收件箱、人、记忆接口 | 已完成 `a32d242` |
| M2 | 记忆、定时任务、`serve`、环境快照差异、摘要块、按需找回、供应商 | 已完成 `233aaf7` |
| M3 | 单一事件处理循环、子代理、执行者、工作区、技能 | 已完成 `9d233fa` |
| M4 | Webhook、文件监听、合并与限流 | 已完成 `304fa00` |
| M5 | Web 界面、PWA、Bot 之间通信、群 | 已完成 `caa53a1`；UI 精修中 |
| M5.x | 后台异步压缩、Fork Bot、主 Bot 与置顶、Bot 档案 | 已完成 `db6487e` `4b07ec4` `5a96e75`。网页接口见 [API.md](API.md)，静态界面由单独的代理维护 |
| M6 | 手机后台推送、远程访问（Cloudflare Tunnel 等）、多节点、人的记忆同步、机器开关、桌面操作、浏览器原生 Link、MCP 适配器与连接器目录、编码代理 Link | 手机推送、隧道（公网可达性未测）、多节点（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）、人的记忆同步（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）、机器开关、桌面、浏览器：已验证。精选目录、Telegram/飞书、MCP（stdio 与 Streamable HTTP，按 Bot 启用，密钥只记名字）、编码代理（确认后启动，输出进对话卡片）：已接上。托管市场未做 |
| M7 | 凭据代理与保险箱、危险操作审核 | 盒子回归（假模型，2026-10-16）。已验证：保险箱 CLI、落盘加密与权限、只向绑定域名注入、跨域跳转不带真值、工具结果/transcript/模型输入里脱敏；删除、外发、带密钥或写方法的 HTTP、`keys:true` 都出确认卡，拒绝后不执行；只读不问；`decisions.jsonl` 留痕。修复：强制 `git push` 不确认；密钥挪到 `<root>/keys/vault/<bot>.key`（旧数据打开时迁移），触及保险箱/密钥路径的 `shell`/文件工具无论机器开关都出确认卡。独立用户守护进程 `xbot vaultd`（[ADR 0005](adr/0005-vault-daemon-user.md)）已在盒子上以 `xbot-vault` 用户端到端验证：迁移后 Bot 读不到密钥和密文，注入、拒绝未绑定域名、跨域跳转不带真值照常；其他 uid 连接被 SO_PEERCRED 拒绝；小请求额外开销 p95 0.22 毫秒（300 次）。遗留：Bot 所在用户若有免密 sudo，隔离形同虚设 |
| M8 | 开源网关与跨设备 Link（[GATEWAY.md](GATEWAY.md)，G1–G7）；P2P、跨用户通信、模板分享推迟 | G1–G7 全部已合入：网关、身份与 `bot@设备` 收发、邀请码配对、端到端加密、发件队列与退信、`list_bots` 目录推送、桌面「设备」抽屉配对（`internal/web/static/devices.js`）。盒子上两个数据根 e2e 通过（配对、加密往返约 1.9 秒、网关停机后重试送达、目录推送）。官方网关 `https://xbot.babelgo.cn`（Cloudflare Workers，ADR 0011）已部署，客户端默认连它，两台安装经它配对和收发实测通过（2026-10-18，往返约 1.2 秒） |
| 桌面端 | 本地优先桌面端（Windows、Mac），见下文「本地优先桌面端」 | A、B、C 三线代码已交付：`xbot app`、跨平台核心、打包出 dmg 与 Windows 安装器，GitHub Actions 上 Windows 与 macOS 全量测试在跑。真机逐项验收未做 |

全局验收：每个里程碑都要报性能数字（交互回复 p95、IPC 开销 p95、后台变慢比例、链路数据量；涉及界面的报帧率），预算见 [AGENTS.md](../AGENTS.md) 的「全局要求：性能与界面流畅」。

## 8 步内核计划（2026-10-17 定）

来自 [Claude Code 内部机制调研](research/claude-code-internals.md)，用户批准。文件检查点和回滚、hooks、规划模式不做（见候选清单）。这里的 M1–M8 是这份计划的编号，和上面总表的里程碑不是一回事。

| 步 | 内容 | 状态 |
|---|---|---|
| M1 | 提醒通道，熔断与打转检测 | 已合入 |
| M2 | 文件新鲜度 | 已合入 |
| M3 | 待办清单 | 已合入 |
| M4 | shell 后台化 | 已合入 |
| M5 | 环境感知（AGENTS.md 分层加载、持久 cd、token 用量、外部改动 diff） | 已合入 |
| M6 | 搜索与看图 | 已接上 |
| M7 | 子代理类型（工具白名单、提示片段、步数上限、防递归） | 待做（步数上限 `MaxSteps` 已在 `internal/exec` 里） |
| M8 | 权限规则（deny → ask → allow，「不再询问」） | 暂缓（2026-10-18，[ADR 0013](adr/0013-features-before-security.md)：先把功能调通调顺，安全只留接口） |

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

提醒走已有的 `todo` 种类。活动清单在，并且距上次成功更新已经过 N 步（默认 6，模型 `todo_steps` 或内核 `TodoSteps`）才发；只发相对上次提醒的差异；冷却 2 分钟；刚更新的那一步不发。步数记在内存里，开关关掉时不碰文件，只是计数也不重写。模型配置 `todo` 显式开关，缺省时名字里带 deepseek 的才开（空绑定也算）。除 `links`/`todo` 外全是只读工具的一轮不发。定时唤醒不发待办提醒，也不跑完成门。清单跨回合保留：用户说继续，或前台插话时后台还在跑，都不清掉。每一项都已 done/dropped、写入空清单、或新清单和旧清单没有相同 id 时，旧清单追加进 `todos-archive.jsonl`，活动清单清空。完成门只在这一轮写过或改过清单、模型又要在还有未完成项时停下时，另起一条用户消息注入一次；最后一句是在向用户提问，或还有待确认卡时，不注入。同一轮只一次。步数上限和熔断照旧，不会为了清单一直续。

假模型回归：`TMPDIR` 在仓库内，`go test -count=1 ./...` 连续三遍，`go test -race ./internal/kernel/...`。本机基准（`go test -bench=BenchmarkTodoDiff -benchmem -benchtime=200ms`，amd64）：3569 ns/op，5808 B/op。远低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测，未改网页步骤弹层。

## shell 后台（研究笔记里的 shell 后台化，本轮称为 M4）

相对待办清单那一版。`shell` 默认超时 120 秒，最长 600 秒，模型可传 `timeout_seconds`。到点不杀进程，转成后台，回一条短结果：任务 id 和输出文件路径。`run_in_background` 立刻转后台。输出写在 `bots/<name>/shell-jobs/<id>.log`（目录 0700，文件 0600）。原始字节先留在内存里。最后一段至少 1024 字节、也至少比已知最长密钥形式多 1 字节，这段不进文件、预览或 `shell_output`。每次写入按替换区间把原文偏移映射到脱敏结果，只提交窗口之前已经确定的那段。结束时把剩下的全部脱敏后写入。文件超过 256 KiB 再丢掉头部。已提交的流长度只增不减。`shell_output` 的 offset 按整段流计算，丢掉的前缀会写明。`shell_output`、`shell_status`、`shell_cancel`（杀进程组）在权限表里分别是只读、只读、普通，只有挂了任务表的 shell 才出现。同一 serve 进程里跨回合还在。重启把对不上 `proc_start` 的 running 标成 lost，不向旧 pid 发信号；另一个进程如果对上了仍活着的 pid 和 `proc_start`，就不改这条记录。完成提醒或叫醒会把 `notified` 写进任务记录，同一次完成不会又提醒又叫醒。只有本进程结束的任务才往收件箱投事件。lost 最多提醒一次。结束的任务只留最近 20 个、且不超过 7 天。`Close` 杀掉本进程拥有的进程组。完成提醒走已有的提醒通道，种类 `shell_job`，贴在下一步工具返回之后，不打断正在跑的调用。Bot 空闲时往收件箱投一条来源 `shell` 的不可信事件叫醒它，不另开 `bg_` 段，熔断照旧。`sleep`、`watch` 和带 `sleep` 的 `while`/`until` 不自动转后台，交给熔断；显式 `run_in_background` 仍可把 `sleep` 放后台。评测沙箱仍是 30/120 秒并在超时杀掉，工具名单不含 `shell_output` / `shell_status` / `shell_cancel`。没挂任务表的 shell（节点、命令行）也是到点杀掉。

假模型回归：`TMPDIR=/workspace/.test-tmp` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race` 覆盖 `internal/kernel`、`internal/tools`、`internal/eval`、`internal/review`。未提交的尾部不落盘。提交点按替换区间从原文偏移映射过来。`TestJobLogRedactionProperty` 做 10000 次随机切块。本机基准（amd64，30 次）：`BenchmarkJobWriterTrickle` 1492 ns/op，3712 B/op，3 allocs/op；`BenchmarkJobWriterBulk` 106015 ns/op，179063 B/op，3 allocs/op（一次约 32 KiB）。20k 行按这样一块写，写端大约 4 ms。单块低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测，未改网页。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`，`go test -fuzz`（目标在，正常测试只跑种子）。

## shell 输出少留（M4 跟进）

运行中的任务不再固定留住最后 1024 字节。待写尾巴里只留最长的、正好是某个已知密钥形式（原文、JSON、URL、base64，以及暂存密钥原文）前缀的那一段；对不上时，完整脱敏过的输出立刻进文件和 `shell_output`。还留着的字节数在 `shell_output` 和 `shell_status` 里写成 `held_bytes: N`（大于 0 才写）。`scrubSecrets` 按从长到短替换，一样长按字节序。测试里的 PEM 头尾在运行时拼接，扫描器不会把假私钥当成真的。

假模型回归：`TMPDIR=/workspace/.test-tmp` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race` 覆盖 `internal/kernel`、`internal/tools`、`internal/eval`、`internal/review`。`go test -fuzz=FuzzJobLogRedaction -fuzztime=2m ./internal/tools/`。本机基准（amd64，`-benchtime=200ms`，脱敏函数同时认一个短 token 和约 1.7 KiB 的 PEM 形式）：`BenchmarkJobWriterTrickle` 4249 ns/op，432 B/op，3 allocs/op；`BenchmarkJobWriterBulk` 93232 ns/op，253532 B/op，3 allocs/op（一次约 32 KiB）。单次写入远低于本机 IPC 5 毫秒预算。2 万行按 32 KiB 一块写，写端大约 4 毫秒。不涉及界面帧率。在 `a9c1ffa` 上会失败的回归：`TestShortRunningOutputIsVisible`、`TestShellTimeoutMovesToBackground`、`TestBackgroundFileRedactsSecret`、`TestShellJobFileUsesKernelRedactor`、`TestRunningShortOutputAndHeldPrefix`、`TestScrubSecretsLongestFirst`、`TestScrubSecretsStableTieBreak`。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`。

## 环境感知（研究笔记里的 M5）

相对 shell 后台那一版。四件事都走已有的提醒通道，不进系统提示，也不新加工具。

项目说明：调用方的工作目录（没设工作区时不读进程当前目录）向上读到工作区或共享目录根。每层有 `AGENTS.md` 就用它，否则用 `CLAUDE.md`，近的在后。同一套只注入一次，`cd` 进了另一个子目录才再发。目录和候选文件的 mtime、大小、inode、ctime 没变就只 stat，不再读文件、不再哈希。合计最多 32 KiB，更远的文件先丢掉，截断时正文写 `[truncated]`。关掉标签按 `(?i)<\s*/\s*project_instructions` 拆开，大小写和空白都盖住。文件用 `O_NOFOLLOW` 打开，最后一段是符号链接就不跟。允许的根以外的路径不读。工作区根上、且该目录不是 git 检出、也不是共享目录时，文件是用户放的项目约定，贴在最新一条消息上。共享目录、带 `.git` 的工作区根、子目录或克隆仓库里的是仓库提供的数据，不是用户的指示，只作编码约定，不能当作批准、发送、读密钥或删除的依据；整段包在 `<untrusted_event source="project">` 里，单独成一条，不写进用户那条可信正文，下次组装也不贴回去。权限表不看这些正文，写入提醒前仍过脱敏。稳定规则加了 `ProjectRule`（`DefaultRules`）。新 Bot 的 `rules.md` 带上它；已经写好的 `rules.md` 不变。`evals/` 和 `internal/eval` 没有提示词快照或哈希基线可改，这次稳定前缀变长是有意的。

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

`web_search` 的代码默认仍是 DuckDuckGo 的 HTML 接口，不需要密钥。部署选 DeepSeek 时在 `web.json` 写 `search.backend` 为 `deepseek`（或 `XBOT_SEARCH_BACKEND=deepseek`），`base_url` 可省略，省略就是 `https://api.deepseek.com`。请求是官方 Search API：`POST /v1/tools/search`，正文只有 `query`（1–128 个字符）和 `count`（1–50；工具侧默认 5、最大 10），不传 `need_content`，没有模型，也不走 Responses API。成功且结果非空按次计费（1000 次 5 元），空的 `results` 算成功、不计费。402 是余额不足，429 是超过每秒 5 次。没写 `api_key_env` 只用 `DEEPSEEK_API_KEY`，不回落到 `XBOT_API_KEY`。现场只有 `XBOT_API_KEY` 时，在 `web.json` 写 `"api_key_env":"XBOT_API_KEY"` 显式复用，例如 `{"search":{"backend":"deepseek","api_key_env":"XBOT_API_KEY"}}`。供应商密钥（`DEEPSEEK_API_KEY`、`XBOT_API_KEY`，或 `providers.json` 里的 `api_key_env`）只能发往 `https://api.deepseek.com`。`providers.json` 读失败时也按供应商密钥处理。已经收进内存、但不在这份名单里的搜索密钥仍可以走别的 https 主机。`http` 一律拒绝。单独的变量名会和供应商密钥一样从进程环境清掉；启动之后才写进 `web.json` 的，下次配置工具时再清一次，并进入脱敏。密钥不进日志。旧的 `search.model` 字段还读得进来，但忽略。DuckDuckGo 遇到非 200 或挑战页会报错，让模型告诉用户，不说明怎么换后端；查询词被当成回显，不拿来判断挑战页。付费抓取后端没有默认实现，配了未知的 `fetch.backend` 就失败。结果是标题、URL、摘要，有 summary 和 date 时一并写上，同样包成不可信数据。围栏里的结束标签换成 `[/untrusted_event]`。`web.json` 的 `trusted_egress_cidrs` 只放行 DNS 答案（例如 `198.18.0.0/15`），不放行字面 IP；`::ffff:127.0.0.0/104` 这类映射地址按里面的 IPv4 再对一遍回环和链路本地。

`view_image` 接受本地路径或公开图片 URL，JPEG、PNG、GIF、WebP。文件超过 32 MiB 不读，超过 10 MiB 不送给模型。边长超过 2048 会缩小；WebP 超了就拒绝。读不出宽高就拒绝。边长不超过 2048 的 JPEG、PNG、GIF 原样通过。要缩小的那一路才按 160 MiB 封顶，解码一次只跑一张，缩小后再看有没有超过 10 MiB。WebP 不解码，不占这份额。相对路径用调用方的工作目录；解析符号链接后必须在工作区或共享目录里，沙箱里钉在沙箱根。看图能力写在模型条目的 `vision` 上。`deepseek-flash`（名字里同时有 deepseek 和 flash，含 `group/auto-deepseek-v4-1-flash`）默认能看；`deepseek-chat` 和 `deepseek-v4-pro` 默认不能。显式 true 或 false 盖过名字。没绑定供应商时跟 `XBOT_MODEL`，没设才是 deepseek-flash。不能看的模型得到 `model <id> does not support vision`，图片不会悄悄丢掉。能看的模型收到 `content` 数组：一块 `text`，一块 `image_url` 数据 URL。同一轮只保留最新一批。一轮最多 4 张、合计 10 MiB。图片字节不进 transcript，这一轮结束才从内存里清掉，下一轮没有。子代理连续调用工具时只留最新一批，在没有工具调用的回复之后全部清掉，再接排队或插话，所以连续或跨五轮各看一张时第五张都能附上。16 位彩色 PNG（类型 2、4、6）按 8 字节每像素估计，16 位灰度按 2 字节；渐进 JPEG 按 12 字节每像素，4032×3024 的手机照片会缩小，不会被拒。`xbot provider add --vision on|off` 写入，省略时保留原值。工具执行里的 panic 变成工具错误，不会把 serve 打崩。确认回调里的 panic 会放开锁，panic 文本先脱敏再按字节截断。HTML 扫描用原文字节上的 ASCII 大小写比较，避免 `İ` 这类变长小写把偏移用错。

评测认证套件没有加新的提示，也没有把这两个工具放进沙箱工具表。假模型用例在 `internal/kernel` 和 `internal/tools` 里，搜索和抓取都用假后端，不访问网络。

假模型回归：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/full` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。本机基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 77.37 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 24843 ns/op，10864 B/op，11 allocs/op（约 177 MB/s，一份重复的小页面）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测。评测认证套件的系统提示没有改；tools Link 的一行摘要也没改，新工具只出现在工具 schema 里。

评审修复这一轮：`1aee130` 上 `HTMLToText(strings.Repeat("İ",200)+"<p>a</p><script>b</script><p>c</p>")` 会 panic `[416:234]`，现在这条回归通过，并留下 `a` 和 `c`、丢掉脚本里的 `b`。工具 panic 收成 `error: tool panic`。结束标签不分大小写、容许空白，全角 `＜／` 也会拆开。`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 76.53 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 18001 ns/op，10864 B/op，11 allocs/op（约 244 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。用户在 deepseek-flash、思考关闭下看过 `view_image` 是对的；网页工具被部署环境的 `198.18.0.0/15` 假 IP 挡住，DuckDuckGo 当时返回 0 条（非 200 的挑战页）。Not run: 这一轮没有再打真模型，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`b1aee62`）：DeepSeek 搜索改成官方 `POST /v1/tools/search`，正文只有 `query` 和 `count`。Responses API、`deepseek-v4-pro` 搜索模型、`search.model` 和散文回退都去掉。402 报余额不足，429 报频率。密钥只用 `DEEPSEEK_API_KEY` 或显式 `api_key_env`，不回落到 `XBOT_API_KEY`；供应商密钥只发往 `https://api.deepseek.com`。`web.json` 和 `providers.json` 要确认卡。启动之后才写上的搜索密钥，配置工具时再收进内存并脱敏。不可信围栏的替换改成 `[/untrusted_event]`，不再是还能被认成结束标签的 `</ untrusted_event`；实体、零宽字符和多出来的斜线、下划线、尖括号也拆开。确认回调的锁用 `defer` 放开，panic 文本先脱敏再截断。DuckDuckGo 的报错只让模型告诉用户，查询词回显不当成挑战页。看图读不出宽高就拒绝，解码按位深限制并串行，缩小后再对 10 MiB。`::ffff:127.0.0.0/104` 不能写进出站白名单。Responses API 的注释偏移随那个解析器一起删掉。子代理工具仍不经过 `reviewedTool`。

假模型回归（Search API 这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 82.58 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 17347 ns/op，10864 B/op，11 allocs/op（253.64 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`3f8db10`）：看图的解码字节上限只加在真正要缩小的那一路，一张位图按 4096×4096×4 估计；2048×1536 原样通过，2560×1440 和 3000×1200 会缩小，16 位 8000×4000 拒绝。WebP 不解码，不占这份额。工具图片留在这一轮剩下的每一步请求里，仍是 4 张、合计 10 MiB，这一轮结束才清掉，不进 transcript，下一轮没有。搜索密钥在 `providers.json` 读失败，或名字属于启动时的 `KeyEnvNames`、或已经收进内存时，只能发往 `https://api.deepseek.com`。现场只有 `XBOT_API_KEY` 时，用 `"api_key_env":"XBOT_API_KEY"` 显式复用。数字字符引用的前导零和超过一个码位的位数会拆开；零宽连接符、不可见运算符和变体选择符也跳过。收进内存的密钥算进 shell 的保留形式，跨两次写入不会漏出去。脱敏用的密钥列表缓存到下次 Hold 或 Forget。`link.Wrap` 仍记在 issue #34，这次没改。子代理工具仍不经过 `reviewedTool`。

假模型回归（这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 79.46 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 17487 ns/op，10864 B/op，11 allocs/op（251.61 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`89848e0`）：子代理在一轮以没有工具调用的回复结束时清掉图片，发生在排队任务和插话之前。跨五轮各看一张时第五张仍然能附上，第一轮的图片不会留在那次回复之后的请求里。主线同一轮只保留最新一批。上下文估算把图片字节按大约 4 字节一个 token 算进去。16 位彩色 PNG（类型 2、4、6）按 8 字节每像素，16 位灰度按 2 字节；渐进 JPEG 再加一块同样大小的缓冲。搜索密钥算不算供应商密钥，只看 `DEEPSEEK_API_KEY`、`XBOT_API_KEY`、`providers.json` 的 `api_key_env`，以及注册表读失败。已经收进内存的单独搜索密钥仍可以走别的 https 主机。`link.Wrap` 仍是 issue #34。子代理工具仍不经过 `reviewedTool`。

假模型回归（子代理看图这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`、`internal/exec`、`internal/compact`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 90.39 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 19150 ns/op，10864 B/op，11 allocs/op（229.76 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`e818aa7`）：子代理在一串工具调用里每次有新图片就清掉之前消息上的图片，连续五次 `view_image` 时最后一次请求只带第五张。渐进 JPEG 改成按 12 字节每像素估计，解码上限 160 MiB，解码仍一次一张；2560×1440、3000×2000、4032×3024 的真实渐进 JPEG 都缩小通过。回归：`TestSubagentImageStreakKeepsNewest`、`TestProgressiveJPEGPhotoPassesCap`、`TestDecodeCapAllows16BitRGBAAndRefusesOversizeSide`，在 `e818aa7` 上失败。修复由本机 dsh 写，独立评审是 zcode。

## 两层记忆（2026-10-17）

用户决定记忆只留 person 和 bot。person 跨所有 Bot 共享，只放每个 Bot 都需要的、关于用户本人的事实（时区、语言、作息、他是谁），写入必须显式带 scope。bot 是这个 Bot 自己的角色、约定和项目决定，也是默认：省略 scope 的 `remember`、自动抽事实、不可信回合的自动写入都进 bot。工作规则和项目决定不进 person。bot-person 不再存储。怎么做事的冲突以 Bot 自己的记忆为准，压过 person。常驻注入从三份 profile（`3×4KiB+512`，12800 字节）改成两份（`2×4KiB+512`，8704 字节）。

迁移每个数据根每进程走一次（`sync.Once`）。`serve` 和 `chat` 打开内核时触发，Remember、Recall、Forget 也会触发，但同一进程里不会重复走。只合并该 Bot `owners` 里的人：Bot 记忆每个发送者都看得见。其他人的目录改名为 `memory.bak`，不并进去。某个目录读失败就记日志、跳过、留在原地，不让别的 Bot 的读写失败，也不让启动失败。合并去重，保留时间戳、来源和原来的 kind（空 kind 不改写成 work）。profile 放不下的改记 log，并写一条 `migration-profile-cap`。log 超过 64 KiB 时，多出来的写到 `log.overflow.md`，`log.md` 本身不超过 64 KiB；检索和 forget 把这份溢出和日志一起读。已经超限的 profile 或 log 上，缩小文件的 forget 仍然成功。人的同步日志仍是 person 通道。不可信回合的 `remember` 和抽事实，不论落在哪一层，都标 `Untrusted` 并加上 provenance。

网页档案不再读已删除的 bot-person 范围，界面上也不再标「你们之间」。`ScopeBotPerson` 常量还留着，存储层拒绝这个范围。

第二轮：包在 `<untrusted_event source=...>` 里的内容，即使这轮是可信的，抽出来的事实也标不可信。`config.json` 缺失、读不了或坏掉不算没有用户，旧目录留在原地；`owners` 为空才把旧目录改成 `memory.bak`，并记日志。迁日志时 `log.md` 留到大约 48 KiB，新的事实留在文件里。没有持久化的迁移标记：每次进程启动都重新走一遍 `bots/*/people/*/memory`，每个数据根每进程一次；迁过的目录已改名为 `memory.bak`，所以重复走是幂等的。同一个键上，最新的那条赢；出处只做标注，不改变取舍。来源后缀在长度检查之后补上，单条仍不超过 1 KiB。

空数据根上 `Migrate` 只做一次目录读取：844.3 ns/op。已有一个 Bot、没有旧的人目录时 7849 ns/op（amd64，`-benchtime=200ms`，一次性基准，没有提交进仓库）。都低于本机进程间通信 5 毫秒。不涉及界面帧率。

评审修复后再跑：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/m33-{1,2,3}` 跑 `go test -count=1 ./...` 三遍。第 1、3 遍全绿。第 2 遍只有 `internal/browser` 的 `TestReleaseTiedLeavesCLI` 失败（`tied chrome still running`，2 秒没等到进程退出）；单独再跑这条和 `TestStopChecksCommandLine` 通过。`TestStopChecksCommandLine`（issue #27）这三遍没有失败。`memory-scope` 的检查放宽成子串 `run the tests` 和 `Asia/Shanghai`。回归：`TestM33UntrustedDefaultScopeRememberIsAttributed`、`TestM33CorruptLegacyIndexDoesNotBreakOtherBots`、`TestMigrateCorruptOwnerStaysAndOtherBotWorks`、`TestMigrateLeavesNonOwnerInBackup`、`TestMigrateLogStaysWithinCap`、`TestForgetOnOverCapLogSucceeds`。Not run: 真模型评测，界面浏览器手点（网页只删了 bot-person 的读取和标签），`-race`，`go test -fuzz`，默认 `TMPDIR`。

## 记忆 v2（2026-10-17 定）
规格见 [MEMORY-V2.md](MEMORY-V2.md)，决策见 [ADR 0008](adr/0008-memory-v2-dated-log-first.md)、[ADR 0009](adr/0009-memory-as-folder.md)。批 1–5 已落地；批 6、7 未做，排期见候选清单。

| 批 | 内容 | 状态 |
|---|---|---|
| 1 | 快照加差异：会话开始和压缩时拍快照（含最近 8 条日志），中途只追加差异，自己的改动不追加 | 已完成 |
| 2 | 目录布局（`memory.md`、`log/YYYY-MM.md`）和迁移 | 已完成 |
| 3 | 记忆路径上的文件工具把关；拿掉 `remember`/`forget` | 已完成（工具已从内核移除） |
| 4 | 只读的 `recall`（含自由文件） | 已完成（`internal/kernel/recall.go`） |
| 5 | 经过摘要 | 已完成（压缩时写 `episode`，`internal/kernel/extract.go`） |
| 6 | 每晚整理（含索引核对、工作档案） | 未做 |
| 7 | 导入 Grok 导出、`xbot backup` | 未做 |

## 撤下的提示类改动（v0.3.0 → v0.4.0）
v0.3.0 只发内核修复。下面三项提示类改动做完、过了 review，但都拉低了 DeepSeek 的 give-up 用例（向一个死端点 POST，应当如实报告做不到，最多 4 次工具调用），已撤回。重做的顺序在候选清单，不在这里排。

- **模糊任务规则**（「一两句计划后同轮开工」）。加上后全量 eval 里 give-up 连续 0/3、1/3、1/3，三轮改措辞没稳住：模型在目标已写明的任务前先翻目录、补细节，超出工具上限。
- **知识问题三档规则**，连同 knowledge-public / knowledge-project / knowledge-fresh 三条用例和 eval 的 `no_tool_calls` 检查。
- **交互回复默认低推理强度**（profile `reasoning_effort`）。

give-up 单测数据（每个构建 6 次，2026-10-17）：7e64ec6 6/6；加上内核修复、没有知识规则 6/6；再加知识规则 5/6；再加低推理强度 4/6。拿掉模糊任务规则后的全量 eval 两遍都只有 give-up 1/3 不过。证据：`/workspace/xbot-test/weekly/v030/`（evalB/D/E/G/H、gu-*.log）。

另：第 9 项（中文回复前的英文开场白；`xbot provider add/list/remove`；`xbot new` 拒绝未知供应商）也留到下个版本。

成绩单已落地：`xbot eval` 的报告带四维成绩单（能力、意愿、可靠性 pass^k、守规矩一票否决，另报 Ask-F1），`xbot eval scorecard <report.json>` 可从旧报告重算。规格见 [SCORECARD.md](SCORECARD.md)，实现见 [atlas/providers.md](atlas/providers.md)。

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
| F2 | 进行中。A：按轮状态挪进 `turn` 结构（完成）。B：内核分叉机制（完成，`Kernel.AllowFork` 开关默认关）：用户插话时把正在跑的一轮登记成 `bg_` 后台（最多 3 个），写 `fork` / `fork_end` 标记和 `agents/<id>/meta.json`，前台提示列出全部在跑的后台，结束时往收件箱投 `fork_end` 事件；崩溃修复能处理多个分支各自末尾的未答调用。C：`serve` 的网页路径已打开分叉（完成）：每个 Bot 一个长期内核，进程内的请求共享处理锁，锁一直拿到所有后台结束；插话作为前台走 SSE。盒子回归：连发 5 句插话，响应 0.01–0.12 秒；后台跑完写 `fork_end`，完成事件由 `serve` 处理并写进通知。F2 完成，遗留见下 |
| F3 | 完成（除前台硬上限，见下）：后台确认走卡片给用户（`BackgroundApprove`）；后台 `steer`/`queue`/`interrupt`/`cancel`，控制消息有 `pending`/`applied` 状态，下一步生效；资源冲突排队（同文件、同目录、同仓库的前台写操作排到对应后台之后，其余并行，后台上限 3）；队列上限 5，排队项带原因和位置、可取消，满了回「队列已满」；后台在跑时网页进程自己处理收件箱里的定时器等事件；删除或改名 Bot 时清理锁记录并取消后台；`GET /backgrounds` 给 F5 的角标和弹层用。盒子回归（假模型）：两个后台并行、插话 0.03 秒回复、steer 下一步生效、冲突的写操作排到对应后台之后、另一个后台的 `fork_end` 在后台还在跑时被处理，两个后台都以 done 结束 |
| F4 | 完成：前台硬上限（4 次工具或 20 秒，可配置；到了就转成后台，槽位满时留在前台）；重启时把有 `fork` 没 `fork_end` 的后台补成 `interrupted`，更新 `meta.json`，只告诉用户一次，不自动续跑；取消路径写 `cancelled`、杀掉正在跑的命令。盒子回归（假模型）：4 次工具后转后台并发提示；20 秒转后台后 `kill -9`，重启后补写 interrupted、事件只投一次、再重启不重复；取消后状态 cancelled、`sleep` 进程已结束 |
| F5 | 完成：按原型做了顶栏「后台 N」角标（只数运行中，没有运行和排队时隐藏）和弹层：运行中（步数、最近工具、追加指示、取消，steer 显示待生效/已生效/已丢弃）、排队（原因、文件名、位置，可取消）、最近结束 3 个；手机是底部弹层。聊天里没有后台卡片，后台的回复和确认卡带「后台」来源标，控制消息和 `fork_end` 事件不显示；插话被拒（429）时显示「队列已满，请稍后再发。」。截图在 `xbot-test/f5/shots/`。验收（假模型）：5 次插话延迟最多 0.02 秒；后台耗时 16.2 秒，基线 16.1 秒（+0.6%）；2 个后台并行加 5 次插话，各 +0.6% 和 +1.9% |

F3/F4 遗留：前台不限制只读工具和单次 10 秒；槽位满时到上限的一轮留在前台；后台之间不按资源互相排队；只读 shell 的判断是一张短名单。

验收：连发 5 句每句 p95 3 秒内回复，后台耗时增加不超过 10%，并测 2 个后台并行。

## M6 切片状态
M6 每片的状态。已验证的是回归对过的；已接上的是代码里有、还没在回归里点过的。托管连接器市场不做。

1. **精选目录和安装**。已接上。一份内置清单，每条标明类型（原生或 MCP）、能力（工具 / 入站事件）、需要的密钥名。已经有运行时的条目可以装到某个 Bot 上，就是给它加上对应的 Link 授权；还没有运行时的条目照样列出来，安装会被拒绝。默认不因为这份清单多装任何东西。
2. **MCP 适配器**。已接上。stdio 与 Streamable HTTP 都把服务器包成这个 Bot 的工具。服务器记在该 Bot 的 `mcp.json`，密钥只记环境变量名，供应商 API key 不传。入站事件可选。不从精选目录一键安装。`xbot connector list/add/remove` 用打进二进制的 JSON 目录，按 Bot 启用，没有托管市场。
3. **连接器**。已接上。Telegram、飞书装上之后，外面的消息变成不可信 Event 进收件箱。代表用户发出去的消息先留草稿，用户确认后才发出。密钥只在确认卡或同名环境变量里填写。
4. **编码代理任务**。已接上。和子代理分开的任务记录：工作目录、仓库、步数上限、时间上限。可以开始、补充、排队、打断、取消。完成、失败或取消时向该 Bot 的收件箱投 Event。
5. **编码代理后端**。已接上。同一份任务记录接到外部命令、`xbot runner` 或本机模型，内核不加新对象。`code_start` 会改文件，但按权限表属于普通动作，不弹确认卡。`XBOT_CODE_AGENT` 优先，否则用 PATH 里的 `cursor-agent`、`codex` 或 `claude`。输出按行流进 transcript 的编码卡片。`code_show` 能看已经流出来的输出，`code_cancel` 停掉进程。仓库非空且目录里还没有 `.git` 时，先 `git clone`。都没有后端时仍只写记录。
6. **手机后台推送**。已验证。每个 owner 一份 VAPID Web Push 订阅，写在数据根的 `push/subs/<owner>.json`（0600），密钥在 `push/vapid.json`（0600）。网页「连接应用」里开启或关掉。订阅地址是公网 https；环回上的 http 只留给本机测试。内网、环回、链路本地、运营商级 NAT 和未指定地址拒绝。主机名解析失败就拒绝，不放行。十进制或十六进制整数主机（例如 `2130706433`）拒绝。地址还要是全球单播，并排除 IANA 专用段，包括运营商级 NAT、基准测试 `198.18.0.0/15`、文档和保留地址，以及 NAT64、6to4 和 Teredo。真正拨号时再用套接字 Control 核对解析出来的地址，避免检查之后被重新绑定到内网。浏览器页面还连着（正在拉接口，或有一条没结束的对话）时，普通通知仍走页面里原来的提示，不发 Web Push。没有标签页连着，并且这条 note 已经写进出件箱时，才按订阅推到手机。免打扰时段不推：时段内 note 只进 held，不写出件箱；即便出件箱里已经有一条，只要这时还在免打扰，后台推送也停。时段结束后合成的那一条写进出件箱，记录上带批量标记，不靠正文开头那几个字来认。页面拉得到。只有页面正有一条没结束的请求时才不推送；仅仅是最近 12 秒有过接口调用时，这条批量通知仍推到手机，避免两边都没人收。页面里的通知列表和提示保持原样。
7. **人的记忆同步**（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）。已验证。人的身份追加在 `people/<id>/identity.jsonl`，人的记忆追加在 `people/<id>/memory/sync.jsonl`。每条记下时间、写下的 Bot、设备。设备用数据根里已有的 `node-id`，没有就记 `local`。`xbot node add` 登记的节点之间自动同步：`xbot serve` 和 `xbot node serve` 每隔 30 秒，以及本机写入或从别的节点合并进新内容之后，用该节点的令牌调用对方的 `sync` 能力，把两份日志换过来再合并。同一条以时间戳较新的为准；时间相同留本机已有的。晚于现在两分钟的时间戳合并时改到这个上限。不合格式的条目（层级、控制字符、长度）丢掉。同步来的账号留在日志里，不授予本机用户身份；用户账号、显示名和时区只由本机操作更换。`xbot person unlink [--force] <link:id>` 去掉一个已关联账号。最后一个本机 `terminal:uid:` 账号要加 `--force`，并先打出警告。记忆放进模型上下文时包在闭合的 memory 块里。不可信记忆的正文、键和设备名里的 `<`、`>` 以及全角和形近尖括号一律转义，块内不能再出现标签。从别的节点来的事实按不可信数据处理，并保留写下的人和设备。只有送达这条记录的已认证对端自己就是所声称的设备，并且该设备已在本机登记，才记成 owner-recorded，仍然是数据，不是指令。经别的节点转发的，即使设备名对得上，也仍是 untrusted。`xbot sync export` 和 `xbot sync merge` 仍可手工导出、合并同一份日志。Bot 目录不改，Bot 仍留在自己的家节点。这一跳走现有的节点调用，单次请求和响应各不超过 1 MiB；装不下时这次同步报错，不截断。
8. **远程隧道**。已验证 Cloudflare 这条路径：`cloudflared` 的地址从 stderr 读出，进程组跟着 `serve` 结束，启动它的 goroutine 锁住操作系统线程直到关闭。公网能不能访问到这个地址，这次没有测。
9. **多节点**（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）。已验证。`xbot node serve` 用 bearer token 公布本机身份，`node add` 先核对 token 再写入。`shell` 不信任调用方传来的 `approved`。`ask` 把命令留在本机，十分钟过期，最多 32 条，`node pending` 列出，`node approve` 先显示命令再问 `y/N`。
10. **机器开关**。已验证。每台机器 `allow`、`ask` 或 `off`，统一在核心的 review 路径处理本机工具。`allow` 时 `shell` 只对删除和对外发送确认，普通写入直接落盘，本机读取不问；`ask` 每次本机动作都要确认；`off` 拒绝 `shell`、`read_file`、`write_file`、`edit_file`、桌面和浏览器动作；`browser_stop` 仍可用来关掉进程。写自己的 persona/profile 文件在 `allow` 下仍确认。
11. **桌面操作**。已验证。截图、点击、输入、滚动、按键。确认卡写明坐标、原文、按键，以及滚动的方向、步数和位置。截图权限 0600。
12. **浏览器**。已验证。CDP 驱动单独配置目录里的 Chrome。命令行拉起的进程留到 `browser stop`；`serve` 只停自己拉起的那一份，用进程组，不用 `Pdeathsig`。`session.json` 里的 pid 对不上就不杀，删掉文件，退出码 0。

用户技能加在已有的 `skills` Link 上，不单列一片，也不替换上面的连接器和编码代理。

## 本地优先桌面端（2026-10-17 定，代码已交付）

用户决定：XBot 装在用户自己的电脑上，Windows 和 Mac 同时做。`serve` 跟着桌面端在本机跑，所有 Bot 数据都在本机，窗口显示本机 `serve` 的现有 Web 界面。设计决定见 [ADR 0007](adr/0007-local-first-desktop-app.md)。

**完成的样子**：用户在 Windows 和 Mac 上各装一个，打开就能直接用本机的 Bot，连续用一周不想换回别的。

**已定的决定**：
- Windows 要求装 Git for Windows，shell 工具用 Git Bash（`bash -c` 语义，权限表规则不变）。
- cron 按电脑的本地时区解释，不再按 UTC；已有例程升级时迁移，触发时刻不变。
- 关窗口就退出（不做托盘和开机自启）。错过的例程不补跑：下次启动时每个 Bot 收到一条汇总通知，列出错过的时间点，由 Bot 自己决定。

**摸底结论（`d6a90a3`）**：`darwin/arm64`、`darwin/amd64` 的 `CGO_ENABLED=0 go build ./...` 通过，`go vet` 只挂在一处测试代码；`windows/amd64` 编译不过（`fsutil`、`kernel` 的 `flock`，`browser`、`tunnel` 的 `kill` 和进程组，`kernel/project.go` 的 `x/sys/unix`）。编过以后，Windows 上还有三处挡启动或挡核心功能：保险箱密钥的权限检查（Windows 上 0600 读回来是 0666，第二次启动所有 Bot 都起不来）、目录 fsync（对话分段封存失败）、shell 写死 `sh -c`。MyGo 外壳的最小窗口能从 Linux 交叉编译出 Windows 和 mac 版本（每个约 9 MB）；在盒子的 WebKitGTK 里指向本机 `serve`，界面、登录、重开免登录都正常。

**怎么做的**：每个 PR 约 500 行以内，测试三遍全绿、触及包跑 `-race`、每个修复带一条在旧代码上会失败的回归测试、没跑的列成 Not run。

### 三线交付内容
A 线（跨平台核心）：文件锁抽象（unix `flock`、Windows `LockFileEx`）、进程控制（Windows Job Object 杀进程树、mac `sysctl` 取启动时间）、Windows shell 走 Git Bash、Windows 存储语义（密钥权限、目录 fsync、原子写重试、只读分段、ID 大小写不敏感）、Windows 重启与升级（`.old` 改名替换）、浏览器按平台找 Chrome、编辑工具保 CRLF 与子进程 `HOME`/`USERPROFILE`。

B 线（桌面外壳）：`internal/appshell` 与 `xbot app`（同进程起 `serve`，固定 `127.0.0.1:8787`，单实例，一次性启动码自动登录，日志不含 token）、界面适配（标题栏留位与拖动、字体、原生通知、外链走系统浏览器）、打包（GitHub Actions 出 mac `.app`/dmg、Windows 安装器、Linux `.deb`，附 SHA256SUMS；`scripts/package-desktop.sh`、`build-macos-dmg.sh`、`test-windows-installer.ps1`）、应用内自更新（复用 `internal/upgrade`，mac 整体替换 `.app`，Windows 用 `.old`）。

C 线（定时任务）：cron 按本地时区解释并迁移已有例程（触发时刻不变）、错过时间点的汇总通知（水位线不进例程目录，停机或睡眠超 2 分钟发一条，ID 可去重，最多看 7 天）。

**当时的顺序**：A1 → C1 → C2 → B1 → B2 → B3，到这里 mac 能装；接着 A4 → A3 → A2 → A5，Windows 能用；最后 B4、A6、A7。A1 与 C 线并行。

**真机验过的**：还没逐项确认。这些只能在用户的真机上验：WebView2 和 WKWebView 的实际渲染、字体、标题栏、拖动和吸附、原生通知；Windows 上 Git Bash、Job Object、`LockFileEx` 的强制锁、目录 fsync、rename 共享冲突、只读文件删除；dmg 和 Windows 安装器、Gatekeeper 和 SmartScreen 放行；mac 整体替换 `.app` 和 Windows `.old` 的自更新；合盖一晚后的汇总通知。

**盒子和 CI 上验过的**：交叉编译和 `go vet`、Linux 窗口（WebKitGTK）、单元测试；GitHub Actions 上跑 Windows 和 macOS 的全量 `go test`（`ci.yml`）；`installer.yml` 在 PR 上构建 Windows 安装器并在 Windows runner 上装、升、卸，`release.yml` 打 tag 时跑同样的检查。桌面端截图的提交在 `desktop-shots` 系列里。

## 测试与陪练
Bot 复刻会在自己的电脑上长期运行 XBot，像真实用户一样使用它，发现问题后整理成复现步骤交给编码代理修复，修复后再做回归测试。

## C1：通用慢工具后台化（2026-10-23）

沿用 `tools.Jobs`，把 shell 的等待裁决扩到实际工具调用：等待片 2 秒或用户插话后只交还前台等待，运行不重试、不取消；完成统一经原 Inbox 回报。删除 shell 的 sleep/poll 例外、忙闲双通知与 claim 账。工具 Job 不占子代理 3+8，保留原输出、取消与回收能力；runner 同步接入，目录、图片、记忆写守卫和已发生副作用仍在实际完成时整理。

假模型六场景取消慢工具豁免后通过，10 个 B 全部受理，最终派出 p95 约 373 毫秒；新增回归在旧代码上因慢工具/记忆写锁阻塞失败。快调用含登记及结果整理实测均值 0.62 毫秒（300 次，本机文件系统），不是 IPC 或真实模型 p95。真实模型与用户日用未测。最终门禁、审阅修订及提交号见仓库外 `~/.xbot-build/env-baseline-c1/commit2.md`；现行规则见 [内核](atlas/kernel.md)「慢工具后台化」。

## 多 Provider 批 1（2026-10-23，本地分支）

实现提交 `54cfade`：消息正文改为有序 `ContentPart`，沿原 transcript、索引、blob、导出和投影链路读写。旧串在读取时转成块，已分段的旧行不重写；Completions 请求保留原有行为。跨块脱敏覆盖写入与 JSONL 导出，压缩保留未知块的可读摘要。现状见 [providers](atlas/providers.md) 与 [context](atlas/context.md)。

验证：gofmt 无输出，vet、全量测试、相关包三遍、build 通过；末轮修改的四包另三遍通过。最终 `codex review --base 860eaf3` 无明确可操作缺陷。六条假模型 smoke 首轮全过，312.511 秒；后一次定时唤醒的重启项超时，定向复跑通过（42.590 秒），根因未验证。另一次 Job 通知时序失败及其复跑结果一并保留在报告中，没有改断言或延长超时。

性能（Apple M1）：100 个 256 字节文本块，JSON 读取与 Completions 投影约 0.758 ms/op；4096 个小增量共 64 KiB 的流式组装约 2.377 ms/op。均为本地基准，不代表真实模型或 IPC p95。

级别：本地提交、假模型端到端通过；未合入 main，未 push，用户日用未验证。Not run: Linux CI、真实模型认证、浏览器视觉检查、全量 race。完整命令、失败记录、文件清单与提交号见 `~/.xbot-build/multi-provider/commit-b1.md`。本批未实现后续协议、hosted 或 UI。

## 多 Provider 批 2（2026-10-23，本地分支）

`feature/multi-provider-b2` 已实现 Responses、模型能力解析、按绑定互斥搜索，以及最小换绑定和 hosted 引用界面。现状见 [供应商](atlas/providers.md) 与 [界面](atlas/ui.md)。实现提交 `acb0af0`，换绑任务保护、流错误分类和引用修复 `0c4897d`；后续收尾提交见分支记录。未合入 main，未 push，未发版。

假模型端到端通过：Completions 六条基础旅程与既有 smoke 共 362.116 秒；Responses 专项覆盖原生请求、内容块、引用、Jobs 边界和换回 Completions，修复后再次通过。Chrome 已走换绑、搜索进行中/完成、引用列表、能力表和 OAuth 禁用态。没有真模型或真实 GPT 账号录制验收，没有用户日用验收。

最终全量检查未通过：状态接口 p95 7.170792 ms，超过 5 ms 门槛，其余包通过。web 包独立三轮通过；状态接口 p95 为 1.050625 / 2.885416 / 1.567959 ms（每轮 500 次）。忙时替身链路 p95 382.269 ms，批量消息 p95 371.717 ms。期间还出现过插话重复回复、临时目录清理及 Chrome watchdog 失败，根因未确认；失败记录与最终命令结果保留在仓外 `~/.xbot-build/multi-provider/commit-b2.md` 和 `b2-evidence/`。未测真实模型延迟、IPC 额外开销、后台变慢比例或 FPS。

## 设置·设备与配对嵌层（2026-10-23）

设备页只留标题行的「+ 配对设备」，删除底部双按钮。配对同屏展示本机码和对方码输入，点码复制、图标换码，下一步空输入禁用；继续使用现有 code/join API。远端对话深链进入设置·设备。

本地浏览器行为测试及 web 包三遍、全量 `go test -count=1 ./...`、`go build ./...`、`go vet ./...` 通过，`gofmt -l .` 无输出。浅色、深色三种状态已查看；网关响应和剪贴板使用替身，双机实配、性能数字和用户日用未测。级别：本地分支验收，未合入、未发布。证据在仓库外 `~/.xbot-build/devices-pair-ev/commit.md`。

### 网页出站去掉地址白名单（2026-10-25）

mihomo、Clash 的 fake-ip 把 `api.deepseek.com` 解析成 `198.18.x`，旧出站检查因「非公网」拒绝拨号，网页搜索「试一下」报「密钥无效或连不上」。现在只拒绝回环、未指定、链路本地（含云元数据）和组播，其余地址都拨；`trusted_egress_cidrs` 删除，旧字段读入忽略。取舍见 [ADR 0022](adr/0022-web-egress-floor.md)。假 IP 解析下 `web_fetch` 和 DeepSeek 搜索探测的回归在旧代码上失败（证据在仓库外 `~/.xbot-build/egress-guard/old-code-regression.txt`）。`internal/tools`、`internal/web`、`internal/config`、`internal/kernel` 通过。级别：本地分支验收，未合入、未发布。

### 设置·技能（2026-10-25）

设置浮层新增「技能」（网页搜索与设备之间）：一层是用户技能库清单，带搜索和唯一主按钮「导入」；二层按 Bot 开关，关只影响该 Bot。导入由浏览器选文件夹，同名先问再覆盖；新增库级 `GET /api/skills`、`GET /api/skills/{slug}`、`POST /api/skills/import`，开关复用每个 Bot 的 enable/disable。现状见 [界面](atlas/ui.md#技能设置)。

其后合入四源导入（Agent / Codex / Claude / 本机文件夹）；设计师用户视角 PASS（tip `a02e3f3`，祖先 `3803f3d`）；`docs/dev/skills-import-sources.md` 已毕业删除。未 push。

本地 Chrome 走查了列表、搜索、无匹配、空态、二层开关、导入、同名覆盖/取消、错误和深浅色、窄屏；`go test` 覆盖库级接口、导入覆盖与并发同名。级别：本地分支验收，未合入、未发布，用户日用未测。证据在仓库外 `~/.xbot-build/skills-settings/`。

### 输入框 URL 高亮可点（2026-10-26）

对话输入框从 textarea 改为纯文本 contenteditable：草稿里的 `http(s)://` 地址就地高亮，点击走气泡链接同款 `target=_blank` + `noopener`（桌面壳复用离域导航 → OpenExternal），点击不触发发送；Enter 发送、Shift+Enter 换行、IME 选词回车不发送均保持。只认可解析为 http/https 的地址，尾随标点与紧贴中文不吞进链接，纯文本上限 16000 照旧，粘贴一律转纯文本，图片粘贴/拖拽流程不变。规格 [docs/dev/composer-url-links.md](dev/composer-url-links.md)，现状已提炼进 [界面](atlas/ui.md)。

验证：node 纯函数单测（识别/协议白名单/尾标点/序列化/偏移）+ 点击不提交 form 的 node 单测 + headless Chrome 端到端（高亮、点击打开不发送、非 http(s) 不亮、Enter/Shift+Enter/IME、16000 上限）全过；`go test -count=1 ./...` 全过，web 包 `-count=3` 过，gofmt/vet 净。浅色/深色截图已在本地 Chrome 查看。级别：本地分支端到端通过，未合入 main、未 push，用户日用未验证。

### 草稿 Markdown 高亮 + 气泡数学（2026-10-26，同一候选第二批）

在 composer URL 高亮分支上续作：输入框在 URL 高亮之上加 Markdown 语法上色（行首 `#`/`##`/`###`、列表 `- `/`* `、`` `code` ``、`**粗**`、`*斜*`、`[text](url)`，全部保留源字符、非 WYSIWYG，`[text](url)` 沿用裸 URL 的 http(s) 白名单与 `_blank`+`noopener` 点击路径）；消息气泡（流式与 transcript 同路径）用内置 KaTeX 0.16.11 渲染 `$…$` 与 `$$…$$`，TeX 源从不作为 HTML 进 DOM，库缺失或渲染失败回退原文；金额 `$5 和 $6`、代码段内的 `$` 不误判；sw SHELL 加 KaTeX CSS/JS（缓存 v15），字体仅 woff2 运行时缓存。规格更新见 [docs/dev/composer-url-links.md](dev/composer-url-links.md)，现状已提炼进 [界面](atlas/ui.md)。

验证：新增 node 单测（草稿高亮 7 组断言、数学 13 组断言含回退）与 headless Chrome 端到端（草稿高亮/点链接不发送/Enter 发送源码、行内+块级公式/KaTeX CSS 接线/回退/未闭合流式/持久化重渲染）全过；既有 composer links 三测试不回退照过；`go test -count=1 ./...` 46 包全过，gofmt/vet 净。浅色/深色截图本地 Chrome 查看：公式排版、草稿各色、金额不误判均确认。KaTeX 体积 gz 约 76 KB JS + 3.5 KB CSS + 296 KB 字体（woff2 运行时缓存）。级别：本地分支端到端通过，未合入 main、未 push，用户日用未验证。

### composer/气泡数学复审一修（2026-10-26，同一候选第三批）

复审 1（仓外 `~/.xbot-build/composer-url/review1.md`）五个问题全修，范围不出该报告：

- **上限拒绝不截尾（R1 P1）**：16000 上限从 renderComposer 事后 `slice` 改回旧 textarea 的 maxlength 语义——超限的用户编辑（中间插字、选区替换、粘贴、拖入、整段替换）在编辑入口整体拒绝并恢复光标，未被选中的已有草稿一字不丢；程序化写入不再截断。
- **撤销/重做（R2 P2）**：每次 input 全量 replaceChildren 弄死原生撤销，改为页面自维护 composerHistory（每接受一步编辑一步，上限 200；⌘/Ctrl+Z、⌘/Ctrl+Shift+Z、Ctrl+Y，菜单撤销/重做经 beforeinput 拦截）；发送清空、草稿恢复、失败回填等程序化写入重置基线，同旧 textarea 赋值。
- **块级数学配对（R3 P2）**：开块行与闭合行都改按首个闭合 `$$` 切分并继续处理尾部：`$$a$$ 和 $$b$$` 两段公式加中间正文，多行闭合行尾再跟公式照切。
- **真实解析失败回退（R4 P2）**：KaTeX 改以 `throwOnError:true` 调用，非法 TeX 抛错进 catch 回退原文，不再以 `.katex-error` 蒙混；node 测试加载仓内真实 KaTeX 回归。
- **回退保留源定界符（R5 P2）**：mathNode 的源串与 displayMode 分开传，句中 `$$…$$` 缺库/抛错回退仍是 `$$…$$`，不改写成 `$…$`。

验证：新增 `TestComposerNativeEditBrowser`（headless Chrome，真实 `execCommand` 输入/删除 + ⌘/Ctrl+Z/⇧Z 撤销重做 + 上限处原生 insertText 断言）、`TestBubbleMathRendering` 加真实 KaTeX/非法 TeX 与配对/回退断言、`TestBubbleMathBrowser` 加真实解析失败与句中 `$$` 回退段；四个测试在旧 HEAD（6595c80）上逐一失败确认。`go test ./internal/web/ -count=1` 与 `-count=3` 过，gofmt/vet 净，`go build ./...` 过。级别：本地分支端到端通过，未合入 main、未 push，用户日用未验证。复审报告与证据在仓外 `~/.xbot-build/composer-url/`。

### 推翻前台后台 · 删限制（2026-10-26，分支 `feature/drop-foreground-bg`，未推）

按 [ADR 0028](adr/0028-no-foreground-background-split.md) 整块删掉前台闸：`foreground.go` 全文（`foregroundHidden`/`foregroundKept`/`workClass`、`foregroundTools`/`foregroundRefuses`/`errForeground`、`foregroundCap`/`cappedTools`/`capTools`/`capNote`/`capGrace`/`<foreground_limit>`）、`StopForegroundLimit`、turn 的 `capped`/`capSteps`/`capBaseTools`/`capFrom`/`capFork`、`freshAllowanceLocked`；`AllowFork` 下「超前台上限自动转后台」（`foregroundCapped`/`capNotice`）一并删，插话分叉保留。`ForegroundMaxTools`/`ForegroundMaxTime` 从 Kernel、role 默认、`Config`、`ProfileUpdate`、`profile set` 参数里删除。`DispatchRule` 改写为「先回人再干活；短活自己干，长活 dispatch」，旧措辞留 `legacyDispatchRule` 只供迁移识别。

验证：新测 `TestMainLineDoesTheWork`、`TestMainLineAndSubagentsReachTheMountedTools`、`TestLongTurnStaysOnMainLine` 在旧代码（71f1ac9）上均失败；`go test -count=1 ./...` 46 包全过，kernel/bot/cmd/xbot/eval `-count=3` 过，gofmt/vet 净。级别：已合入分支（未合 main、未推）。Not run：真模型 `evals/dispatch/*`（新规则下模型可能自己做短活，期望值需用户用真 DeepSeek 再认证）；浏览器手测。

### 思考回传与启用（2026-10-26，本地分支）

`feature/thinking-enable` 续接原未提交实现：Completions 保存并多轮回传 reasoning_content，DeepSeek 默认启用；显式 on 真启用 Anthropic thinking 预算与 Responses 原生 reasoning。默认思考档 low，配置和 CLI 支持 low/high/max，runner 保留档位。思考只在内部 transcript 保存、脱敏和回放，SSE/公开历史/气泡均不展示。现状见 [供应商](atlas/providers.md)，后续验收见 [开发设计](dev/thinking-enable.md)。

最后一轮 `go test -count=1 ./...`、vet、build、gofmt 与 diff 检查通过；六条基础旅程加三协议思考专项 smoke 通过（283.327 秒），本机 Chrome、真实 CLI 档位校验和 provider 思考专项 race 通过。相关七包三遍已跑，web 插话重复回答与 kernel shell 通知时序存在失败，两者均在隔离的旧提交 9046089 上复现；首轮全量另有长程回忆失败，末轮通过，旧代码长程三遍通过，原因未完整闭环，未修改验收断言。atlas audit 仍报其它文档的既有告警。完整失败记录和复测见 `~/.xbot-build/thinking-enable/encode-report.md`。

性能：三种协议六次真实进程回复含两次读文件工具，耗时 47.4–115.6 毫秒（scripted 上游）；ContentReplay 基准 0.156 毫秒/op。不是实际模型延迟或 IPC p95。级别：本地分支提交、假模型专项端到端通过；未合入 main、未 push，未重启 8788；真实账号、Linux CI、用户日用、全量 race、FPS 与后台减速未验证。

### 思考回传 fix1：公开导出隔离（2026-10-26，本地分支）

修复 review1 P2-1：`ExportTo/visibleMessage` 共用公开投影，JSONL、Markdown 及 CLI 导出统一移除 reasoning 块、清空其余块的 VendorRaw；保留正文、公开步骤/引用和原工具开关，内部段文件与重启后同协议回放完整。现状见 [上下文](atlas/context.md#怎么运转)。

三协议 × tools=0/1 × 两种格式的 HTTP 回归在旧实现上失败，修复后通过；Export/Thinking/Reasoning 相关五包专项三遍、bot/web 整包三遍、导出专项 race、三协议“导出后重启再续轮”冒烟及 vet 通过。全量仅既有 `TestInterjectionGoesStraightIntoTheTurn` 因重复回答失败，未修改该测试或调度逻辑；gofmt/diff 检查净，atlas audit 仍报既有文档告警。专项六次请求含读文件及四次导出检查累计 34.7–59.0 毫秒（scripted 上游，小样本，非真实模型或 IPC p95）。级别：本地分支修复、假模型专项端到端通过，未合 main、未 push、未重启 8788；报告见 `~/.xbot-build/thinking-enable/fix1-report.md`。

### Bot 自装技能通路（2026-10-26，本地分支）

`feature/bot-self-skill` 按 [开发设计](dev/bot-self-skill.md) 为内置 dev 类型加入 `read_skill` / `write_skill`；catalog、grant 和工具描述统一指向本 Bot 用户的本机技能库，删除 shared 和写盘前强制批准的误导。主线继续沿用 ADR 0028 与 skills lazy load，写入仍走 `review.Normal` 和原 `skill.Write`，未改权限、校验或落盘逻辑。现状同步到 [生态](atlas/ecosystem.md) 和 [子代理](atlas/subagents.md)。

验证：新增 scripted 内核回归同时启用 Dispatch、agents grant 与权限检查，断言 load 前不可写、load 后主线包含读写工具，并实际写入测试 Bot Root 下的 owner 库再读回。文案、dev 白名单回归在 f22b708 旧源码覆盖下失败，主线可达回归在该基线已通过（此前 ADR 0028 已打通）。指定五包 `go test ./internal/kernel/ ./internal/link/ ./internal/agenttype/ ./internal/skill/ ./internal/review/`、`go test -count=1 ./...`、`go vet ./...`、gofmt 与 diff 检查通过；atlas audit 已跑，仓内其它文档告警未在本批扩改。证据：`~/.xbot-build/bot-self-skill/`。

验证耗时：指定五包中的 kernel 为 34.261 秒，全量中的 kernel 为 48.838 秒；这是测试运行时间，不是产品性能数据。本批未测交互/IPC p95 或后台减速。级别：本地功能分支提交待复审；未合入 main、未 push，Bot 自装技能的真实入口端到端与用户日用待验。Not run: `-count=3`（按任务要求）、真模型/真实对话安装、Linux CI、性能专项。

### 生图体验热修、头像即时刷新与开发 tip（2026-10-26，本地分支）

`feature/imggen-ux-hotfix` 补齐生图账号与 Bot 模型绑定的人话说明；助手流式和历史统一剥掉图片语法与残片，保留代码、公式和普通链接；xAI 未登录主钮统一「登录」。`update_profile` 回执改为 `profile saved`，保存结果主动刷新侧栏与顶栏，头像字段进入侧栏缓存签名。开发脚本成功构建时同步短 HEAD 到 build-info 和 xbot.tip。现状见 [界面](atlas/ui.md)、[多 Bot](atlas/multibot.md)、[版本](atlas/release.md)。

开关同步在基线 c91e7dd 已通过本机 Chrome：本批补 class、aria、section.hidden、实际滑块/背景、初始开启、保存失败和重开断言，未修改已同步的 paint/CSS，也不宣称复现了现场失同步。原始 box 证据不可读，全局首个 `.tog` 是隐藏的手机推送开关，原采样是否选错仍待执行官复核。

验证：全量 `go test -count=1 ./...`、`go vet ./...` 通过；config/web/kernel/link 相关单遍通过，修改到的 web/kernel/link 整包三遍通过。六条基础旅程 smoke 通过（280.203 秒）；本机 Chrome、脚本隔离构建回归、gofmt、diff 检查通过。绑家提示、气泡、xAI、头像与 tip 的回归均已验证旧代码失败。atlas audit 与基线对照无新增告警，已有告警保留。证据在 `~/.xbot-build/imggen-ux-hotfix/`，浅深色截图在 `~/.xbot-build/ui-shots/imggen-ux-hotfix*/`。

性能：17,280 字节图片语法过滤，Node v24.22.1 预热后 200 次 p95 0.137 ms；fake 页面工具结果到头像更新为虚拟时钟 10 ms，不能当作真实响应 p95。级别：本地分支自动化与 scripted 基础旅程通过，未合 main、未 push、未重启 8788；真实账号生图、运行 tip 一致性、设计师本批截图验收、用户日用、真实模型/IPC p95、FPS 与后台减速未验证。后续边界（工作区可发现、交付话术、关于页 tip 展示等）见候选清单，不另留开发短记。

### 技能主名与落库默认范围（2026-10-26）

`feature/skills-main-default` 落地：新技能的 `name` 保持 slug，`title` 为可选人读短名；解析、库/来源接口与列表/详情优先 title，旧 name/slug 降级，description 不再作主标题。`write_skill` 支持 title 与显式 scope：self/同 owner 本机单 owner Bot 名（忽略大小写）只开目标，省略/空全开；不扫描 transcript，设置页新导入继续全开，覆盖保留既有开关。

范围默认复用 `EnableSkill` / `DisableSkill` 与 `disabled_skills`；同库自装串行，配置更新在原文件锁内读最新值并原子写入，其它配置保存不覆盖既有开关。已加载内核也读取持久化名单，安装后手动开关立即影响技能列举和读取。现状见生态、界面、多 Bot atlas；设计师用户视角 PASS（tip `a02e3f3`）；`docs/dev/skills-title-and-scope-defaults.md` 已毕业删除。

验证：skill/kernel/web/bot/link/review/cmd/xbot 七包单遍通过，最终 `go test -count=1 ./...` 46 个有测试包全过，`go vet ./...`、gofmt、diff 检查通过；kernel/bot 的范围并发、不同 slug 并发与旧配置保存专项 race 通过。新增/更新 title YAML 读写、旧文件兼容、API、DOM、scripted 工具调用、self/具名/省略/空 scope、非法与跨 owner scope、已加载内核手动开关、并发写入回归；c580239 隔离归档上回归按预期失败（只添加新字段/参数的编译适配，不改变旧行为）。本机 Chrome 真设置入口行为通过，浅深色列表/详情前后截图已保存。atlas audit 已跑，与同一 git 历史下基线相比无新增文档告警，既有告警保留。

性能：本机持久化开关读取基准 10,314 ns/op（约 10.3 微秒/次，200 ms 基准窗口）；不是交互/IPC p95 或 FPS。证据与完整自检：`~/.xbot-build/skills-main-default/`；截图：`~/.xbot-build/ui-shots/skills-main-default/`。

级别：已合本地 main（tip `a02e3f3` / 本切片祖先 `b4fd276`←`18b6989`）；设计师用户视角 PASS；未 push。Not run: 用户日用、Linux CI、交互/IPC p95、FPS 和后台减速。

### 聊天图片单图查看（2026-10-27）

`feature/chat-image-viewer` 把气泡点图后的多图墙替换成单图沉浸层，点击索引直达、同条前后切换、缩放拖动、关闭与焦点／滚动恢复共用现有图片列表。现状见 [界面](atlas/ui.md#聊天图片查看)。docs/dev 交接已删；设计师逛与用户日用仍待验。

本机 Chrome 真鼠标、键盘、滚轮、触屏滑动、捏合、双击、取消手势和减少动态效果通过；真实隔离 `xbot serve` 经登录、paste 上传、发消息、历史、图片查看完整入口通过，模型为 scripted，匿名 paste 请求仍为 401。新浏览器回归三遍通过，在原始 `ce4ebc9` 归档上因仍为多图墙而失败。前后浅深色截图及完整命令证据在 `~/.xbot-build/chat-image-viewer/` 和 `~/.xbot-build/ui-shots/chat-image-viewer/`。

检查未全绿：全量检查遇到原有审批测试 `shell` / `sh` 断言失败并阻塞清理，`internal/work` 原有 shell 测试缺 description；两项在原始提交复现。跳过该阻塞审批用例后，web 整包三遍仅四条已有 parked approval 用例失败，基线同样失败。六条基础旅程中五条通过，WebChat 非空 delta 数断言在本分支与基线同样失败。全量的 90 秒诊断上限曾截断长程测试，随后按默认超时单跑 `go test -count=1 ./e2e` 全过（68.436 秒）。`go vet`、gofmt、JS 语法、diff 检查通过；atlas audit 无新增告警。详尽结果与基线对照见编码报告；没有改这些范围外的测试期望。

性能：1440×900、DPR 2、本机 Chrome、缓存 scripted 图片各 30 次，从 DOM 点击到图片加载并经历两帧的 p95 为浅色 34.5 ms、深色 34.4 ms；不代表网络延迟或真实屏幕 FPS。级别：已合本地 main（merge `4a4ad70` / 功能 `1d8e24e`）；Pi civ-rev PASS（P0/P1=0）；未 push；等设计师逛后再 →已完成。Not run: 真模型、Linux CI、Safari／真实手机、真实网络交互与 IPC p95、真实屏幕 FPS、后台减速专项。

交付前已 rebase 到含编码克制约束的 `28501a6`（相对原基线只新增 AGENTS.md 约束），未修改该指令文件。rebase 后图片回归三遍及 trusted 检查再次通过：鼠标双击 scale=2.5、触屏捏合 scale=3、触屏双击 scale=2.5；均为浏览器真实输入后的 transform 实测。

## 2026-10-27 聊天仅工具产物可见（feature/tool-mediated-chat）

`notify_owner` 改名 `send_message_to_user`（扁平 text/images，每个 Bot 都挂，描述要求简洁人话）；用户正聊（web/terminal）只进气泡，不写 outbox/桌面，非正聊照旧通知。SSE 不再发 `delta`，送达的 `result` 带 `text`，`done.text` 为送达消息。网页聊天、侧栏预览、引用摘要只画该工具产物；主列不画工具过程行和自发往来系统行；旧轮（2026-10-27 11:00 +08:00 前）无该工具时仍按正文画。LanguageRule 改为短答与结果都经该工具；SilenceRule 旧措辞保留识别。新增只读工具 `get_group_message_history`（成员可读、limit 20/50、keyword/sender/before_id、正文不可信且一行加引号）。验证见 dev/tool-mediated-chat.md「怎样算对」；原有失败未改：`TestWebApprovalAllowAndDeny` 挂起、4 条 parked approval、`internal/work` TestShellUsesBotDir，基线同样失败。级别：已提交本分支，未合 main、未 push。Not run: 真模型、Linux CI、本机浏览器人工查看、性能数字。
## 2026-10-27 更换模型行横铺（功能分支）

- 单点修复：模型 id 在左、能力句靠右、选中标记贴右缘；本机 Chrome 用 scripted 名单检查 390 / 820 / 1440 像素布局和既有换绑交互，旧布局在新增几何回归中失败。尚未合入或经过用户日用验收。
- CSS 资源由 17126 增至 17289 字节（+163 字节）；无新增请求、轮询或动画。未测帧率、交互及 IPC p95。
- `gofmt -l .`、`go vet ./...` 和布局浏览器回归 `-count=3` 通过。全量测试中 E2E `TestSpawnWhileChatKeepsAnswering`、kernel `TestLongTurnStaysUnderTheCeiling`、work `TestShellUsesBotDir` 失败；Web 全包两次运行均卡在测试服务器关闭，手动中止，故全量及 Web 全包重复检查未通过。

- 2026-10-27：本机可选账号分支实现本机免账号进入、设置账号与设备双态；脚本替身检查及实际验收见 `docs/dev/device-optional-account.md`，未合入 main。
- 2026-10-27：统一中文称呼「主人→用户」（feature/wording-owner-to-user）；产品文案/提示/文档口径；docs/guard 术语软待政东确认。复审 PASS，已合本地 main。
- 2026-10-27：资料目录的契约和根层索引通过环境流发布，保留旧记忆过渡；后续已接入关键词提醒。20 篇小文件的资料索引微基准平均 0.366 ms/次（本机，非 p95）；验证及基线软点见本地提交说明。尚未做真实模型或用户日用验收。

## 2026-10-27 对话身份卡与面板遮挡回修

按对话区宽度让信息面板避开悬浮身份卡，恢复整卡点击开合；开态阴影沿用浮层 token。浏览器 scripted 回归覆盖 959、960、1100、1200 等宽度、展开侧栏、长简介和桌面壳；旧样式在 960、1100、1200 复现遮挡。当前为功能分支验证，未合 main、未经过用户日用。目标浏览器测试三遍通过；web 整包三遍检查在 `TestWebApprovalAllowAndDeny` 等待连接关闭时 90 秒超时，全仓测试长时间未结束而中止。未测交互 p95 或帧率；没有新增动画、轮询或请求。
- 2026-10-27：连接应用设置内密钥弹层及安装前探测在特性分支实现（ADR 0031）；废掉商店关设置、代发聊天的路径。脚本替身覆盖弹层、探测失败不落盘、并发重复添加；真实上游手探 Playwright 导航 1.7 秒通过，Context7 坏 key、飞书坏 token 均被拒；正 token 的飞书与 Telegram 未实测。进度见 `docs/dev/connectors-secret-modal.md`。

## 2026-10-27 改名与 tool job 持久化（feature/rename-tool-jobs）

Bot 改名后所有工具回执 `tool job storage unavailable`（裸文本进 transcript，气泡无产物）的三处修复：`tools.Jobs.Load` 写 lost 失败时把 output 收束到当前 `Dir/id.log` 重试一次，仍失败丢弃该条，不再 `return err` 让 `shellJobs()` 永久 nil；`bot.Rename` 搬目录后原子改写 `shell-jobs/*.json` 里指向旧目录的 output；内核 `shellJobs()` 检测目录漂移后从新目录重建任务表，`bot_update_profile` 同一轮里再调工具（含 `send_message_to_user`）不再往旧目录写。回归测试 5 条，回退修复验证均先红：TestLoadRepairsRenamedOutputPath、TestLoadSkipsUnwritableRecord（root 跳过）、TestRenameMigratesShellJobs、TestRenameKeepsToolJobsWorking、TestShellJobsFollowRename。三个改动包 `-count=3` 全绿。原有失败未改（基线同样失败）：TestWebApprovalAllowAndDeny 挂起、4 条 parked approval、internal/work TestShellUsesBotDir。级别：已提交本分支，未合 main、未 push。Not run: Linux CI、真模型、性能数字。

复审回修（review1 FAIL 的 2 项 P0 + P1）：`readMeta` 要求正文 id 与文件名一致，Load 收束改用文件名 id，堵住 `id:"../escaped"` 写出目录外；内核不再从新目录重建任务表，改为 `Jobs.Move` 原地换目录并改写活任务的 output，`bot_update_profile` 改名后立刻搬表，改名工具自己的完成写回落到新目录、不再重建旧 Bot 目录；并发首次查表只会拿到同一张表。新增回归 3 条，回退修复验证均先红：TestLoadIgnoresRecordWithForeignID、TestProfileRenameDoesNotRecreateOldDir（scripted provider 真实调用 bot_update_profile）、TestShellJobsKeepOneTableAfterRename。级别：已提交本分支，未合 main、未 push。Not run: Linux CI、真模型、性能数字。

## 2026-10-29 桌面壳日常与重启续跑（工作树）

`scripts/xbot-dev start/restart` 默认改为后台 `xbot app`，`--web` 显式回到 serve + 浏览器；PID 和日志仍在 `~/.xbot-build/dev/`，README 说明安装版 `/Applications/XBot.app` 使用默认 `~/.xbot`。新增 kernel 工具 `restart_xbot`，走统一注册表和权限表，即使 permissions off 仍须用户确认；先将活跃 Bot 名单与共享提示原子落入数据根唯一续跑文件，再请求桌面 `appshell.Relaunch` 或 serve `reexecService`。启动经现有 append-only Inbox 给持有前台 handle lock 或带 running/queued 子代理的所有 Bot 注入“XBot 已重启”与用户提示；确定性事件 ID 让部分 fan-out 可幂等重试，闲置 Bot 不收事件。未做外置 Codex 兜底、自动改代码合入或回滚链。

验证：`TestRestartContinuationPersistsAndWakesEveryWorkingBot` 覆盖前台忙、运行子代理、排队子代理均收到提示且 idle 未收到；kernel 注册/提示转交和 restart 必须确认测试通过；目标包 `cmd/xbot`、`internal/review` `-count=3` 通过。`gofmt -l .`、`go vet ./...`、`git diff --check`、`bash -n scripts/xbot-dev` 通过。全量测试跑到 web 包后，kernel 的已知 `TestLongTurnStaysUnderTheCeiling` 失败且 web 测试进程约 4 分钟无输出后中止；目标包 `internal/kernel -count=3` 同一已知用例三次失败。此前里程碑已记录该用例失败。未在本机实际打开桌面窗，性能数字未测；不代表端到端或主人日用通过。代码仍在 `19b13842` 基线上的未提交工作树，未 push、未合并。

- 2026-10-29：CLI 外部 Event 入站实现与脚本化定向测试；真实入口及主人日用待验。

## 2026-10-31 对话／语音簇 v4（功能分支）

按本机设计师 v4 定稿修正聊天渲染，现状见 [界面](atlas/ui.md#语音交流)。装配仍走 `appendMessageBubbles`：连续文字一泡、明确短尾注归音、音图各自独立；簇内各泡自然宽，播放钮与十根竖波形行内同高。未改 transcript、`spokenLines` 或存盘，也没有齐宽分支。

浏览器回归覆盖用户、Bot、legacy 与乐观发送，包含三段正文加语音尾注恰为两泡、引用和图片导航、代码／公式空行、模糊尾句仍留正文、音频互斥与键盘播放、轮询保留播放器。新回归在 `101c2bcf` 归档上因五泡而失败；目标浏览器组 `-count=3` 通过，原生录音和播放单独三遍通过（组合运行曾有一次播放等待超时）。浅深色 1440×900／DPR 2 的前后、悬停、播放态及 390px 窄屏截图在 `~/.xbot-build/ui-shots/chat-voice-cluster-v4/`；待设计师复核。

性能：短音泡 197.94px、长文泡 451.78px，播放钮与波形均高 36px，窄屏 1 秒／60 秒音频均无溢出。scripted transcript 响应到 DOM 更新后两帧各取 30 次，p95 浅色 28.8ms、深色 33.5ms；测试轮询压到 100ms，只计响应后的渲染，不代表真实网络、屏幕 FPS 或 IPC。

`gofmt -l .`、JS 语法、`go vet ./...` 与 diff 检查通过；atlas audit 退出 0，保留已有告警。全量测试及 Web 整包三遍未绿：kernel 的 `TestLongTurnStaysUnderTheCeiling`／`TestMemoryFileLogGuard`、memory 的 `TestMonthlyGuardAndRoundTrip`、work 的 `TestShellUsesBotDir`，以及 Web 的旧头像静态断言与 `TestAskOnlyMessageDoesNotLeaveEmptyRow` 源码切片 panic 均在基线复现；扩大浏览器组的 `TestChatStreamBrowser` 布局断言也在基线复现。六条基础旅程冒烟仍在旧送达文本／delta 期望处失败，语音二进制冒烟通过。完整日志在 `~/.xbot-build/chat-voice-cluster-v4/`。

级别：已提交功能分支、本机渲染与原生语音验证通过，未合 main、未 push，未宣称整套旅程通过。Not run: 真模型、真实麦克风／扬声器、Linux CI、真实网络交互／IPC p95、FPS、后台减速专项、设计师截图验收与用户日用。
