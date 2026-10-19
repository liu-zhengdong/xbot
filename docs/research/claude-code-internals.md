---
kind: research
verified: 2026-10-17（未提交）
---
# Claude Code 内部机制调研 → XBot 借鉴

> **30 秒版**：Claude Code 的主循环只是一个 `while(true)`。工程量几乎全在循环外面：分层压缩、缓存稳定、按输入判定能否并发、读后写校验、分级错误恢复、"没进展就停"、权限按 deny→ask→allow 求值。XBot 最该抄的是**确定性的外围机制**，不是提示词。其中 8 条借鉴列在文末，并按 7 个里程碑给了调整建议。

## 来源与可信度
- **[S1]** bits-bytes-nn《Claude Code Architecture Analysis》，基于 2026-03-31 npm source map 泄露源码的解读：https://bits-bytes-nn.github.io/insights/agentic-ai/2026/03/31/claude-code-source-map-leak-analysis-en.html
- **[S2]** claude-code-from-source（同一批源码的书式重写，伪代码）：https://github.com/alejandrobalderas/claude-code-from-source ，章节 https://claude-code-from-source.com/ch06-tools/ 、/ch08-sub-agents/ 、/ch09-fork-agents/ 、/ch18-epilogue/
- **[S3]** VILA-Lab/Dive-into-Claude-Code（架构综述，含 Piebald-AI 提示词语料索引）：https://github.com/VILA-Lab/Dive-into-Claude-Code
- **[S4]** claude-harness 文件操作篇：https://claude-harness.dev/en/articles/21-file-operations
- **[S5]** cablate 缓存失效报告：https://github.com/cablate/claude-code-research/blob/master/reports/agent-sdk-cache-invalidation/report-en.md
- **[S6]** anthropics/claude-code issue #49585（system-reminder 被并入 tool_result，导致缓存失效）：https://github.com/anthropics/claude-code/issues/49585
- **[S7]** issue #40573（TodoWrite 提醒过频）：https://github.com/anthropics/claude-code/issues/40573
- **[S8]** issue #48390（"modified since read" 误报）：https://github.com/anthropics/claude-code/issues/48390
- **[S9]** 泄露源码镜像中的 permissions.ts：https://github.com/claude-code-best/claude-code/blob/8246ffa3/src/utils/permissions/permissions.ts
- **[S10]** Trilogy AI《How to Use Claude Code like a Claude Code Engineer》：https://trilogyai.substack.com/p/how-to-use-claude-code-like-a-claude
- **[S11]** OutSight AI《Peeking under the hood of Claude Code》（抓包分析）：https://medium.com/@outsightai/peeking-under-the-hood-of-claude-code-70f5a94a9a62
- **[D]** 官方文档：memory https://code.claude.com/docs/en/memory 、permissions https://code.claude.com/docs/en/permissions 、tools-reference https://code.claude.com/docs/en/tools-reference
- 泄露源码的解读属于二手资料，版本停在 2026-03 前后，之后可能已经改过。标「推断」的是我的推理，没有找到直接出处。
- 对 XBot 现状的描述基于 `/workspace/xbot-f5`（v0.3.0 `f12fd9b`）里的 rg 检索，标「需核实」的没有逐行读代码。
- 优先级：P0 = 并入现有里程碑，马上做；P1 = 下一批；P2 = 可选；✗ = 不做。

---

## 1. 主循环与 turn 结构
- **怎么做**：`query.ts` 约 1,729 行，是一个 `async function*` 的 `while(true)`。它逐条 yield 流式事件，最后 return 一个 `Terminal`（终止原因）[S1]。
  - 参数分两类。不变的参数包括 messages、systemPrompt、canUseTool、maxTurns、fallbackModel。可变的 State 包括 turnCount、压缩跟踪、max-output 恢复计数、`transition` 等。每个 continue site 都整体重赋 `state = {...}`，并写明 `transition.reason`（比如 `next_turn`、哪种恢复、哪次回退）[S1]。
  - 每轮 6 段：压缩 → 调模型（流式，边生成边执行只读工具）→ 错误恢复 → stop hook 与 token 预算 → 执行剩余工具 → 收取预取的技能和记忆、排空排队命令、刷新 MCP 工具 [S1]。
  - 9 种终止原因：completed、blocking_limit、aborted_streaming、aborted_tools、prompt_too_long、image_error、model_error、hook_stopped、max_turns [S1]。
  - 会话层由 `QueryEngine` 管：累计 usage、权限拒绝记录、readFileState。用户消息同步落盘（`--resume` 依赖它），助手消息异步落盘 [S1]。
