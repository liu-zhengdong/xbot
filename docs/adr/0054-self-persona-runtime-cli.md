---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0054 完整职责沿原 self source CLI 与 SelfEdit 审批

## 问题与调用方

仅迁原bot_propose_persona{完整text}：所有Bot都能改自己，不要求primary，不接其它NAME、卡/role预算/通用patch。此前没有独立Persona CLI setter；profile runtime JSON没有职责全文。调用方选 `"$XBOT_BIN" persona set --self (--text TEXT | --file FILE|-) [--json]`；file/stdin为原Shell命令16KiB限下完整32KiB职责的实际输入通路。help离线，无self/endpoint不回落磁盘。2d9具体合同单次SHAPE PASS后Accepted；实现caller/DTO/边界见 [API](../API.md#自身职责运行时-cli)、实际接缝见 [内核](../atlas/kernel.md#自身职责与原-selfedit-审批)，[母dev](../dev/cli-platform-entry.md)保其它平台族路线。

## 两种实际形状与选择

A：仿离线Load(root,XBOT_BOT).WritePersona或再newK。虽可改文件，却信诊断名/旧路径、丢原资源路由与真实strict Shell调用/owner或child审批身份，不能沿原wake装配；淘汰。

B：在原BotHandlers/ParseBotRuntime/原listener增加一个typed Persona写，self沿同Resources当前route，原WritePersona是唯一物理写入与职责校验点；严格模式复用现runtimeApprovalSource/approveRuntimeIntent，解码后冻结同一完整有效text，不重读file或造新审批。选择B；原script命令、caller/状态/文件不另存第二份，不加K/ref/session、版本账/任务表/事务或唤醒机制。

## 提交点、确认与生效

WritePersona仍TrimSpace后1..32KiB字节，text+newline、0600、单WriteAtomic且Rename为最终提交点。短回执 `{name,written,effective?,error?}`：仅成功written=true/effective=next_wake，失败false，拒绝或FS错误非0；无primary式后续阶段或多文件partial，也不新全文read/artifact。raw/file/stdin及最终转义请求沿1MiB cap，不把原始输入先截32KiB再trim。回包HTTP64KiB、整JSON含newline和准确human各≤原Shell32KiB。

strict明确原SelfEdit恒确认，不查退掉bot_propose_persona的Known/Unknown、不降Normal或借Delete/ownerPermissive。沿实际live Local/running/未结束同listener ctx、原wrapped Shell callback/providerID/mutex，冻结policy/原owner动态fork卡/actual child原回调保。approval-only原Shell args附稳定 `_xbot_persona:{action:"set",text_sha256:FULL_SHA256_OF_TRIMMED_TEXT,bytes:BYTE_LENGTH}`；仅局部确定性摘要覆盖完整被审值，不把最大1MiB无效正文再复制进审批链或新存一份；grant/deniedBefore同canonical，改file内容或同命令A→B不能吃A，真Run raw不改，不引HTTP随机数/新确认协议。生产Permissive仍无许可卡且普通写不强CallID；strict无真原ctx在写前拒，只做actual-context单位，不虚构生产strict E2E。

当前资源在确认锁外等待后才短IO WritePersona，不拿route/Jobs/host锁问人，不改config.Persona fallback/rules/记忆/卡/transcript，不追加管理Event/Inbox或额外wake。原下一Handle assemble经currentComposedPersona读新完整职责，active已组装请求不主动重拼；环境沿0021仅职责label+shortHash、卡名/指纹，不塞全文到Event/旧历史/记忆或版本账。PickedCard首次正常EnsureSeeds需先warm再比卡不变，不能把首wake初始化当职责写副作用。

## 实现与验证

260首compiled已独立通过非主真source file完整写→原下一wake职责内容/hash链；546补退门与file/stdin/字节/trim/原子失败/自身份改名/非主source/readonly及strict原上下文边界，精确phase/当前独立与合入级别见 [MILESTONES](../MILESTONES.md)。仅退personaTool及空bot_profile组/Known/schema/index，保其它工具与owner HTTP原组合partial；旧族证据复用，不改guard，不操作root/main、用户独立owner-SSE、push或日用。完整验收与后续Event/Link仍在母路线。
