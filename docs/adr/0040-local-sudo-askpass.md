---
kind: adr
status: Accepted
date: 2026-10-31
---
# 0040 本机 sudo 经专用 askpass 出口

## 背景

用户确认在当前 Bot 的设置「保险库」管理密钥，并让本机 Shell 使用 sudo。密码不得进模型、环境变量明文、transcript 或工具结果。独立用户 vaultd 仍遵守 [ADR 0005](0005-vault-daemon-user.md) 的不返回明文约束。

## 决定

- 空 domains 表示本机用途；沿现有 HostAllowed 拒绝任何 HTTP 注入，不把空名单解释成任意域名。非空但全部非法的域名仍拒绝写入。
- 设置、CLI、连接器使用同一 Bot 的 OpenBackend。HTTP 响应只用 name/domains DTO；修改域名用 SetDomains，在原写锁内保留密钥，不读取旧值到前端。
- 主线与子代理的 Shell 复用同一绑定；在 Shell.Run 进程环境装配处准备 SUDO_ASKPASS。助手只含可执行文件与当前 Bot vault 路径，不含密码；专用内部入口现读固定的 LOCAL_SUDO，只向 sudo 的 askpass stdout 输出。
- 助手 0700，实际进程结束即清理，转后台后不提前删除。密码沿既有脱敏注册处理；注册身份变化也使脱敏形式缓存失效，不能只比较注册数量。
- 本条仅支持本地同用户 Store。配置 vaultd 时不绕开后端、不新增明文读取协议。没有新模型工具，不改变权限分类、确认卡或 sudoers。

## 后果

用户在命令中使用 `sudo -A`；删掉 LOCAL_SUDO 后新调用（包括仍留着的助手）不能再取密码。系统已有的 sudo 认证缓存、NOPASSWD 与管理员策略不由 XBot 撤销。同用户后备不是真正隔离；它仍有 ADR 0005 已说明的同用户可读边界。
