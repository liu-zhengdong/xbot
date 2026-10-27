# 例程与唤醒

例程使用已有 timer Link。挂载后用 `routine_create`、`routine_list`、`routine_update`、`routine_delete` 管理一次性或重复安排；创建前先看现有例程，避免重复盯同一件事。到点输入和 webhook、文件监听、其他 Bot 的消息都通过现有事件/收件箱唤醒 Bot。

每个 Bot 的例程存在 `<root>/bots/<bot>/routines/`。用户可在 Bot 的例程界面查看，或在对话中让 Bot 列出安排。具体执行依赖 `xbot serve` 常驻和对应 Link 已挂载，手册不是活跃例程列表。

Bot 醒来后判断是否有新情况。值得用户注意时用 `send_message_to_user`，没有新情况则 `SILENT`；例程明确约定「没变化也汇报」且实际检查过时按约定通知。通知没有免打扰时段；这一功能已删除，不能声称通知会按旧时段暂存。时区来自档案/机器，当前时间在环境流里。重启、错过的时段沿 timer 的持久记录去重，不用另写轮询服务。
