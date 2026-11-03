---
kind: part
parent: README.md
code: [internal/kernel/bgrecover.go, internal/kernel/bgcompact.go, internal/bot/replay.go, internal/proc/proc.go]
verified: 2026-11-02 @ 83368b48 (retired live fork-turn engine; retained legacy replay and locked recovery)
---
# 旧分叉记录与恢复

> **30 秒版**：主线直接执行工具，插话接入同一轮。长任务可以交给真实子代理。旧轮次分叉引擎已退役，旧 transcript 仍能读取、引用、导出和修复。

## 现行边界

`AllowFork`、`ForkTurns`、旧 `bg_` 执行器、资源冲突队列和共享 hold 调度已删除。处理锁继续约束每个 Bot 的主线。插话沿 [ADR 0017](../adr/0017-owner-words-go-straight-into-the-turn.md) 接入；主线与子代理沿 [ADR 0028](../adr/0028-no-foreground-background-split.md) 工作。

真实子代理仍有自己的执行器、审批、并发与预算。慢工具继续使用 Jobs。`bot-fork` 派生新 Bot，`dispatch(context=fork)` 派生孩子上下文；这两项能力仍然可用。见 [子代理](subagents.md)、[内核](kernel.md) 和 [ADR 0067](../adr/0067-review-hotspot-simplification.md)。

## 保留的历史格式

主 transcript 始终只追加，seq 仍是原记录序号。

- `Entry.Branch` 标识旧分支。空值表示主线。
- `fork` 标记保存 branch、from 和 task。Replay 将 from 到标记前的相关行归到该分支。
- `fork_end` 标记保存结束状态和步数。
- 旧 `agents/bg_*/meta.json` 继续提供进程身份、任务、起始位置和步数。

Replay、窗口、chat 投影、工具结果配对、记忆材料、压缩边界和导出保留原分支规则。导出仍带分支说明；`reply_to` 仍可引用已有分支 seq。普通旧记录缺少 branch 时按主线读取。

网页继续隐藏内部标记，并读取旧任务轨迹。历史兼容不再创建实时分叉、旧控制队列或后台轮询。

## 未闭合分支的修复

`RecoverInterrupted` 只修复旧格式。调用方必须持有 handle 锁。

1. `proc.Drain` 在重载 Inbox 前调用，空 Inbox 也能产生恢复事件。
2. 直接 `Handle` 在 `repairTail` 前调用，补齐未经过 Drain 的入口。
3. 同一 `BotResources` 通过独立 once 只检查一次。无 Resources 时使用 Kernel 本地 once。

首次检查允许读取完整历史。后续替换 Kernel 不重复扫描。改名沿用资源对象；每次实际 IO 都取当前 Bot 路径。恢复闭包不整体持 route 锁。

原 PID 与启动身份仍存活时跳过。否则追加 interrupted 标记，更新旧 meta，并投固定 EventID 的普通完成事件。修复不重跑任务，不改旧行。`repairTail` 继续为原分支配对未答工具调用。

恢复保持尽力而为。资源生命周期内不持续监控存活的旧进程；写失败不新增重试账。原处理锁仍是并发边界。

真实 `sa_`、code、Remote 和 Jobs 仍走原 `RecoverTasks` 生命周期，不纳入遗留分支的 once。

## 检查入口

恢复生命周期由 `legacy_fork_lifecycle_test.go` 和 `legacy_fork_recovery_test.go` 覆盖。旧 Replay、窗口、工具配对和导出用例保留。忙时插话与 Bot 派生由 `TestInterjectMidTask`、`TestCLIBotForkRuntimeEntry` 覆盖。

验证结果见 [MILESTONES](../MILESTONES.md)。
