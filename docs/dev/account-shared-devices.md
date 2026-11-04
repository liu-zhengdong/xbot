# 账号共享设备与前端交互落地

Created: 2026-11-04

## 授权与级别

L 档。用户先要求设备与 Bot 解耦，再要求前端交互原型。用户于 2026-11-04 确认原型：“可以，派子 agent 去干吧”。本轮授权真实实现与相关验证。

原始要求：所有绑定到当前账号的设备，账号内所有 Bot 都可以操作。

UC 与设计来源：[长期设计](</Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/设计/账号共享设备与云电脑.md>)。确认原型：[index.html](</Users/liuzhengdong/.xbot-build/ui-shots/account-devices/prototype/index.html>)；交接与交互范围：[README](</Users/liuzhengdong/.xbot-build/ui-shots/account-devices/prototype/README.md>)。原型数据和自动模拟进度不进入产品。

## 怎样算对

### UC-DEVICE-01：任一 Bot 操作账号设备

参与者：用户、源 Bot、账号服务、目标设备。

触发条件：用户在原 Bot 对话中指定设备并发送任务。

前置条件：源安装与目标安装绑定同一账号。目标设备在线并具备请求能力。

主流程：用户选择设备；系统接受主人消息；源 Bot 在该设备执行；目标返回真实任务与结果；原对话交付结果。

成功后置条件：目标磁盘真实出现预期文件。原 Bot 对话和执行详情显示正确设备。

验收标准：两个源 Bot 和新建 Bot 均能调用目标。目标设备不需要代办 Bot。无需旧短码配对。设备离线或能力缺失时明确失败，不静默在本机执行。

### UC-DEVICE-02：同一 Bot 使用多台设备

设备选择仅作用当前 Bot 下一条消息，发送成功后恢复自动。Bot 草稿与待选设备各自保存。实际任务绑定执行设备，后续选择不改已有任务。文件引用保留设备来源，跨机取回使用有界传输，不自动同步记忆或文件树。

### UC-DEVICE-03：多个 Bot 共用设备

源 Bot、主线与子代理的执行上下文分别保存 cwd 与 Jobs。Shell 可并行。真实桌面按操作序列协调。状态和取消回到目标端原 Jobs 实例。research 保留既有只读限制。

### UC-DEVICE-04：管理与解除设备绑定

账号设备列表使用真实成员关系。详情展示实际系统、可用能力和任务。改名真实保存，失败保留输入。解绑取消不改变状态；确认后所有 Bot 失去该成员的执行能力，旧票/已连执行通道不能继续发起操作。设备文件与 Bot 历史保留。心跳不能恢复已撤销成员。

### UC-DEVICE-UI：按定稿原型使用界面

设置页使用账号设备集合，不建立逐 Bot 勾选矩阵。聊天输入旁提供“本次使用：自动”设备选择，支持搜索和离线禁用。执行进度在单行状态中显示，点击查看详情与停止。任务进度、结果与产物必须来自实际执行，不用定时器造进度。

添加设备引导用户在新电脑登录同一账号，并主动绑定。检查设备重新读取真实列表，不插入假设备。无设备、失败重试、长名称、窄屏目录和弹层焦点均可用。现有无账号本机使用与旧配对消息旅程保留。

## 结构决定

账号拥有设备池；Bot 保留对话主机；每次执行使用独立设备上下文。采用直接设备执行，不用目标 Bot 代办。复用当前账号、加密 pipe、Shell/文件/browser/desktop 和 Jobs 实现。

当前 pipe 已接 serve，但每请求与响应限 700 KiB，同连接串行、整包缓冲、非 SSE。首期执行采用短请求受理、任务查询、有界输出读取与停止。附件分块须服从较小体限。副作用请求发送前生成稳定调用引用；回执丢失后按引用查询，未知时不自动重发。

设备执行通道与用户控制台身份分开。源身份从真实运行入口产生，目标验证账号成员。生产继续 ADR 0019 自动执行，不加逐操作许可卡。绑定安装中所有本机 Bot 继承该账号；Owners 仍管理可信输入，不增加 Bot×设备授权矩阵。子代理与例程继承真实源 Bot 的账号。

目标服务为外来上下文持有 Jobs 实例，不创建代办 Bot 或第二个调度器。结果与状态仍走工具回执、原对话和 ADR 0021 环境流。

## 实现分工与合同

- 后端 Agent：全部本轮 Go 改动，覆盖账号成员、设备执行、pipe 身份、源 Bot 入口、消息目标与真实状态 API。先报告具体接口合同，再编码。
- 前端 Agent：仅本轮静态 JS/CSS/HTML 与相关浏览器前端用例。按确认原型复用现有组件、token、挂卸载与草稿，不复制原型整套壳。
- 主管：冻结合同、ADR、独立验收与代码复审、集成和文档。相互独立 worktree，禁止覆盖其他进行中的供应商改动。

