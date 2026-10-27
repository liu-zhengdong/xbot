# Bot 面板 · 收件箱 Tab（在制）

档：M · 旅程：打开 Bot 信息面板 → 点「收件箱」→ 看见入站叫醒事件（或空态）
来源：设计师 `~/repos/xbot-ui-design/bot-inbox-tab/` · 政东 [t1326u]
## 还没做的

真实入口端到端验收、主人日用确认；Webhook CRUD 不在本条。

冒烟用例：待独立编写。当前实现以接口测试验证列表和鉴权，尚未声明端到端通过。

---
# Bot 面板 · 收件箱 Tab · 实现交接

2026-10-27 · 用户体验设计师 → XBot 主管
源：`ia.md` · 轻高保真 `hifi/index.html` · 截图 `refs/t1326u-tabs-arrow.png`
对照：内核 `internal/inbox` · `link.Event` · 现网面板 Tabs · ux **H25** · Webhook CRUD 另见 `bot-webhooks/`

## 验收（人话）

1. 打开某 Bot 信息面板：顶栏 Tab 为 **详情 · 记忆 · 任务 · 轨迹 · 上下文 · 收件箱**；「收件箱」在「上下文」右侧。
2. 点「收件箱」：导语「叫醒这个 Bot 的外部消息」；有事件则新→旧列表（时间 · 人话来源 · 一行摘要）；点行可展开看摘要正文。
3. 空态：「还没有收到外部消息」+ 次文「Webhook、定时、其他 Bot 等叫醒时会出现在这里」。
4. 不出现原始 `<untrusted_event>` XML；不把主人铃铛/邮件当本 Tab；轨迹 Tab 行为不变。
5. 窄面板 Tab 可横滚、不折两行；pending>0 时 Tab 旁可有弱数字，无红角标。

## 挂载

- DOM：现网 Bot 信息面板 Tablist 末尾加「收件箱」；内容区 `#panel-inbox`（名随意，语义如上）。
- 选中态 fork 现网 **pill**（见截图），勿另起一套 underline。
- **不要**改铃铛；**不要**在连接应用加入口。

## API（内核有 Pending/Ack · 网页尚无列表 · 本批须补）

建议（路径可微调，行为须齐）：

| | |
|---|---|
| `GET /api/bots/{name}/inbox?limit=` | 返回该 Bot **pending + 近期已吸收（acked）** 的事件，供 UI 列表；默认新→旧或由 UI 反转 |

建议响应形（字段名可对齐现有 JSON 风格）：

```json
{
  "pending_count": 2,
  "items": [
    {
      "id": "…",
      "source": "webhook",
      "type": "message",
      "sender": "…",
      "sender_kind": "person",
      "time": "2026-10-27T12:00:00Z",
      "summary": "一行人话摘要（已剥 untrusted 壳）",
      "status": "pending"
    }
  ]
}
```

实现要点：

- 数据源：`internal/inbox` + 必要时读 append-only 日志里已 ack 的近期条（「近期」窗口由实现定，handoff 要求 UI 能看见刚处理完的，不只 pending）。
- **摘要**：服务端或前端须剥掉 `link.UntrustedEventPrefix` / `<untrusted_event…>`，只给人看 content 摘要；禁把整段 XML 当 `summary`。
- `source` → UI 人话表见 `ia.md`。
- 鉴权：与其它 `/api/bots/...` 同登录会话。
- 本批**不要求**网页 Ack 按钮；Ack 仍是内核消费语义。只读列表即可。
- Webhook CRUD **不在本 API**（见 `bot-webhooks/handoff.md`）。

## 文案

| UI | 用 | 禁 |
|---|---|---|
| Tab | 收件箱 | Inbox / 入站队列 / pending |
| 导语 | 叫醒这个 Bot 的外部消息 | untrusted events |
| 空态 | 还没有收到外部消息 | no inbox items |
| 来源 | Webhook · 定时 · 其他 Bot · … | NameWebhook · TypeTimer |

## Hash（hifi）

| Hash | 含义 |
|---|---|
| `#inbox-empty` | 收件箱空态 |
| `#inbox-list` | 有若干条（折叠） |
| `#inbox-open` | 一条已展开 |

## 非本条

- 主人铃铛 / notes 出站改版
- Bot Owners 配置
- 邮件收件箱；「例程读 Bot 收件箱」
- Webhook 新建/密钥/撤掉（`bot-webhooks/`）
- 重做详情厂头 / 职责 / 模型行
- 灰框简壳（G1）

## 自检

- [x] Tab 在上下文右侧（[t1326u]）
- [x] 产品义 = 本 Bot 叫醒事件，非铃铛/邮件
- [x] 须补 list API；内核 Pending/Ack 已有
- [x] 与轨迹分工清；剥 untrusted XML
- [x] 与 webhook CRUD 分批边界清
