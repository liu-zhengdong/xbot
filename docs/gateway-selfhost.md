# 自建网关

XBot 默认使用官方网关 https://gateway.babelgo.cn:8443（腾讯云上的 `xbot gateway serve`），无需设置。
Cloudflare 上的旧官方网关 https://xbot.babelgo.cn 留作备用，要用它就 `xbot remote gateway https://xbot.babelgo.cn`。
官方部署材料在 [packaging/gateway/](../packaging/gateway/README.md)。
如果你想自己控制中继的部署位置、访问权限和运行资源，可以自建网关。
网关只中继设备间的信封，不同步记忆或文件。

在服务器上启动：

```sh
XBOT_GATEWAY_TOKEN=<口令> xbot gateway serve --addr 127.0.0.1:8790 --dir <目录>
```

口令只从环境变量读取，不要放进命令参数或配置文件。
`--open` 表示任何人都能注册，只应在你自己的访问控制后使用。
在服务前面加 HTTPS，可使用任意反向代理或 Cloudflare Tunnel。

在发邀请的那台安装上，先设地址，再带着口令发码和运行 serve：

```sh
xbot remote gateway https://gw.example.com
XBOT_GATEWAY_TOKEN=<口令> xbot remote invite
XBOT_GATEWAY_TOKEN=<口令> xbot serve
```

口令只发给显式设置的网关，不会发往官方网关。
配对码携带网关地址和口令，加入的一方不需要口令，`remote join` 会采用码里的地址。
已经有配对（或有进行中的邀请）的安装不会被一个别的网关的码改走，会报 `gateway mismatch`；要换网关，先 `xbot remote gateway URL`，再重新配对。
用 `xbot remote gateway` 查看当前地址。
恢复官方网关：

```sh
xbot remote gateway default
```

验证网关实现：一致性测试注册时不带口令，每跑一次会注册十几个测试安装，而 Go 网关最多 64 个安装、闲置 30 天才淘汰。
所以别对正在用的网关跑，起一个临时的 `--open` 实例，用单独的目录，并固定测试密钥：

```sh
xbot gateway serve --open --addr 127.0.0.1:8799 --dir /tmp/xbot-gw-test &
XBOT_GATEWAY_TEST_SEED=selfhost XBOT_GATEWAY_URL=http://127.0.0.1:8799 go test -count=1 -run TestConformanceURL ./internal/gateway
```
