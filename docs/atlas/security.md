---
kind: part
parent: README.md
code: [internal/vault/, internal/review/, internal/tools/guard.go]
verified: 2026-10-27 @ 064f81f + shell-by-interpreter
---
# 安全：保险箱与权限表

> **30 秒版**：Bot 只知道密钥的名字，出站代理才换成真值。**默认不弹权限卡**（[ADR 0019](../adr/0019-no-permission-cards-by-default.md)，`xbot permissions on|off`，`permissions.json`，没有就是关）：只剩 `links` 填 key 的卡和触到保险箱、密钥这两处还问；机器开关（ask/off）和外部事件开的一轮碰记忆的围栏也一起关掉。打开开关时，是否确认只由内核里的一张权限表决定，只确认对外发送、删除、带密钥或非 GET 的 HTTP、persona/profile 自改。

> **现阶段（2026-10-18 起）**：先把功能调通调顺，再接安全，见 [ADR 0013](../adr/0013-features-before-security.md)。本页写的是现行代码的行为。权限表、审查入口和保险箱占位作为接口保留；不再加新的安全工作。计划中的改动：确认卡、shell 黑名单、保险箱和配置路径出卡、不可信回合碰记忆时出卡、文件新鲜度拒写收进一个默认放行的开关；主人绑定的 Telegram、飞书消息按主人消息处理。落地之前，下面的描述仍然有效。

## 边界
负责：工具环境、保险箱、出站代理、脱敏、权限表、决定留痕。不负责：机器开关的配置（见 [机器与设备](machines.md)），但开关在这里的 review 路径生效。

## 怎么运转
- **工具环境**：shell 和文件工具仍可访问这台机器，主人本来就是为此授权的。进程读完供应商 API key（`XBOT_API_KEY` 和各 provider 的 `api_key_env`）后就把这些变量从进程环境里清掉，包括启动时留在 `/proc` 环境块里的那一份，只留在内存里。`web.json` 里搜索或抓取的 `api_key_env`，以及 `XBOT_SEARCH_API_KEY_ENV` / `XBOT_FETCH_API_KEY_ENV` 点名的变量，同样清掉。选了 DeepSeek 搜索又没写变量名时，`DEEPSEEK_API_KEY` 也清掉。这些名字在进程启动之后写进 `web.json` 的，下次配置网页工具时再清一次。没登记的变量（例如 `OPENAI_API_KEY`）仍留在环境里。命令参数 `keys` 为 true 时，这次调用从内存把登记过的 key 放回环境，并且要主人确认。
- **网页出站**：`web_fetch`、`view_image` 的网址和 `web_search` 只做一道最小的地址检查（[ADR 0022](../adr/0022-web-egress-floor.md)）：DNS 解析出的地址，以及 URL、重定向里写死的 IP，落在回环、未指定（`0.0.0.0/8`、`::`）、链路本地（含云元数据 `169.254.169.254`）或组播时拒绝；域名 `localhost`、`*.localhost`、`metadata*` 直接拒绝；同一个名字再解析一次变成这类地址，按 DNS 改绑拒绝。私网、`100.64.0.0/10`、`198.18.0.0/15` 等其它地址都放行，所以本机代理（mihomo、Clash 的假 IP）不用配置。没有地址白名单；旧 `web.json` 里的 `trusted_egress_cidrs` 读进来后忽略，下次保存时丢掉。出站不走环境代理。POST（搜索密钥探测）不跟重定向，凭证头不会被带到别的主机。
- **搜索密钥**：代码默认仍是不需要密钥的 DuckDuckGo。选 DeepSeek 时，没写 `api_key_env` 就用已经收进内存的 `DEEPSEEK_API_KEY`，不回落到 `XBOT_API_KEY`。现场只有 `XBOT_API_KEY` 时，在 `web.json` 写 `"api_key_env":"XBOT_API_KEY"` 显式复用，例如 `{"search":{"backend":"deepseek","api_key_env":"XBOT_API_KEY"}}`。另写了变量名时，那个名字和供应商密钥一样只留在内存里。供应商密钥（`DEEPSEEK_API_KEY`、`XBOT_API_KEY`，或 `providers.json` 里的 `api_key_env`）只能发往 `https://api.deepseek.com`。`providers.json` 读失败时也按供应商密钥处理，不放行别的主机。已经收进内存、但不在这份名单里的搜索密钥仍可以走别的 https 主机。`http` 一律拒绝。请求是 `POST /v1/tools/search`，密钥只出现在 `Authorization` 头里，错误、日志和工具结果里的真值换成 `[secret]`。`web.json` 和 `providers.json` 跟保险箱一样，读或改都要确认卡。`providers.json` 的 `base_url` 必须是 http(s) 绝对地址，不带用户信息、查询串和片段；明文 `http` 只允许本机（`localhost`、回环）和私网 IP，其余必须 `https`。`xbot provider add` 时检查一次，每次按绑定解析供应商时再检查一次，手改的文件也挡得住。
- **保险箱**：Bot 只知道 key 的名字。内核的出站代理在请求发出时把 `{{secret:名字}}` 替换成真值，并且只允许发往绑定的域名。key 由内核注入到相关进程中，这些进程和 Bot 的 Shell 隔离。工具输出中出现真值会被脱敏。
- **危险操作审核**：执行前审查命令本身。
- **代表主人对外发消息**：默认先出草稿，主人确认后再发。