- **为什么**：终止原因和续轮原因都显式化，测试就能断言「走了哪条恢复路径」，不用比对消息内容 [S1]。
- **XBot**：`internal/agent/agent.go` 已有步数上限加 `stopSummary`。
  - **借鉴**：给 agent loop 加 `StopReason` 枚举和 `transition` 字段，写进 transcript 和 trace。M1 打转检测、错误恢复、评测打分都能直接用。
  - **P0**，并入 M1，改动小。

## 2. System prompt、工具描述与动态注入（system-reminder）
- **怎么做**：
  - 系统提示词由多源拼装：设置、CLAUDE.md、工具、MCP、agents、output style、技能 [S3]。
  - 运行时把 `<system-reminder>` 塞进 user 消息、tool_result 后面，甚至工具结果里面。内容包括 todo 提醒、文件被外部改动的 diff、token 用量、日期、CLAUDE.md 等 [S11][S5][S6]。
  - 提醒是**条件触发**的。例如 `ls` 的结果后面会追加「你还没用 TodoWrite」[S11]。
  - 子代理（Task）收不到 todo 提醒 [S11]。
  - Agent 工具的说明按场景生成。可用 agent 的列表放在 attachment 消息里而不是工具描述里，避免改动缓存前缀 [S2 ch08]。
- **为什么**：模型在长任务里会漂。小块提醒的成本比在系统提示里写长规则低，也更贴近当时的上下文。
- **坑**：
  - 提醒里带动态数值，又被并入历史的 tool_result，会让历史字节漂移，每轮都缓存失效 [S6]。
  - 进程重启后注入内容重组，哪怕只差一个字节，约 45k token 都要重写缓存 [S5]。
- **XBot**：已有的「待确认卡提醒」就是 reminder 雏形。
  - **借鉴**：统一做一个 `reminder` 通道，有三条规则：
    1. 只追加到**最新一条** user/tool 消息的末尾，永远不改写历史。
    2. 落盘时和正文分开存，重放时原样重现。
    3. 每类提醒有触发条件和冷却期。
  - todo 提醒、待确认卡、文件改动、打转警告都走这个通道。
  - **P0**，是 M1、M2 的共同基础设施。

## 3. 上下文管理与自动压缩

> **2026-10-19 实测修正**：本节根据二手资料写成，反汇编本机 2.1.292 后有几条被证伪，原文保留在下面并就地标注。修正结论：
> - **Context Collapse / collapse store / `projectView()` 查不到。** 全库 `collapse` 66 处全是 UI、CSS、SQL 等无关词；`"context collapse"` 只出现 1 次，是一句 `autocompact_state` 的 schema 说明（「context collapse owns headroom」），只有能力位级别的认知，没有 store、没有投影函数。第 4 级应视为**未证实**。
> - **Snip 机制不存在。** `CLAUDE_SNIP` 是个开关，`snip` 的其余命中绝大多数是 WebKit 的 `Snippet`。
> - **摘要不是多块，是单块。** `summaryMessages` 恒为单元素数组；`pi()` 从最后一个 `compact_boundary` 起截断数组，之上的历史全部丢弃。`compactMetadata` 没有区间字段，保留段用 UUID（`preservedSegment.headUuid/tailUuid`）加 `parentUuid` 单链表。
> - **`AUTOCOMPACT_BUFFER_TOKENS` / 减 13k 查不到。** 真实字段是 `autoCompactWindow`。
> - **9 段是 LLM 输出格式，不是存储结构。** 实际是 `<analysis>` + `<summary>`，后者内部 9 节。
> - **熔断不是简单 3 次计数**，是 rapid-refill 检测（`consecutiveRapidRefills`、`turnsSincePreviousCompact`）。
> - 方法与局限：Bun 打包会压短局部标识符，所以「搜不到函数名」证明力弱；上述否定结论都建立在搜**字符串字面量**（遥测事件名、zod `.describe()` 文案、prompt 模板）的基础上。本机 5 个会话 jsonl 都没有真实压缩产物，只有二进制证据，没有运行时样本交叉验证；联网核对全部失败（代理与 key 问题）。

