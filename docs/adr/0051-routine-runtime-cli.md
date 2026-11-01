---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0051 当前 source 的实际 Timer 经 CLI 做例程 CRUD

## 背景与调用方

skills 切片已收口；本刀只继续同一 CLI 母路线的 routine。现四模型工具借 mounted Timer，离线 CLI 则另造目录 Timer；二者不能靠 --self 换字样当作同一运行态。现 source HTTPctx 也不含实际 Shell 调用的 turn、fork/park 或完整审批回调，不能假称复用现审批即已连接。

调用方使用 `"$XBOT_BIN" routine list --self --json`、`create --self --args JSON|--args-file FILE|- --json`、`update ID --self --args JSON|--args-file FILE|- --json`、`delete ID --self --json`。完整 DTO、路由、具体接缝与怎样算对只写在同份[母 dev 的 routine 切片](../dev/cli-platform-entry.md)。无 --self 的旧 migrate/list --bot/update --bot --skills 保离线；runtime 失败不回落。

## 两个结构与决定

A 为 CLI 直接 new Timer{Dir} 写文件，没有 source 的实际 Timer/Loc/审批，淘汰。选择 B：原 source listener 捕获 actual mounted cached Timer，四个具名 typed CRUD handler；不启动/替换 Timer、Kernel、scheduler 或 Jobs，不调用退役 Tool.Run、不加 action bus。

- StartOperations 将现 mounted snapshot 的 bindCurrentPaths 放发布前，再捕获同一实际 Timer 与 source Root/Now/动态 Redact/Resources/Jobs/listener身份。缺 Timer 只 routine 回原未挂载错误，不阻其它入口。Timer 已持 current route，handler 不外套 currentIO，Update 闭包不嵌 bound Load/Save。
- 保 create Enabled=true、空ID审后随机、同ID覆盖、cron local、once UTC/now−2min窗口。update 只有 name/prompt/cron/Enabled*bool/Precheck*string/Skills*[]，保空省与清除区别，不加 at/zone/LastSlot；改cron不暗清旧At，原 fresh merged validator 仍拒冲突。边界一次 decode/规范 typed 输入，不移除持久状态不变量检查。
- precheck 审查与触发 gate 分开。create/update effective命令（update省仍取原命令）在所有业务锁外交原 source reviewPrecheck，以 process cwd、原 machine/protected/untrusted-memory/权限表审；不借 owner CheckRoutinePrecheck 的临时 Permissive K。
- 补现实际审批连接：agent.execTool 在 opt.RunTool 前把已包装 opt.Approve、原Shell名/provider调用ID、现审批mutex绑定ctx；原Jobs.live仅附一份最终prepared ctx，ctx-aware Shell最终env传其Jobs ID/header。HTTP只从该source原Jobs内存live表取Local/running/未finished且同listener的原call，放锁后调用，complete清ctx。无新任务表、反查历史或确认协议；bridge只服务非空effective precheck，不强制其它族或普通CRUD。
- main政策保原冻结身份，审批卡ctx回明确原owner以读动态forkbranch；child在实际Local.RunTool处给nested review明确source-untrusted/no-person取值，保原subApprove/subApprover，不把现父turn fallback冒原child、不造turn/role/persona。缺原call、code/remote/外部命令的非空precheck明确失败且定义不写；普通无precheckCRUD仍可。
- parked grant仍由现parkable/argsKey/deniedBefore处理。只在nested审批ToolArgs向原Shell字段附服务端canonical action/请求routineID/typedpayload/effectivePrecheck；真正Shell.Run raw不改。requestID不是HTTP随机ID，自动routineID审批后才生，canonical不含Job/provider随机标识，同A重试稳定、argsfile或effective命令B不吃A。原Shell名/command/provider调用ID与late消息保持可重试，不向退役工具发grant。
- Save/Delete定义已提交而.zones失败，仅在原后段用带 Applied:true/Stage:zone_index/Unwrap 的具名错误保事实；CLI回实际partial并非0，不伪全事务。update原预读审查→锁内重读窗口保留，不加CAS、冲突重试或锁内等待。
- HTTP请求≤1MiB、回包≤64KiB；最终JSON含换行还服从现Shell32KiB完整输出预算。大list沿现results保存完整脱敏JSON，当前Resources工作区小引用也须fit；不截内容伪成功、不提高全Shell。原Due/Deliver/emit/Commit、水印、slot去重、livegate、唯一Inbox/waker不迁移，CRUD不手动post/wake。
- 同cut退四模型工具/wrapper/registry/review分支与三入口schema指引；保Timer器官/Emitter、其它Tools门、原离线命令及例程skills投影。research复用同窄lexer和纯parser，仅help/list--self只读，不新Shell路径框架。

## 代价、范围与验证

接受完整原callctx只在真实活跃Job内可取；没有原审批上下文的非空precheck不能冒另一turn执行。接受较小消费者预算及完整产物，避免64KiB HTTP成功而模型收32KiB坏JSON。接受真实zone-index partial及原预读窗口，不建跨文件事务或新调度语义。现wrapper对带空白旧at先验遗漏，本刀以同规范值实施已定now−2min边界。

最窄 source list 红已独立固定在 c5605860；具体合同单次窄核通过后先首可编译CRUD链，再按实际受影响的审批/Timer/partial/消费者接缝验收；已封skills/profile/MCP不重跑、不全仓/真模型或完整矩阵。本决定不改0021环境流、0043实例、0046资源、0049改名或0050技能，母路线其它族继续保留。
