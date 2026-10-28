# 记忆

长期资料默认写入本 Bot 的 `<root>/bots/<bot>/memory/`；可选 `<root>/memory/` 仅存跨 Bot 事实，存在时才在环境里的 `reference` 字段给出路径。用文件工具维护自由 Markdown：根层 `description` 进索引，`defaultopen: true` 带全文；子目录只列入口，正文按需读取。Bot 库的 `memory.md`、`log/`、索引和备份不进资料索引。每个来源超过 256 KiB 时整源索引排除并说明。`keywords` 按字面命中用户或模型文本时提醒路径，每轮路径去重。索引变化走环境流，不变不重复发送；压缩不写记忆或经历。Bot 和 person 的既有记忆继续注入。

- Bot：`<root>/bots/<bot>/memory/`，Bot 工作记忆和默认资料库。职责与边界在有效人设里（选用的人设卡正文，未选用时是 `persona.md`）；学到的工作档案复用 Bot 记忆，不双写职责、不让记忆覆写人设。
- person：`<root>/people/<id>/memory/`，旧的用户事实记忆，过渡期保留。
- `memory.md`：常用事实，每个范围上限 4 KiB。Bot 常驻事实及最近 8 条日志在初始/压缩后的记忆快照中，后续变化走环境流；人物资料随当前说话的人进入环境流。
- `log/YYYY-MM.md`：过程与有日期的事件，默认向本地当月追加 `- YYYY-MM-DD HH:MM [对话] 内容`；更正写新行，不改旧日志。普通文件工具写入受格式及当月 64 KiB 上限校验。其他细节可放同一记忆文件夹的自由文件，按需用 `recall` 搜索，再 `read_file` 读。
- 经历：`<root>/bots/<bot>/experience.sqlite`，记「这段时间发生过什么」。Bot 自己调 `experience_record` 记一两句，内核补时间并锚到 transcript 序号（默认从最近一条用户消息到当前），同时存一段不含工具结果的原文段提纲；新经历在下一轮环境流里以 `experience` 字段出现。`experience_search` 返回经历摘要和原文段提纲，`detail` 按 seq（或经历 id 加下标）读单条消息全文。压缩不写经历，漏记了就自己翻 transcript。

用户可以在 Bot 详情的「记忆」中查看，也能用文本编辑器打开上述 Markdown。`<root>` 默认是 `~/.xbot`，显式 `--root DIR` 会改变它；`<bot>` 是 Bot 名，`<id>` 是人物标识，不猜成显示名。当前实例的目录以环境中的 paths 和记忆标题给出的路径为准。手册不读取用户文件，不证明某条事实已经保存；需要核实时读文件或检索。
