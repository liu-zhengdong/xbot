---
kind: adr
guard: owner
status: Proposed
date: 2026-11-01
---
# 0050 技能以当前实例 read/write 领域入口替换两模型工具

## 问题与调用方

平台 CLI 母路线已定；现 read_skill/write_skill 仍是模型工具，write 同时决定本库文件与所有本地 Bot 的默认开关。不能把它替换成 CLI import 加若干 enable 调用而丢掉原库锁配套语义。当前 skill.Read 静默限16KiB，也不能称作完整 runtime read。

```sh
"$XBOT_BIN" skill read ship --self --json
"$XBOT_BIN" skill write ship --description '发布检查' --title 发布 --body-file body.md --scope self --self --json
```

## 两形状与决定

A 为 CLI 离线 import 再逐 Bot 设置，状态和错误会散在多次调用中，正文与默认开关可能由不同竞争写者决定，淘汰。采用 B：原 source endpoint 绑定两个 typed 具名 read/write handler，CLI 只处理 argv/body 与有界输出，不导入 Kernel、不开新 Kernel/会话/任务/客户端框架。具体 DTO、路由、锁位与验收落在同份 [母 dev 的 skills 切片](../dev/cli-platform-entry.md)。

- source 捕获固定 Root、配置 SkillsDir、原动态 Redact 函数及 Resources ref；实际当前 Bot/disabled/self 与产物工作区通过现 WithCurrentIO 取。read 使用配置 SkillsDir、write 写 skill.Dir(root) 的现有差异保留。--self 定位 source，不决定 scope。
- scope 缺省/空为所有本地 Bot，self 为唯一保留字，其它值 EqualFold 匹配真实 Bot 名（包括 global、不同 owner/shared/orphan）。Slug/Name 而非 Title 用于 read 识别；手动开关仍权威。先完整校验与范围预检，库锁内解析当刻目标，再写文件与逐 Bot fresh config；最后一个完整库锁赢家决定正文及默认开关。真实局部失败如实回 Written/UpdatedBots/error，非0，不称跨 Bot 事务。
- 锁顺序具体为 source route → 现 skills/.write.lock → 各 Bot config.json.lock；锁内只有本地领域 IO，不反取 host/Jobs/exec，不做模型/网络/发布。既有 ImportData/Remove/Migrate 保同库锁，Remove 的 ForgetSkill 保留。不新建全 Bot 注册或统一准入。
- write 必需显式 body（空串可用），完整最终序列化 JSON 最多1MiB、超限拒不截；read 以同 parse bounded 打开，原始 SKILL.md 最多现 ResultMaxBytes20MiB/max+1。小结果按完整 JSON64KiB预算内联，大正文存现 results 产物并回 preview/file/access/local/bytes；预算错误不伪称全文。routine 保原引用和16KiB投影，历史 transcript 不改。
- 同 cut 退两模型工具/schema、旧执行 wrapper 和相关指引；保其它 Tools 门及 routine/read 历史脱敏。research 复用现 DirectXBotArgs 与共享纯 ParseSkillRuntime，只放 self read/help，不将 write 或离线命令全族放行。读写原 source guard/root 校验及生命周期不改。

## 接受的代价与范围

接受全库写锁覆盖文件与多个 config 的短 IO，换取原正文/默认范围同赢家；接受真实 IO 失败留下可观察 partial，换取不伪造跨 Bot 原子事务。接受完整 CLI read 与 routine16KiB投影两种不同消费预算，换取不暗改例程行为。离线 show/import/remove/enable/disable 保现语义；不新增人用 owner HTTP、自动 persona/role/routine 修改或下一平台族框架。

本决定不修改 desktop 的0048或 profile 的0049；不替代0021环境流、0043实例协议、0046资源所有权、0047MCP或0049改名。状态在窄结构核通过后转 Accepted，再按该单一 phase contract 实现。
