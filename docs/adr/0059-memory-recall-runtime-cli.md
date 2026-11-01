---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0059 可见记忆文件检索沿原 source CLI

## 边界与实际caller

TRUE Experience maind632e7a3后仅迁recall一个能力；0059先glob空后建Proposed，afd6单次实际SHAPE通过后Accepted，沿该具体形状实施。具体形状见 [母路线](../dev/cli-platform-entry.md#批-memory-recall具体合同pass实施中)。命令选独立 `"$XBOT_BIN" memory-recall --self [--query TEXT|--query-file FILE|-] [--scope bot|person] [--since YYYY-MM-DD] [--json]` 和离线help；cmd实际grep无旧recall入口，eval memory-recall只是场景名。查询可缺或空，原Run虽schema必填query却允许无token返回no matches，不增加非空领域前提。scope/since按原字串传递，不Trim/casefold；raw与escaped JSON各≤1MiB。

## 两种实际结构

A离线全局Memory.Store.Recall不等价：它读事实与索引而不是原递归文件正文行BM25，并缺原发送者人物选择。B既有Handlers加单个Recall callback，原source Kernel→原runtimeSourceCall取得同listener live Local/running Shell preparedctx且turnKey非nil→唯一原扫描器，选B；不造RecallHandlers单服务、MemoryService/PersonSelector/sessionCache/ctxRegistry或owner-person-project授权表。原Readonly/Confirm false/always-allow语义保持，不调用approvalIntent/card、不要求approver函数/primary。不能用runtimeApprovalSource的owner重绑（child会nil再误回latest）、HTTPctx/curTurn代原prepared；真实Local child只是policy marker，不证明turnKind/SourceSubagent/人物为空，按实际prepared来源期望。

## 唯一扫描与当前引用

从recallTool.Run就近提私有k.recall(ctx,recallInput)（Query/Scope/Since三字串）作为唯一领域扫描；native壳在首candidate薄适配，最后仅退这一壳。保持Scope精确empty/bot/person；empty依原bot→可见person顺序union，query仅匹配正文行非path/scope字串。since原ParseInLocation YYYY-MM-DD/time.Local，不profile时区：memory.md dated条目与log/*.md按原行日期，其它文件mtime。原MemoryPersonOf(frozen)规则不重写：KindBot/Group优先无person；SourceSubagent或sa_发送者走父Bot首非空Owner，其它turnSender，trusted并非scope资格，不加--person/project。

扫描实际currentBot访问在一次原currentIO回调内取currentb.Root/Name，root EvalSymlinks/每scope的memory祖先规范、scope文件锁、WalkDir/打开inode核对均在该回调内；不外取旧Name再等Lock42的MkdirAll而改名后重建旧目录，不在持route时再调用currentName/current-helper。PersonDir不随Bot改名搬。MemoryPersonOf用原prepared turn与原parent owner分支，不改全局人物selector；BM25/最终格式可在读完后锁外。扫描是bounded本地FS IO无模型/网络，原取消只Walk callback/原scope锁等待不新增deadline；不加稀有CAS/跨调用rename事务或全局锁框架。

原逐scope递归.md/.txt、跳.bak和symlink/非regular、当前/打开inode同一性、最多2000文件/4MiB总读/每文件256KiB/正文分行去空头等保持。只总文件/字节cap触incomplete，不把单文件截断冒全局incomplete。原BM25所有命中里最多20条、各200 Unicode runes再…、scope:relpath:line编号、剩余条数及incomplete尾句/no matches完整保；不收缩成字节200或改变格式/日期/排序。

## 回包与实际消费者

MemoryRecall{Query,Scope,Since}→POST/v1/memory/recall严格单对象/unknown/EOF/query拒/原root-loopback-Host-browser guards；CallMemoryRecall(ctx,endpoint,root,originalCallID,input)。Reply只Text或File/Access/Bytes及Error，不反解析Hits/Count/新scope元数据。原scanner无selfRedact/无总outputcap，原Agent外层RedactMaxResult位置的持有secret/resource脱敏放在源typed回包前，完整原20hit语义文本再actualJSONNL+准确human各≤Shell32KiB/HTTP64KiB；必要时原work.WriteResult CURRENT workspace/0600/≤20MiB存完整该Text JSON，metadata两格式先验。不是导出4MiB原扫描文件，不截records/抬cap或新genericArtifact框架；artifact/error明确非0、无假File/新Inbox-waker-Env-MemoryReceipt。

实际消费为CLI/原memory指引与read_file读命中/完整产物；另public mock RECALL_PERSON有一个native产出及sawRecall进度门，eval scripts的memory-recall场景实际发recall，recall_result_test/grade又要求原工具结果包含路径与真实正文。退门时仅将该实际产出/进度门/场景driver及闭合场景的能力判定迁到规范Shell memory-recall，核原人物scope/query与该真实调用对应结果；不能改anyShell就通过、删case名称或添通用能力事件registry，旧native历史结果读法兼容保。无Web结果parser或新env事实。breaker原looseCall检索阈值应由实际Shell规范memory-recall调用承接：复用现DirectXBotArgs与本纯parser识别单一literal `"$XBOT_BIN" memory-recall ...` 后self/help，先于旧looseShell截首命令处理，不用Contains任意词放宽；它只影响循环阈值，不改变权限/解释器，旧recall历史键可兼容。不顺手扩所有平台CLI阈值。只退recall wrapper/Known/schema/index，memory工具注册组确空才退TOOL组，不删Memory器官/Link-Event/文件写守卫/4KiB cap/context四工具/detail/原memory历史字面。

## 交付与验证

具体SINGLE SHAPE后才0d固定最窄source caller红。首可编译真实Source owner人物目录+Bot自由文件/dated条目→Shell原prepared→原scanner→完整path:line正文→原read_file全文的链早交；真实KindBot Inbox no-person与query/date/scope/escaping artifact-currentRename边界同cut继续，不等首反馈停。当前没有已绿或代码实现声明。

作者UNIT核u1/u2另turn-latest变更、缺/finished/foreign/nonShell/cancel与nonNilfrozen必需、noapprover、真实Localchild按实际prepared Kind/SourceSender而非policyPerson空推人物资格，原SA父Owner分支/KindBotGroup优先，不能冒独立。真实FS锁阻塞→scan持current route→rename按现IO顺序及结果artifact后写当前路径、旧Bot/work不复活，无新生产测试钩子/期限。旧两个Recall FS/date/inode/budget夹具实际必要迁唯一scanner一次，旧current路径/MemoryPrompt/Organs-registry-links/公开mock RECALL_PERSON一个产出与sawRecall识别、manual/blurb/standing指引及looseCall按真实依赖窄迁；不改共享脚本harness或整旧Memory/Context族。Experience3/Fork7/Message6/Group5等与旧三审批证据封存；原source resolver无改则复用，不因依赖整族重跑。Root4USERdocs/ownerSSE/guard/main/push日用不动。
