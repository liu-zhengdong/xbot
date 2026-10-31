# 限流等待上限与一次备用

M 档；用户已授权本 MVP。仅 Provider / config / cmd 装配，不改内核、UI、transcript。

## 怎样算对

沿用候选和用户正反手：两小时 Retry-After 在上限内结束且有明确原因；配置 flash 备用后主模额度墙可换一次完成；备用也失败则结束；2 秒 429 可恢复；无备用不声称切换。测试通过 ModelSelector.Select → Provider.Chat → HTTP 替身断言请求次数、目标模型、类型化错误和耗时，不以日志词命中代替行为。

## 方案

唯一装配入口为 RateLimitSelector（包装 FixedSelector）。每次 Select 建立本轮 Provider，重试层通过 context 共享累计等待预算（默认 3 分钟）；无建议时固定 200ms 退避。有 Retry-After（秒/HTTP 日期）、常见 reset header 或结构化 reset 建议则遵从；建议超过剩余预算立即结束，不真的等两小时。无建议的连续限流也由预算封顶。普通 5xx/EOF/超时仍最多重试两次；任何文字或 hosted 输出后不重试/切换。

providers.json 的供应商级 rate_limit_wait_seconds 与 fallback: {provider, model} 覆盖默认；备用不递归装配其备用，与主模共用累计等待预算。只在本轮临时使用备用，不持久改绑、不新增状态通道；后续工具步继续用备用，一轮至多切一次。

## 对照回顾

| 材料 | 结论 |
|---|---|
| 候选全部验收点 | 上述正反手均挂 selector/retry seam |
| atlas providers / multi-provider 设计 | 复用三协议共用 complete 与 ModelSelector，不另设调用方路由 |
| ADR 0021 | 不加环境状态机制、不改前缀或历史；失败原因沿现有错误处理落盘 |
| 六条基本旅程 | 保持正常模型、工具和主动唤醒路径，跑 smoke 守护 |
| transcript 窗口 / 控机 / 公式块 | 不在冲突面，不修改 |

**冒烟用例**：`e2e/smoke/rate_limit_test.go` 的 `TestRateLimitBinary`（真 serve / registry / API / SSE，上游 scripted）；`internal/config/rate_limit_test.go` 与 `internal/provider/rate_limit_test.go` 钉住装配 seam、三协议、累计等待、短重置、一次切换与无备用。由实现方依据用户正反手编写，待主管独立验收。
**级别**：分支专项真二进制通过，尚未合入、既有旅程守护未全绿、不声称整体端到端或日用通过。

## 还没做的

- 主管独立验收与主人日用。专项真二进制已覆盖交互 API 和真实到期 Timer：slot 被消费、trace 状态 failed、serve 既有错误出口记录「限流等待过久」；不新增例程跑次状态机制。
- 全仓和既有六条旅程的基线失败尚未修复（未改动 HEAD 归档复现同组失败）；交主管决定，不越界扩修。
- 用户确认后才能移入 guard；未合本地 main、未接 8788 日用服务。
