---
kind: adr
status: Accepted
date: 2026-11-01
---
# 0041 产品保险库是整机单库

## 背景

政东否决 6bd6c31f 的 per-bot 保险库。设置、连接应用 NeedsSecret、出站注入、本机 sudo、CLI 必须共享一份真源。设计交接为本机 `xbot-ui-design/settings-vault/handoff.md`（2026-11-01）；用户另确认顶区纯「添加」，无下拉。

## 决定

- 唯一产品装配入口 `vault.OpenGlobal(root)`，不接收 Bot 名；本地密文 `<root>/vault/_product_vault/secrets.enc`，密钥 `<root>/keys/vault/_product_vault.key`。vaultd 使用保留产品 namespace `_product_vault`。OAuth `_provider_accounts` 保持独立，低层 OpenBackend 留给独立 namespace，不用于产品分桶。
- 首次打开在目标原有写锁内合并旧 Bot 密文及产品 secrets.json。先验证全部条目，再一次提交目标；成功后才删源密文/密钥/旧明文。非空同名值不同，或值相同而域名不同，均显式失败，不扩张域名权限、不静默覆盖。相同条目可重入；空值让位于非空。冲突报名称，不报值；修正旧条目后重试。
- HTTP 仅 `/api/secrets` 与 `/api/secrets/{secret}`，沿现有认证/同源检查，无 Bot 所有权前置；响应使用 metadata DTO，永不含 Value。CLI 暂留 Bot 参数校验兼容，但不参与分桶；migrate 将同一个本地产品库搬至 vaultd；远端无法比较明文，所以任何同名条目都在写入前显式拒绝，保留本地源。daemon 的迁移错误由 OpenGlobal 传播，不让旧 List 无错误签名把冲突吞成空列表。
- NeedsSecret 的 MCP 环境键也存同库，本地进程从此库读取；不继续以 Bot 明文文件当回退。旧 Telegram/飞书变量名仅在迁移时依现有映射转成 connector vault 名。连接器授权仍独立于密钥共享。
- 本机 askpass 沿用 ADR 0040 的专用出口、固定 LOCAL_SUDO、脱敏与进程生命周期；路径改为全局。vaultd 不新增明文出口，本机 askpass/MCP 环境注入不绕过配置的 daemon。
- 设置切 Bot 不重挂、不更换列表、不丢草稿；添加及删除说明所有 Bot 共用。仅保留添加卡内 sudo 快捷条和空态次钮。

## 对照回顾

用户六项验收逐项落实到以上入口、API、UI、迁移、CLI、行为测试。复用 atlas security 的 Backend/写锁/DTO/askpass，ecosystem 的连接器名字及 MCP Secret lookup，ui 的现有卡行和 nestcard；不新建工具、权限或状态账本。与 ADR 0005 的 daemon 不返回明文、0013 的不扩安全机制、0021 的不增状态机制相容。ADR 0040 的出口及边界不变，仅覆盖其 per-bot 作用域决定。guard 界面与六条旅程保持原契约；候选清单不加新功能。设计师新稿作为已给定第三方设计参考，不派新 Agent。

## 后果

删除或改名 Bot 不会删除全局密钥；删除仍带旧桶的 Bot 前先完成本地合并，冲突则中止删除。不同 Bot 过去使用不同同名凭证时需先解决冲突再启动；不会悄悄择一。部署 vaultd 的本地源迁入继续用显式 `secret migrate`，daemon 内旧 Bot 桶在首次访问产品 namespace 时合并。
