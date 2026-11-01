# 聊天页面顶部标签改版

## 授权与级别

L 档。2026-11-01 用户认可 docs/prototypes/chat-tabs 原型，授权派 Agent 实现，并明确这是整体 UI 变动，旧布局测试可大范围拆除。负责人独立验收。此设计直接由已确认原型落仓。

## 目标与怎样算对

用户从左侧选择 Bot，通过顶部标签查看聊天、记忆、轨迹、任务、上下文、收件箱。
聊天固定第一项，更多控制其他标签显示，记忆与轨迹获得宽阔阅读空间。
身份与操作一层，页面标签一层，遵循原型间距、灰阶和留白。
切换回聊天保留原消息 DOM、输入草稿、附件、引用、当前历史窗口和上翻位置。
其他视图中 SSE 继续，返回可见；切 Bot 正确隔离，旧 Bot 迟到响应不得串页。
历史搜索、分页、语音、图片、忙时插话、群组与往来仍可用，无假数据进入正式页面。

记忆复用原文件全文与经历分页；任务保留例程、Webhook、选择复制、编辑草稿；轨迹保留思考和工具展开；上下文、收件箱复用现有接口。Bot 资料和编辑模型在顶栏更多操作或资料入口保持可达。右侧抽屉由全宽主视图替代，不叠第二套聊天或轮询。

## 对照回顾

| 已有约束 | 本次处理 |
| --- | --- |
| 用户认可的原型 | 作为视觉验收依据；演示文案、假在线/假模型/假工作区不进入正式 UI |
| atlas/ui.md、API.md | 复用已有投影与 API，改入口与布局，不改内核及存储 |
| ADR 0021、0039 | 只展示原数据，不建第二套状态账，不把工具轨迹混入对话 |
| guard/界面.md | 延续 tokens、Lucide、黑白灰、无描边和浮层阴影；用户已授权改变布局，旧布局断言废止 |
| 六条基本旅程、长程运行 | 旅程语义不变，现有冒烟全部回归；入口改用顶部标签 |
| header-chip-panel、memory-panel-whole、bot-inbox-tab 等设计 | 抽屉入口由本设计替代；内容能力保留，资料入口移到顶栏 |
| 现有浏览器与静态测试 | 删除纯旧布局/静态字符串断言；保留或迁移业务行为与数据/接口断言，记录每项删改理由 |

## 设计审查补足

- 保留更新入口及 ready/manual 的安装重启或下载行为。
- 群组身份可进入群资料和成员；往来只读，关闭返回原聊天位置，不误露发送框。
- 忙态打开任务、上下文条统一切主视图，目标被隐藏时恢复该标签并定位。
- 补 macOS 与 in-app.os-win/os-linux 中宽和窄屏壳验收，避让系统按钮和拖动区。
- 修复了验收正文的终端写入损坏，以上述明确行为为准。

## 实现边界

复用现有聊天节点与 panel-body 渲染，统一一个活跃视图状态；样式值放 tokens.css。标签显隐仅为 UI 偏好，聊天固定，隐藏当前标签回聊天，更多菜单焦点/Escape/窄屏可达。切 Bot 默认聊天，标签显隐偏好可持久；保留全局设置与资料编辑功能。

先确认最窄路径：聊天与记忆/轨迹切换且草稿/滚动/SSE 保持，再覆盖其余标签、资料、群组、窄屏、错误态。页面不因标签切换重载或重复发送请求/消息。

## 验证与职责

实现 Agent 改正式静态文件；测试迁移 Agent 修改绑定旧入口的既有测试；负责人编写独立行为冒烟并复审实现及删测清单。截图 1440×900 DPR 2 浅深色，覆盖聊天/记忆/轨迹/更多与窄屏，保留原型对照。真入口使用脚本模型替身，不调用真实模型。

检查：gofmt、go vet、全量 go test、修改包 count=3、guard 冒烟、atlas audit，记录性能数据与未跑项。严禁用跳过、宽松断言或只改通过标记掩盖回归。完成后同步 atlas、ADR 与里程碑，主干本地提交，不推送或发版。

