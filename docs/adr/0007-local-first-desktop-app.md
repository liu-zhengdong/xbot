---
kind: adr
guard: owner
status: Accepted
date: 2026-10-17
---
# 0007 本地优先的桌面端：系统 WebView 外壳收在 internal/appshell

## 背景
用户要在自己的 Windows 和 Mac 上直接用 XBot。XBot 装在当前这台电脑上，`serve` 跟着桌面端在本机跑，所有 Bot 数据都在本机；窗口显示的就是本机 `serve` 的现有 Web 界面。XBot 目前只在 Linux 上测过：Windows 交叉编译不过（`flock`、`kill`、进程组），mac 能编译，但有几处读 `/proc` 的退化。

## 决定
- **本地优先**：桌面端就是同一个 `xbot` 二进制加 `xbot app` 子命令。`serve` 在同一进程里启动，只听 `127.0.0.1` 的固定端口（默认 8787），窗口指向它。数据根仍是 `~/.xbot`。不需要远程隧道。
- **外壳**：用系统 WebView（Windows WebView2、mac WKWebView、Linux WebKitGTK），不用 cgo。首选 MyGo（`github.com/egoist/mygo`，MIT，锁版本），它用 purego 在运行时加载系统库，`CGO_ENABLED=0` 就能从 Linux 交叉编译三个平台。外壳代码全部收在 `internal/appshell`，只暴露打开窗口、原生通知、关闭和退出几个接口，页面不调用外壳的 API，所以随时能换框架；备选 Wails v3。不叫 `internal/desktop`，那是已有的桌面操控工具。
- **登录**：桌面进程直接把会话交给内嵌的 `serve`（一次性启动码换 cookie），用户不粘贴 token。token 只在本地进程里，不进日志。
- **关窗即退出**：不做托盘和开机自启。关掉窗口，`serve` 和定时任务一起停；下次启动时，每个 Bot 收到一条汇总通知，列出停机期间错过的时间点，由 Bot 自己决定要不要做，不机械补跑。
- **cron 按本机时区**：例程的 cron 字段按电脑的本地时区解释，不再按 UTC；已有的例程在升级时迁移，触发时刻不变。
- **Windows 的 shell 用 Git Bash**：Windows 上要求装 Git for Windows，shell 工具继续是 `bash -c` 语义，权限表按 POSIX 命令名判断删除和只读的规则不变。PowerShell 以后再说，届时同时扩充权限表。
- **打包与更新**：不签名。mac 的 `.app` 和 dmg 在 GitHub Actions 的 macOS runner 上打；Windows 安装器用 makensis、Linux `.deb` 用 nfpm，在 ubuntu runner 上打。自更新沿用 `internal/upgrade` 的 SHA256SUMS 校验，后台预下载，点「重启以更新」生效。
- **验收先过 UX**：桌面端做出来后，先由 UX 设计同事在浏览器里走一遍，再交用户实际使用。

## 考虑过的方案
- **桌面端连远程 `serve`**：数据不在用户自己的电脑上，和本地优先相反。
- **Electron**：安装包上百 MB。
- **Tauri**：要引入 Rust 工具链。
- **Wails v3**：还在 beta，要 cgo 和各平台原生编译机，留作备选。
- **Windows 用 PowerShell 作 shell**：`Remove-Item`、`del` 不在权限表的删除名单里，会绕过删除确认。

## 后果
- 内核要先补齐跨平台：文件锁、进程树控制、目录 fsync、保险箱密钥权限检查、大小写不敏感文件系统、CRLF、重启和升级。见 [里程碑](../MILESTONES.md) 的「本地优先桌面端」。
- 电脑关机或合盖时定时任务不跑，靠下次启动时的汇总通知兜底。
- MyGo 要求 Go ≥ 1.27.1，`go.mod` 要随之升级。
- 外壳依赖新、维护者少，所以只放在一个包里，换框架时改动不出 `internal/appshell`。
