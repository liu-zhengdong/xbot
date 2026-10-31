# 环境提示光框架（语义公告 · 外置真源）

档位：**L**。政东 2026-10-31 点头世界模型（对话「好」）。对照 DSH：`systemPrompt.context` 注册缝、time-context 语义散文、真值≠公告双轨。本稿只定缝与第一刀；**不派编码实现**，等另开批。

写稿 tip：`7d84cb3b`。相关：候选「环境更新只发当前值」「环境实体自注册更新」；Obsidian `设计/上下文一批` §4；仓内桩 `docs/dev/env-entity-registry-stub.md`。ADR 修订提议：[0021-env-stream-amend-draft](../adr/0021-env-stream-amend-draft.md)（Proposed，主人确认前不得改 Accepted 0021 正文）。

## 做完长什么样

墙上的钟（外置真源）才是几点；聊天串里最多是一句「现在几点」的**语义公告**。模型看到的是人话（例如「当前时间：2026-10-31 16:17（Asia/Shanghai）；距上条环境公告约 N 分钟」），不再是主文 `{"set":{"time":"…"}}`。新字段只经 `Env.Register`；其它往上下文塞提示的用途挂同一层 `promptinject` / `context.Register`，禁止再各写一套标签拼装。

## 世界模型（已对齐，勿改）

1. **真源外置**：现采（必要时投影）。**不靠**从 transcript 抠 JSON / `<environment_update>` 当字段账本。
2. **模型面语义散文**；与「上次已告知的渲染全文」比，**有变才在会话尾部追加**公告（可带 supersede /「本条取代先前同类」）。公告**可进串**便于压缩与可见，但**不是**字段账本。
3. **双轨**：操作真值 ≠ 模型公告（对照 DSH）。查几点永远看钟；公告只服务模型面。
4. 前缀仍固定；环境永不改 StablePrefix（继承 0021 仍有效的部分）。

## 唯一装配 seam

```text
Env.Register(Field{
  Key:    "time",
  Sample: func(...) (Value, error),           // 写进外置 / 现采真值
  Prompt: func(v Value, change Change) string, // 专门构造给模型看的语义串；空=本轮不说
})
```

上层光框架（比 Env 再薄一层）：

```text
promptinject.Register(Source{   // 名称可随实现微调；形状要对齐 DSH context({name, order, text})
  Name:   "environment" | "reminder" | …,
  Order:  int,
  Render: func(ctx) string,     // 空串=本轮不注入
})
```

- 环境是第一个 Source：内部用 `Env.Register(Field)` 合包语义快照。
- 提醒通道（`Channel.Register(Kind{Trigger→string})`）形状已近；**本期可不迁**，只保证光框架接口能接。
- **禁止**再往 `sampleEnvironment` 硬编码表堆字段当完成；**禁止**以改 `encodeEnvironment` JSON 信封为主交付。
- 深模块：删掉 Register+Prompt，语义文案与采样会再次散回 `sampleEnvironment` / `encodeEnvironment` 两处。

## Interface（形状，非逐行抄代码）

| 面 | 约定 |
|---|---|
| 外置 Env | 各字段当前真值；「上次已告知」存**渲染全文或等价指纹**，不存「从对话回放的 JSON 基线」当权威 |
| 公告正文 | 语义散文；合包可带 supersedes 声明；标签可暂仍用 `<environment_update>` 或并进统一注入块——实现批再钉，但**主文不是 JSON set** |
| 差分 | 有变才追加尾部；同分钟 time 不重发（继承批 B 精神） |
| 压缩后 | 仍可尾部补一次全量语义快照（不改前缀）；体积进既有 T/H 预留 |
| 写记忆工具回执 | 与独立 env 公告共用 framing 精神：改一起改；回执内容可逐步语义化，本期以时间为先 |
| 回放 / 调试 | 需要机器值时读外置或 trace，不把「解析公告句子反推数字」当主路径 |

## 实现批次

| 批 | 交付 | 怎样算对（一句） |
|---|---|---|
| 0 设计（本篇） | `docs/dev/env-prompt-frame.md` + ADR 修订提议 | 主人点头世界模型已落仓；Accepted 0021 未擅自改写 |
| 1 时间第一刀 | `time` 经 Register+Prompt；废 JSON set 作 time 主文；外置真源 + 语义尾部公告 | 假模型/单测：同分钟不重发；变分钟出现语义串；请求里看不到 time 的 JSON set 主文；压缩后仍能再发全量语义快照 |
| 2 光框架挂稳 | `promptinject`/`context.Register` 成型；环境 Source 迁入 | 新用途只能 Register，不能改硬编码表；提醒可接但不强制迁 |
| 3 其余字段 | person / tasks / memory_diff / tool_index… 逐个 Prompt | 逐字段语义化；JSON 通用路径降为遗留或仅调试 |
| ADR | 主人确认后改写或 supersede 0021 | 见提议稿；Agent 只提议 |

## 对照回顾

| 来源 | 撑得住 / 冲突 / 可复用 | 取舍 |
|---|---|---|
| 候选·环境更新只发当前值 | 撑得住（语义 + 有变才发 + supersede） | 本文第一刀对准 time；废「每分钟 JSON now」 |
| 候选·环境实体自注册更新 | 撑得住（Register 采样/展示） | 升级为正式 seam；不再等「JSON 拧巴才开」 |
| 候选·对话区统一插入系统提示 | 无冲突（那是 UI 时间线插入） | 本期不做；光框架是**模型请求**注入，不是主列横幅 |
| Obsidian 设计/上下文一批 §4 / 批 D | 冲突于「已公布从对话回放」 | 真源改外置；批 D 桩精神并入 Register |
| `docs/dev/env-entity-registry-stub.md` | 可复用接口方向 | 语义 Prompt 取代「默认仍走通用 JSON」为主路径 |
| atlas/context · 环境流 / env_replay | **冲突**（回放 JSON 账） | 合入后改述：真源外置；公告语义；见一句指针 |
| atlas/kernel · 提醒通道 | 可复用形状 | Trigger→string ≈ Prompt；本期可不迁 |
| ADR 0021 | **部分作废**：「已公布只从对话回放、忌第二份账」 | 提议修订；保留「只追加、不改前缀、压缩后尾部快照」 |
| 批 B context-batch-b（当前值/同分钟/快照） | 行为精神保留 | 差分对象改为渲染全文/外置投影，不是 JSON 字段回放 |
| DSH context / time-context / RuntimeContext | 对照蓝本 | 跟双轨与语义 supersede；不抄 JS API 名到死 |
| guard 六条 / 长程 | 不可退化 | 长程须重跑；缓存前缀命中不得因本改动被破坏 |

## 非目标（本期）

- 不实现编码；不迁全量字段；不强制迁提醒 / 插话 / turn_progress。
- 不改「环境只追加、不改前缀」。
- 不做「公告也不进串、只进当次请求」（比 DSH 更激进，另议）。
- 不建第二套环境 UI 账本；不与「对话区统一插入系统提示」抢 UI 缝。
- 不擅自改写 Accepted ADR 0021 正文（只提交 Proposed 提议稿）。

## ADR 修订要点（摘要）

详见 [0021-env-stream-amend-draft](../adr/0021-env-stream-amend-draft.md)。一句话：**废**「已公布值只从对话环境消息回放、不另存第二份账」；**改成**外置真源 + 语义公告双轨；保留追加式尾部更新与压缩后全量快照。
