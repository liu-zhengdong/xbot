# AGENTS.md

XBot：Go 写的个人 AI 助手，交互单位是长期存在的 Bot。模块 `xbot`，入口 `cmd/xbot`。

## 改动前先读
先读 [docs/atlas/README.md](docs/atlas/README.md) 的 30 秒版和组成表，再读要改那一块的节点。

| 改这些路径 | 先读 |
|---|---|
| `internal/kernel/`、`internal/inbox/`、`internal/inbound/`、`internal/link/` | [kernel.md](docs/atlas/kernel.md) |
| `internal/bot/transcript.go`、`internal/compact/`、`internal/bm25/` | [context.md](docs/atlas/context.md) |
| 分叉、`branch`、`fork`/`fork_end`、`bg_` | [fork.md](docs/atlas/fork.md) |
| `internal/exec/`、`internal/agent/` | [subagents.md](docs/atlas/subagents.md) |
| `internal/memory/`、`internal/person/` | [memory.md](docs/atlas/memory.md) |
| `internal/bot/`、`internal/peer/`、`internal/cron/`、`internal/notify/` | [multibot.md](docs/atlas/multibot.md) |
| `internal/machine/`、`node*`、`tunnel`、`browser`、`desktop` | [machines.md](docs/atlas/machines.md) |
| `internal/skill/`、`internal/code/`、`internal/tools/`、MCP、连接器 | [ecosystem.md](docs/atlas/ecosystem.md) |
| `internal/vault/`、`internal/review/`、新增任何工具 | [security.md](docs/atlas/security.md) |
| `internal/provider/`、`prompts/`、`internal/eval/`、`evals/` | [providers.md](docs/atlas/providers.md) |
| `internal/web/` | [ui.md](docs/atlas/ui.md)、[API.md](docs/API.md) |

## 核心旅程
- 开工前读 [docs/JOURNEYS.md](docs/JOURNEYS.md)。同一时间只推进一个功能点，它必须挂在某一条旅程上；挂不上的写进 [候选清单](docs/CANDIDATES.md)，不当场开工。
- 主人一次说了好几件事，只挑最影响旅程的那一件来做，其余写进候选清单，并告诉主人放了哪些。
- 汇报完成时说清级别：已合入 / 端到端通过 / 主人日用通过。只有「已合入」时不说「做完了」。
- 旅程和「怎样算对」由主人定，Agent 不能自己改。

## 硬约束
- 不打印、不记录、不提交任何密钥；供应商 key 只记环境变量名。
- 新工具必须在内核权限表里归类，工具自己不决定确认（守门测试会挡）。
- 测试和 CI 只用 `fake/scripted`，不调真模型。
- transcript 只追加，不改旧行。
- UI 改动先由审美顾问出原型，主人定稿后再实现。

## 验证
`gofmt -l .`、`go vet ./...`、`go test -count=1 ./...`。合入 main 前跑三遍测试。

## 合并门槛
- `go test` 全绿才合并；绿了就直接合进 main，不再等截图或确认。
- 每个 bugfix 都要带回归测试，且这条测试在旧代码上会失败（回退修复验证一次）。
- 提交说明或 PR 里把没跑的检查列成「Not run: …」。
- 教训记在 [LESSONS.md](LESSONS.md)；同一条教训在 3 个不同日子出现，就升级成这里的硬约束。

## 全局要求：性能与界面流畅
- 链路不传大块数据：只传摘要或流式传输；每次节点调用上限 1 MiB，处处设大小上限。
- 交互回复 p95 < 3 秒；本机进程间通信（IPC）额外开销 p95 < 5 毫秒；后台导致的变慢不超过 10%。
- 界面：60fps；动画只动 transform/opacity，时长不超过 200 毫秒；遵守 prefers-reduced-motion；轮询不能引起重排、卡顿或抢焦点。
- 每个里程碑验收都要报性能数字。

## 维护
改了某块的行为，同步改对应 atlas 节点并刷新 `verified`；设计决策被推翻时新建 ADR，旧的标 Superseded。进度只写 [docs/MILESTONES.md](docs/MILESTONES.md)。运行 `python3 scripts/atlas_audit.py` 检查。

## 地图
全部文档的入口：[docs/atlas/README.md](docs/atlas/README.md)。
