---
kind: part
parent: README.md
code: [internal/vault/, internal/review/, internal/tools/guard.go, internal/tools/tooljob.go, internal/tools/shelljob.go]
verified: 2026-11-02 (cards family CLI migrated; HTTP needs-owner card preserved; Event/Link write-side retained)
---
# 安全：保险箱与权限表

> **30 秒版**：Bot 只知道密钥的名字，出站代理才换成真值。**生产内核永远不弹工具权限卡**（[ADR 0019](../adr/0019-no-permission-cards-by-default.md)）：`serve`/`chat` 固定 `Permissive: true`，旧 `permissions on` 不能开回卡；保险箱/密钥/配置路径、机器 ask/off、外部事件记忆写入也不弹卡。只保留 links install/remove CLI 意图的填 key 输入卡。以下权限分类与确认条件仅描述直接构造的严格测试/评测内核，不是生产许可限制。

> **现阶段（2026-10-18 起）**：先把功能调通调顺，再接安全，见 [ADR 0013](../adr/0013-features-before-security.md)。本页写的是现行代码的行为。权限表、审查入口和保险箱占位作为接口保留；不再加新的安全工作。生产工具许可已经固定放行；文件新鲜度、密钥脱敏、出站域名绑定和存储校验不受这次改动影响。

## 子代理工具分类

统一权限表把 `agent_message`、`agent_stop`、`send_message_to_parent` 归 Normal；三读 list/read/expand 已退为 subagent self CLI（原 Readonly 语义不变）；沿既有registry与review，不添加工具内确认或新的审批UI。孩子的父信目标由scope固定，Web直接输入与模型父信来源由内核装配，不接受任意sender/target；HTTP沿Bot owner鉴权，descriptor不保存密钥。可继续能力与旧记录兼容见[子代理](subagents.md)。

## 当前实例资料入口

