---
kind: part
parent: README.md
code: [internal/version/, internal/upgrade/, scripts/release.sh]
verified: 2026-10-17 @ 0418c26
---
# 版本与自动升级

> **30 秒版**：`xbot version` 打印编译时注入的版本；`scripts/release.sh vX.Y.Z` 测绿、交叉编译、打 tag、发 GitHub Release；`xbot serve` 每 6 小时查一次新版本，校验 SHA256 后原地换二进制并优雅重起；`xbot upgrade [--check]` 手动升级。

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
`scripts/release.sh VERSION`（必须是 `vX.Y.Z`）：

1. 拒绝：工作区不干净、tag 已存在、版本号不合规。
2. 跑 `go test -count=1 ./...`，不绿就停。
3. `CGO_ENABLED=0` 交叉编译 linux/amd64、linux/arm64、darwin/arm64 到 `dist/VERSION/xbot_<os>_<arch>`，写 `SHA256SUMS`（sha256sum 格式）。
4. `git tag -a`；tag 直接推到 GitHub 仓库 `liu-zhengdong/xbot` 的地址，不经本地的 `origin` 远程；再 `gh release create`。token 用 `gh auth token -u liu-zhengdong` 取，只走环境变量和一次性的推送地址，输出里的 token 换成 `***`，不打印。
5. `DRY_RUN=1` 只做前 3 步。

`dist/` 在 `.gitignore` 里。

## 检查与应用
`internal/upgrade`：

- `Check(ctx, current)` 请求 `https://api.github.com/repos/liu-zhengdong/xbot/releases/latest`，地址可用环境变量 `XBOT_UPGRADE_URL` 覆盖（测试用 `httptest`）。按 semver 比较：比当前新才 `Available`，旧的是 `Downgrade`，永不降级。
- `Apply` 只取运行时 `GOOS/GOARCH` 对应的资产和 `SHA256SUMS`，校验 SHA256；不匹配或资产缺失直接拒绝，原二进制一个字节都不动。
- 下载到 `os.Executable()` 所在目录的临时文件（解析符号链接，保证同文件系统），`chmod 0755` 后 `os.Rename` 原子替换。单个下载上限 200 MiB。

## 命令行与 serve
- `xbot version` 打印版本。
- `xbot upgrade [--check]` 打印当前/最新，较新就应用。
- `xbot serve` 默认开启周期检查：启动约 3 分钟后第一次，之后每 6 小时。`--no-auto-upgrade` 或 `XBOT_AUTO_UPGRADE=0` 关闭。
- 应用成功后：`web.Server.Drain` 停止接收新的前台轮，等在飞的跑完（上限 2 分钟），关掉监听，`syscall.Exec(同一路径, 同一参数, 同一环境)` 重起。后台任务不等待，交给既有的 `kernel.RecoverInterrupted` 恢复路径。
- `SIGHUP` 触发同一套优雅重起，方便手动验证。
