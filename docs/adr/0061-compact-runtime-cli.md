---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0061 主动范围压缩沿原 source CLI

## 原能力与调用方

基线 TRUE transcript main `c4b368bfe924fcd5b7f14a0288de5a866610deef`。只迁模型主动 compact 门，继续履行 [ADR0021 决定6](0021-env-stream.md)：Bot 仍能主动压缩已结束范围，自动压缩与无网页手动按钮不变。原 compactTool.Run 将 `{from,to,focus?}` 直接交给现有导出 `Kernel.CompactRange(ctx,from,to,focus)`，Normal/Confirm=false，仅 source；没有 bot/person/owner 目标参数。

```sh
"$XBOT_BIN" compact --self --from N --to N [--focus TEXT|--focus-file FILE|-] [--json]
"$XBOT_BIN" compact --help
```

调用方用原 Shell 的 XBOT_BIN、XBOT_ENDPOINT、规范 XBOT_ROOT 和当次 XBOT_CALL_ID；旧 XBOT_BOT 只诊断，显式 root 必须一致。成功人读原 `compacted f-t` 加 newline，JSON 为 `{text:"compacted f-t"}`；错误为实际脱敏 `{error:...}`、CLI 非0。原 API 不返回 summary/body/阶段状态，故不新增 File/artifact、Applied/Confirmed/rollback ACK；摘要仍由原 State 与后续投影消费，不额外导出。

## 两个实际结构

**A：离线 Load 与新 Kernel。** CLI 按诊断 source 名装载 Bot，重建压缩调用环境：

```go
type OfflineCompaction struct { Root, SourceName string }
func (r OfflineCompaction) Run(ctx context.Context, from, to int, focus string) (string, error) {
    // Load(source); 新建K/Compactor/Clock/Memory，再CompactRange
    panic("not implemented")
}
```

它丢失原 Compactor/provider/Clock、调用 turn 用量、环境回放及 runtime 寿命；旧 source 名与改名后的当前 ref 不等价。要补足又会复制原装配/状态知识。落选。

**B：同 K、原 prepared source context、原 CompactRange。** 既有 listener 只增一个 typed callback，不新 CompactService/dispatcher/context 表或领域抽取：

```go
type Compact struct { From int `json:"from"`; To int `json:"to"`; Focus string `json:"focus,omitempty"` }
type CompactReply struct { Text string `json:"text,omitempty"`; Error string `json:"error,omitempty"` }
// 既有 cliops.Handlers 增一个字段：
// Compact func(context.Context, Compact) (CompactReply, error)
// Kernel callback伪码：
// release := k.holdOperations(); defer release()
// prepared, cleanup, err := k.runtimeSourceCall(httpCtx); if err != nil { return actualError }
// defer cleanup()
// text, err := k.CompactRange(prepared,in.From,in.To,in.Focus)
// 返回原text或实际error；不外套currentIO/envMu，不重绑owner/person。
```

CLI 导入 cliops，`CallCompact(ctx,endpoint,root,originalCallID,input)` 到 `POST /v1/compact`；call ID 来自原 Native Shell 环境，不提供 caller 自报身份 flag。只用现 runtimeSourceCall 的同 listener/live Local/running/未finished Shell prepared lookup及取消链，不用 HTTP ctx/curTurn/latestTurn、runtimeApprovalSource、approveRuntimeIntent 或新权限表。缺失/完成/foreign/nonShell/cancel 沿原 helper 拒绝，不能假造 ctx；它不是 primary/person 资格或审批卡。仅保原 helper 检查，不另套 Recall 专用的非nil frozen-turn/person门，不要求 approver 函数。

原 turnSeqOf 只看 prepared ctx 的 turnSeqKey/turnKey，否则0，不回退当前turn；这样背景/child按实际ctx归属。普通 HTTP取消与原调用取消合入该ctx，原 helper/Jobs/CLI 环境装配不改；承载 Shell 的机器与 policy gate 独立。

## 原范围、模型和保存语义

