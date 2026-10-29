# XBot 里程碑与进度

## 2026-10-29 CLI 消息通道（功能分支）

- `xbot message` 经本机 owner 会话和现有 messages SSE 给运行中的 Bot 发消息；脚本化 HTTP 测试覆盖命令分发、正文与失败路径。未合 main，尚待真实入口及主人日用验收。真实二进制对假 HTTP 服务单次 632 ms；新增每次调用 2 次本机 HTTP 请求，交互 p95 尚未测。命令包三次、go vet、gofmt 通过；全仓测试遇既有内核 TestLongTurnStaysUnderTheCeiling 失败，网页包未结束时中止，全仓未完成。

## 2026-10-28 统一工具注册中心与代理门（feature/tool-registry-proxy）

- 一张工具注册表（`internal/kernel/registry.go`，`kernelTools` 声明表替掉 `namedLink` 拼装）。主线 `tools[]` 只声明代理门 `tools`、send、todo、壳、文件和 `web_fetch`，其余经 `tools` 的 search/describe/call 调用（抄 Pi 的 mcp 代理，不抄 deferred）。`expand_tool_group` 删除。环境流加 `tool_index`。见 [ADR 0035](adr/0035-unified-tool-registry.md)。
- 原生和 Links 分开：电脑操作是 organ，开机即挂；浏览器回到商店（带探测）；fetch 标 `retired`，不进目录、不挂载、不算已连接。
- 性能：同一 Bot 声明的工具从 38 个、22.8 KB（约 5.5k token）降到 10 个、9.2 KB（约 2.25k token），能调用 46 个；`tool_index` 668 字节。一轮里多次 describe/call 之后，`tools[]` 按字节比不变。新增请求、轮询、动画都是 0。
- 级别：端到端通过（假模型，scripted）；未合 main，未 push，没有用户日用，没有真模型认证。

## 2026-10-28 商店目录真源去掉原生器官（功能分支）

- `StoreApps()` 不再列电脑操作、浏览器（改为 `link.LookupOrgan`，提示词「连接器」与 `links load` 按标题照旧认）；`fetch` 目录行 `hidden`，`/api/apps` 不再出现「网页抓取」；前端 `apps.js` 的 `NATIVE` 滤掉删除。商店 POST `desktop` 返回 404。

## 2026-10-28 设置页扁平面举一反三（功能分支）

- 通用、权限、关于、技能、人设卡、已连接清单、设备本机行、图片生成开关与引导去掉 `--x-fill-1` 灰壳，清单去分隔线；关于头区不再复读短号；供应商卡绿点/「登录」贴右缘；连接应用去「本机」芯片，电脑操作、浏览器、网页抓取不进商店、推荐、已连接和计数。表面回归在旧样式上失败、修后通过；无头 Chrome 截图核对通用、关于、设备、技能、图片生成、连接应用；尚未在真实设置入口目视或用户日用。新增请求、轮询和动画均为 0；帧率和交互 p95 未测。

## 2026-10-28 网页搜索去灰垫（功能分支）

- 网页搜索开关、未选行与列表容器透明，选中及展开密钥区保留 fill-2；其他设置页不变。明暗主题 Chrome 样式回归旧样式失败、修复后连续三次通过；尚未目视验收真实设置入口或用户日用。
- gofmt、go vet、atlas audit 通过（audit 有既有告警）。全仓测试出现已有的 `TestLongTurnStaysUnderTheCeiling` 失败；全仓和 web 全包三次测试因 web 长时间未结束而中断，均未跑完。新增请求、轮询和动画均为 0；帧率和交互 p95 未测。

## 2026-10-28 供应商网格透明回修（功能分支）

- 供应商及订阅网格、换绑和网页搜索列表容器不铺整块底色；选中仍为 fill-2，未选供应商卡保留 fill-1。明暗主题 Chrome 渲染回归连续三次通过，旧 tip 样式回退时失败；尚未在真实设置入口目视验收，也未日用验收。
- gofmt、go vet、atlas audit 通过（audit 有既有告警）。全仓测试复现 `TestLongTurnStaysUnderTheCeiling` 失败；web 全包三次测试在约 100 秒时仍未结束，两项中断。新增请求、轮询和动画均为 0；帧率和交互 p95 未测。

## 2026-10-28 供应商卡选中底回修（功能分支）

- 供应商及订阅卡选中用 fill-2，网格缝与未选卡同为 fill-1；换绑行和网页搜索选项使用较浅选中底。Chrome 在现有本机设置入口加载本 worktree 样式，浅深主题点选返回并失焦后查看邻卡竖边，未见描边感；键盘焦点环保留。这是脚本化自检，尚未用户日用验收。
- 新增渲染回归旧代码失败、新代码连续三次通过；gofmt、vet、atlas audit 通过（audit 有既有告警）。全仓测试出现既有 `TestLongTurnStaysUnderTheCeiling` 失败，web 长时间未结束；全仓和 web 全包三次运行约 100 秒后中断，未完成。浅色选中底与缝 RGB 差之和为 23，深色为 25；新增请求、轮询和动画为 0，帧率与交互 p95 未测。

## 2026-10-28 供应商选中卡只留底色（功能分支）

- 选中卡保留中性 `--x-fill-3` 底，边框与未选卡相同；焦点轮廓保留。Chrome 设置入口脚本覆盖明暗主题，回归在旧样式失败、修复后连续三次通过，并查看了脚本化截图。gofmt、vet 和 atlas audit 通过（audit 有既有告警）。
- 全仓测试中 kernel 的 `TestLongTurnStaysUnderTheCeiling` 失败（40 步折叠 12 次）；全仓和 web 全包三次运行因 web 测试长时间未结束而中断，未完成。新增请求、轮询和动画为 0；未测帧率或交互 p95，未做用户日用验收，未合 main、未 push。

## 2026-10-28 私有资料与共享工作空间（功能分支）

- 私有资料继续使用 `bots/<bot>/memory/`；整机共享工作空间保留 `<root>/memory/`，采样时创建并始终注入契约与索引，空库也给路径。每轮轻推列出两条绝对路径，默认写私有、跨 Bot 才写共享；沿用环境流与 sidecar，关键词和预算覆盖两源。见 [ADR 0033](adr/0033-bot-and-shared-reference-workspace.md)。
- 空库、跨 Bot 写读、关键词、去重、预算与压缩定向 kernel 测试三次通过，分叉/重试/跨 Bot 写入 race 通过；`TestSpawnWhileChatKeepsAnswering` 三次通过，手册和命令包三次通过；gofmt、vet、atlas audit 通过（既有告警）。全仓测试 e2e 包通过；kernel 长轮、work shell 失败和 web 审批超时均在上一 tip 分别复现。kernel 全包三次运行在 90 秒总超时前两次遇到同一长轮失败，第三次未完成；全仓运行后仅调整了私有契约措辞，定向与入口测试重新通过。
- 20 个深层文件关键词扫描微基准约 0.20 ms/次；未测交互 p95、真实模型或主人日用，未合 main、未 push。

## 2026-10-28 资料默认归 Bot 回修（功能分支，之后共享策略由 ADR 0033 更新）

- 按政东新决定，资料默认使用 `bots/<bot>/memory/`，可选 `<root>/memory/` 保存跨 Bot 事实；Bot 库跳过系统记忆文件、日志、索引和备份。两源沿用环境流及提醒 sidecar，按来源独立预算，旧 extract 不恢复。决定见 [ADR 0032](adr/0032-bot-reference-and-optional-shared.md)。
- 定向 kernel 测试连续三次通过，覆盖私有隔离、共享可见、两源关键词、每轮轻推、系统条目排除、独立预算、压缩和分叉；提醒相关 race 测试通过。`TestSpawnWhileChatKeepsAnswering` 连续三次通过，手册与命令包三次通过；gofmt、vet、atlas audit 通过（既有告警）。全仓 e2e 包通过，kernel 长轮折叠失败、work shell 描述失败、web 审批测试 90 秒超时，三项在旧 tip 分别复现；kernel 全包三次运行受总超时影响未完成，前两次均遇到同一长轮失败。
- 20 个深层文件的单源关键词扫描微基准约 0.20 ms/次；交互 p95、真实模型和主人日用未测，未合 main、未 push。

## 2026-10-28 经历与资料收口（功能分支）

- 资料库增加关键词路径提醒和每轮写入轻推，通过提醒 sidecar 绑定并回放；原文不写入提醒，根层已有索引不重复提示。旧压缩抽事实代码及接线退役；资料索引超 256 KiB 整源排除并说明。
- 脚本化入口 `TestSpawnWhileChatKeepsAnswering` 连续三次通过，原 tip 在 `pong` 超时；脚本模型现能识别附有既有 reply-first 提醒的输入，旧 tip 仅带此脚本修正也通过。关键词、写入轻推、预算、压缩、分叉、溢出重试及稳定前缀等定向 kernel 测试三次通过，相关 race 检查通过。全仓测试 e2e 包通过；kernel 长轮折叠失败、work shell 描述失败及 web 确认测试超时均在原 tip 单独复现。kernel 全包三次运行在 90 秒总超时前两次出现同一长轮失败，未完成第三次。gofmt、vet、atlas audit 通过（audit 有既有告警）。
- 20 个文件的关键词扫描微基准约 0.41 ms/次；未测交互 p95、真实模型及主人日用，未合 main、未 push。
## 2026-10-28 日巡检界面修复

- 供应商卡选中皮与焦点改中性，主标题用供应商展示名；技能 YAML 块说明正确解析，缺失或无效说明不展示符号和 slug。相关浏览器与 skill 回归见本批测试；未合 main，尚待日用验收。

## 2026-10-27 对话顶悬浮卡紧凑回修（功能分支）

- 复审 P1 回修：底部状态行预留高度，发送和回复结束时不再推动消息列；浏览器回归覆盖两次切换，断言在修复前失败，修复后连续 3 次通过，线程底边变化均小于 0.1px。gofmt、vet 和 atlas audit 通过（audit 有既有告警）；全仓及 web 全包三次检查再次在 `TestWebApprovalAllowAndDeny` 超时，未完成三次；全仓还报 e2e、kernel、work 既有失败，以及 OAuth 固定端口占用（并行检查时发生）。未测帧率或交互 p95。
- 各断点身份卡只显示 28px 头像和名字，状态及 Bot 往来提示移到对话底部；消息顶垫和会遮挡卡的抽屉回到顶栏下沿。Chrome 脚本在 390–1440px 检查尺寸、命中及开合，另检查回复状态；相关测试三次通过，新增回归在旧代码失败，1200px 脚本截图已查看。gofmt、vet 通过；atlas audit 退出 0，有既有告警。
- 全仓测试报 e2e `TestSpawnWhileChatKeepsAnswering` 等待超时、kernel `TestLongTurnStaysUnderTheCeiling` 和 work `TestShellUsesBotDir` 失败；全仓和 web 全包三次均在 `TestWebApprovalAllowAndDeny` 以 180 秒超时退出，未做基线对照。新增请求、轮询和动画均为 0；未测帧率或交互 p95，未做用户日用验收，未合 main、未 push。

## 2026-10-27 技能收成提议（功能分支）

空库也注入提议与确认后写入的模型规则，沿用现有写工具和 Normal 权限；脚本化内核对话、Dispatch、skill、review、agenttype 定向测试和 vet 通过，atlas audit 退出 0（有既有告警）。未测真模型、主人日用、交互 p95；新增网络请求和后台任务均为 0。未合 main、未 push。

## 2026-10-27 任务记录折叠同头（功能分支）

- 已结束的运行记录开合共用紧凑行头，正文在下方显隐；标题节点在运行和结束状态间复用，例程配置行未改。
- Chrome 脚本从任务 Tab 验证开合，另验证标题节点、轮询及运行转完成；相关回归连续三次通过，旧代码的同头回归失败。gofmt、vet 通过；atlas audit 退出 0，报告既有告警。全仓单次出现 e2e 等待超时及 kernel 长轮折叠失败，web 尚未结束时中止；web 全包三次也未结束时中止，均未做基线对照。新增请求、轮询、动画均为 0；未测帧率或交互 p95，未做主人日用验收，未合 main、未 push。

## 2026-10-27 主列忙态单行（功能分支）

- 活泡复用一行三点胶囊和 description；状态及 SSE 的文案来自工具参数 description 首行，确认摘要仍在步骤中。侧栏运行预览同规则；厂商搜索流式提示不再另占一行。
- 新浏览器回归旧代码先失败，所选定向 agent/kernel/web 测试连续三次通过，但未覆盖 TestReplyToRoundTrip；该测试在复审时确认因本提交回归失败；本机 Chrome 脚本化截图核对同行。gofmt、vet、atlas audit 通过（audit 有既有告警）。全仓单次失败：e2e 聊天测试（复审确认是送达正文回归）、kernel 长轮折叠、work shell 描述，以及 web 审批测试 180 秒超时；受影响包三次中 agent 通过，kernel 长轮折叠失败，web 同一审批测试超时，未完成三次。未做基线对照；并行有其他工作区测试。未测帧率、交互 p95 或主人日用，未合 main、未 push。

- 复审回修：恢复送达消息记账，`result.text` 与 `done.text` 带正文，忙态仍只显示 description；删除无调用的 progress 和闲置参数。新 result 断言在旧 tip 失败，修复后 `TestReplyToRoundTrip`、忙态与活泡浏览器回归各连续三次通过，kernel 定向测试三次通过；gofmt、全仓 vet 通过，atlas audit 有既有告警。全仓单次失败：e2e `TestSpawnWhileChatKeepsAnswering` 超时、kernel `TestLongTurnStaysUnderTheCeiling`、work `TestShellUsesBotDir`，web `TestWebApprovalAllowAndDeny` 在 240 秒包超时；kernel/web 全包三次同样分别因长轮失败和审批超时未通过（长轮复审已在基线复现）。未测交互 p95、帧率和主人日用；未合 main、未 push。

## 2026-10-27 收件箱定时摘要修复（功能分支）

定时 routine 摘要只显示标签内正文，去掉后续唤醒套话；接口回归在旧代码失败、修复后专项通过。级别：本分支，尚未端到端验收；未测性能数字。

## 2026-10-27 收件箱来源和摘要修复（功能分支）

- Webhook、文件监听按真实来源前缀显示；摘要提取首个 Webhook delivery 正文、去掉 watch 外壳。真实格式接口及 Chrome 脚本回归在旧代码失败、修复后专项连续三次通过。
- gofmt、vet 通过；atlas audit 退出 0，存在既有告警。全仓测试失败于 e2e 等待、kernel 长轮折叠、work shell 描述及 web 审批 180 秒超时；web 全包三次在同一审批测试超时，未完成三次，与复审记录现象一致，未重新核验基线。未测真实入口端到端、用户日用、交互 p95 或帧率；新增请求、轮询和动画均为 0。未合 main、未 push。

## 2026-10-27 技能删除确认计数（功能分支）

- 删除确认的 N 统计本数据根所有 Bot，开关列表仍限当前用户可管理的 Bot；对齐设计的文案和危险按钮已存在。回归先在旧代码失败，定向 API、界面与 Chrome 脚本三次通过；gofmt、vet 通过，atlas audit 有既有告警。
- 全仓测试失败：e2e 等待超时、kernel 长轮折叠、work shell 描述、OAuth 回调端口占用，web 审批测试三分钟超时；web 全包三次亦在同一审批测试超时，未完成三次。未测交互 p95、帧率或用户日用，未合 main、未 push。

## 2026-10-27 网页搜索标题说明（功能分支）

- 「本机用哪家」标题旁的 ⓘ 复用全局悬停和聚焦提示，收起原先两段说明。
- 指定网页搜索测试单次和三次、Chrome 脚本悬停与聚焦、gofmt、vet 通过；atlas audit 有既有告警。全仓测试失败：e2e 等待超时、kernel 折叠次数超限、work shell 描述缺失，web 的审批测试 180 秒超时；web 全包三次也在同一审批测试超时，未完成三次。未做旧版本对照，未测交互 p95 和帧率，未验收用户日用。

## 2026-10-27 连接应用商店卡密度（功能分支）

- 商店卡标盒 48px、图形 30px，替换品牌形标；同行等高、说明最多两行，保留密钥范围说明与单格末卡。Chrome 回归旧代码失败、新代码与商店定向测试三次通过；gofmt、vet 通过，atlas audit 有既有告警。全仓测试早一轮报 e2e 等待超时和 kernel 长回合失败，web 包久未结束；最终版本全仓和 web 全包三次均中止，未完成。未测交互 p95 或帧率，未合 main、未 push，用户日用待验收。
## 2026-10-27 人设卡工具条单行（功能分支）

- 有卡时「全部」与搜索、新建、导入共用工具条；空库和筛选行为保持不变。当前布局见 [ui](atlas/ui.md)。
- 浏览器回归旧版失败、新版连续三次通过；1440×900 截图确认单行，搜索、分段和空库用脚本化数据验证。gofmt、vet 和 atlas audit 通过（audit 有既有告警）；全仓及 web 全包三次因并行资源竞争按要求中止，全仓中止前 e2e 和 kernel 有失败，未做基线对照。新增请求、轮询、动画均为 0；帧率与交互 p95 未测。未合 main、未 push。

## 2026-10-27 碎嘴群友本机主人残留升级（功能分支）

- 本机「碎嘴群友」比 `legacy-owner/1.txt` 多一段【跟人交流】，不逐字相等而被跳过；`legacyOwnerBodies` 改为一名多快照，新增 `legacy-owner/1-talk.txt`，仍走库锁内精确匹配，1.txt 照旧匹配，改过的卡不覆盖。
- 回归在旧逻辑失败；人设卡包三次、gofmt、vet 通过；全仓单次仍有既有失败（web 600 秒超时、work `TestShellUsesBotDir`，与人设卡无关）。对本机卡库的拷贝跑 EnsureSeeds：碎嘴群友逐字等于现行用户版，四张均无「主人」；live 库未动。note-capture「当主人说」本批未改。未合 main、未 push、未操作 8788。

## 2026-10-27 人设卡旧正文升级（功能分支）

- 复用库锁内的种子升级，四张未改过的旧「主人」完整版与原有三张薄稿按读盘正文精确匹配后升级；改过的卡不覆盖。
- 回归在旧逻辑失败；人设卡包三次、gofmt、vet、atlas audit 通过（audit 有既有告警）。全仓测试失败：e2e 聊天超时、kernel 长回合折叠次数、web 确认测试 3 分钟超时、work shell 描述缺失；此前里程碑也记录了这些问题，本次未重做基线对照。未测交互 p95、用户日用或 live 库，未合 main、未 push、未操作 8788。

## 2026-10-27 xAI 账号与密钥分离（功能分支）

- 订阅 xAI 和 API 密钥分别绑定；账号登录期间可创建或复用让名后的密钥供应商，退出账号保留密钥。订阅详情和密钥详情均可保存密钥。
- HTTP 回归在旧代码上复现 409；最终代码下密钥保存和脚本化 Chrome 回归连续三次通过。gofmt、vet 通过；全仓单次和 web 全包三次运行约两分钟仍未结束，已中止，不能判全绿。atlas audit 有既有告警。未测真实 OAuth、主人日用、交互 p95 或帧率；未合 main、未 push、未操作 8788。

## 2026-10-27 关于页版本点串复制（功能分支）

- 版本行左标签、右贴版本串；点击串复制完整展示并短显「已复制」，头区短号只展示。复用已有复制能力和配对码式行内反馈。
- 本机 Chrome 脚本回归连续三次通过，旧代码先失败；截图自查。gofmt、vet 通过；全仓测试报 e2e `TestSpawnWhileChatKeepsAnswering` 超时、kernel `TestLongTurnStaysUnderTheCeiling` 失败，web 全包久未结束后中止，全仓亦未完整结束；atlas audit 有既有告警。未测交互 p95 和帧率；未合 main、未 push，用户日用待验收。

## 2026-10-27 本机可选账号设备页复审修复（功能分支）

