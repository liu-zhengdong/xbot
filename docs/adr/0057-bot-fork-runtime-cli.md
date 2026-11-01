---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0057 Bot 交接派生沿原 source Fork 进入 CLI

## 问题与调用方

message_bot已TRUE main98fba5e8，本刀仅迁fork_bot{name,role,group?}，不是bg_插话分支管理。0057先glob空后建Proposed，fecc具体合同单次SHAPE通过后Accepted；实际协议见 [API](../API.md#bot-交接派生运行时-cli)，证据见 [MILESTONES](../MILESTONES.md)，未迁族留在 [母路线](../dev/cli-platform-entry.md)。选独立 `"$XBOT_BIN" bot-fork NAME --self (--role TEXT|--role-file FILE|-) [--group NAME] [--json]` 与离线help，旧有效offline `fork SRC NAME --role TEXT [--group NAME]` 完全保留，不引混合arity/flag值dispatcher，不接owner message-SSE/externalAgent send。

## 两种实际结构

A复用旧offline fork或Web owner fork：前者TryLockHandle/OpenKernel/Backgroundctx，正忙source会拒绝；后者owner/host gate与cachedK，均不能冒原SourceShell身份、Memory/Compactor/turnSeq。淘汰。B既有BotHandlers加Fork callback、原listener typed request→同source Kernel.ForkWithResult，原live prepared SourceCall上下文连取消与turnSeq。选B，所有Bot可self派生、非primary也可；Normal不加审批卡，不造Kernel/Memory服务、ctx表、Source授权登记或bgFork管理框架。

## 保持原行为与锁边界

原name合法/非self/非existing、role职责Trim非空≤8KiB、groupTrim≤48bytes禁/\\/换行、source Owners非空与Memory非nil保。role是新Bot persona和handoff focus，不是cfg.Role；新child默认chat/45min，不复制sourceDev预算。写序原样：handoff→bot.Create→Owners→仅Provider/Model/ContextLimit binding→源rules读/迁移→child rules→逐handoff事实→child fork-source→source fork-NAME→可选source+child群meta，不启动child/投Inbox/wake或复制原transcript/blocks/person store。

handoff输入在原currentRef短IO冻结State/未覆盖行/原projected结果、ScopeBot profile facts及按原非空分支读取的源persona；MemoryRecall与rules读可能发生既有迁移，不把前置错误冒全root零写。原Focused compactor优先/Compact fallback、原prepared ctx/turnSeq记usage、compact.LLM无tools的Chat均在route/host/Jobs锁外。保持空源原fallback、摘要Trim/8KiB clip/OneLine/960byte切事实；不为了4KiB记忆容量暗缩摘要。Owners按原preLLM时点取；Links/binding原postLLM时点沿current source读。后source note直接短currentIO内以当前名作BotName/WrittenBy再原Memory.Remember，不能准备旧名后迟写重建旧目录；规则读用原current helper，不在持route回调中重入。无跨调用CAS/rename事务。

## 实际部分完成

原Fork任意error返回空结果会丢写事实。旧Fork保兼容签名与旧错误空result，只薄适配到唯一ForkWithResult writer；新typed接口拿真实进度。Created仅原Create成功或CreateCommitError.Applied，CreateStage仅原typed stage。Confirmed仅封闭的owners/binding/rules/handoff/child_note/source_note/group各原调用nil确认；handoff_parts只逐原Remember返回nil次数，handoff_total为原split结果，不冒已经确认的payload零写。

handoff最多8KiB而profile Memory只有4KiB，写若干960B事实后真实ErrProfileFull可止；Memory.Remember的saveTier是MD先atomic、index后atomic，随后还有原noteSync，任意error不能推本条MD未成。UncertainMemory仅给当前失败的{bot,key}并明确可能已写，前面确认count/Created/Confirmed/GroupID保；不扩wholeMemoryTxn结果框架、不stat/error字串猜提交、不回滚/自动重试/抬4K cap。GroupID仅原CreateGroup成功返回，阶段未确认不承诺全无字节。原usage/迁移副作用另不在Created含义里。

## 验证与边界

先母合同+本ADR单次具体SHAPE后才固定caller红与实现。首链必须真实非空raw history+summary+Bot facts，实际原compact.LLM无tools Chat一次、非空模型handoff→child记忆→child下一wake真正provider compose；不以emptySource跳模型冒绿。核child empty transcript/blocks、Owners/Links/三binding/rules、默认chat45min、两侧认识事实与optionalGroup meta无Post。直接Recall只ScopeBot/Person文件不复制，不等于原projected历史绝无person内容，不加CLI历史过滤器或新隐私保证。

真实8K摘要触4K latepartial应见child已成、0<confirmed parts<total和原容量错，按实际格式不写死第N条；另真实index FS错可MD已成但当前Remember未确认，回uncertain不假零写。LLM中改source名，前模型输入冻结、后rules/sourceMemory/group归当前路径、旧Bot/work不复活；原preparedctx与turnSeq不跟latest。原基准流程/原审批resolver不因新消费者重复全族；只有本次affected司机改真实新入口，独立owned由0d维护。短JSONNL与准确human各≤32KiB/HTTP64KiB，raw及转义请求各≤1MiB，不回显handoff/body或造artifact账。只退fork_bot；peers若确空才退，其余platform/EventLink/Inbox waker不拆。root4USERdocs、guard、main/push/日用与OwnerSSE边界不动。
