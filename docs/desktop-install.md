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

## 电脑操作（Mac / Windows）

电脑操作是自带能力，不需要去连接应用商店添加。首次使用需安装官方组件（Node.js 18+）：

```sh
npm install -g agent-desktop
agent-desktop status
```

已安装在其它位置，可在启动 XBot 的环境里设置 `XBOT_AGENT_DESKTOP` 为可执行文件的完整路径；未设置时从 PATH 查找。图形启动的 XBot 若找不到终端里安装的组件，可通过数据目录 `env` 配置这个路径，再重启 XBot。当前适配实探版本为 agent-desktop 1.0.2 / JSON 协议 2.5，不捆绑或自动下载它。Windows 的 npm `.cmd` 入口会按上游全局/本地安装布局解析到原生 `.exe`，不经过 cmd.exe；非标准布局请将 `XBOT_AGENT_DESKTOP` 直接指向原生 `.exe`。

- Mac：在「系统设置 → 隐私与安全性 → 辅助功能」允许启动 XBot 的应用（部分系统另列 agent-desktop，也需允许）；macOS 27+ 可能显示为设备控制与数据访问。截图另需「屏幕录制」，读取树和语义动作不需要屏幕录制。授权后在 XBot「设置 → 设备 → 这台电脑」点「重新检查」。也可手动运行 `agent-desktop permissions --guide` 打开官方引导。
- Windows：在登录用户的交互桌面运行；不能控制权限更高的进程、锁屏、UAC 安全桌面或另一用户的会话。被完整性等级拦住时，让 XBot 和目标 App 以相同权限运行，不以管理员身份默认启动。Windows type 是物理输入，set-value 是语义替换。

先 `xbot desktop snapshot "App 名称"` 拿到 `@snapshot:eN` 形式的引用，再 `xbot desktop click "@实际引用"` 或 `xbot desktop type "@实际引用" "文字"`，最后重新 snapshot 核验。引用不是永久编号，过期就重新取；失败的投递状态不确定时先观察，别直接重复操作。Mac / Windows 不把坐标点击当作自动降级；Linux 原有像素路径保留。

## 常见问题

- 什么都没发生：可能已经开着一个 XBot（同一个数据目录只能开一个），或者 8787 端口被别的程序占了。原因写在数据目录的 `app.log` 里（`~/.xbot/app.log`，Windows 是 `%USERPROFILE%\.xbot\app.log`）；也可以在命令行运行 `xbot app --port 8788` 直接看。
- 升级：Windows 直接运行新版 setup.exe；macOS 退出 XBot 后把新版拖进「应用程序」替换。数据在 `~/.xbot`，不受影响；卸载也不会删它。
