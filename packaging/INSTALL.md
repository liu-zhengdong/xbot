# XBot 桌面版安装（简版）

改动见发布说明。完整说明见仓库 docs/desktop-install.md。关窗口就退出。

| 系统 | 下载 |
|---|---|
| Windows 10/11 | `XBot-*-windows-amd64-setup.exe`（骁龙等 ARM 电脑用 `arm64-setup.exe`） |
| macOS 13+（Apple 芯片） | `XBot-*-macos-arm64.dmg` |
| macOS 13+（Intel） | `XBot-*-macos-amd64.dmg` |
| Linux | `xbot-*-linux-amd64.tar.gz`（`xbot app` 需要 WebKitGTK 4.1） |

校验（可选）：`sha256sum -c SHA256SUMS`（macOS 用 `shasum -a 256 -c SHA256SUMS`）。

## 以后有新版本

XBot 自己会查（启动半分钟后一次，之后每 6 小时一次）。发现新版本就在后台下载好，窗口右上角出现「更新」：点一下，装好并重开窗口，数据不动。下载期间不打扰你。

一个例外是 Windows：安装程序要把正在运行的 XBot 换掉，所以那里点「更新」是下载新的 setup.exe，双击运行一遍就行（旧版本不用先卸，数据保留）。

## Windows
1. 双击 `setup.exe`。出现蓝色的「Windows 已保护你的电脑」时，点「更多信息」→「仍要运行」（安装包没有微软代码签名，只需一次）。
2. 按提示点「安装」。不需要管理员权限，装在 `%LOCALAPPDATA%\Programs\XBot`，开始菜单里有 XBot，桌面快捷方式可选。缺少 WebView2 运行库时安装程序会提示并帮你装上。
3. 升级：窗口里点「更新」下载新版 setup.exe 后双击运行，或直接下载新版运行；数据（`%USERPROFILE%\.xbot`）保留。卸载：「设置 → 应用 → 已安装的应用 → XBot → 卸载」，数据同样保留。

安装程序不改 PATH 或任何环境变量。命令行版在安装目录里：`& "$env:LOCALAPPDATA\Programs\XBot\xbot-cli.exe" serve`。不想安装可以用 `windows-*.zip` 便携版，解压后双击 `XBot.exe`。

## macOS：第一次打开时放行（Gatekeeper）
打开 .dmg，按窗口里的提示把 XBot 拖到「应用程序」。安装包没有 Apple 开发者签名和公证，第一次打开会被拦一下：
- macOS 15 Sequoia 及以上：双击 XBot，提示时点「完成」；打开「系统设置 → 隐私与安全性」，找到「已阻止打开 XBot」点「仍要打开」，输入密码后点「打开」。
- macOS 13–14：在「应用程序」里按住 Control 点 XBot（或右键）→「打开」→「打开」。
- 提示「已损坏」或没有「仍要打开」按钮：在终端执行 `xattr -dr com.apple.quarantine /Applications/XBot.app` 后再打开。

之后双击就能直接打开。

### 命令行版
XBot.app 里那个程序本身就是命令行版：带子命令就是命令行，不带就开窗口。装好之后直接用完整路径：

```sh
/Applications/XBot.app/Contents/MacOS/XBot version
/Applications/XBot.app/Contents/MacOS/XBot list
/Applications/XBot.app/Contents/MacOS/XBot serve
```

不想每次敲全路径，就在 `~/.local/bin` 里建一个链接：

```sh
mkdir -p ~/.local/bin
ln -sf /Applications/XBot.app/Contents/MacOS/XBot ~/.local/bin/xbot
```

然后确认 `echo $PATH` 输出里有 `~/.local/bin`（显示成 `/Users/你的名字/.local/bin`）。没有的话把这一行加进 `~/.zshrc`，再开一个新终端：

```sh
export PATH="$HOME/.local/bin:$PATH"
```

之后 `xbot version`、`xbot list`、`xbot serve` 都能用。桌面版和命令行版共用同一个数据目录 `~/.xbot`，不要同时开着两个。

## 第一次启动：填模型密钥
第一次打开 XBot 会先让你粘贴模型的 API key（默认 DeepSeek），点「测试并保存」后进入。密钥存在本机数据目录（`~/.xbot/env`，Windows 是 `%USERPROFILE%\.xbot\env`），只有你的账户能读，不会写进系统或用户环境变量。以前手写的 `env` 文件照常可用，有密钥时不会再问。

## 打不开时
看 `~/.xbot/app.log`（Windows：`%USERPROFILE%\.xbot\app.log`）。常见原因：已经开着一个 XBot；同一个数据目录已经有终端里的 `xbot serve` 在跑（先停掉它）；或 8787 端口被占（可在命令行用 `xbot app --port 8788`）。