**冒烟用例**：负责人新增 internal/web/chat_tabs_browser_test.go；端到端使用 e2e/smoke 的真实 serve 框架。
**级别**：正式页面和独立真入口端到端已通过，独立复审无剩余 finding；未宣称用户日用或六条旧旅程门禁通过。

## 验证结果（2026-11-01）

- 全仓 `go test -count=1 ./...` 通过，web 107.396s；web 整包 `-count=3` 通过，290.788s。首次全仓遇到既有 CLIEventQueue 在模型刚开始时就清目录的竞态，测试现用 channel 与 kernel gate 等实际写完再结束；单项30遍和 race3遍通过，业务断言保留。
- 独立行为覆盖：聊天第一项与默认页、重复点当前 Bot 返回聊天但保留节点/草稿/历史位置、不重复请求、跨页 SSE、不重复消息、旧 Bot 迟到响应隔离、方向键/更多/Escape、模型目录迟到重绘、各桌面壳点击命中、跨页搜索/复制回执、失败保留任务编辑、经历首屏变化与分页在途变化。旧 UI 上最初导航回归确实失败。
- 真二进制 `TestChatTabsJourney` 在最后版本连续3遍通过（34.729s），实际 serve/API/存储/脚本模型工具投递，不做页面 API route 替身。浅深色、1440/768/390、六页、轨迹展开、资料编辑与更多均留图。最后60次本机采样各轮标签切换 p95 为67–69ms；数字为脚本化本机环境，不是生产性能或持续帧率保证。
- 独立设计复核和三轮代码复审完成；7项初审问题及分页在途补充问题均修复，最后 No findings。保留完整语义断言，恢复了迁移时遗漏的侧栏 pointer 拖动 rail 验证。
- 六条旧 guard smoke 在本分支和未修改 main 上全部以相同旧 `done.text/delta` 契约失败；不改变 guard 步骤或为本次布局重定义回复语义。此为既有验收缺口，不能据此宣称整体旅程全绿。长程 `TestLongRunJourney` 通过（43.08s）。
- DPR2 同内容改前改后：聊天像素变化13.8%，记忆81.8%；对应顶部/侧栏/输入框和抽屉改主视图。明暗背景分别白色与 #18181b，选中面与原型 tokens 相同。390菜单等待侧栏收起后严格验证边界，避免将 resize 动画中间帧当成稳态。

日志、审查和逐项迁移记录：`~/.xbot-build/chat-tabs/`；真入口截图：`~/.xbot-build/ui-shots/chat-tabs/after/{light,dark}/`；同内容像素差与几何：`~/.xbot-build/ui-shots/chat-tabs/comparison/`。桌面截图为壳 CSS 模拟，点击测试注入原生环境标记；未执行 Windows/Linux 原生窗口、真模型、用户日用或生产性能专项。

## 还没做的

- 用户日用反馈，日用通过后再按仓库流程处理设计与 guard 的归档。

## 测试迁移记录

以下为迁移 Agent 原始盘点与处理记录；正式完成状态以上方主管验收为准。

