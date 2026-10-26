# 远程控制台管道部署（腾讯云，ADR 0024 批 1）

管道中枢 `https://pipe.babelgo.cn:8443` 就是 `xbot pipe serve`，与账号服务、信箱网关同机（shanghaigo）。协议见 [docs/pipe.md](../../docs/pipe.md)。

| 文件 | 作用 |
|---|---|
| `compose.yaml` | 复用 `packaging/gateway/Dockerfile` 的镜像，命令换成 `pipe serve --addr :8792 --public https://pipe.babelgo.cn:8443`；绑 `127.0.0.1:8792:8792`；`ACCOUNT_API` 指账号服务 |
| `deploy/openresty/pipe.babelgo.cn.conf` | 站点：8443 TLS + :80 只留 ACME |
| `deploy/openresty/proxy/root.conf` | `/pipe/v1/ws` 带 `Upgrade`/`Connection` 头、读写超时 120 秒；其余（领票、健康检查）30 秒 |

## 步骤

1. DNS：`pipe.babelgo.cn` A 记录指向 shanghaigo，**灰云**。
2. 证书：Let's Encrypt DNS-01，放到 `/www/sites/pipe.babelgo.cn/ssl/`，建 `log/`。
3. 在仓库根目录：`docker compose -f packaging/pipe/compose.yaml up -d --build`；`curl -s http://127.0.0.1:8792/healthz` 回 `ok`。
4. OpenResty：放好两个 conf，`openresty -t && openresty -s reload`；外网 `curl -s https://pipe.babelgo.cn:8443/healthz`。

## 要点

- 没有数据卷、不放密钥：票只在内存里，重启后所有会话断开，客户端重新领票。
- 中枢日志只写票 id 前 8 位、配对/结束和字节数；帧内容是端到端密文，不落日志。
- 只想少一个子域时，也可以把 `/pipe/` 挂到 `gateway.babelgo.cn` 的站点下（同样两段 location，`proxy_pass` 指 8792）。
