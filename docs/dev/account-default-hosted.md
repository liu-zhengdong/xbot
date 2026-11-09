# 桌面版默认连官方账号服务

## 问题

新装的机器上，`xbot app` 没设 `XBOT_ACCOUNT_API`，`xbot serve` 就在进程里跑一份账号替身，
数据落在 `<root>/account-api/`。替身里没有别人注册的账号，于是随便输什么密码都回
`credentials`，界面显示「邮箱或密码不对」——人看到的是「密码错」，真因是「连的不是同一个
账号库」。2026-11-09 在一台 MacBook Air 上就是这么卡住的，排查绕了一圈。

## 做法

- `internal/account.HostedBase = https://account.babelgo.cn:8443`。
- `cmd/xbot` 新增 `accountAPI(app bool)` 决定地址：
  - `XBOT_ACCOUNT_API` 有值时用它；值为 `local` 表示明确要进程内替身；
  - 没设时，桌面版（`cfg.App`）默认 `HostedBase`；`xbot serve` 仍不设、仍跑替身（测试和
    本地开发都不碰网络）。
- 决定结果回写进 `XBOT_ACCOUNT_API`，因为 `deviceclient.Accounts` 也读同一个变量，两处不能
  一个连官方、一个连替身。

## 边界

- 测试不受影响：`internal/web` 的用例直接构造 `Server`，`cmd`/`e2e` 的用例显式设
  `XBOT_ACCOUNT_API`。
- 连不上的时候界面报的是网络错，不再是「密码错」——这一条没改，属于下一步。
