# 自建网关

XBot 默认使用官方网关 https://xbot.babelgo.cn，无需设置。
如果你想自己控制中继的部署位置、访问权限和运行资源，可以自建网关。
网关只中继设备间的信封，不同步记忆或文件。

在服务器上启动：

```sh
XBOT_GATEWAY_TOKEN=<口令> xbot gateway serve --addr 127.0.0.1:8790 --dir <目录>
```

口令只从环境变量读取，不要放进命令参数或配置文件。
`--open` 表示任何人都能注册，只应在你自己的访问控制后使用。
在服务前面加 HTTPS，可使用任意反向代理或 Cloudflare Tunnel。

在每台安装上设置地址：

```sh
xbot remote gateway https://gw.example.com
```

配对码携带网关地址，加入的安装会自动采用它。
用 `xbot remote gateway` 查看当前地址。
恢复官方网关：

```sh
xbot remote gateway default
```

验证自建网关：

```sh
XBOT_GATEWAY_URL=https://gw.example.com go test -count=1 -run TestConformanceURL ./internal/gateway
```
