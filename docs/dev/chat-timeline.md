# 聊天记录与 transcript 切开

档位：**L**（碰主列数据面）。政东 2026-10-31 拍板立刻开编码。候选：Obsidian `06-XBot/候选/聊天记录与transcript切开.md`；统一设计稿：Obsidian `06-XBot/设计/聊天记录与transcript切开.md`（与本文同步）。Stop That Shit：不建第二套可改正文真源、不做会话折叠/context_edit、不为「万一还要 raw 主列」留永久双主路径兼容层。

设计时 tip（写稿）：`7d84cb3b`（busy-desc）；主列 stick 祖先含 `451836f7`。编码前再 `git tip`。

## 用户旅程与怎样算对

挂 **guard/六条基本旅程 · 1. 网页对话**（入口：打开/切本机 Bot → 主列加载尾窗 → 上翻更早 → 历史搜索定位 → 回到最新；失败报「对话加载失败」）。守 **guard/界面**「对话里只放对话」。

**怎样算对（可观测）**

1. 打开/切 Bot：主列只请求 **chat 时间线**尾窗；响应几乎全是主人可见行，不再为凑可见条数扩 raw transcript。
2. 上翻 `before`：按 **chat 游标** prepend；视觉锚与 stick 与现合同一致；pipe 正文仍 ≤ ~700KiB。
3. 历史搜索：打在 **chat FTS**；命中是会话正文；点选按 chat 行开窗（回链 transcript seq 给轨迹/expand）。
4. 新送达：`send_message_to_user` 落地写入 chat（送达快照）；轮询/收工读 chat，不以 spokenLines 反推当主路径。
5. 轨迹 / recall_history / expand / 导出 / 模型上下文：仍只读 transcript。
6. 旧 Bot：首次打开或回填命令按现行 spokenLines/skipMessage/windowVisibleCounts 投影；不改旧 jsonl。

## 唯一装配 seam（白话）

**谁写**：transcript 仍只追加；在主人可见事件确定时经唯一入口 `Chat.Record(...)` 追加 chat 行（主人话、`send_message_to_user`/`notify_owner` 送达、主列决策/审批卡、主列来源行）。
**谁读**：主列打开/上翻/轮询/搜索默认只走 `GET …/chat`（+ `…/chat/search`）。
**禁止**：继续以「Bot.Window 扩 raw → HTTP 投影 → 前端 spokenLines/skipMessage 筛」当主列主路径；禁止第二套可手改全文。

深模块：删掉 Chat 后，可见规则会散回 Window 扩窗 + app.js spokenLines。

## Interface 形状

### Chat 行

建议 `bots/<bot>/chat.sqlite`（对齐 experience 旁路；或索引库新表，须同一写入缝、可重建）：

| 字段 | 含义 |
|---|---|
| `id` | chat 单调序号（主列游标） |
| `transcript_seq` | 回链账本 seq |
| `delivery_key` | 幂等键：`user:<seq>` / `say:<tool_call_id>` / `card:<id>:<state>` / `source:<seq>` |
| `kind` | `owner` \| `bot` \| `card` \| `source` |
| `time` | UTC |
| `text` | 送达快照正文 |
| `images` / `audios` | 附件 id 快照 |
| `reply_to_seq` | 可选 |
| `meta` | 小 JSON（来源展示、卡状态等） |

同一 `delivery_key` 只写一次；快照不因日后改 transcript 回写。

### 分页

`GET /api/bots/{name}/chat?before=&after=&around=&limit=`

- 游标互斥，对齐现窗语义；无参=尾窗。
- limit 直接限 chat 行（默认约 40、硬顶 200）；不做可见扩 raw。
- 响应 messages + window（head_id/tail_id/total/next_*/has_more_*）；行含 id、transcript_seq、kind 及主列字段。
- ETag/已读/鉴权精神同现主列；pipe 700KiB 仍守。
- `GET …/transcript` 保留给非主列用途；主列 UI 改挂 chat。

### 搜索

`GET /api/bots/{name}/chat/search?q=&limit=` — chat FTS；matches 带 id/transcript_seq/preview/time/kind。`transcript/search` 保留给 Agent。

### 写入时机

1. 主人话 Append 且现行会进主列 → Record(owner)。
2. **已定稿：选 1，保留现归组。** say 工具结果确认送达后先待整轮定稿；最终助手行到达时，经 Chat.Record 的幂等插入一次提交完整快照。多 say 多行，整轮图片/语音挂最后一条 say；相同 delivery_key 只插入一次，禁止事后补附件。SSE 仍即时显示，持久对账等整轮定稿。
3. 主列卡/来源行 Append 时 Record。
4. SSE live 不变；持久对账以 chat 为准。

