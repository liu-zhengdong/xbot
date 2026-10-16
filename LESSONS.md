# 教训

格式：一行规则；原因；证据（提交或问题）；出现次数「Seen N×」。同一条在 3 个不同日子出现，就升级进 [AGENTS.md](AGENTS.md) 的硬约束。

## 教训
- **检查 shell 命令要看命令链里的每一段，强制推送算破坏性操作。** 原因：`git push --force` 不弹确认；命令里第一个 git 不是 clean/push 时，后面的就漏查（`git add . && git push -f`）。证据：`2617bc0`。Seen 1×（2026-10-16）
- **密钥不能和它加密的数据放在一起。** 原因：保险箱密钥放在 `bots/<name>/vault/key`，Bot 的 shell 读出来就能解密。证据：`42cbed0`，[ADR 0005](docs/adr/0005-vault-daemon-user.md)。Seen 1×（2026-10-16）
- **耐久文件不放 /tmp。** 原因：盒子重启清空了 /tmp，丢了 F5 的工作，也删掉了 pnpm 包仓库（dsh 跑不起来）。证据：2026-10-16 约 16:42 重启。Seen 1×（2026-10-16）
- **测试要和外部环境变量隔离（用 t.Setenv 清掉 XBOT_VAULT_SOCKET 等）。** 原因：shell 里留着 XBOT_VAULT_SOCKET，合并前 3 遍测试都挂在 TestSecretMigrateMovesSecretsToDaemon。证据：12602f5 之后的修复。Seen 1×（2026-10-16）
- **自我 re-exec 要把内存里持有的 key 放回新进程的环境。** 原因：v0.1.0 的 serve 升级重启后报 XBOT_API_KEY is empty（启动时 key 已从环境清掉）。证据：v0.1.1 的修复。Seen 1×（2026-10-16）

## 做对了
- 盒子回归用假模型和回显服务，真值只在服务端出现，transcript 和模型输入里 0 处（M7，2026-10-16）。
