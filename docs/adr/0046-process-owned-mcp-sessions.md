---
kind: adr
guard: owner
status: Accepted
date: 2026-11-01
---
# 0046 MCP 会话由进程持有，Kernel 只借用

## 背景

已确认的 [CLI 平台路线](../dev/cli-platform-entry.md) 进入 MCP。现 `link.MCP` 同时持协议 client、工具适配、配置与通知 Emitter；serve 的通知长期 K、web 缓存 K 和每次 Pass 的短命 K 可各建连接。短命 K.Close 还会 Shutdown 自己的 Jobs，导致原 MCP client 即使延寿，外层慢 Shell 仍被停止，后续 K 也不能取消原 Local 任务。

这是本次会话唯一所有权切换必须处理的实际后果，不是引入另一个 Agent 内核或通用平台 hub 的理由。

## 决定（2026-11-01 整体结构审计 PASS）

1. stdio、HTTP、JSON-RPC、诊断、配置机械移入 `internal/mcp`。core 不依赖 link/bot/kernel/web/exec/tools；凭据解析、provider key 判定和当前 redactor 由入口注入，通知为有界字符串流。协议结果保留 isError/content/未知块/structuredContent/Raw，不在 core 丢图片或未知数据。
2. serve/chat 各有一个进程 Manager，同 Bot/server 只有一个真实 Session。Kernel、旧工具 adapter 借用它，不 Close；Manager reconcile 同时拥有 client 与通知泵。通知在装配变为原 Event/Inbox/waker，不增加完成事件或对话账本。商店 probe 保有界临时会话和真实鉴权探测。
3. 复用现每 Bot 缓存映射作为资源条目，共享原 `*tools.Jobs`，不复用整颗 Kernel。Pass 仍以 fresh Bot/Provider/role/budget/恢复构造；chat 不为此装配完整 web。每次 call 的 Prepare/Finalize/turn/replyGen/cwd、Redact/Forms 属原 source，默认投递只绑定一次，request.OnDone 与默认保持单选；没有 JobOwners/Job ID 路由表。
4. 本刀保留 source Kernel 的 CLI listener 所有权。实际解释器在 Prepare 取本 listener lease，完整 cleanup 最后无资源锁释放，取消请求不等于真实结束；同表其它 source 活动不能保住空 Pass。原 Local/code 集合继续覆盖未来 Shell 与队列交接；宿主 context 仍是上界，不同时引入进程 profile endpoint。
5. global+Bot 同名整条 override，只在真正 effective 传输 spec 变化时替换。沿现 Close 旧再连新，短 slot gate 保唯一连接；锁不跨网络，不增加 drain/active/desired 等待状态机。已发 call 因变更而失败明确 configuration_changed/outcome_unknown，绝不自动重发写。发现 15s 与工具调用 ctx 分离；显式取消仅对原 request ID 最佳努力提示，不能保证撤销副作用。
6. 2a 保留只借用的旧模型 adapter 是短暂可观察迁移缝；2b 真 CLI 纵向验收后同刀删除 MCP 的 adapter、schema、旧装配。2a 不提前增加完整 CLI/结果产物接口，不长期双入口；其余冷工具门不因本决定提前删除。
7. 共享资源涉及的既有配置、商店、web 重建、改名、删除必须在实施前核对真实调用链。改名沿既有 code/Jobs mover 包物理移动并更新通知路由，不能只事后改名字；删除先验拒绝不误关，停止资源与真实 join 在全局锁外，长等待后的失败如实可恢复。具体锁/关闭/恢复切口已在同份 dev 经独立结构审计通过，不引删除账本、持久 marker、UUID/alias 或全旧 K 登记表，也不宣称完整异步 runtime rename 已完成。

## 考虑过的方案

- 每个 CLI 或 Kernel 自建 MCP：不能保连续会话、长期通知或后续轮取消，继续当前多 owner 问题。
- 缓存一颗长期 Kernel 承包所有入口：会改变 Provider/role/budget/恢复/gate 语义，并让 chat 依赖 web，超出 MCP 责任。
- 进程域 MCP owner、共享原 Jobs、source listener lease：选择；只提升实际需要稳定的资源，保留新轮 fresh 构造。
- drain 热变更或新 JobOwners/control 路由：本次没有保全热改在途事务的产品责任；现表可共享，额外状态和路由不成立。

## 与现行决定的关系

[ADR 0021](0021-env-stream.md) 的消息/环境规则继续有效。[ADR 0043](0043-cli-platform-runtime-entry.md) 的当前实例身份、原 Kernel profile 操作和 source listener 继续有效。

本刀只移动 MCP 会话所有权，不在 2a 宣告 [ADR 0035](0035-unified-tool-registry.md) 的 MCP 模型适配已经退役。2b 实际退役时再明确其替代范围；剩余工具注册/代理门继续守原契约。旧 Accepted 正文不改。

## 后果与未覆盖

同用户同进程的 Bot 隔离会话，不跨 OS 进程共享 client；远端 runner 尚无回连，不把 loopback 当远端能力。vaultd 不提供明文时不得暗降本地，stderr 保启动秘密快照和当前脱敏。完整 runtime rename、OAuth/Registry/自动安装器、2b CLI/文件产物与 research 只读分类不在 2a 冒称完成。

结构和最小红夹具以 [同份开发设计](../dev/cli-platform-entry.md) 的批 2a 节为准；现协议 core 的纯机械迁移已获独立放行，不改变生命周期或HTTP契约；新所有权、取消修正与runtime接线已获整体具体结构PASS，按可编译切片实施，尚未宣称产品完成。
