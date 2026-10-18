---
kind: research
verified: 2026-10-18 @ c545964
---
# Agent 功能对标表

> **30 秒版**：每周日调研一次市面上的 Agent（Claude Code 为主，加 Codex、Cursor、OpenCode、Gemini CLI、Grok Bot），把新出现或被称赞的功能对照 XBot 现状，定「做 / 不做 / 候选」。2026-10-18 是第一期：19 条，做 1、候选 9、不做 9。最值得做的是**只读工具并行执行**（Codex 0.163.0 已发，清单里早就有）；新进候选的有**例程免模型前置检查**、**每轮上下文检视与请求复现**、**限流等太久就停或换模型**、**安全对抗评测**。

## 怎么读
- **只用公开资料**：官方 changelog 与 release、公开文章、X 上的公开帖子。Grok Bot 只按外部能看到的行为和用户的公开反馈推断，不获取内部提示词或私有接口。
- **XBot 现状**按 GitHub `liu-zhengdong/xbot` main 的代码核对（本期 `c545964`），证据写路径；更早的整体对照见 [claude-code-internals.md](claude-code-internals.md)。
- **决定**：做 = 建议排进下一版；候选 = 进 [候选清单](../CANDIDATES.md)，不直接开工；不做 = 写明理由。已定不做：文件检查点/回滚、hooks、规划模式。
- 每期只追加新行或改已有行的状态，旧行不删；日期是写入或最后改动的日期。

## 对标表

