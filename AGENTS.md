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

## 硬约束
- 不打印、不记录、不提交任何密钥；供应商 key 只记环境变量名。
- 新工具必须在内核权限表里归类，工具自己不决定确认（守门测试会挡）。
- 测试和 CI 只用 `fake/scripted`，不调真模型。
- transcript 只追加，不改旧行。
- UI 改动先由审美顾问出原型，主人定稿后再实现。

## 验证
`gofmt -l .`、`go vet ./...`、`go test -count=1 ./...`。合入 main 前跑三遍测试。

## 维护
改了某块的行为，同步改对应 atlas 节点并刷新 `verified`；设计决策被推翻时新建 ADR，旧的标 Superseded。进度只写 [docs/MILESTONES.md](docs/MILESTONES.md)。运行 `python3 scripts/atlas_audit.py` 检查。

## 地图
全部文档的入口：[docs/atlas/README.md](docs/atlas/README.md)。
