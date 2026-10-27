# 聊线程 live / paint 梳理（dev）

候选：`候选/活气泡分段对齐transcript.md`（实现中）。

## 目标

1. 流式分段与 transcript 一致（消灭聚→散）。
2. 先写清「原本该有的流程」，再砍重复渲染。

## 原本该有的流程

| 阶段 | 该做什么 | 不该做什么 |
|---|---|---|
| 流中 delta | 写入当前 live 泡 | `loadThread` / 全量 `renderMessages` |
| 流中 tool | 封当前 live（若有文）、新 live；工作条/sysline | 把多段文拼进同一 `acc` |
| 收工 | 一次对账 `loadThread`；能无感则无感 | 肉眼「一条炸成多条」 |
| 空闲轮询 (~3s) | status / notes / lists / transcript 各用签名；无变则跳过 DOM | 无变仍 `thread.replaceChildren` |
| 切会话 | 重置 sig、一次拉 transcript | 与旧会话交叉写 |

## 已知浪费（改前核对，按证据砍）

- `readSSE`：`acc` 跨 tool 累加 ↔ 服务端 `replyStream.tool` Reset。
- 收工强制 `state.sig = ""` 再 `loadThread`：必要对账，但 live 已对齐时应避免可感重排。
- `startPoll` 每 3s：`loadThread` + `refreshLists` 等；`loadThread` 虽有 `sig === state.sig` 早退，但早退前仍 `renderLists()`、且整份 `JSON.stringify(messages)`。
- 流中每个 delta：`prose.replaceChildren` + 全量 `appendMarkdown(acc)`（可后置；本批优先分段与无变化短路）。

## 参考实现（关键点，非剧本）

- 主落点 `internal/web/static/app.js`：`readSSE` / `liveBubble` / `loadThread` / `renderMessages` / `startPoll`。
- tool 事件：`acc` 非空 → 封 `.msg.live` → 新 `liveBubble()` → `acc=""`。
- 轮询/对账：补齐或收紧签名短路；禁止为「理论上更稳」加多余全量 paint。

## 底线

禁 sync-app；不 push；编码克制；允许反问。大翻写或上虚拟列表须先问政东/主管。

## 怎样算对

`TestLiveBubbleSplitBrowser`（无头 Chrome，scripted SSE）：ack → tool → 终稿，流中已是两泡、只有一条 `.live`；收工对账后仍两泡，全程没有一泡同时含两段；空闲轮询 transcript 不变时 `.thread-inner` 与轨迹 `.trace` 节点不换。

## 还没做的

- 流中每个 delta 仍 `prose.replaceChildren` + 全量 `appendMarkdown(acc)`；长段落时是主要 paint 开销。
- 收工对账仍整串 `renderMessages`；分段对齐后肉眼无变化，但 DOM 整体换新（滚动保持靠 near-bottom）。
- 轮询签名仍整份 `JSON.stringify(messages)`（纯 JS，不碰 DOM）；要更省需服务端给版本号/ETag。
- tool 前没有正文时，peer 系统行插在当前空 live 之前，live 已按续写标了 `cont`；对账后会断组（只影响 peer 工具首段无 ack 的情形）。