- inclusive `1≤from≤to≤Count`；KeepTail 默认8条message/entry留 raw，不切工具对。现有summary可严格整块包住再扩大合并，部分重叠及同范围再压按原 validateRange/blockOverlap 拒绝。旧 Summary→Blocks 只原本次内存 migrate。focus只TrimSpace，空回defaultFocus，无新增领域长度帽。
- currentCount/State/Slice沿原 current source ref，Slice是stored preview，不 hydrate blob。summarize跳审批/代码卡与forkmark，保图/音频路径、范围内整块prior summary、summaryFacts中的任务/结论/数字/seq-result路径/未完成与最新环境人物。原文只追加、不重写，不借压缩写记忆正文/log/episode/experience；既有读取维护不等物理零IO。
- 同原Compactor：compact.LLM 两消息（system+user）、无tools，ctx直接传Chat，原provider限制不变；仅LLM实现拒空输出并截8KiB。自定义Compactor不自动加8KiB帽，不加新token/range/timeout预算。usage wrapper仅包装compact.LLM；Chat nil error先记原call:compact/turn trace，后续空摘要/刷新/保存错误不抹去这次真实用量；Chat error/cancel不由此wrapper记成功usage，原trace写错被忽略的语义保。
- 模型返回后原addBlock→refreshMemSnap→saveCurrentState；refresh在envMu内回放有效对话，personID空读取Bot记忆，busy且已有MemSnap则保旧；按回放最新source重采人物，不把旧caller人物覆盖后来人的环境。完整环境snapshot先append，再保存覆盖State，没有第二份已见账。
- 只有全链nil才原 `compacted f-t`。模型成功后后段可能已有usage/环境公告再失败；WriteAtomic实际错误照传，不推断零副作用、成功回滚或盲重试。原后段没有ctx.Err提交屏障，不能承诺取消永不提交；不新阶段状态机/重试事务/CAS/Block版本表/额外Receipt-Inbox-Wake。
- LLM在本路径短current IO及envMu之外，不能外套route跨LLM（内部还会取route），不扩大成所有调用方ambient锁都不持有的保证。State/trace依原current_store引用，模型等待期间source rename后跟当前路径。memory快照仍有currentName→Recall分步、profile采样仍读k.Bot等既有接缝，不冒称全流程rename原子性，不为理论微竞态扩框架。

## 边界与短回包

纯 `ParseCompactRuntime` 无位置参数/alias，拒未知/重复flag，按arity消费字面--help/--；from/to显式必需，只解析Go int及溢出，范围语义交原方法。help离线，其他self；focus与focus-file至多一，文件路径不可空、`-`读stdin，内容可空。raw文本/文件/stdin max+1与最终escaped typedJSON各≤1MiB，不先trim或截断，不套经历summary2KiB或新focus领域cap。main早于resolveRoot/default/legacy分流，不离线Load/new K。

请求 `{from,to,focus?}` 严格单object，unknown/null/nonobject/尾随内容/URLquery拒绝，整数typed解码；复用loopback/Host/browser/canonical-root guard，HTTP回包64KiB。CallCompact必须携原CallID/header，不能复用transcript三读无ID的client语义。成功只回原短text，错误沿原动态redactor及held/Resources secret scrub，仅显示error可缩短；共享准确 `FormatCompact` 与整JSONNL各≤32KiB，CLI先打印收到事实再非0，不自动retry/proxy/redirect。这里无大正文返回，不挂20MiB结果artifact框架、不暴露summary或新状态检查API。

## 实际消费者与验收边界

registry/Normal Known/schema/index/describe、Link.Context自动+主动范围说明是实际门。只退compact wrapper/Known/index与届时确空context TOOLgroup，Link.Context器官/Event及记忆4KiB写守卫保；旧compact工具历史literal/paired读取保，无暗执行shim/假工具配对。research仅同 canonicalBIN/受控XBOT_BIN窄DirectXBotArgs+本parser的离线help，合法self仍Normal写，在解释器前拒；focus中的字面--help不冒help。不新增loose循环阈值。

autoCompact/foldBlocks/bgcompact.finishJob/applyReady/idle/bindingPrecheck直接走原规划或方法，不经过native门或解析ACK，不重造队列。现manual/context无要求调用compact的literal脚本，mock/eval精确grep无producer，不臆造批量迁移。现独立transcript foreign fixture只有一处compact describe作为无关live control，后续仅0d换仍live工具、保原旅程；作者registry describe控制同样必要窄迁，不重整个transcript三链。

最窄未来证据：真实source Shell+scripted compact.LLM（不能只用无usage裸fake Compactor），验原source turn的usage/Block、环境先于覆盖、memory.md/log正文未改与原文仍可读（不把锁/派生文件维护冒物理零IO）；首compiled早交，不等全部边界。复用未改range/pair/tail/overlap原有效证据，再必要窄核model错/cancel及Chat成功后State失败实错/已发布env；模型等待期间实际rename后State/trace跟新ref、旧目录不复活。无新源resolver逻辑则原三群审批/前序green有效复用，不整longrun/缓存/FTS-Memory-Context-Store/旧已绿族、真模型或性能矩阵。

选择B，接受原晚错副作用与微竞态界限，换取原状态真源。完整caller合同见 [母路线](../dev/cli-platform-entry.md#批-compact主动范围压缩具体合同pass已实施)。67f64c6a两docs经24 SINGLE ACTUAL CONCRETE SHAPE PASS，本ADR Accepted，按上述原结构实施；实际入口红与业务证据分别收，不以未知命令冒领域红。Root4USERdocs/OwnerSSE/guard/main/push日用不动。
