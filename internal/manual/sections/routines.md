# 例程与唤醒

例程沿当前 source 的实际 timer 管理。先用 `"$XBOT_BIN" routine list --self --json` 看已有安排，再用 `routine create --self --args JSON`、`routine update ID --self --args-file FILE|-`、`routine delete ID --self`，均可加 `--json`。create/update只收一个JSON对象；create支持id/name/prompt/cron或at/precheck/skills，update支持name/prompt/cron/enabled/precheck/skills，省略保留、precheck空串清除、skills的[]清空。cron按机器本地时间，最小5分钟；at用带offset的RFC3339且不早于当前时间两分钟前。暂停用update的enabled:false，不要另建轮询。大列表给完整本地文件引用，按file/access/bytes读取，不能把摘要当全量。创建前先看现有例程，避免重复盯同一件事。到点仍沿原事件/唯一收件箱唤醒，CRUD本身不触发或post消息。

非空precheck（含update省略时保留的原命令）沿原source Shell审批上下文审查，明确缺原上下文时不写；不要复制过期CallID、改用别的Bot或假称已获批。precheck触发时仍重读机器gate，只allow执行，2秒硬限/无交互，非0或ask/off会消耗slot不叫醒。help离线可用；无--self的旧migrate、list --bot、update --bot --skills仍只操作离线定义，runtime错误不回落。

每个 Bot 的例程存在 `<root>/bots/<bot>/routines/`。用户可在 Bot 的例程界面查看，或在对话中让 Bot 列出安排。具体执行依赖 `xbot serve` 常驻和对应 Link 已挂载，手册不是活跃例程列表。

Bot 醒来后判断是否有新情况。值得用户注意时用 `send_message_to_user`，没有新情况则 `SILENT`；例程明确约定「没变化也汇报」且实际检查过时按约定通知。通知没有免打扰时段；这一功能已删除，不能声称通知会按旧时段暂存。时区来自档案/机器，当前时间在环境流里。重启、错过的时段沿 timer 的持久记录去重，不用另写轮询服务。