- **怎么做**：从便宜到贵共 5 级 [S1][S3]：
  1. **Tool Result Budget**：截断超大工具结果。
  2. **Snip**：整段丢弃旧消息，主要用于 headless 会话；释放的 token 数会传给后面的 autocompact 判断。**（2026-10-19 证伪：机制不存在）**
  3. **Microcompact**：把旧的 read/shell/grep/web 等结果替换成 `[Old tool result content cleared]`。它感知缓存：落在已缓存前缀里的结果先不清，记为待清，等缓存窗口移过去再清。
  4. **Context Collapse**：读时投影。原消息不动，摘要存在 collapse store，发请求时 `projectView()` 叠加。它排在 autocompact 前面，「如果 collapse 已经降到阈值以下，autocompact 就不触发，保留细粒度上下文」。**（2026-10-19 查不到，视为未证实）**

  5. **Auto-Compact**：让 LLM 做全量摘要。
- **阈值**：有效窗口减 13k（`AUTOCOMPACT_BUFFER_TOKENS`）时触发；**连续失败 3 次就熔断**。警告分级：剩 20k 黄，剩 13k 压缩，剩 3k 阻断 [S1][S10]。
- **摘要格式**：9 段，依次是意图、关键概念、文件与代码、错误与修复、问题解决、**用户原话逐条保留**、待办、当前工作、下一步 [S10]。（本对话自己的压缩摘要就是这个结构，可以作为旁证。）
- **压缩前的预处理**：图片替换成 `[image]`；tool_use/tool_result 按 API 轮次分组，**丢就整组丢**；压缩请求本身超长时，从最旧的组开始丢，兜底一次丢 20% [S1]。
- **XBot**：`internal/kernel/history.go` 有压缩（「Sequence numbers stay valid after compaction」）。超过 4KB 的结果已经存 blob、只留预览，这相当于第 1 级。（需核实：现有阈值、熔断、摘要模板。）
  - **借鉴**：
    - (a) 加 microcompact。旧工具结果换成占位符加 blob 引用，`expand` 可以取回。XBot 已有 blob，成本很低。
    - (b) 压缩丢消息时按 tool 配对分组。这和 XBot 的 tool-result 配对修复是同一类 bug，要加回归测试。
    - (c) 摘要模板采用 9 段，「用户原话」一段必须保留。
    - (d) 连续 3 次失败就熔断。
  - **P1**，(b) 是 **P0**（正确性问题）。

## 4. 记忆（仅参考）
- **怎么做**：
  - CLAUDE.md 和 CLAUDE.local.md 从 cwd 向上逐级加载。子目录的 CLAUDE.md 等访问到该目录里的文件时再懒加载 [D memory]。
  - auto memory 在 `~/.claude/projects/<proj>/memory/`。启动时只加载 `MEMORY.md` 索引的前 200 行或 25KB；主题文件按需用 Read 读。索引写超了照样写成功，但会返回错误，要求模型重写索引 [D memory]。
  - 召回：用 Sonnet 做 side-query，从清单里挑 5 条相关记忆，不用向量 [S2 ch18]。
  - 后台有记忆抽取 agent，每轮结束后 fork 出来跑，共享父缓存，所以很便宜 [S2 ch18]。
  - 主会话的 memory 不进入子代理（fork 除外）[D memory]。