范围：只修改既有 internal/web/*_test.go、testdata 和直接受入口影响的 e2e/smoke；不修改 static、独立 chat_tabs_browser_test.go、接口/内核语义或正式文档。

## 迁移前盘点与理由

| 测试 | 类型与处理 | 仍需守住 |
| --- | --- | --- |
| panel_drawer_ui_test.go / TestInfoPanelIsDrawer | 删除。只用源码匹配限定右侧滑出、scrim、宽度/transform、Escape 关闭抽屉、手机 sheet；ADR0042 已替代。往来与聊天返回行为另以浏览器实际入口验证，不能依赖 event 字符串。 | 往来真实入口另验证 |
| panel_browser_test.go / TestBotPanelBrowser | 混合大用例。删 closed/open 矩阵、44/88/96px 头高、居中 chip、消息 padding、抽屉尺寸/动画/scrim、旧 identity toggle；保留模型/资料和侧栏菜单业务，改真实资料入口。换绑器自身布局非此次废止对象，保留。 | live catalog/分组重试、能力标签、搜索、暂选/取消/确认换绑、资料、右键/长按目标隔离、置顶、复制、菜单关闭 |
| memory_panel_browser_test.go | 入口从抽屉记忆改顶部记忆，详情切换改聊天；保留全文/Markdown、文件入口、经历展开、分页失败重试、节点与焦点保持、空状态。全宽内容无溢出仍应验证。 | 记忆全文与经历全部业务 |
| inbox_browser_test.go | 删除旧详情起首的六项顺序与 panel-tabs 强制240px横滚；改顶栏收件箱入口。 | 空态、来源分类、正文展开 |
| live_bubble_split / todo / trace_thinking / imggen_discover / webhooks / routine_skills / routine_projection / task_fold | 混合业务用例，仅迁移 head-chip -> 顶部 role=tab。 | 气泡隔离、trace 思考/工具、任务折叠、例程编辑草稿/焦点/失败、Webhook CRUD/复制/重试/真实错误 |
| profile_name_browser_test.go | 改真实资料入口和保存后只读资料入口；不保留隐藏详情 tab。 | 名字/简介/职责 PATCH、persona-card 绑定及无多余字段 |
| testdata/transcript-window.cjs | 真实 HTTP/Chrome：轨迹与返回聊天改顶栏。 | 窗口/搜索/前后分页/窄屏/trace 读原文 |
| testdata/custom-provider.cjs | 去掉旧 panel-open 偏好依赖，换绑前从真实资料入口打开。 | 自定义供应商真实注册/探测/换绑/更新/删除 |
| context_bar_test.go | 摘取函数的行为测试可能依赖旧 paintPanel 直接调用；待新实现稳定后按公开导航装配验证，tokens 与 snapshot 语义不改。 | 最新快照数值、无数据隐藏、点击进入上下文 |
| update_ui_test.go | 混合静态布局与更新契约；删除旧 pill 标签/CSS/线程切片匹配，补真实按钮行为覆盖，保留版本归一化验证。 | ready/manual 可达，install POST / 手动下载 |
| empty_state_ui_test.go | 去掉旧卡片圆角/贴边与纯 CSS 字符串；starter 不发送的业务断言保留。 | starter 填输入且不自动发送 |
| account_profile_drawer_ui_test.go | 资料编辑独立 modal 的能力并未授权删除；样式若因正式改版移除，仅删外观匹配，焦点/字段/提交仍由浏览器守住。 | 资料编辑与恢复焦点 |
| e2e/smoke | 盘点：大多数是接口/脚本模型，不依赖旧布局，保持；transcript smoke 复用迁移后的 testdata。 | 所有既有旅程语义 |

## 验证与问题

基线：用户提供 baseline-web.log，go test ./internal/web 已全绿。迁移后结果和实现方问题在本节追加。

## 落地结果

- 删除 `panel_drawer_ui_test.go` 整份纯抽屉源码匹配用例。
- `panel_browser_test.go` 仅删旧 drawer slide/scrim/chip 居中、固定头高、旧 rail 外观等布局段；资料、换绑、搜索/重试/取消/确认、能力信息、右键/长按/复制/置顶/目标隔离全部保留。从真实 Bot 资料入口进入，返回聊天走顶栏。
- 10 份混合浏览器测试改为 `#workspace-tabs [role=tab]`；inbox 不再以旧详情首项顺序或旧 panel-tabs 强制尺寸判断功能；memory 改聊天/记忆切换刷新。
- `profile_name_browser_test.go` 从 More 的真实资料菜单进入，保存字段、人设卡与职责断言原样保留。
- `context_bar_test.go` 函数测试按新导航调用缝装配，快照数值/隐藏条件不变；新增实际上下文/忙态任务间接导航（隐藏目标自动恢复）。
- `groups_header_test.go` 保留原分组标题断言，新增群组资料成员、Bot 专属入口隔离和返回 Bot。
- `update_ui_test.go` 删除 pill HTML/CSS/线程源码切片断言；替换成真实按钮 ready 安装失败恢复、manual 下载、成功安装后重启轮询、无更新隐藏和聊天 DOM 保持。版本归一化执行真实函数，旧版本继续轮询、匹配新版触发 reload。
- `empty_state_ui_test.go` 删除卡片圆角/贴边/字体尺寸等 CSS 匹配，starter 仅填入且不发送的既有业务断言保留。
- `transcript_window_browser_test.go` 原历史窗口/锚/轮询回归保留，补往来只读 composer 隐藏与关闭后恢复。
- `testdata/transcript-window.cjs` 使用顶栏轨迹/聊天；`testdata/custom-provider.cjs` 不依赖 panel-open 偏好，换绑显式打开真实资料。
- `account_profile_drawer_ui_test.go` 未修改：这是保留的独立资料编辑 modal，当前外观未被实现拆除，业务焦点与字段验证没有理由删除。
- e2e/smoke 既有 Go 文件未改：除共享的 transcript-window.cjs 外，没有旧抽屉入口；接口/数据/内核断言保持。

### 本 Agent 实际删改文件

- `internal/web/context_bar_test.go`
- `internal/web/empty_state_ui_test.go`
- `internal/web/groups_header_test.go`
- `internal/web/imggen_discover_browser_test.go`
- `internal/web/inbox_browser_test.go`
- `internal/web/live_bubble_split_browser_test.go`
- `internal/web/memory_panel_browser_test.go`
- `internal/web/panel_browser_test.go`
- `internal/web/panel_drawer_ui_test.go`
- `internal/web/profile_name_browser_test.go`
- `internal/web/routine_projection_browser_test.go`
- `internal/web/routine_skills_browser_test.go`
- `internal/web/task_fold_browser_test.go`
- `internal/web/testdata/custom-provider.cjs`
- `internal/web/testdata/transcript-window.cjs`
- `internal/web/todo_browser_test.go`
- `internal/web/trace_thinking_browser_test.go`
- `internal/web/transcript_window_browser_test.go`
- `internal/web/update_ui_test.go`
- `internal/web/webhooks_browser_test.go`

### 业务覆盖边界

仍守住：模型真实名单与换绑、资料名字/简介/职责与人设卡、侧栏右键/长按/复制/置顶、消息和草稿投影、工具/思考隔离、任务折叠与选择复制、例程草稿/焦点/取消/失败重试、Webhook、两层记忆全文与经历分页、历史搜索/分页/锚、往来与群组。没有新增 skip、改接口期望、弱化业务目标或加入隐藏兼容入口。

### 跑过与未跑

- PASS：`migration-node-static-final.log`，16 项相关 Node/源码行为检查，Go 输出 0.728s；迁移 JS/CJS 27 段语法；修改的既有 Go 测试 gofmt；`git diff --check`。
- 阻断：`migration-focused-1.log`，Chrome 在页面加载前启动失败，MachPortRendezvous bootstrap_check_in Permission denied(1100)。
- 阻断：`migration-web-full.log`，`go test ./internal/web -count=1` 在首个 TestAccountClaimBindsThisMachine 的 httptest listen tcp6 [::1]:0 被沙箱拒绝，没有跑完，不能计为全包通过。
- 额外静态集合尝试 `migration-static.log` 也因调用现有 approval helper 启动 httptest 被拒；该尝试没有产出额外通过结论。
- 未完成执行：迁移后的 Chrome 业务回归、新增资料/群组/更新/间接导航、真 HTTP/Chrome 与 smoke、全仓 go vet/go test、count=3、生产/真实模型和日用验收。测试只能 fake/scripted，未调用真实模型。
- 性能：本次仅获得 Node/源码测试墙钟耗时 0.728s；无页面交互 p95/帧率/IPC 数字，不能据此声称性能达标。

### 需要实现方/负责人处理

1. 在允许 Chrome 与 loopback listener 的环境跑 `go test ./internal/web -count=1`，再按负责人全局验收跑 count=3 与 smoke。当前环境审批模式 never，不能由本 Agent 放开。
2. 若迁移后的行为失败，按真实页面/请求修实现，不把行为断言删掉；本次未获得可执行页面，因此没有已确认产品 bug 可以归因，也没有把环境失败当作布局变更消掉。
3. 本次只交付测试迁移，尚未达到浏览器端到端通过，更不代表整项 UI 已完成或日用通过。未修改正式代码、文档、独立验收文件；未提交、push 或发送外部消息。

## 宿主首轮反馈修正（tests-feedback-1）

继续只改既有测试/fixture/harness；三份主管独立验收 `internal/web/chat_tabs_browser_test.go`、`e2e/smoke/chat_tabs_test.go`、`internal/web/testdata/chat-tabs.cjs` 均未修改。

| 文件 | 已确认依赖与修正 | 保留的期望 |
| --- | --- | --- |
| bubble_split_browser_test.go | speaker 成为 Bot 行首节点；引用首位、气泡间距定位改为显式 User `.bubble-col` / Bot `.body`，legacy 同步 | 引用次数仍为1且在正文首位；段落/音频/图片隔离与顺序、间距和 optimistic payload 不变 |
| voice_cluster_browser_test.go | 不再以 row.firstElementChild 当正文，明确取 User/Bot 内容容器 | 两泡、三段、caption、声波10柱、140–170宽度/对齐/6px间距、元数据宽度全部不变 |
| chat_image_viewer_browser_test.go + testdata/chat-image-viewer.cjs | `.thread-inner` 新增横向 padding；border-box 不等于可用内容列。按 `clientWidth - paddingLeft - paddingRight` 量内容宽；与图片 `cqw` 的容器 content box 对齐。首轮数值172.328/549对应约517/3，差额是32px父容器内边距，而非图片比例改变 | 仍严格断言列宽1/3（容差2px）、比例1.6/高度<=320、contain、多图84px与换行；未放宽比例和数值 |
| settings_ui_test.go | applyTheme 新增 workspace-theme 图标；最小 Node harness 增加 replaceChildren 与 icon 参数 | system保留偏好且随系统切换、显式light/dark不随系统、桌面主题API断言原样 |
| groups_header_test.go | fixture 的 Bot 默认 pinned，行在 sidebar 的 pinned 区而非 roster；返回 Bot 定位覆盖真实整个 sidebar，并等待行可用 | 群成员全量、群组无Bot专属页面/编辑、返回Bot恢复页面全部原样 |
| panel_browser_test.go | 资料视图先同步画模型占位，loadPanel后才有真实字段；等待活动公开资料中的模型/供应商/职责到齐，再执行原断言。后续资料检查限定活动视图，不误选隐藏缓存页；失败输出stack | 模型值、供应商协议、职责、异步换绑/取消/确认、焦点和菜单业务原样，不以空值通过 |
| profile_name_browser_test.go | 重命名会重新 selectBot，按设计回聊天；保存后显式从真实 #head-chip 再进资料，检查活动视图 | PATCH字段、人设卡、职责顺序与内容原样 |
| update_ui_test.go | download 字段及 window.open 三参数与生产一致，无需改 fixture。原 manual等待仅匹配「下载」，会误匹配 ready 提示「已下载」并提前点击；改为等待已渲染 manual 操作再点击 | 仍要求打开准确安装包 URL、_blank/noopener、manual不POST install，ready失败恢复及成功后轮询不变 |

本轮验证：`migration-feedback-1-node.log` 三项（SettingsSystemThemeKeepsPreference、RestartPollNormalizesVersions、ContextBarUsesLatestSnapshot）通过，0.954s。浏览器由主管宿主接管，不重复启动受限 Chrome。

主管反馈的真实产品回归（TranscriptIncremental/VoiceChat未变Bot行重建、历史切页scroll、MemoryPanel空数据旧入口、VisibleTail分页锚）没有修改或放宽相关期望；留待实现方修复。


### 主管最终审计补充

- `review105_test.go` 保留桌面/网页 session/logout 的真实HTTP断言，删除附带的9段源码片段匹配；重复点当前Bot不重置、草稿/排队/失败发送由实际浏览器与原业务用例守护。
- `panel_browser_test.go` 已恢复 side-grip 的 pointerdown/move/up -> rail 行为验证，只移除旧抽屉/居中芯片的几何契约。资料等待完整模型副标；正式 renderKey 同时包含目录与上下文元数据，修复已通过独立迟到目录用例。
- `cli_event_test.go` 仅修队列测试生命周期：等Provider开始后同步kernel gate，避免后台还在耐久写入时TempDir被清理；保留队列提前返回和模型接收内容的断言。
