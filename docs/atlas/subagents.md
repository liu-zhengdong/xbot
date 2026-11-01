---
kind: part
parent: README.md
code: [internal/exec/, internal/agent/, internal/notable/]
verified: 2026-11-01 @ a407f83b (local continuation, per-turn lifecycle, selected-model HTTP and scripted binary checks)
---
# 后台工作与子代理

> **30 秒版**：长活交给有独立记录的子代理，主线保持响应。本机孩子完成、失败或停止后仍可沿原 id 继续；fresh/fork、双向消息、单次选模与独立「子代理」Tab 使用同一执行入口。

## 边界

负责：内核本机子代理与执行者接口（`xbot runner`）。长编码 Link 见 [生态](ecosystem.md)，旧插话分叉见 [fork](fork.md)。新续聊只适用于带 descriptor 的本机记录；旧一次性 spawn/runner 仍可读取、控制活任务，不猜参数冷恢复。决定见 [ADR 0045](../adr/0045-continuable-local-subagents.md)。

## 创建、上下文与模型

- **主线全工具，长活派子代理**：[ADR 0028](../adr/0028-no-foreground-background-split.md) 仍有效。`Kernel.Dispatch` 在 serve/chat 打开，Bot 挂 agents link 时提供 `dispatch`；否则保留旧 `spawn`，两入口不同时出现。短活主线自己做，先经 `send_message_to_user` 回用户；没有前台只剩派活工具或强制步数/秒数收尾。
- **typed 创建**：`dispatch(type,task,context?,provider?,model?,thinking?,reasoning_effort?)`。context 默认 `fresh`，只有孩子系统前缀与任务；`fork` 继承父当前轮开轮之前的已完成上下文，不带当前轮未完成工具调用或插话。父固定模型补丁不作为子补丁；即使父历史为空，子规则、类型、所选模型 overlay 与任务仍存在。工具结果按调用配对投影，原文只追加。
- **类型与工具**：`internal/agenttype` 内置 research（20分钟，读文件/只读解释器/搜索/网页/看图）、dev（2小时，读写/解释器/搜索/看图/本机技能库）、general（45分钟，挂载目录内全部）。`bots/<name>/agents/types/<name>.md` 可覆盖或新增，头部为 when/tools/budget/report，坏文件跳过；没有类型默认模型字段。每一步从当前挂载目录刷新工具，再用创建时白名单过滤，懒加载 Link 可在后续步出现。research 的 `shell:readonly` 是权限标记，按集中分类器包装实际解释器，cmd/PowerShell 不放行 POSIX 读命令。
- 孩子不持有 dispatch/spawn、父/兄弟控制或给主人直发工具；只有 scoped `send_message_to_parent(text)`，没有任意 target。read_subagent/expand_subagent 在孩子内限定自己的记录。工具仍沿原工具确认和 Jobs 行为；本批未把整套子工具升级为主线 reviewedTool。
- **单次选模**：省略沿父当前绑定，只给 model 沿父 provider，只给 provider 取其已配置首模型。显式组合和推理值沿既有 config.Resolve/协议校验；创建时固定有效身份，继续时重新解析当前凭据与能力，不换绑父 Bot，不保存密钥、Endpoint 或旧 listener。孩子实际 route 决定 overlay、vision、hosted/本地搜索互斥、T/H、工具定义预算和摘要器。fit/overflow ratio 每激活独立，派生摘要只在请求投影，冷恢复从原文重新 fit；超 H 无法压入时明确失败。MCP schema 拒绝冷却按实际子 provider/model 隔离，不污染父或其他 route。协议见 [供应商](providers.md)。

## 身份、执行与消息

