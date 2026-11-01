---
kind: adr
guard: owner
status: Accepted
date: 2026-11-01
---
# 0045 本机子代理保留身份，每轮执行有限释放

## 背景

用户确认「子代理对齐DSH-讨论稿」五项并授权实现：完成后同id继续、fresh/fork、父子双向消息与自动结算、单次选择供应商/模型/推理参数、查看并直接继续或停止。现有exec在任务结束后删除job，Control只找活job；固定result.txt和一次性任务假设不足以表达多轮。用户同时要求保留3运行+8等待与同仓写排队，首版本机，不做嵌套派生或workflow。

已读现行atlas、ADR0021/0028/0035/0043、guard、相关dev与全部165份候选的标题/metadata；直接相关8篇全文范围结论和可审计接口见 [正式设计](../dev/continuable-subagents.md)。本记录说明新决定，不把计划写成已落地现状，不修改旧守护ADR。

## 决定

1. 采用**持久子代理身份 + 每次有限执行**。原agents/<id>目录保存重建事实、只追加原文和逐轮结算；Local的jobs/waiting仍只表示实际执行与入场。空闲释放goroutine、并发slot、repo写资源与已退役runtime入口，不升级成长期Bot、不新增Session产品概念。
2. exec独占子记录、descriptor/meta、逐轮结果与admission。Kernel独占类型/供应商解析、作用域工具、模型预算和当前runtime装配；web与模型工具共用Kernel领域入口，不各写一套运行状态。进程内仅用canonical AgentDir→原Local的活owner指针路由，不复制任务状态；跨进程可证明活着则拒绝重复冷启动。
3. 默认fresh。fork明确截至当前父轮开轮之前的已完成边界，不携带当前轮不完整调用；子前缀仍明确子规则、类型和所选模型overlay。继续使用原id与原对话，不重新dispatch一个新id。
4. 新消息统一经过本机Message：活轮steer下一step，queue在自然结算后另开轮；每轮重新预算与按原admission排队，等待不计时，BLOCKED沿原墙钟。后续待轮与控制容量有界，满则受理前拒绝；终结窗口消息不能掉入死通道。Stop丢弃此前未消费消息并追加记录，显式新消息才可继续；不重放旧工具。
5. 每轮使用首输入原文seq为turn_seq，不建run账本。结果固定在results/<turn_seq>.txt且不覆盖；自动终态使用subagent:<id>:turn:<turn_seq>:end，主动父消息使用子记录seq。恢复中断也用同一逐轮终态key，避免原一次性id被Inbox去重吞掉后续轮。
6. 父子消息使用原对话串、Inbox与唤醒。孩子只有scoped send_message_to_parent，不接受任意target，也不复用给主人说话的工具；主线进度不复制。普通消息与自动结算各自保留，不增加签收/已转告账，保持ADR0021。
7. 单次选择只使用既有供应商目录和凭据入口；创建时固定有效provider/model/thinking/effort，继续时重新解析当前凭据与模型能力。descriptor不保存Endpoint/APIKey/令牌/旧listener。子模型窗口、overlay、vision、搜索与fit摘要独立，不改父Kernel预算或binding，不复制带锁Kernel。
8. cwd在真实执行终结时同步采样持久化、然后释放scope，再交接admission；新轮绑定当前实际runtime，旧终态事件不能异步删除同id下一轮scope。重启只中断并配平悬空工具结果，不自动重跑任何已发生副作用。
9. 新能力只覆盖带descriptor的本机路径；旧一次性spawn/runner记录保持可读与轻兼容，不猜恢复参数、不建设迁移框架。共享ResultText等旧路径不因本批破坏。主Agent全工具与统一registry/review继续沿ADR0028/0035；CLI入口寿命继续沿ADR0043。

## 考虑过的方案

- A：job完成后常驻idle，继续持有provider/tools/内存对话。热继续简单，但活表同时成历史目录，idle与slot/repo/runtime寿命都要补分支，跨重启另加恢复又产生两条续聊路径，否决。
- B：目录保留身份、每轮有限执行，恢复重新入原admission。接受装配和读取开销，换取空闲不占运行资源、冷/热同一条路径，采用。
- 不新增通用workflow、父邮箱、第二套调度器或任意递归。候选「子代理工作流」的review/max_rounds与idle持repo锁不纳入本次决定。

## 后果与边界

旧结果引用稳定、父Bot能用同一id反馈、UI能查看真实子对话。2026-11-01 用户明确入口归属为独立「子代理」Tab，「任务」Tab 查看 Routine；查看、继续和停止的行为验收不变。需要增加无密钥descriptor和逐轮边界、活owner定位、明确受理/终结的原子裁决；这些均属于当前exec的知识，不交给web或第二个manager同步。正常结算保存最终cwd，崩溃只保证最近已落盘事实；不承诺跨文件任意崩溃点exactly-once，不自动消费崩溃前待做输入。

实现文件归属、Go/JSON/工具契约、测试路径、验收、候选逐项筛选与还没做的统一见 [正式设计](../dev/continuable-subagents.md)。本ADR是已批准范围的结构决定，不表示代码已实现、已合入或端到端通过；不修改现有guard步骤及验收。UI仍先由专门设计方出原型定稿。
