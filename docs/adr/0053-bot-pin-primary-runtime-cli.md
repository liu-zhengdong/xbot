---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0053 原 source 的置顶与主 Bot 转交经 CLI

## 背景与调用方

Bot四操作已合main ac519c8d。下一刀只迁原pin_bot/set_primary；persona、fork、消息群、role/budget不是本刀。实际离线CLI是pin NAME/unpin NAME各恰一参数、primary零参数ensureSeed并打印、primary set NAME恰set+name。独立最窄红b860b672在mainac/validation4ea58a06实际source Shell pin Peer --self只得旧usage（.68s/package2.050s），封存不重。

调用方明确为 `"$XBOT_BIN" pin NAME --self --json`、`unpin NAME --self --json`、`primary set NAME --self --json`。self定位原source，不是target；不新增runtime primary读口或通用Bot patch。离线help，未选self的原有效CLI形状不变。完整caller/DTO/阶段与怎样算对见 [母dev具体合同](../dev/cli-platform-entry.md#批-pinprimary置顶与主身份转交具体合同pass实施中)。

## 两种结构与选择

A：仿旧离线Load(target)/SetPinned/SetPrimary，或按XBOT_BOT再new Kernel。虽短，却跳过原source当前primary/VisibleBots、可能冒诊断身份或旧primary资格，不能让A原endpoint在转交后立即失权；淘汰。

B：在既有BotHandlers、ParseBotRuntime、原listener上加两个typed小写操作，复用原source Root/Resources与原SetPinned/SetPrimary，不在publish时缓存primary资格。每次运行取当前source名与root primary再检查self或原VisibleBots本地目标；B既有endpoint下一调用即可得资格，无new Kernel/ref/session。选择B；红线复核不加registry/路由表/alias/授权账本、通用字段patch或新锁事务框架。

## 真实提交点与回执

pin/unpin沿原SetPinned→SaveConfig锁下写，不另raw覆盖配置；primary自己也可unpin。primary在原primary.lock中Load目标→WriteAtomic primary.json→若目标未置顶再SetPinned(true)。保留SetPrimary原签名，拟只在实际失败点附PrimaryCommitError{Applied,Stage,Err}/Unwrap；primary.json写成后pin失败Applied=true，不能error推未转交、stat猜、回滚或给旧A假资格。typed BotPrimaryReply的primary仅在该次指针实际写成后携目标名字，连同applied/stage明确新指针事实；资格只由下一实际调用fresh核，不拿回执当永久grant。

成功/partial只短ACK，JSON含换行与准确human都≤现Shell32KiB，HTTP仍原Bot64KiB；不复制List/Info大产物机制。两个原Normal动作不新增确认卡或source-call approval桥；原Shell gate独立保持。只改指针与目标Pinned，不unpin旧主，不改Owners/Links/persona/binding/role/budget，不发管理Event/Inbox/wake。原两nativewrapper、空bots注册组/相关Known/schema/index同cut退；其它原门不借刀迁移，research只准新动作help。

## 验证与下一步

具体合同单次SHAPE PASS后才实现；首compiled交独立真实A source pin/unpin→primary转交B→原A下一操作拒/B原endpoint下一操作有权链一次，再补实际后段pin失败已转交partial、same-primary及primary可unpin、原配置合并、可见本地/非主拒、arity/help/root/短回执和退门。按影响最小范围通过即停；不重已封Bot5/routine6/skills/profile/MCP、whole packages/matrix/真模型/用户日用，不操作root/main或用户独立owner-SSE文档。
