---
kind: part
parent: README.md
code: [internal/version/, internal/upgrade/, scripts/release.sh, internal/web/update.go, scripts/xbot-dev]
verified: 2026-10-26 @ c91e7dd + 开发构建 tip 同步
---
# 版本与自动升级

> **30 秒版**：`xbot version` 打印编译时注入的版本；推一个 `v*` tag，release.yml 就打包并发 Release；`xbot serve` 和桌面版每 6 小时查一次发布列表，有新版就后台下载校验好，网页右上角出现「更新」，点一下装好并重启；`xbot upgrade [--check]` 在命令行走同一条路径。

`scripts/xbot-dev` 每次成功构建后，把编译版本所用的同一短 HEAD 写入 `build-info` 和 `bin/xbot.tip`；编译失败不更新版本记录。构建与重启分开，运行实例的版本在重启后才与新构建一致。

## 边界
负责：版本号的注入与展示、发布流程、运行中检查与应用升级、优雅重启。
不负责：系统服务托管（systemd/launchd 各平台自理）、回滚（重新下载旧 tag 即可）。

## 版本
`internal/version` 只有一个变量：

```go
var Version = "dev"
```

发布脚本用 `-ldflags "-X xbot/internal/version.Version=v0.1.0"` 注入。未注入时是 `dev`，`dev` 构建**不会**自动升级（避免开发机上跑测试版把自己换掉），只有手动 `xbot upgrade` 强制检查。

## 发布
**推 tag 就是发布**。`.github/workflows/release.yml` 在 `push: tags: ["v*"]` 时自己打包并
`gh release create --prerelease`：macOS 的 dmg 和 zip、Windows 的 setup.exe 和 zip、
Linux 的 tar.gz（都带版本号，如 `XBot-0.3.1-beta.6-macos-arm64.zip`），外加
`SHA256SUMS` 和 `INSTALL.md`。发布说明取自 `packaging/release-notes/<tag>.md`，所以先提交
那个文件再打 tag。

`scripts/release.sh` 是另一条路，留着做本地预演：它跑测试、交叉编译、打 tag、发
Release，但产物是更早的 `xbot_<os>_<arch>` 裸二进制，和 release.yml 的桌面包不是一套。
自动更新只认 release.yml 的包，所以**正式发版不要用它**。

## 检查与应用
`internal/upgrade` 是整条更新链：

- **查**：`Check` 请求 `https://api.github.com/repos/liu-zhengdong/xbot/releases?per_page=30`，
  地址可用 `XBOT_UPGRADE_URL` 覆盖（测试用 `httptest`）。从返回的列表里取**最高的 semver**，
  不按返回顺序——GitHub 按创建时间排，晚打的补丁 tag 会排在 beta 前面。草稿和非 semver tag
  跳过。**不用 `/releases/latest`**：这个接口从不返回预发布，而 xbot 至今只发预发布，用它
  永远「已是最新」。预发布之间按 semver 比较，`beta.10` 高于 `beta.6`。
- **挑包**：`AssetName` 按运行时平台拼资产名。macOS 取 `XBot-<v>-macos-<arch>.zip`（用 zip
  不用 dmg：解包不需要 hdiutil）；Linux 取 `xbot-<v>-linux-amd64.tar.gz`；Windows 返回空，
  那条路走 `DownloadAsset`，页面给的是 `setup.exe` 的下载而不是自动安装。
- **暂存**：`Stage` 下载到 `<root>/update/<版本>/`，先比对 `SHA256SUMS` 里的 SHA256，不匹配
  就整个删掉、不动任何已安装的东西；通过后解包，解包时挡住 `../` 和绝对路径。只留一份暂存包，
  新的来了先把旧的清掉。上限 200 MiB。
- **安装**：`Install` 在 macOS 上换的是**整个 `.app`**——旧的改名让位、新的改名就位、删掉旧的，
  第二步失败就把旧的放回原处，放在 `/Applications` 之外的就地对换在同一个卷上完成；Linux 写
  覆盖运行中的二进制。
- **状态**：`Updater` 把状态机摊给网页（`idle`/`checking`/`downloading`/`ready`/`error`/
  `manual`），并把暂存包记在 `manifest.json` 里，重启后 `NewUpdater` 直接认出来是 `ready`，
  不用重下。比当前版本旧的暂存包在启动时清掉。

## 界面与命令行
- 网页顶栏的「更新」按钮（`#update`）只在 `ready` 和 `manual` 时出现，下载期间不打扰；点一下
  `POST /api/update/install`，安装完重启，页面轮询 `/api/session` 等新进程回来再刷新。
  Windows 那枚点下去是打开安装包下载。
- `GET /api/update` 给状态，`POST /api/update/check` 立刻查一次。三个接口都要登录。
- `xbot upgrade [--check]` 在命令行走同一条 `Stage` + `Install`；`xbot version` 打印版本。
- 周期检查：`xbot serve` 和桌面版都是启动 30 秒后第一次，之后每 6 小时。`--no-auto-upgrade`
  或 `XBOT_AUTO_UPGRADE=0` 关闭。桌面版也查——它以前被显式关掉了。
- 装完的重启：桌面版用 `appshell.Relaunch`（MyGo 重开窗口）；serve 走下面这套优雅重起。

## 优雅重起
`web.Server.Drain` 停止接收新的前台轮，等在飞的跑完（上限 2 分钟），关掉监听，
`syscall.Exec(同一路径, 同一参数, 同一环境)` 重起。后台任务不等待，交给既有的
`kernel.RecoverInterrupted` 恢复路径。`SIGHUP` 触发同一套流程，方便手动验证。
