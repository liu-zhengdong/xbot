# XBot 桌面版安装（简版）

改动见发布说明。完整说明见仓库 docs/desktop-install.md。安装包没有 Apple / 微软开发者签名，第一次打开要放行一次。关窗口就退出；桌面版不需要也没有「退出登录」。

| 系统 | 文件 |
|---|---|
| macOS 13+（Apple 芯片） | `XBot-*-macos-arm64.dmg`（或 .zip） |
| macOS 13+（Intel） | `XBot-*-macos-amd64.dmg`（或 .zip） |
| Windows 10/11 | `XBot-*-windows-amd64.zip`（ARM 电脑用 arm64） |
| Linux | `xbot-*-linux-amd64.tar.gz`（`xbot app` 需要 WebKitGTK 4.1） |

校验：`sha256sum -c SHA256SUMS`（macOS 用 `shasum -a 256 -c SHA256SUMS`）。

## 先放好模型密钥
双击启动时拿不到终端里的环境变量。在 `~/.xbot/env`（Windows：`%USERPROFILE%\.xbot\env`）写一行：
```
XBOT_API_KEY=你的密钥
```

## macOS（Gatekeeper）
1. 打开 .dmg，把 XBot 拖进「应用程序」。
2. 第一次打开：
   - macOS 15 Sequoia 及以上：双击 → 提示时点「完成」→「系统设置 → 隐私与安全性」→ 找到「已阻止打开 XBot」点「仍要打开」→ 输入密码 →「打开」。
   - macOS 13–14：在「应用程序」里按住 Control 点 XBot（或右键）→「打开」→「打开」。
3. 提示「已损坏」或没有「仍要打开」按钮时，在终端执行后再打开：
   `xattr -dr com.apple.quarantine /Applications/XBot.app`

## Windows（SmartScreen）
1. 右键 zip →「属性」→ 勾「解除锁定」→「确定」，再「全部解压缩」到固定位置（如 `文档\XBot`）。
2. 双击 `XBot.exe`；出现「Windows 已保护你的电脑」时点「更多信息」→「仍要运行」。
3. 打不开窗口：安装微软「WebView2 Runtime（常青版）」（Windows 11 自带）。
`xbot-cli.exe` 是命令行版：`.\xbot-cli.exe serve`。

## 打不开时
看 `~/.xbot/app.log`（Windows：`%USERPROFILE%\.xbot\app.log`）。常见原因：已经开着一个 XBot；同一个数据目录（`~/.xbot`）已经有终端里的 `xbot serve` 在跑（一个数据目录只能开一个，先停掉它）；或 8787 端口被占（可在命令行用 `xbot app --port 8788`）。

## 为什么要放行
安装包暂时没有 Apple 开发者签名和公证，也没有微软代码签名，所以 macOS 的 Gatekeeper 和 Windows 的 SmartScreen 第一次会拦一下。按上面的步骤放行一次即可，之后正常打开。下载后可以先用 SHA256SUMS 校验文件没有被改动。
