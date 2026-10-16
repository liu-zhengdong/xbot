---
kind: part
parent: README.md
code: [internal/vault/, internal/review/]
verified: 2026-10-16 @ 5d3f39c
---
# 安全：保险箱与权限表

> **30 秒版**：Bot 只知道密钥的名字，出站代理才换成真值；是否要主人确认只由内核里的一张权限表决定，只确认对外发送、删除、带密钥或非 GET 的 HTTP、persona/profile 自改。

## 边界
负责：工具环境、保险箱、出站代理、脱敏、权限表、决定留痕。不负责：机器开关的配置（见 [机器与设备](machines.md)），但开关在这里的 review 路径生效。

## 怎么运转
- **工具环境**：shell 和文件工具仍可访问这台机器，主人本来就是为此授权的。进程读完供应商 API key（`XBOT_API_KEY` 和各 provider 的 `api_key_env`）后就把这些变量从进程环境里清掉，包括启动时留在 `/proc` 环境块里的那一份，只留在内存里。没登记的变量（例如 `OPENAI_API_KEY`）仍留在环境里。命令参数 `keys` 为 true 时，这次调用从内存把登记过的 key 放回环境，并且要主人确认。
- **保险箱**：Bot 只知道 key 的名字。内核的出站代理在请求发出时把 `{{secret:名字}}` 替换成真值，并且只允许发往绑定的域名。key 由内核注入到相关进程中，这些进程和 Bot 的 Shell 隔离。工具输出中出现真值会被脱敏。
- **危险操作审核**：执行前审查命令本身。
- **代表主人对外发消息**：默认先出草稿，主人确认后再发。

## 保险箱与出站代理
钥匙只在主人这一侧。Bot 和模型只看得到名字，出站时代理才把 `{{secret:名字}}` 换成真值。

- **存储**：每个 Bot 一份保险箱目录 `bots/<name>/vault/`，目录 0700，所有文件 0600。内容用 AES-GCM 加密，密钥挪到 Bot 工作区之外的数据根 `<root>/keys/vault/<bot>.key`（目录 0700，文件 0600）。旧版密钥仍和密文同目录时，`Open` 会在文件锁下把它移到新位置（跨设备则先写 0600 再删旧文件），绝不因迁移另造新钥。Bot 改名时密钥文件跟着走，删除 Bot 时删掉。
- **保险箱路径一律确认**：`shell`、`read_file`、`write_file`、`edit_file` 触到本 Bot 的 `vault/`、`<root>/keys/vault/` 或任何 `bots/*/vault/` 时，无论机器开关是不是 `allow`，都出确认卡（摘要带「触及保险箱/密钥」）。判定是尽力而为：文件工具按解析后的路径，shell 把命令按词解析、对 cwd 取绝对、清理并解析符号链接后比对。这只是防随手读取，**不是真正隔离**：同一系统用户的进程仍然能读这些文件。真正隔离见 [ADR 0005](../adr/0005-vault-daemon-user.md) 的独立系统用户守护进程方案，本步只是它的第一步。
- **绑定**：每个凭据记一串允许的域名。请求域名不在名单里就拒绝注入，一个字节的真值都不发。按域名标签边界匹配，`a.com` 不覆盖 `evil-a.com` 和 `a.com.evil.com`；只认 ASCII 域名（IDN 用 punycode），只认 http/https，重定向的每一跳都重新校验，跳转 URL 或被复制的头里捎带真值（含 URL 编码、base64 等变体）同样拒绝。
- **出站代理**：内核在请求离开进程前，把 URL、请求头和请求体里的 `{{secret:名字}}` 换成真值。模型只写名字。
- **脱敏**：真值出现在工具结果、transcript 或导出里时换成 `[secret]`，接的是现成的 `RedactText`；URL 编码、JSON 转义、base64 等变体以及被截断拆开的残段同样脱敏。
- **CLI**：`xbot secret set <bot> NAME --domains a.com,b.com`（值从 stdin 读，永不从 argv）、`xbot secret list <bot>`（只列名字和域名）、`xbot secret rm <bot> NAME`。

完成标准：
1. Bot 和模型只看得到凭据名字，看不到值。
2. 内核的出站代理在请求发出前替换 URL、请求头和请求体里的 `{{secret:名字}}`。
3. 每个凭据绑定允许的域名；向名单外域名注入会被拒绝，真值不发出。
4. 工具结果、transcript 和导出里出现的真值会被脱敏（接上 `RedactText`）。
5. 落盘加密（AES-GCM + 本地密钥文件），保险箱文件 0600，目录 0700，密钥在 `<root>/keys/vault/<bot>.key`（0600，目录 0700）。
6. 触及保险箱或密钥路径的 `shell`/`read_file`/`write_file`/`edit_file` 无论机器开关如何都出确认卡；这是防随手读取的尽力而为，不是隔离。

## 危险操作审核
执行前审查动作本身，看工具名和参数，不看模型怎么说。要确认的走已有的确认卡。

