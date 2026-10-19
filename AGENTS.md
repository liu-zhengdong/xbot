# AGENTS.md

XBot：Go 写的个人 AI 助手，交互单位是长期存在的 Bot。模块 `xbot`，入口 `cmd/xbot`。

## 改动前先读
先读 [docs/atlas/README.md](docs/atlas/README.md) 的 30 秒版和组成表，再读要改那一块的节点。

**做正在开发的东西时，先读对应的 [docs/dev/](docs/dev/) 设计**（有的话）。atlas 写的是仓库现在的样子，dev 写的是打算做成什么样、为什么这么切、哪些还没做；两者对不上时以 atlas 为准。设计全部落地后，把它提炼进 atlas 再删掉那份 dev 文件。

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

## 已有旅程与当前重点
- 开工前读 [docs/FOCUS.md](docs/FOCUS.md)。已经做好的旅程在 `docs/guard/`（还没有就是第一条还没毕业），任何改动都不能让它们退化；出了退化先修，先于一切新功能。
- 同一时间只推进一个功能点，它必须挂在 FOCUS.md 里的某一条重点旅程上（重点最多 3 条，主人挑）；挂不上的写进 [候选清单](docs/CANDIDATES.md)，不当场开工。
- 主人一次说了好几件事，只挑最影响重点的那一件来做，其余写进候选清单，并告诉主人放了哪些。
- 汇报完成时说清级别：已合入 / 端到端通过 / 主人日用通过。只有「已合入」时不说「做完了」。
- 旅程的步骤和「怎样算对」由主人定，Agent 不能自己改；冒烟用例和级别可以自己同步。
- 一条旅程日用通过、冒烟用例也通过，就移进 `docs/guard/<旅程>.md` 守着（移进去属于守护层改动，要主人确认）。做法见技能 `focus`。

## 硬约束
- 不打印、不记录、不提交任何密钥；供应商 key 只记环境变量名。
- 新工具必须在内核权限表里归类，工具自己不决定确认（守门测试会挡）。
- 测试和 CI 只用 `fake/scripted`，不调真模型。
- transcript 只追加，不改旧行。
- UI 改动先由审美顾问出原型，主人定稿后再实现。

## 验证
`gofmt -l .`、`go vet ./...`、`go test -count=1 ./...`。提交前：全量跑一遍，这次改到的包再用 `-count=3` 跑（为抓并行才出现的偶发失败）。界面改动在本机浏览器里看过再算完。

界面没定稿前，不为它写查静态文件文字的测试（类名、文案、颜色值）；只测接口、数据和行为（例如切换 Bot 不能显示上一个 Bot 的数据）。已进 `docs/guard/` 的旅程照常守。旧的这类测试挡住界面改动时直接删，不改写。

## 合并门槛
- 平时在本机 main 上直接提交：不开 issue、不开 PR，本机检查过了就提交。
- 一个功能在浏览器里能用了、要发版，或本地攒了一两天的提交需要备份时，才推到 GitHub。推完看 CI：沙箱评测、读 `/proc` 这类测试在 macOS 上会跳过，只有 CI 的 Linux 在跑；CI 挂了先修，修好才算完成。
- 发版必须推到 GitHub：`xbot serve` 的自动升级读的是 GitHub Release。
- 每个 bugfix 都要带回归测试，且这条测试在旧代码上会失败（回退修复验证一次）。
- 提交说明里把没跑的检查列成「Not run: …」。
- 教训记在 [LESSONS.md](LESSONS.md)；同一条教训在 3 个不同日子出现，就升级成这里的硬约束。

## 全局要求：性能与界面流畅
- 链路不传大块数据：只传摘要或流式传输；每次节点调用上限 1 MiB，处处设大小上限。
- 交互回复 p95 < 3 秒；本机进程间通信（IPC）额外开销 p95 < 5 毫秒；后台导致的变慢不超过 10%。
- 界面：60fps；动画只动 transform/opacity，时长不超过 200 毫秒；遵守 prefers-reduced-motion；轮询不能引起重排、卡顿或抢焦点。
- 每个里程碑验收都要报性能数字。

## 维护
文档分三层，同一件事只写一处：仓库现在的样子写 [docs/atlas/](docs/atlas/)；打算做成什么样、还没落地的写 [docs/dev/](docs/dev/)，落地后提炼进 atlas 再删掉那份；宏观先做哪件事写 [docs/FOCUS.md](docs/FOCUS.md) 和[候选清单](docs/CANDIDATES.md)。atlas 里不写计划，dev 里不写现状，FOCUS 里不写设计细节；dev 的每份设计必须有一节「还没做的」，没有就说明该删了。

改了某块的行为，同步改对应 atlas 节点并刷新 `verified`；设计决策被推翻时新建 ADR，旧的标 Superseded。做过什么、验了什么写 [docs/MILESTONES.md](docs/MILESTONES.md)；正在做什么、怎样算做完写 [docs/FOCUS.md](docs/FOCUS.md)，两处不互抄。运行 `python3 scripts/atlas_audit.py` 检查。

## 地图
全部文档的入口：[docs/atlas/README.md](docs/atlas/README.md)。