- **XBot**：已有 store 和 `extract.go`。
  - **只参考**两点。一是**索引有上限，超限时提示模型自己整理**。二是「小模型挑记忆」的思路，可以对照现有召回方式。（推断：比关键词召回更准，但要多付一次调用。）
  - **P2**，不进里程碑。

## 5. 子代理（Task/Agent）调度、隔离与并发
- **怎么做**：
  - 普通子代理开新上下文，父代理只看到最终结果。工具集由 `tools`/`disallowedTools` 求交得到，求出来是空集时直接报错，不启动 [D tools-reference]。
  - 有 `maxTurns`，到上限返回 partial，可以 resume [D]。
  - **fork 子代理**：继承父代理**渲染后的** system prompt、**完全相同的工具数组**（连 Agent 工具也保留，虽然禁止调用，因为删掉会改变数组、打破缓存）、模型和历史。各个子代理的占位 tool_result 是同一个常量串，只有最后一段指令不同，所以能最大化共享缓存 [S2 ch09]。
  - 防递归有两道：`querySource` 标记，加上扫描历史里的 fork 标签 [S2 ch08]。
  - 前台 agent 超过 120s 自动转后台（受开关控制）；fork 实验下全部异步 [S2 ch09]。
  - 规划中的 Coordinator 只能用 Agent/TaskStop/SendMessage/SyntheticOutput，没有 Bash 也不能读文件 [S1]。
- **XBot**：`spawn` 有 brief/fork 两种模式，加 steer/queue/cancel，F4 后台分段。这部分已经比较强。
  - **借鉴**：
    - (a) fork 模式**原样复用父代理已渲染的系统提示和工具数组**（字节一致），不要重新拼。
    - (b) 子代理类型等于「工具白名单加系统提示片段加步数」，空集时报错。
    - (c) explore 类型只给只读工具，并且不注入 todo 提醒。
  - **P1**，对应 M6。

## 6. Todo 工具与提醒
- **怎么做**：
  - TodoWrite（现在是 TaskCreate/Get/List/Update）有 pending/in_progress/completed 三态。
  - 提醒通过 system-reminder 在工具调用后条件注入，「还没用 todo」时提醒；有清单时重注入整张清单 [S11][S7]。
  - 社区抱怨提醒过频：一个 3 小时的会话注入约 15 次，**包括刚调完 TodoWrite 的下一轮**，导致模型做「表演性更新」。建议的抑制规则是：最近 N 次工具调用里有 todo 就不提醒，或者存在不陈旧的 in_progress 项就不提醒 [S7]。
  - 官方文档现在只在部分模型上默认启用 todo。理由是「新模型不用清单也能跟踪多步工作，工具定义和提醒反而占上下文」[D tools-reference]。
- **XBot**：M2 路线图。
  - **借鉴**：
    - 提醒带抑制规则（最近 3 次调用里有 todo 就不提醒，或 in_progress 项更新不到 10 分钟）。
    - 提醒只发**差异或当前项**，不发整表。
    - 按模型开关，对 DeepSeek 默认开。（推断：弱模型更需要它。）
    - 「结束前还有未完成项」做成续轮门（continuation gate），这是 XBot 已定的设计。
  - **P0**，对应 M2。

## 7. 工具并发：只读与写的调度
- **怎么做**：
  - `isConcurrencySafe(input)` **按输入判定**。例如 Bash 解析子命令，全部是读或搜索时才为 true [S2 ch06]。
  - 调度条件：当前没有工具在跑，或者在跑的和新来的都是安全的，才能启动 [S1]。
  - 流式执行器在模型还在输出时就开跑只读工具 [S1]。
  - 一个工具出错时，同批的兄弟工具以 `sibling_error` 取消 [S1]。
  - 默认值走保守方向：漏写 `isConcurrencySafe` 或 `isReadOnly` 一律按 false 处理 [S2 ch06]。
