# 安装和打开 XBot 桌面版

桌面版就是 `xbot app`：同一个程序在本机跑 serve，再开一个系统 WebView 窗口，自动登录。数据在 `~/.xbot`（Windows 是 `%USERPROFILE%\.xbot`），和命令行版共用。关窗口就退出。

安装包没有 Apple 或微软的开发者签名，第一次打开系统会拦一下，按下面做一次就好。

## 模型密钥

第一次打开桌面版会先出现「先填模型密钥」：粘贴 API key（默认 DeepSeek），点「测试并保存」。XBot 先用它向模型服务（`XBOT_BASE_URL`，默认 api.deepseek.com）列一次模型，通过后写进数据目录里的 `env` 文件（权限 600），不会设置系统或用户环境变量，也不用重启。

以前的做法照常可用：数据目录里的 `env` 文件（一行一个 `名字=值`，可以加引号，`#` 开头的行是注释），macOS / Linux 是 `~/.xbot/env`，Windows 是 `%USERPROFILE%\.xbot\env`。已经在环境里的变量不会被覆盖。已有密钥或配置了 providers.json 时不会出现这个画面。

## macOS（13 Ventura 及以上）

选和芯片对应的包：Apple 芯片（M1 及以后）用 `macos-arm64`，Intel 用 `macos-amd64`。

1. 双击 `.dmg`，在打开的窗口里把 XBot 拖到旁边的「应用程序」。（`.zip` 也一样：解压后把 XBot.app 拖进「应用程序」。）
2. 第一次打开：
   - macOS 15 Sequoia 及以上：双击 XBot，提示「无法验证开发者」时点「完成」；然后打开「系统设置 → 隐私与安全性」，往下找到「已阻止打开 XBot」，点「仍要打开」，输入密码后再点「打开」。
   - macOS 14 及以前：在「应用程序」里按住 Control 点 XBot（或右键），选「打开」，再点「打开」。
3. 如果提示「已损坏，无法打开」或上面的方法不出现按钮，在终端里去掉下载隔离标记后再打开：

   ```
   xattr -dr com.apple.quarantine /Applications/XBot.app
   ```

之后双击就能直接打开。命令行用法不变：`/Applications/XBot.app/Contents/MacOS/XBot serve` 等。

## Windows 10 / 11

选 `windows-amd64-setup.exe`（大多数电脑）或 `windows-arm64-setup.exe`（骁龙等 ARM 电脑）。

1. 双击 setup.exe。出现蓝色的「Windows 已保护你的电脑」（SmartScreen）时，点「更多信息」→「仍要运行」。只需一次。
2. 点「安装」。按用户安装到 `%LOCALAPPDATA%\Programs\XBot`，不需要管理员权限；开始菜单里有 XBot，桌面快捷方式可选；在「设置 → 应用」里能卸载。
3. 窗口需要 Microsoft Edge WebView2 运行时。Windows 11 自带；没有时安装程序会提示并下载微软的安装器。

安装和卸载都不读写 PATH 或任何环境变量（CI 每次发布前在 Windows 上装、升级、卸载一遍并核对）。命令行版 `xbot-cli.exe` 在安装目录里：`& "$env:LOCALAPPDATA\Programs\XBot\xbot-cli.exe" serve`。

不想安装：`windows-*.zip` 是便携版。右键 zip →「属性」→ 勾「解除锁定」，解压到固定位置后双击 `XBot.exe`。

## Linux

`xbot-*-linux-amd64.tar.gz` 里是命令行程序。`xbot app` 需要 WebKitGTK 4.1（Debian/Ubuntu：`sudo apt install libwebkit2gtk-4.1-0`）。

## 常见问题

- 什么都没发生：可能已经开着一个 XBot（同一个数据目录只能开一个），或者 8787 端口被别的程序占了。原因写在数据目录的 `app.log` 里（`~/.xbot/app.log`，Windows 是 `%USERPROFILE%\.xbot\app.log`）；也可以在命令行运行 `xbot app --port 8788` 直接看。
- 升级：Windows 直接运行新版 setup.exe；macOS 退出 XBot 后把新版拖进「应用程序」替换。数据在 `~/.xbot`，不受影响；卸载也不会删它。