- `agents/<id>/descriptor.json` 保留无密钥重建事实；同目录 `transcript.jsonl` 是单一只追加原文。物理行 seq 为稳定身份，message_received 存一次正文，consumed/dropped 引用 input_seq，turn_end 记录逐轮结算；meta 只是最新状态投影，不是另一份待做账。主线不复制子步骤。
- **持久身份 + 有限执行**：Local.jobs/waiting 只存活执行和入场。完成后释放 goroutine、并发槽、repo 写资源和旧 scope，不保常驻 idle job。新消息走同一 Message 路径；进程内 canonical child 目录只索引实际 active Local 指针，Web/例程的另一个 Kernel 可命中同 id 原 owner。跨进程仍能核实存活而无法控制时返回冲突，不复制冷启动。
- **容量沿原每 Local 边界**：最多3运行、8等待，同一个 git 检出 `repo:<根>`（非git为 `dir:<目录>`）的写类型串行；research 可并行。跨 Kernel 新建不同 id 不构成全 Bot 总池。等待不计墙钟，BLOCKED 仍计类型预算，步数防呆上限1000。已接受后续输入有16条上限；首 queue/finishing 后消息为该身份预留原等待容量，交接转换成同一 waiting 项，不能成功受理后丢失。
- `agent_message(id,text,delivery="steer|queue")`：活轮 steer 在下一步消费，不打断已跑工具；queue 在自然结算后另开轮，每轮重新预算并重新 admission，原等待任务优先。最后回复到释放之间的新消息进有界 pending，不投死通道。`list_subagents` 能发现近期完成的可继续孩子。
- `agent_stop(id)` 与 Web stop 共用原 owner 原子入口。停止当前执行、丢弃此前未消费输入并记原文；已结束重复停止幂等，排队轮只结算一次。新消息是显式继续；未响应取消的工具真正退出前保持 stopping。已转独立工具 Job 的调用仍沿原取消政策，不因停孩子谎称副作用已撤销。
- **父子通信**：模型父信由内核固定 `bot:<name>`，认证网页直发固定 `owner`，HTTP不能自报来源；模型投影带可读来源。孩子主动父信先记子原文、以真实执行 context 投原 Inbox，忙闲均可接收；来源为孩子而非主人。每轮终结另有自动回报，两者不互相代替，不建签收/已转告账。
- **结果与通知**：每轮首输入 seq 为 turn_seq，结论写不可覆盖 `results/<turn_seq>.txt`。普通回报给状态、步数、轮次、路径和有界预览，主线按路径读取并核对约定证据；写失败报失败。终态和重启中断统一 `subagent:<id>:turn:<seq>:end`，显式父信按实际子记录 seq 独立编号；异步事件自带闭轮身份，不读取下一轮 meta 猜归属。progress 只留子记录，done/failed/blocked 和主动父信沿普通 Inbox、环境流与既有 Waker 唤醒，主线只讲新结果。

## 工作目录、恢复与实际工具

- 创建继承调用方 cwd，孩子 cd 不改父 cwd 或 bots/<name>/cwd。真实结算先同步采样 cwd 落 descriptor、再释放旧 scope，才交接 admission/terminal；旧异步终态不能删除同 id 下一轮 scope。新轮绑定当前实际 runtime，不恢复旧端口。
- dev/general scoped Shell 沿当前 Bot 二进制、数据根和操作 endpoint。短命 serve.Pass 请求退役后，活 Local 执行仍持有入口；实际任务移出并排空队列后，无锁 OnIdle 关闭退役入口。服务退出/明确 Kernel 淘汰沿原关闭政策。research CLI只读分类及远程 runner 回连仍是既有边界，入口见 [内核](kernel.md#当前实例的-cli-资料入口)。
- 服务重启按 PID+ProcStart 核实，失联 running/queued 标 interrupted；补齐悬空 tool_call 的取消结果、丢旧 pending，沿逐轮稳定 ID 报原 Inbox。不自动消费旧消息、不重放工具；显式新消息才继续。保留旧结果引用，不承诺跨文件任意崩溃点 exactly-once。
- 看图每执行只留最新一批，最多4张/合计10MiB，字节不写 transcript；模型无调用回复后、下一轮前清理。工具 panic（含确认回调）收为工具错误并放锁，先脱敏再截断。搜索/看图按所选子模型，而非父绑定。
- **工具 Job 独立**：慢调用2秒或插话后交还等待，实际工具继续，不占子代理3+8。父任务结束/取消不杀已后台化调用；输出、图片和副作用在真实结束时整理，沿原事件回报。详情见 [内核](kernel.md)「慢工具后台化」。Agent主循环与收尾共用[输出截断续写](providers.md#输出截断)，续写时不执行本地工具。
- **值得留意**：`internal/notable` 记录实际删除、对外发送、工作区外写入、全局安装，不拦操作。shell只认明确exit0的简单命令，展开/管道/预演不猜。每任务对象明细最多64KiB，超限标不完整；meta.notable、父结算和UI读同一事实。界面规则见 [ui](ui.md)。

## 读取与界面

独立「子代理」Tab 展示所属孩子、公开对话/工具、继续和停止；Routine 留「任务」。GET transcript 只读不创建模型轮，before 排他、默认40/max200、正序、单行64KiB公开预览、响应700KiB预算；原始恢复记录不截断。公开剥 reasoning/vendor_raw，保工具关联、来源及 truncation；草稿/选区/详情按 Bot+id 保留，迟到读回不串对象。HTTP见 [API](../API.md#子代理通知群)，界面见 [ui](ui.md)，模型读同份[机制手册](../../internal/manual/sections/subagents.md)。
