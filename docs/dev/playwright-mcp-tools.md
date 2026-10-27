# 连接器 Agent 侧可用

候选文件：`Playwright连接器无工具.md`（题为「连接器 Agent 侧可用」）；设计真源：`设计/连接应用.md` 与 atlas ecosystem。

## 目标与验收

主人 2026-10-27 指定：商店安装全局能力；上下文 `## 连接器` 仅列商店类应用，每行 `名称:描述`；用法由 `links load` 渐进披露，不每轮注入全量 schema。电脑操作、原生浏览器、精选 MCP 共用该路径，不合并 CDP 与 Playwright。

- `Playwright` / `playwright` / `mcp:playwright` 以及 `电脑操作` / `desktop` 等名称和 id 均可加载；成功返回工具名，并进入本轮后续模型调用。
- 启动、初始化、列工具失败返回可读原因，保留有界、脱敏的 stderr；失败不能报告 loaded/no tools。
- `links` 中文说明明确“看到连接器摘要 → load 名称或 id”。
- webhook/watch/skills 等非商店能力移出连接器摘要块，只调整该块，不改其他能力路径。
- API 分开表达安装与每个 Bot 的运行状态；读取状态不启动进程。主人明确本次暂不改商店界面。

## 实现取舍与对照回顾

复用 `ResolveApp`、`links load`、MCP Link 与商店接口；Link 的 `Tools` 签名保留，额外提供可返回错误的加载入口和只读运行状态。沿用原 15 秒 RPC 上限。启动失败保留在当前实例；配置变化会按既有同步机制换实例，重启也可重新连接。

| 对照 | 结论 |
| --- | --- |
| atlas/ecosystem、kernel | 全局安装、每轮 load 已有，只补错误与摘要筛选 |
| ADR 0021 | 结果沿原工具返回，不新建消息、状态文件或环境流账本 |
| ADR 0023、atlas/security | 安装/凭据状态不等于工具可用；不改授权或权限分类 |
| guard 六条基本旅程、界面 | fake/scripted 验证；前端和守护旅程不改 |
| 主人本轮需求、编码克制 28501a6 | 不新增工具、不改无关指令、发版或 lock，不 sync-app、不 push |

## 还没做的

- 通过真实 Playwright 与本机电脑操作/浏览器入口做人工端到端及日用验收。
- 商店界面接入 API 运行状态与文案，待单独界面定稿（主人要求本次不改）。

**冒烟用例**：`internal/kernel/playwright_load_test.go`、`internal/link/mcp_diagnostics_test.go`、`internal/web/apps_runtime_test.go`（fake/scripted 组件/API 回归，不冒称真实日用端到端）。
