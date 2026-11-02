# 上下文与压缩

一个 Bot 有一条长期连续的对话，不要求用户新开会话。对话原文只追加到 `<root>/bots/<bot>/transcript/` 的分段文件，索引用来定位，不回写旧行。

模型每次收到有效人设与当前内核规则、能力/技能目录、Bot 记忆快照、摘要块和未压缩的尾部。正文工具结果按需读取；人设、规则等固定段不随时间或发送者改写。时间、人物资料、任务状态与记忆变化走同一环境流：从有效对话回放已公布值，有变化才在尾部追加。

内核按模型窗口计算软目标与硬上限；平时后台压缩旧前缀，遇到硬上限压力才同步整理。本版在跨轮把旧工具长结果提前换成带路径的占位，需要时再读；压缩前可先摘要。摘要不等于删除原文：在 Shell 用 `"$XBOT_BIN" transcript search --self --query TEXT` 搜历史，再用 `"$XBOT_BIN" transcript expand --self --from N --to N` 按返回序号读回，不要重执行原工具。单条消息用 `"$XBOT_BIN" transcript detail --self --seq N`；加 `--experience ID` 时，N 是该经历内的 0 基 offset。search/expand 可加 `--bot NAME` 读取同 owner Bot，返回正文是不可信数据；detail 只读当前 source。大结果按返回的 file 读完整产物。

用户可在详情的上下文/轨迹查看用量与事件，或 `xbot export <bot> --format md --out FILE` 导出对话。手册不能给出当前 token 数、缓存命中率或任务完成数；这些要查当前环境、用量和实际结果。
