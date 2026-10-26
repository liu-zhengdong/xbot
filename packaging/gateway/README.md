# 官方信箱网关部署（腾讯云）

官方网关 `https://gateway.babelgo.cn:8443` 就是 `xbot gateway serve --open --behind-proxy`，跑在 shanghaigo，与账号服务同机。没有备案，公网只开 8443。Cloudflare 上的 `https://xbot.babelgo.cn` Worker 不动，留作备用。

| 文件 | 作用 |
|---|---|
| `Dockerfile` | 多阶段编出 `xbot`，入口 `gateway serve --open --behind-proxy --addr :8790 --dir /data` |
| `compose.yaml` | 绑 `127.0.0.1:8791:8790`，数据在 `/opt/xbot-gateway/data`，`XBOT_GATEWAY_MAX_INSTALLS=1000` |
| `deploy/openresty/gateway.babelgo.cn.conf` | 站点：8443 TLS + :80 只留 ACME |
| `deploy/openresty/proxy/root.conf` | 反代 `127.0.0.1:8791`，读写超时 45 秒（长轮询最多 30 秒），不缓冲 |

## 步骤

1. DNS：`gateway.babelgo.cn` A 记录指向 shanghaigo，**灰云**（不走 Cloudflare 代理）。
2. 证书：Let's Encrypt DNS-01，放到 `/www/sites/gateway.babelgo.cn/ssl/{fullchain,privkey}.pem`，建 `log/` 目录。
3. 数据目录：`mkdir -p /opt/xbot-gateway/data && chown 10001:10001 /opt/xbot-gateway/data`。
4. 在仓库根目录：`docker compose -f packaging/gateway/compose.yaml up -d --build`，然后 `curl -s http://127.0.0.1:8791/healthz` 应回 `ok`。
5. OpenResty：把站点 conf 放进 `conf.d/`，`proxy/root.conf` 放进站点的 `proxy/`，`openresty -t && openresty -s reload`。
6. 外网验：`curl -s https://gateway.babelgo.cn:8443/healthz`；再跑一致性测试 `XBOT_GATEWAY_URL=https://gateway.babelgo.cn:8443 go test ./internal/gateway -run TestConformanceURL`。

## 要点

- 不放任何口令：官方网关开放注册，靠每 IP 每 UTC 日 30 个新安装（内存计数，重启清零）和 1000 个安装上限兜着。
- `--behind-proxy` 只信 `X-Forwarded-For` 的最后一段，OpenResty 的 `$proxy_add_x_forwarded_for` 正好把真实客户端地址追加在最后。不要把容器端口直接暴露到公网，否则客户端可以伪造这个头。
- 回滚：客户端 `xbot remote gateway https://xbot.babelgo.cn` 回到 Cloudflare；Worker 未删即可用。CF 里没取走的信不会搬过来。