- 已登录设备页通过同源账号接口单独展示账号关联设备，保留配对列表和本机认领；未登录不拉账号目录。浏览器脚本回归以账号服务替身覆盖登录、展示、认领和退出。
- 账号、会话、配对定向测试连续三次通过；回归浏览器测试在旧 tip 失败、新代码通过。全量测试中 e2e 聊天超时、kernel 长轮失败，等待 web 包后中断；web 包三次运行在审批测试超时。gofmt、vet 通过。尚未合入、未做真实多设备端到端及用户日用验收；未测交互 p95 或帧率。每次已登录打开设备页增加一次同源账号设备请求。

## 2026-10-27 侧栏头像菜单收敛

- 按定稿保留头像开关菜单与唯一「设置」行；设备、关于仍在设置里，侧栏拖拽仍可切换窄条。详见 [界面](atlas/ui.md)。
- 本机 Chrome 脚本回归覆盖展开侧栏、窄条、设置和设备/关于；浅色和深色截图已自查，设计师复审及用户日用尚待验收。

## 2026-10-27 更换模型能力图标（功能分支）

- 浮层行保留横铺，在右侧用看图、搜索 outline 图标和短数字窗口替代能力句；语义通过 title / 无障碍名称提供。
- 本机 Chrome 脚本连续三次覆盖手机、桌面及桌面壳尺寸的能力语义和布局；未进行用户日用验收。新增网络请求和轮询为 0；未测帧率或交互 p95。全量测试已报 e2e 并发聊天超时、kernel 长轮折叠次数失败；全量及 web 包三次测试在 web 包等待超过三分钟后中断，未完成。

## 2026-10-27 xAI 旧密钥占用态修复（功能分支）

- xAI OAuth 绑定占用同名条目时，旧密钥详情改为说明先退出登录才能修改密钥，隐藏不能保存的表单；订阅入口、跳转及 OAuth 详情未改。
- 脚本化 Chrome 相关回归单次及三次通过，回归在旧代码上失败；gofmt、vet 通过。全仓检查在 e2e `TestSpawnWhileChatKeepsAnswering` 超时、kernel `TestLongTurnStaysUnderTheCeiling` 失败后仍未结束；web 全包三次亦未结束，两者约两分钟时中止，不能判全绿。atlas audit 有既有告警。未测真实 OAuth、用户日用或性能 p95/帧率；未合 main、未 push、未操作 8788。

## 2026-10-27 xAI 订阅入口（功能分支）

- 订阅区并列 ChatGPT 与 xAI；xAI 详情未登录引导回订阅，图片生成 Grok 未登录跳回 xAI 卡，复用既有 OAuth 与密钥绑定。账号占用同名条目时保留密钥折叠说明，不提供会被后端拒绝的密钥写入。
- Chrome 脚本化界面与相关测试 `-count=3` 通过；gofmt、vet、atlas audit 已跑。全仓单次与 web 全包三次未通过：e2e `TestSpawnWhileChatKeepsAnswering`、web `TestWebApprovalAllowAndDeny` 超时，work `TestShellUsesBotDir` 失败；三条在 `f4a54cd` 归档复现。未测真实 OAuth、生产性能或用户日用；未合 main、未 push、未启动 8788。

## 2026-10-27 provider 请求原文 debug

- 三种协议共用请求 body 落盘函数；热开关、路径与原文约定见 [供应商](atlas/providers.md)。按用户纠正，含原始密钥、未 redact，仅 body，不额外采集 header。实现与测试仅改 provider；不改权限、vault、UI 或 transcript。
- 复审 PASS（Pi prd-rev，P0/P1/P2=0）。功能 tip `1c94e90` 已合本地 main；`docs/dev/provider-request-debug.md` 已毕业删除。未 push；禁 sync-app；纯工程无 UI，不喊设计师。

## 2026-10-27 连接器 Agent 侧可用

- 沿现有 `links load` 补全名称/id 解析与商店类摘要筛选；MCP 失败回传阶段与有界脱敏 stderr；`/api/apps` 提供按 Bot runtime，读取不拉进程；前端未改。见 [生态](atlas/ecosystem.md)。
- 复审 PASS（Pi pwm-rev，P0/P1=0，P2=4 不阻塞）。功能 tip `5c04f8e`（rebase 自 `de23bd0`）已合本地 main；`docs/dev/playwright-mcp-tools.md` 已毕业删除。未 push；禁 sync-app；前端未改，待设计师逛后再 →已完成。

## 2026-10-27 云上账号壳切开

