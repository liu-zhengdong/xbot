---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0067 退役旧轮次分叉并统一公共协议与发送身份

## 背景

用户在全仓审查后，明确批准修复三处最严重的坏味道。生产已经关闭自动轮次分叉，但内核和 Web 仍保留其完整执行机制。多个 CLI 族重复实现同形传输和文本产物。网页发送则用正文数量和 DOM 字段推断消息是否落地。

## 决定

1. 删除 AllowFork/ForkTurns 对应的实时轮次分叉、资源冲突队列和第二套 Web 处理锁调度。现行插话继续沿 [ADR 0017](0017-owner-words-go-straight-into-the-turn.md)，主线与子代理沿 [ADR 0028](0028-no-foreground-background-split.md)。保留真实子代理审批、并发与预算、慢工具 Jobs、bot-fork 派生以及 dispatch 的 context=fork。
2. 旧 branch/fork/fork_end 的读取、导出和引用保留。最小未闭合修复只在持 handle 的 Drain/Handle 内执行，每 BotResources 生命周期一次，无 Resources 则每 Kernel 一次。真实任务恢复不并入该 once。上下文快照写入沿同一当前资源路由，避免保留的 Kernel.Bot 在改名后重建旧目录。不新增恢复账、索引 schema 或旧进程监控表。
3. 经历检索、记忆召回、原文读取共用一个 TextResult 和 formatter。严格 JSON 解码、本机 POST 与文本产物输出各有单一实现。各领域继续拥有 CallID、冻结 turn、审批、脱敏和业务结果，不建设通用 operation runner。记录经历的部分成功 ACK 保持独立。
4. 每次网页新提交使用独立 client_id，格式为 web_ 加 32 位小写十六进制。Bot 消息沿既有 Event.ID、Entry.EventID、ChatMeta.EventID 关联。服务端接受可选字段，省略时保原 WebEvent 行为。HTTP 2xx 和回执 ID 只表示受理或关联；持久对账以对应 owner chat 行为准。
5. 前端发送状态由普通记录持有，DOM 只展示。只按精确 EventID 消解对应临时气泡，不按正文或附件数量推断。跨进程等待同样按 Entry.EventID 定位。群发送沿原同步 status=sent 回执。重试沿对应记录，不引入自动重发、永久送达账或副作用幂等承诺。

## 选择

- 旧分叉采用已有资源持一次最小兼容恢复。为退役功能增加开放分支 schema 或离线迁移命令，都会增加维护或用户操作成本。
- CLI 采用小型公共函数和唯一文本形状。策略式泛型 runner 会把领域差异变成开关和 hooks，并增加转发层。
- 发送采用普通记录和已有 EventID。扩充正文/附件签名仍不能区分内容完全相同的独立提交；引入 reducer/store 又会增加当前不需要的机制。

## 接受的代价

旧恢复首次允许一次全历史读取，保持原 best-effort 语义。遇存活旧进程时跳过，不在同一资源生命周期持续监控；下一次资源或进程可重查。处理锁是原并发边界，不新增跨版本无锁执行协议。

CLI 保留清晰的 typed 领域 callback。少量领域错误前缀与输入差异就近表达。公共协议继续守 raw 与 escaped 请求 1 MiB、JSONNL/human 各 32 KiB、HTTP 64 KiB 和完整产物上限；非 2xx 也保实际回执。

未出现在当前 chat 窗口的发送 ID 仍属未确认。刷新后已记录消息由现有 chat 恢复；本次不增加持久待发箱。

## 关系与验证

本条落实 0028 的单一现行执行路径，并补充 [ADR 0039](0039-chat-delivery-timeline.md) 的发送关联。0021 的对话串、环境流与插话口继续是唯一运行机制。原 0001、0002 已被取代，无需恢复其测试或执行路径。

验证覆盖旧分支读取/修复、同资源多 Kernel 的恢复次数、普通插话与真实子代理/Jobs、CLI 协议边界与部分回执，以及同文不同附件的独立发送状态。现行 guard 旅程和视觉要求保持。
