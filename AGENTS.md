# AGENTS.md

XBot：Go 写的个人 AI 助手，交互单位是长期存在的 Bot。模块 `xbot`，入口 `cmd/xbot`。

## 改动前先读
先读 [docs/atlas/README.md](docs/atlas/README.md) 的 30 秒版和组成表，再读要改那一块的节点。

**做正在开发的东西时，先读 `docs/dev/` 里对应的设计**（有正在做的设计时才放 `docs/dev/`，没有就没有这个目录）。atlas 写的是仓库现在的样子，dev 写的是打算做成什么样、为什么这么切、哪些还没做；两者对不上时以 atlas 为准。设计全部落地后，把它提炼进 atlas 再删掉那份 dev 文件。

**做新设计前，先做「对照回顾」**（技能 `liu-dev-workflow` §3）：拿候选清单全部条目、atlas 已有能力、`docs/adr/` 现行决定和 `docs/guard/` 旅程逐条核对撑不撑得住、冲不冲突、能不能复用。交流、上下文、后台状态相关的设计必须先读 [ADR 0021 环境流](docs/adr/0021-env-stream.md)：能用对话串、环境流和插话口表达的，不加新机制。

| 改这些路径 | 先读 |
|---|---|
| `internal/kernel/`、`internal/inbox/`、`internal/inbound/`、`internal/link/` | [kernel.md](docs/atlas/kernel.md) |
| `internal/bot/transcript.go`、`internal/compact/`、`internal/bm25/` | [context.md](docs/atlas/context.md) |
| 分叉、`branch`、`fork`/`fork_end`、`bg_` | [fork.md](docs/atlas/fork.md) |
| `internal/exec/`、`internal/agent/`、`internal/notable/` | [subagents.md](docs/atlas/subagents.md) |
| `internal/memory/`、`internal/person/` | [memory.md](docs/atlas/memory.md) |
| `internal/bot/`、`internal/peer/`、`internal/cron/`、`internal/notify/` | [multibot.md](docs/atlas/multibot.md) |
| `internal/machine/`、`node*`、`tunnel`、`browser`、`desktop` | [machines.md](docs/atlas/machines.md) |
| `internal/skill/`、`internal/code/`、`internal/tools/`、MCP、连接器 | [ecosystem.md](docs/atlas/ecosystem.md) |
| `internal/vault/`、`internal/review/`、新增任何工具 | [security.md](docs/atlas/security.md) |
| `internal/provider/`、`prompts/`、`internal/eval/`、`evals/` | [providers.md](docs/atlas/providers.md) |
| `internal/web/`、`internal/appshell/` | [ui.md](docs/atlas/ui.md)、[guard/界面.md](docs/guard/界面.md)、[API.md](docs/API.md) |

## 已有旅程与正在做的
- 开工前看 `docs/dev/`：有正在做的设计时才放 `docs/dev/`，里面只放正在做的那一件，有什么就是在做什么。已经做好的旅程在 `docs/guard/`，任何改动都不能让它们退化；出了退化先修，先于一切新功能。
- 同一时间只做 dev 里那一件。dev 里没有的想法不当场开工，写进候选清单（不在仓库，在主人的 Obsidian：`正在干什么/06-XBot/XBot 候选清单.md`，按 P1–P4 往下做，记了不用提交）。
- 主人一次说了好几件事，只挑跟正在做的那份最相关的一件来做，其余写进候选清单，并告诉主人放了哪些。
- 汇报完成时说清级别：已合入 / 端到端通过 / 主人日用通过。只有「已合入」时不说「做完了」。
- 旅程的步骤和「怎样算对」由主人定，Agent 不能自己改；冒烟用例和级别可以自己同步。
- 一份 dev 设计做完：现状提炼进 `docs/atlas/`；里面的旅程日用通过、冒烟用例也通过，就连同用例移进 `docs/guard/<旅程>.md` 守着（移进去属于守护层改动，要主人确认）；然后删掉那份 dev 文件，没做完的退回候选清单。做法见技能 `liu-dev-workflow`。

## 硬约束
- 不打印、不记录、不提交任何密钥；供应商 key 只记环境变量名。
- 新工具必须在内核权限表里归类，工具自己不决定确认（守门测试会挡）。
- 测试和 CI 只用 `fake/scripted`，不调真模型。
- transcript 只追加，不改旧行。
- UI 改动先由审美顾问出原型，主人定稿后再实现。

## 验证
`gofmt -l .`、`go vet ./...`、`go test -count=1 ./...`。提交前：全量跑一遍，这次改到的包再用 `-count=3` 跑（为抓并行才出现的偶发失败）。界面改动在本机浏览器里看过再算完。界面没定稿前，不为它写查静态文件文字的测试（类名、文案、颜色值），只测接口、数据和行为；已进 `docs/guard/` 的旅程照常守；旧的这类测试挡住界面改动时直接删，不改写。

## 合并门槛
- 平时在本机 main 上直接提交，不开 issue、不开 PR。一个功能在浏览器里能用了、要发版，或本地攒了一两天需要备份时，才推到 GitHub；推完看 CI 的 Linux（沙箱评测、读 `/proc` 的测试在 macOS 上会跳过），挂了先修好才算完成。发版必须推：`xbot serve` 的自动升级读 GitHub Release。
- 每个 bugfix 都要带回归测试，且这条测试在旧代码上会失败（回退修复验证一次）。
- 提交说明里把没跑的检查列成「Not run: …」。
- 教训记在 [LESSONS.md](LESSONS.md)；同一条教训在 3 个不同日子出现，就升级成这里的硬约束。

## 全局要求：性能与界面流畅
- 链路不传大块数据：只传摘要或流式传输；每次节点调用上限 1 MiB，处处设大小上限。
- 交互回复 p95 < 3 秒；本机进程间通信（IPC）额外开销 p95 < 5 毫秒；后台导致的变慢不超过 10%。
- 界面：60fps；动画只动 transform/opacity，普通过渡不超过 200 毫秒，弹性动画最长 300 毫秒（弹出用 `--dur-pop` 220 毫秒，气泡回弹 300 毫秒）；遵守 prefers-reduced-motion；轮询不能引起重排、卡顿或抢焦点。
- 每个里程碑验收都要报性能数字。

## 维护
文档分三层，同一件事只写一处：仓库现在的样子写 [docs/atlas/](docs/atlas/)；正在做的写 `docs/dev/`（有正在做的设计时才放；打算做成什么样、怎样算做完、还没做的），做完就删，内容转进 atlas 和 `docs/guard/`；还没排上的想法在候选清单，在仓库外（见上）。atlas 里不写计划，dev 里不写现状；dev 的每份设计必须有一节「还没做的」，没有就说明该删了。

改了某块的行为，同步改对应 atlas 节点并刷新 `verified`；设计决策被推翻时新建 ADR，旧的标 Superseded。做过什么、验了什么写 [docs/MILESTONES.md](docs/MILESTONES.md)；正在做什么、怎样算做完写在 docs/dev/ 那份设计里，两处不互抄。运行 `python3 scripts/atlas_audit.py` 检查。

## 地图
全部文档的入口：[docs/atlas/README.md](docs/atlas/README.md)。
