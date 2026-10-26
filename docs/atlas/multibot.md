---
kind: part
parent: README.md
code: [internal/bot/, internal/peer/, internal/cron/, internal/notify/]
verified: 2026-10-27 @ c809f54 + routine-precheck graduated
---
# 多 Bot

> **30 秒版**：一个主人有多个长期存在的 Bot：主 Bot、置顶、分组、Fork Bot、Bot 之间的私聊和群、档案、定时例程。

## 边界
负责：Bot 的生命周期、档案、Bot 之间通信、例程。不负责：跨用户网关（见 [网关](gateway.md)）。

## 怎么运转
- **主 Bot**：首次运行时自动创建，承接没有指定 Bot 的输入。主 Bot 不能直接删除，要先把主 Bot 身份转给别的 Bot；系统里永远至少有一个 Bot。
- **置顶**：任何 Bot 都可以置顶，主 Bot 默认置顶。
- **侧边栏分组**：每个 Bot 的档案有一个可选的 `group`。主人另外有一份分组顺序，没有拖拽排序。侧边栏按这份顺序显示可折叠的分组，每组带上 Bot 数量；`group` 为空，或不在这份顺序里的，归到最后的「未分组」。可以新建、改名、删除分组。删除分组只把里面的 Bot 挪到未分组，不删除 Bot。「未分组」不是一个可以新建或删除的分组。分组不进模板。这和 Bot 之间的聊天群不是一回事：聊天群仍用 `g_` 开头的 id，有自己的消息记录。
- **创建 Bot**：`create_bot` 只接受非空职责正文；用途未知先用 `ask_user` 或普通提问补清名字、职责、是否现在建，清楚用途和名字（或授权取名）可直接建，仍是 normal 权限。工具把职责正文写进新 Bot 的人设，成功只返回 `created <name>`。新 Bot 的正文进入自己的固定前缀；紧随的环境消息只公布人设可读标签与短哈希，格式见 [上下文](context.md)。
- **Fork Bot**：原 Bot 写一份交接摘要作为新 Bot 的初始记忆；新 Bot 有自己的职责，对话流从零开始；两个 Bot 自动互相认识；可以选择同时建一个项目群。
- **Bot 之间通信**：同一用户的 Bot 默认互相可见，可以互发消息、建群。群有自己的记录，群里的消息分发到每个成员的收件箱。设有防循环的限制（跳数上限、频率上限）。`message_bot` 的目标写成 `bot@设备` 时，经网关发给主人另一台配对设备上的 Bot，规则见 [网关](gateway.md)。
- **Bot 之间的私聊记录**：Bot 发给另一个 Bot 的消息全部留存，不分成临时和永久两类。正文先进收件 Bot 的收件箱，处理之后留在收件 Bot 的 transcript 里，不另建一份会话。因此这些话一直能被搜索、导出，也能被 `recall_history` 看见。主人跟某个 Bot 聊天时，对方 Bot 发来的内容仍只显示成一行淡淡的提示。要按时间翻看两边的原文，用只读接口 `GET /api/bots/{a}/peers/{b}/messages`，而且这个人必须同时是两个 Bot 的 owner。群里的消息不走这个接口。`{b}` 也可以是另一台配对设备上的 `bot@设备`：只读本机这一半，收到的取自 `bot:bot@设备` 事件，发出的取自 `message_bot` 调用和工具结果（`status` 为 sent/queued/failed），退回通知是 `kind: "bounce"` 的一行；每行都拆出 `from_bot`/`from_device`/`to_bot`/`to_device`，客户端不解析 `bot@设备`。以后可以单独压缩这类来回；现在不压，它们跟这份 transcript 里的其他行走同一套摘要。
- **跨用户**：必须双方授权，授权范围要具体、可以撤销；来自对方的内容一律当作不可信。
- **Bot 档案**：名字、简介、头像（没上传时是按 `avatar_seed` 在浏览器里画的 Notionists 线条人像，旧 Bot 的 seed 就是名字；人像只用冻结的白名单，只有平静和微笑两种表情，见 scripts/avatar。16 像素用只剩脸部的裁切，容器是 `#e4e4e7` 上的圆，深色 `#a8a8ad`。上传失败、seed 是 `glyph`、或脚本没加载时，画由名字决定的几何图形。「我」的头像不变，XBot 的 logo 从不拿来当头像。生成的人像只在浏览器里画，从不经过服务端，服务端不存也不发这类 SVG。编辑资料的抽屉里「换一张」「换一批」「上传」「清除」换头像；Licenses：DiceBear 代码 MIT，Notionists 美术 CC0 1.0，作者 Zoish，见 static/vendor/LICENSES.txt）、简介、选用的人设卡、IANA 时区。没有安静、适中、积极三档；想怎么通知，写在自己的 persona 里（[ADR 0004](../adr/0004-drop-proactivity-levels.md)）。旧档案里的 `proactivity` 还能读进来，被忽略，不进提示，也不再从 API 或网页返回。没有「说话方式」和免打扰：怎么说话写在人设卡正文里；通知在任何唤醒、任何时刻都直接送出。旧配置里的 `quiet_start`/`quiet_end` 读进来丢掉，下次保存就不再写回；旧的 `held.json` 由 serve 的下一轮把每条当普通通知送出后删除。Bot 用 `update_profile` 更新本 Bot 头像/资料，默认调用即落盘，普通资料回执为 `profile saved`，换头像回执再明确「头像已换好；侧栏与顶栏当面刷新」；描述与 Persona Link 引导也对齐当前对话即时刷新，不许推迟到后续唤醒；网页收到工具结果即刷新侧栏与顶栏头像，流式回复期间也生效。可以导出模板（不含记忆）。默认人设和规则要求用主人的语言回答，先调用工具再给结果，不要先用另一门语言讲自己打算做什么。主人没接的提议不要再说一遍。 档案里没有 `style`；旧配置的 language、tone、length、emoji 在首次加载时合成一条该 Bot 的长期记忆，写记忆成功后才裁掉 `style`（[ADR 0020](../adr/0020-voice-in-kernel-preferences-in-memory.md)）。迁移与 SaveConfig 共用 config.json.lock。旧模板里的 `style` 和免打扰字段导入时忽略。
- **人设卡**（`internal/personacard`，[ADR 0026](../adr/0026-persona-cards-replace-voice-and-quiet-hours.md)）：主人一份卡库，`persona-cards/<owner>/<id>/card.md`，第一行 `name: 卡名`，空一行后是正文；正文最多 32 KiB，最多 200 张，id 是 16 位十六进制，只作目录名，界面不展示。Bot 配置的 `persona_card` 是选用的卡 id，空就是未选用、走 `persona.md`。选用后 `ComposedPersona` 每轮从磁盘读绑定和卡正文，是引用不是快照：改卡后用它的 Bot 下一轮就按新的说话。`PersonaText` 与环境流共用 `PersonaSource`：一次 `personacard.Get` 同时读到卡名和当前正文，前缀与模板导出仍由 `PersonaText` 取得正文；环境公布卡名（无卡用稳定标签）加正文指纹，导出的模板带正文快照、不带卡 id。已经加载的旧 Bot 副本保存配置时保留磁盘上的绑定，只有 `SetPersonaCard` 能改它。卡被删或 Bot 不是唯一主人时回到 `persona.md`。导入 Markdown 总是新建：第一行短的非空行作卡名，否则用文件名；同名先 409，确认后加「 (2)」「 (3)」。删卡时有 Bot 在用先 409 报数量，确认后逐个解绑再删；选用和删卡都在卡库锁里（库锁先于配置锁）。旧配置的说话方式迁进记忆时，profile 层满了就写进 log 层，不挡 Bot 加载。新卡库第一次被网页列出时（`GET /api/persona-cards` 先调 `EnsureSeeds`）写入三张内置卡「碎嘴群友」「值班运维」「周末搭子」（`internal/personacard/seeds.go`），种完、或成功新建/导入/改过/删过卡都会留 `.seeded`（删卡在库锁里先留标记再删，所以升级前的旧卡库删光也不补）；有标记的库在同一库锁下检查存量卡：名字仍匹配三张种子且读盘正文逐字等于 `7173d25` 旧薄稿时，用当前厚稿原子写回；已是厚稿、改过标题或正文的卡零写入。单张升级失败保留原稿，继续其余卡并汇总错误，下次列表重试；未标记的有卡库不动，删光也不再补。`.lock` 不算历史（失败的选用/删除也会建它）；有卡但可能正在种时，列表在库锁上等种完再读，失败的一轮会删掉自己写的卡。
- **Bot 自改头像**：`update_profile` 接受 `name`、`description`、`timezone` 和 `avatar_seed`，仅修改当前 Kernel 的 Bot，可组合更新。默认 permissions off 调用即落盘，没有头像预览确认卡例外；permissions on 按既有 SelfEdit 权限表审核。summary 带 `avatar_seed "…"`。`ValidAvatarSeed` 在写入前校验：去首尾空白后 1–80 个字符，不含控制字符及 `<>"'&` 和反引号；非法 seed、非字符串或未知字段返回工具错误，不出卡。执行时复用 `SetAvatarSeed`，保存 seed、清除上传图及 MIME，头像状态改为 `generated`。换一批就是设置新 seed，习惯 `名字#n`（如 `XBot#3`），与编辑资料一致；没有跨 Bot 参数或上传图片参数，不增加出图工具，也不改变头像风格。[验收设计](../dev/bot-self-avatar.md)。
- **运行档位**：档案里的 `role` 是 `chat` 或 `dev`，决定这个 Bot 默认能干多久。`chat` 是聊天型：后台一件事最多 45 分钟。`dev` 是工程型：后台 2 小时。空的或不认识的值按 `chat`。`xbot profile set --role chat|dev` 设置，`--background-max-time`（分钟）可以单独覆盖，写 0 保留档位默认值，负数关掉这项限制（任何负值都归一为关闭）。主线没有工具次数或秒数上限（[ADR 0028](../adr/0028-no-foreground-background-split.md) 删掉了 `foreground_max_tools`/`foreground_max_time` 和对应的命令行参数；旧 `config.json` 里留着这两个键也照常读，不再生效）；「多久之内回答主人」靠规则里的「先回人再干活」，后台上限才是「一件事能干多久」，所以步数不是可配项：内核里只有一个防呆常量（1000 步），正常任务撞不到。`serve` 和 `chat` 都按这里算出来的上限跑。注意 `xbot fork --role` 是新 Bot 的职责描述（persona），和这里的运行档位不是一回事。
- **定时例程**：五段 cron，最小间隔 5 分钟；或者一次性的 `at`（RFC3339）。cron 按电脑本地时区解释（文件里 zone 为 local；旧文件没有 zone 视为 UTC，serve 启动时自动迁移，触发时刻不变，先备份到 routines-backup-<时间>，可用 `xbot routine migrate --dry-run` 预览）。夏令时重复的那一小时只触发一次、跳过的时刻不触发。一次性例程不受最小间隔限制，到点后自动停用，错过 2 分钟窗口就停用、不补发。serve 里定时器每 15 秒单独检查一次并把事件投进收件箱，不等 Bot 的模型调用结束；处理事件仍在下一轮 serve 处理。回滚到 C1 之前的版本时，要把 routines-backup-<时间> 还原回 routines，否则旧版本会把本地时间的 cron 当成 UTC。纯排期的创建、修改、暂停、删除由 Bot 自己做主；带前置命令的变更还须经过 [shell 审核](security.md)。停机或睡眠超过 2 分钟后，下一次检查给每个 Bot 发一条汇总事件（serve 和 chat 轮询共用 internal/proc 水位线，ID 按水位线确定、可去重，最多看 7 天，首次运行不发），列出错过的时间点，不补跑。
- **例程前置检查**：`Routine.precheck` 是可选的 shell 命令字符串，缺字段、空字符串或纯空白直接按原路投递。非空时在投递前复用现有无 Jobs 的 `sh -c` 硬超时路径：命令最多 16 KiB、2 秒执行预算，工作目录沿用进程 cwd，stdin 关闭，stdout/stderr 保留 0 字节；供应商密钥按 shell 的现有环境过滤规则剔除，实时机器 gate 的有效模式为 ask/off 或读取失败时不执行。退出码 0 才投递原 routine Event；非 0、超时或启动失败静默跳过，但仍 Commit LastSlot，一次性例程同时停用，不发通知。`Fire`、serve 和 chat/proc 共用 `Timer.Deliver`；事件 ID、正文与 Inbox 去重不变，投递失败仍不提交 slot、下次重试。并发轮询仍可能重复执行检查命令，应使用无副作用的检查；唤醒按原稳定事件 ID 去重。`routine_create/update/list` 可设置、清空和查看；更新省略保留，显式空串移除。创建/修改的前置命令复用 [安全](security.md) 的同表 shell 审核，不能靠排期 Normal 分类放行。HTTP 字段见 [API](../API.md)。
- **Bot 状态**：内核从当前这一轮直接推出状态，不调模型。`BotStatus` 有 `bot`、`state`（`idle`、`running`、`awaiting`、`offline`）、`tool`（最近一个工具）、`since`（RFC3339，本机时区）、`backgrounds`、`last_message`（空闲时最近一条主人或助手消息，最多 80 个字符，写前过替换函数）、`todo`（有活动待办清单时的一行摘要，见 [内核](kernel.md)）、`node`（本机为空）、`stale_seconds`（只有缓存过的远端点掉线时才有）。`awaiting` 表示这一轮停在等主人确认的那一步。内核的每一轮还留一个最多 20 条的步骤环 `{time, tool, summary≤120 字符, state}`，`state` 是 `running`、`done`、`error` 或 `awaiting`。`GET /api/bots/status` 和 `GET /api/bots/{name}/steps` 见 [ui.md](ui.md)；Bot 之间用 `bot_status` 只读地看同一个主人的 Bot。没有活内核的 Bot 是 `idle`，`last_message` 只读 transcript 尾巴。状态接口会被轮询，所以尾巴的原文按 `TranscriptStamp`（最新分段的文件名、大小、mtime，不加锁、不开索引）缓存在进程里：任何进程追加都会让分段变大，签名一变就重读；缓存只存原文，脱敏在每次返回时做，之后加进保险箱的密钥照样被替换。5 个 Bot 时 `/api/bots/status` 的 p95 从 2–8ms 降到约 0.45ms（macOS 本机）。
- **跨用户的状态可见性（M8）**：默认只看得到别的主人的 Bot 是「空闲 / 在忙 / 稍后回复」，看不到工具名、步骤或消息。要更细，得这个主人按联系人逐条开启，开启后也只给一行状态，永远不给步骤。对方主人写下的东西一律当不可信数据看。这条在 M8 实现。


