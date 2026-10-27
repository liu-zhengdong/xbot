---
kind: design
---
# 设置内连接应用

## 旅程与怎样算对

用户在设置商店点击添加或连接。需要密钥时在设置内填写；实际探测成功才标已添加，失败留在原处并可重试。不关闭设置、不代发聊天；密钥不进入聊天、日志、列表或模型提示。无密钥精选与本机能力同样先探测。验收依据为用户提供的设计师交接及 2026-10-27 补钉。

## 方案

复用应用商店与 nestcard，设置 API 在落盘前按类型调用上游或启动能力。已有按 Bot 的密钥存储继续使用。聊天里的 links install 不改。决定见 ADR 0031（取代 0023 第 2 条的密钥部分）。

**冒烟用例**：`internal/web/apps_secret_browser_test.go`、`internal/web/apps_probe_test.go`；脚本化替身，未达到真实上游端到端通过。

## 还没做的

- 飞书、Telegram 用真 token 跑一遍设置添加（只用假 token 验过拒绝分支）；飞书入站校验用同一个值，无法主动探测。
- 在 serve 真机浏览器里点一遍 Context7 真 key 与 Playwright 添加；独立复审；用户日用验收。
- Playwright 探测时子进程 cwd 是 serve 的 cwd，`@playwright/mcp` 会在那里留 `.playwright-mcp/` 空快照文件。
