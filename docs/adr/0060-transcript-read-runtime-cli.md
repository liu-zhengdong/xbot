---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0060 原 transcript 三读沿 source CLI

## 问题与调用方

基线 TRUE Recall main `1053d0a9bb735e5de90ba75fcd3681c5742b3bf0`。只迁 `recall_history`、`expand`、`detail`；compact 的模型/State 写留后刀。真实原方法分别是 `RecallHistoryBot(query,name)`、`ExpandBot(name,from,to)`、`Detail(seq,experienceID)`，均无 ctx 参数，原 Run 忽略 ctx、Confirm false。不能把文件 Recall 的 mandatory CallID/prepared-person 门移来，也不另抽领域方法。

Agent 沿原 source Shell 使用：

```sh
"$XBOT_BIN" transcript search --self [--query TEXT|--query-file FILE|-] [--bot NAME] [--json]
"$XBOT_BIN" transcript expand --self --from N --to N [--bot NAME] [--json]
"$XBOT_BIN" transcript detail --self --seq N [--experience ID] [--json]
"$XBOT_BIN" transcript --help
"$XBOT_BIN" transcript search --help
```

search 返回原稳定 seq/role/短正文；按返回的真实 tool-result seq 调 expand 读原记录，不重执行原工具。Detail 普通 seq 为 1 基；experience>0 时 seq 是该经历 From 起的 0 基 offset，仅 source，不给 detail 加 bot/target 参数。原 experience<=0 仍走普通 seq，不顺手改成新错误。

## 两个实际形状

**A：离线/global store 重建读取。** `cmd → loadRoot/Load(sourceName) → TranscriptReader{Store,Owners,Workspace}.Search/Expand/Detail`；Reader 自己查全局目录、做 owner 交集、定位 blob、附件与 fence。它必须重造 source currentRef/原 owner 快照/目标授权/读副作用/脱敏上限；离线旧 source 名可在 rename 后访问错误路径，raw blob 又不等原语义文本。其骨架为：

```go
type OfflineTranscriptReader struct { Root, SourceName string; Owners []string }
func (r OfflineTranscriptReader) Search(query, target string) (string, error) { panic("not implemented") }
func (r OfflineTranscriptReader) Expand(target string, from, to int) (string, error) { panic("not implemented") }
func (r OfflineTranscriptReader) Detail(seq int, experienceID int64) (string, error) { panic("not implemented") }
// 三方法自己Load源/目标、直接索引/segment/blob/附件，再重做owner/fence/cap。
```

A 落选。

**B：原 listener 三个 typed callback 直用现有 K。** `cmd → cliops DTO/HTTP → k.TranscriptHandlers() → 原三个导出方法`。无新 K/StoreService/action bus/ctxMetaFrame/target registry。仅边界持原 operations lease、补当前持有 secret scrub、计输出预算，必要写原当前工作区产物；不会在原方法外套 currentIO。

```go
// cliops: 三个请求没有 action envelope；Detail 没有 Bot 字段。
type TranscriptSearch struct { Query string `json:"query"`; Bot string `json:"bot,omitempty"` }
type TranscriptExpand struct { Bot string `json:"bot,omitempty"`; From int `json:"from"`; To int `json:"to"` }
type TranscriptDetail struct { Seq int `json:"seq"`; Experience int64 `json:"experience,omitempty"` }
type TranscriptReadReply struct {
    Text string `json:"text,omitempty"`; File string `json:"file,omitempty"`
    Access string `json:"access,omitempty"`; Bytes int64 `json:"bytes,omitempty"`; Error string `json:"error,omitempty"`
}
type TranscriptHandlers struct {
    Search func(context.Context, TranscriptSearch) (TranscriptReadReply, error)
    Expand func(context.Context, TranscriptExpand) (TranscriptReadReply, error)
    Detail func(context.Context, TranscriptDetail) (TranscriptReadReply, error)
}
// 既有 cliops.Handlers 增一个 Transcript *TranscriptHandlers 字段。
// k.TranscriptHandlers 捕获 THIS K，三个回调分别直接调用：
// k.RecallHistoryBot(in.Query,in.Bot); k.ExpandBot(in.Bot,in.From,in.To); k.Detail(in.Seq,in.Experience)
// 工具以外的既有方法调用方继续用原导出方法。
```

CLI 导入 cliops，分别 `CallTranscriptSearch(ctx,endpoint,root,input)`、`CallTranscriptExpand(...)`、`CallTranscriptDetail(...)`；同 `FormatTranscriptRead` 生成准确 human 输出。POST 分别 `/v1/transcript/search`、`/v1/transcript/expand`、`/v1/transcript/detail`。没有 CallID 参数/header 要求；缺失、finished、foreign 或旧 process CallID 不成为领域资格。ctx 只用于 HTTP 传输取消，不能宣称原方法新增即时取消。三 typed handler 共享只读 text 回包，不新 rows/count 或通用操作框架。

## 保留的领域边界

