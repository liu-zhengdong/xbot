# 设置保险库与本机 sudo

## 范围与怎样算对（L：跨界面、密钥、Shell）

按用户本次确认与新定稿 `settings-vault/handoff.md`：设置左栏「保险库」在权限后网页搜索前；整机一份、所有 Bot 共用的密钥用全部/出站/本机计数 chip、搜索、圆角卡行和「管理」显示；没有「当前 Bot」，切 Bot 不换列表。管理可改域名、重新设置值、经 D5 全局语气确认删除。主钮纯「添加」直接开 nestcard，不带箭头/菜单；本机 sudo 只从卡内快捷条和空态次钮进入，LOCAL_SUDO 预填，域名空。空态、筛选无结果与搜索无结果分别呈现；旧安静行皮作废。

值不在列表、HTTP 响应、模型、环境明文、transcript 或工具结果中出现。空域名绝不能被 HTTP proxy 注入；CLI 可省略 --domains。LOCAL_SUDO 走 SUDO_ASKPASS，非交互 `sudo -A` 可使用；删除后新的 askpass 调用失败。

## 设计切口

- Store.Set 允许空域，复用 HostAllowed 拒绝出站；非空非法域名不变成本机绑定。
- 唯一产品入口 OpenGlobal(root)，连接应用全部 NeedsSecret、CLI、出站与 askpass 都用它；OAuth namespace 不动。全局 /api/secrets CRUD 响应只给 name/domains。POST 有 value 是添加/重设，省略 value 是 SetDomains。迁移先校验全部旧桶，值或绑定冲突失败且保留源，成功一次提交再删源；具体决定见 ADR 0041。
- Shell.Run 的进程环境装配点生成临时 0700 助手，专用内部入口只读取固定的 LOCAL_SUDO；主线与作用域 Shell 共用绑定。助手存活至实际进程结束，复用脱敏注册；注册身份变化使缓存失效。
- 本机 sudo 仅本地 Store；配置 vaultd 不绕开后端，不增加明文接口。决定见 [ADR 0040](../adr/0040-local-sudo-askpass.md)。
- UI 复用设置壳、nestcard、危险删除。离页作废旧响应并清空输入；切 Bot 不重挂全局设置，不轮询重画搜索和草稿。权限卡并行功能不碰。

## 对照回顾

| 对照 | 结论 |
|---|---|
| 用户全量要求与新 handoff / Grok 参考 | 用户六项验收均在范围；全局单库；主钮纯添加；全部/出站/本机，不照搬登录/支付/联系 |
| atlas security / ADR 0005 | 同一 backend；vaultd 不返回明文；本地后备不声称隔离 |
| atlas ecosystem / ADR 0031 | 连接器及 MCP NeedsSecret 全部接同一全局 vault，不引入第二真源；供应商/搜索密钥不迁库 |
| atlas kernel / ADR 0021 | 无新状态账本、模型轮或通知机制；只绑定现有 Shell |
| atlas ui / guard 界面 | 采用已定稿浅深稿；专属色块走 token；不改守护层，不安排合入后逛验收 |
| 并行权限卡 | 仅插导航与 vault mount，不改权限分支 |

**冒烟用例**：`e2e/smoke/vault_settings_sudo_test.go`，浏览器辅助 `internal/web/testdata/vault-settings.cjs`；缝上回归在 `internal/vault/{global,local_sudo,domains}_test.go`、`internal/web/{secrets,vault_browser}_test.go`、`internal/tools/sudo_askpass_test.go`、`internal/kernel/local_sudo_test.go`、`cmd/xbot/{local_sudo,secret,secret_migrate}_test.go`；`internal/bot/vaultkey_test.go` 守全局库不随 Bot 删除/改名及删旧 Bot 前迁移。

验证结果与性能只记 [MILESTONES](../MILESTONES.md)。

## 还没做的

- 用户日用尚未验证；本条不自行移入 guard。
- 主干合入由主管安排；本分支不合 main、不 push。
