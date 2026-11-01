---
kind: adr
guard: owner
status: Accepted
date: 2026-11-01
---
# 0048 profile 族经当前实例完整更新，原流与任务跟随改名

## 背景

[ADR0043](0043-cli-platform-runtime-entry.md) 已提供普通资料/生成头像seed运行入口，[ADR0046](0046-process-owned-mcp-sessions.md) 与 [ADR0047](0047-mcp-cli-domain-entry.md) 已稳定进程资源及MCP CLI。profile仍缺upload/clear、name与普通字段的原组合，以及改名后的完整活任务/原流路径连续性。旧工具/Web真实接受name+description/timezone/seed；仅加独立rename动作会削弱已有调用语义。

本刀沿 [同份dev](../dev/cli-platform-entry.md) 的具体profile合同，不重新选择路线/候选，不提前迁其它工具族。

## 决定

1. 选择一个typed ProfileChange领域调用，保name+description/timezone/seed同请求。avatar upload/clear保原独立动作；不扩role/timebudget，不新加set头像文件组合。self始终用原endpoint/Resources定位，XBOT_BOT只诊断，同脚本改名后第二CLI沿原endpoint认新名，不增加UUID/别名。
2. 纯输入规范化/校验与持久提交分开，不调用ApplyProfile作试校验。preflight在任何业务写前检查name、目标Bot/work/key和图片；提交primary锁内复核依赖冲突。改名时profile字段与legacy seed pin合入最终fresh config写缝，不经会覆盖新字段的旧SaveConfig。可预测无效输入/冲突零更改；任意多文件I/O不承诺全事务。实际move/apply后错误回真实name/previous_name/applied_fields/moved/error，CLI非0并发布已发生变化，不能把route退回旧名。
3. profile JSON/回包仍64KiB；新增 `/v1/profile/avatar` 原始二进制流body至多1MiB，mime header与原root/Host/browser边界，clear用DELETE。不传base64、服务端本地path或临时文件服务。Web原multipart兼容入口仍file至多1MiB，转同domain；普通profile元数据是独立请求。
4. 沿原operations→host→code→现exec owners/Local→短storage→Jobs→route→physical链扩展真实移动；route不能反取exec/Join/网络。只迁现accepted owner索引中的真实Local/job，空Local用当前目录闭包冷恢复，record/descriptor/status/results写在短path fence内。job/request/workspace/descriptor.Bot+CWD及实际内部cwd按旧Bot/work前缀重基，外部自定义cwd保留，旧transcript行不改，ID/turn_seq/原选模/per-Local准入不变。
5. 用同Resources当前路径的短IO回调承接Timer/Inbox/父信/Code/工作区与主线存储，不异步写所有旧K.Bot、不建全旧K登记表。liveTurns只rekey原表中同Resources的真实value，turn保存登记key并按value CompareAndDelete，完成后锁外按原Sink发布name/previous_name。保原Jobs/MCP ref/entry/Gate、source调用lease与hold释放，允许的原活任务场景不能以新busy拒绝逃避。
6. UI只改现有状态处理：previous_name匹配当前selection时就地更新同对象、URL/sidebar/header/composer与child草稿key，保SSE reader/abort/text/focus/scroll，不selectBot重开流；非当前/迟到信号保原gen拒绝。无视觉改版。
7. 等价通过后同cut退profileTool/parseProfileArgs、bot_update_profile注册/旧review名与UI工具结果hack，保领域RenameBot/FlushLegacyHeld、persona与group原独立业务。普通资料与头像也统一profile SSE，不执行隐藏旧profile兼容工具。

## 对照与替代范围

A保真实组合语义；B把rename剥成独立动作虽省接口，但削弱旧同请求能力，淘汰。不以多文件回滚账本或全旧K registry补复杂性。

本决定在实际profile族退役时替代[ADR0043](0043-cli-platform-runtime-entry.md)第4条的临时保留条件，以及[ADR0035](0035-unified-tool-registry.md)的该旧模型项；其它工具门、persona、MCP与权限默认放行不变。旧Accepted正文不改。完整远端runner、人用PATH安装、其它平台族迁移不在本刀。

## 怎样算对

最窄真实Shell组合改名后原endpoint第二show/MCP和下一步append认新名；冲突/非法组合零改；大于64KiB有效头像、1MiB边界与clear不经JSON图像；活Local gate跨move父信/结算和冷原ID续聊落新目录，原Job/code/timer/hold不断；浏览器previous_name就地改名保原流与草稿。普通profile/seed及2a既有证据复用，只验新增组合和连续性；独立用例仍由验证方维护。24对de01d5bb+8a2d382f新增结构复核通过，唯一rekey锁位澄清已纳入合同：整段MoveResources/code/exec/Jobs返回、route/storage/owners全释放后，publish前按同value与保存key迁移，不复活closed turn；chat/name-only/cold同末段。现获授权实施，不是产品完成记录。
