---
kind: part
parent: README.md
code: [internal/machine/, internal/node/, internal/nodesync/, internal/tunnel/, internal/browser/, internal/desktop/]
verified: 2026-10-16 @ d04f79e
---
# 机器与设备

> **30 秒版**：Bot 住在主人选的家节点上，能在授权的机器上做主人能做的事；每台机器一个 allow/ask/off 开关，多节点、隧道、浏览器、桌面都是 Link。

## 边界
负责：工作机、家节点、远程访问、多节点、机器开关、多设备同步、浏览器和桌面。不负责：哪些动作要确认（见 [安全](security.md)）。

## 怎么运转
- **工作机**：一个用户的多个 Bot 共用一台机器，包括装好的工具和登录状态；每个 Bot 有自己的工作目录，另有一个共享目录。技能按 owner 分开，不是全机共用。
- **家节点**：Bot 住在用户选择的机器上（比如家里的 PC），数据不离开用户的设备。手机、Web、Slack 都只是入口。
- **远程访问**：Cloudflare Tunnel、Tailscale 直连、自建中继，都可以选；它们只是不同的传输方式，内核不用关心。`xbot serve --tunnel cloudflare` 用本机的 `cloudflared` 把已经在听的 `--http` 公布出去。真实的 `cloudflared` 把地址打在 stderr，stdout 不关，所以两边一起读。stderr 打印 `public url`（`*.trycloudflare.com`）。`cloudflared` 单独一个进程组，父进程死掉时收到 `SIGKILL`，serve 被硬杀掉也不会留下公开隧道。拉起它的 goroutine 锁住操作系统线程，直到隧道关掉，避免线程先退出把 `SIGKILL` 提前打出去。`--tunnel tailscale` 跑 `tailscale ip -4`；`--http` 若是回环地址，就改绑到这个 tailnet 地址，别的绑定保持原样，`public url` 用 tailnet 地址加端口。二进制路径可用 `XBOT_CLOUDFLARED` 和 `XBOT_TAILSCALE`。自建中继仍是后面的网关，这一片不实现。
- **多节点**：每台设备都可以运行 `xbot node serve`（默认 `127.0.0.1:18772`），用 bearer token 公布本机身份。token 在该数据根的 `node-token`（0600），第一次启动打印一次。也可以用 `--token TOKEN`、`--token -`（stdin 一行）或环境变量 `XBOT_NODE_TOKEN`。`SIGTERM` 和中断一样停掉监听。主人用 `xbot node add <name> --url URL --token TOKEN` 把地址记在 `nodes.json`（0600）之前，先用这个 token 问一次对方，对不上就不写入。`node list` 把 401 显示成 `unauthorized`，连不上才是 `offline`。装上 `node` Link 之后，系统提示里只有一行；`links load node` 才带上 `node_list` 和 `node_call`。能力是 `info`（id、名字、系统、架构）和 `shell`。`shell` 看那台机器的开关。要给别的设备连，把 `--addr` 绑到 tailnet 地址，不要绑 `0.0.0.0`。
- **每台机器一个开关**：允许、每次询问、关闭，决定 Bot 能不能在这台机器上执行。记在数据根的 `machines.json`（0600）。没写的机器是 `allow`，shell 只对删除和对外发送要确认（看集中分类器），其余命令直接跑。`ask` 让每条 shell 都要主人确认。`off` 直接拒绝，确认也跑不了。`xbot machine set <name> allow|ask|off`，本机的名字是 `local`。节点上的 `shell` 看那台机器自己的开关，不看调用方传来的 `approved`。`allow` 才执行。`ask` 把命令记在 `node-pending/<id>.json`（0600），带上调用方、命令和时间，返回 `xbot node approve <id>`。十条钟后过期，最多留 32 条。`xbot node pending` 列出 id、调用方、命令和本机本地时间。`xbot node approve` 先把命令打出来，默认要回答 `y` 才执行，`--yes` 跳过提问。拒绝时退出码非零，并删掉这条待批。这台机器上的 `xbot serve` 在网页里给出允许或拒绝，并往每个 Bot 的通知里写一条。`off` 拒绝，批准也跑不了。`write_file`/`edit_file` 也跟这个开关：`allow` 普通写不弹卡，写自己的 persona/profile 仍确认，`ask` 每次问，`off` 拒绝。
- **多设备同步**：Bot 不合并，各自住在自己的家节点上；只同步人的身份和人的记忆，用可合并的追加日志实现（记录时间、Bot、设备，以最新为准），通过中继同步。
- **浏览器**：用 CDP 驱动本机 Chrome，配置目录是数据根下的 `browser/profile`，不碰主人正在用的那一份。`xbot browser open [--headless] <url>` 打开 http 或 https，留下调试端口；`snapshot` 读标题、地址和可见正文；`click <selector>` 点第一个匹配的元素；`stop` 关掉这次拉起的进程。二进制用 `XBOT_CHROME`，否则在 PATH 里找 `google-chrome` 或 `chromium`。装上 `browser` Link 之后，`links load browser` 才有 `browser_open`、`browser_snapshot`、`browser_click`、`browser_stop`。`browser_stop` 在机器开关是 `off` 时也能确认，用来关掉进程。`xbot browser open` 退出后 Chrome 还在，直到 `xbot browser stop`。`xbot serve` 只关掉自己拉起的那一份，用进程组发信号，不用 `Pdeathsig`。停进程之前核对 `/proc/<pid>/cmdline` 里有这个配置目录。对不上就不杀，删掉过期的 `session.json`，命令退出码是 0，并写明原因。页面正文包在 `<untrusted_event>` 里。机器开关和桌面一样：`off` 拒绝，`ask` 每一下都要确认，`allow` 时只有打开和点击要确认。能用命令行或 API 完成的事，优先不用浏览器。
- **桌面操作**：截图、点击、输入、滚动、按键，也是一个 Link。`xbot desktop` 和装上 `desktop` 之后的工具走同一条。schema 要 `links load desktop` 才进当前这一轮。机器开关是 `off` 就拒绝；`ask` 时每一下都要主人确认，截图也一样；`allow` 时截图不确认，点击、输入、滚动、按键要确认。主人最近几秒还在用这台电脑（`xprintidle` 小于 5 秒）时不点、不打字、不滚动、不按键，截图可以。看不出来就照常做。确认卡写明点击坐标、要输入的原文、按键名，以及滚动的方向、步数和位置。输入或按键里出现付款、登录、发消息时，确认卡再写明这一点。能用命令行或 API 完成的事，工具说明里要求优先不用界面。截图是 PNG，写在数据根的 `desktop/`，权限 0600。点击和按键用 `xdotool`，截图用 ImageMagick 的 `import`。


## 坑
评测里的文件工具和 shell 限制在用例目录里。普通 Bot 不是这样，这一轮不改。

普通 shell 的工作目录是该 Bot 的工作区，环境是当前进程的环境，包括 `HOME`。已经登记的供应商密钥不在里面，除非这次调用把 `keys` 设为 true 并得到确认。命令没有命名空间。机器开关是 `allow` 时，只有删除和对外发送弹确认卡，`cat ~/sand-data/box-secrets.json`、`ls ~`、`printenv`、`python 脚本` 这类命令都不经确认直接跑，所以普通 Bot 能读到工作区外面的文件和环境变量。终端的 `--yes` 会同意所有命令。子代理在允许后台运行时也会自动同意。

文件工具的相对路径从工作区算起，但绝对路径和 `../` 不拒绝，符号链接会跟着走。普通 Bot 因此也能读到工作区外面。和 shell 一样，这一轮只记下来，不改。