- **一张表定权限**：确认只经过这一张表；工具自己从不确认。内核里有一份显式的权限表，按工具名给出类别，`http`、`shell`、`self_edit` 再按参数判定。工具自己的 `Confirm` 只负责生成确认卡上的摘要，返回值一律不参与决定；模型改不了这个判断。
- **只确认四类**：对外发送、删除、注入密钥或非 `GET`/`HEAD` 的 HTTP、persona/profile 自改；另外触及保险箱/密钥路径的 `shell`/文件工具总是确认。routine 的创建、修改、暂停、删除，Bot 之间的 `message_bot`/`post_group`，写计划文件、写技能、编码任务都是 `normal`，不弹确认卡。
- **机器开关在核心里**：对本机工具（`shell`、`read_file`、`write_file`、`edit_file`、桌面、浏览器）统一在核心的 review 路径处理：`off` 直接拒绝；`ask` 每次都要确认；`allow` 按下面的表走。`browser_stop` 不在此列，`off` 时仍可用来关掉进程。`shell` 的 `keys:true` 也要确认。
- **HTTP**：`http_request` 只在注入 `{{secret:名字}}`（URL、请求头或正文任意一处），或者方法不是 `GET`/`HEAD` 时确认。
- **shell**：删除命令（`rm`、`rmdir`、`unlink`、`shred`、`find -delete`、`git clean` 等）和真正离开 XBot 的发送（`curl`/`wget` 上传或 POST/PUT、`mail`/`sendmail`、`nc`、发往远端的 `scp`/`rsync`、Python 的 `requests.post` 等）确认；`node_call` 的 `shell` 能力走同一条分类器。
- **删除**：文件/数据删除工具、`delete_bot`、`secret rm`、删除 Link 或 MCP 服务器确认；`routine_delete` 是 Bot 自己的排期，不算数据删除。
- **对外发送**：连接器（`telegram_send`、`lark_send` 等）、`links` 的 `install`/`remove`、未知工具和没标 `readOnlyHint` 的 MCP 工具确认；标了 `readOnlyHint` 的 MCP 工具是 `readonly`。主人自己 Bot 之间的消息不算对外发送。
- **self_edit**：`propose_persona`/`propose_profile` 总是确认；`write_file`/`edit_file` 只在解析后的路径正是本 Bot 的 `persona.md` 或 `config.json` 时确认。
- **只读不动**：`read_file`、列表、搜索、只看不发的检查、`list_secrets` 不弹确认卡。
- **决定留痕**：每个 Bot 一份 `bots/<name>/decisions.jsonl`（0600），每次决定追加一条，写允许、拒绝或过期，以及工具、摘要和时间；时间写本地时间加时区偏移（RFC3339，例如 `2026-10-16T09:30:00+08:00`），不写 UTC；摘要先过脱敏，不落真值。

策略表：

| 类别 | 工具 | 确认条件 |
|---|---|---|
| `readonly` | `read_file`、`list_secrets`、`list_bots`、`list_groups`、`routine_list`、`recall`、`recall_history`、`expand`、`read_skill`、`read_subagent`、`expand_subagent`、`code_show`、`node_list`、`desktop_screenshot`、`browser_snapshot`，以及服务器标了 `readOnlyHint` 的 `mcp_*` 工具 | 从不（`read_file` 触及保险箱/密钥路径时确认） |
| `normal` | `remember`、`forget`、`notify_owner`、`routine_create`、`routine_update`、`routine_delete`、`routine_pause`、`compact`、`spawn`、`steer`、`queue`、`interrupt`、`cancel`、`ask_user`、`create_bot`、`pin_bot`、`set_primary`、`create_group`、`message_bot`、`post_group`、`fork_bot`、`write_skill`、`code_start`、`code_steer`、`code_queue`、`code_interrupt`、`code_cancel`、`desktop_click`、`desktop_type`、`desktop_scroll`、`desktop_key`、`browser_open`、`browser_click`、`browser_stop` | 从不 |
| `delete` | `delete_bot`、`delete_file`、`remove_file`、`secret_rm`、`delete_link`、`remove_mcp` 等名称明确的删除工具 | 总是 |
| `external_send` | `telegram_send`、`lark_send`、`send_email`、`send_webhook`、其余 `mcp_*`（按前缀规则，没标 `readOnlyHint` 的），以及 `links` 的 `install`/`remove` | 总是（`links` 只在 install/remove） |
| `http` | `http_request` | 注入 `{{secret:}}` 或方法非 `GET`/`HEAD` |
| `shell` | `shell`、`node_call` | 删除或对外发送，`shell` 的 `keys:true`，或触及保险箱/密钥路径 |
| `self_edit` | `propose_persona`、`propose_profile`、`write_file`、`edit_file` | persona/profile 自改，或触及保险箱/密钥路径 |

完成标准：
1. 执行前在内核里审查动作本身（工具加参数），不依赖模型的说明，也不依赖工具自己的 `Confirm`；工具自己的 `Confirm` 只给出摘要。
2. 只有对外发送、删除、注入密钥或非 `GET`/`HEAD` 的 HTTP、persona/profile 自改需要主人确认卡；其余动作按表放行。routine 的增删改暂停、Bot 之间的消息、写计划/技能、编码任务都不问。
3. 只读动作（`read_file`、列表、搜索、只看不发的检查、`list_secrets`）不问。
4. 每次决定（allowed/declined/expired、工具、摘要、时间）追加进每个 Bot 的 `bots/<name>/decisions.jsonl`（0600），时间带本地时区偏移，真值脱敏。


## 为什么这样设计
确认曾散落在各工具里，范围不断被放宽，见 [ADR 0003](../adr/0003-one-permission-table.md)。