前后端合同于 2026-11-04 冻结：

| 入口 | 合同 |
| --- | --- |
| `GET /api/account/devices` | `{devices:[{id,name,self,online,bots,os,capabilities,last_seen,running_count}]}` |
| `GET /api/account/devices/{id}` | `{device,tasks}` |
| `PATCH /api/account/devices/{id}` | 请求 `{name}`，返回 `{device}` |
| `DELETE /api/account/devices/{id}` | 返回 `{unbound:true}` |
| `GET /api/account/devices/{id}/tasks?bot=NAME` | `{tasks}` |
| `GET /api/account/devices/{id}/tasks/{ref}?offset=N&limit=N` | `{task,output,next_offset}`，输出默认 16 KiB、最多 32 KiB |
| `POST /api/account/devices/{id}/tasks/{ref}/stop` | `{task}` |
| `GET /api/account/devices/{id}/tasks/{ref}/artifacts/{artifact_id}` | 原始字节有界下载，较大产物使用 offset/limit 分块 |
| `GET /api/bots/{name}/device-tasks` | `{tasks}`，供原输入区单行状态使用 |
| 原主人 messages POST | 增加可选 `device_id`；成功受理后前端恢复自动 |

`Task` 字段为 `id,ref,device_id,device_name,bot,context_id,kind,description,status,started_at,finished_at?,error?,artifacts:[{id,name,size}]`。状态为 `running|completed|failed|cancelled`。传输回执不明作为真实错误回报，不伪造未知任务。`ref` 是原设备/上下文的稳定引用，不用不同设备可能碰撞的裸 Job ID。

设备 capabilities 为字符串数组，last_seen 使用 RFC3339。没有真实能力或任务数据时不编造对应显示。状态轮询复用现有生命周期，不另造 SSE。消息目标按每次真实受理冻结，不设跨 Bot 全局当前设备。

源运行时 CLI 合同：`"$XBOT_BIN" device exec --self --json --args '{"command":"...","description":"..."}'`。可显式 `--device ID`，否则沿消息 prepared context 的冻结目标。`read/write` 使用同一 `--args` 方式传递 `path/content`。查询使用 `device tasks --self --json`、`device task REF --self --json`，停止使用 `device stop REF --self --json`。exec 默认 sh。副作用调用发送前生成稳定 ref，也可显式传 `--ref REF`。来源必须来自原运行入口，不能自报账号/Bot或脱离真实prepared Shell上下文。

## 对照回顾

| 来源 | 处理 |
| --- | --- |
| 长期设计及独立审查 | 已覆盖账号归属、pipe体限、外来Jobs持有者、回执丢失和桌面占用 |
| 现有账号/配对 | 账号设备成员为新执行通路依据；旧配对仅保原消息旅程 |
| ADR 0012、0024 | 新ADR明确调整消息以外设备执行边界；不复活node协议 |
| ADR 0021、0043、0066 | 复用环境流、真实实例CLI入口和Jobs控制，不另存转告账 |
| ADR 0019、atlas security | 无新增工具许可卡；账号成员认证不得被Permissive跳过 |
| ADR 0030 | 无账号本机仍可用；账号设备池为可选能力 |
| design.md、guard界面 | 原型已获用户确认，按现有字体、密度和token接入 |
| 六条基本旅程 | 不迁移Bot记忆、不改历史、保忙时插话与原工具结果路径 |
| 候选与并行工作 | 本次用户授权本项开工；不调整其他候选，不接管供应商图标任务 |

## 验证映射

先打通两真实 serve、假账号服务与本地真实 pipe、scripted 模型的最窄端到端：在源对话选目标设备，目标无代办 Bot，目标生成 marker 文件，源得到真实结果。第二 Bot 重复调用验证共享。再测离线、解绑撤销、cwd隔离、长任务停止与回执丢失。

前端检查真实 API 数据与保存行为；浏览器走选设备发送、详情、改名、解绑、添加检查、Bot草稿、窄屏目录与焦点。原型检查不替代产品验证。截图使用独立数据根，目录 `~/.xbot-build/ui-shots/account-devices/product/`，代表桌面浅色、窄屏及受影响深色状态。

按 docs/TESTING.md 选择最小有效范围，一次通过即停。Go测试与CI只使用fake/scripted。使用同一状态有效日志，不重复全仓检查。

## 还没做的

- 冻结实际接口与执行凭证协议。
- 新ADR与原决定的调整引用。
- 后端与前端真实实现。
- 独立编写的端到端验收与代码复审。
- 真实入口截图、产品验收、文档与本地主干提交。

本轮不包含云电脑供应、计费、虚拟独立屏幕和实时远程桌面流；也不迁移账号级Bot存储。它们不是本原型的真实接口范围。
