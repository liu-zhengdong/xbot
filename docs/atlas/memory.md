---
kind: part
parent: README.md
code: [internal/memory/, internal/person/, internal/experience/]
verified: 2026-11-02 @ dcbf05fd (compact Bot snapshot-only refresh, latest published person preservation and unchanged memory writes)
---
# 身份与记忆

> **30 秒版**：人是跨 Bot 的全局身份；记忆分人和 Bot 两层，以 Markdown 加索引存放。

机制说明由 `xbot manual memory` 与模型经 `xbot manual memory` 共用同一 embed 正文，固定称 `memory.md` / `log/YYYY-MM.md`。职责与工作档案的字段边界见 [多 Bot](multibot.md#人设组成与提示词边界)。

## 可见记忆文件检索

`memory-recall` 复用原 Run 就近提取的私有 `k.recall(ctx,recallInput{Query,Scope,Since})` 唯一文件扫描器，不是结构化事实/索引的 `Memory.Store.Recall`。Scope 仅精确空串、bot、person；空串先 Bot 后可见 person，共享 2000 文件、总 4 MiB 读取预算，Bot 用尽则 person 不扫描，不另报人物无目录或无命中错误。query 可省略、空白或无 token，沿原 BM25 返回 no matches；只检索正文行，不把 path/scope 当查询正文。汉字按重叠双字分词。

人物只取原 Shell prepared 冻结 turn 的 `memoryPersonOf`：KindBot/KindGroup 优先无 person；SourceSubagent 或 `sa_` sender 沿父 Bot 首个非空 Owner；其它用原 sender。trusted 不决定人物范围，Local child 的 review policy marker 也不替代其实际 Kind/Source/Sender，不保证人物为空或取首 Owner。领域入口先要求原同 listener 的 live Local/running Shell context 和明确非 nil 冻结 turn，不能用审批重绑、HTTP ctx 或最新 turn 代替；不新建人物选择服务或 person/project 授权表，见 [内核](kernel.md#可见记忆文件检索实例入口)。

Scope/Since 不 trim/casefold，Since 非空按原 YYYY-MM-DD/`time.ParseInLocation(...,time.Local)`，用进程本地时区而非 Bot profile 时区。memory.md dated 条目与 log/*.md 按原行日期，自由文件按 mtime。递归 `.md`/`.txt`，跳备份名 `.bak`、symlink、非 regular 文件与原空行/标题/暂无行；每文件最多 256 KiB，单文件截断不置 incomplete，只有共享文件数或总字节 cap 置位。读取在一次 currentIO 内直接用 current Bot Root/Name，规范 root、memory 祖先 resolved==dir、scope `.lock`、Walk/Open 与 inode 核对同在 route；避免旧名等待锁的 MkdirAll 在改名后复活旧 Bot 目录，PersonDir 本身不搬。扫描无模型/网络，原 Walk callback 检查取消，锁等待可能延迟取消，不承诺新增即时取消或 deadline。

结果保原排名前 20 条，每条 `scope:相对路径:行号` 加正文；正文最多 200 Unicode runes，超出加 `…`，保剩余条数/incomplete/no matches 原词法。扫描器自身无总输出 cap 或 redactor；source 回包前沿原 secret 遮盖，再按准确 JSONNL/human 双预算内联或将完整这份 Text JSON 写当前工作区产物，没有经历检索 6 KiB cap。产物保二十命中投影，原文件全文仍用 `read_file`，不冒全库导出。字段、1 MiB/32 KiB/64 KiB/20 MiB 边界仅在 [API](../API.md#可见记忆文件检索运行时-cli) 维护。唯一 recall wrapper/Known/schema/index 与空 memory 工具注册组已退，历史 native 字面、Memory 器官/Link/Event、文件写入 4 KiB 守卫保留，空 context 工具注册组已退，compact 与三读沿 source CLI；检索不新增环境事实或记忆回执，决定见 [ADR0059](../adr/0059-memory-recall-runtime-cli.md)。

## 经历锚点与对话原文

经历搜索仍只返回摘要/原文段提纲，完整消息由 source Shell 的 `"$XBOT_BIN" transcript detail --self --seq N` 读取：普通 seq 是 transcript 的 1 基序号，加正 `--experience ID` 后 N 为该经历 From 起的 0 基 offset；experience≤0 保普通 seq。仅 source 可读 Detail，不接 bot/人物/project 字段，保原经历范围与错误、source 本地格式时间、附件路径和 toolBlob，以及原语义上限，不承诺无限 blob 或附件字节。原导出 Detail 方法未改，命令、字段和容量见 [API](../API.md#对话原文三读运行时-cli)。

ExperienceRule、SearchExperiences 末尾 detail 提示、机制手册与 live Link.Context 已指向同一 source CLI，序号仍是只追加 transcript 的稳定锚点。记录/检索的 SQLite 七列、Web 全库字面分页与提纲/raw segment 消费者、下一自然 experience 环境采样保持；不新加经历存储、环境账、Inbox/waker、MemoryReceipt 或假工具配对。三读不需要 memory-recall 的 prepared-person/CallID 门，也不扩它的限定闭合评测桥。三读 wrapper/Known/schema/index 已退，空 context 工具注册组已退，compact 沿下文 CLI，Link.Context 器官/Tools 空壳与 Event、两层记忆和文件写守卫保持，旧 native 字面/工具配对记录继续可读。决定见 [ADR0060](../adr/0060-transcript-read-runtime-cli.md)，实例接线见 [内核](kernel.md#对话原文三读实例入口)。

## 压缩时刷新记忆快照

主动 `compact --self` 沿原 CompactRange 更新摘要块与 State.MemSnap 投影，不写 memory.md、log 或 experience 正文。refreshMemSnap 在 envMu 内先读当前 State/有效环境，personID 为空读取 Bot 记忆，沿原 TryRecall/锁 fallback；忙且已有 MemSnap 时保旧快照。随后完整环境按最新已公布 source 重采当前人物，不用旧 caller 的人物覆盖之后的新输入；环境 snapshot 先 append，再保存覆盖 State。两层记忆、人物可见性及原文件写守卫保持，决定见 [ADR0061](../adr/0061-compact-runtime-cli.md)。

原 prepared Shell ctx 只保模型取消与 turn 用量，compact 不加 Recall 专用人物门；Memory 的 currentName→Recall 分步、profile 读 k.Bot 等旧接缝保持，不声称全流程改名原子性或底层零写。后期失败可能已有用量/环境效果，不增记忆回执、环境账、Inbox/waker 或回滚；回包只为原短 ACK/实际错误，没有摘要正文或产物。context 四工具均已迁 source CLI，空工具注册组退役，Link.Context/Memory 器官和旧 native 历史仍在。命令/容量见 [API](../API.md#主动范围压缩运行时-cli)，调用与保存边界见 [内核](kernel.md#主动范围压缩实例入口)。

## 边界
负责：人的身份映射、两层记忆、冲突规则，以及启动时把旧的 Bot 与人目录并进 Bot 记忆。不负责：多台电脑之间的配对与消息（见 [网关](gateway.md)）；记忆不在机器之间同步（[ADR 0012](../adr/0012-retire-node.md)）。

## 怎么运转
- **人**是跨 Bot 的全局身份。各个 Link 负责把外部账号映射到同一个人。
- **记忆分两层**（[ADR 0006](../adr/0006-two-memory-scopes.md)）：
  - **人（person）**：所有 Bot 共享。只放每个 Bot 都需要的、关于用户本人的事实：时区、语言、日常作息、他是谁。每条注明是哪个 Bot、什么时间写的。模型用文件工具编辑标题给出的人记忆路径。
  - **Bot（bot）**：这个 Bot 自己的记忆：学到的工作现状、和用户约定的规则、项目决定。模型用 `write_file`/`edit_file` 编辑记忆文件；不可信回合的文件写入须用户确认。压缩不写记忆：不抽事实、不追加日志或 episode（[ADR 0021](../adr/0021-env-stream.md) 决定 6，2026-10-27）。工作规则和项目决定不进 person。Bot 记忆对每个发送者都可见。

- **不再有 Bot 与人这一层。** 单用户系统里三选一没有默认，一个开发 Bot 把 19 条开发规则写进了共享范围。以后只有真的要多人使用时，才加回按人分开的范围。
- **冲突规则**：关于人本身的事实，以最新的为准；关于怎么做事，以 Bot 自己的记忆为准，压过 person。同一个键上，最新的那条赢；不可信出处只做标注，不改变取舍。
- **存储方式**：常驻层用可直接编辑的 memory.md（含 ## 索引和带日期的条目）加 memory.index.json，日志层用 log/YYYY-MM.md，每行 `- YYYY-MM-DD HH:MM [source] 文本`，本地时间、按时间排序，无索引 JSON。旧 log.md 与 log.overflow.md 合并到月文件，按分钟、渲染来源和完整正文逐份匹配，保留真实重复记录与出处标记，不截断迁移正文；零时间按 Unix epoch 保存，原文件及索引留作 .bak（冲突用 .bak.N）；中断可重试，未迁移时读取报错。迁移先合并并存的 profile.* 与 memory.md、恢复旧索引元数据，再将 profile.* 留作 .bak 备份；失败而仅剩 profile.md 时读取报错。手改 Markdown 按文本恢复元数据，日期只用于同文条目消歧，索引条目只匹配一次；仅 person 条目解析 via 作者，无索引时按范围补 kind 并识别出处标记。常驻层每次进上下文并有大小上限，日志层按需检索。
- **文件写入守卫**：内核对两个范围的 `write_file`/`edit_file` 持目录锁校验并在违规时恢复；`memory.md` 超过 4 KiB 只许缩小，日志只许向本地当月追加合法行且不超过 64 KiB，索引与锁文件禁止写入，自由文件不限大小。逻辑路径或真实路径落入记忆目录均受保护，拒绝记忆目录本身的文件写入及目录内（含目录自身）的符号链接。不可信回合须用户确认；仅成功的常驻与日志文件改动跳过下一轮差异，回滚失败明确提示路径可能残留被拒内容，快照标题给出文件工具路径。目录根上由存储自己管理的名字（`.lock`、`sync.jsonl`、旧布局的 `log.md`/`log.overflow.md`/`profile.md`、`*.index.json`）一律拒写；`memory.md` 只收存储能读回的三个标题、一个索引段和 `- (YYYY-MM-DD) 文本` 条目；在 macOS 和 Windows 上按大小写不敏感判定（`MEMORY.md` 就是 `memory.md`）。子代理的文件工具同样过守卫，但它的改动算外部改动，前台下一轮能看到差异。不可信回合里写到记忆目录的 `shell`（含先 `cd` 再写）也要用户确认，纯只读命令照常执行。
- **慢写期间的环境读取**：新一轮直接复用已有的固定前缀，不为读取一个不会使用的新版本等待慢写；压缩时该范围正忙也保留原前缀，之后继续公布有效对话的差异。环境采样复用记忆目录的非阻塞 `TryLock`；该范围正在提交时，保留有效对话已公布的值，真实写入仍受原目录锁、校验和回滚保护。完成后通过原结果回执和环境回放公布差异；不加另存的记忆版本，接收新话不等慢工具。
- **人的记忆只在本机**：人的身份和人的记忆是每台安装自己的存储（`people/<id>/identity.jsonl`、`people/<id>/memory/sync.jsonl`），不在机器之间同步；跨节点同步已于 2026-10-18 随旧的「节点」一起退役（[ADR 0012](../adr/0012-retire-node.md)）。这些日志是本机的事实记录，往模型里注入时仍然只当数据。
- **容量劝导与轻量沉淀**：走内核已有提醒 Register 缝，门槛/冷却与行为测试见 [内核：记忆提醒](kernel.md#怎么运转)，不另造容量账或自省模型轮；原 `ErrProfileFull` 与超限不落盘、不截断的后盾不变。
- **常驻上限**：每个范围的 profile 约 4 KiB；默认键事实注入为 `- text`，自定义键保留 `- key: text`，person 范围追加 ` via X`；写入和迁移按 memory.md 的实际渲染字节数加每个自定义键的 `len(Key)+2` 计量，文件格式不变。Bot 快照和人物环境资料分别沿原 4 KiB 预算。写超了就报错，要求合并事实、忘掉过时的、或把细节挪到日志层，不静默截断。只拦会让文件变大的写：已经超限的文件（上限之前写的，或同步合并进来的）照样能删减条目，一条一条整理；日志层同样只拦变大的写。同步合并不受 profile 上限挡，免得整次合并卡死。注入只有越界才裁，并注明该整理。
- **Bot 交接派生的内部 Remember**：`bot-fork --self` 沿原 source Memory 实例，将交接 OneLine/split960 后逐条写 child 的 ScopeBot/TierProfile（handoff、handoff-2…），再写 child fork-source、source fork-NAME；person store 直接共享、不复制。handoff 输入直接 Recall 仅源 Bot profile，但 projected 历史仍可有人物内容，不承诺人物绝不进交接模型。child 目标固定/作者取后写时当前 source，源认识事实在一次短 current IO 内以当前 b.Name 同作 BotName/WrittenBy 直接 Remember，不先取旧名再迟写；不嵌套 current helper或加全局记忆协议。原 saveTier 先 atomic Markdown，再 atomic index，随后 noteSync，任意 error 不能断言正文没写。fork短回执的 handoff_parts 仅原 Remember nil ACK、total为实际960切分数，当前失败以 uncertain_memory{bot,key} 指明结果未知；ACK不是当前正文必仍在的承诺。原8KiB交接摘要可超过4KiB profile，真实 child与若干parts已成后仍会遇到容量晚错，不暗剪摘要、抬cap或回滚/重试。handoff/认识事实由child下一wake记忆组装及Web记忆面板直接消费；无复制transcript/blocks或专用fork历史parser。完整回执见 [API](../API.md#bot-交接派生运行时-cli)，源模型与改名写点见 [内核](kernel.md#bot-交接派生与原-source-模型链)。
- **注入：快照加差异**（记忆 v2 批 1，已落地）：
  - 写入规则在 `<memory>` 块里紧随 `MemoryRule` 注入，不依赖已保存的 `rules.md`；渲染版本 3 使旧 Bot 下一次唤醒重新拍快照。
  - 渲染版本变化时重建 Bot 快照，不把格式升级当记忆变化。会话开始及自动压缩后，把 Bot profile 加最近 8 条 Bot 日志拍成一份 State 快照，固定作为第二条消息；人物资料不进入它。换发送者不会换前缀。
  - 每轮从固定 Bot 记忆前缀与有效对话中的环境消息还原已公布事实，再和当前记忆比较。变化用同一可回放环境格式完整追加，人物资料也是其中按人更新的一项；不再存 EnvRecord、MemSeen 或人物已见版本。非人唤醒只报来源，不提人物或清人物基线。格式与压缩补快照规则见 [上下文](context.md)。
  - Bot 自己的成功文件改动在原工具结果里附可回放的记忆增删或人物资料回执。回放原调用及其回执后，不再把自写当外部提醒；失败或回滚不发回执。别人后来的修改、撤销自写仍按实际差异报告。没有内存自写队列或 state 中的第二份账；最近日志离开窗口不算外部删除。
  - 不再保留按人停放的快照。定时器、子任务和其他来源沿用同一份 Bot 前缀；来源与人物变化各自追加进共享对话，自动压缩后沿原快照路径让被压缩的人物版本失效，当前人立即恢复、其他人下次出现恢复。不同人的资料标明身份，但共享 transcript 不承诺人物隔离。
- **旧数据**：每次进程启动都重新走一遍 `bots/*/people/*/memory`，每个数据根每进程一次。没有持久化的迁移标记；迁过的目录已改名为 `memory.bak`，所以重复走是幂等的。`config.json` 缺失、读不了或不是合法 JSON 时，这个 Bot 的旧目录留在原地。`owners` 为空才算没有用户，旧事实只留在 `memory.bak`。只合并 owners 里的人，其他人的目录改名为 `memory.bak` 并记日志，读不读得动都挪走。用户的目录读失败才留在原地。不让 Remember、Recall、Forget 或启动失败。合并去重，保留时间戳、来源和原来的 kind。profile 放不下的改记日志并写一条说明；说明同样按日志身份匹配，防止中断重试重复写入。person 日志的 Forget 先折叠同步记录，只删除当前仍匹配的键，不影响同键后来更新的值。迁进日志时按月落盘，不受大小守卫限制。普通写入只在当月超过 64 KiB 且继续增长时拒绝，提示压成几条整理记录；同步折叠绕过守卫。无文本查询从最新月份读够 200 条即停，文本查询读全部月份；快照总条数单独按行计数。人的同步日志仍是 person 通道。
- **只读检索**：Agent 在原 source Shell 使用 `"$XBOT_BIN" memory-recall --self [--query TEXT|--query-file FILE|-] [--scope bot|person] [--since YYYY-MM-DD] [--json]`，命中后用 `read_file` 读全文，大结果读完整 file。空 scope 合并 Bot 与原调用人物可见目录；Bot 是写入默认，不能据此把检索默认收缩为 bot。命令与回包集中见 [API](../API.md#可见记忆文件检索运行时-cli)，扫描与人物语义见 [可见记忆文件检索](#可见记忆文件检索)。
- **文件夹记忆**：[ADR 0008](../adr/0008-memory-v2-dated-log-first.md) 和 [ADR 0009](../adr/0009-memory-as-folder.md) 的两层布局由普通文件工具维护，快照加差异沿上述路径；可见文件检索现沿独立 source CLI，[ADR0059](../adr/0059-memory-recall-runtime-cli.md) 替代其原 native 执行入口。记忆 v2 的原始规格见 [记忆 v2 规格](../MEMORY-V2.md)。
- **面板读取**：装配仍直接读 person / bot 的 `memory.md` 原文，不经 `Recall(TierProfile)` 拆条；页面只展示 Bot 常驻。经历的 `Store.Page(limit,before,query)` 在完整库对摘要、关注点、提纲做字面包含过滤（英文忽略大小写），然后按记录时间和 id 分页，不新增索引或影响模型侧的 BM25 搜索；空查询沿普通分页。Web 装配与行为见 [界面](ui.md)，不改变快照、人物资料注入或落盘模型。
- **经历存储**（[ADR 0030](../adr/0030-experience-and-reference.md)）：每个 Bot 一个 `bots/<bot>/experience.sqlite`，七列 `experiences(id, created_at, summary, focus, from_seq, to_seq, segment)`；它是本 Bot 的经历库，与 person/Bot Markdown 记忆、共享资料工作区和全局 DuckDB 教训库分开，不增加 person/project scope。首次记录才建库；缺库的 Recent/Latest/Get/Page 经 exists 直接返回，已有库读取仍沿原 open，可建表/WAL，不承诺全 root 零写。
- **记录与锚点**：Agent 用 `"$XBOT_BIN" experience record --self --summary TEXT`，完整 file/stdin 与可选 focus/from/to 见 [API](../API.md#经历记录与检索运行时-cli)。原 summary trim 后非空≤2 KiB，focus 仅 trim、不加独立 cap。0 默认取操作时 currentCount 与截至 to 的最近 200 行里最新非 Internal/ForkMark 的 RoleUser，找不到取尾，空 transcript 可 0/0；不限定新 owner 身份，不借 prepared turn 替换，也保原负 to/nullSeq 宽容。segment 从范围前 200 行取提纲，每行 160 字节、合计约 600 字节，跳内部/分叉标记/工具结果，保 `user:` / `assistant:` / `[tools: names]` 词法，不调模型。原 Store.Add 一次 INSERT 后取 LastInsertId，nil 才回 `recorded:true`；任意 error 为 null，保原 Entry 的实际 ID/range，不能推断库或行没写，不回滚、去重或自动重试。
- **检索与全文**：`"$XBOT_BIN" experience search --self [--query TEXT]` 空白 query 列最新，非空原文在最近 2000 条 summary/focus/segment 上 BM25；limit≤0 或>8 回默认 8。原 presentHistory 先脱敏再 clip 至 6 KiB，返回原时间/seq/detail 提示文本，不反解析 rows/count，也不套 Web 的 512 字节 q。准确 JSONNL 或 human 任一超 Shell 32 KiB 时，当前工作区存完整该语义 Text JSON 的 0600 产物；不补回原 6 KiB 已裁记录。`transcript detail --self --seq N` 保原 raw seq 1 基/正经历 ID+0 基 offset，读实际消息与 blob 并经原脱敏及语义上限；详见 [经历锚点与对话原文](#经历锚点与对话原文)。命令、nullable ACK 与产物字段仅在 [API](../API.md#经历记录与检索运行时-cli) 维护。
- **经历消费者与环境**：Web Page 仍对全库 summary/focus/segment 做 ASCII 大小写不敏感的字面子串过滤，按 created_at+id 分页；页面按原提纲词法作阅读投影，并可看原始 segment。环境的 experience 仍为 count 与最新 `#id 摘要`（摘要截 200 字节），新记录在下一自然采样沿原差异流追加，读错保已公布旧值；不额外 Inbox/waker、MemoryOwnReceipt、假 native 调用配对或普通事实账。ExperienceRule 是常驻轻推，当前指引使用 record/search self，并用 transcript detail self 读取锚点；压缩不写经历。两旧经历执行 wrapper/Known/schema/index 已退，历史 native 字面保留；可见文件检索已迁独立 CLI，已空的 memory 工具注册组已退，`link.Memory` 器官及原 Tools 空壳保留。入口决策见 [ADR0058](../adr/0058-experience-runtime-cli.md)。
- **资料库**（[ADR 0033](../adr/0033-bot-and-shared-reference-workspace.md)）：默认 `bots/<bot>/memory/`，整机共享工作空间 `<root>/memory/` 存跨 Bot 事实；采样时创建两目录，空库也注入契约。环境流 `reference` 先注入 Bot 契约及根层索引，再注入共享契约及索引。根层 Markdown 列文件名和 `description`，`defaultopen: true` 才带全文，子目录只列入口；Bot 库的 `memory.md`、`log/`、索引、旧系统文件及备份跳过。各来源根层文件或索引超过 256 KiB 时只排除该来源索引并说明，不截断；变化才发布，压缩后环境快照重新公布。内核只建这两个目录和读取，文件工具写资料；旧 Bot/person 记忆仍沿原路径注入。每轮一次 sidecar 轻推给出两条绝对路径，默认写 Bot 资料、跨 Bot 才写共享；两源 `keywords` 按英文大小写不敏感、中文原样的字面子串匹配用户或模型文本，只提醒路径，单轮路径去重，已进入该源根层索引的不重复提醒。关键词扫描最多读 4 MiB，每文件最多读 64 KiB，单步路径最多 1 KiB；压缩不写记忆或经历。