### 技能启停持久化

`DisableSkill` / `EnableSkill` 在现有 config.json 文件锁内读取最新配置、修改 `disabled_skills`、原子保存，重复调用不增加条目。其它配置保存保留磁盘上的技能开关；内核列举与读取技能实时读取这份名单，因此已加载的 Bot 也会感知自装范围和设置页后续启停。

## 人设组成与提示词边界

| 字段 | 存储 / 谁写 / 变化 | 模型入口与界面 |
|---|---|---|
| 名字 | bots/<name>/config.json 的 name；主人或 Bot，少变 | 档案、环境 paths 中的 Bot 目录可识别；不另复制名字提示；编辑资料 |
| 头像 | config 的 avatar/avatar_seed；上传在 avatar、avatar.mime；主人或 Bot，少变 | 不进提示；编辑资料 |
| 标签/分组 | 没有独立 tags；侧栏分组用 config.group（Bot.SetGroup）；主人/已有分组工具，偶变 | 不把分组注入人设；侧栏 |
| 简介 | config.description；主人或 update_profile，少变 | 只给人看，ComposedPersona 不注入简介；环境仅留已有 profile 指纹，不含正文；编辑资料 |
| 职责/人设 | 已选 persona-cards/<owner>/<id>/card.md 正文，否则 bots/<name>/persona.md；主人定边界，Bot 沿既有修改入口改，少变 | 进入 S0 人设块；人设卡/创建入口。卡是引用，不另抄进记忆 |
| 工作档案 | bots/<name>/memory/memory.md 和 log/YYYY-MM.md；Bot 对话中更新，常变 | 常驻事实与近期日志快照、后续环境差异；详情记忆。只记学到的现状、规则、决定，不复制或覆盖职责，无第三文件 |
| 说话风格 | 有效人设正文；旧 style 已迁入 Bot 记忆 | 随所在块，不新增风格字段 |
| 技能 | skills/<slug>/SKILL.md；主人导入，Bot 可起草；config.disabled_skills 按 Bot 停用 | S2 目录，read_skill 按需正文；技能页 |
| 连接器/工具 | config.links、MCP 配置/目录；主人授权 | Links 的 S1 及已装目录的 S2；工具 schema 按需，连接器页 |
| 运行档位 | config.role = chat/dev；主人，少变 | 内核预算使用，不注入人设；CLI profile set --role 帮助称运行档位 |
| 时区 | config.timezone；主人或 update_profile，少变 | 当前时间只在环境流；不拼人设 |
| 免打扰 | 已按 ADR 0026 删除；旧字段忽略后移除 | 无提示入口、无界面，不恢复 |

`fork --role` 保留 flag，但帮助称职责；`profile set --role` 是运行档位。工作档案边界通过跟版 MemoryWriteRule 提醒；升级规则不改主人职责或记忆文件。
