---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0055 成员 Bot 的聊天群沿原 Bus 与真实调用链进入 CLI

## 问题与调用方

本刀仅迁list_groups/get_group_message_history/create_group/post_group四原模型动作。现xbot group是terminalOwner侧栏Folders的list/create/rename/delete/set，不是g_聊天群；用户root独立message设计是主人HTTPsession/SSE，send是externalAgent Event，均只读避冲突。因此新namespace选 `"$XBOT_BIN" chat-group list|history|create|post --self ...`，不接管group/message/send，不加聊天群rename/editmember/delete。0055 glob空后建Proposed，精确caller/typed字段/阶段与验收见 [母dev](../dev/cli-platform-entry.md#批-chat-group成员聊天群四动作具体合同pass实施中)。

## 两种实际结构与选择

A：CLI走owner Web groups HTTP（CreateGroupForOwner/KindPerson），或Load/root再newK。这把原source MEMBER范围改成owner全members范围、可能冒人发言或从当前另一turn重造链，淘汰。B：原listener挂typed ChatGroupHandlers，捕原Resources/source，用peer.Bus原ListGroups/Transcript/CreateGroup及单一PostGroup写路；current source member范围，Post只从原live Shell Job prepared ctx取冻结turnChain/turnHops。选择B，不新K/ref/Bus注册表/任务授权账或事务，沿原时钟/资源/存储。

## 原调用、提交事实与预算

List/History/Create无原turn业务需求，不强CallID。Post原Normal不新审批卡，却必须有效原Call-ID/live Local/running/未finished/同listener Shell prepared ctx，不能用普通HTTPctx或latestTurn假造链。拟从现runtimeApprovalSource只抽出共享runtimeSourceCall解析器，approval wrapper继续原owner动态fork/child rebinding；Post只用原prepared frozen turnKey显式取chain/hops，不调用approvalIntent、不走重绑后turnFor fallback。空chain为ch_CURRENT_SOURCE_NAME，hops为原值+1，KindBot。missing/completed/foreign/nonShell或无冻结turn写前拒；保原caller Shell gate，不造生产身份hook。

原Bus.PostGroup先trim1..8KiB/成员/hops≤3、sender-group8/min rate，再Append群transcript，按原members顺序逐其它member Load/OpenInbox/Post；Bus本身无waker。保现PostGroup(error)签名为薄兼容入口，新PostGroupWithResult返回实际GroupPostResult并承载同一writer；appended仅原AppendJSONL成功后true，message_id取该line，posted_members仅每次Inbox.Post成功后追加，failed_member/stage在实际失败点取。后段失败可appended=true且部分posted，不能error推没发/自动重试、rollback/rate恢复/签收账或改ownerKind。create仅saveGroup原atomic meta成功后created=true，无虚构多阶段partial。

成员范围/历史过滤保原方法与算法，full typed Group{id,name,members,created}/Line{id,time,sender,kind,text}不丢字段，history untrusted banner+Quote text防伪行。HTTP64KiB，整JSON+newline与同准确human formatter各≤原Shell32KiB，任一大则在source当前Resources工作区用原work.WriteResult完整脱敏JSON/真实bytes（20MiB cap）和file/access:local/count小回执，不截群或原records、不提高预算。写仅短ACK，无大结果框架；节点raw/escaped请求各1MiB。

## 验证与后续

具体合同单次SHAPE PASS后，先首compiled list/history真只读入口交独立一次，再同cohesive刀完成create/post与原链/hops、可见成员、实际晚fanout失败与短回执/大产物。仅退四wrapper/Known/schema/index，保message_bot/fork与其它Link.Tools/Emitter/Messenger/DeliverDraft导线。原Inbox/serve处理与忙时吸收/waker不动，不承诺Bus即时叫醒所有生产者，不加群管理wake。代码/测试前不改atlas现状，不重旧已封族/全仓矩阵/用户日用/guard，不操作root/main、用户dirty文档、push/release；单聊remoteoutbox与fork各后刀，Event/Link装配在逐族等价之后另切。