- **XBot**：F3 只做了同目录写排队。（需核实：同一轮里多个 tool call 是不是串行执行。）
  - **借鉴**：Tool 接口加 `ConcurrencySafe(raw) bool`，默认 false；read、http GET、links search 返回 true，shell 走只读命令白名单。只读工具并行跑，**结果仍按原 tool_call 顺序回填**（保住配对和顺序的修复）。
  - **P1**。流式提前执行：P2。

## 8. 权限判定流程
- **怎么做**，按 [S9] 的源码顺序：
  1. 整个工具命中 deny → 拒绝。
  2. 命中 ask → 询问。
  3. 工具自身的 `checkPermissions`。
  4. **内容级 ask 规则**（如 `Bash(npm publish:*)`）连 bypass 模式也要询问。
  5. **安全检查**（.git/、.claude/、shell 配置文件）同样不受 bypass 影响。
  6. bypass 或 allow 规则 → 放行。
  7. 剩下的 passthrough 转成 ask。
- 文档原则：「deny→ask→allow，first match，具体程度不改变顺序；allow 不能在 deny 里开例外」。裸工具名 deny 会直接把工具从上下文里移除 [D permissions]。
- 规则来源共 8 个层级 [S1]。
- auto 模式用分类器判定，**连续 3 次或累计 20 次拒绝后退回到询问**；分类器本身失败时退回询问，而不是拒绝 [S1][S10]。
- Bash 有 23 个具名注入检查 [S10]；用小模型提取命令前缀，用于规则匹配和注入检测 [S11]。
- 危险模式：解释器、curl、sudo 等不允许加通配 allow [S1]。
- **XBot**：已有 approval card、`internal/review` 权限表和 vault。
  - **借鉴（M7）**：
    - 规则三态 deny/ask/allow，first match 按 deny→ask→allow。
    - 「不再询问」生成的是**精确前缀规则**，并禁止对解释器或网络命令生成通配规则。
    - 一组受保护路径（.git、vault 文件、shell rc、XBot 配置）**永远询问**，不受 allow 影响。
    - 裸 deny 的工具从工具列表里移除。
    - 连续被拒 3 次就注入提醒「换思路或问用户」，这条接到 M1。
  - **P0/P1**，对应 M7。

## 9. Bash 后台化与超时
- **怎么做**：
  - 默认 2 分钟，上限 10 分钟。超时**不杀**，自动转后台，返回任务 id 和输出文件路径（`sleep` 开头的命令除外）[D tools-reference]。
  - `run_in_background` 由模型主动指定。输出写文件，用 Read 读；停止用 TaskStop，连脱离的子进程一起停 [D]。
  - 后台完成的通知走排队命令，在 turn 尾排空注入 [S1 Stage 6]。
- **XBot**：M3 已经规划了这些。
  - 需要补的一点：**完成通知在下一个安全点（turn 尾）以 reminder 注入**，不要打断正在进行的 tool 配对。
  - 以及：`sleep` 或轮询类命令不转后台，交给 M1 识别为轮询。
  - **P0**。

## 10. 文件读写的新鲜度校验
- **怎么做**：
  - `readFileState` 是一个 LRU，存内容、mtime，以及是否只读了一部分 [S4]。
  - Edit/Write 前比较 mtime：比读的时候新就拒绝，提示「File has been modified since read... Read it again」。没读过不让改；只读了一部分的也有限制 [S4][S2 ch06]。
  - Windows 下 mtime 可能抖动，所以对完整读过的文件再比一次内容 [S4]。
  - 重复读一个没变的文件，返回约 30 token 的桩 [S4]。
  - 每轮还会扫描已读文件，mtime 变了就把 diff 作为 reminder 注入 [S5]。
  - 已知的坑：自己的 Edit 没刷新缓存，导致误报 [S8]。
- **XBot**：`internal/tools` 里没有 mtime 检查（rg 结果为空）。
  - **借鉴**：每个 agent 会话维护 readState，记录路径、mtime、size、hash。
    - edit/write 要求先读过，并且 mtime 和 hash 都没变；**自己写完立即刷新**（规避 [S8]）。
    - 未变的重复读返回桩。
  - 这对多 Bot 共用一台盒子（dev 和其他 bot 改同一个文件）特别有价值。
  - **P0**，建议新开小里程碑或并入 M4 环境感知。必须配一个旧代码会失败的回归测试。