资料 `profile set --self` 只接名字、简介、时区和生成头像 seed，可组合更新；persona、group 与运行预算仍沿独立业务。运行接口校验当前 loopback Host、非浏览器调用和规范 root，不把旧 `XBOT_BOT` 当 self 身份。完整输入先规范化，冲突只读预检并在提交锁内复核；后期 IO 错误保实际部分回执，客户端输出后非 0，不静默改写其它实例。头像是独立原始字节请求，最多 1 MiB；profile JSON 与回包仍各最多 64 KiB，不传图片 base64、服务端文件路径或复杂元数据 header。具体字段与 source/Web 区分见 [API](../API.md#本机-cli-profile-操作)。

## MCP CLI 与研究只读

MCP不再有可执行mcp_模型工具；Known不为任意旧前缀兜底，历史分类/记录仍可读。research只接受当前canonical BIN或精确受控XBOT_BIN的单POSIX直调，窄lexer保整词引号并拒替换/串联/重定向；list/tools/describe仅self，call另需readonly且在同slot/session精确检查hint再执行。legacy与continuable同接，生产许可仍默认放行，不添加审批UI。source新路由复用Host/browser/root约束；human先owner/owned，未显式root不以客户端默认目录作认证。结果JSON字符串/错误沿动态脱敏，产物实际路径取值+独占0600写在短route内；解码/网络锁外，详情见 [API](../API.md#mcp-运行时-cli)。

## 技能 CLI 与默认开关

两旧技能执行项及review Known条目已移除，历史read_skill正文脱敏仍可投影。research的当前BIN/精确XBOT_BIN单POSIX直调与MCP共享lexer，业务分类复用cmd的纯ParseSkillRuntime，只放self read/help，不放write/离线动作/操作符或伪身份。source继承原root/Host/browser guard，完整wire请求1MiB、整JSON回包64KiB、文件候选20MiB max+1；正文和错误沿原source动态脱敏。全部local scope含其它owner/shared/orphan，只有显式参数决定；只读预检不调用会迁移记忆的Load，config锁内的开关merge保未请求字段。完整行为见 [生态](ecosystem.md) 与 [API](../API.md#技能运行时-cli)。

## Bot 交接派生 CLI

`bot-fork NAME --self (--role TEXT|--role-file FILE|-)` 对所有 source Bot（含非 primary）沿原 Normal 语义，不新设工具类别。原 loopback/Host/browser/规范 root guard 与 source endpoint/ref 定位保持，XBOT_BOT 仅诊断；必须同 listener 实际 Local/running/未 finished Shell prepared context 和冻结 turn，不用 latestTurn，不要求 approver 函数，也不调用 approval intent/增加确认卡。承载 Shell 的原机器 gate 与分类独立；research 仅同窄 lexer/parser 的离线 help，参数中的字面 --help 不放行写。

回执的名字、错误及不确定记忆标识沿原 source 脱敏，raw及最终转义typed请求各1MiB，准确JSONNL/human各32KiB、HTTP64KiB；短回执不回显职责/摘要或另存artifact。created仅原config ACK、confirmed/parts仅原nil返回，当前Memory错误的uncertain_memory{bot,key}明确Markdown可能已写；不把容量/index/sync晚错说成零写，不自动重试或回滚。`fork_bot` wrapper/Known/schema/index及已空peers工具组已退，旧native记录和Link/Event其它作用域保持；旧离线/Web Fork仍错误空结果adapter，不能冒用source调用身份或typed部分回执。模型/记忆及当前路径边界见 [内核](kernel.md#bot-交接派生与原-source-模型链)、完整协议见 [API](../API.md#bot-交接派生运行时-cli)与 [ADR0057](../adr/0057-bot-fork-runtime-cli.md)。

## 经历记录与检索 CLI

`experience record/search --self` 沿原 source endpoint/ref、规范 root 和 loopback/Host/browser guard，旧 XBOT_BOT 仅诊断；所有 Bot 含非 primary 可用，不加主人/person/project scope 资格。两原语忽略 ctx，客户端不发 CallID header，缺失或过期 CallID 不影响领域许可，header 也不是身份或权限。原 Normal record 不调用 approval intent、不加确认卡、不要求 approver；承载 Shell 的原机器 gate 与分类独立，不能把群发送/派生的 prepared context 要求套到这里。

research 仅允许当前 canonical BIN 或精确受控 XBOT_BIN 的窄 POSIX 直调，经同 lexer/纯 parser 放 help 和 `experience search --self`；record 在解释器前拒绝，summary/query 值中的字面 `--help` 不冒帮助。dev/general 仍可沿原 source Shell 记录与检索。两旧 experience_record/experience_search 的 wrapper、Known、schema、index 与执行指引已退，不列入原生模型工具权限表；可见记忆文件检索沿下文独立 CLI，空 memory 工具注册组已退；compact CLI 保原 Normal 语义，空 context 工具注册组已退，三读沿下文只读 CLI。

raw 输入与最终转义请求各≤1 MiB；准确 JSONNL/human 各≤32 KiB、HTTP≤64 KiB。搜索返回原 presentHistory 已脱敏/6 KiB 文本，转义超预算才存当前 source 工作区完整该 Text JSON 的 0600 产物，先核 metadata 两格式，实际写点才短持 currentIO；错误沿原 k.redactor/held secrets/Resources 遮盖。记录仅原方法 nil ACK 为 true，原方法任意错误为 recorded:null，保返回 ID/range，不保证 SQLite 零副作用；仅显示 error 可缩短。CLI 先输出事实再非 0，不自动重试。字段与产物协议见 [API](../API.md#经历记录与检索运行时-cli)，当前路径和生命周期见 [内核](kernel.md#经历记录与检索实例入口)。

## 可见记忆文件检索 CLI

`memory-recall --self` 保原只读语义，沿原 source endpoint/ref、规范 root 与 loopback/Host/browser guard，旧 XBOT_BOT 仅诊断。必须由原 `runtimeSourceCall` 关联同 listener 实际 live Local/running/未 finished Shell prepared context，且冻结 turn 明确非 nil；不能取 HTTP ctx、最新 turn 或审批 owner 重绑后的 turn。此关联保原人物选择与取消链，不新加 owner/person/project/primary 资格、审批 intent、确认卡或 approver 函数要求；承载 Shell gate 仍独立。Local child 的 policy marker 只用于 review，人物可见性按其实际 frozen Kind/Source/Sender 与原 memoryPersonOf，不由 trusted 或 no-person policy 推断。

research 经原 `DirectXBotArgs` 和纯 `ParseMemoryRecallRuntime` 仅接受当前 canonical BIN 或精确受控 `"$XBOT_BIN"` 的单 POSIX 直调 help/self；值中的 literal --help/-- 不冒帮助，操作符、替换与错 root 沿原规则拒绝。这是该 CLI 的只读分类，不把任意 xbot/其它平台命令设为只读；熔断对同规范 literal 调用的识别仅调整原循环阈值。唯一 recall wrapper/Known/schema/index 和空 memory 工具注册组已退，不再列为原生模型工具；旧 native 历史、Memory 器官、4 KiB 文件写守卫保留，空 context 工具注册组已退，compact 与三读沿下文 CLI。

源扫描先沿规范 root、memory 祖先、scope 文件锁及打开 inode 同一性读取，完整二十命中文本在 typed 回包前经原 k.redactor/held secrets/Resources scrub；没有经历 6 KiB cap。准确 JSONNL/human 超预算时，仅保存完整该 Text JSON 到当前 source 工作区的原 0600 产物，写前核 metadata 两格式，实际 IO 才短持 current route；错误明确非 0、无假 file，CLI 先输出事实再退出，不自动重试。扫描范围与人物规则见 [记忆](memory.md#可见记忆文件检索)，完整字段/容量见 [API](../API.md#可见记忆文件检索运行时-cli)，生命周期见 [内核](kernel.md#可见记忆文件检索实例入口)，决定见 [ADR0059](../adr/0059-memory-recall-runtime-cli.md)。

## 对话原文三读 CLI

`transcript search/expand/detail --self` 复用原三个只读领域方法，source 仍取 endpoint/规范 root/ref，旧 XBOT_BOT 仅诊断。三原 Run 忽略 ctx，不要求 CallID/prepared turn，也不借 runtimeSourceCall、审批 owner 重绑、primary/person/project 资格、approver 函数或确认卡；HTTP ctx 仅管传输取消，原 Shell 的机器 gate 与 policy 保持独立。search/expand 的 foreign 目标只核保留的 source `k.Bot.Config().Owners` 与一次 Load 得到的目标 Owners 非空交集，不另强刷 Resources.current 配置；无交集、orphan/missing/load error、仅同群或可消息都统一 not found，授权后磁盘错误保留。Detail 只读 source，目标无稳定 ref/rename 跟随承诺。

research 经原 DirectXBotArgs 单 POSIX lexer 与同 ParseTranscriptRuntime，只放当前 canonical BIN 或精确受控 `"$XBOT_BIN"` 的合法三 action self/help；literal --help/-- 作为值时不冒帮助，操作符、替换和错 root 在解释器前拒绝。熔断只对规范 literal transcript search 承接原历史检索宽阈值，expand/detail/noun help 保普通阈值，不泛化所有平台命令。三 native wrapper/Known/schema/index 已退，无执行 shim；空 context 工具注册组已退，compact 沿下文 CLI，Link.Context 器官/Event、Memory 器官与 4 KiB 文件写守卫、旧 native 字面及工具配对历史保留。

原 presentHistory 先 redactor 再 cap，foreign 用原 Neutralize/完整 Wrap，缩正文重包及极端 fallback 保持，self 不加 foreign 围栏。runtime 只补 held secrets/Resources scrub，不把整份文本再过一次 k.Redact 或新造 fence/cap 防御。双格式预算溢出时保存当前 source 工作区完整该语义 Text JSON，写前预检引用、实际短 currentIO 写原 0600 产物，错误非 0、无假 file，先输出事实再退出且不自动重试；原语义上限以外 blob 不会补回。原 segStore 读维护仍可能写目录/锁、迁移或修索引，不能宣称物理零写。完整输入、字段与容量见 [API](../API.md#对话原文三读运行时-cli)，路由与生命周期见 [内核](kernel.md#对话原文三读实例入口)，决定见 [ADR0060](../adr/0060-transcript-read-runtime-cli.md)。

## 主动范围压缩 CLI

`compact --self` 保原 Normal 范围压缩，source 取 endpoint/规范 root/ref，旧 XBOT_BOT 仅诊断。原 Shell CallID/header 必须关联同 listener 的实际 live Local/running/未 finished Shell prepared ctx，原 runtimeSourceCall 保模型取消与用量归属；这与三读忽略 ctx、无需 CallID 不同。它不调用 runtimeApprovalSource/owner 重绑或审批 intent，不要求卡、approver 函数、primary/person/project 资格，也不叠 Recall 的非 nil frozen turn/person 门；Shell 机器 gate/policy 独立，header 关联原调用而非新的批准身份。

research 只准当前 canonical BIN/精确受控 XBOT_BIN 经原 DirectXBotArgs 单 POSIX lexer 与同 ParseCompactRuntime 解析成功且 Help=true 的离线帮助，合法 self+help 同样只读帮助；focus 中字面 --help 仍是值。self 压缩为 Normal 写，在解释器前拒绝，操作符/替换/错 root 保原拒绝，不增加 loose 熔断阈值。最后 native compact 的 wrapper/Normal Known/schema/index 与空 context 工具组已退，无执行 shim；Link.Context 器官/Event、4 KiB 记忆写守卫和旧四 native 字面/工具配对历史保留。

成功仅原短 ACK；错误沿动态 source redactor、held/Resources scrub，仅显示 error 可缩短，先输出实际回执再非 0，不自动重试。晚期错误前可能已有模型用量、环境或 State 效果，不伪造 Applied=false、成功回滚、零副作用或取消永不提交承诺。回执不含 summary/产物，不新增摘要读 API、权限表、阶段账或 Receipt/Inbox/wake；压缩不写记忆正文，既有读维护也不等物理零 IO。完整输入、DTO、容量见 [API](../API.md#主动范围压缩运行时-cli)，原 ctx/锁/保存链见 [内核](kernel.md#主动范围压缩实例入口)，决定见 [ADR0061](../adr/0061-compact-runtime-cli.md)。

## links 目录搜索加载与安装移除 CLI

`links search --self` 为只读，`links load --self` 把目录项标进运行中 kernel 的进程内 loaded 状态（deferred 解除、stableLinkNames 排除 lazy），不算只读；`install`/`remove --self` 改授权与密钥，经 owner 卡。四动作都需原 Shell CallID/header 关联同 listener 实际 live Local/running/未 finished Shell：search/load 经未改的 runtimeSourceCall 取 prepared ctx（`mcpSpecs`/`EffectiveLinks`/`secretValues`/`snapshotLinks`/`markLoaded` 都要求同一运行中 kernel），install/remove 经 runtimeApprovalSource 在 prepared ctx 上 `WithValue(turnKey{}, source.owner)` 重绑 turn，卡显示在 owner 当前 fork 分支，child call 时 owner=nil 走原 approver 语义；缺失/完成/foreign/nonShell/cancel 沿原 helper 拒绝，不假造 ctx。search/load 不调用审批 intent/卡/approver，install/remove 必须经这条发卡；都不加 primary/person/project 资格或 Recall 的非 nil 冻结 turn 门；承载 Shell 机器 gate/policy 独立。

install/remove 的卡在 runtime handler 里发：原 Shell ToolArgs map 嵌 canonical `_xbot_links` intent blob `{action,id}`（id 经 linksCardID 归一与 StageSecrets 键一致），summary 与原 linksTool Confirm 同构逐字节相同（`install <id>\n<summary>\nsecrets: <names>`/`connectorCard`/`remove <id>`），`approveRuntimeIntent` 发卡；`deniedBefore`/`parkable`/`grant` 按 `(name="sh", canonArgs嵌blob)` 键控，A 批准不喂 B。**permissive 不短路**：persona/bot delete 的 `checkRuntime*` early return 先例不照抄——密钥只能从卡 stage 来，handler 不走 `k.permissive()` 短路始终发卡，等价原 NeedsOwner permissive 例外的保留。web 侧 `approvalSecretNames` 按 args 判不按文案：`tool=="links"` 原分支保，另认 Shell 类 tool+`_xbot_links` blob+action=="install"；值经原 filterSecrets 白名单→StageSecrets→同 id takeSecrets→storeSecrets/dropSecrets→GrantLink/RevokeLink→EnableConnector/RemoveMCP→Reconcile 整链不变，模型永不接触值。

research 经原 DirectXBotArgs 单 POSIX lexer 与 noun+subcommand 双层 `ParseLinksRuntime`，只放当前 canonical BIN/精确受控 `"$XBOT_BIN"` 的合法 help 与 `search --self`；`load`/`install`/`remove --self` 都改变状态在解释器前拒绝（与 compact self 同档——parser 的 Action 判定天然不放行这三个），裸 `links`/未知 subcommand/缺 `--self`/query 值中字面 `--help` 不放行。linksTool 已整门退役：registry 无 links 条目、review 表无 `"links"` 行、NeedsOwner/cardtext/notable/todo 的 links 分支无残留，模型侧无双入口；工具结构体仅作领域方法容器，由 runtime handler 直调。旧 native literal/工具配对历史仍读，无执行 shim。

search 返回原 TSV（`id\tstate\tsummary\tsecrets:…`，三态 listed/available/installed，空集 "no links"），install/remove 回原 `"installed <id>"`/`"removed <id>"`；收尾仍 `scrubSecrets(text, k.secretValues())`——密钥名出现、值不出现；错误沿原动态 redactor+secretValues scrub，仅显示 error 可缩短。human/JSONNL 各≤32 KiB、HTTP≤64 KiB，CLI 先输出事实再非 0，不自动重试。完整输入、DTO 与容量见 [API](../API.md#links-目录搜索加载与安装移除运行时-cli)，决定见 [ADR0062](../adr/0062-links-runtime-cli.md) 与 [ADR0063](../adr/0063-links-install-remove-cli.md)。

## 共享慢调用的脱敏

每 Bot 原 Jobs 由进程资源持有，默认脱敏/形式只装一次；实际 CallRequest 保 source 的动态 Redact/Forms，不继承后来 K 的私有状态。初始 Command、进程 Command/Description、text/error、日志、Finalize Result/Command/Description 及失败诊断在各真实出口遮盖；writeMeta 不作补救。读 Output 在原 offset 切片后叠原 source 与当前进程已知秘密，offset/cap 不变。错误文本实际变化时才包一层保 errors.Is/As 的遮盖对象。

MCP 按引用名字、存在性与实际值比较输入；真实 env/HTTP header/启动 stderr 共用一次冻结快照，旧会话至少保到真实退出，仍叠当前 redactor。缺失/不可导出是 credentials 失败，不偷偷保旧 header 或降级；配置 vaultd 时不转本地/环境。无关密钥、同值与仅域名更新不全局重连。后端策略见 [生态](ecosystem.md#怎么运转)。本段验证基线 `aaa66c49`，沿定向 source/masking/credentials race 证据，不重验本页其它安全功能。

## 音频工具

统一权限表把 `listen` 归为 `Readonly`、`tts` 归为 `Normal`；挂泡仍用既有 `send_message_to_user`。本地音频读取和保存经工作区 `os.Root`，拒绝工作区外路径／符号链接；上传和取回复用 Bot owner 鉴权。文件与消息大小上限见 [API](../API.md#语音附件)，凭据沿供应商既有配置和传输复用。


## 边界
负责：工具环境、保险箱、出站代理、脱敏、权限表、决定留痕。不负责：机器开关的配置（见 [机器与设备](machines.md)），但开关在这里的 review 路径生效。

## 怎么运转
- **工具环境**：shell 和文件工具仍可访问这台机器，用户本来就是为此授权的。进程读完供应商 API key（`XBOT_API_KEY` 和各 provider 的 `api_key_env`）后就把这些变量从进程环境里清掉，包括启动时留在 `/proc` 环境块里的那一份，只留在内存里。`web.json` 里搜索或抓取的 `api_key_env`，以及 `XBOT_SEARCH_API_KEY_ENV` / `XBOT_FETCH_API_KEY_ENV` 点名的变量，同样清掉。选了 DeepSeek 搜索又没写变量名时，`DEEPSEEK_API_KEY` 也清掉。这些名字在进程启动之后写进 `web.json` 的，下次配置网页工具时再清一次。没登记的变量（例如 `OPENAI_API_KEY`）仍留在环境里。命令参数 `keys` 为 true 时，这次调用从内存把登记过的 key 放回环境，并且要用户确认。
- **网页出站**：`web_fetch`、`view_image` 的网址和 `web_search` 只做一道最小的地址检查（[ADR 0022](../adr/0022-web-egress-floor.md)）：DNS 解析出的地址，以及 URL、重定向里写死的 IP，落在回环、未指定（`0.0.0.0/8`、`::`）、链路本地（含云元数据 `169.254.169.254`）或组播时拒绝；域名 `localhost`、`*.localhost`、`metadata*` 直接拒绝；同一个名字再解析一次变成这类地址，按 DNS 改绑拒绝。私网、`100.64.0.0/10`、`198.18.0.0/15` 等其它地址都放行，所以本机代理（mihomo、Clash 的假 IP）不用配置。没有地址白名单；旧 `web.json` 里的 `trusted_egress_cidrs` 读进来后忽略，下次保存时丢掉。出站不走环境代理。POST（搜索密钥探测）不跟重定向，凭证头不会被带到别的主机。
- **搜索密钥**：代码默认仍是不需要密钥的 DuckDuckGo。选 DeepSeek 时，没写 `api_key_env` 就用已经收进内存的 `DEEPSEEK_API_KEY`，不回落到 `XBOT_API_KEY`。现场只有 `XBOT_API_KEY` 时，在 `web.json` 写 `"api_key_env":"XBOT_API_KEY"` 显式复用，例如 `{"search":{"backend":"deepseek","api_key_env":"XBOT_API_KEY"}}`。另写了变量名时，那个名字和供应商密钥一样只留在内存里。供应商密钥（`DEEPSEEK_API_KEY`、`XBOT_API_KEY`，或 `providers.json` 里的 `api_key_env`）只能发往 `https://api.deepseek.com`。`providers.json` 读失败时也按供应商密钥处理，不放行别的主机。已经收进内存、但不在这份名单里的搜索密钥仍可以走别的 https 主机。`http` 一律拒绝。请求是 `POST /v1/tools/search`，密钥只出现在 `Authorization` 头里，错误、日志和工具结果里的真值换成 `[secret]`。`web.json` 和 `providers.json` 跟保险箱一样，读或改都要确认卡。`providers.json` 的 `base_url` 必须是 http(s) 绝对地址，不带用户信息、查询串和片段；模型供应商允许用户明确填写任意 `http` 地址，设置给出灰字 HTTPS 建议且允许保存（[ADR 0037](../adr/0037-custom-provider-registration.md)）；搜索与 OAuth 的独立地址约束保持原样。`xbot provider add` 时检查一次，每次按绑定解析供应商时再检查一次，手改的文件也挡得住。
- **保险箱**：Bot 只知道 key 的名字。内核的出站代理在请求发出时把 `{{secret:名字}}` 替换成真值，并且只允许发往绑定的域名。key 由内核注入到相关进程中，这些进程和 Bot 的 Shell 隔离。工具输出中出现真值会被脱敏。
- **危险操作审核**：执行前审查命令本身。
- **代表用户对外发消息**：默认先出草稿，用户确认后再发。

## 保险箱与出站代理
钥匙只在用户这一侧。Bot 和模型只看得到名字，出站时代理才把 `{{secret:名字}}` 换成真值。

- **存储**：整机一个产品保险库，所有 Bot 共用；唯一装配入口 `vault.OpenGlobal(root)`，不接收 Bot 名。密文 `<root>/vault/_product_vault/secrets.enc`，目录 0700，文件 0600，AES-GCM；密钥 `<root>/keys/vault/_product_vault.key`（0600，目录 0700），不随 Bot 改名/删除。首次打开把旧 `bots/*/vault/` 密文及产品 `secrets.json` 在目标原写锁内合并：先验证全部，再一次提交，成功才删源；同名非空值不同或绑定不同显式失败并保留源，相同项可重入，空值让位非空。删除 Bot 前也先做本地迁移，避免连旧桶一起丢。OAuth 等保留独立 namespace。见 [ADR 0041](../adr/0041-global-product-vault.md)。
- **严格测试/评测内核的保险箱路径确认**（生产不出卡）：`shell`、`read_file`、`write_file`、`edit_file` 触到全局 `<root>/vault/`、`<root>/keys/vault/`、旧 `bots/*/vault/`、数据根上的 `web.json` 或 `providers.json`、跨设备的安装私钥 `<root>/keys/remote/` 或设备表所在的 `<root>/remote/`（见 [网关](gateway.md)）、桌面应用启动时读取密钥和端点的 `<root>/env`时，无论机器开关是不是 `allow`，都出确认卡（摘要带「触及保险箱/密钥」）。`xbot remote add/invite/join/remove`、`xbot remote gateway <url>` 不写路径也会改设备表，命令位置出现时同样出卡。判定是尽力而为：文件工具按解析后的路径，shell 把命令按词解析、对 cwd 取绝对、清理并解析符号链接后比对。这只是防随手读取，**不是真正隔离**：同一系统用户的进程仍然能读这些文件。真正隔离见 [ADR 0005](../adr/0005-vault-daemon-user.md) 的独立系统用户守护进程方案，本步只是它的第一步。
- **绑定**：空域名是本机用途，不出站；`HostAllowed` 对空名单拒绝，URL、头、正文占位注入均不能发出。非空但全部非法的域名仍拒绝写入，不会被悄悄转成本机。每个出站凭据记一串允许的域名。请求域名不在名单里就拒绝注入，一个字节的真值都不发。按域名标签边界匹配，`a.com` 不覆盖 `evil-a.com` 和 `a.com.evil.com`；只认 ASCII 域名（IDN 用 punycode），只认 http/https，重定向的每一跳都重新校验，跳转 URL 或被复制的头里捎带真值（含 URL 编码、base64 等变体）同样拒绝。
- **出站代理**：内核在请求离开进程前，把 URL、请求头和请求体里的 `{{secret:名字}}` 换成真值。模型只写名字。URL 路径里的花括号会被 Go 转义成 `%7B%7Bsecret:名字%7D%7D`，代理也认这种写法，同样先查域名绑定。Telegram 和飞书连接器的 token 也存在这里，出站只带占位符（见 [生态](ecosystem.md)）。
- **OAuth 模型鉴权**：凭证格式、刷新、退出时的并发写回协调与共享命名空间见 [供应商](providers.md)。模型请求通过同一后端的 `InjectStream` 只替换鉴权头，保留正文里的占位示例；本地和 daemon 都校验域名。出站时固定这次使用的脱敏快照，响应的令牌、编码变体与折行形式按既有口径隐藏，正常 SSE 文本不等待完整令牌长度的缓冲窗口。
- **脱敏**：真值出现在工具结果、transcript 或导出里时换成 `[secret]`，接的是现成的 `RedactText`；URL 编码、JSON 转义、base64 等变体以及被截断拆开的残段同样脱敏。base64 还认两种情况：值落在更长的 base64 流里、不在 3 字节组的开头（例如 Basic auth 的 `user:token`），这时它中间的整组照样认得出，两端最多各剩 2 个字节混在边界组里；以及被 base64 工具按 64、76 或任意固定宽度折行的，同宽的 base64 行先拼起来再找，连同换行一起换掉。替换是一遍从左到右：每个位置最长的形式先匹配，换进去的 `[secret]` 不再参与匹配。形式列表和替换器按保险箱版本缓存，保险箱变了才重算。`Store.Forms` 和 `RegisteredForms` 按从长到短列出这些形式（一样长按字节序），后台 shell 用这份列表决定待写尾巴留哪一段。
- **设置**：左栏「保险库」管理全局列表，切 Bot 不换库；HTTP `/api/secrets` 响应 DTO 只有名称/域名，永无 value。连接应用 NeedsSecret（含 MCP 环境键）、CLI、出站和 askpass 共用 `OpenGlobal`；旧 Bot 明文不是运行回退。改域名用 `SetDomains` 在原跨进程写锁内保留旧值，删除不存在条目不会复活；接口见 [API](../API.md#全局保险库)，界面见 [UI](ui.md#保险库设置)。
- **本机 sudo**：本地 Store 的固定名 `LOCAL_SUDO` 经 `Shell.Run` 的环境装配点配置 `SUDO_ASKPASS`，主线和作用域/子代理 Shell 共用绑定。临时 0700 助手只含二进制和 vault 路径，专用内部入口现读该密钥，密码只向 askpass stdout 输出，不放进环境变量或助手文件。助手至实际进程退出才清理，转后台不提前删除；输出复用既有脱敏注册，注册身份变化会使形式缓存失效。删除后即使旧助手尚在，新调用也取不到密码。本条不改 sudoers 或撤销系统 sudo 认证缓存；vaultd 不提供此明文出口、不绕过已配置后端。见 [ADR 0040](../adr/0040-local-sudo-askpass.md)。
- **CLI**：`xbot secret set <bot> NAME [--domains a.com,b.com]`（不传域名为本机，值只从 stdin 读）、`xbot secret list <bot>`（只列名字和域名）、`xbot secret rm <bot> NAME`。Bot 参数暂留兼容并验证存在，但不参与分桶，所有命令读写同一个全局产品库。配了守护进程后 `xbot secret migrate <bot>` 把同一个本地产品库整体搬至 daemon 的产品 namespace。

完成标准：
1. Bot 和模型只看得到凭据名字，看不到值。
2. 内核的出站代理在请求发出前替换 URL、请求头和请求体里的 `{{secret:名字}}`。
3. 每个凭据绑定允许的域名；向名单外域名注入会被拒绝，真值不发出。
4. 工具结果、transcript 和导出里出现的真值会被脱敏（接上 `RedactText`）。
5. 落盘加密（AES-GCM + 本地密钥文件），保险箱文件 0600，目录 0700，密钥在 `<root>/keys/vault/_product_vault.key`（0600，目录 0700）。
6. 生产内核触及保险箱或密钥路径不出确认卡；严格测试/评测内核仍验证保护路径分类。这不是隔离。

## 独立用户守护进程
第一步只把密钥挪出 Bot 目录、读写出确认卡，密钥和密文仍在同一系统用户可读处；真正隔离由独立系统用户 `xbot-vault` 的守护进程 `xbot vaultd` 提供（见 [ADR 0005](../adr/0005-vault-daemon-user.md)）。

- **后端接口**：`vault.Backend` 只有 `List`（`Secret.Value` 永远为空）、`Names`、`Set`、`SetDomains`（只改域名，旧值不回传）、`Remove`、`Transport(base)`、`Redact`、`MaskTruncated`。本地 `*Store` 和守护进程客户端 `*Client` 都实现它；产品统一由 `vault.OpenGlobal(root)` 选择本地 `Store` 或 `XBOT_VAULT_SOCKET` 客户端；低层 `OpenBackend(namespace, vaultDir, keyPath)` 只留给 OAuth 等独立 namespace。`http_request`、内核 `Vault` 字段、serve、`secret` CLI、导出都用接口，不再写死 `*Store`。
- **协议**：unix socket 上的 HTTP/1.1。接入时每个连接只读一次 `SO_PEERCRED`（`syscall.GetsockoptUcred`；macOS 上是 `LOCAL_PEERCRED`，`unix.GetsockoptXucred`；其他平台一律拒绝），uid 不在允许名单就直接关闭，之后不再逐请求握手。Bot 名走 `X-Vault-Bot` 头，校验 `[A-Za-z0-9_-]`，底层显式 namespace 使用 `dataDir/<namespace>/`（0700），密钥 `dataDir/keys/<namespace>.key`（0700 目录 / 0600 文件）。产品固定 `_product_vault`，首次打开合并旧 Bot 桶，OAuth `_provider_accounts` 不参与；产品调用不再传 Bot 名。`GET /v1/list` 只回名字和域名；`POST /v1/set`（正文上限 64 KiB）、`POST /v1/domains`（只改域名）和 `POST /v1/rm` 写；`/v1/forward` 用 `X-Vault-Method`、`X-Vault-URL`、`X-Vault-Host` 带目标，原请求头以 `X-Vault-H-<Name>` 传递，正文流式。守护进程走原有 `Store.Inject`/`guard`（只发绑定域名、重定向逐跳重校验、编码变体），用现有 Proxy 逻辑转发，回包状态放 `X-Vault-Status`、头放 `X-Vault-H-*`，正文边流边脱敏：跨块保留「最长变体长度 − 1」字节的重叠窗口，EOF 冲尾。含占位符的请求体才需要整体替换并缓冲，上限 1 MiB（超了 413）；否则不缓冲直接流。响应上限 8 MiB（超出截断）。任何响应和错误都不含明文，错误先过 `sanitizeError`。
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
生产路径只保留审查入口和分类，不出工具许可卡；links install/remove CLI 意图的填 key 输入例外（runtime handler 始终发卡，permissive 不短路）。下列确认规则属于严格测试/评测模式：执行前审查动作本身，看工具名和参数，不看模型怎么说。要确认的走已有的确认卡。

- **一张表定权限**：确认只经过这一张表；工具自己从不确认。内核里有一份显式的权限表，按工具名给出类别，`http`、`shell`、`self_edit` 再按参数判定。工具自己的 `Confirm` 只负责生成确认卡上的摘要，返回值一律不参与决定；模型改不了这个判断。
- **permissions on 时只确认四类**：对外发送、删除、注入密钥或非 `GET`/`HEAD` 的 HTTP、persona/profile 自改；另外触及保险箱/密钥路径的 `shell`/文件工具总是确认。Bot之间的 peer-message/chat-group post、写计划文件、写技能、编码任务保持Normal；CLI承载Shell的原gate独立。routine纯排期不额外加审批，承载它的原Shell仍受自身机器gate/分类，非空命令另沿原ctx审查。
- **例程前置命令同表审核**：四旧模型门及Known策略项已退，排期沿当前source CLI；同typed请求解码保末位null语义，update省字段仍审保留命令、空串清除。非空命令在所有Timer/route/Jobs锁外用原source reviewPrecheck，按process cwd及既有machine/protected/untrusted-memory/Shell表。原live Job保实际prepared ctx与wrapped approver，main审冻结身份、卡片跟原owner fork，child用明确untrusted/no-person与自己的subApprover，不借最新父turn。nested审批只向原Shell args附规范请求/effective命令，现grant与deniedBefore同键，argsfile A→B不吃A，原执行raw不变；autoID审后生、无HTTP随机nonce，不发不可调用旧工具grant。缺/旧/foreign/completed原call非空命令拒且定义不写；普通无命令CRUD不强制bridge。owner HTTP仍原临时Permissive专缝，不能拿来冒source审批。触发live gate另读，仅allow执行，ask/off/错静默skip并Commit，详见 [原调用连接](kernel.md#例程实例与原调用审批)及 [ADR0051](../adr/0051-routine-runtime-cli.md)。
- **Bot删除原审批**：四等价模型门/Known已退，strict明确沿原Delete类别恒需确认，不查已退名而降Normal；production permissive不增卡/不强普通操作原ctx。仅strict Delete取原live Local/running/未finished同listener ctx/原wrapped Shell approver，main原owner动态fork卡与冻结policy、child原回调保留，missing/completed/foreign/noapprover在Pause/删除前拒。nested只附canonical delete/目标给原Shell args，grant/denied同键，目标A→B不吃A；routine共用primitive且need=false仍查deniedBefore，真实Run args不改、不向旧delete_bot发grant。批准后重验primary/可见性再走原host保护；原Host limited owned join/removed事实见 [内核](kernel.md#bot-发现与管理实例入口)。
- **置顶/主身份Normal语义保持**：pin/unpin与primary set的source CLI每次核当前primary/self或VisibleBots本地目标，原两Normal动作不加许可卡、CallID或审批桥，不能误套SelfEdit/Delete。退两native/Known后也不能省source资格；B既有endpoint下一写fresh获得，A原endpoint下一写fresh失去，后pin错但pointer已写成同样生效，不把receipt当永久grant。原Shell gate独立，research只control help、所有写和离线primary解释器前拒。见 [ADR0053](../adr/0053-bot-pin-primary-runtime-cli.md)。
- **职责自改仍SelfEdit**：全Bot self的persona set CLI仅替原bot_propose_persona，strict显式need=true而非查已退Known/Unknown降Normal或借Delete；Permissive不加卡/不强ctx。strict沿原actual live Local/running同listener ctx/Shell wrapped approver/providerID/mutex/冻结policy、原owner动态fork卡/actualchild，不借ownerHTTP临时Permissive。锁外问后同currentref单原WritePersona，附加intent仅set+有效完整文本SHA256 64hex+bytecount，body冻结、原Shellargs/Runraw保，原deniedBefore/grant/park同canonical、文件A→B不吃A。无原ctx/finished/foreign/noapprover写前拒；测试为actual-context单位，非生产strict E2E。退空bot_profile组/schema/Known，保raw file SelfEdit与历史卡片，不改卡/记忆/版本账或发管理wake；见 [ADR0054](../adr/0054-self-persona-runtime-cli.md)。
- **群发送Normal仍保原链**：四群native/Known/schema退而chat-group member CLI沿原Bus；List/History/Create不强CallID，Post不调用approveIntent/不加卡，却只取actual live同listener Shell prepared冻结tuple（nil approver函数可用），不能审批ctx owner/child重绑后fallback到另一turn。原chain/hops+1/KindBot/时钟和8KiB/hops3/8次每分钟保，missing/finished/foreign/nonShell/noFrozen在rate/append前拒；无身份/Job/许可新表。actual Append/各Inbox.Post成功才计appended/posted，latepartial非0不冒全未写或重试回滚；原Shell机器gate独立，research只list/history/help。见 [ADR0055](../adr/0055-chat-groups-runtime-cli.md)与 [内核](kernel.md#成员聊天群与原消息链)。
- **单聊Normal与真实来源**：peer-message的LOCAL/REMOTE沿同source loopback/Host/browser/root guard，必须原live Local/running同listener Shell prepared context，不加approvalIntent或确认卡，nil approver函数可用；原Shell gate独立。chain/hops显式取冻结turnKey，远端普通source事实记录显式取callTurnKey实际owner动态branch，不能借latestTurn/审批重绑身份。保本地非空owner交集、自发/跳数/频率限制及远端精确配对、Person-owner/XKey/互发开关；历史读取保持原owned本机+ByName边界，不套发送门。完整冻结正文经原scrub，不进CLI回执，记录失败保已知发送/queue事实、非0且不重发。message_bot wrapper/Known/schema/index已退；research只同窄parser识别的help，text/target字面--help不构成许可。见 [ADR0056](../adr/0056-peer-message-runtime-cli.md)及 [单聊消息链](kernel.md#bot-单聊与远端源记录)。
- **机器开关在核心里**：对本机工具（`shell`、`read_file`、`write_file`、`edit_file`、`view_image`、桌面、浏览器）统一在核心的 review 路径处理：`off` 直接拒绝；`ask` 每次都要确认；`allow` 按下面的表走。`browser_stop` 不在此列，`off` 时仍可用来关掉进程。`web_fetch` 和 `web_search` 不碰本机文件，不走这道开关。`shell` 的 `keys:true` 也要确认。
- **HTTP**：`http_request` 只在注入 `{{secret:名字}}`（URL、请求头或正文任意一处），或者方法不是 `GET`/`HEAD` 时确认；迁 `xbot http --self` 后原 `NeedsOwner` 条件卡由 runtime handler 的 `approveRuntimeIntent` 保留（[ADR0068](../adr/0068-cards-family-cli.md)）。
- **shell**：删除命令（`rm`、`rmdir`、`unlink`、`shred`、`find -delete`、`git clean` 等）和真正离开 XBot 的发送（`curl`/`wget` 上传或 POST/PUT、`mail`/`sendmail`、`nc`、发往远端的 `scp`/`rsync`、Python 的 `requests.post` 等）确认。
- **删除**：文件/数据删除工具、strict Bot CLI delete、`secret rm`、删除 Link 或 MCP 服务器确认；routine CLI delete是Bot自己的排期，不额外走数据删除模型工具确认，外Shell原policy保持。
- **对外发送**：连接器（`telegram_send`、`lark_send` 等）、未知工具和没标 `readOnlyHint` 的 MCP 工具确认；标了 `readOnlyHint` 的 MCP 工具是 `readonly`。用户自己 Bot 之间的消息不算对外发送。原 `links` 工具的 install/remove 已随工具退役离开此表，其 owner 卡改由 runtime handler 的 `approveRuntimeIntent` 直发。
- **代理门**：`tools` 的 search、describe 归 `readonly`；call 在 agent 层转发成被调用的工具，确认卡、拒绝表和决定记录都用那个工具的名字和参数，跟直接调用一模一样，代理门本身不增加确认，也不能绕开确认（见 [内核](kernel.md#工具注册与代理门)、[ADR 0035](../adr/0035-unified-tool-registry.md)）。Event/Link 生产链入口侧的工具（`send_message_to_user`、`ask_user`、`dispatch`/`spawn`、`code_*` 五写）经 `inbox.Event` + `inbox.Inbox` 保留线投回，不移入 CLI 面（[ADR0070](../adr/0070-event-link-bind-write-decision.md)）。
- **self_edit**：严格测试/评测内核对persona self CLI显式按SelfEdit审核职责修改；`write_file`/`edit_file` 只在解析后的路径正是本 Bot 的 `persona.md` 或 `config.json` 时按自改审核。资料更新已经由当前实例 CLI 领域入口承接，旧 profile 模型工具没有注册、Known 或专用审批卡入口；生产默认放行与现有 persona/group 规则保持。
- **记忆文件夹**（记忆 v2 批 3）：不可信回合里 `write_file`/`edit_file` 写进任何 `bots/*/memory/` 或 `people/*/memory/` 都确认；可信回合不弹卡，但内核持目录锁校验（`memory.md` ≤ 4 KiB、日志只能在当月文件末尾按格式追加、索引和锁文件不许写、文件夹内不许符号链接），违规就恢复原文件并把原因告诉模型。`remember`/`forget` 工具已拿掉。
- **只读不动**：`read_file`、列表、搜索、只看不发的检查不弹确认卡。
- **决定留痕**：每个 Bot 一份 `bots/<name>/decisions.jsonl`（0600），每次决定追加一条，写允许、拒绝或过期，以及工具、摘要和时间；时间写本地时间加时区偏移（RFC3339，例如 `2026-10-16T09:30:00+08:00`），不写 UTC；摘要先过脱敏，不落真值。

策略表：

| 类别 | 工具 | 确认条件 |
|---|---|---|
| `readonly` | `tools`（代理门的 search/describe）、`read_file`、`web_fetch`、`web_search`、`view_image`、chat-group list/history CLI、memory-recall self CLI、transcript search/expand/detail self CLI、links search self CLI、subagent list/read/expand self CLI、`code_show`、`desktop_screenshot`、`browser_snapshot`，以及服务器标了 `readOnlyHint` 的 `mcp_*` 工具 | 从不（`read_file` 或 `view_image` 触及保险箱/密钥路径时确认） |
| `normal` | `generate_image`（复用全局 OAuth 的生图代理）、`send_message_to_user`、compact self CLI、`spawn`、`steer`、`queue`、`interrupt`、`cancel`、`ask_user`、`todo`、chat-group create/post CLI、`code_start`、`code_steer`、`code_queue`、`code_interrupt`、`code_cancel`、`desktop_click`、`desktop_type`、`desktop_scroll`、`desktop_key`、`browser_open`、`browser_click`、`browser_stop` | 从不 |
| `delete` | strict Bot CLI delete、`delete_file`、`remove_file`、`secret_rm`、`delete_link`、`remove_mcp` 等名称明确的删除工具 | 总是 |
| `external_send` | `telegram_send`、`lark_send`、`send_email`、`send_webhook`、其余 `mcp_*`（按前缀规则，没标 `readOnlyHint` 的） | 总是 |
| `http` | `http_request` | 注入 `{{secret:}}` 或方法非 `GET`/`HEAD` |
| `shell` | `bash`、`sh`、`zsh`、`powershell`、`cmd`、`git-bash` | 删除或对外发送，`keys:true`，或触及保险箱/密钥路径 |
| `self_edit` | strict persona self CLI、`write_file`、`edit_file` | 严格测试/评测模式的职责修改及 persona/profile 文件自改；触及保险箱/密钥路径沿原规则 |

完成标准：
1. 执行前在内核里审查动作本身（工具加参数），不依赖模型的说明，也不依赖工具自己的 `Confirm`；工具自己的 `Confirm` 只给出摘要。
2. permissions on 时，只有对外发送、删除、注入密钥或非 `GET`/`HEAD` 的 HTTP、persona/profile 自改需要用户确认卡；其余动作按表放行。Bot之间的消息、写计划/技能、编码任务按normal；routine纯排期不额外加审批，原Shell gate与分类仍有效。
3. 只读动作（`read_file`、列表、搜索、只看不发的检查）不问。
4. 每次决定（allowed/declined/expired、工具、摘要、时间）追加进每个 Bot 的 `bots/<name>/decisions.jsonl`（0600），时间带本地时区偏移，真值脱敏。


所有解释器工具复用同一权限类别、机器开关、保护路径和写资源判断；`description` 只用于用途展示，不参与放行。cmd / PowerShell 不套用 POSIX 只读白名单。旧 `shell` 仅识别历史记录和事件来源，不作为可调用别名注册。

## 为什么这样设计
确认曾散落在各工具里，范围不断被放宽，见 [ADR 0003](../adr/0003-one-permission-table.md)。