## 保险箱与出站代理
钥匙只在主人这一侧。Bot 和模型只看得到名字，出站时代理才把 `{{secret:名字}}` 换成真值。

- **存储**：每个 Bot 一份保险箱目录 `bots/<name>/vault/`，目录 0700，所有文件 0600。内容用 AES-GCM 加密，密钥挪到 Bot 工作区之外的数据根 `<root>/keys/vault/<bot>.key`（目录 0700，文件 0600）。旧版密钥仍和密文同目录时，`Open` 会在文件锁下把它移到新位置（跨设备则先写 0600 再删旧文件），绝不因迁移另造新钥。Bot 改名时密钥文件跟着走，删除 Bot 时删掉。
- **保险箱路径一律确认**：`shell`、`read_file`、`write_file`、`edit_file` 触到本 Bot 的 `vault/`、`<root>/keys/vault/`、任何 `bots/*/vault/`、数据根上的 `web.json` 或 `providers.json`、跨设备的安装私钥 `<root>/keys/remote/` 或设备表所在的 `<root>/remote/`（见 [网关](gateway.md)）、桌面应用启动时读取密钥和端点的 `<root>/env`时，无论机器开关是不是 `allow`，都出确认卡（摘要带「触及保险箱/密钥」）。`xbot remote add/invite/join/remove`、`xbot remote gateway <url>` 不写路径也会改设备表，命令位置出现时同样出卡。判定是尽力而为：文件工具按解析后的路径，shell 把命令按词解析、对 cwd 取绝对、清理并解析符号链接后比对。这只是防随手读取，**不是真正隔离**：同一系统用户的进程仍然能读这些文件。真正隔离见 [ADR 0005](../adr/0005-vault-daemon-user.md) 的独立系统用户守护进程方案，本步只是它的第一步。
- **绑定**：每个凭据记一串允许的域名。请求域名不在名单里就拒绝注入，一个字节的真值都不发。按域名标签边界匹配，`a.com` 不覆盖 `evil-a.com` 和 `a.com.evil.com`；只认 ASCII 域名（IDN 用 punycode），只认 http/https，重定向的每一跳都重新校验，跳转 URL 或被复制的头里捎带真值（含 URL 编码、base64 等变体）同样拒绝。
- **出站代理**：内核在请求离开进程前，把 URL、请求头和请求体里的 `{{secret:名字}}` 换成真值。模型只写名字。URL 路径里的花括号会被 Go 转义成 `%7B%7Bsecret:名字%7D%7D`，代理也认这种写法，同样先查域名绑定。Telegram 和飞书连接器的 token 也存在这里，出站只带占位符（见 [生态](ecosystem.md)）。
- **OAuth 模型鉴权**：凭证格式、刷新、退出时的并发写回协调与共享命名空间见 [供应商](providers.md)。模型请求通过同一后端的 `InjectStream` 只替换鉴权头，保留正文里的占位示例；本地和 daemon 都校验域名。出站时固定这次使用的脱敏快照，响应的令牌、编码变体与折行形式按既有口径隐藏，正常 SSE 文本不等待完整令牌长度的缓冲窗口。
- **脱敏**：真值出现在工具结果、transcript 或导出里时换成 `[secret]`，接的是现成的 `RedactText`；URL 编码、JSON 转义、base64 等变体以及被截断拆开的残段同样脱敏。base64 还认两种情况：值落在更长的 base64 流里、不在 3 字节组的开头（例如 Basic auth 的 `user:token`），这时它中间的整组照样认得出，两端最多各剩 2 个字节混在边界组里；以及被 base64 工具按 64、76 或任意固定宽度折行的，同宽的 base64 行先拼起来再找，连同换行一起换掉。替换是一遍从左到右：每个位置最长的形式先匹配，换进去的 `[secret]` 不再参与匹配。形式列表和替换器按保险箱版本缓存，保险箱变了才重算。`Store.Forms` 和 `RegisteredForms` 按从长到短列出这些形式（一样长按字节序），后台 shell 用这份列表决定待写尾巴留哪一段。
- **CLI**：`xbot secret set <bot> NAME --domains a.com,b.com`（值从 stdin 读，永不从 argv）、`xbot secret list <bot>`（只列名字和域名）、`xbot secret rm <bot> NAME`；配了守护进程后用 `xbot secret migrate <bot>` 把本地保险箱整体搬过去（见下）。