## 11. 打转与停止条件
- **怎么做**：
  - maxTurns；token 预算到 90% 就停。
  - **收益递减检测**：连续续轮 3 次以上，且最近两次各自新增都少于 500 token，就停 [S1]。
  - autocompact 连续 3 次失败熔断；max-output 恢复最多 3 次；权限拒绝 3/20 次退回询问 [S1][S10]。
  - stop hook 可以拒绝结束，并把原因注入后重试 [S1]。（hooks 已砍，但「结束门」的思路可以留着。）
- **XBot**：M1 已经列了重复输出、同参工具循环、文件来回改、轮询同一失败、自我扩张。
  - **补充**：
    - 收益递减（低产出续轮）。
    - 所有计数器统一成 `breaker` 小组件，3 次触发，触发后给出 `StopReason`。
    - 第一次触发先注入 reminder（软），第二次才硬停并要求出总结（复用 `stopSummary`）。
  - **P0**。

## 12. 成本控制：小模型做辅助任务
- **怎么做**：
  - 用小模型（Haiku）做会话标题、话题是否切换的判断、Bash 命令前缀提取和注入检测、配额探测 [S11][S3]。
  - 记忆召回用 Sonnet side-query [S2]。
  - auto 模式的权限分类器另开一次调用，只读工具跳过分类器 [S1]。
  - 后台记忆抽取靠 fork 共享缓存来降本 [S2 ch18]。
- **XBot**：
  - **借鉴**：配一个 `aux_model`（如 deepseek-chat 或更便宜的模型），专做标题、记忆抽取、命令风险预判、压缩摘要。
  - 所有辅助调用都要有超时，**失败时走保守路径**（询问用户 / 跳过），不能阻塞主循环。
  - **P1**。

## 13. 缓存（prompt caching 布局）
- **怎么做**：
  - 内置工具和 MCP 工具**分区各自排序后再拼接**。服务端在最后一个内置工具后放全局断点，这样 MCP 工具的增减不会扰动内置区 [S1]。
  - 动态内容（agent 列表等）移到 attachment 消息 [S2 ch08]。
  - microcompact 避开缓存区 [S1]。
  - 有选项排除动态的 system prompt 段（`--exclude-dynamic-system-prompt-sections`）[S3 → vibewatch]。
  - 反例见 [S5][S6]：历史字节漂移导致整段重写，按 125% 计费。
- **XBot**：provider 是 OpenAI 兼容，`cache_control` 检索为空。DeepSeek 的缓存是自动的前缀缓存（推断：同样依赖字节一致）。
  - **借鉴**：给系统提示分层。稳定的身份/规则段在前；工具按名排序，内置在前、Link 在后；易变的日期、环境、待办放到**最后一条消息**。
  - 加一个测试：同一会话连续两次请求，前缀字节一致。
  - **P1**。成本收益大，改动集中在 prompt 组装。

## 14. 错误恢复与重试
- **怎么做**：
  - **prompt 过长（413）**：先把 collapse 的候选立即提交（零成本），再做一次 reactive compact（去图后重试，每轮只做一次），还不行才报错。
  - **max-output 截断**：先把上限从 8k 透明提到 64k，再注入「从断处继续」最多 3 次，最后用已有结果收尾。
  - 回退模型：切换时给悬空的 tool_use 补 **tombstone 结果**，保持配对 [S1]。
  - 529 过载只在前台来源时重试 [S10]。
- **XBot**：provider 有两次退避重试（`openai.go`）。
  - **借鉴**：
    - (a) 截断时自动续写（DeepSeek 常见）。
    - (b) 上下文超长时先做 microcompact 加压缩，再重试一次。
    - (c) **任何中断、取消、回退都给悬空的 tool_call 补占位结果**。这正是 XBot 修过的配对问题，应该收敛成一个统一函数。
    - (d) 后台或辅助调用不重试过载错误。
  - (c) **P0**，其余 P1。

