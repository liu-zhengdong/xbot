# 技能库去掉主人路径段

候选真源：主人 Obsidian `正在干什么/06-XBot/候选/技能库去掉主人路径段.md`。Ultra 2026-10-26 23:59 排期；执行官并行编码。

## 对照回顾

Agent 与设备绑定后，路径再嵌 `<owner>` 无产品意义。现状 `~/.xbot/skills/<owner>/<slug>/SKILL.md`；目标扁平为 `~/.xbot/skills/<slug>/SKILL.md`（任意数据根下的 `skills/` 同理）。导入三源 IA、人设卡 OwnerDir、云端共享库均不在本条。

## 实现规格与怎样算对

| 项 | 规格 / 验收 |
|---|---|
| 路径 | `skills/<slug>/SKILL.md`；`OwnerDir` 语义改为实例级技能根（或等价 API），调用方不再传 owner 拼路径。开发根 `~/.xbot-dev` 同理。 |
| 迁移 | 一级已有 `SKILL.md` 保留；旧 `<owner>/<slug>/` 提升到 `skills/<slug>/`。同名冲突：较新 mtime 留在目标 slug，另一侧旁路改名 `<slug>__from_<owner>`；写迁移日志；幂等；`~/.xbot` 与 `~/.xbot-dev` 各跑。 |
| 导入/API/CLI/测试 | `xbot skill import`、设置页导入、API list/import、测试夹具、文档示例路径一并改扁平。 |
| 可见性 | **废**「单主人才见库 / 多主共用无库 / 无主无库」。本机/本数据根所有本机 Bot 见同一份库。**保留**群无库、`disabled_skills` 按 Bot。 |
| atlas | `ecosystem.md` / `machines.md` / `ui.md` 对齐；文案「主人库」→「技能库/本机技能库」。kernel/link 工具描述里 `~/.xbot/skills/<owner>/` 改扁平。 |
| 样例 | 旧 `skills/u1/foo` + `skills/u2/foo`（u2 较新）→ `skills/foo` 来自 u2，`skills/foo__from_u1` 旁路；已扁平 `skills/bar/SKILL.md` 不动。 |

## 非目标

不引入云端共享库；不重做导入三源 IA；不改人设卡 OwnerDir；不改无关文件；不 push。相关包 `go test` 一遍绿即可（不要 `-count=3`）。

## 还没做的

独立复审、合入 main、设计师逛 UI 文案面、docs/dev 毕业。
