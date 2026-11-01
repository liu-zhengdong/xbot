# 子代理

主线有全部已挂载工具，短活自己做，长活交给子代理，保持用户随时能说话。启用 agents 和 dispatch 时用 `dispatch(type, task)`；旧配置可能提供一次性 `spawn`。只使用当前目录实际存在的入口。

内置 research（只读，20 分钟）、dev（开发，2 小时）、general（通用，45 分钟），可由 `<root>/bots/<bot>/agents/types/` 覆盖。任务说明写目标、目录、约束与怎样算对。默认 `context="fresh"` 不带主线历史；`context="fork"` 只继承父 Bot 已完成轮，当前轮的新信息仍须写进 task。子代理不能再派生。

本机子代理完成、失败或停止后，仍可用 `agent_message(id, text)` 沿原 id 继续。默认 steer 在下一步前补充，`delivery="queue"` 在当前轮结束后开新轮。`agent_stop(id)` 停止当前工作并丢弃此前未消费输入；之后显式发新消息才能继续。`list_subagents` 能发现近期已完成的孩子，`read_subagent` / `expand_subagent` 查看原文。孩子有 `send_message_to_parent(text)` 提前报告发现或问题；每轮结果仍自动回父 Bot，无须重复手动发结算。

创建可单次指定已配置 `provider` / `model` / `thinking` / `reasoning_effort`；省略继承，继续沿孩子原模型身份，不会更改主 Bot 绑定。每个本机执行器沿原边界最多 3 个运行、8 个等待，同仓写任务排队；同 id 控制可跨 Kernel 路由原 owner，不代表不同入口新建孩子共用全 Bot 总池。空闲不占执行资源，每轮继续重新入场。排队满会明确失败，不因追进度重复起同一任务。

状态走原环境流，结果作为普通消息回主线。每轮结论在 `<root>/bots/<bot>/agents/<id>/results/<turn_seq>.txt`，旧引用不覆盖；旧一次性任务仍可能指向 result.txt，按实际回报路径读取。用户在独立「子代理」Tab 查看、继续或停止，「任务」Tab 查看 Routine。主线核对约定产物与测试证据，不能把 done 或一句「通过」当作验证过。

服务重启只标记中断，不自动重跑工具或消费旧消息；显式新消息可恢复有完整记录的本机孩子。远端 runner 和外部编码 Link 尚不具备本次续聊保证，不能把它们当成本机路径。
