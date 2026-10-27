# 输出截断自动续写

候选真源：用户 Obsidian `正在干什么/06-XBot/候选/输出截断自动续写.md`。Ultra 2026-10-27 主管规格：检出截断（如 `length` / `max_tokens`）后自动续写 ≥1 轮并拼回完整输出；过程不对用户展示。

## 对照回顾

DeepSeek 等常见因 max tokens 截断半截回复。现状 provider 流已见 Anthropic `stop_reason=max_tokens`；需在内核侧检出后静默续写拼回。

## 实现规格与怎样算对

| 项 | 规格 / 验收 |
|---|---|
| 检出 | 识别输出截断信号（OpenAI `finish_reason=length`、Anthropic `stop_reason=max_tokens`、及仓库内等价字段）。 |
| 续写 | 自动再请求 ≥1 轮续写；把后续文本拼回同一条助手输出。 |
| 静默 | 过程**不对用户展示**（无「正在续写」气泡、无中间半截当最终回复发出）。 |
| 边界 | 有上限（避免死循环）；非截断结束（end_turn / stop）不续写。 |
| 测试 | 有复现或单测：截断→续写→拼回；用户可见面只有完整结果。 |

样例正：假 provider 先 `length` 半截再完整 → 用户侧一条完整消息。
样例反：把半截先发出去再补；无限续写；把普通 stop 当截断。

## 实现约束

- 统一在逻辑回复完成前收齐；结束信号在流尾，所以普通回复也延后到整条完成才公开，不能只在看到截断后才开始隐藏。
- 复用原 provider 请求、上下文预算、鉴权、记账和取消路径；最多续写两轮，累计内容上限 1 MiB。失败或上限仍截断时走原错误出口，不把半截当成功结果。
- 三个协议的流式和 JSON 返回共用停止原因判断；正文为空的截断也进入续写。截断工具参数不执行，报错交回。
- 完整内容拼接后再做回复控制行剥离与校验；临时片段和续写提示不单独写 transcript。
- 回归用例：`internal/kernel/output_continuation_test.go`、`internal/provider/continue_test.go`、`internal/agent/continuation_test.go`。

## 非目标

不改上下文 overflow 压缩主路径；不 push；不提手测。相关包 `go test` 一遍绿。

## 还没做的

独立复审、合入、8788、docs/dev 毕业。
