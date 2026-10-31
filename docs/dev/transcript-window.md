# 聊天主列窗口与历史搜索

档位：L。候选与用户 2026-10-31 明确授权开编码；朴素功能界面先通，设计师后补视觉。

## 用户旅程与怎样算对

沿用候选与用户要求：打开 Bot 默认只拉尾窗；更早 prepend 接连续 seq；聊天内搜历史，命中可跳到附近窗口并继续前后翻、回最新；加载失败诚实显示对话加载失败。不依赖 compact，不改追加语义，不加 WS，不按 Bot 名分支。

## 对照回顾

| 来源 | 核对与取舍 |
|---|---|
| 候选目录全部条目标题 | 本次只处理聊天主列窗口与历史搜索；增量刷新/live/语音/来源投影现状复用，其余不扩面 |
| atlas/context、ui、multibot | 复用 segStore 的 lines 偏移、Slice、Bot.Search；不加全文索引；保留消息附件与来源字段 |
| ADR 0021、0017、0028 | 只改读取投影，无新上下文/后台机制，无写历史/改前缀 |
| ADR 0024、0029 | 保留同一用户鉴权路由，响应设置字节上限，长历史全量走流式 export |
| guard 六条基本旅程、长程运行、界面 | 不动模型/追加/导出；条件请求不换 DOM、历史阅读不抢位置；用户明确授权朴素界面，像素审稿后补 |
| LESSONS 三类 | 保持源边界、测试挂真 HTTP/Chrome，失败与端到端/日用分开汇报 |

## 实现方案

- 同一 GET transcript，无参数尾窗与 before 页以25条可见交付为目标，先取60条原始记录，不足由 Bot.Window 向前扩，仍受400条/256 KiB硬帽约束；显式 limit 1–200 仍按原始条数，after/around 缺省60条；before/after 为排他 seq，around 为包含目标的居中窗口，三者互斥。响应 messages + window{head_seq,tail_seq,total,next_before,next_after,has_more_before,has_more_after}。无全量兼容开关；export 保留全量用途。
- Bot.Window 在存储锁内确定快照尾序号，按索引长度和范围选窗口，最多约 256 KiB 原始行（单行超大仍允许前进），复用偏移读取，不 Entries 后 slice。旧 fork 回溯分支需按衍生索引补上下文，不能靠整本 replay。
- Web 保留原行投影，单超大行公开预览裁剪并标 truncated；最终窗口响应字节受限，裁剪仍保持连续范围与可推进游标。ETag 纳入规范窗口参数，身份先校验，历史浏览不错误标记尾部已读。
- GET transcript/search?q=&limit=，鉴权同原入口，q 最多 1024 字节，limit 默认20/最多100，直接 Bot.Search；返回 matches[{seq}]，不读整本/另建索引。朴素命中列表显示序号对应的短预览（接口提供或定位后），空/失败/加载状态可区分。
- 主列使用有界窗口缓存（最多800行，容纳两页各400 raw），合并从旧端裁、保最新尾部，更早/更新保持可见锚点；满窗不能继续上翻时提示用现有搜索定位；搜索定位替换主列窗口，前后都可继续翻，返回最新可用；历史位置不受轮询抢占。常规尾部轮询保留条件请求与 keyed patch；窗口间缺口明确提供更新入口，不静默拼接。
- 取消过期异步回包的 UI 应用，切 Bot/群/往来不泄漏旧结果；发送消息前回到最新窗口。加载失败保留已有消息并给出重试；首次失败是对话加载失败。

## 冒烟用例

HTTP：默认4460行历史响应显著小于全本，翻页连续、around包含目标、跨段、不读取无关旧段；搜索等于 Bot.Search、无命中、鉴权、参数、ETag与窗口隔离、fork跨窗、超大行、错误不吞。
**冒烟用例**：`e2e/smoke/transcript_window_test.go` 的 `TestTranscriptWindowJourney`（真 serve + HTTP + Chrome）；`internal/web/transcript_window_browser_test.go`（独立行为断言）。

Chrome：真 app.js 默认尾窗→更早（锚点保留）→搜索命中跳转→前后翻→返回最新→轮询/切Bot/失败文案；行为测试不锁像素。
全量：gofmt、vet、go test ./...、改动包 count=3、atlas audit。性能记录响应字节和 HTTP p95，不冒充生产或模型延迟。

## 验收状态

独立设计审已通过。HTTP4460行样本、连续游标/字节帽、索引升级/fork跨窗、真实HTTP+Chrome主列/FTS跳窗/640和390窄屏均通过；独立Chrome六组包括半JSON零泡、整包替换、长等轮换、切Bot隔离、reduced全静态、遮罩失败重试。完整基线差异与最终检查在 MILESTONES 记录，不将功能专项绿算作全仓绿。

## 还没做的

- 待设计师对当前Bot圆头像+闲话的浅深/窄屏截图复审；打开过渡以2026-10-31最新T1–T6当前Bot圆头像遮罩版为准，旧骨架揭开已否决。
- 用户日用验收；未合main、未push，不移动guard旅程。