- 账号仓 tip `26a0067`：云静态只留登录 / 注册 / 查收邮件；删 `#devices` / `#gateway`；有会话一律 `/app/`；无设备/离线用设备侧 boot。见 [界面](atlas/ui.md#公网登录后的设备壳)。
- 复审 PASS（Pi cas-rev，P0/P1/P2=0）。`docs/dev/cloud-auth-split.md` 已毕业删除。未 push；禁 sync-app；登录/注册壳待设计师逛后再 →已完成。

## 2026-10-27 生图挂气泡重开（Bot 主动 notify_owner）

- 上一版内核 Save 自动 `attachOutImages`（`b389806`）作废。Save 只落盘 `generated/`；交付改由 Bot 调既有 `notify_owner` images 挂进本轮回复。引导禁「自动挂」话术。见 [内核](atlas/kernel.md#厂商生图)、[供应商](atlas/providers.md)、[界面](atlas/ui.md#图片生成设置)。
- 复审 PASS（Pi，无 P0/P1；两则 P2 备案）。功能 tip `07d6592` + atlas `b64ff8e`；已合本地 main。`docs/dev/imggen-attach-bubble.md` 已毕业删除。未 push；**未**跑 sync-app-from-xbot（ADR 0029）。设置脚注文案有改，待设计师逛后再收口已完成。

## 2026-10-27 生图同轮挂回复

- hosted 生图保存后接入既有挂图通路；普通工具轨迹展示、成功/部分失败的真实回执和轮次归属见 [内核](atlas/kernel.md#厂商生图)、[供应商](atlas/providers.md)、[界面](atlas/ui.md#图片生成设置)。按用户纠正移除独立生图步骤块，保留原 gallery 与可选工作区菜单。
- kernel 全包单次通过（41.609 秒），provider 全包通过；最终图片与长轮定向、定向 race、本机 Chrome 五项行为回归通过。ChatGPT/xAI scripted 冒烟跑真实 serve、HTTP 设置、OAuth 传输、保存与图片下载；不调真模型。新 kernel/UI 回归在 `77c0ee6` 归档分别因缺少挂图和仍有独立 hosted 块失败。
- 全仓包分批单次执行：work 的 `TestShellUsesBotDir` 失败；web 原审批测试卡住后主动终止诊断，跳过该条继续跑其余用例，另四条 parked approval 用例失败。这五条 web 和一条 work 均在 `77c0ee6` 复现；其余包通过，本次未扩修审批/解释器测试。gofmt、vet、diff 检查通过，atlas audit 保留既有告警。
- 本机 1024×1024 PNG（28,629 字节）保存＋挂图 100 次 p50 0.393 ms、p95 0.865 ms，最大 3.573 ms；仅小图文件链路，不是厂商耗时、IPC 或线上交互 p95。真实 serve 的两家 scripted 首个可见回复耗时和完整日志、浅深色截图见 `~/.xbot-build/imggen-attach-bubble/encode-report.md`。
- 已合本地 main（功能 tip `b389806`）；复审 PASS（无 P1）；`docs/dev/imggen-attach-bubble.md` 已毕业删除；atlas kernel/providers/ui 覆盖同轮挂图与普通工具轨迹。未 push；无 UI 大改未喊设计师。未使用 `-count=3`。

## 2026-10-27 例程 create 重复键末位 null 回修（功能分支）

- 审核与创建落盘共用 `parseRoutineCreate` 和 `Precheck *string`，消除 `914a2f8` 中“审核为空、string 落盘保留先前命令”的不一致；保护路径仍走原 reviewCall，未改 Confirm 决策、执行器、HTTP/update 或 P2。解析约定见 [安全](atlas/security.md)。
- 正式 `TestRoutineCreatePrecheckDuplicateNull` 用真实 agent 审批、落盘与 Timer.Fire 覆盖九种输入；旧实现的末位 null 与大小写变体均实际执行了未批准 marker，修后不落命令、不执行。缺字段、null、空串、末位敏感命令需审批和单字段 true 保持一致。
- kernel/link 全包 `go test -count=1` 通过（49.700 / 2.917 秒），web 例程单次通过（0.605 秒）；相关 vet、gofmt、diff 检查通过，atlas audit 已执行。未跑 count=3、race 或全仓；本改动不增加进程、网络或文件调用，未测生产 p95。
- 证据：`~/.xbot-build/precheck/fix-p1-dupnull-report.md`。仅分支提交，待独立复审 PASS 后合入；未合 main、未 push、未起 8788，P2 串行等待/取消残余未改。

## 2026-10-27 例程前置检查 P1 审核回修（功能分支）

- 有效 precheck 的创建/修改转回现有 shell 的 `Kernel.reviewCall`，更新省略字段也审保留的命令；保护路径使用执行时的进程 cwd。HTTP 修改复用同一判定，需确认时 403、整次不落盘。定时执行遇有效 machine ask/off/读取错误静默跳过并 Commit slot。约定见 [安全](atlas/security.md)；P2 串行等待/取消不传仍为已知残余、不阻断。
- link/kernel/tools/review/proc/serve 全包 `-count=1` 通过；web 例程用例单次通过。新增敏感路径、机器 ask/off、permissions on 删除/外发、创建与修改拒绝/确认、实际 cwd、HTTP 拒绝写入、运行中策略变化用例，放回 `59b41d1` 归档均能检出旧绕过。未使用 `-count=3`。
- link/tools/review 全包 race、web 例程与 kernel 例程/权限定向 race 通过；kernel 全包 race 报既有 configureWeb 竞争（四个子代理/环境用例），其中环境用例在 `59b41d1` 同样复现，未扩修。相关包 vet、gofmt、diff 通过，atlas audit 保留既有告警。完整证据 `~/.xbot-build/precheck/fix-p1-report.md`。
- 100 次 Timer 微基准均值：缺字段 0.637 ms/op、true 4.436 ms/op、false 3.084 ms/op；仅本机文件/进程链路，不是交互或 IPC p95。P2 多条检查仍可能累计约 2×N 秒，生命周期取消问题本轮未改。
- 仅分支提交，待独立复审；未合入 main、未启动 8788、未 push，未声明真入口或用户日用通过。

## 2026-10-27 例程免模型前置检查（功能分支编码交付）

- `Routine.precheck` 使用可选 shell 命令字符串；三处日常投递入口共用检查，非零、超时、启动失败静默消费 slot，成功沿用原事件。工具及现有 HTTP 接口可读写，未增加 UI。执行边界与幂等约定见 [多 Bot](atlas/multibot.md)。
- link/kernel/tools/proc/serve 单次通过；link/tools/proc/serve 三遍通过，例程 HTTP 接口三遍通过，六个相关包的新增用例 race 通过。旧 tip `714cfbe` 归档反证：非零检查仍投递 1 个事件，新实现为 0 且 LastSlot 落盘。全仓 vet、gofmt、diff 检查通过。
- 全仓测试未全绿：web 审批用例阻塞、work 旧 shell 测试缺 description，均在上述基线归档复现；首轮三遍 kernel 测试出现一次 shell 通知时序失败，专项基线 100 次未复现，当前 kernel 全包隔离三遍通过（118.894 秒）；保留首轮失败证据。atlas audit 的既有告警未扩展修复。
- macOS/arm64，100 次 Timer.Fire 微基准：无检查 0.644 ms/op，true 3.602 ms/op，false 7.288 ms/op（含 slot 文件读写，测试并行期间采样）。这是局部均值，不是交互/IPC p95，也未验证生产后台变慢比例。超时用例在 5 秒断言范围内返回，执行预算为 2 秒。
- 证据与报告：`~/.xbot-build/precheck/delivery-report.md`。本批仅分支提交；未合入 main、未起 8788、未 push；未做真模型、真入口端到端或用户日用验收，保留 dev 供父代理收尾。

## 2026-10-27 下一版提示词与配套用例（功能分支编码交付）

- 按 SCORECARD 的 D1–D5 收拢共享原则，清空四份 DeepSeek overlay；S1–S3 去重，S4 不动。新增知识三档、模糊任务同轮开工、桌面端主动配套和中文开场六条开发用例；复用 eval/scorecard，增加零工具调用检查与带工具开场白的 fake 回放。现状见 [供应商](atlas/providers.md) 与 [内核](atlas/kernel.md)，未完成验收见 [设计](dev/next-prompt-scorecard.md)。
- fake 每题三次：原 14 题能力 7/7、意愿 5/5；原提示与最终提示在同样 20 题上均为能力 11/11、意愿 7/7、守规矩 2/2、可靠性 pass³=1.00、违例 0，give-up 3/3。真实 DeepSeek 改前改后均因未配置 provider/密钥而未启动；fake 不证明真实提示效果，真实四维与独立留出验收待复审侧完成。
- eval/kernel/prompts 三遍通过；最终全仓单次运行中这三包及 e2e 通过，web 审批测试超时、work 旧 shell 测试缺 description，均在原 tip 归档复现。gofmt、vet、diff 检查通过；atlas audit 已执行，既有提醒未扩展修复。零调用检查与开场白回放有旧实现失败反证。
- 同夹具共享前缀 6328→6247 字节；StablePrefix 三次微基准中位数 1095→1086 ns/op，均 10516 B/op、20 allocs/op；40 步最多 10 次折叠的门槛不变，短措辞十次专项通过。未测真实回复/IPC p95 或帧率。
- 证据与交付报告：`~/.xbot-build/nprompt/`。仅功能分支提交，未合入、未 push，未声明真实模型端到端或用户日用通过；保留 dev 的剩余验收项。

> 这里只记**已经发生的事**：做完了什么、验了什么、实测数字、提交号、踩过的坑。**正在做什么、怎样才算做完写在 [dev](dev/) 里那份设计**；还没开始的想法在候选清单（不在仓库，在用户的 Obsidian：`正在干什么/06-XBot/XBot 候选清单.md`）。现行设计见 [atlas](atlas/README.md)。每日测试报告不进仓库。

## 2026-10-27 编辑资料名字标签（分支验证）

- 编辑资料名字行由「标识」改为「名字」，复用原字段与保存逻辑；人设卡库、选用、导入和已删除字段保持原行为。当前用语见 [ui](atlas/ui.md)，复审与合入待办保留在 [设计](dev/persona-label-name.md)。
- 全仓 `go test -count=1 ./...`、web 包 `-count=3`、全仓 vet、gofmt、JS 语法与 diff 检查通过；新增 Chrome 回归按可见标签编辑并保存名字和简介，旧产品代码因找不到「名字」输入框失败。atlas audit 已执行，既有文档告警详见交付报告。
- 1440×900、DPR 2 的前后浅深色默认／选中截图在 `~/.xbot-build/ui-shots/persona-label-name/`；自检与日志在 `~/.xbot-build/persona-label-name/delivery-report.md`。产品 app.js 前后均 334719 字节，新增请求／轮询为 0；未测模型、IPC p95 或帧率。分支验证通过，未合入、未 push、未做设计师验收。

## 2026-10-26 公网 UI 单源

- 设备新增受限 `GET /api/ui/static/<path>`，只读内嵌 staticFS；保留 MIME / ETag / no-cache，补设备 tip 与 700 KiB 超限 413。pipe 保持 owner `/api/` 边界，复用 Do / AuthorizeOwner，仅扩展静态元数据白名单。
- 配套账号分支删除业务 static 副本，保留 boot / bridge / scoped resource worker 与字体；完整页面、脚本、样式来自当前设备，切设备关闭旧 pipe、重载，无设备/离线/旧设备不支持时不回落旧壳。账号 API 仍在云上，sync 脚本退出业务 UI 发版路径。决策见 [ADR 0029](adr/0029-public-ui-single-source-via-pipe.md)；atlas 真源见 [界面](atlas/ui.md)「公网登录后的设备壳」。
- 相关 Go 包 `internal/web`、`internal/pipe` 单次测试及账号仓 `internal/account`、`internal/fsutil`、19 条 Node 测试、smoke-public 通过。新增出口测试在旧 xbot 归档上因路由缺失失败；新 bridge 行为测试在旧账号归档上失败。gofmt、相关包 vet、diff 检查通过；atlas audit 执行，既有陈旧、链接、映射提醒未顺手修改。
- Chromium 使用真实 Go hub / owner API 与 scripted 模型，验证完整桌面/手机界面、发消息、原生图片、只改设备端后刷新、Mini → Laptop 切换、关于版本、无业务缓存、无设备重试、账号退出及 CSP。完整相关包运行时启动 207ms，20 次 Bot API 读取 p95 0.40ms；仅为本机 fixture，不代表公网启动/交互 p95、IPC 或 60fps。
- 已合本地 main（合入 tip `a02e3f3`，现 tip 含其后批次）；设计师 PASS：设备侧 + 公网引导壳（pipe 薄引导，非另部署业务拷贝）；登录后主壳缺会话不挡收口。`docs/dev/public-ui-single-source.md` 已毕业删除。证据 box `/workspace/xbot-ui-design/review/a02e3f3-public-ui-single-source/`；encode `~/.xbot-build/public-ui-single-source/encode-report.md`。未 push。

## 2026-10-26 上下文一批 · 批 C

- 接入四项：合伙人导向对齐既有跟版规则；create_bot 用途不明先问、空职责拒绝但不加审批；七节机制手册 go:embed，模型按需 kernel_manual 与用户 CLI 同源；任务数和验收话术复用环境与结果证据。简介退出人设正文，工作档案复用记忆，人设卡/职责数据不迁移，CLI 区分运行档位与职责；无网页 UI 改动。约定见 [kernel](atlas/kernel.md) 机制手册/共享规则与 [multibot](atlas/multibot.md) 人设边界。
- 新增三组外在行为正反脚本、创建澄清/直接创建、七节先读再答、五任务状态和无测试证据两案；脚本是验收夹具，不是真实模型改前改后分数。旧基线隔离归档上的空职责、简介注入、手册接线和规则约束回归均失败。已有长轮折叠频率门槛保持，缩短规则后连续十次通过。
- 独立长程 450 轮、3 次压缩、峰值 64460 token，压缩开销 1.760%，缓存恢复最差为压缩前中位数的 97.6%，K=0.520。同一升级夹具前缀 6033→6716 字节；手册读取微基准 1036ns/op。真 CLI 七节同源与离线读取通过；其启动到结束延时见 `~/.xbot-build/context-batch-c/cli-smoke.json`，不代表模型回复/IPC/帧率。
- 普通全仓 go test、六个相关包三遍复测、批 C 专项 race 三遍、vet/gofmt 通过，六条 guard 冒烟通过。扩展 smoke 另有忙时回报、OAuth 生图、Responses 恢复三项失败，均在 f16c9a6 隔离基线复现；全工具 configureWeb 既有 race 也有基线反证，本批未修这些模块。
- Pi glm-5.3 review1 **PASS**；已合本地 main（tip `d052a38`）；`docs/dev/context-batch-c.md` 已毕业删除（本批无网页 UI，不经设计师；此前误留/revert 已纠正）。完整检查见 `~/.xbot-build/context-batch-c/encode-report.md` / `review1-report.md`。四维真实模型对照、C8 与手册面板后置；五条候选已完成。未 push。

## 2026-10-26 Agent Todo streaming 热修

- 针对 `dfaa232` 设计师不 PASS 的四处：流式回复期间继续既有 3 秒 status 轮询；侧栏和 live 优先同一 `st.todo`，无摘要时 todo／shell 用「待办／后台命令」；未完成轨迹用「进度 n/m 项」；running 预览恢复后可点击打开中文待办步骤。不改账本、工具执行、模型默认或 Tasks。
- `go test -count=1 ./internal/web/` 通过（30.797 秒，含本机 Chrome）；`go vet ./internal/web/`、JS 语法检查通过。`TestTodoBrowser`、`TestTodoStreamingBrowser`、`TestTodoLiveToolsBrowser` 均新绿；仅回退 app.js 至 `dfaa232` 后分别因 0/3 完成口吻、streaming 无 running 进度、live 裸 todo 而失败。流式测试还覆盖摘要连续变化、shell 事件不盖摘要、摘要消失后的工具人话兜底和两个轮询周期节点复用。
- 1440×900、DPR 2 的浅深色前后截图（流中默认/悬停、步骤打开、轨迹折叠/展开）在 `~/.xbot-build/ui-shots/agent-todo-hotfix/`，日志和夹具在 `~/.xbot-build/agent-todo-hotfix/`。两次非虚拟时间 Chrome 采样：status 变化到进度可见为 2993/3004 ms，符合既有 3 秒轮询；不是模型回复、IPC、60fps 或线上 p95 验收。
- 编码期未改 kernel、未跑全仓三遍；其后已合本地 main（tip `a02e3f3`，祖先含热修与 `949d542`）；设计师用户视角 Ultra 四块 PASS（含本热修）；`docs/dev/agent-todo-tool.md` 已毕业删除。未 push。

## 2026-10-26 Agent Todo 工具可见性

- 复用内核 `todo`、`todos.json` 和既有归档/提醒，将 TodoSummary 改为中文；侧栏显示待办进度，步骤弹层和轨迹使用「待办」及中文状态。轨迹成功回包才展示已保存条目，失败/未返回单独标明，原文折叠留在轨迹；全部完成归档后仍可看历史。未改模型默认开关、写盘锁或原子写路径，没有第二账本、Tasks 或对话任务卡。约定见 [ui](atlas/ui.md) 状态/轨迹段与 [kernel](atlas/kernel.md) 待办清单。
- kernel/web 的 `go test -count=1`、相关 vet、gofmt 和 diff 检查通过；新增摘要边界与浏览器行为测试，API 测试改为真实工具连续建三项/更新后读取同一磁盘账本。旧实现隔离归档上的摘要、API、浏览器三条回归均失败。浏览器验证成功/失败/未返回/放弃原因、原文折叠、归档清空预览和未变节点跨两次轮询保持；1440×900、DPR 2 浅深色截图经审美顾问复审通过。
- 五 Bot 状态接口 500 样本 p95 1.205ms；本机 Chrome 30 次轨迹切换至下一帧 p95 浅色 19.7ms、深色 20.2ms。前者为测试 HTTP handler 耗时，后者为 scripted HTTP 页面交互耗时，不代表真模型回复、IPC、后台负载或 60fps 达标。证据：`~/.xbot-build/agent-todo-tool/`，截图：`~/.xbot-build/ui-shots/agent-todo-tool/`。
- 已合本地 main；设计师 PASS（tip `a02e3f3`）；`docs/dev/agent-todo-tool.md` 已毕业删除。未 push。按本批范围未跑用户日用。

## 2026-10-26 轨迹可发现与回放

- 环境流与 `xbot trace paths` 共用真实绝对路径 helper；`list` 逐行过滤已有遥测，`replay` 直接复用 `ExportTo(Tools:true)`，无新账本和 UI 改动。约定见 [kernel](atlas/kernel.md#轨迹路径与脚本回放)。
- 编码验证：全仓 `go test -count=1 ./...`、vet、gofmt 与六条 guard 旅程冒烟通过；fake HTTP 模型经真二进制收到路径，shell 结果落盘，CLI 时间筛选与 seq 4–5 工具回放通过。证据 `~/.xbot-build/trace-discoverability/encode-report.md`。
- 已合本地 main（tip `f16c9a6`）；设计师用户视角 PASS；`docs/dev/trace-discoverability.md` 已毕业删除。未 push。
- 本机并行测试负载下各 30 次 CLI 进程启动至结束：paths/list/replay p95 为 13.438/15.470/35.565ms；本次路径字段 JSON 483 字节，约 121 估算 token。长回合测试按实际路径长度补足环境及快照预算，仍检查原折叠频率与请求上限；生产压缩不变。未测线上响应、IPC、后台负载或真模型；未 push。

## 2026-10-26 生图工作区可发现与头像热修余量

- 生图步骤与 Bot 菜单可进入 generated 路径浮层；浏览器可见/可复制服务端路径，桌面复用 open-workdir 打开固定子目录，拒绝符号链接。状态主题最多 18 字符，完整 prompt 可点击展开。模型生成请求及保存回执明确工作区交付，头像回执说已换好，图片设置脚注压短。关于页显示并复制独立构建 Tip，xbot-dev 用同一短 HEAD 注入并写两份已有记录；未改账号菜单底栏结构。约定见 [ui](atlas/ui.md#图片生成设置)。
- 编码验证：相关包单次完整测试通过（web/kernel/provider/link/work）；Chrome 覆盖目录入口/复制/失败、prompt 展开、关于短号复制。证据 `~/.xbot-build/imggen-discover-hotfix/encode-report.md`。
- 已合本地 main（tip `949d542`，本切片 `e7da2c5`）；设计师用户视角 PASS；`docs/dev/imggen-discover-hotfix.md` 已毕业删除。未 push。
- 隔离 Chrome 1440×900、DPR 2，浅深色截图在 `~/.xbot-build/ui-shots/imggen-discover-hotfix/`。假 API 下点击目录入口到两帧后（各 30 次）p95：浅色 33.3ms、深色 33.7ms；不是模型/IPC/后台负载或 60fps 结论。未测线上响应、真模型日用；未 push。

## 2026-10-26 图片生成设置（分支实现，待合入/验收）

- 设置在人设卡后新增图片生成，复用网页搜索密度与 web.json 写锁；全局默认关，OpenAI / Grok 单选，账号状态只读现有 OAuth 元数据，未登录同浮层深链至 ChatGPT / xAI 详情。新轮固定配置，只有所选家的已登录 OAuth Responses 绑定挂生图；图片继续进 generated/，不挂气泡。设计与待验收项见 [image-gen-settings](dev/image-gen-settings.md)。
- 全仓 Go 单次测试、vet、gofmt 通过；新增配置并发/幂等、API 输入边界、门控与浅深色 Chrome 交互回归。旧门控 overlay 上全局关/错家/未登录/API key 的回归失败；原工作区落图与无气泡断言继续通过。截图在 `~/.xbot-build/ui-shots/image-gen-settings/`，编码报告在 `~/.xbot-build/image-gen-settings/encode-report.md`。
- 本机 Chrome、1440×900、DPR 2、模拟 API，每个主题 30 次开关点击至下一帧：浅色 p95 35.7ms / 最大 35.9ms，深色 p95 34.9ms / 最大 35.0ms；零页面脚本错误。这是设置交互耗时，不代表真模型响应、IPC、后台负载或 60fps 达标。未合 main、未 push、未重启服务；待设计师截图复核及合入后真实账号工作区验收。

## 2026-10-26 update_profile 改名与默认直接落盘文案（分支提交，未合入）

- 基于 revert 后的 `2217ce2`，既有 profile 工具统一命名为 `update_profile`，不保留别名；工具/schema、Link 引导、权限表、cardtext、eval 与文档同步。默认 permissions off 含头像也直接落盘，无预览确认卡例外；SelfEdit 分类和 permissions on 审核不变，未改 propose_persona 流程。
- 单次相关套件通过：kernel 40.422s、bot 5.770s、review 0.702s、eval 4.591s；头像/资料 API 0.524s。补齐 Link 文案后重跑 link 0.761s、kernel 34.603s。gofmt、vet、diff 检查通过；atlas audit 执行完成，仍有文档陈旧、链接与映射等提醒。
- 回归覆盖模型工具列表、默认无卡（含无审批入口）、落盘重载、组合字段、上传清除、仅本 Bot、重复调用与非法参数。overlay 恢复旧工具实现后工具列表断言失败；恢复 `27f1970` 的头像例外后默认无卡和无审批入口落盘断言失败。
- 首轮三个默认模式 scripted 头像回合为 32.028–41.052ms（含落盘），不是线上交互/IPC p95 或浏览器帧率。日志与逐项交付在 `~/.xbot-build/update-profile-rename/`。按本次范围未跑全仓测试、三遍复测、真模型与浏览器；未 push、合入或重启服务，日用验收仍由主管安排。
## 2026-10-26 上下文一批 · 批 B（分支实现，未合入/未日用验收）

- 实现提交 `b160417`：环境补待批卡当前计数与清空 null；跨轮旧大工具结果提前变 expand 指针并保留回执；serve 空闲巡检复用后台摘要队列；换小窗口在保存绑定前压缩/拒绝；轮内先摘要开关默认关闭。没有 UI 或手动压缩入口，实体注册留批 D。规格见 [context-batch-b](dev/context-batch-b.md)。
- 最终 `go test -p 1 -count=1 ./...` 46 个测试包、kernel/compact/web/serve 三遍复测、专项 race、vet、gofmt 通过。基线 `f22b708` 隔离归档上的请求投影、空闲 serve 与换绑 HTTP 回归实际失败；B1/B3 同时复用已有环境回归。初次并行全量遇到 OAuth 固定端口冲突和插话顺序断言；未改这些断言，最终串行全量与三遍复测均绿。
- 独立 LongRunJourney：453 轮、3 次后台压缩，峰值 64552 token；压缩与抽取占比 1.755%，缓存恢复最差 97.3%，加权成本比 K=0.518。1001 行历史的未变待批卡查询微基准从 1.705ms 降至 0.051ms；阻塞 fake 摘要期间前台 Handle 单次 33.59ms。不是线上 p95、IPC 或 UI 帧率证明。
- 未合 main、未 push、未碰 8788；Linux CI、真实供应商/缓存质量、用户日用未验。完整自检、开关、已知限制与命令日志在 `~/.xbot-build/context-batch-b/encode-report.md` 与 `accept.md`。

## 2026-10-26 Bot 面板与窄屏、rail 修正（已合本地 main，待手测）

- 满宽厂头仅留编辑；模型整栏、人设满宽；更多复用原菜单，改由本机 Bot 行和置顶头像右键／长按进入，动作绑定被操作的 Bot，旧菜单及子菜单统一关闭。信息面板各断点统一右进右出，手机全高近全宽；汉堡按 52px 内容带加安全区、16px 左距、8px 间距落实。72px rail 分区头仅留 10px 透明间隔，设备头同样清掉灰底和圆角，壳间距保留。编码 tip `1765587`（rebase onto main 后 tip 见合入提交）；Pi glm-5.3 review1 **PASS**（`~/.xbot-build/bot-panel-detail/review1.md`）。
- `internal/web` 单次及新增三档浏览器回归通过；旧提交归档上的同一回归在手机/桌面关闭轴和中宽厂头额外动作处失败。Chrome 真实触摸长按通过，浅深色截图在 `~/.xbot-build/ui-shots/bot-panel-detail/`。模拟顶部安全区 44px 时内容带仍为 52px；24 次点击至动画结束 p95 196.3ms，最大 198.6ms（CSS 动画 180ms，含浏览器事件调度；不是 60fps、模型回复或 IPC 测量）。
- gofmt、vet 通过；atlas audit 的已有提醒未增加。全仓单次末轮有 `internal/code/TestLocalModelStepCapIsTheTasks` 的 TempDir 清理失败（directory not empty），本批未改该包。已合本地 main（不 push）；8788 手测见 `~/.xbot-build/bot-panel-detail/accept.md`。无独立候选（热修）。

## 2026-10-26 Bot 自改头像（已合本地 main，待手测）

- 扩展既有 `update_profile` 的 `avatar_seed`，复用 ValidAvatarSeed / SetAvatarSeed，仅作用于本 Kernel Bot；与名字、简介、时区组合更新，默认直接落盘；permissions on 按 SelfEdit 表审核。无上传能力、风格或主题变更。tip `0752c11`；Pi glm-5.3 review1 **PASS**（`~/.xbot-build/bot-self-avatar/review1.md`）。
- 当时的 permissions on scripted Kernel 覆盖拒绝、允许、非法 seed、非字符串、未知/跨 Bot 字段、同卡组合、上传清除、重载 AvatarState 与重复 seed；仅回退工具实现的 overlay 实际复现旧代码 unknown field、零确认和 schema 缺字段。指定三包与全量 Go 测试 count=1、既有头像/资料 API 与 vet 通过；三遍复测按政东要求取消。
- 首次专项四个 scripted 回合（含一拒绝）为 32.882–36.081 ms，含立即确认与落盘；不是线上回复 p95、IPC 或浏览器帧率。命令、结果与逐项自检在 `~/.xbot-build/bot-self-avatar/encode-report.md`。
- 已合本地 main（不 push）；8788 手测见 `~/.xbot-build/bot-self-avatar/accept.md`。保留 dev 设计待验收。

## 2026-10-26 StablePrefix 分段与可读环境人设（分支实现，待合入/验收）

- 实现提交 `e64d62a`：S0 人设/内核规矩/可选模型补丁、S1 能力框各用固定 `###` 标题和槽说明，保留 S2 技能边界；环境 persona 改为卡名或稳定标签加正文指纹，全文仍只进前缀。复用 PersonaText 的读取路径，由 PersonaSource 一次返回卡名与正文，保留 JSON 差分与快照回放。设计见 [stable-prefix-sections](dev/stable-prefix-sections.md)。
- `go test -count=1 ./...`（46 包，含 e2e 长程）、kernel 定向、kernel/bot `-count=3`、vet、gofmt 和 diff 检查通过；四项最终回归在 e164b7a 隔离归档中失败、当前实现通过。缓存专项确认 link 改动保留 S0 7062 字节，skill 改动保留 S0/S1 7336 字节。Atlas audit 已跑，既存告警没有新增项目。
- 扩展 smoke 套件未全绿：唯一失败 `TestNewTaskWhileBusy` 缺子任务验收值转告、relay-s3 超时（当前 124.64s）；e164b7a 原实现隔离复跑同样失败（124.80s），属基线已有失败，本批未改调度或冒烟期望。其余冒烟未报失败。完整日志与交付：`~/.xbot-build/stable-prefix-sections/encode-report.md`。
- Apple M6 / Darwin arm64 五次微基准中位数：StablePrefix 1494→1226 ns/op、14309→8595 B/op；完整分段组装 1510→1664 ns/op、12460→11612 B/op（约 +154ns，+10.2%）；四槽齐全固定增加 332 字节。未测真实模型缓存收益、交互/IPC p95、后台负载或帧率，不以微基准代替这些指标。
- Pi glm-5.3 review1 **PASS**（`~/.xbot-build/stable-prefix-sections/review1-report.md`）；真模型自述与用户日用尚未验。dev 保留供手测。

## 2026-10-26 官方薄入口续作（分支交付，本机 scripted 浏览器通过，未合入）

- 复用同款 static、账号 Bearer、ADR 0024 票/待命/AuthorizeOwner；加五种 `/api/` 方法、双向 Content-Type、正文限额与领票 CORS。serve 保存/恢复用户账号会话并独立报在线，轮换/登出/失效取消旧管道。
- 本机 Chromium 经真实 JS Web Crypto → Go Hub → 用户 API，用 scripted Provider 完成桌面/手机主界面、发消息、原生图片取回、离线重试和退出。连续 3 次首屏样本 48–54 ms，每次 20 次 `/api/bots` 的 p95 0.6–0.8 ms；不是公网、真模型、IPC 或帧率测量。完整 Go 套件与前端 15 条测试通过，详细命令、部署和余项见 `~/.xbot-build/public-thin-entry/encode-remote-ui-report.md`。
- 扩展 `smoke` 套件唯一失败为 `TestNewTaskWhileBusy`（子任务缺结果汇报、relay 等待超时），在隔离归档的 main `e164b7a` 上同样复现；六条基础旅程未出现失败。本批没有修改忙时任务逻辑，不能把扩展冒烟报为全绿。
- 不合入 main、不 push、不重启 Mini 8788。真实账号手机公网验收交指挥官与用户，尚不报用户日用通过。

## 2026-10-26 hosted 生图 P2：多图完成事件预算（分支回修，未合入）

- SSE 生图行预算按既有 20 MiB/图 × 8 张的 base64，加有限输出元数据与事件封装计算，修复 completed 重复携带多张大图使整轮报 token too long、最终路径丢失的问题。单图/张数/落盘上限、未启用生图的 1 MiB 行限与清理后的 1 MiB 输出预算不变。
- 新增两张各 13 MiB 的实际字节夹具，completed 行 36350890 字节；覆盖 done 已保存后重复 completed、仅 completed 两种路径，检查文件内容、去重、最终 Message 和模型回放中的相对路径。修前两场景分别在保存 2/0 张后报 token too long，修后均保存两张并保留路径。
- 专项命令、耗时与提交见 `~/.xbot-build/hosted-image-gen/fix-p2-report.md`；本批不碰 shelljob、消息挂图或 gallery，不合 main、不 push，不再启动全量长测。

## 2026-10-26 厂商 hosted 生图（分支实现，替身端到端通过，未合入）

- 沿用已有未提交实现，完成 Responses `image_generation` 的独立开关、OpenAI tool 级模型、xAI 默认 Imagine、工作区 `generated/` 落盘及相对路径回放。设置写入现有 ModelSpec；搜索关仍可生图。复用 OAuth/vault、PartHosted、BotDir，不新增本地工具，不改消息挂图或气泡 gallery，不自动写 transcript Images。
- 单图 20 MiB、每个响应 8 张、目录 500 项；流式图片行最多 32 MiB，移除 base64 后保持 1 MiB 输出预算。补终态失败、空结果、解码/落盘失败、重复完成省略 result、无 ID 限额及并发目录限额检查。四条失败终态回归和原实现边界回归有修前失败证据；独立复审发现的残留 result 假成功已修复，独立 overlay、四包专项 race 及真实 serve 双 OAuth 冒烟通过。
- 六条基本旅程与 Responses 搜索兼容冒烟通过；hosted 冒烟使用真实二进制、HTTP 设置/绑定、vault OAuth 注入和磁盘，上游 scripted。独立复测首个可见回复 ChatGPT 83.7 ms、xAI 101.1 ms（各一例）；本机 Chrome 30 次开关保存 p95 69.7 ms，含驱动开销，不代表线上回复 p95、IPC 或帧率。1440×900、DPR 2 的浅深色默认/悬停/开启截图已留存，保存和刷新读回通过；用户确认沿用界面。
- 按用户最终收束要求以 hosted 专项作为本批门禁；此前全量/三遍有 shell 通知测试失败，未修该测试或生产机制。vet、格式、构建、atlas audit 与完整交付见仓外 `~/.xbot-build/hosted-image-gen/encode-report.md`。未合 main、未 push、未重启日用服务；未测真实账号权限与生图、Mini 8788、用户日用、60fps 或后台负载。dev 保留，候选不标完成。

## 2026-10-26 xAI 退出与自动刷新竞态 P2（分支实现，未合入）

- 保留 `b2e586f` 的刷新写回核对：与删除共用 secrets 文件锁，重读后只有旧凭证仍匹配才保存；退出无需等待 token 端点。没有用第二次 logout 清理。
- 回归覆盖本地独立 Store 与 daemon 的 Remove，以及两种后端的真实 logout HTTP 路由：刷新阻塞 → 退出成功且凭证不存在 → 放行刷新 → 凭证仍无、绑定为 0、status 为 idle。HTTP 用例限制退出最多等待 3 秒，失败时也释放假端点。
- 用 overlay 仅换回 `7589cca` 的刷新实现，四个子用例全部重现凭证 configured；修复版各三遍通过。本机假服务的六次 logout 往返为 0.440–0.749 ms，端点全程阻塞；这是局部样本范围，非线上 p95、IPC p95 或帧率结论。
- 指定抽测、四包三遍、race、Chrome/Node、gofmt、vet 通过。全量首跑遇到未改动的 kernel 通知用例失败，该例单跑三遍通过后，全量复跑通过；保留首跑日志。atlas audit 退出 0，仍有既有文档告警。
- 级别：分支实现与 fake/scripted 局部验证；未合 main、未 push、未重启 8788、未做真人 xAI 或用户日用验收。门禁与交付证据见仓外 `~/.xbot-build/xai-oauth/detail-ui-logout-fix1.md`。

## 2026-10-25 网页搜索设置（本地实现，未合入）

- 设置浮层在权限后、设备前增加网页搜索。全局开关、自动 / 总用本机和九家单选按定稿接入；默认 DuckDuckGo，DeepSeek 已实现，其余七家后端占位。选中为中性灰；密钥「试一下」成功即保存，失败保留输入，不回显密钥，不增加保存主按钮和供应商详情开关。
- 搜索策略覆盖已有内核、作用域工具、本机子代理、内置编码任务和新远端任务的执行端点；修改不重建内核，运行中的远端任务保留启动快照。密钥沿用本机凭证写入与脱敏路径，serve 重启前置载入；探测沿用可信出口网段并固定官方地址。
- 本机 Chrome 1440×900、DPR 2 的六态与浅深色均有截图。读回、空密钥禁用、通 / 挂、失焦加点击单次探测、改字时迟到响应、关闭取消及保存失败保持状态通过。30 次自动化切换至选中态更新 p95 68.7 ms，含浏览器驱动开销；不是网络搜索时延或帧率结论。与同视口改稿做了像素差和取色，三种禁用紫色均未出现。状态接口顺序三轮 p95 为 1.207 / 1.183 / 1.156 ms；并行跑两组门禁时曾测得 8.987 ms 超过 5 ms，未改阈值。
- 最终全量 Go 测试、cmd/xbot / config / provider / kernel / web 三次、go vet、gofmt、前端语法检查通过。审阅发现的启动脱敏、可信出口、编码任务绕过、旧凭据绑定丢失、界面状态同步、地址与密钥混配、旧响应覆盖新校验结果与密钥轮换脱敏竞态已修复；回退修复后，对应回归实际失败，恢复后通过。
- 没做真供应商密钥 / 真实搜索网络验收、Windows 原生窗口、60fps 测量和设计师复审。未合 main、未推送、未发版，未重启日用服务。代码审阅、门禁与交付证据在 `~/.xbot-build/web-search/commit.md`。

## 2026-10-25 流式结束后回复消失（本地实现，未合入）

- Responses 保留流中完整输出条目；完成事件没有条目时，不再把已流出的回答落盘为空。真正空回复走已有错误与失败通知。历史空助手行显示可读提示，当前 Bot 和用户的消息保留；不改 OAuth、模型能力或设置布局。
- 旧代码反证复现回复消失和空落盘。浏览器覆盖正常、空回复、轮询、切换 Bot 与迟到流；HTTP 集成覆盖落盘及错误。两包三遍、全量 Go 测试、vet、gofmt 通过。只读 `codex review --base d39a8d0` 未发现明确缺陷；审阅沙箱不能跑 HTTP/浏览器，主 Agent 本机已跑通。
- 1440×900、DPR 2 的浅色和深色前后实图已自检。20 次脚本化流结束到历史 DOM 重绘的 p95 为 3.5 ms，最大 4.7 ms；不代表真模型回复、IPC 或后台负载性能。现场完整页面消失和供应商原始事件未复现，已复现的是回复气泡消失；未做真实账号或 8788 日用验收。
- 证据与手测步骤：`~/.xbot-build/chat-vanish/commit.md`。未 push、未发版、未合 main、未重启服务。

## 2026-10-24 ChatGPT 新模型能力（本地实现，未合入）

- OpenAI 看图与 Responses 搜索共用默认名单，补齐 GPT-5.6 luna / sol / terra、GPT-6 astra / luna / sol、GPT-6.1 sol 及家族别名；显式能力覆盖仍优先。设置详情与绑定运行时复用同一规则，SIWC 仍只提供模型名单与窗口。
- 相关回归通过；回退旧推断后，看图、Resolve 与订阅详情回归均失败。本机替身 SIWC 名单 HTTP 往返 1.225 ms，非线上 p95。完整门禁与审阅结果见 `~/.xbot-build/chatgpt-caps/commit.md`。
- 级别：本地实现；未做浏览器、真实模型或用户日用验收。未合 main、未 push、未发版、未重启 8788。

## 2026-10-24 ChatGPT 订阅登录入口（本地实现，待主管复审）

- 放开 A-magpie 订阅区 ChatGPT 卡。点击后复用既有 OAuth start/status 与 vault；等待时可重开授权窗且不重绘，失败可重试，成功刷新供应商并显示绿点。详情支持重新登录，模型仍用能力芯片，Bot 绑定路径不变。
- 模型名单接口复用登录时的 SIWC 目录解析，按账号可见模型返回实时名单；无凭证引导重新登录。不增加 OpenRouter 列表登录入口，不做 Claude 订阅 OAuth。
- 全量 Go 测试、web / config / providerauth 三遍、vet、gofmt、node 语法与行为测试通过；旧代码回退后两条回归失败。Chrome 已走脚本化登录与详情，浅深色截图已查看。SIWC 本机替身名单往返 0.683 ms；未测真人授权、线上 p95 或原生桌面壳。证据在仓外 `~/.xbot-build/chatgpt-oauth-ui/commit.md`；未合 main、未 push、未重启服务。

## 2026-10-24 模型与供应商：A-magpie 返工（本地实现，未合入）

- 统一设置浮层保留。列表改成订阅 / 供应商分区、右侧搜索、两列圆角卡。卡片只放透明品牌标、短名和已配置绿点；当时的 ChatGPT 订阅占位已在本页「ChatGPT 订阅登录入口」批次放开。详情用协议标签和 hosted / vision / 窗口芯片行，删除表格及原型制作说明。
- 详情按需拉供应商模型名单，复用配置中的能力规则，未知窗口使用仓内默认 128000。名单不写回配置，不改绑定选择器。拉取有超时、分页和 1 MiB 输入 / 输出限制；密钥未配、失败和空名单均有下一步。返回保留搜索条件，迟到响应不会改写新页面。
- 全量 Go 测试、web / config 三遍、vet、gofmt、前端语法检查通过。回退路由至 base 后，三协议动态名单回归均因 404 失败。审阅发现的特殊供应商名称已修复；当时的 ChatGPT 名单限制已在本页「ChatGPT 订阅登录入口」批次解除，两个回归均有修复前失败证据。Chrome 用实际设置壳和脚本化 HTTP 数据通过搜索、详情、重试及 2048 行检查；浅色、深色和窄屏截图已对照定稿。九次本机替身 catalog HTTP 往返为 0.52–1.35 ms；这不是线上 p95 或 60fps 结论。
- 完整命令、截图、差异和提交见仓外 `~/.xbot-build/mp-capability/commit.md`。atlas audit 退出 0，保留既有告警。未测真实供应商、Windows 或原生桌面窗口；未合 main、未 push、未发版、未重启 xbot-dev，待主管与设计师复审。

## 2026-10-24 多 Provider 批 4：OAuth（本地实现，未合入，验收未全过）

- OpenRouter PKCE 换取可重新授权轮换的 key，复用 Completions；ChatGPT 官方 Sign in with ChatGPT 动态注册、身份与 scope 校验、自动刷新，复用 Responses。令牌原子保存在现有 vault 的共享命名空间，本地和 daemon 都用原域名校验及注入。共享命名空间采用 Bot 名称不允许的前缀，避免 Bot 删除或改名影响密钥。API key 绑定保留；没有 Claude 订阅 OAuth 入口。
- 假授权服务验证 PKCE、nonce/签名/scope 拒绝、令牌加密与重启、并发单次轮换、上游实际 Bearer、重新登录保留模型配置；真实 Unix socket daemon 通过。模型正文中的工具占位示例不替换；编码折行与重复前缀缓冲边界脱敏均有修复前失败证据。桌面登录用系统浏览器，返回空句柄仍持续查询状态，脚本化宿主回归通过；未做原生窗口实机登录。
- 两条 OAuth 绑定各跑完整六旅程：5/6，分别 312.934 s、313.409 s。动手做事已产出文件并报告完整路径，但守护用例仍要求完成事件内联 summary.txt，当前事件只含 result.txt 指针；纯 key 同一断言失败。没有改旅程、断言或内核事件来换绿。替身沿既有 mockopenai 做法读取子代理结果，插话旅程通过。
- ChatGPT 替身网页首字 350/333 ms，插话 320/322 ms。带长身份令牌的 race 流式测试本地/daemon 首段约 9.0/9.7 ms；未测线上 p95、IPC 额外开销 p95、后台变慢比例或真人账号。API key Responses 搜索与换回 Completions 专项通过。
- 全量 Go 测试、相关五包三遍、vet、build、gofmt、atlas audit 有本次通过记录；较早一轮 web 插话顺序测试偶发失败，保留日志，未扩范围修内核。三轮 `codex review --base 2052ee1` 的实际问题当次处理并补回归。完整命令、限制、提交与审阅证据在仓外 `~/.xbot-build/multi-provider/commit-b4.md`。未 push、未发版、未合 main；整批与用户日用验收均未通过。

## 2026-10-23 上下文 A″：缓存前缀分段（本地实现）

- S0（人设、跟版 KernelRules、自有补充、overlay）、S1（Links）、S2（installed 与技能目录）用连续 system 消息组装；空目录省略，记忆与环境位置仍在后面。复用 A′ 规则入口、既有目录渲染与安全包裹，不改 Provider。
- 专项假缓存：技能变化时完整消息边界缓存从 0 到 6797 字节；Link 变化从 0 到 6616 字节。细粒度公共前缀分别为 6955→6951、6723→6720 字节，差异来自分隔换行；该口径无实质提升。总估算含消息开销 1748→1756 token。以明确分界收益和微小开销保留拆段，不承诺真实供应商收益。
- Apple M1 前缀组装基准 1000 次：3794 ns/op、12164 B/op、30 allocs/op。长程替身 455 轮通过，3 次压缩、prompt 峰值 64687、压缩成本 1.758%、缓存恢复通过，成本比 K=0.516（压缩对不压缩，非拆段对照）。未测真实供应商、线上 p95、IPC、后台变慢比例与用户日用。
- 首轮目录测试因只读取第一条 system 失败，读取器改为读取全部连续 system，原技能隔离与停用检查保留。只读 `codex review --base c938d1b` 未发现明确可操作缺陷；其 kernel/provider 测试受沙箱限制，compact 测试通过。`go test -count=1 ./...`、`go test -count=3 ./internal/kernel ./internal/skill`、专项缓存测试、`go vet ./...` 通过；`gofmt -l .` 无输出。atlas audit 退出 0，仍报既有陈旧/未挂接文档及测试临时文件告警，未扩范围处理。完整门禁和提交证据见工作区 commit.md。未推送、未发版、未合 main。

## 2026-10-24 多 Provider 批 3：Anthropic Messages（本地实现，未合入）

- Claude API key 绑定接入 Messages，沿用内容块、协议槽位与既有工具执行路径。显式启用的 hosted 搜索与本地搜索互斥；签名、搜索结果与引用可同协议回放，未知块降成文字摘要。无 OAuth 接线，无静态界面或前缀组装改动。
- 合成流与内核替身覆盖工具往返、GPT → Claude → GPT → DeepSeek 换绑、窗口和环境工具集更新、hosted 计次及 Jobs 隔离。单次 Claude 合成工具轮约 32 ms；未测真模型回复 p95、IPC p95 或后台变慢比例。
- build、vet、gofmt 和 Provider/kernel 三次测试通过。默认全量曾通过，最后一轮 web 插话顺序用例失败；四包三次检查也有同一失败。六条旅程 smoke 中忙时插话、动手做事缺少最终报告，Responses 搜索和其余旅程通过。审阅发现的工具图片、无工具总结、参数精度、重定向凭据、图片限制、搜索状态及续请求用量问题均有修复前失败的回归证据。
- 检查、审阅及提交证据见仓外 `~/.xbot-build/multi-provider/commit-b3.md`。未 push、未发版、未合 main；未做真钥或用户日用验收。

## 2026-10-23 上下文去重：overlay、todo 与完成事件（本地实现，未合入）

- 验收 #1–#5 已落地。Overlay 正文只在固定前缀，环境差异和压缩快照只公布模型键与短哈希；todo 保留完整磁盘清单，回包只报变更项和条数；完成事件改为结果文件指针与短开头。create_bot 既有短回执和 persona 环境哈希保持，补齐真实请求回归。
- 实际载荷：2030 字节 overlay 在环境中只留键和 12 位哈希；8 项 todo 初次回包 1051 字节，两次单项状态更新均 184 字节。1363 字节 fork 结论的完成事件 526 字节，全文可通过已有 read_file 读取。20KB 未读控制留在同一结果文件，完成事件守住 4096 字节上限。
- 回退五个生产文件到 25caf8f 后，新增 overlay、todo、fork 与子代理完成回归均失败；修复已恢复。旧派活断言改为核对指针和完整文件，e2e 假模型用已有 read_subagent 取回结论，原异步响应和记忆验收保留。
- gofmt 无输出，go vet ./...、相关包 kernel/exec/mockopenai 三次、go test -count=1 -v ./... 通过。长程假模型 455 轮、3 次后台压缩，prompt 峰值 64684（硬上限 107200），压缩成本 1.759%，缓存恢复检查通过。M1 本机已有基准各 1000 次：环境回放 30895 ns/op，todo 差异 2827 ns/op。未测线上回复 p95、IPC p95 和后台变慢比例；无界面改动。
- 首轮只读审阅的 P2（长未读控制绕过完成事件整体上限）已修复并补回归，最终只读复审未发现明确可操作缺陷。atlas audit 仍有既有告警，未扩范围清理。未跑真模型、Linux CI、线上长对话和用户日用；未推送、未发版、未合 main、未重启 8788。

## 2026-10-23 上下文 A′：规则随版本组装（本地提交，未推送）

- 当前 `KernelRules` 每次从二进制组装，`rules.md` 只留可映射的 Bot 补充；创建、导入不复制默认规则，fork 继承补充。迁移不提示、不保留历史目录。范围与异议见 [短计划](../plan.md)。
- 新增升级回归通过；回退旧 `kernel.go` 后同一回归失败。实际组装前缀 5635 字节，包含当前 Silence 的 routine fallback 与中文补充。并发迁移、重复迁移和空补充均覆盖。复审发现并修复 CRLF 段落、旧重试残句及 CLI 创建遗漏；新增初始化回归在旧 CLI 上失败。
- `gofmt -l .` 无输出，`go vet ./...` 与 `go test -count=1 ./...` 通过（包含 e2e）。`go test -count=3 ./internal/kernel ./internal/bot ./internal/web`：kernel/bot 通过；web 出现状态接口 p95 6.058ms 与插话多一条 answer 的失败。只复跑这两项各三次均通过，状态接口 p95 1.002–1.163ms；修复迁移边界后，完整三次包测试 kernel/bot/web 均通过，命令行包 `go test -count=3 ./cmd/xbot` 也通过。
- atlas audit 有既有的文档大小、陈旧基准、未挂接文档等告警，未扩大范围清理。最终只读 `codex review --base 6ff19e6` 未发现明确可操作缺陷。未跑真实模型 Silence 六次对照、线上升级、Linux CI 和用户日用验收；不宣称端到端升级已通过。

## 总表
| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 核心循环、Link、Event、收件箱、人、记忆接口 | 已完成 `a32d242` |
| M2 | 记忆、定时任务、`serve`、环境快照差异、摘要块、按需找回、供应商 | 已完成 `233aaf7` |
| M3 | 单一事件处理循环、子代理、执行者、工作区、技能 | 已完成 `9d233fa` |
| M4 | Webhook、文件监听、合并与限流 | 已完成 `304fa00` |
| M5 | Web 界面、PWA、Bot 之间通信、群 | 已完成 `caa53a1`；UI 精修中 |
| M5.x | 后台异步压缩、Fork Bot、主 Bot 与置顶、Bot 档案 | 已完成 `db6487e` `4b07ec4` `5a96e75`。网页接口见 [API.md](API.md)，静态界面由单独的代理维护 |
| M6 | 手机后台推送、远程访问（Cloudflare Tunnel 等）、多节点、人的记忆同步、机器开关、桌面操作、浏览器原生 Link、MCP 适配器与连接器目录、编码代理 Link | 手机推送、隧道（公网可达性未测）、多节点（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）、人的记忆同步（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）、机器开关、桌面、浏览器：已验证。精选目录、Telegram/飞书、MCP（stdio 与 Streamable HTTP，按 Bot 启用，密钥只记名字）、编码代理（确认后启动，输出进对话卡片）：已接上。托管市场未做 |
| M7 | 凭据代理与保险箱、危险操作审核 | 盒子回归（假模型，2026-10-16）。已验证：保险箱 CLI、落盘加密与权限、只向绑定域名注入、跨域跳转不带真值、工具结果/transcript/模型输入里脱敏；删除、外发、带密钥或写方法的 HTTP、`keys:true` 都出确认卡，拒绝后不执行；只读不问；`decisions.jsonl` 留痕。修复：强制 `git push` 不确认；密钥挪到 `<root>/keys/vault/<bot>.key`（旧数据打开时迁移），触及保险箱/密钥路径的 `shell`/文件工具无论机器开关都出确认卡。独立用户守护进程 `xbot vaultd`（[ADR 0005](adr/0005-vault-daemon-user.md)）已在盒子上以 `xbot-vault` 用户端到端验证：迁移后 Bot 读不到密钥和密文，注入、拒绝未绑定域名、跨域跳转不带真值照常；其他 uid 连接被 SO_PEERCRED 拒绝；小请求额外开销 p95 0.22 毫秒（300 次）。遗留：Bot 所在用户若有免密 sudo，隔离形同虚设 |
| M8 | 开源网关与跨设备 Link（[GATEWAY.md](GATEWAY.md)，G1–G7）；P2P、跨用户通信、模板分享推迟 | G1–G7 全部已合入：网关、身份与 `bot@设备` 收发、邀请码配对、端到端加密、发件队列与退信、`list_bots` 目录推送、桌面「设备」抽屉配对（`internal/web/static/devices.js`）。盒子上两个数据根 e2e 通过（配对、加密往返约 1.9 秒、网关停机后重试送达、目录推送）。官方网关 `https://xbot.babelgo.cn`（Cloudflare Workers，ADR 0011）已部署，客户端默认连它，两台安装经它配对和收发实测通过（2026-10-18，往返约 1.2 秒） |
| 桌面端 | 本地优先桌面端（Windows、Mac），见下文「本地优先桌面端」 | A、B、C 三线代码已交付：`xbot app`、跨平台核心、打包出 dmg 与 Windows 安装器，GitHub Actions 上 Windows 与 macOS 全量测试在跑。真机逐项验收未做 |

全局验收：每个里程碑都要报性能数字（交互回复 p95、IPC 开销 p95、后台变慢比例、链路数据量；涉及界面的报帧率），预算见 [AGENTS.md](../AGENTS.md) 的「全局要求：性能与界面流畅」。

## 8 步内核计划（2026-10-17 定）

来自 [Claude Code 内部机制调研](research/claude-code-internals.md)，用户批准。文件检查点和回滚、hooks、规划模式不做（见候选清单）。这里的 M1–M8 是这份计划的编号，和上面总表的里程碑不是一回事。

| 步 | 内容 | 状态 |
|---|---|---|
| M1 | 提醒通道，熔断与打转检测 | 已合入 |
| M2 | 文件新鲜度 | 已合入 |
| M3 | 待办清单 | 已合入 |
| M4 | shell 后台化 | 已合入 |
| M5 | 环境感知（AGENTS.md 分层加载、持久 cd、token 用量、外部改动 diff） | 已合入 |
| M6 | 搜索与看图 | 已接上 |
| M7 | 子代理类型（工具白名单、提示片段、步数上限、防递归） | 待做（步数上限 `MaxSteps` 已在 `internal/exec` 里） |
| M8 | 权限规则（deny → ask → allow，「不再询问」） | 暂缓（2026-10-18，[ADR 0013](adr/0013-features-before-security.md)：先把功能调通调顺，安全只留接口） |

两周验收：三个 Bot 从 2026-10-16 连续运行到 2026-10-30。

## 提醒通道与熔断（研究笔记里的 M1）

相对 v0.3.0。提醒只追加在最新一条消息末尾，sidecar 为 `reminders.jsonl`，重放不改历史。已接上待确认卡（10 分钟冷却，定时唤醒不带）、打转警告、后台完成；`todo` 与文件改动走同一个注册口。熔断六项：重复输出、同参工具、文件来回改、同一失败轮询、收益递减、连续 3 次确认被拒。收益递减只统计后台因步数上限开的续段（开口那段不算），连续 3 段且最近两段助手新增各少于 500 token 先提醒，下一段硬停；普通短工具步不计入。第一次提醒，第二次硬停并写 `<stop_reason>` 与 `trace.jsonl`。中断、取消、换模型的悬空工具调用收成 `DanglingResult`。常驻规则里的「失败最多再试一次」改到轮询提醒上。DeepSeek 模型提示文件未改。

假模型回归：`go test ./...` 连续三遍。本机基准（`go test -bench`，amd64）：`BenchmarkReminderBind` 287 ns/op，`BenchmarkBreakerObserve` 696 ns/op。一步熔断观察远低于 IPC 5 毫秒预算；不涉及界面帧率。未跑真模型评测。

评审后收紧：同参/轮询只数连续且按命令分计，成功清掉失败计数，只读和 `go test`/`build`/`vet` 放宽；提醒脱敏后落盘，绑到刚写下的序号，段与段之间按序号贴回而不是粘在最新一条；`reminders.jsonl` 缓存并压掉活窗口之外的旧行。缓存只在文件大小和 mtime 都没变时复用，另一个进程写过之后重新装入。

## 文件新鲜度（研究笔记里的 M2）

相对提醒通道。每个 Bot 的活内核记下本进程读过和自己写过的文件（路径、mtime、大小、哈希，最多 64 个，哈希按流计算、最多前 4 MiB）。已经存在但本会话没读过的文件，`edit_file` / `write_file` 报错让先读；大小或哈希对不上就拒绝，并说明自第 N 步之后文件变了。只有 mtime 变、哈希没变，允许写。大小和 mtime 都相同且不是截断哈希时跳过内容哈希，同样大小、同样 mtime 的替换会当成没变。自己写成功后刷新记录，结果里写明不必再读。`write_file` 可以把记录标成完整；`edit_file` 保持改之前的完整或局部。同一轮里没变的完整重读返回短回执，除非要了行范围或 `force`；下一轮重新读正文。外部改动用 `file_change` 提醒，带短 diff，冷却 1 分钟，同一次变化不重复；`.env`、`*.key`、`*.pem`、credentials 一类文件只写 changed。被拒绝的调用在这一轮里不能用相同参数再试，下一轮可以再问。

假模型回归：`go test -count=1 ./...` 连续三遍；`go test -race ./internal/kernel/...`。基准（本机 amd64）：`BenchmarkFileFreshGuard` 7076 ns/op、3272 B/op，未变文件不重新哈希；`BenchmarkSnapshotSmall` 2984 ns/op、576 B/op（6 字节文件，原先按 4 MiB 分配）。都远低于 5 毫秒。不涉及界面帧率。未跑真模型评测。评测脚本里改已有的 `site.css` / `view.html` 之前先读一遍。内核测试把 `TMPDIR` 指到仓库里的 `.test-tmp/kernel/p<pid>`，每个测试进程一份，避免并行的 `go test` 互相删掉临时目录。

## 待办清单（研究笔记里的 todo，本轮称为 M3）

相对提醒通道那一版。工具 `todo` 写一份短清单：content，状态 pending / in_progress / done，放弃用 dropped 加原因，同时最多一项 in_progress。存在 `bots/<name>/todos.json`（0600），一个 Bot 一份。状态接口多一行 `todo` 摘要；全文是 `GET /api/bots/{name}/todos`。步骤环和网页侧边栏不画它。

提醒走已有的 `todo` 种类。活动清单在，并且距上次成功更新已经过 N 步（默认 6，模型 `todo_steps` 或内核 `TodoSteps`）才发；只发相对上次提醒的差异；冷却 2 分钟；刚更新的那一步不发。步数记在内存里，开关关掉时不碰文件，只是计数也不重写。模型配置 `todo` 显式开关，缺省时名字里带 deepseek 的才开（空绑定也算）。除 `links`/`todo` 外全是只读工具的一轮不发。定时唤醒不发待办提醒，也不跑完成门。清单跨回合保留：用户说继续，或前台插话时后台还在跑，都不清掉。每一项都已 done/dropped、写入空清单、或新清单和旧清单没有相同 id 时，旧清单追加进 `todos-archive.jsonl`，活动清单清空。完成门只在这一轮写过或改过清单、模型又要在还有未完成项时停下时，另起一条用户消息注入一次；最后一句是在向用户提问，或还有待确认卡时，不注入。同一轮只一次。步数上限和熔断照旧，不会为了清单一直续。

假模型回归：`TMPDIR` 在仓库内，`go test -count=1 ./...` 连续三遍，`go test -race ./internal/kernel/...`。本机基准（`go test -bench=BenchmarkTodoDiff -benchmem -benchtime=200ms`，amd64）：3569 ns/op，5808 B/op。远低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测，未改网页步骤弹层。

## shell 后台（研究笔记里的 shell 后台化，本轮称为 M4）

相对待办清单那一版。`shell` 默认超时 120 秒，最长 600 秒，模型可传 `timeout_seconds`。到点不杀进程，转成后台，回一条短结果：任务 id 和输出文件路径。`run_in_background` 立刻转后台。输出写在 `bots/<name>/shell-jobs/<id>.log`（目录 0700，文件 0600）。原始字节先留在内存里。最后一段至少 1024 字节、也至少比已知最长密钥形式多 1 字节，这段不进文件、预览或 `shell_output`。每次写入按替换区间把原文偏移映射到脱敏结果，只提交窗口之前已经确定的那段。结束时把剩下的全部脱敏后写入。文件超过 256 KiB 再丢掉头部。已提交的流长度只增不减。`shell_output` 的 offset 按整段流计算，丢掉的前缀会写明。`shell_output`、`shell_status`、`shell_cancel`（杀进程组）在权限表里分别是只读、只读、普通，只有挂了任务表的 shell 才出现。同一 serve 进程里跨回合还在。重启把对不上 `proc_start` 的 running 标成 lost，不向旧 pid 发信号；另一个进程如果对上了仍活着的 pid 和 `proc_start`，就不改这条记录。完成提醒或叫醒会把 `notified` 写进任务记录，同一次完成不会又提醒又叫醒。只有本进程结束的任务才往收件箱投事件。lost 最多提醒一次。结束的任务只留最近 20 个、且不超过 7 天。`Close` 杀掉本进程拥有的进程组。完成提醒走已有的提醒通道，种类 `shell_job`，贴在下一步工具返回之后，不打断正在跑的调用。Bot 空闲时往收件箱投一条来源 `shell` 的不可信事件叫醒它，不另开 `bg_` 段，熔断照旧。`sleep`、`watch` 和带 `sleep` 的 `while`/`until` 不自动转后台，交给熔断；显式 `run_in_background` 仍可把 `sleep` 放后台。评测沙箱仍是 30/120 秒并在超时杀掉，工具名单不含 `shell_output` / `shell_status` / `shell_cancel`。没挂任务表的 shell（节点、命令行）也是到点杀掉。

假模型回归：`TMPDIR=/workspace/.test-tmp` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race` 覆盖 `internal/kernel`、`internal/tools`、`internal/eval`、`internal/review`。未提交的尾部不落盘。提交点按替换区间从原文偏移映射过来。`TestJobLogRedactionProperty` 做 10000 次随机切块。本机基准（amd64，30 次）：`BenchmarkJobWriterTrickle` 1492 ns/op，3712 B/op，3 allocs/op；`BenchmarkJobWriterBulk` 106015 ns/op，179063 B/op，3 allocs/op（一次约 32 KiB）。20k 行按这样一块写，写端大约 4 ms。单块低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测，未改网页。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`，`go test -fuzz`（目标在，正常测试只跑种子）。

## shell 输出少留（M4 跟进）

运行中的任务不再固定留住最后 1024 字节。待写尾巴里只留最长的、正好是某个已知密钥形式（原文、JSON、URL、base64，以及暂存密钥原文）前缀的那一段；对不上时，完整脱敏过的输出立刻进文件和 `shell_output`。还留着的字节数在 `shell_output` 和 `shell_status` 里写成 `held_bytes: N`（大于 0 才写）。`scrubSecrets` 按从长到短替换，一样长按字节序。测试里的 PEM 头尾在运行时拼接，扫描器不会把假私钥当成真的。

假模型回归：`TMPDIR=/workspace/.test-tmp` 跑 `go test -count=1 ./...` 连续三遍，默认 `TMPDIR` 一遍。`go test -race` 覆盖 `internal/kernel`、`internal/tools`、`internal/eval`、`internal/review`。`go test -fuzz=FuzzJobLogRedaction -fuzztime=2m ./internal/tools/`。本机基准（amd64，`-benchtime=200ms`，脱敏函数同时认一个短 token 和约 1.7 KiB 的 PEM 形式）：`BenchmarkJobWriterTrickle` 4249 ns/op，432 B/op，3 allocs/op；`BenchmarkJobWriterBulk` 93232 ns/op，253532 B/op，3 allocs/op（一次约 32 KiB）。单次写入远低于本机 IPC 5 毫秒预算。2 万行按 32 KiB 一块写，写端大约 4 毫秒。不涉及界面帧率。在 `a9c1ffa` 上会失败的回归：`TestShortRunningOutputIsVisible`、`TestShellTimeoutMovesToBackground`、`TestBackgroundFileRedactsSecret`、`TestShellJobFileUsesKernelRedactor`、`TestRunningShortOutputAndHeldPrefix`、`TestScrubSecretsLongestFirst`、`TestScrubSecretsStableTieBreak`。Not run: 真模型评测，界面浏览器检查（没有界面改动），这四个包以外的 `-race`。

## 环境感知（研究笔记里的 M5）

相对 shell 后台那一版。四件事都走已有的提醒通道，不进系统提示，也不新加工具。

项目说明：调用方的工作目录（没设工作区时不读进程当前目录）向上读到工作区或共享目录根。每层有 `AGENTS.md` 就用它，否则用 `CLAUDE.md`，近的在后。同一套只注入一次，`cd` 进了另一个子目录才再发。目录和候选文件的 mtime、大小、inode、ctime 没变就只 stat，不再读文件、不再哈希。合计最多 32 KiB，更远的文件先丢掉，截断时正文写 `[truncated]`。关掉标签按 `(?i)<\s*/\s*project_instructions` 拆开，大小写和空白都盖住。文件用 `O_NOFOLLOW` 打开，最后一段是符号链接就不跟。允许的根以外的路径不读。工作区根上、且该目录不是 git 检出、也不是共享目录时，文件是用户放的项目约定，贴在最新一条消息上。共享目录、带 `.git` 的工作区根、子目录或克隆仓库里的是仓库提供的数据，不是用户的指示，只作编码约定，不能当作批准、发送、读密钥或删除的依据；整段包在 `<untrusted_event source="project">` 里，单独成一条，不写进用户那条可信正文，下次组装也不贴回去。权限表不看这些正文，写入提醒前仍过脱敏。稳定规则加了 `ProjectRule`（`DefaultRules`）。新 Bot 的 `rules.md` 带上它；已经写好的 `rules.md` 不变。`evals/` 和 `internal/eval` 没有提示词快照或哈希基线可改，这次稳定前缀变长是有意的。

持久工作目录：前台 `shell` 的 `cd` 留到下一次调用，记在内存和 `bots/<name>/cwd`。每个后台分支按分支 id 另有一份，每个子代理在创建时继承调用方当时的目录；这三份互不影响，只有前台写入 `bots/<name>/cwd`。每次调用确认目录还在、并且在工作区或共享目录里；丢了或不在里面就回到工作区，结果里写明。结果在状态行后面加 `cwd:`。`false` 和显式 `exit` 也留下目录。后台 shell 任务只记下启动时的目录，不改调用方的会话目录。到点转后台的那次也不改。cwd 标记在 `bots/<name>/cwd-markers`，不放进程临时目录。没挂会话的 shell（评测沙箱、节点、命令行）每次仍从自己的目录开始。查项目说明或做权限判断时不把回退写进这份记录，免得把「目录丢了」的说明吞掉。`read_file`、`write_file`、`edit_file`、项目说明、权限判断和后台冲突检测的相对路径按调用方的目录解析；绝对路径不变；新鲜度记下的是绝对路径。评测沙箱的文件工具仍钉在沙箱根上。

用量：流式请求带 `include_usage`。有 usage 对象就按原数记 prompt、completion、cached，0 也算报告过，不用估计值替换。没有对象才按大约 4 字一 token 估计，trace 的 reason 是 estimated，提醒里写 estimated。prompt 跨过该模型 `providers.json` 上下文窗口的 50/75/90% 时，提醒通道发一行；同一档不重复，掉下去再跨过才再发。没有窗口长度就不发百分比，计数仍进 `trace.jsonl` 和 `BotStatus.tokens`。本进程累计，重启从 0 开始，历史在 trace 里。

外部改动：`file_change` 改成不超过 4 KiB 的统一 diff，放不下写 `[diff truncated]`。敏感文件名仍只写 changed。自己 `write_file` / `edit_file` 成功后刷新记录，冷却过了也不再报。

提醒通道这一轮多出来的 token（`compact.Estimate`，大约 4 字一 token，不是供应商分词器）：一份大约 3094 字节的 `AGENTS.md` 第一次注入 3419 字节、855 token（根上的约定，措辞比第一版稍长）；下一轮新注入 0 字节，历史里仍带着那 855 token，直到压缩。再 `cd` 进一个大约 1 KiB 的子项目说明后，新的一套是 4634 字节、1159 token（含不可信包裹）。50% 那一行 133 字节、34 token，只有跨过门槛才发。一小段外部 diff 大约 34 字节。读这一套项目说明的平均耗时 40 微秒（50 次，含第一次读盘）；基准 `BenchmarkProjectText` 38242 ns/op，17868 B/op，218 allocs/op（amd64，200ms，命中按 mtime、inode、ctime 缓存的路径）。远低于本机 IPC 5 毫秒预算。不涉及界面帧率。

评审留下、这次没改的两处：shell 里 `sed -i` 改到的文件仍可能被 `file_change` 报成外部变化（只有 `write_file` / `edit_file` 会刷新新鲜度）。后台段记下的用量可能盖住前台的 prompt 计数（`noteUsage` 写的是同一份内核计数）。`internal/browser` 的 `TestStopChecksCommandLine` 在 main 上也会失败，不追。评审留下的 P2/P3 都记成了 issue：#19–#27，另有 #18（已有 Bot 的 `rules.md` 拿不到 `ProjectRule`）。

假模型回归（评审修复这一轮）：`gofmt -l` 对已跟踪的 Go 文件无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。Not run: 真模型评测，界面浏览器检查（没有界面改动），这五个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。`evals/` 没有提示词基线文件可更新。

再评审：子代理或后台分支的 `cd` 不再改前台目录，前台相对 `write_file` 仍落在前台目录。工作区根自己是 git 检出，以及共享目录上的 `AGENTS.md`，都按仓库提供的数据处理。项目说明缓存键含 inode 和 ctime。回归：`TestSubagentCwdDoesNotMoveParent`、`TestBackgroundCwdDoesNotMoveForeground`、`TestReviewFileConflictUsesCallerCwd`、`TestProjectRootCloneIsUntrusted`、`TestProjectSharedDirIsUntrusted`、`TestProjectCacheSeesSameMtimeEdit`。

假模型回归（目录按执行上下文分开这一轮）：`gofmt -l` 对已跟踪的 Go 文件无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/full` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。`BenchmarkProjectText` 38242 ns/op，17868 B/op，218 allocs/op。Not run: 真模型评测，界面浏览器检查（没有界面改动），这五个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

## 搜索与看图（研究笔记里的 M6）

相对环境感知那一版。三个只读工具挂在内置 tools Link 上，权限表里是 `readonly`。工具自己的 `Confirm` 不决定要不要问。`view_image` 算本机工具，机器开关 `off` 拒绝、`ask` 出确认卡；`web_fetch` 和 `web_search` 不走这道开关。本地路径触及保险箱或密钥时，`view_image` 和文件工具一样出确认卡。

`web_fetch` 只取公开 http(s)。解析之后和每一次重定向前都拒绝私网、回环、链路本地、元数据、运营商级 NAT 和其余 IANA 专用段；十进制和 `0x` 字面地址同样拒绝。名字解析两次，第二次不再是公网就当成 DNS 重绑定拒绝。重定向最多 5 次，超时 20 秒，不走环境代理。HTML 收成文字，`focus` 只留含该词的段落。结果包在 `<untrusted_event source="web_fetch">` 里，正文里的结束标签会被拆开。大约 48 KiB 以上截断。再长的工具结果仍按原有 transcript blob 存全文、行内只留预览。

`web_search` 的代码默认仍是 DuckDuckGo 的 HTML 接口，不需要密钥。部署选 DeepSeek 时在 `web.json` 写 `search.backend` 为 `deepseek`（或 `XBOT_SEARCH_BACKEND=deepseek`），`base_url` 可省略，省略就是 `https://api.deepseek.com`。请求是官方 Search API：`POST /v1/tools/search`，正文只有 `query`（1–128 个字符）和 `count`（1–50；工具侧默认 5、最大 10），不传 `need_content`，没有模型，也不走 Responses API。成功且结果非空按次计费（1000 次 5 元），空的 `results` 算成功、不计费。402 是余额不足，429 是超过每秒 5 次。没写 `api_key_env` 只用 `DEEPSEEK_API_KEY`，不回落到 `XBOT_API_KEY`。现场只有 `XBOT_API_KEY` 时，在 `web.json` 写 `"api_key_env":"XBOT_API_KEY"` 显式复用，例如 `{"search":{"backend":"deepseek","api_key_env":"XBOT_API_KEY"}}`。供应商密钥（`DEEPSEEK_API_KEY`、`XBOT_API_KEY`，或 `providers.json` 里的 `api_key_env`）只能发往 `https://api.deepseek.com`。`providers.json` 读失败时也按供应商密钥处理。已经收进内存、但不在这份名单里的搜索密钥仍可以走别的 https 主机。`http` 一律拒绝。单独的变量名会和供应商密钥一样从进程环境清掉；启动之后才写进 `web.json` 的，下次配置工具时再清一次，并进入脱敏。密钥不进日志。旧的 `search.model` 字段还读得进来，但忽略。DuckDuckGo 遇到非 200 或挑战页会报错，让模型告诉用户，不说明怎么换后端；查询词被当成回显，不拿来判断挑战页。付费抓取后端没有默认实现，配了未知的 `fetch.backend` 就失败。结果是标题、URL、摘要，有 summary 和 date 时一并写上，同样包成不可信数据。围栏里的结束标签换成 `[/untrusted_event]`。`web.json` 的 `trusted_egress_cidrs` 只放行 DNS 答案（例如 `198.18.0.0/15`），不放行字面 IP；`::ffff:127.0.0.0/104` 这类映射地址按里面的 IPv4 再对一遍回环和链路本地。

`view_image` 接受本地路径或公开图片 URL，JPEG、PNG、GIF、WebP。文件超过 32 MiB 不读，超过 10 MiB 不送给模型。边长超过 2048 会缩小；WebP 超了就拒绝。读不出宽高就拒绝。边长不超过 2048 的 JPEG、PNG、GIF 原样通过。要缩小的那一路才按 160 MiB 封顶，解码一次只跑一张，缩小后再看有没有超过 10 MiB。WebP 不解码，不占这份额。相对路径用调用方的工作目录；解析符号链接后必须在工作区或共享目录里，沙箱里钉在沙箱根。看图能力写在模型条目的 `vision` 上。`deepseek-flash`（名字里同时有 deepseek 和 flash，含 `group/auto-deepseek-v4-1-flash`）默认能看；`deepseek-chat` 和 `deepseek-v4-pro` 默认不能。显式 true 或 false 盖过名字。没绑定供应商时跟 `XBOT_MODEL`，没设才是 deepseek-flash。不能看的模型得到 `model <id> does not support vision`，图片不会悄悄丢掉。能看的模型收到 `content` 数组：一块 `text`，一块 `image_url` 数据 URL。同一轮只保留最新一批。一轮最多 4 张、合计 10 MiB。图片字节不进 transcript，这一轮结束才从内存里清掉，下一轮没有。子代理连续调用工具时只留最新一批，在没有工具调用的回复之后全部清掉，再接排队或插话，所以连续或跨五轮各看一张时第五张都能附上。16 位彩色 PNG（类型 2、4、6）按 8 字节每像素估计，16 位灰度按 2 字节；渐进 JPEG 按 12 字节每像素，4032×3024 的手机照片会缩小，不会被拒。`xbot provider add --vision on|off` 写入，省略时保留原值。工具执行里的 panic 变成工具错误，不会把 serve 打崩。确认回调里的 panic 会放开锁，panic 文本先脱敏再按字节截断。HTML 扫描用原文字节上的 ASCII 大小写比较，避免 `İ` 这类变长小写把偏移用错。

评测认证套件没有加新的提示，也没有把这两个工具放进沙箱工具表。假模型用例在 `internal/kernel` 和 `internal/tools` 里，搜索和抓取都用假后端，不访问网络。

假模型回归：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/full` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`。本机基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 77.37 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 24843 ns/op，10864 B/op，11 allocs/op（约 177 MB/s，一份重复的小页面）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。未跑真模型评测。评测认证套件的系统提示没有改；tools Link 的一行摘要也没改，新工具只出现在工具 schema 里。

评审修复这一轮：`1aee130` 上 `HTMLToText(strings.Repeat("İ",200)+"<p>a</p><script>b</script><p>c</p>")` 会 panic `[416:234]`，现在这条回归通过，并留下 `a` 和 `c`、丢掉脚本里的 `b`。工具 panic 收成 `error: tool panic`。结束标签不分大小写、容许空白，全角 `＜／` 也会拆开。`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 76.53 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 18001 ns/op，10864 B/op，11 allocs/op（约 244 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。用户在 deepseek-flash、思考关闭下看过 `view_image` 是对的；网页工具被部署环境的 `198.18.0.0/15` 假 IP 挡住，DuckDuckGo 当时返回 0 条（非 200 的挑战页）。Not run: 这一轮没有再打真模型，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`b1aee62`）：DeepSeek 搜索改成官方 `POST /v1/tools/search`，正文只有 `query` 和 `count`。Responses API、`deepseek-v4-pro` 搜索模型、`search.model` 和散文回退都去掉。402 报余额不足，429 报频率。密钥只用 `DEEPSEEK_API_KEY` 或显式 `api_key_env`，不回落到 `XBOT_API_KEY`；供应商密钥只发往 `https://api.deepseek.com`。`web.json` 和 `providers.json` 要确认卡。启动之后才写上的搜索密钥，配置工具时再收进内存并脱敏。不可信围栏的替换改成 `[/untrusted_event]`，不再是还能被认成结束标签的 `</ untrusted_event`；实体、零宽字符和多出来的斜线、下划线、尖括号也拆开。确认回调的锁用 `defer` 放开，panic 文本先脱敏再截断。DuckDuckGo 的报错只让模型告诉用户，查询词回显不当成挑战页。看图读不出宽高就拒绝，解码按位深限制并串行，缩小后再对 10 MiB。`::ffff:127.0.0.0/104` 不能写进出站白名单。Responses API 的注释偏移随那个解析器一起删掉。子代理工具仍不经过 `reviewedTool`。

假模型回归（Search API 这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 82.58 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 17347 ns/op，10864 B/op，11 allocs/op（253.64 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`3f8db10`）：看图的解码字节上限只加在真正要缩小的那一路，一张位图按 4096×4096×4 估计；2048×1536 原样通过，2560×1440 和 3000×1200 会缩小，16 位 8000×4000 拒绝。WebP 不解码，不占这份额。工具图片留在这一轮剩下的每一步请求里，仍是 4 张、合计 10 MiB，这一轮结束才清掉，不进 transcript，下一轮没有。搜索密钥在 `providers.json` 读失败，或名字属于启动时的 `KeyEnvNames`、或已经收进内存时，只能发往 `https://api.deepseek.com`。现场只有 `XBOT_API_KEY` 时，用 `"api_key_env":"XBOT_API_KEY"` 显式复用。数字字符引用的前导零和超过一个码位的位数会拆开；零宽连接符、不可见运算符和变体选择符也跳过。收进内存的密钥算进 shell 的保留形式，跨两次写入不会漏出去。脱敏用的密钥列表缓存到下次 Hold 或 Forget。`link.Wrap` 仍记在 issue #34，这次没改。子代理工具仍不经过 `reviewedTool`。

假模型回归（这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 79.46 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 17487 ns/op，10864 B/op，11 allocs/op（251.61 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`89848e0`）：子代理在一轮以没有工具调用的回复结束时清掉图片，发生在排队任务和插话之前。跨五轮各看一张时第五张仍然能附上，第一轮的图片不会留在那次回复之后的请求里。主线同一轮只保留最新一批。上下文估算把图片字节按大约 4 字节一个 token 算进去。16 位彩色 PNG（类型 2、4、6）按 8 字节每像素，16 位灰度按 2 字节；渐进 JPEG 再加一块同样大小的缓冲。搜索密钥算不算供应商密钥，只看 `DEEPSEEK_API_KEY`、`XBOT_API_KEY`、`providers.json` 的 `api_key_env`，以及注册表读失败。已经收进内存的单独搜索密钥仍可以走别的 https 主机。`link.Wrap` 仍是 issue #34。子代理工具仍不经过 `reviewedTool`。

假模型回归（子代理看图这一轮）：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/review` 跑 `go test -count=1 ./...` 一遍全绿（含 `internal/browser`）。`go test -race -count=1` 覆盖 `internal/kernel`、`internal/tools`、`internal/provider`、`internal/bot`、`internal/eval`、`internal/agent`、`internal/config`、`internal/exec`、`internal/compact`。基准（amd64，`-benchtime=200ms`）：`BenchmarkPublicIP` 90.39 ns/op，0 B/op，0 allocs/op；`BenchmarkHTMLToText` 19150 ns/op，10864 B/op，11 allocs/op（229.76 MB/s）。都远低于本机 IPC 5 毫秒预算。不涉及界面帧率。Not run: 真模型评测，界面浏览器检查（没有界面改动），上面几个包以外的 `-race`，`go test -fuzz`，默认 `TMPDIR` 再跑一遍，合入前的连续三遍。

再评审（`e818aa7`）：子代理在一串工具调用里每次有新图片就清掉之前消息上的图片，连续五次 `view_image` 时最后一次请求只带第五张。渐进 JPEG 改成按 12 字节每像素估计，解码上限 160 MiB，解码仍一次一张；2560×1440、3000×2000、4032×3024 的真实渐进 JPEG 都缩小通过。回归：`TestSubagentImageStreakKeepsNewest`、`TestProgressiveJPEGPhotoPassesCap`、`TestDecodeCapAllows16BitRGBAAndRefusesOversizeSide`，在 `e818aa7` 上失败。修复由本机 dsh 写，独立评审是 zcode。

## 两层记忆（2026-10-17）

用户决定记忆只留 person 和 bot。person 跨所有 Bot 共享，只放每个 Bot 都需要的、关于用户本人的事实（时区、语言、作息、他是谁），写入必须显式带 scope。bot 是这个 Bot 自己的角色、约定和项目决定，也是默认：省略 scope 的 `remember`、自动抽事实、不可信回合的自动写入都进 bot。工作规则和项目决定不进 person。bot-person 不再存储。怎么做事的冲突以 Bot 自己的记忆为准，压过 person。常驻注入从三份 profile（`3×4KiB+512`，12800 字节）改成两份（`2×4KiB+512`，8704 字节）。

迁移每个数据根每进程走一次（`sync.Once`）。`serve` 和 `chat` 打开内核时触发，Remember、Recall、Forget 也会触发，但同一进程里不会重复走。只合并该 Bot `owners` 里的人：Bot 记忆每个发送者都看得见。其他人的目录改名为 `memory.bak`，不并进去。某个目录读失败就记日志、跳过、留在原地，不让别的 Bot 的读写失败，也不让启动失败。合并去重，保留时间戳、来源和原来的 kind（空 kind 不改写成 work）。profile 放不下的改记 log，并写一条 `migration-profile-cap`。log 超过 64 KiB 时，多出来的写到 `log.overflow.md`，`log.md` 本身不超过 64 KiB；检索和 forget 把这份溢出和日志一起读。已经超限的 profile 或 log 上，缩小文件的 forget 仍然成功。人的同步日志仍是 person 通道。不可信回合的 `remember` 和抽事实，不论落在哪一层，都标 `Untrusted` 并加上 provenance。

网页档案不再读已删除的 bot-person 范围，界面上也不再标「你们之间」。`ScopeBotPerson` 常量还留着，存储层拒绝这个范围。

第二轮：包在 `<untrusted_event source=...>` 里的内容，即使这轮是可信的，抽出来的事实也标不可信。`config.json` 缺失、读不了或坏掉不算没有用户，旧目录留在原地；`owners` 为空才把旧目录改成 `memory.bak`，并记日志。迁日志时 `log.md` 留到大约 48 KiB，新的事实留在文件里。没有持久化的迁移标记：每次进程启动都重新走一遍 `bots/*/people/*/memory`，每个数据根每进程一次；迁过的目录已改名为 `memory.bak`，所以重复走是幂等的。同一个键上，最新的那条赢；出处只做标注，不改变取舍。来源后缀在长度检查之后补上，单条仍不超过 1 KiB。

空数据根上 `Migrate` 只做一次目录读取：844.3 ns/op。已有一个 Bot、没有旧的人目录时 7849 ns/op（amd64，`-benchtime=200ms`，一次性基准，没有提交进仓库）。都低于本机进程间通信 5 毫秒。不涉及界面帧率。

评审修复后再跑：`gofmt -l` 无输出，`go vet ./...` 通过。`TMPDIR=/workspace/.test-tmp/m33-{1,2,3}` 跑 `go test -count=1 ./...` 三遍。第 1、3 遍全绿。第 2 遍只有 `internal/browser` 的 `TestReleaseTiedLeavesCLI` 失败（`tied chrome still running`，2 秒没等到进程退出）；单独再跑这条和 `TestStopChecksCommandLine` 通过。`TestStopChecksCommandLine`（issue #27）这三遍没有失败。`memory-scope` 的检查放宽成子串 `run the tests` 和 `Asia/Shanghai`。回归：`TestM33UntrustedDefaultScopeRememberIsAttributed`、`TestM33CorruptLegacyIndexDoesNotBreakOtherBots`、`TestMigrateCorruptOwnerStaysAndOtherBotWorks`、`TestMigrateLeavesNonOwnerInBackup`、`TestMigrateLogStaysWithinCap`、`TestForgetOnOverCapLogSucceeds`。Not run: 真模型评测，界面浏览器手点（网页只删了 bot-person 的读取和标签），`-race`，`go test -fuzz`，默认 `TMPDIR`。

## 记忆 v2（2026-10-17 定）
规格见 [MEMORY-V2.md](MEMORY-V2.md)，决策见 [ADR 0008](adr/0008-memory-v2-dated-log-first.md)、[ADR 0009](adr/0009-memory-as-folder.md)。批 1–5 已落地；批 6、7 未做，排期见候选清单。

| 批 | 内容 | 状态 |
|---|---|---|
| 1 | 快照加差异：会话开始和压缩时拍快照（含最近 8 条日志），中途只追加差异，自己的改动不追加 | 已完成 |
| 2 | 目录布局（`memory.md`、`log/YYYY-MM.md`）和迁移 | 已完成 |
| 3 | 记忆路径上的文件工具把关；拿掉 `remember`/`forget` | 已完成（工具已从内核移除） |
| 4 | 只读的 `recall`（含自由文件） | 已完成（`internal/kernel/recall.go`） |
| 5 | 经过摘要 | 已完成（压缩时写 `episode`，`internal/kernel/extract.go`） |
| 6 | 每晚整理（含索引核对、工作档案） | 未做 |
| 7 | 导入 Grok 导出、`xbot backup` | 未做 |

## 撤下的提示类改动（v0.3.0 → v0.4.0）
v0.3.0 只发内核修复。下面三项提示类改动做完、过了 review，但都拉低了 DeepSeek 的 give-up 用例（向一个死端点 POST，应当如实报告做不到，最多 4 次工具调用），已撤回。重做的顺序在候选清单，不在这里排。

- **模糊任务规则**（「一两句计划后同轮开工」）。加上后全量 eval 里 give-up 连续 0/3、1/3、1/3，三轮改措辞没稳住：模型在目标已写明的任务前先翻目录、补细节，超出工具上限。
- **知识问题三档规则**，连同 knowledge-public / knowledge-project / knowledge-fresh 三条用例和 eval 的 `no_tool_calls` 检查。
- **交互回复默认低推理强度**（profile `reasoning_effort`）。

give-up 单测数据（每个构建 6 次，2026-10-17）：7e64ec6 6/6；加上内核修复、没有知识规则 6/6；再加知识规则 5/6；再加低推理强度 4/6。拿掉模糊任务规则后的全量 eval 两遍都只有 give-up 1/3 不过。证据：`/workspace/xbot-test/weekly/v030/`（evalB/D/E/G/H、gu-*.log）。

另：第 9 项（中文回复前的英文开场白；`xbot provider add/list/remove`；`xbot new` 拒绝未知供应商）也留到下个版本。

成绩单已落地：`xbot eval` 的报告带四维成绩单（能力、意愿、可靠性 pass^k、守规矩一票否决，另报 Ask-F1），`xbot eval scorecard <report.json>` 可从旧报告重算。规格见 [SCORECARD.md](SCORECARD.md)，实现见 [atlas/providers.md](atlas/providers.md)。

## 插话时分叉（F1–F5）
设计见 [插话时分叉](atlas/fork.md)。
| 切片 | 内容 |
|---|---|
| F1 | transcript 层：`branch` 字段、`fork`/`fork_end` 标记行、并发安全的 seq、界面历史和导出的重放规则（先做最简显示，不做卡片），旧数据照常读 |
| F2 | 分叉内核：按轮状态拆进 turn 结构，插话时把正在跑的一轮登记成 `bg_` 执行器；可以同时有多个后台，前台提示列出全部在跑的后台；完成事件回收件箱 |
| F3 | 连续插话与资源冲突：前台硬上限，到上限就转成新的后台；每个后台登记自己要碰的资源（文件、仓库、工作目录），和在跑的后台重叠才排队，否则并行；上限 3 个；权限表加「前台写操作交给后台」 |
| F4 | 取消与崩溃恢复：`cancel`/`interrupt`、重启补写 `interrupted` |
| F5 | 网页后台任务卡：审美顾问出原型，政东定稿后实现；按第 8 节验收 |

| 切片 | 状态 |
|---|---|
| F1 | 已完成；遗留项（崩溃修复的占位结果带上所属后台的 branch）已在 F2 分支补上 |
| F2 | 进行中。A：按轮状态挪进 `turn` 结构（完成）。B：内核分叉机制（完成，`Kernel.AllowFork` 开关默认关）：用户插话时把正在跑的一轮登记成 `bg_` 后台（最多 3 个），写 `fork` / `fork_end` 标记和 `agents/<id>/meta.json`，前台提示列出全部在跑的后台，结束时往收件箱投 `fork_end` 事件；崩溃修复能处理多个分支各自末尾的未答调用。C：`serve` 的网页路径已打开分叉（完成）：每个 Bot 一个长期内核，进程内的请求共享处理锁，锁一直拿到所有后台结束；插话作为前台走 SSE。盒子回归：连发 5 句插话，响应 0.01–0.12 秒；后台跑完写 `fork_end`，完成事件由 `serve` 处理并写进通知。F2 完成，遗留见下 |
| F3 | 完成（除前台硬上限，见下）：后台确认走卡片给用户（`BackgroundApprove`）；后台 `steer`/`queue`/`interrupt`/`cancel`，控制消息有 `pending`/`applied` 状态，下一步生效；资源冲突排队（同文件、同目录、同仓库的前台写操作排到对应后台之后，其余并行，后台上限 3）；队列上限 5，排队项带原因和位置、可取消，满了回「队列已满」；后台在跑时网页进程自己处理收件箱里的定时器等事件；删除或改名 Bot 时清理锁记录并取消后台；`GET /backgrounds` 给 F5 的角标和弹层用。盒子回归（假模型）：两个后台并行、插话 0.03 秒回复、steer 下一步生效、冲突的写操作排到对应后台之后、另一个后台的 `fork_end` 在后台还在跑时被处理，两个后台都以 done 结束 |
| F4 | 完成：前台硬上限（4 次工具或 20 秒，可配置；到了就转成后台，槽位满时留在前台）；重启时把有 `fork` 没 `fork_end` 的后台补成 `interrupted`，更新 `meta.json`，只告诉用户一次，不自动续跑；取消路径写 `cancelled`、杀掉正在跑的命令。盒子回归（假模型）：4 次工具后转后台并发提示；20 秒转后台后 `kill -9`，重启后补写 interrupted、事件只投一次、再重启不重复；取消后状态 cancelled、`sleep` 进程已结束 |
| F5 | 完成：按原型做了顶栏「后台 N」角标（只数运行中，没有运行和排队时隐藏）和弹层：运行中（步数、最近工具、追加指示、取消，steer 显示待生效/已生效/已丢弃）、排队（原因、文件名、位置，可取消）、最近结束 3 个；手机是底部弹层。聊天里没有后台卡片，后台的回复和确认卡带「后台」来源标，控制消息和 `fork_end` 事件不显示；插话被拒（429）时显示「队列已满，请稍后再发。」。截图在 `xbot-test/f5/shots/`。验收（假模型）：5 次插话延迟最多 0.02 秒；后台耗时 16.2 秒，基线 16.1 秒（+0.6%）；2 个后台并行加 5 次插话，各 +0.6% 和 +1.9% |

F3/F4 遗留：前台不限制只读工具和单次 10 秒；槽位满时到上限的一轮留在前台；后台之间不按资源互相排队；只读 shell 的判断是一张短名单。

验收：连发 5 句每句 p95 3 秒内回复，后台耗时增加不超过 10%，并测 2 个后台并行。

## M6 切片状态
M6 每片的状态。已验证的是回归对过的；已接上的是代码里有、还没在回归里点过的。托管连接器市场不做。

1. **精选目录和安装**。已接上。一份内置清单，每条标明类型（原生或 MCP）、能力（工具 / 入站事件）、需要的密钥名。已经有运行时的条目可以装到某个 Bot 上，就是给它加上对应的 Link 授权；还没有运行时的条目照样列出来，安装会被拒绝。默认不因为这份清单多装任何东西。
2. **MCP 适配器**。已接上。stdio 与 Streamable HTTP 都把服务器包成这个 Bot 的工具。服务器记在该 Bot 的 `mcp.json`，密钥只记环境变量名，供应商 API key 不传。入站事件可选。不从精选目录一键安装。`xbot connector list/add/remove` 用打进二进制的 JSON 目录，按 Bot 启用，没有托管市场。
3. **连接器**。已接上。Telegram、飞书装上之后，外面的消息变成不可信 Event 进收件箱。代表用户发出去的消息先留草稿，用户确认后才发出。密钥只在确认卡或同名环境变量里填写。
4. **编码代理任务**。已接上。和子代理分开的任务记录：工作目录、仓库、步数上限、时间上限。可以开始、补充、排队、打断、取消。完成、失败或取消时向该 Bot 的收件箱投 Event。
5. **编码代理后端**。已接上。同一份任务记录接到外部命令、`xbot runner` 或本机模型，内核不加新对象。`code_start` 会改文件，但按权限表属于普通动作，不弹确认卡。`XBOT_CODE_AGENT` 优先，否则用 PATH 里的 `cursor-agent`、`codex` 或 `claude`。输出按行流进 transcript 的编码卡片。`code_show` 能看已经流出来的输出，`code_cancel` 停掉进程。仓库非空且目录里还没有 `.git` 时，先 `git clone`。都没有后端时仍只写记录。
6. **手机后台推送**。已验证。每个 owner 一份 VAPID Web Push 订阅，写在数据根的 `push/subs/<owner>.json`（0600），密钥在 `push/vapid.json`（0600）。网页「连接应用」里开启或关掉。订阅地址是公网 https；环回上的 http 只留给本机测试。内网、环回、链路本地、运营商级 NAT 和未指定地址拒绝。主机名解析失败就拒绝，不放行。十进制或十六进制整数主机（例如 `2130706433`）拒绝。地址还要是全球单播，并排除 IANA 专用段，包括运营商级 NAT、基准测试 `198.18.0.0/15`、文档和保留地址，以及 NAT64、6to4 和 Teredo。真正拨号时再用套接字 Control 核对解析出来的地址，避免检查之后被重新绑定到内网。浏览器页面还连着（正在拉接口，或有一条没结束的对话）时，普通通知仍走页面里原来的提示，不发 Web Push。没有标签页连着，并且这条 note 已经写进出件箱时，才按订阅推到手机。免打扰时段不推：时段内 note 只进 held，不写出件箱；即便出件箱里已经有一条，只要这时还在免打扰，后台推送也停。时段结束后合成的那一条写进出件箱，记录上带批量标记，不靠正文开头那几个字来认。页面拉得到。只有页面正有一条没结束的请求时才不推送；仅仅是最近 12 秒有过接口调用时，这条批量通知仍推到手机，避免两边都没人收。页面里的通知列表和提示保持原样。
7. **人的记忆同步**（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）。已验证。人的身份追加在 `people/<id>/identity.jsonl`，人的记忆追加在 `people/<id>/memory/sync.jsonl`。每条记下时间、写下的 Bot、设备。设备用数据根里已有的 `node-id`，没有就记 `local`。`xbot node add` 登记的节点之间自动同步：`xbot serve` 和 `xbot node serve` 每隔 30 秒，以及本机写入或从别的节点合并进新内容之后，用该节点的令牌调用对方的 `sync` 能力，把两份日志换过来再合并。同一条以时间戳较新的为准；时间相同留本机已有的。晚于现在两分钟的时间戳合并时改到这个上限。不合格式的条目（层级、控制字符、长度）丢掉。同步来的账号留在日志里，不授予本机用户身份；用户账号、显示名和时区只由本机操作更换。`xbot person unlink [--force] <link:id>` 去掉一个已关联账号。最后一个本机 `terminal:uid:` 账号要加 `--force`，并先打出警告。记忆放进模型上下文时包在闭合的 memory 块里。不可信记忆的正文、键和设备名里的 `<`、`>` 以及全角和形近尖括号一律转义，块内不能再出现标签。从别的节点来的事实按不可信数据处理，并保留写下的人和设备。只有送达这条记录的已认证对端自己就是所声称的设备，并且该设备已在本机登记，才记成 owner-recorded，仍然是数据，不是指令。经别的节点转发的，即使设备名对得上，也仍是 untrusted。`xbot sync export` 和 `xbot sync merge` 仍可手工导出、合并同一份日志。Bot 目录不改，Bot 仍留在自己的家节点。这一跳走现有的节点调用，单次请求和响应各不超过 1 MiB；装不下时这次同步报错，不截断。
8. **远程隧道**。已验证 Cloudflare 这条路径：`cloudflared` 的地址从 stderr 读出，进程组跟着 `serve` 结束，启动它的 goroutine 锁住操作系统线程直到关闭。公网能不能访问到这个地址，这次没有测。
9. **多节点**（2026-10-18 已退役，见 [ADR 0012](adr/0012-retire-node.md)）。已验证。`xbot node serve` 用 bearer token 公布本机身份，`node add` 先核对 token 再写入。`shell` 不信任调用方传来的 `approved`。`ask` 把命令留在本机，十分钟过期，最多 32 条，`node pending` 列出，`node approve` 先显示命令再问 `y/N`。
10. **机器开关**。已验证。每台机器 `allow`、`ask` 或 `off`，统一在核心的 review 路径处理本机工具。`allow` 时 `shell` 只对删除和对外发送确认，普通写入直接落盘，本机读取不问；`ask` 每次本机动作都要确认；`off` 拒绝 `shell`、`read_file`、`write_file`、`edit_file`、桌面和浏览器动作；`browser_stop` 仍可用来关掉进程。写自己的 persona/profile 文件在 `allow` 下仍确认。
11. **桌面操作**。已验证。截图、点击、输入、滚动、按键。确认卡写明坐标、原文、按键，以及滚动的方向、步数和位置。截图权限 0600。
12. **浏览器**。已验证。CDP 驱动单独配置目录里的 Chrome。命令行拉起的进程留到 `browser stop`；`serve` 只停自己拉起的那一份，用进程组，不用 `Pdeathsig`。`session.json` 里的 pid 对不上就不杀，删掉文件，退出码 0。

用户技能加在已有的 `skills` Link 上，不单列一片，也不替换上面的连接器和编码代理。

## 本地优先桌面端（2026-10-17 定，代码已交付）

用户决定：XBot 装在用户自己的电脑上，Windows 和 Mac 同时做。`serve` 跟着桌面端在本机跑，所有 Bot 数据都在本机，窗口显示本机 `serve` 的现有 Web 界面。设计决定见 [ADR 0007](adr/0007-local-first-desktop-app.md)。

**完成的样子**：用户在 Windows 和 Mac 上各装一个，打开就能直接用本机的 Bot，连续用一周不想换回别的。

**已定的决定**：
- Windows 要求装 Git for Windows，shell 工具用 Git Bash（`bash -c` 语义，权限表规则不变）。
- cron 按电脑的本地时区解释，不再按 UTC；已有例程升级时迁移，触发时刻不变。
- 关窗口就退出（不做托盘和开机自启）。错过的例程不补跑：下次启动时每个 Bot 收到一条汇总通知，列出错过的时间点，由 Bot 自己决定。

**摸底结论（`d6a90a3`）**：`darwin/arm64`、`darwin/amd64` 的 `CGO_ENABLED=0 go build ./...` 通过，`go vet` 只挂在一处测试代码；`windows/amd64` 编译不过（`fsutil`、`kernel` 的 `flock`，`browser`、`tunnel` 的 `kill` 和进程组，`kernel/project.go` 的 `x/sys/unix`）。编过以后，Windows 上还有三处挡启动或挡核心功能：保险箱密钥的权限检查（Windows 上 0600 读回来是 0666，第二次启动所有 Bot 都起不来）、目录 fsync（对话分段封存失败）、shell 写死 `sh -c`。MyGo 外壳的最小窗口能从 Linux 交叉编译出 Windows 和 mac 版本（每个约 9 MB）；在盒子的 WebKitGTK 里指向本机 `serve`，界面、登录、重开免登录都正常。

**怎么做的**：每个 PR 约 500 行以内，测试三遍全绿、触及包跑 `-race`、每个修复带一条在旧代码上会失败的回归测试、没跑的列成 Not run。

### 三线交付内容
A 线（跨平台核心）：文件锁抽象（unix `flock`、Windows `LockFileEx`）、进程控制（Windows Job Object 杀进程树、mac `sysctl` 取启动时间）、Windows shell 走 Git Bash、Windows 存储语义（密钥权限、目录 fsync、原子写重试、只读分段、ID 大小写不敏感）、Windows 重启与升级（`.old` 改名替换）、浏览器按平台找 Chrome、编辑工具保 CRLF 与子进程 `HOME`/`USERPROFILE`。

B 线（桌面外壳）：`internal/appshell` 与 `xbot app`（同进程起 `serve`，固定 `127.0.0.1:8787`，单实例，一次性启动码自动登录，日志不含 token）、界面适配（标题栏留位与拖动、字体、原生通知、外链走系统浏览器）、打包（GitHub Actions 出 mac `.app`/dmg、Windows 安装器、Linux `.deb`，附 SHA256SUMS；`scripts/package-desktop.sh`、`build-macos-dmg.sh`、`test-windows-installer.ps1`）、应用内自更新（复用 `internal/upgrade`，mac 整体替换 `.app`，Windows 用 `.old`）。

C 线（定时任务）：cron 按本地时区解释并迁移已有例程（触发时刻不变）、错过时间点的汇总通知（水位线不进例程目录，停机或睡眠超 2 分钟发一条，ID 可去重，最多看 7 天）。

**当时的顺序**：A1 → C1 → C2 → B1 → B2 → B3，到这里 mac 能装；接着 A4 → A3 → A2 → A5，Windows 能用；最后 B4、A6、A7。A1 与 C 线并行。

**真机验过的**：还没逐项确认。这些只能在用户的真机上验：WebView2 和 WKWebView 的实际渲染、字体、标题栏、拖动和吸附、原生通知；Windows 上 Git Bash、Job Object、`LockFileEx` 的强制锁、目录 fsync、rename 共享冲突、只读文件删除；dmg 和 Windows 安装器、Gatekeeper 和 SmartScreen 放行；mac 整体替换 `.app` 和 Windows `.old` 的自更新；合盖一晚后的汇总通知。

**盒子和 CI 上验过的**：交叉编译和 `go vet`、Linux 窗口（WebKitGTK）、单元测试；GitHub Actions 上跑 Windows 和 macOS 的全量 `go test`（`ci.yml`）；`installer.yml` 在 PR 上构建 Windows 安装器并在 Windows runner 上装、升、卸，`release.yml` 打 tag 时跑同样的检查。桌面端截图的提交在 `desktop-shots` 系列里。

## 测试与陪练
Bot 复刻会在自己的电脑上长期运行 XBot，像真实用户一样使用它，发现问题后整理成复现步骤交给编码代理修复，修复后再做回归测试。

## C1：通用慢工具后台化（2026-10-23）

沿用 `tools.Jobs`，把 shell 的等待裁决扩到实际工具调用：等待片 2 秒或用户插话后只交还前台等待，运行不重试、不取消；完成统一经原 Inbox 回报。删除 shell 的 sleep/poll 例外、忙闲双通知与 claim 账。工具 Job 不占子代理 3+8，保留原输出、取消与回收能力；runner 同步接入，目录、图片、记忆写守卫和已发生副作用仍在实际完成时整理。

假模型六场景取消慢工具豁免后通过，10 个 B 全部受理，最终派出 p95 约 373 毫秒；新增回归在旧代码上因慢工具/记忆写锁阻塞失败。快调用含登记及结果整理实测均值 0.62 毫秒（300 次，本机文件系统），不是 IPC 或真实模型 p95。真实模型与用户日用未测。最终门禁、审阅修订及提交号见仓库外 `~/.xbot-build/env-baseline-c1/commit2.md`；现行规则见 [内核](atlas/kernel.md)「慢工具后台化」。

## 多 Provider 批 1（2026-10-23，本地分支）

实现提交 `54cfade`：消息正文改为有序 `ContentPart`，沿原 transcript、索引、blob、导出和投影链路读写。旧串在读取时转成块，已分段的旧行不重写；Completions 请求保留原有行为。跨块脱敏覆盖写入与 JSONL 导出，压缩保留未知块的可读摘要。现状见 [providers](atlas/providers.md) 与 [context](atlas/context.md)。

验证：gofmt 无输出，vet、全量测试、相关包三遍、build 通过；末轮修改的四包另三遍通过。最终 `codex review --base 860eaf3` 无明确可操作缺陷。六条假模型 smoke 首轮全过，312.511 秒；后一次定时唤醒的重启项超时，定向复跑通过（42.590 秒），根因未验证。另一次 Job 通知时序失败及其复跑结果一并保留在报告中，没有改断言或延长超时。

性能（Apple M1）：100 个 256 字节文本块，JSON 读取与 Completions 投影约 0.758 ms/op；4096 个小增量共 64 KiB 的流式组装约 2.377 ms/op。均为本地基准，不代表真实模型或 IPC p95。

级别：本地提交、假模型端到端通过；未合入 main，未 push，用户日用未验证。Not run: Linux CI、真实模型认证、浏览器视觉检查、全量 race。完整命令、失败记录、文件清单与提交号见 `~/.xbot-build/multi-provider/commit-b1.md`。本批未实现后续协议、hosted 或 UI。

## 多 Provider 批 2（2026-10-23，本地分支）

`feature/multi-provider-b2` 已实现 Responses、模型能力解析、按绑定互斥搜索，以及最小换绑定和 hosted 引用界面。现状见 [供应商](atlas/providers.md) 与 [界面](atlas/ui.md)。实现提交 `acb0af0`，换绑任务保护、流错误分类和引用修复 `0c4897d`；后续收尾提交见分支记录。未合入 main，未 push，未发版。

假模型端到端通过：Completions 六条基础旅程与既有 smoke 共 362.116 秒；Responses 专项覆盖原生请求、内容块、引用、Jobs 边界和换回 Completions，修复后再次通过。Chrome 已走换绑、搜索进行中/完成、引用列表、能力表和 OAuth 禁用态。没有真模型或真实 GPT 账号录制验收，没有用户日用验收。

最终全量检查未通过：状态接口 p95 7.170792 ms，超过 5 ms 门槛，其余包通过。web 包独立三轮通过；状态接口 p95 为 1.050625 / 2.885416 / 1.567959 ms（每轮 500 次）。忙时替身链路 p95 382.269 ms，批量消息 p95 371.717 ms。期间还出现过插话重复回复、临时目录清理及 Chrome watchdog 失败，根因未确认；失败记录与最终命令结果保留在仓外 `~/.xbot-build/multi-provider/commit-b2.md` 和 `b2-evidence/`。未测真实模型延迟、IPC 额外开销、后台变慢比例或 FPS。

## 设置·设备与配对嵌层（2026-10-23）

设备页只留标题行的「+ 配对设备」，删除底部双按钮。配对同屏展示本机码和对方码输入，点码复制、图标换码，下一步空输入禁用；继续使用现有 code/join API。远端对话深链进入设置·设备。

本地浏览器行为测试及 web 包三遍、全量 `go test -count=1 ./...`、`go build ./...`、`go vet ./...` 通过，`gofmt -l .` 无输出。浅色、深色三种状态已查看；网关响应和剪贴板使用替身，双机实配、性能数字和用户日用未测。级别：本地分支验收，未合入、未发布。证据在仓库外 `~/.xbot-build/devices-pair-ev/commit.md`。

### 网页出站去掉地址白名单（2026-10-25）

mihomo、Clash 的 fake-ip 把 `api.deepseek.com` 解析成 `198.18.x`，旧出站检查因「非公网」拒绝拨号，网页搜索「试一下」报「密钥无效或连不上」。现在只拒绝回环、未指定、链路本地（含云元数据）和组播，其余地址都拨；`trusted_egress_cidrs` 删除，旧字段读入忽略。取舍见 [ADR 0022](adr/0022-web-egress-floor.md)。假 IP 解析下 `web_fetch` 和 DeepSeek 搜索探测的回归在旧代码上失败（证据在仓库外 `~/.xbot-build/egress-guard/old-code-regression.txt`）。`internal/tools`、`internal/web`、`internal/config`、`internal/kernel` 通过。级别：本地分支验收，未合入、未发布。

### 设置·技能（2026-10-25）

设置浮层新增「技能」（网页搜索与设备之间）：一层是用户技能库清单，带搜索和唯一主按钮「导入」；二层按 Bot 开关，关只影响该 Bot。导入由浏览器选文件夹，同名先问再覆盖；新增库级 `GET /api/skills`、`GET /api/skills/{slug}`、`POST /api/skills/import`，开关复用每个 Bot 的 enable/disable。现状见 [界面](atlas/ui.md#技能设置)。

其后合入四源导入（Agent / Codex / Claude / 本机文件夹）；设计师用户视角 PASS（tip `a02e3f3`，祖先 `3803f3d`）；`docs/dev/skills-import-sources.md` 已毕业删除。未 push。

本地 Chrome 走查了列表、搜索、无匹配、空态、二层开关、导入、同名覆盖/取消、错误和深浅色、窄屏；`go test` 覆盖库级接口、导入覆盖与并发同名。级别：本地分支验收，未合入、未发布，用户日用未测。证据在仓库外 `~/.xbot-build/skills-settings/`。

### 输入框 URL 高亮可点（2026-10-26）

对话输入框从 textarea 改为纯文本 contenteditable：草稿里的 `http(s)://` 地址就地高亮，点击走气泡链接同款 `target=_blank` + `noopener`（桌面壳复用离域导航 → OpenExternal），点击不触发发送；Enter 发送、Shift+Enter 换行、IME 选词回车不发送均保持。只认可解析为 http/https 的地址，尾随标点与紧贴中文不吞进链接，纯文本上限 16000 照旧，粘贴一律转纯文本，图片粘贴/拖拽流程不变。规格 [docs/dev/composer-url-links.md](dev/composer-url-links.md)，现状已提炼进 [界面](atlas/ui.md)。

验证：node 纯函数单测（识别/协议白名单/尾标点/序列化/偏移）+ 点击不提交 form 的 node 单测 + headless Chrome 端到端（高亮、点击打开不发送、非 http(s) 不亮、Enter/Shift+Enter/IME、16000 上限）全过；`go test -count=1 ./...` 全过，web 包 `-count=3` 过，gofmt/vet 净。浅色/深色截图已在本地 Chrome 查看。级别：本地分支端到端通过，未合入 main、未 push，用户日用未验证。

### 草稿 Markdown 高亮 + 气泡数学（2026-10-26，同一候选第二批）

在 composer URL 高亮分支上续作：输入框在 URL 高亮之上加 Markdown 语法上色（行首 `#`/`##`/`###`、列表 `- `/`* `、`` `code` ``、`**粗**`、`*斜*`、`[text](url)`，全部保留源字符、非 WYSIWYG，`[text](url)` 沿用裸 URL 的 http(s) 白名单与 `_blank`+`noopener` 点击路径）；消息气泡（流式与 transcript 同路径）用内置 KaTeX 0.16.11 渲染 `$…$` 与 `$$…$$`，TeX 源从不作为 HTML 进 DOM，库缺失或渲染失败回退原文；金额 `$5 和 $6`、代码段内的 `$` 不误判；sw SHELL 加 KaTeX CSS/JS（缓存 v15），字体仅 woff2 运行时缓存。规格更新见 [docs/dev/composer-url-links.md](dev/composer-url-links.md)，现状已提炼进 [界面](atlas/ui.md)。

验证：新增 node 单测（草稿高亮 7 组断言、数学 13 组断言含回退）与 headless Chrome 端到端（草稿高亮/点链接不发送/Enter 发送源码、行内+块级公式/KaTeX CSS 接线/回退/未闭合流式/持久化重渲染）全过；既有 composer links 三测试不回退照过；`go test -count=1 ./...` 46 包全过，gofmt/vet 净。浅色/深色截图本地 Chrome 查看：公式排版、草稿各色、金额不误判均确认。KaTeX 体积 gz 约 76 KB JS + 3.5 KB CSS + 296 KB 字体（woff2 运行时缓存）。级别：本地分支端到端通过，未合入 main、未 push，用户日用未验证。

### composer/气泡数学复审一修（2026-10-26，同一候选第三批）

复审 1（仓外 `~/.xbot-build/composer-url/review1.md`）五个问题全修，范围不出该报告：

- **上限拒绝不截尾（R1 P1）**：16000 上限从 renderComposer 事后 `slice` 改回旧 textarea 的 maxlength 语义——超限的用户编辑（中间插字、选区替换、粘贴、拖入、整段替换）在编辑入口整体拒绝并恢复光标，未被选中的已有草稿一字不丢；程序化写入不再截断。
- **撤销/重做（R2 P2）**：每次 input 全量 replaceChildren 弄死原生撤销，改为页面自维护 composerHistory（每接受一步编辑一步，上限 200；⌘/Ctrl+Z、⌘/Ctrl+Shift+Z、Ctrl+Y，菜单撤销/重做经 beforeinput 拦截）；发送清空、草稿恢复、失败回填等程序化写入重置基线，同旧 textarea 赋值。
- **块级数学配对（R3 P2）**：开块行与闭合行都改按首个闭合 `$$` 切分并继续处理尾部：`$$a$$ 和 $$b$$` 两段公式加中间正文，多行闭合行尾再跟公式照切。
- **真实解析失败回退（R4 P2）**：KaTeX 改以 `throwOnError:true` 调用，非法 TeX 抛错进 catch 回退原文，不再以 `.katex-error` 蒙混；node 测试加载仓内真实 KaTeX 回归。
- **回退保留源定界符（R5 P2）**：mathNode 的源串与 displayMode 分开传，句中 `$$…$$` 缺库/抛错回退仍是 `$$…$$`，不改写成 `$…$`。

验证：新增 `TestComposerNativeEditBrowser`（headless Chrome，真实 `execCommand` 输入/删除 + ⌘/Ctrl+Z/⇧Z 撤销重做 + 上限处原生 insertText 断言）、`TestBubbleMathRendering` 加真实 KaTeX/非法 TeX 与配对/回退断言、`TestBubbleMathBrowser` 加真实解析失败与句中 `$$` 回退段；四个测试在旧 HEAD（6595c80）上逐一失败确认。`go test ./internal/web/ -count=1` 与 `-count=3` 过，gofmt/vet 净，`go build ./...` 过。级别：本地分支端到端通过，未合入 main、未 push，用户日用未验证。复审报告与证据在仓外 `~/.xbot-build/composer-url/`。

### 推翻前台后台 · 删限制（2026-10-26，分支 `feature/drop-foreground-bg`，未推）

按 [ADR 0028](adr/0028-no-foreground-background-split.md) 整块删掉前台闸：`foreground.go` 全文（`foregroundHidden`/`foregroundKept`/`workClass`、`foregroundTools`/`foregroundRefuses`/`errForeground`、`foregroundCap`/`cappedTools`/`capTools`/`capNote`/`capGrace`/`<foreground_limit>`）、`StopForegroundLimit`、turn 的 `capped`/`capSteps`/`capBaseTools`/`capFrom`/`capFork`、`freshAllowanceLocked`；`AllowFork` 下「超前台上限自动转后台」（`foregroundCapped`/`capNotice`）一并删，插话分叉保留。`ForegroundMaxTools`/`ForegroundMaxTime` 从 Kernel、role 默认、`Config`、`ProfileUpdate`、`profile set` 参数里删除。`DispatchRule` 改写为「先回人再干活；短活自己干，长活 dispatch」，旧措辞留 `legacyDispatchRule` 只供迁移识别。

验证：新测 `TestMainLineDoesTheWork`、`TestMainLineAndSubagentsReachTheMountedTools`、`TestLongTurnStaysOnMainLine` 在旧代码（71f1ac9）上均失败；`go test -count=1 ./...` 46 包全过，kernel/bot/cmd/xbot/eval `-count=3` 过，gofmt/vet 净。级别：已合入分支（未合 main、未推）。Not run：真模型 `evals/dispatch/*`（新规则下模型可能自己做短活，期望值需用户用真 DeepSeek 再认证）；浏览器手测。

### 思考回传与启用（2026-10-26，本地分支）

`feature/thinking-enable` 续接原未提交实现：Completions 保存并多轮回传 reasoning_content，DeepSeek 默认启用；显式 on 真启用 Anthropic thinking 预算与 Responses 原生 reasoning。默认思考档 low，配置和 CLI 支持 low/high/max，runner 保留档位。思考只在内部 transcript 保存、脱敏和回放，SSE/公开历史/气泡均不展示。现状见 [供应商](atlas/providers.md)，后续验收见 [开发设计](dev/thinking-enable.md)。

最后一轮 `go test -count=1 ./...`、vet、build、gofmt 与 diff 检查通过；六条基础旅程加三协议思考专项 smoke 通过（283.327 秒），本机 Chrome、真实 CLI 档位校验和 provider 思考专项 race 通过。相关七包三遍已跑，web 插话重复回答与 kernel shell 通知时序存在失败，两者均在隔离的旧提交 9046089 上复现；首轮全量另有长程回忆失败，末轮通过，旧代码长程三遍通过，原因未完整闭环，未修改验收断言。atlas audit 仍报其它文档的既有告警。完整失败记录和复测见 `~/.xbot-build/thinking-enable/encode-report.md`。

性能：三种协议六次真实进程回复含两次读文件工具，耗时 47.4–115.6 毫秒（scripted 上游）；ContentReplay 基准 0.156 毫秒/op。不是实际模型延迟或 IPC p95。级别：本地分支提交、假模型专项端到端通过；未合入 main、未 push，未重启 8788；真实账号、Linux CI、用户日用、全量 race、FPS 与后台减速未验证。

### 思考回传 fix1：公开导出隔离（2026-10-26，本地分支）

修复 review1 P2-1：`ExportTo/visibleMessage` 共用公开投影，JSONL、Markdown 及 CLI 导出统一移除 reasoning 块、清空其余块的 VendorRaw；保留正文、公开步骤/引用和原工具开关，内部段文件与重启后同协议回放完整。现状见 [上下文](atlas/context.md#怎么运转)。

三协议 × tools=0/1 × 两种格式的 HTTP 回归在旧实现上失败，修复后通过；Export/Thinking/Reasoning 相关五包专项三遍、bot/web 整包三遍、导出专项 race、三协议“导出后重启再续轮”冒烟及 vet 通过。全量仅既有 `TestInterjectionGoesStraightIntoTheTurn` 因重复回答失败，未修改该测试或调度逻辑；gofmt/diff 检查净，atlas audit 仍报既有文档告警。专项六次请求含读文件及四次导出检查累计 34.7–59.0 毫秒（scripted 上游，小样本，非真实模型或 IPC p95）。级别：本地分支修复、假模型专项端到端通过，未合 main、未 push、未重启 8788；报告见 `~/.xbot-build/thinking-enable/fix1-report.md`。

### Bot 自装技能通路（2026-10-26，本地分支）

`feature/bot-self-skill` 按 [开发设计](dev/bot-self-skill.md) 为内置 dev 类型加入 `read_skill` / `write_skill`；catalog、grant 和工具描述统一指向本 Bot 用户的本机技能库，删除 shared 和写盘前强制批准的误导。主线继续沿用 ADR 0028 与 skills lazy load，写入仍走 `review.Normal` 和原 `skill.Write`，未改权限、校验或落盘逻辑。现状同步到 [生态](atlas/ecosystem.md) 和 [子代理](atlas/subagents.md)。

验证：新增 scripted 内核回归同时启用 Dispatch、agents grant 与权限检查，断言 load 前不可写、load 后主线包含读写工具，并实际写入测试 Bot Root 下的 owner 库再读回。文案、dev 白名单回归在 f22b708 旧源码覆盖下失败，主线可达回归在该基线已通过（此前 ADR 0028 已打通）。指定五包 `go test ./internal/kernel/ ./internal/link/ ./internal/agenttype/ ./internal/skill/ ./internal/review/`、`go test -count=1 ./...`、`go vet ./...`、gofmt 与 diff 检查通过；atlas audit 已跑，仓内其它文档告警未在本批扩改。证据：`~/.xbot-build/bot-self-skill/`。

验证耗时：指定五包中的 kernel 为 34.261 秒，全量中的 kernel 为 48.838 秒；这是测试运行时间，不是产品性能数据。本批未测交互/IPC p95 或后台减速。级别：本地功能分支提交待复审；未合入 main、未 push，Bot 自装技能的真实入口端到端与用户日用待验。Not run: `-count=3`（按任务要求）、真模型/真实对话安装、Linux CI、性能专项。

### 生图体验热修、头像即时刷新与开发 tip（2026-10-26，本地分支）

`feature/imggen-ux-hotfix` 补齐生图账号与 Bot 模型绑定的人话说明；助手流式和历史统一剥掉图片语法与残片，保留代码、公式和普通链接；xAI 未登录主钮统一「登录」。`update_profile` 回执改为 `profile saved`，保存结果主动刷新侧栏与顶栏，头像字段进入侧栏缓存签名。开发脚本成功构建时同步短 HEAD 到 build-info 和 xbot.tip。现状见 [界面](atlas/ui.md)、[多 Bot](atlas/multibot.md)、[版本](atlas/release.md)。

开关同步在基线 c91e7dd 已通过本机 Chrome：本批补 class、aria、section.hidden、实际滑块/背景、初始开启、保存失败和重开断言，未修改已同步的 paint/CSS，也不宣称复现了现场失同步。原始 box 证据不可读，全局首个 `.tog` 是隐藏的手机推送开关，原采样是否选错仍待执行官复核。

验证：全量 `go test -count=1 ./...`、`go vet ./...` 通过；config/web/kernel/link 相关单遍通过，修改到的 web/kernel/link 整包三遍通过。六条基础旅程 smoke 通过（280.203 秒）；本机 Chrome、脚本隔离构建回归、gofmt、diff 检查通过。绑家提示、气泡、xAI、头像与 tip 的回归均已验证旧代码失败。atlas audit 与基线对照无新增告警，已有告警保留。证据在 `~/.xbot-build/imggen-ux-hotfix/`，浅深色截图在 `~/.xbot-build/ui-shots/imggen-ux-hotfix*/`。

性能：17,280 字节图片语法过滤，Node v24.22.1 预热后 200 次 p95 0.137 ms；fake 页面工具结果到头像更新为虚拟时钟 10 ms，不能当作真实响应 p95。级别：本地分支自动化与 scripted 基础旅程通过，未合 main、未 push、未重启 8788；真实账号生图、运行 tip 一致性、设计师本批截图验收、用户日用、真实模型/IPC p95、FPS 与后台减速未验证。后续边界（工作区可发现、交付话术、关于页 tip 展示等）见候选清单，不另留开发短记。

### 技能主名与落库默认范围（2026-10-26）

`feature/skills-main-default` 落地：新技能的 `name` 保持 slug，`title` 为可选人读短名；解析、库/来源接口与列表/详情优先 title，旧 name/slug 降级，description 不再作主标题。`write_skill` 支持 title 与显式 scope：self/同 owner 本机单 owner Bot 名（忽略大小写）只开目标，省略/空全开；不扫描 transcript，设置页新导入继续全开，覆盖保留既有开关。

范围默认复用 `EnableSkill` / `DisableSkill` 与 `disabled_skills`；同库自装串行，配置更新在原文件锁内读最新值并原子写入，其它配置保存不覆盖既有开关。已加载内核也读取持久化名单，安装后手动开关立即影响技能列举和读取。现状见生态、界面、多 Bot atlas；设计师用户视角 PASS（tip `a02e3f3`）；`docs/dev/skills-title-and-scope-defaults.md` 已毕业删除。

验证：skill/kernel/web/bot/link/review/cmd/xbot 七包单遍通过，最终 `go test -count=1 ./...` 46 个有测试包全过，`go vet ./...`、gofmt、diff 检查通过；kernel/bot 的范围并发、不同 slug 并发与旧配置保存专项 race 通过。新增/更新 title YAML 读写、旧文件兼容、API、DOM、scripted 工具调用、self/具名/省略/空 scope、非法与跨 owner scope、已加载内核手动开关、并发写入回归；c580239 隔离归档上回归按预期失败（只添加新字段/参数的编译适配，不改变旧行为）。本机 Chrome 真设置入口行为通过，浅深色列表/详情前后截图已保存。atlas audit 已跑，与同一 git 历史下基线相比无新增文档告警，既有告警保留。

性能：本机持久化开关读取基准 10,314 ns/op（约 10.3 微秒/次，200 ms 基准窗口）；不是交互/IPC p95 或 FPS。证据与完整自检：`~/.xbot-build/skills-main-default/`；截图：`~/.xbot-build/ui-shots/skills-main-default/`。

级别：已合本地 main（tip `a02e3f3` / 本切片祖先 `b4fd276`←`18b6989`）；设计师用户视角 PASS；未 push。Not run: 用户日用、Linux CI、交互/IPC p95、FPS 和后台减速。

### 聊天图片单图查看（2026-10-27）

`feature/chat-image-viewer` 把气泡点图后的多图墙替换成单图沉浸层，点击索引直达、同条前后切换、缩放拖动、关闭与焦点／滚动恢复共用现有图片列表。现状见 [界面](atlas/ui.md#聊天图片查看)。docs/dev 交接已删；设计师逛与用户日用仍待验。

本机 Chrome 真鼠标、键盘、滚轮、触屏滑动、捏合、双击、取消手势和减少动态效果通过；真实隔离 `xbot serve` 经登录、paste 上传、发消息、历史、图片查看完整入口通过，模型为 scripted，匿名 paste 请求仍为 401。新浏览器回归三遍通过，在原始 `ce4ebc9` 归档上因仍为多图墙而失败。前后浅深色截图及完整命令证据在 `~/.xbot-build/chat-image-viewer/` 和 `~/.xbot-build/ui-shots/chat-image-viewer/`。

检查未全绿：全量检查遇到原有审批测试 `shell` / `sh` 断言失败并阻塞清理，`internal/work` 原有 shell 测试缺 description；两项在原始提交复现。跳过该阻塞审批用例后，web 整包三遍仅四条已有 parked approval 用例失败，基线同样失败。六条基础旅程中五条通过，WebChat 非空 delta 数断言在本分支与基线同样失败。全量的 90 秒诊断上限曾截断长程测试，随后按默认超时单跑 `go test -count=1 ./e2e` 全过（68.436 秒）。`go vet`、gofmt、JS 语法、diff 检查通过；atlas audit 无新增告警。详尽结果与基线对照见编码报告；没有改这些范围外的测试期望。

性能：1440×900、DPR 2、本机 Chrome、缓存 scripted 图片各 30 次，从 DOM 点击到图片加载并经历两帧的 p95 为浅色 34.5 ms、深色 34.4 ms；不代表网络延迟或真实屏幕 FPS。级别：已合本地 main（merge `4a4ad70` / 功能 `1d8e24e`）；Pi civ-rev PASS（P0/P1=0）；未 push；等设计师逛后再 →已完成。Not run: 真模型、Linux CI、Safari／真实手机、真实网络交互与 IPC p95、真实屏幕 FPS、后台减速专项。

交付前已 rebase 到含编码克制约束的 `28501a6`（相对原基线只新增 AGENTS.md 约束），未修改该指令文件。rebase 后图片回归三遍及 trusted 检查再次通过：鼠标双击 scale=2.5、触屏捏合 scale=3、触屏双击 scale=2.5；均为浏览器真实输入后的 transform 实测。

## 2026-10-27 聊天仅工具产物可见（feature/tool-mediated-chat）

`notify_owner` 改名 `send_message_to_user`（扁平 text/images，每个 Bot 都挂，描述要求简洁人话）；用户正聊（web/terminal）只进气泡，不写 outbox/桌面，非正聊照旧通知。SSE 不再发 `delta`，送达的 `result` 带 `text`，`done.text` 为送达消息。网页聊天、侧栏预览、引用摘要只画该工具产物；主列不画工具过程行和自发往来系统行；旧轮（2026-10-27 11:00 +08:00 前）无该工具时仍按正文画。LanguageRule 改为短答与结果都经该工具；SilenceRule 旧措辞保留识别。新增只读工具 `get_group_message_history`（成员可读、limit 20/50、keyword/sender/before_id、正文不可信且一行加引号）。验证见 dev/tool-mediated-chat.md「怎样算对」；原有失败未改：`TestWebApprovalAllowAndDeny` 挂起、4 条 parked approval、`internal/work` TestShellUsesBotDir，基线同样失败。级别：已提交本分支，未合 main、未 push。Not run: 真模型、Linux CI、本机浏览器人工查看、性能数字。
## 2026-10-27 更换模型行横铺（功能分支）

- 单点修复：模型 id 在左、能力句靠右、选中标记贴右缘；本机 Chrome 用 scripted 名单检查 390 / 820 / 1440 像素布局和既有换绑交互，旧布局在新增几何回归中失败。尚未合入或经过用户日用验收。
- CSS 资源由 17126 增至 17289 字节（+163 字节）；无新增请求、轮询或动画。未测帧率、交互及 IPC p95。
- `gofmt -l .`、`go vet ./...` 和布局浏览器回归 `-count=3` 通过。全量测试中 E2E `TestSpawnWhileChatKeepsAnswering`、kernel `TestLongTurnStaysUnderTheCeiling`、work `TestShellUsesBotDir` 失败；Web 全包两次运行均卡在测试服务器关闭，手动中止，故全量及 Web 全包重复检查未通过。

- 2026-10-27：本机可选账号分支实现本机免账号进入、设置账号与设备双态；脚本替身检查及实际验收见 `docs/dev/device-optional-account.md`，未合入 main。
- 2026-10-27：统一中文称呼「主人→用户」（feature/wording-owner-to-user）；产品文案/提示/文档口径；docs/guard 术语软待政东确认。复审 PASS，已合本地 main。
- 2026-10-27：资料目录的契约和根层索引通过环境流发布，保留旧记忆过渡；后续已接入关键词提醒。20 篇小文件的资料索引微基准平均 0.366 ms/次（本机，非 p95）；验证及基线软点见本地提交说明。尚未做真实模型或用户日用验收。

## 2026-10-27 对话身份卡与面板遮挡回修

按对话区宽度让信息面板避开悬浮身份卡，恢复整卡点击开合；开态阴影沿用浮层 token。浏览器 scripted 回归覆盖 959、960、1100、1200 等宽度、展开侧栏、长简介和桌面壳；旧样式在 960、1100、1200 复现遮挡。当前为功能分支验证，未合 main、未经过用户日用。目标浏览器测试三遍通过；web 整包三遍检查在 `TestWebApprovalAllowAndDeny` 等待连接关闭时 90 秒超时，全仓测试长时间未结束而中止。未测交互 p95 或帧率；没有新增动画、轮询或请求。
- 2026-10-27：连接应用设置内密钥弹层及安装前探测在特性分支实现（ADR 0031）；废掉商店关设置、代发聊天的路径。脚本替身覆盖弹层、探测失败不落盘、并发重复添加；真实上游手探 Playwright 导航 1.7 秒通过，Context7 坏 key、飞书坏 token 均被拒；正 token 的飞书与 Telegram 未实测。进度见 `docs/dev/connectors-secret-modal.md`。

## 2026-10-27 改名与 tool job 持久化（feature/rename-tool-jobs）

Bot 改名后所有工具回执 `tool job storage unavailable`（裸文本进 transcript，气泡无产物）的三处修复：`tools.Jobs.Load` 写 lost 失败时把 output 收束到当前 `Dir/id.log` 重试一次，仍失败丢弃该条，不再 `return err` 让 `shellJobs()` 永久 nil；`bot.Rename` 搬目录后原子改写 `shell-jobs/*.json` 里指向旧目录的 output；内核 `shellJobs()` 检测目录漂移后从新目录重建任务表，`bot_update_profile` 同一轮里再调工具（含 `send_message_to_user`）不再往旧目录写。回归测试 5 条，回退修复验证均先红：TestLoadRepairsRenamedOutputPath、TestLoadSkipsUnwritableRecord（root 跳过）、TestRenameMigratesShellJobs、TestRenameKeepsToolJobsWorking、TestShellJobsFollowRename。三个改动包 `-count=3` 全绿。原有失败未改（基线同样失败）：TestWebApprovalAllowAndDeny 挂起、4 条 parked approval、internal/work TestShellUsesBotDir。级别：已提交本分支，未合 main、未 push。Not run: Linux CI、真模型、性能数字。

复审回修（review1 FAIL 的 2 项 P0 + P1）：`readMeta` 要求正文 id 与文件名一致，Load 收束改用文件名 id，堵住 `id:"../escaped"` 写出目录外；内核不再从新目录重建任务表，改为 `Jobs.Move` 原地换目录并改写活任务的 output，`bot_update_profile` 改名后立刻搬表，改名工具自己的完成写回落到新目录、不再重建旧 Bot 目录；并发首次查表只会拿到同一张表。新增回归 3 条，回退修复验证均先红：TestLoadIgnoresRecordWithForeignID、TestProfileRenameDoesNotRecreateOldDir（scripted provider 真实调用 bot_update_profile）、TestShellJobsKeepOneTableAfterRename。级别：已提交本分支，未合 main、未 push。Not run: Linux CI、真模型、性能数字。
