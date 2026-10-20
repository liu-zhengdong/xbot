---
kind: part
parent: README.md
code: [internal/machine/, internal/tunnel/, internal/browser/, internal/desktop/]
verified: 2026-10-18 @ 0eec2b1
---
# 机器与设备

> **30 秒版**：Bot 住在主人选的家里那台机器上，能在授权的机器上做主人能做的事；每台机器一个 allow/ask/off 开关，隧道、浏览器、桌面都是 Link。多台电脑只靠网关配对（链 [网关](gateway.md)），旧的「节点」（`xbot node`、远程 shell、人和记忆同步）已于 2026-10-18 退役（链 [ADR 0012](../adr/0012-retire-node.md)）。

## 边界
负责：工作机、主机、远程访问、机器开关、多台电脑的配对（在[网关](gateway.md)里）、浏览器和桌面。不负责：哪些动作要确认（见 [安全](security.md)）。

## 怎么运转
- **工作机**：一个用户的多个 Bot 共用一台机器，包括装好的工具和登录状态；每个 Bot 有自己的工作目录，另有一个共享目录。技能按 owner 分开，不是全机共用。
- **主机**：Bot 住在用户选择的机器上（比如家里的 PC），数据不离开用户的设备。手机、Web、Slack 都只是入口。
- **远程访问**：Cloudflare Tunnel、Tailscale 直连、自建中继，都可以选；它们只是不同的传输方式，内核不用关心。`xbot serve --tunnel cloudflare` 用本机的 `cloudflared` 把已经在听的 `--http` 公布出去。真实的 `cloudflared` 把地址打在 stderr，stdout 不关，所以两边一起读。stderr 打印 `public url`（`*.trycloudflare.com`）。`cloudflared` 单独一个进程组，父进程死掉时收到 `SIGKILL`，serve 被硬杀掉也不会留下公开隧道。拉起它的 goroutine 锁住操作系统线程，直到隧道关掉，避免线程先退出把 `SIGKILL` 提前打出去。`--tunnel tailscale` 跑 `tailscale ip -4`；`--http` 若是回环地址，就改绑到这个 tailnet 地址，别的绑定保持原样，`public url` 用 tailnet 地址加端口。二进制路径可用 `XBOT_CLOUDFLARED` 和 `XBOT_TAILSCALE`。跨设备的中继见[网关](gateway.md)。
- **每台机器一个开关**：允许、每次询问、关闭，决定 Bot 能不能在这台机器上执行。**只在权限检查打开时生效**（`xbot permissions on`，[ADR 0019](../adr/0019-no-permission-cards-by-default.md)）；默认关着，`machine.Get` 一律读成 `allow`，存下的值不动，`xbot machine set` 会提示这一点。下面写的是打开之后的行为。记在数据根的 `machines.json`（0600）。没写的机器是 `allow`，shell 只对删除和对外发送要确认（看集中分类器），其余命令直接跑。`ask` 让每条 shell 都要主人确认。`xbot machine set <name> allow|ask|off`，本机的名字是 `local`。`off` 直接拒绝，确认了也跑不了。`write_file`/`edit_file` 也跟这个开关：`allow` 普通写不弹卡，写自己的 persona/profile 仍确认，`ask` 每次问，`off` 拒绝。
- **多台电脑**：Bot 不合并，各自住在自己的主机上。多台电脑只靠[网关](gateway.md)配对：配对设备之间不同步任何文件或记忆，唯一的能力是 Bot 之间发消息（`bot@设备`，[ADR 0010](../adr/0010-gateway-mailbox-relay.md)）。旧的「节点」（`xbot node`、远程 shell、人和记忆同步、`xbot sync`）已于 2026-10-18 退役（[ADR 0012](../adr/0012-retire-node.md)）；`xbot node …` 和 `xbot sync …` 只说已移除，并指向 `xbot remote invite` / `xbot remote join`。旧的 `nodes.json`、`node-token`、`node-pending/` 留着不动，`serve` 启动只打一行日志说明忽略。
- **浏览器**：用 CDP 驱动本机 Chrome，配置目录是数据根下的 `browser/profile`，不碰主人正在用的那一份。`xbot browser open [--headless] <url>` 打开 http 或 https，留下调试端口；`snapshot` 读标题、地址和可见正文；`click <selector>` 点第一个匹配的元素；`stop` 关掉这次拉起的进程。二进制用 `XBOT_CHROME`，否则在 PATH 里找 `google-chrome` 或 `chromium`。装上 `browser` Link 之后，`links load browser` 才有 `browser_open`、`browser_snapshot`、`browser_click`、`browser_stop`。`browser_stop` 在机器开关是 `off` 时也能确认，用来关掉进程。`xbot browser open` 退出后 Chrome 还在，直到 `xbot browser stop`。`xbot serve` 只关掉自己拉起的那一份，用进程组发信号，不用 `Pdeathsig`。停进程之前核对 `/proc/<pid>/cmdline` 里有这个配置目录。刚拉起、还在 exec 里的进程命令行会短暂读成空，空的时候最多再等 0.5 秒才判断。对不上就不杀（macOS 和 Windows 没有 `/proc`，核对总是不通过，所以这两个平台上 `stop` 目前不会杀进程），删掉过期的 `session.json`，命令退出码是 0，并写明原因。页面正文包在 `<untrusted_event>` 里，正文里的结束标签换成 `[/untrusted_event]`。机器开关和桌面一样：`off` 拒绝，`ask` 每一下都要确认，`allow` 时只有打开和点击要确认。能用命令行或 API 完成的事，优先不用浏览器。
- **桌面操作**：截图、点击、输入、滚动、按键，也是一个 Link。`xbot desktop` 和装上 `desktop` 之后的工具走同一条。schema 要 `links load desktop` 才进当前这一轮。机器开关是 `off` 就拒绝；`ask` 时每一下都要主人确认，截图也一样；`allow` 时截图不确认，点击、输入、滚动、按键要确认。主人最近几秒还在用这台电脑（`xprintidle` 小于 5 秒）时不点、不打字、不滚动、不按键，截图可以。看不出来就照常做。确认卡写明点击坐标、要输入的原文、按键名，以及滚动的方向、步数和位置。输入或按键里出现付款、登录、发消息时，确认卡再写明这一点。能用命令行或 API 完成的事，工具说明里要求优先不用界面。截图是 PNG，写在数据根的 `desktop/`，权限 0600。点击和按键用 `xdotool`，截图用 ImageMagick 的 `import`。


## 坑
评测里的文件工具和 shell 限制在用例目录里。普通 Bot 不是这样，这一轮不改。

普通 shell 的工作目录是该 Bot 的工作区，环境是当前进程的环境，包括 `HOME`。已经登记的供应商密钥不在里面，除非这次调用把 `keys` 设为 true 并得到确认。命令没有命名空间。机器开关是 `allow` 时，只有删除和对外发送弹确认卡，`cat ~/sand-data/box-secrets.json`、`ls ~`、`printenv`、`python 脚本` 这类命令都不经确认直接跑，所以普通 Bot 能读到工作区外面的文件和环境变量。终端的 `--yes` 会同意所有命令。子代理在允许后台运行时也会自动同意。

文件工具的相对路径从工作区算起，但绝对路径和 `../` 不拒绝，符号链接会跟着走。普通 Bot 因此也能读到工作区外面。和 shell 一样，这一轮只记下来，不改。

