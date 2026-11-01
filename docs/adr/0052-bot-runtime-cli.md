---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0052 原 source 的 Bot 发现、状态与创建删除经 CLI

## 背景与实际调用方

routine已合main2f03f51d；本刀仅沿母路线迁list_bots、bot_status、create_bot、delete_bot，不迁消息/群、fork、persona、role/budget或pin/primary。现CLI是平面list/new/delete，不存在bot子命令；list忽略余参，带--self仍走离线seed/全本地列表，独立真红89cea90b已固定在2f/validation4a8bfd14（exit0但不是runtime JSON）。用户另份cli-message-bot是主人SSE与外Agent send设计，不是Bot message_bot，本刀不修改。

调用方：`"$XBOT_BIN" list --self --json`、`info [NAME] --self [--steps N] --json`、`new NAME --self (--persona TEXT|--persona-file FILE|-) --json`、`delete NAME --self --json`。--self只定位source endpoint，不指目标。精确DTO/路由/预算/生命周期及怎样算对见同份 [母dev具体合同](../dev/cli-platform-entry.md#批-bot发现状态创建删除具体合同pass实施中)。无--self的原list/new/delete、pin/primary/fork等保原离线含义，runtime失败不回落。

## 两种结构与决定

A仿离线Load/openKernel或new目标Kernel：会走terminal owner/seed/handle/provider，丢原StatusSource、host entry、Source Links/Owners和审批，淘汰。选择B：原listener四named typed List/Info/Create/Delete handler，借原source K/ref/root/动态redaction、现原Jobs/host callback，不重建Kernel/会话/索引/任务表或调用旧Tool.Run。

- List保原local VisibleBots（排self、非空owner交集）与remote缓存（source Owners匹配设备Person、未unpair、7天内、每设备先64）；不改为全root或新网络发现。Info承接原bot_status：请求时调原k.BotStatuses/BotSteps，保Host Statuses整体视图和fallback source+self/visible的差异，不偷加交集。Web在StartOperations之后才赋Statuses，不得publish时冻结nil。Info无remote/node/stale字段，Steps保原≤10及named/singleton规则。
- Create/Delete每次重验当前source primary。Create需明确非空trim职责，继承source全部Links/Owners、用原全局默认binding，不继承source模型/role/budget，不建目标K或wake。保原config→state→persona→owners阶段：窄CreateCommitError在实际写点带Applied/Stage/Unwrap，config成功后后段错仍applied:true，不stat猜创建或回滚。persona原32KiB限制仍在WritePersona，不偷偷将原可partial的失败改为提前零写；EnsureDefault可能另写全局配置。
- Delete仅现source可见本地目标或self，保primary/last保护，remote不作管理目标。原Resources.Delete、DeleteRuntime与web.DeleteRuntimeBot改为透出已有(removed bool,error)；chat其它目标直接DeleteWithResult。保CheckDelete→同entry BeginDelete暂停Jobs/MCP→全锁外Jobs.Shutdown真owned join→同entry/name重查→Jobs fence/route/primary FinishDelete→removed后锁外Close清entry顺序。false错恢复同幸存表，true+vaultkey错仍终态，不stat推断/新事务/第二host表。不承诺原实现没有的全部child/code/remote/MCP join。
- Strict Delete保原Delete类别的无条件确认语义：!sourceK.permissive时取已main的真实live call及原wrapped Shell approver；不得在删Known后再查delete_bot而掉成Normal。复用现Jobs.CallContext/agent.ToolApprover/同listener/owner或明确child/取消/fork/park/deniedBefore；nested ToolArgs附canonical _xbot_bot={action:delete,name:trimTarget}，无随机nonce，真Run raw不改。missing/foreign/completed/noapprover不进入pause/删除；原A grant不准B，同A可重试。所有锁外批准后重验source primary/可见性。production permissive不新发卡、不强制无必要原ctx。
- request≤1MiB、reply≤64KiB；List/Info同准确纯formatter同时核完整JSON+newline及实际human≤现Shell32KiB。超限在当前source Resources工作区现results存完整脱敏JSON（现20MiB上限），小引用两格式也fit、bytes真实；无截列表/丢remote/抬Shell/强制json。继承CallID剥除，只有strict Delete需原执行关联，非新认证协议。
- 同cut只退四等价wrapper/registry/review/index/schema指引；实际bots组保pin_bot/set_primary，peers组保message/group/fork，persona/role等原门保留。research共用窄lexer/新纯argv parser，仅list/info/help，写/离线/操作符不放行。CRUD不制造Event/Inbox/wake，不改变0021环境流或原handle/资源所有权。

## 代价与验证边界

接受List与Info原来不同的可见范围、原StatusSource吞错/skip行为及limited owned-Job join，避免借迁移偷做统一目录或生命周期升级。接受config后partial/removed后清钥错误，不冒整事务或applied:false=全root零副作用。原上下文连接只有strict Delete实际需要时复用，不加新确认工具/表；生产无strict开关，不造独立strict E2E。

具体合同单次窄核PASS后，首可编译链立即交独立 source Shell list/info→new→后续发现→delete，再仅补primary/owner/remote、原default继承、stagepartial/同host删除/32KiB双格式、research及剩余Tools入口。strict deny/park A→B/原child/complete与fault partial由实现方真实上下文单位守护。封存routine六项/skills/profile/MCP及最窄List红不重跑，不全仓/真模型/无关日用；母路线其它族保留。