### 回填

库空/世代落后时按 transcript 顺序用**单一 Go 可见谓词**（与 windowVisibleCounts + spokenLines/skipMessage 对齐）幂等投影；可选 `xbot chat-reproject <bot>`；不改 jsonl。旧轮无 say 且时间早于 toolChatSince 仍回填遗留 bot 行。

## 与现有能力关系

| 现件 | 关系 |
|---|---|
| Bot.Window 可见扩窗 | 主列换 Chat.Window；transcript Window 可留非主列 |
| transcript FTS | 保留给 Agent；主列搜 chat FTS |
| applyTranscript / keyed patch | 复用缝，key 改 chat id/delivery_key |
| threadStick / 打开遮罩 | 行为复用 |
| experience.sqlite | 参照旁路+seq，不合并 |
| peers messages API | 不改；主列灰字来源只投影 |

## 对照回顾

| 候选或能力 | 撑得住 / 冲突 / 可复用 | 取舍 |
|---|---|---|
| 聊天记录与 transcript 切开 | 撑得住 | 本文 |
| 聊天主列窗口与历史搜索 / transcript-window / ADR 0038 | 冲突（数据源）/ 可复用游标与 700KiB/遮罩 | 主列迁 chat；可见扩 raw 退出主路径 |
| 聊天主列增量刷新 | 可复用 | patch 缝保留，换行模型 |
| 聊天仅工具产物可见 | 撑得住 | 可见规则升格为 Record 谓词 |
| 活气泡分段 | 可复用 | SSE 保留；对账认 chat |
| 裸正文不进气泡 | 撑得住 | 新轮不 Record；旧轮回填保留截止 |
| 出图 / 语音附件 | 可复用 | id 快照 |
| 来源行 CLI/往来 | 可复用 | kind=source；peer 原文仍分 API |
| 会话折叠 | 无冲突 | 本批不做 |
| xbot chat 回放 | 可后接 | 本批不改 CLI |
| 轨迹 / recall / expand | 撑得住 | 仍 transcript |
| 被动旧事 / 经历 | 无冲突 | 经历仍锚 transcript |
| atlas/context 可见扩窗叙述 | 冲突 | 合入后改述主列读 chat |
| ADR 0017/0021/0028/0029/0030 | 可复用 | 无新上下文机制；pipe 帽仍守 |
| guard 网页对话/界面/长程 | 撑得住 | 不改追加与导出 |
| windowVisibleCounts vs spokenLines 双份 | 冲突 | 单一 Go 谓词；前端主筛退出 |

## 实现批次

1. Chat 存储 + Record + 回填（单测）。
2. HTTP chat 窗 + search。
3. 主列改挂；主路径去掉 spokenLines 扩窗依赖。
4. 冒烟红→绿；atlas/API 随合入轻更。

## 冒烟怎样算对

先红可写：

- `internal/bot/chat_timeline_test.go` — Record/回填/幂等/旧轮截止。
- `internal/web/chat_timeline_http_test.go` — 窗、帽、鉴权、ETag。
- `internal/web/chat_timeline_search_test.go` — 会话正文命中。
- 浏览器 journey（新测或扩 transcript_window_*）— 尾窗→上翻→搜索→回最新。
- 回归 stick / live / 语音 / 增量 patch。

## 验证入口

- 存储：`internal/bot/chat_timeline_test.go`（Record、不可变/幂等、整轮多 say/媒体、重开、回填世代、中文 FTS、分支完成、internal 谓词）。
- HTTP：`internal/web/chat_timeline_http_test.go`（窗口、搜索、700 KiB、鉴权、ETag、真实消息入口→scripted Kernel→chat）。
- 浏览器：`TestTranscriptWindowLiveBrowser`、`TestTranscriptWindowVisibleTailBrowser` 与既有 stick/live/语音/增量 patch 用例，改用 chat DTO；真实 HTTP 旅程另外核对主列不读 transcript、轨迹仍读 transcript。

## 还没做的

- 设计师视觉、用户日用、跨设备 pipe 实测及用户确认后迁入 guard；本批不改守护层旅程。
- CLI 回放与删除非主列 transcript 扩窗明确不在本批。

## 明确不做

- 第二套可编辑正文；会话折叠/context_edit；常驻 WS；改 jsonl；peer 原文并进 chat 表。