完成标准：
1. Bot 和模型只看得到凭据名字，看不到值。
2. 内核的出站代理在请求发出前替换 URL、请求头和请求体里的 `{{secret:名字}}`。
3. 每个凭据绑定允许的域名；向名单外域名注入会被拒绝，真值不发出。
4. 工具结果、transcript 和导出里出现的真值会被脱敏（接上 `RedactText`）。
5. 落盘加密（AES-GCM + 本地密钥文件），保险箱文件 0600，目录 0700，密钥在 `<root>/keys/vault/<bot>.key`（0600，目录 0700）。
6. 触及保险箱或密钥路径的 `shell`/`read_file`/`write_file`/`edit_file` 无论机器开关如何都出确认卡；这是防随手读取的尽力而为，不是隔离。

## 独立用户守护进程
第一步只把密钥挪出 Bot 目录、读写出确认卡，密钥和密文仍在同一系统用户可读处；真正隔离由独立系统用户 `xbot-vault` 的守护进程 `xbot vaultd` 提供（见 [ADR 0005](../adr/0005-vault-daemon-user.md)）。

- **后端接口**：`vault.Backend` 只有 `List`（`Secret.Value` 永远为空）、`Names`、`Set`、`Remove`、`Transport(base)`、`Redact`、`MaskTruncated`。本地 `*Store` 和守护进程客户端 `*Client` 都实现它；`vault.OpenBackend(bot, vaultDir, keyPath)` 在 `XBOT_VAULT_SOCKET` 有值时返回客户端，否则回退到本地 `Store`。`http_request`、内核 `Vault` 字段、serve、`secret` CLI、导出都用接口，不再写死 `*Store`。
- **协议**：unix socket 上的 HTTP/1.1。接入时每个连接只读一次 `SO_PEERCRED`（`syscall.GetsockoptUcred`；macOS 上是 `LOCAL_PEERCRED`，`unix.GetsockoptXucred`；其他平台一律拒绝），uid 不在允许名单就直接关闭，之后不再逐请求握手。Bot 名走 `X-Vault-Bot` 头，校验 `[A-Za-z0-9_-]`，每个 Bot 一份 `dataDir/<bot>/`（0700），密钥 `dataDir/keys/<bot>.key`（0700 目录 / 0600 文件）。`GET /v1/list` 只回名字和域名；`POST /v1/set`（正文上限 64 KiB）和 `POST /v1/rm` 写；`/v1/forward` 用 `X-Vault-Method`、`X-Vault-URL`、`X-Vault-Host` 带目标，原请求头以 `X-Vault-H-<Name>` 传递，正文流式。守护进程走原有 `Store.Inject`/`guard`（只发绑定域名、重定向逐跳重校验、编码变体），用现有 Proxy 逻辑转发，回包状态放 `X-Vault-Status`、头放 `X-Vault-H-*`，正文边流边脱敏：跨块保留「最长变体长度 − 1」字节的重叠窗口，EOF 冲尾。含占位符的请求体才需要整体替换并缓冲，上限 1 MiB（超了 413）；否则不缓冲直接流。响应上限 8 MiB（超出截断）。任何响应和错误都不含明文，错误先过 `sanitizeError`。
- **性能**：客户端 `http.Transport` 的 `DialContext` 拨 socket，keep-alive（`MaxIdleConnsPerHost` 4、`IdleConnTimeout` 90s）复用一条长连接；链路只走小控制头加流式正文，没有大块缓冲。`TestDaemonOverheadP95` 用 300 个小 GET（预热、长连接）对比直连，p95 额外开销必须 < 5 毫秒；`BenchmarkDaemonForward` 量转发成本。
- **后备**：没配 `XBOT_VAULT_SOCKET` 就用本地的同用户 `Store`，开发与测试用；客户端 `Redact`/`MaskTruncated` 原样返回（值只在守护进程侧，回包已脱敏）。
- **仍防不住什么**：同一用户的进程仍可以叫守护进程把密钥发去它绑定的域名（由权限表的确认卡和域名绑定兜住），也可以覆盖或删除密钥；但读不到值。