## 15. Hooks / MCP 内部流程（简述）
- **Hooks**：用户已砍，✗。只留一点：stop hook 的「结束门」思路已并入 M2 续轮门。
- **MCP**：
  - 客户端文件 `client.ts` 约 119K。工具经 deny 规则过滤后，分区排序拼到内置工具后面 [S1]。
  - 每轮结束时刷新工具列表 [S1]。
  - MCPTool 默认按可并发处理 [S10]。
  - 工具多时用 ToolSearch 延迟加载 [S1]。
- **XBot**：`links search/load` 已经等价于延迟加载。
  - **借鉴**：Link 工具在工具数组里固定放在内置工具之后（缓存）；MCP 工具默认**不可并发**。这里比 Claude Code 保守，因为远端副作用未知。
  - **P2**。

---

## 最重要的 8 条借鉴
1. **文件新鲜度校验**：先读后写，比对 mtime 和 hash，自己写完刷新，重复读返回桩。多 Bot 共用一台盒子时这是正确性问题。P0，新增或并入 M4。
2. **统一 reminder 通道**：只追加在最新消息尾，不改写历史；带触发条件和冷却期。todo、待确认卡、打转警告、后台完成、文件改动都走它。P0，是 M1/M2/M3 的底座。
3. **熔断加显式 StopReason**：所有「3 次」类计数器统一成一个组件，先软提醒再硬停。补上收益递减检测（连续 3 次续轮各自少于 500 token）。P0，对应 M1。
4. **悬空 tool_call 一律补 tombstone，压缩按配对分组丢**：把 XBot 修过的配对 bug 收敛成一个函数加回归测试。P0。
5. **Todo 提醒要克制**：有抑制规则，只发差异，按模型开关，未完成项做续轮门。P0，对应 M2。
6. **权限 deny→ask→allow first-match，受保护路径永远询问**：「不再询问」只生成精确前缀规则，解释器和网络命令禁止通配。P0/P1，对应 M7。
7. **缓存友好的 prompt 布局**：稳定段在前，内置工具和 Link 分区排序，易变信息放最后，fork 时原样复用父代理已渲染的提示。另加字节一致测试。P1。
8. **分层压缩**：先 microcompact（复用 blob），再 LLM 摘要（9 段模板，保留用户原话），连续 3 次失败熔断；截断时自动续写。P1。

## 对 7 个里程碑的调整建议
| 里程碑 | 调整 |
|---|---|
| M1 打转检测 | 先做 reminder 通道和 breaker 组件（作为 M1 的前置）。加入收益递减、连续被拒 3 次、压缩失败熔断。产出 `StopReason` 写进 trace，供收敛评测打分 |
| M2 todo | 提醒加抑制规则，只发差异。续轮门替代 stop hook。子代理（explore）不注入提醒。按模型开关 |
| M3 shell 后台化 | 超时转后台、不杀。完成通知在 turn 尾经 reminder 注入。`sleep`/轮询命令不转后台，交给 M1 |
| M4 环境感知 | **并入文件新鲜度校验**（或单独拆成 M4a，排在 M4 前面）；外部改动的 diff 作为 reminder 注入。AGENTS.md 分层加载（子目录懒加载）参考 CLAUDE.md |
| M5 搜索+看图 | 不变。压缩前把图片替换成占位符；microcompact 的清理对象包括 web 结果 |
| M6 子代理类型 | 类型 = 工具白名单 + 提示片段 + 步数，空集时报错；fork 复用父代理已渲染的提示和工具数组；防递归两道 |
| M7 权限规则 | deny→ask→allow，受保护路径不受 allow 影响，精确前缀，危险命令不允许通配；裸 deny 把工具从列表里移除 |
| 新增候选（不立即开） | 只读工具并发（`ConcurrencySafe`）、`aux_model` 辅助小模型、prompt 缓存布局、截断自动续写、microcompact |
