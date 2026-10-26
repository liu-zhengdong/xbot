# 模型上下文窗口跟真值走

候选真源：主人 Obsidian `正在干什么/06-XBot/候选/模型上下文窗口跟真值走.md`。Ultra 2026-10-26 23:59 排期；执行官并行编码。

## 对照回顾

T/H/fit 依赖 `ModelSpec.Context`。缺省曾退 `compact.DefaultWindow=128K`；deepseek-flash 真窗 1M 漏配时被写错。本条：**目录为源**；缺省 **256K**；有上游 `/models` `context_length` 可刷新；探测只补洞。勿与对话上下文拼装、multi-provider 大改搅在一起。

## 实现规格与怎样算对

| 项 | 规格 / 验收 |
|---|---|
| 缺省 | `compact.DefaultWindow`（及等价退路）改为 **256_000**，不再 128_000。未知模型用 256K。 |
| 目录为源 | `providers.json` / `ModelSpec.Context` 有值则用之。deepseek-flash（及目录已有）验 Context=1048576 / 1M，T/H 按真值比例。 |
| 上游刷新 | 若 `/models` 带 `context_length`：目录为源 + 有元数据可刷新 + 探测只补洞；策略写进本文。不每轮请求前拖延迟。 |
| 测试 | 更新含 `DefaultWindow` 断言与 flash/未知模型相关测试；相关包 `go test` 一遍绿。 |
| 样例 | providers 漏配 flash → 不再当 128K；目录写了 1M → 用 1M；完全未知 → 256K。 |

## 刷新策略（钉死）

1. **信目录**：已有显式 Context > 0 时，日常 Resolve/装配用目录值。
2. **有元数据可刷新**：复用设置详情/供应商同步的按需拉取入口，仅接受 1～10000000 的整数 context_length；与目录冲突时信上游。整份响应与分页验证成功后，在既有 registry 文件锁内重读，批量更新已登记模型的 Context 并原子保存一次，记录更新数量。缺失或无效元数据保留目录值；重复相同值不写文件；删除或换端点后的迟到响应不写回。未登记模型只展示元数据，选用时走原登记路径；ChatGPT 的 context_window 保留原有覆盖规则。日常 Resolve 不拉取网络；内核缓存应感知目录文件更新。
3. **探测补洞**：超窗错误解析出 maximum context length 时只能补缺失 Context，不能覆盖显式目录；不作为主路径，不主动撞墙。本次不新增错误正文解析或探测写回，保留现有超窗裁剪重试，补齐真值走按需目录刷新。

## 非目标

勿跟对话上下文拼装搅；不重开 multi-provider 大改；不 push。

## 还没做的

独立复审、合入 main、docs/dev 毕业。
