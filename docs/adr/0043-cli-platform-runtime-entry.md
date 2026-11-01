---
kind: adr
guard: owner
status: Accepted
date: 2026-11-01
---
# 0043 CLI 平台操作连接原运行时

## 背景

2026-11-01 用户确认 [CLI 平台路线](../dev/cli-platform-entry.md)，授权先实施批 0/1。Agent 需要通过当前二进制操作当前 Bot；直接改配置文件不能保证活实例生效，经 message/send 让自己再处理又会等待本轮持有的对话处理锁。独立 chat 自持 Kernel，因此不能只接 serve 的网页 API。

## 决定

1. 平台管理逐领域迁到 CLI，命令连接发起方所属运行时的领域操作；帮助离线可用，运行操作不可达明确失败，不另起 Kernel，不静默写文件冒充热更新。
2. 首刀只覆盖 `profile show/set --self --json` 的简介和时区。原进程内提供只绑定当前 Kernel 的窄本机 HTTP 操作入口，serve、桌面和独立 chat 使用同一协议。短命 serve.Pass 的 Kernel 返回后，已有本机子代理/编码任务沿原 live 集合持有该入口；实际任务结束并排空原队列后，经无资源锁的 OnIdle 回调关闭退役入口，服务 context 是寿命上界。无活任务则立即关闭，不造新任务表或常驻全部短命 Kernel。实例数据根须吻合；环境传递当前二进制绝对路径、数据根、endpoint 和诊断 Bot 名，`--self` 以运行时绑定为准。
3. 普通 profile 操作共用 bot 领域校验和 config.json.lock 内的合并保存；操作不拿对话处理锁、web gate 或 turnMu，不取消任务、不重建 Jobs。UI 读取业务资料状态，在流尚未结束时刷新既有组件，不解析 Shell 输出或伪造旧工具事件。
4. 已有显式名字的离线配置命令保留明确语义。头像和改名未由 CLI 完整覆盖前，保留旧模型工具；本批不删除整个 profile 工具族。
5. 后续 MCP 与平台工具按路线逐族切换。Event → Inbox → 唤醒、基础交互工具、当前权限默认放行和已有脱敏保持；不增万能 action 总线、独立 daemon、Job 表或持久 Bot 别名库。

## 取代范围

本批补充普通 profile 的 CLI 运行入口，尚不移出 profile 模型工具族。[ADR 0035](0035-unified-tool-registry.md) 在剩余模型工具上的注册表、代理门和 MCP 契约继续有效；后续实际移出工具族时再逐条记录取代范围。本 ADR 不据路线提前声明 Link 或 MCP 装配已经退役，不修改原 Accepted 正文。

## 考虑过的方案

- CLI 各自加载配置或新建 Kernel：不能同步更新原运行态，也会丢失现有连接和锁所有权。
- 独立 chat 装配完整网页服务：引入无关登录、UI 和对话调度，超出窄操作需求。
- 每个活 Kernel 绑定窄回环 listener：当前选择；不需要路由登记表，chat/serve 协议一致。接受多个活 Bot 各有一个小 listener，后续出现真正实例级共享领域时再调整所有权。

## 后果与边界

每次 CLI 有进程启动和 IPC 成本；本机 endpoint 是同用户定位信息，不是主人身份认证或安全隔离。远端 runner 尚不具备回连原实例的完整通路，不将回环 endpoint 当作远端已支持。具体锁、命令、可观测接口与检查见路线的批 0/1 实施节；2026-11-01 独立设计审计与主管复核通过后开始实现。
