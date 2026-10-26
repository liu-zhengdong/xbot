---
kind: part
parent: README.md
code: [internal/bot/, internal/peer/, internal/cron/, internal/notify/]
verified: 2026-10-26 @ 46a2c70 + review1 修复
---
# 多 Bot

> **30 秒版**：一个主人有多个长期存在的 Bot：主 Bot、置顶、分组、Fork Bot、Bot 之间的私聊和群、档案、定时例程。

## 边界
负责：Bot 的生命周期、档案、Bot 之间通信、例程。不负责：跨用户网关（见 [网关](gateway.md)）。

## 怎么运转
- **主 Bot**：首次运行时自动创建，承接没有指定 Bot 的输入。主 Bot 不能直接删除，要先把主 Bot 身份转给别的 Bot；系统里永远至少有一个 Bot。
- **置顶**：任何 Bot 都可以置顶，主 Bot 默认置顶。
- **侧边栏分组**：每个 Bot 的档案有一个可选的 `group`。主人另外有一份分组顺序，没有拖拽排序。侧边栏按这份顺序显示可折叠的分组，每组带上 Bot 数量；`group` 为空，或不在这份顺序里的，归到最后的「未分组」。可以新建、改名、删除分组。删除分组只把里面的 Bot 挪到未分组，不删除 Bot。「未分组」不是一个可以新建或删除的分组。分组不进模板。这和 Bot 之间的聊天群不是一回事：聊天群仍用 `g_` 开头的 id，有自己的消息记录。
- **创建 Bot**：`create_bot` 把角色正文写进新 Bot 的人设，成功只返回 `created <name>`。新 Bot 的正文进入自己的固定前缀；紧随的环境消息只公布人设短哈希。
- **Fork Bot**：原 Bot 写一份交接摘要作为新 Bot 的初始记忆；新 Bot 有自己的职责，对话流从零开始；两个 Bot 自动互相认识；可以选择同时建一个项目群。
- **Bot 之间通信**：同一用户的 Bot 默认互相可见，可以互发消息、建群。群有自己的记录，群里的消息分发到每个成员的收件箱。设有防循环的限制（跳数上限、频率上限）。`message_bot` 的目标写成 `bot@设备` 时，经网关发给主人另一台配对设备上的 Bot，规则见 [网关](gateway.md)。
- **Bot 之间的私聊记录**：Bot 发给另一个 Bot 的消息全部留存，不分成临时和永久两类。正文先进收件 Bot 的收件箱，处理之后留在收件 Bot 的 transcript 里，不另建一份会话。因此这些话一直能被搜索、导出，也能被 `recall_history` 看见。主人跟某个 Bot 聊天时，对方 Bot 发来的内容仍只显示成一行淡淡的提示。要按时间翻看两边的原文，用只读接口 `GET /api/bots/{a}/peers/{b}/messages`，而且这个人必须同时是两个 Bot 的 owner。群里的消息不走这个接口。`{b}` 也可以是另一台配对设备上的 `bot@设备`：只读本机这一半，收到的取自 `bot:bot@设备` 事件，发出的取自 `message_bot` 调用和工具结果（`status` 为 sent/queued/failed），退回通知是 `kind: "bounce"` 的一行；每行都拆出 `from_bot`/`from_device`/`to_bot`/`to_device`，客户端不解析 `bot@设备`。以后可以单独压缩这类来回；现在不压，它们跟这份 transcript 里的其他行走同一套摘要。
- **跨用户**：必须双方授权，授权范围要具体、可以撤销；来自对方的内容一律当作不可信。
- **Bot 档案**：名字、简介、头像（没上传时是按 `avatar_seed` 在浏览器里画的 Notionists 线条人像，旧 Bot 的 seed 就是名字；人像只用冻结的白名单，只有平静和微笑两种表情，见 scripts/avatar。16 像素用只剩脸部的裁切，容器是 `#e4e4e7` 上的圆，深色 `#a8a8ad`。上传失败、seed 是 `glyph`、或脚本没加载时，画由名字决定的几何图形。「我」的头像不变，XBot 的 logo 从不拿来当头像。生成的人像只在浏览器里画，从不经过服务端，服务端不存也不发这类 SVG。编辑资料的抽屉里「换一张」「换一批」「上传」「清除」换头像；Licenses：DiceBear 代码 MIT，Notionists 美术 CC0 1.0，作者 Zoish，见 static/vendor/LICENSES.txt）、简介、选用的人设卡、IANA 时区。没有安静、适中、积极三档；想怎么通知，写在自己的 persona 里（[ADR 0004](../adr/0004-drop-proactivity-levels.md)）。旧档案里的 `proactivity` 还能读进来，被忽略，不进提示，也不再从 API 或网页返回。没有「说话方式」和免打扰：怎么说话写在人设卡正文里；通知在任何唤醒、任何时刻都直接送出。旧配置里的 `quiet_start`/`quiet_end` 读进来丢掉，下次保存就不再写回；旧的 `held.json` 由 serve 的下一轮把每条当普通通知送出后删除。Bot 可以提议修改，主人确认后生效。可以导出模板（不含记忆）。默认人设和规则要求用主人的语言回答，先调用工具再给结果，不要先用另一门语言讲自己打算做什么。主人没接的提议不要再说一遍。 档案里没有 `style`；旧配置的 language、tone、length、emoji 在首次加载时合成一条该 Bot 的长期记忆，写记忆成功后才裁掉 `style`（[ADR 0020](../adr/0020-voice-in-kernel-preferences-in-memory.md)）。迁移与 SaveConfig 共用 config.json.lock。旧模板里的 `style` 和免打扰字段导入时忽略。
- **人设卡**（`internal/personacard`，[ADR 0026](../adr/0026-persona-cards-replace-voice-and-quiet-hours.md)）：主人一份卡库，`persona-cards/<owner>/<id>/card.md`，第一行 `name: 卡名`，空一行后是正文；正文最多 32 KiB，最多 200 张，id 是 16 位十六进制，只作目录名，界面不展示。Bot 配置的 `persona_card` 是选用的卡 id，空就是未选用、走 `persona.md`。选用后 `ComposedPersona` 每轮从磁盘读绑定和卡正文，是引用不是快照：改卡后用它的 Bot 下一轮就按新的说话。前缀、环境流的 persona 短哈希和模板导出都读同一个 `PersonaText`（当前生效正文），导出的模板带正文快照、不带卡 id。已经加载的旧 Bot 副本保存配置时保留磁盘上的绑定，只有 `SetPersonaCard` 能改它。卡被删或 Bot 不是唯一主人时回到 `persona.md`。导入 Markdown 总是新建：第一行短的非空行作卡名，否则用文件名；同名先 409，确认后加「 (2)」「 (3)」。删卡时有 Bot 在用先 409 报数量，确认后逐个解绑再删；选用和删卡都在卡库锁里（库锁先于配置锁）。旧配置的说话方式迁进记忆时，profile 层满了就写进 log 层，不挡 Bot 加载。
- **工作角色**：档案里的 `role` 是 `chat` 或 `dev`，决定这个 Bot 默认能干多久。`chat` 是聊天型：前台最多 4 次工具调用或 20 秒，后台一件事最多 45 分钟。`dev` 是工程型：前台 8 次或 60 秒，后台 2 小时。空的或不认识的值按 `chat`。`xbot profile set --role chat|dev` 设置，`--foreground-max-tools`、`--foreground-max-time`（秒）、`--background-max-time`（分钟）可以单独覆盖某一项，写 0 保留角色默认值，负数关掉这一项限制（任何负值都归一为关闭）。前台上限管的是「多久之内回答主人」，后台上限才是「一件事能干多久」，所以步数不是可配项：内核里只有一个防呆常量（1000 步），正常任务撞不到。`serve` 和 `chat` 都按这里算出来的上限跑。注意 `xbot fork --role` 是新 Bot 的职责描述（persona），和这里的工作角色不是一回事。
- **定时例程**：五段 cron，最小间隔 5 分钟；或者一次性的 `at`（RFC3339）。cron 按电脑本地时区解释（文件里 zone 为 local；旧文件没有 zone 视为 UTC，serve 启动时自动迁移，触发时刻不变，先备份到 routines-backup-<时间>，可用 `xbot routine migrate --dry-run` 预览）。夏令时重复的那一小时只触发一次、跳过的时刻不触发。一次性例程不受最小间隔限制，到点后自动停用，错过 2 分钟窗口就停用、不补发。serve 里定时器每 15 秒单独检查一次并把事件投进收件箱，不等 Bot 的模型调用结束；处理事件仍在下一轮 serve 处理。回滚到 C1 之前的版本时，要把 routines-backup-<时间> 还原回 routines，否则旧版本会把本地时间的 cron 当成 UTC。创建、修改、暂停、删除都由 Bot 自己做主，不弹确认卡。停机或睡眠超过 2 分钟后，下一次检查给每个 Bot 发一条汇总事件（serve 和 chat 轮询共用 internal/proc 水位线，ID 按水位线确定、可去重，最多看 7 天，首次运行不发），列出错过的时间点，不补跑。
- **Bot 状态**：内核从当前这一轮直接推出状态，不调模型。`BotStatus` 有 `bot`、`state`（`idle`、`running`、`awaiting`、`offline`）、`tool`（最近一个工具）、`since`（RFC3339，本机时区）、`backgrounds`、`last_message`（空闲时最近一条主人或助手消息，最多 80 个字符，写前过替换函数）、`todo`（有活动待办清单时的一行摘要，见 [内核](kernel.md)）、`node`（本机为空）、`stale_seconds`（只有缓存过的远端点掉线时才有）。`awaiting` 表示这一轮停在等主人确认的那一步。内核的每一轮还留一个最多 20 条的步骤环 `{time, tool, summary≤120 字符, state}`，`state` 是 `running`、`done`、`error` 或 `awaiting`。`GET /api/bots/status` 和 `GET /api/bots/{name}/steps` 见 [ui.md](ui.md)；Bot 之间用 `bot_status` 只读地看同一个主人的 Bot。没有活内核的 Bot 是 `idle`，`last_message` 只读 transcript 尾巴。状态接口会被轮询，所以尾巴的原文按 `TranscriptStamp`（最新分段的文件名、大小、mtime，不加锁、不开索引）缓存在进程里：任何进程追加都会让分段变大，签名一变就重读；缓存只存原文，脱敏在每次返回时做，之后加进保险箱的密钥照样被替换。5 个 Bot 时 `/api/bots/status` 的 p95 从 2–8ms 降到约 0.45ms（macOS 本机）。
- **跨用户的状态可见性（M8）**：默认只看得到别的主人的 Bot 是「空闲 / 在忙 / 稍后回复」，看不到工具名、步骤或消息。要更细，得这个主人按联系人逐条开启，开启后也只给一行状态，永远不给步骤。对方主人写下的东西一律当不可信数据看。这条在 M8 实现。

