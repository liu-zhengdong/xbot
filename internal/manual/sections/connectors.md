# 连接器

非 MCP Link 仍以出站工具、入站事件接入。`links search` 查目录，`links install/remove` 改安装，非 MCP 的 `links load` 列出工具 id，再经 `tools` 的 search/describe/call；安装不等于实际请求已经成功。

MCP 用当前实例 CLI：Shell 中先运行 `"$XBOT_BIN" mcp --help`，然后 `mcp list --self --json`、`mcp tools <服务器> --self --json`、`mcp describe <服务器> <远端原名> --self --json`、`mcp call <服务器> <远端原名> --args '<JSON object>' --self --json`。服务默认全局配置，显式 `--scope bot` 是当前 Bot 的整条覆盖；调用总用有效配置。MCP 不再注册模型 schema，不用旧 `mcp_` 工具名或 links load。research 只允许当前实例的读取命令，call 另需 `--readonly` 且远端当前工具明确只读。

人用终端显式给 `--http <本机地址> --bot <名字>`，复用本机主人会话；文件配置用明确 `mcp offline list/add/remove <bot>…`。普通在线命令不隐式改客户端本地配置。`--args-file <文件或->` 与 `--args` 互斥，只收单JSON对象；默认工具调用随调用方context，timeout显式缩短。

结果是有界 JSON/摘要与工作区相对文件引用；`access:local` 只表示本机可读。文本、图片、未知块和完整原结果保在本地产物，resource_link只给URI不会自动抓取。`is_error` 或结果投影错误保留可用内容，同时命令非0；不要重跑未知结果的写操作。Telegram、飞书等外部消息仍通过原收件箱路径，MCP通知也保持原不可信事件/唤醒。

用户可在连接应用设置管理凭据和安装。只称密钥名字，不输出值。技能按需执行 `"$XBOT_BIN" skill read <id> --self --json`，写入先看 `skill --help`；省略 scope 对所有本地 Bot 生效，`--self` 只定位发起方实例。正文过长返回当前工作区文件，`show` 仍是离线原文件读取；是否就绪、是否外发成功以当前状态和实际回执为准。
