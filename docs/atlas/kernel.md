---
kind: part
parent: README.md
code: [internal/kernel/, internal/inbox/, internal/link/, internal/inbound/]
verified: 2026-10-16 @ d04f79e
---
# 内核：Link、Event、收件箱

> **30 秒版**：内核只有几个抽象：Link 是 Bot 和外界之间的任何通道，Event 是统一的事件结构，每个 Bot 一个收件箱和一个事件循环。新能力尽量用这几个抽象表达，不加新对象。

## 边界
负责：事件进出、可信判断、一轮对话的循环、工具调度和确认路径。不负责：具体工具（见 [生态](ecosystem.md)）、transcript 存储（见 [上下文](context.md)）、权限分类（见 [安全](security.md)）。

## 怎么运转
- **Link**：Bot 与外部世界之间的任何一条通道。出站方向把动作暴露成工具；入站方向把外部发生的事变成 Event，投进 Bot 的收件箱。终端、Web 界面、定时器、Webhook、文件监听、MCP、浏览器、编码代理、其他机器、其他 Bot，都是 Link。
- **Event**：统一的事件结构 `{source, type, content, sender, time}`。内核根据来源判断是否可信：只有主人通过经过认证的入口发来的消息才算可信，其余一律当作不可信数据包裹起来。
- **收件箱与唤醒**：每个 Bot 只有一个事件处理循环，事件按顺序处理，同一事件不会处理两次，主人正在聊天时事件也会被及时处理。终端只在真正跑一轮时拿着处理锁，停在提示符上时不占锁，网页可以当场回复，也可以 fork。网页写进 transcript 的行，终端下一次刷新时补上。主循环在两步之间把这一轮期间新到的收件箱事件按到达顺序接进当前对话，下一步就能看见，transcript 的时间不会倒退。
- **主体（Principal）**：人、Bot、群都是主体，都可以发消息、可以被授权。


## 共享规则
所有 Bot 共用的规则在 [assemble.go](../../internal/kernel/assemble.go) 的 `DefaultRules`。其中两条是主人的做事方式：`JudgmentRule`（先给答案和理由；主人的说法只对一半时指出哪部分对、哪部分不对，给独立判断；需要决定时以一份可直接批准的方案收尾）和 `ResourcefulRule`（缺工具或信息时先做能做的部分，推断的内容标「待确认」，或换一条路，例如把文件放到共享处再给别的 Bot 发消息让它自己改；只把最小的剩余一步交出去并说明原因）。评测用例 `half-right`、`propose-plan`、`vision-draft`、`avatar-handoff` 和留出的 `ci-version` 守着它们。
