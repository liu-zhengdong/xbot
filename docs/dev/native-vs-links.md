---
created: 2026-10-28
updated: 2026-10-30
status: 已实现（功能分支 feature/tool-registry-proxy）·待合入与日用
来源: 政东 2026-10-28；划分刀口：「自己实现 → 原生工具；依赖外部 → Links」
---

# 原生能力与 Links

## 划分刀口（唯一）

| | 原生工具 | Links（连接应用） |
|---|---|---|
| 判据 | **功能是我们自己实现的** | **依赖别人的东西**（MCP 进程、本机 Chrome/CDP、外部 SaaS 等） |
| 例子 | context、sub agent、memory、壳、文件、`web_fetch`、电脑操作（本进程实现）、Webhook/定时/文件监听、对人说话… | Playwright / Context7 等 MCP、**浏览器操作**（靠 Chrome）、Telegram/飞书、其它要装的外接 |
| 装法 | 开机即有，不进商店，不经 `apps.json` 才「装上」 | 连接应用商店安装；上下文「名称:描述」按需展开 |

不要再用「本机 / 远程」「有没有密钥」当主刀口——**有没有我们自己的实现**才是。

## 做完长什么样

1. **原生工具常驻**：凡自己实现的能力，默认挂上；危险面用「权限」总开关闷，不是商店装卸。
2. **浏览器 = Links**：依赖本机 Chrome，进商店可装（grant / `apps.json` 可以）。
3. **抓页**：内核 `web_fetch` 是原生；目录里的 MCP `fetch` **废除**（删条目；已装进 `mcp.json` 的也不进 `/api/apps` / 已连接计数）。
4. **电脑操作**：本进程实现 → **原生常驻**（不进商店）。
5. 能力框只列真 Links；每轮可见工具约 ≤10～20。

## 和旧口径

推翻「凡通道都叫 Link、再用 grant 扮装」；推翻「浏览器当默默自带器官」；推翻 hidden fetch 仍出现在已连接。

实现以本页为准；《连接应用》文首跟本刀口。


## 工具面

注册与渐进披露见 [[工具注册与渐进披露]]（统一注册中心；渐进披露抄 Pi 默认代理门 search/describe/call；不改 tools[]）。

## 现状（2026-10-28）

- `link.Organs()` 驱动 CLI 与内核的原生能力装配；memory、timer、persona、context、agents、skills、webhook、watch、web、peers、terminal、tools、desktop 和有本进程后端的 coding-agent 都常驻注册，旧原生 grant 被忽略。能力框和 catalog 不列这些器官。
- 浏览器：`StoreApps()` 里的原生 grant 卡（`dev` 分类），安装前探测 Chrome。
- fetch：`connectors.json` 里标 `retired`，`Connectors()` 不返回；旧 `mcp.json` 里的同名条目不挂载，也不出现在 `/api/apps` 和已连接计数。
- 判断按 `Organs()` / `StoreApps()` / `StoreGrant()` / `retired` 做，没有 id 黑名单，也没有命令指纹。

## 还没做的

- 设置页没有目视：浏览器卡回到商店之后的样子、分类和探测失败时的提示。
- 用户日用验收。
