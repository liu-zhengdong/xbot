# 例程可挂技能（L：叫醒上下文 + 例程写口）

## 旅程与怎样算对
按用户本次指令：例程按序挂已有技能，触发后的模型请求可见技能正文；未挂字段/空数组保持原行为；删除/改名/停用后明确跳过原因。设置 UI、CLI、routine_create/update/list 读写。未确认不写技能库，不改调度/precheck，不碰 voice-chat、记忆、webhooks，不合 main、不 push。

## 方案
Routine.skills []string，可选有序引用。Timer 仅把引用带入 Event.RoutineSkills（cron/at 共用例程事件构造函数），不读技能正文。唯一正文装配 seam 是 Kernel.projectRoutineSkills，初次 Handle 完成 assemble 后和忙时 eventMessage 吸收共用此缝的 routine-skills 投影：仅 timer 事件，在该次叫醒的 user message 附加 read_skill 等价的正文（复用 openSkill、skill.Read），不改固定前缀、不写旧 transcript、不写技能库。正文只在该轮内；下一轮重新读取。缺失、改名、停用：跳过该项，在同一上下文列出 id 和原因，继续任务。最多 32 项、每项 240 字节，正文复用 16 KiB 上限。

写口：工具 create 接受数组、update 省略保留/[]清空、list 显示；HTTP PATCH 同义，GET 自动返回。CLI 保留 migrate，增加 routine list --bot 和 routine update ID --bot --skills id1,id2（空串清空）。只改技能字段，不扩张通用 CRUD。
UI：沿用任务面板例程行；增加技能编辑入口，展示有序引用，编辑逗号分隔名称，保存/取消；保存失败保留输入。用户已批准设计师 `routine-skills/ia.md` 与 `handoff.md` 的生产约束：唯一装配缝为原 `.task-routine`，36px 行头、14px 图标、34×20px 开关、6×10px pad；不照抄小样的双白卡灰壳或偏矮尺寸。不做选择器、技能展示名映射、Webhook 皮或例程 CRUD 重做。

## 对照回顾
| 材料 | 结论 |
|---|---|
| 本候选与用户本次验收 | 直接落地正文可见、未挂不变、缺失明确；不触及候选其它能力 |
| atlas multibot/kernel/ecosystem/context | 复用 Timer、Event、Handle 装配、openSkill/Read、现有启停；无新调度机制 |
| ADR 0021 | 当次 user message 临时投影，不改固定前缀，不加状态账本 |
| ADR 0028/0035 | 不改 tools[]/权限表；不增加模型工具 |
| guard 六条基本旅程/长程运行 | 不变更调度、precheck、去重；旧未挂例程路径保留 |
| guard 界面 | 原例程行和令牌组件；原型审批后才改 UI |
| 自装技能/干完提议写技能设计 | 只读已有库，不调用 write_skill，不动提议确认策略 |
| 其它候选：记忆/语音/webhooks | 无数据或行为依赖；保持独立 |

## 验证
独立方补冒烟；实现侧测试 Timer→Inbox 序列化→Handle→scripted provider 的真实装配 seam，顺序、缺失、停用、技能更新重读、空数组、下轮不泄漏、技能库字节不变；工具/API/CLI 省略保留与清空。浏览器验证读写、失败重试、窄屏和焦点。全量 test 一遍、改动包 count=3、vet、gofmt、atlas_audit；性能记录本地装配耗时。真入口冒烟用 fake/scripted，不调用真模型。

**冒烟用例**：`e2e/smoke/routine_skills_test.go` 的 `TestRoutineSkillsCLIAndWake`；后端级别为 CLI→serve→scripted 模型端到端通过。UI 行为用例为 `internal/web/routine_skills_browser_test.go` 的 `TestRoutineSkillsBrowser`，使用生产静态资源与受控 fetch，验证有序保存、清空、添加、失败重试、取消、轮询焦点与原开关；本地 Chrome 与例程 API 定向三遍通过，截图已生成。UI 未合 main，尚不等于真实 serve 全链路或日用验收。

## 还没做的
设计师对生产 UI 浅深色截图复审；真实 serve 界面全链路与用户日用验收；独立编码复审与合入由主管后续处理。