**Linux 部署**（socket 目录 0755、socket 0666，但访问由 `SO_PEERCRED` 决定）：

```sh
sudo useradd --system --home /var/lib/xbot-vault --shell /usr/sbin/nologin xbot-vault
sudo install -d -o xbot-vault -g xbot-vault -m 0700 /var/lib/xbot-vault
sudo install -d -o xbot-vault -g xbot-vault -m 0755 /run/xbot-vault
sudo -u xbot-vault xbot vaultd --dir /var/lib/xbot-vault --socket /run/xbot-vault/vault.sock --allow-uid $(id -u)
export XBOT_VAULT_SOCKET=/run/xbot-vault/vault.sock; xbot secret migrate <bot>
```

systemd 单元：

```ini
[Unit]
Description=XBot vault daemon
After=network.target

[Service]
Type=simple
User=xbot-vault
Group=xbot-vault
RuntimeDirectory=xbot-vault
RuntimeDirectoryMode=0755
ExecStart=/usr/local/bin/xbot vaultd --dir /var/lib/xbot-vault --socket /run/xbot-vault/vault.sock --allow-uid 1000
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## 危险操作审核
执行前审查动作本身，看工具名和参数，不看模型怎么说。要确认的走已有的确认卡。

- **一张表定权限**：确认只经过这一张表；工具自己从不确认。内核里有一份显式的权限表，按工具名给出类别，`http`、`shell`、`self_edit` 再按参数判定。工具自己的 `Confirm` 只负责生成确认卡上的摘要，返回值一律不参与决定；模型改不了这个判断。
- **permissions on 时只确认四类**：对外发送、删除、注入密钥或非 `GET`/`HEAD` 的 HTTP、persona/profile 自改；另外触及保险箱/密钥路径的 `shell`/文件工具总是确认。routine 的创建、修改、暂停、删除，Bot 之间的 `message_bot`/`post_group`，写计划文件、写技能、编码任务都是 `normal`，不弹确认卡。
- **机器开关在核心里**：对本机工具（`shell`、`read_file`、`write_file`、`edit_file`、`view_image`、桌面、浏览器）统一在核心的 review 路径处理：`off` 直接拒绝；`ask` 每次都要确认；`allow` 按下面的表走。`browser_stop` 不在此列，`off` 时仍可用来关掉进程。`web_fetch` 和 `web_search` 不碰本机文件，不走这道开关。`shell` 的 `keys:true` 也要确认。
- **HTTP**：`http_request` 只在注入 `{{secret:名字}}`（URL、请求头或正文任意一处），或者方法不是 `GET`/`HEAD` 时确认。
- **shell**：删除命令（`rm`、`rmdir`、`unlink`、`shred`、`find -delete`、`git clean` 等）和真正离开 XBot 的发送（`curl`/`wget` 上传或 POST/PUT、`mail`/`sendmail`、`nc`、发往远端的 `scp`/`rsync`、Python 的 `requests.post` 等）确认。
- **删除**：文件/数据删除工具、`delete_bot`、`secret rm`、删除 Link 或 MCP 服务器确认；`routine_delete` 是 Bot 自己的排期，不算数据删除。
- **对外发送**：连接器（`telegram_send`、`lark_send` 等）、`links` 的 `install`/`remove`、未知工具和没标 `readOnlyHint` 的 MCP 工具确认；标了 `readOnlyHint` 的 MCP 工具是 `readonly`。主人自己 Bot 之间的消息不算对外发送。
- **self_edit**：`propose_persona`/`update_profile` 在 permissions on 时确认；`update_profile` 默认 permissions off 直接落盘，含 `avatar_seed` 也没有预览确认卡例外；`write_file`/`edit_file` 只在解析后的路径正是本 Bot 的 `persona.md` 或 `config.json` 时确认。
- **记忆文件夹**（记忆 v2 批 3）：不可信回合里 `write_file`/`edit_file` 写进任何 `bots/*/memory/` 或 `people/*/memory/` 都确认；可信回合不弹卡，但内核持目录锁校验（`memory.md` ≤ 4 KiB、日志只能在当月文件末尾按格式追加、索引和锁文件不许写、文件夹内不许符号链接），违规就恢复原文件并把原因告诉模型。`remember`/`forget` 工具已拿掉。
- **只读不动**：`read_file`、列表、搜索、只看不发的检查、`list_secrets` 不弹确认卡。
- **决定留痕**：每个 Bot 一份 `bots/<name>/decisions.jsonl`（0600），每次决定追加一条，写允许、拒绝或过期，以及工具、摘要和时间；时间写本地时间加时区偏移（RFC3339，例如 `2026-10-16T09:30:00+08:00`），不写 UTC；摘要先过脱敏，不落真值。

策略表：

| 类别 | 工具 | 确认条件 |
|---|---|---|
| `readonly` | `kernel_manual`、`read_file`、`web_fetch`、`web_search`、`view_image`、`shell_output`、`shell_status`、`list_secrets`、`list_bots`、`list_groups`、`routine_list`、`recall`、`recall_history`、`expand`、`read_skill`、`read_subagent`、`expand_subagent`、`code_show`、`bot_status`、`desktop_screenshot`、`browser_snapshot`，以及服务器标了 `readOnlyHint` 的 `mcp_*` 工具 | 从不（`read_file` 或 `view_image` 触及保险箱/密钥路径时确认） |
| `normal` | `notify_owner`、`routine_create`、`routine_update`、`routine_delete`、`routine_pause`、`spawn`、`steer`、`queue`、`interrupt`、`cancel`、`shell_cancel`、`ask_user`、`todo`、`create_bot`、`pin_bot`、`set_primary`、`create_group`、`message_bot`、`post_group`、`fork_bot`、`write_skill`、`code_start`、`code_steer`、`code_queue`、`code_interrupt`、`code_cancel`、`desktop_click`、`desktop_type`、`desktop_scroll`、`desktop_key`、`browser_open`、`browser_click`、`browser_stop` | 从不 |
| `delete` | `delete_bot`、`delete_file`、`remove_file`、`secret_rm`、`delete_link`、`remove_mcp` 等名称明确的删除工具 | 总是 |
| `external_send` | `telegram_send`、`lark_send`、`send_email`、`send_webhook`、其余 `mcp_*`（按前缀规则，没标 `readOnlyHint` 的），以及 `links` 的 `install`/`remove` | 总是（`links` 只在 install/remove） |
| `http` | `http_request` | 注入 `{{secret:}}` 或方法非 `GET`/`HEAD` |
| `shell` | `bash`、`sh`、`zsh`、`powershell`、`cmd`、`git-bash` | 删除或对外发送，`keys:true`，或触及保险箱/密钥路径 |
| `self_edit` | `propose_persona`、`update_profile`、`write_file`、`edit_file` | permissions on 时 persona/profile 自改；触及保险箱/密钥路径始终确认 |

完成标准：
1. 执行前在内核里审查动作本身（工具加参数），不依赖模型的说明，也不依赖工具自己的 `Confirm`；工具自己的 `Confirm` 只给出摘要。
2. permissions on 时，只有对外发送、删除、注入密钥或非 `GET`/`HEAD` 的 HTTP、persona/profile 自改需要主人确认卡；其余动作按表放行。routine 的增删改暂停、Bot 之间的消息、写计划/技能、编码任务都不问。
3. 只读动作（`read_file`、列表、搜索、只看不发的检查、`list_secrets`）不问。
4. 每次决定（allowed/declined/expired、工具、摘要、时间）追加进每个 Bot 的 `bots/<name>/decisions.jsonl`（0600），时间带本地时区偏移，真值脱敏。


所有解释器工具复用同一权限类别、机器开关、保护路径和写资源判断；`description` 只用于用途展示，不参与放行。cmd / PowerShell 不套用 POSIX 只读白名单。旧 `shell` 仅识别历史记录和事件来源，不作为可调用别名注册。

## 为什么这样设计
确认曾散落在各工具里，范围不断被放宽，见 [ADR 0003](../adr/0003-one-permission-table.md)。