- source endpoint/规范 root 权威，旧 XBOT_BOT 仅诊断；显式 root 必须相同。不得离线 Load source/新 K/primary/person/project 资格、runtimeSourceCall、审批 owner 重绑、approver 函数或确认卡。承载 Shell 的原机器 gate/审批仍独立。
- History/Expand 原 target trim；空或等 currentName 是 self，沿原 currentIO。foreign 只 `bot.Load(k.Bot.Root,name)` 一次，原 `k.Bot.Config().Owners` 与 target 加载时盘 Owners 非空交集；不强制刷新 Resources.current 的 source cfg。加载错/不存在/无交集/同群或只可消息的非同 owner均原 history ErrNotFound 遮蔽；授权后真实读盘错保留。foreign 没 stable ref，不承诺跟随其 rename；显式旧 target 名不当 source 诊断别名，不新 registry/cache/CAS。
- self 方法已有 currentIO；Detail 多次 currentIO 跟 source 原引用，不扩成一致快照事务。外套 route 再调 currentName/withLineNote/原方法会有 writer 等待下递归 RLock 风险。只有结果真正写入才短持 currentIO 取 current Root/Name/重基 WorkDir。原 segStore.with/ensure 可能建目录/锁、迁 legacy、修索引，读取不等物理零写。
- History 原 FTS5/BM25 最多 8 hit/stored preview、不 hydrate；snippetRunes 实为 240 **字节**，fork marker跳过、正文空才 toolBlob、整体4KiB/no-match词法原样。Expand inclusive `1≤from≤to≤Count`、summary覆盖范围可读、逐条HydrateLimit8KiB后整体8KiB、seq/role/附件line-note/foreign目标workspace与正文空才toolBlob保。Detail source经历范围/普通seq/时间、逐条HydrateLimit64KiB且整体64KiB、正文非空也追加toolBlob保。64KiB是完整原有语义上限，不是无限 blob/附件字节导出。
- 原 presentHistory 先 k.Redact 或 bot.RedactText 再4/8/64KiB cap；foreign Neutralize后完整Wrap(source=bot:target/type=transcript/sender=target)，膨胀时缩正文重Wrap的原逻辑和极端fallback原样，不顺手重写 fence/cap。self 不新加foreign fence。边界只如原experience search补 `config.RedactHeld`、Resources.Redact，不重建/再裁原文本；error沿原动态redactor。

## 输入、输出与原消费者

纯 `ParseTranscriptRuntime` 与早入口在 root/default/legacy 初始化前分流。help离线；其余必须self；无位置参数/别名，拒未知/重复flag，按arity消费，query中的字面--help/--不冒help。search query可省略/空，query与query-file至多一、query-file路径不可空；raw文本/文件/stdin max+1与最终escaped typed请求各1MiB，无截断后trim。expand要求from/to、detail要求seq；整数只在边界解析Go int/int64与溢出，不另夹正数/范围，领域仍交原方法。bot保持原输入交原trim。HTTP严格单object/unknown字段/非object/null/尾随内容/URL query拒绝，复用Host/browser/loopback/root guard，HTTP回包64KiB。

原完整 semantic Text（含原附件说明/foreign fence）补scrub后，准确JSONNL与human各≤32KiB；超任一用现 `work.WriteResult` 当前source workspace `results/<32hex>.json`、0600、≤20MiB写完整这份Text JSON含newline，metadata两格式写前预检、Bytes实际值。artifact不是target目录/无限blob导出，不二次裁fence/正文、不抬Hydrate cap或反解析typed rows；失败非0无假File，CLI先输出实际事实再报错，无自动retry。原read effects与产物写区分，不宣称root0IO。

实际consumer须同cut迁：fit.omittedResult 的seq→expand及无seq→search→expand指引保“不要重执行”、自定义noSeq保；SearchExperiences末detail提示、manual/context、live Link.Context、readonly分类/原History loose循环阈值、registry/Known/schema/describe。research仅当前canonicalBIN/精确$XBOT_BIN单POSIX直调、本parser合法self/help；不Contains放宽。loose只承接原recall_history对应search，expand/detail不借此新增宽阈值。只退三wrapper/Known/index，context TOOLgroup保compact，LinkContext器官/Event与4KiB memory写守卫不动；旧native literal/工具配对历史仍读，无可执行暗shim/伪native配对/新ENV/Inbox/waker。当前mock/eval精确grep未发现三读producer，不机械扩memoryRecall闭合eval桥。

## 下一步与代价

选B，接受原source owner快照、foreign名字不跟rename、原语义cap及原读取维护副作用，换取领域单一真源。具体合同见 [母路线](../dev/cli-platform-entry.md#批-transcript-read三读具体合同pass实施中)；c51fb3d7两docs经reviewer24 SINGLE ACTUAL CONCRETE SHAPE PASS，本ADR Accepted，按原结构实施。真实namespace入口红已封，不以unknown command冒FTS/owner/Detail领域失败。

首candidate沿真实source NativeSH search(query-file预置私有needle，避免命令自身FTS污染)→定位返回RoleTool真实seq→expand同条paired结果/full hydrated原文本；唯一原工具payload与计数器证实未重执行，不能只任意命中/seq存在。首compiled链早交独立后继续同cut sharedOwner与foreign/orphan/group mask、foreign完整fence/目标附件、source Detail两经历锚FULL10K与64KiB语义artifact、source rename/current结果/旧BOTS-WORK不复活。0d独占经验chain194–203两Detail司机及八处无关readonly describe control只由0d窄迁，保全部旅程断言；作者RuntimeTest170两锚保，必要影响项最小once，不整Experience/FTS/Memory/Context族或旧Fork7/Msg6/Group5/群三审批，源resolver不改复用。Root4USERdocs/OwnerSSE/guard/main/push日用不动。
