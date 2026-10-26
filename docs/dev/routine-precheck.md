# 例程免模型前置检查

真源：Obsidian `候选/例程免模型前置检查.md`（Ultra 方案 2026-10-27）。

## 背景

自查类例程多数时候没事，每次到点整轮进模型费 token。要可选 shell 前置检查：退出码 0 才叫醒。

## 要干啥

1. `internal/link.Routine` 增加可选 `precheck`（JSON `precheck,omitempty`）。
2. Timer 到点、提交 slot **之前或同时**：若 `precheck` 非空，先短超时跑 shell；非 0 则**不 emit** routine 事件，但仍提交 slot（防同分钟轰炸）；0 则照旧 emit。
3. `routine_create` / `routine_update`（及 CLI/API 对等路径）可读写该字段；空/省略=旧行为。
4. 单测：缺字段、exit 0、exit 非 0（可用短命令 `true`/`false` 或可控 stub）。

## 怎样算对

- 非 0：无模型叫醒、slot 已记。
- 0 / 无字段：与现网一致。
- 测试绿；合入本地 main；起 8788。无新 UI 控件则不喊设计师。

## 底线

- 复用现有 shell 执行，不新造例程对象。
- precheck 失败默认静默，不 `notify_owner`。
- 不碰网关/跨设备/环境实体。
- 不 push，除非另令。

## 本批边界

- `precheck` 按 shell 命令字符串实施；口头样例的布尔 `true/false` 语义不采用。`"true"` / `"false"` 是命令，JSON 布尔值拒绝。
- 本批交付仅功能分支提交、测试与旁路报告；不合入 main、不起 8788、不 push。上面的最终验收标准不变，合入和启动验收由父代理继续。

## P1 审核边界

- 含非空前置命令的创建/修改复用现有 shell 审核；不得只按 routine 排期分类放行。实际执行的机器策略变为 ask/off 时无交互跳过且记 slot。
- 本轮仅关闭审核绕过；P2 的多检查串行占住 chat 与取消不传不在回修范围。

## 还没做的

P1 修复独立复审；P2 多检查串行等待与生命周期取消传递待后续处理。合入 main、8788 真入口验收、主人日用确认；因此暂留本设计，不迁 guard。当前实现约定见 [多 Bot](../atlas/multibot.md)，本批验证见 [里程碑](../MILESTONES.md)。
