---
kind: adr
guard: owner
status: Accepted
date: 2026-10-17
---
# 0006 记忆只留人和 Bot 两层

## 背景
记忆原来有三层：person（所有 Bot 共享）、bot（这个 Bot 自己）、bot-person（这个 Bot 和这个人）。这是一个单主人系统。三选一没有默认，开发角色的 Bot 把 19 条开发规则写进了共享的 person 范围，别的 Bot 也被注入了这些规则。

## 决定
- 只留两层。person 只放每个 Bot 都需要的、关于主人本人的事实（时区、语言、作息、他是谁），写入必须显式指定 scope。bot 是这个 Bot 自己的角色、约定和项目决定，也是省略 scope 时的默认：`remember`、自动抽事实、不可信回合的自动写入。
- 删掉 bot-person。已有的 `bots/<name>/people/<id>/memory/` 在加载时并进 `bots/<name>/memory`，去重，保留时间戳、来源和原来的 kind。空 kind 不改写成 work：冲突规则里它仍按「关于人」处理，最新的一条为准。profile 超过 4 KiB 时把溢出放进日志层并写明。迁进日志时 `log.md` 留到大约 48 KiB，新的事实留在文件里，更旧的写到 `log.overflow.md`，检索和 forget 把它和日志一起读。上面还留着大约 16 KiB，给以后的 remember。旧目录留备份。不丢事实。
- 只合并该 Bot 的 `owners` 里的人。Bot 记忆对每个发送者都可见，所以其他人的目录不并进去，改名为 `memory.bak`，并记一条日志。没有主人的 Bot（`owners` 是空的）不把旧目录并进 bot，旧事实只留在 `memory.bak`。`config.json` 缺失、读不了或不是合法 JSON 不算「没有主人」：这个 Bot 的旧目录留在原地，不写 `bots/<name>/.memory-migrated`。某个目录读失败同样留在原地。不挡住别的 Bot，也不让 Remember、Recall、Forget 或启动失败。没有数据根上的总标记。每个 Bot 干净迁完写自己的 `bots/<name>/.memory-migrated`。进程内每个数据根走一次；下次启动还会看还没有标记、或者标记之后又出现旧目录的 Bot。
- 冲突：怎么做事以 Bot 自己的记忆为准，压过 person。同一个键上，可信的 bot 事实压过更新的不可信事实。
- 注入预算从三份 profile 改成两份（`2×4KiB+512`）。
- 人的跨设备同步日志仍是 person 通道。那份日志本来就只记 person，空 scope 在那里不是「省略后的默认」，不改成 bot。
- 只有以后真要多人使用时，才加回按人分开的范围。

## 考虑过的方案
- 继续保留 bot-person，只把默认改成 bot：三选一还在，模型仍会把工作规则写进共享层。
- 把 person 改成只读、禁止模型写入：主人自己的时区也写不进去。

## 后果
跨 Bot 共享的只有主人本人的少量事实。每个 Bot 的工作约定留在自己的目录里，而且这份 Bot 记忆每个发送者都能看见，所以迁移只并主人自己的旧目录。网页档案读 person 和 bot 两层。
