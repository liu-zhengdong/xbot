# Shell 按解释器拆工具

候选真源：主人 Obsidian `正在干什么/06-XBot/候选/Shell 按解释器拆工具.md`。Ultra 2026-10-27 拍板排期；执行官开干净 wt 编码。

## 对照回顾

现状：`internal/tools.Shell` 名固定 `shell`，Unix 走 `sh -c`。模型容易把 PowerShell 语法写进 cmd、把 bashism 丢给 `/bin/sh`。政东要按解释器拆成独立工具，注入时带上壳名与版本。

## 实现规格与怎样算对

| 项 | 规格 / 验收 |
|---|---|
| 工具面 | **不再暴露**泛用 `shell`；按平台拆独立工具，有则注入。 |
| Windows | `powershell` · `cmd` · `git-bash`（检测到 Git Bash 才挂）；注入名+版本。 |
| macOS | 至少 `zsh` / `bash`（按本机默认与实装；有则挂）。 |
| Linux | 至少 `bash` / `sh`；常见再挂 `zsh`（有则挂）。 |
| 版本 | 探测失败：仍挂工具但标「版本未知」（勿静默隐藏已存在的壳）。 |
| 参数 | 每个壳工具带 **`description`**：一句话说明命令目的/用途（轨迹/审计），不是命令复述。 |
| 旧名 | 模型侧与注入表里旧泛 `shell` **不可达**（测试断言工具列表无 `shell`）。 |
| 轨迹/审批 | 确认卡、review、notable、Jobs 等对旧 `shell` 的特判要跟到新工具名，勿漏审。 |

样例正：本机有 zsh+bash → 工具列表含二者、无 `shell`；描述含版本或「版本未知」+ description 参数。
样例反：继续挂泛 `shell`；未装 Git Bash 却挂 `git-bash`；缺 description。

## 非目标

不合并 [[常驻终端 PTY]]；不改无关文件；不 push；不提手测。相关包 `go test` 一遍绿（不要 `-count=3`）。

## 还没做的

独立复审、合入 main、8788、docs/dev 毕业。