| 功能 | 来源 Agent | 出处 | XBot 现状 | 效果评估 | 决定 | 理由 | 日期 |
|---|---|---|---|---|---|---|---|
| 只读工具并行执行（结果按序回填） | Codex | [rust-v0.163.0](https://github.com/openai/codex/releases/tag/rust-v0.163.0)（#52245）；[X 上的版本播报](https://x.com/Codex_Changelog/status/2110855940463583297) | ❌ 工具调用按序执行；权限表已有只读分类（`internal/kernel/review.go`、`review.ClassOf`） | 一轮里连读多个文件、多次 `recall` 时，延迟按调用数线性涨；并行后接近最慢的一个。只放行只读类，写操作照旧串行 | **做**（建议排进下一版） | 清单里已有这条，现在有产品级先例；复用权限表的只读类，不加新对象。验收：只读批次延迟下降、回填顺序不变、写调用不并发 | 2026-10-18 |
| 空闲压缩：赶在前缀缓存过期前压缩 | Claude Code | [v2.1.299](https://github.com/anthropics/claude-code/releases/tag/v2.1.299) 新增、[v2.1.302](https://github.com/anthropics/claude-code/releases/tag/v2.1.302) 加关闭开关；[X：有人觉得烦到需要开关](https://x.com/OpenViber/status/2111220366777757770)、[X：好评](https://x.com/pggalaviz/status/2110763130158727615) | ❌ 只有约 70% 时的后台异步压缩（`internal/kernel/bgcompact.go`） | 常驻 Bot 空闲多，适用；但 Anthropic 是 1 小时缓存，DeepSeek 等服务商的缓存保留时间不同，收益要先实测 | 候选（已在清单） | 口碑显示必须可关；先量 DeepSeek 的缓存命中随空闲时长怎么掉，再定阈值 | 2026-10-18 |
| 例程免模型前置检查：没变化就不叫醒模型 | Grok Bot（用户反馈） | [X 文章：例程一周醒 672 次什么也没做](https://x.com/twinedon0/status/2109245137565925816)、[X：例程频率才是花钱大头](https://x.com/fleyta88/status/2109701942783942694) | ❌ 例程每次触发都进模型（`internal/cron`），没有前置条件 | 自查、巡检这类例程大多数时候「没事」，每次都整轮跑费 token；一条 shell 检查（退出码 0 才叫醒）能挡掉多数空跑 | 候选 | 新想法，按规矩先进清单；复用 `shell` 和例程，不加新对象 | 2026-10-18 |
| 每轮上下文检视：看每一轮模型实际收到了什么 | Claude Code（社区 mod「Turn Inspector」） | [X：Turn Inspector 发布帖，353 收藏](https://x.com/dani_avila7/status/2109822699174559748) | 🟡 有 token 用量和窗口占比（`internal/kernel/usage.go`），`trace.jsonl` 只记数字（`internal/bot/decision.go`） | 调 DeepSeek 提示词、查 Bot 为什么偏离时最缺这个；只读调试视图，不影响前缀缓存 | 候选 | 和下一行一起做；网页只读，复用前缀组装和 transcript | 2026-10-18 |
| 请求可复现：记录每次请求的系统提示、工具和参数 | Claude Code | [v2.1.301](https://github.com/anthropics/claude-code/releases/tag/v2.1.301)（Changed local session transcripts…） | ❌ transcript 记消息，不记当时的完整请求 | 评测失败、线上偏离时能逐字重放；注意脱敏（保险箱只记名字） | 候选（并入上一行） | 同上 | 2026-10-18 |
| 无人值守时，额度或限流要等太久就结束请求 | Claude Code | [v2.1.300](https://github.com/anthropics/claude-code/releases/tag/v2.1.300)（`CLAUDE_CODE_RETRY_WATCHDOG_MAX_USAGE_LIMIT_WAIT_MS`） | 🟡 429 和 5xx 退避重试两次（`internal/provider/openai.go` `retryable`），不读重置时间、不换模型 | 夜里例程碰到长时间限流时，现在要么失败要么白等；本周共享 dsh 就碰到过上游限流 | 候选 | 可以和「按 Bot 配备用模型」一起考虑 | 2026-10-18 |
| 换到上下文更小的模型前先用旧模型摘要 | Claude Code | [v2.1.301](https://github.com/anthropics/claude-code/releases/tag/v2.1.301)（Improved switching to a model with a smaller context window） | ❌ 换绑定模型时不检查窗口（窗口只在 `usage.go` 里用来算占比） | XBot 一个 Bot 可换模型，换小窗口时下一轮直接超长；改动小 | 候选 | 小改动，复用已有压缩 | 2026-10-18 |
| MCP 工具 schema 被模型服务商拒绝时，去掉参数继续，而不是每轮 400 | Claude Code | [v2.1.301](https://github.com/anthropics/claude-code/releases/tag/v2.1.301)（Fixed a conversation failing on every turn…） | ❌ 没找到对应处理（`internal/link/mcp*.go`） | 装了一个 schema 写坏的 MCP 服务器，整个 Bot 就不能用；属于健壮性问题 | 候选 | 先写一个复现用例确认 XBot 真会这样，确认后按 bug 修 | 2026-10-18 |
| 编码 Agent 安全对抗基准（HarnessSecurity-Bench：打开自动批准，攻击成功率 29.2% → 95.6%） | 研究（测了 Claude Code、Codex CLI、Gemini CLI 等 6 个） | [X：结果摘要](https://x.com/al3rez/status/2109570007613259850)、[X：实验设置](https://x.com/himanshu19890/status/2109483239576322324) | 🟡 规则式审查、不可信围栏、机器开关 `allow` 档（`internal/review`） | 正好检验 XBot 的 `allow` 档和围栏在投毒文件、投毒网页下守不守得住 | 候选（旁路评测） | 测评是旁路，不卡发布；借它的投毒场景写几条 eval | 2026-10-18 |
| 协调者 + 并行线程：给目标，自动拆线程、汇总、开 PR | Claude Code（Projects，9/17 beta，10/9 放开 waitlist） | [官方介绍](https://claude.com/resources/articles/projects-redesigned)、[METAL 报道](https://metallab.ai/en/2026/10/claude-code-projects-waitlist-pro-max) | 🟡 多 Bot、群、主 Bot；没有「目标→自动拆线程→汇总」 | 和 XBot 的多 Bot 方向一致，但官方自己说用量涨得快 | 候选（远期） | 先做完子代理类型（M7）；主人对 token 消耗敏感 | 2026-10-18 |
| 回复中途插话、立即转向 | Codex | [rust-v0.163.0](https://github.com/openai/codex/releases/tag/rust-v0.163.0)（#51812） | ✅ 插话分叉（F1–F5），`steer/queue/interrupt` | XBot 前台几秒内回答、原来那轮转后台，比「立即转向」更强 | 不做 | 已覆盖 | 2026-10-18 |
| fork 子代理默认开、子代理默认后台跑 | Claude Code | [v2.1.302](https://github.com/anthropics/claude-code/releases/tag/v2.1.302) | ✅ `spawn` 的 fork 模式、后台续段（`internal/kernel/agents.go`、`bgsegment.go`） | 已有 | 不做 | 已覆盖 | 2026-10-18 |
| 后台完成通知写清原因；模型不再轮询后台输出或 sleep 等 | Claude Code | [v2.1.300](https://github.com/anthropics/claude-code/releases/tag/v2.1.300) | ✅ `shell_job` 完成提醒和叫醒，`sleep` 轮询交给熔断（`docs/MILESTONES.md` shell 后台） | 已有 | 不做 | 已覆盖 | 2026-10-18 |
| Mods：带界面和生命周期钩子的扩展 | Claude Code | [X：Mods 介绍](https://x.com/aiwithuday/status/2109527683206107570) | ❌ | 本质是 hooks 加界面扩展 | 不做 | hooks 已定不做；thin harness，扩展走 Link 和技能 | 2026-10-18 |
| 环境里的日期和时区可配置 | Codex | [rust-v0.163.0](https://github.com/openai/codex/releases/tag/rust-v0.163.0)（#52164） | ✅ Bot 的 `timezone` 配置（`cmd/xbot/cmd_bot.go`） | 已有 | 不做 | 已覆盖 | 2026-10-18 |
| 执行前的沙箱完整性检查 | Codex | [rust-v0.163.0](https://github.com/openai/codex/releases/tag/rust-v0.163.0)（#51843 等） | ❌ | — | 不做 | 沙箱有意不做，隔离靠部署方式和 `xbot vaultd` | 2026-10-18 |
| 文件工具串行化与原子写；状态文件原子落盘、坏了从备份恢复 | Gemini CLI | [v0.64.0](https://github.com/google-gemini/gemini-cli/releases/tag/v0.64.0) | ✅ `fsutil.WriteAtomic`、记忆写锁、同文件写操作排队（F3） | 已有；「坏了从备份恢复」XBot 没有，但目前没出过事 | 不做 | 已覆盖主要部分 | 2026-10-18 |
| 云端代理产出可交互画布 | Cursor | [changelog](https://cursor.com/changelog)（Canvases from cloud agents） | ❌ | 对编码团队有用，对个人常驻 Bot 价值低 | 不做 | 先补「把文件发给主人」 | 2026-10-18 |
| 手机遥控本机上的 Agent | Cursor（10/6） | [changelog](https://cursor.com/changelog)（Remote control for local agents） | ✅ 网页 + PWA + 隧道 + 官方网关 | 已有 | 不做 | 已覆盖 | 2026-10-18 |

## 本期没有功能可对标的动态
- OpenCode 1.19.x 停掉了会话分享，2.x 改成显式升级（`opencode upgrade --major`），没有新功能（[v1.19.1](https://github.com/anomalyco/opencode/releases/tag/v1.19.1)、[v1.19.2](https://github.com/anomalyco/opencode/releases/tag/v1.19.2)）。
- Gemini CLI 本周只有 nightly 和 v0.65 预览，正式版 v0.64.0 以修复为主。
- Grok Bot 本周公开可见的变化是语音通话更新（[X](https://x.com/CarolinaDill/status/2111241359612944513)），和 XBot 当前目标无关。
