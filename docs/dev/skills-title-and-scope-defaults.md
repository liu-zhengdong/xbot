# 技能列表主名 + 只给你用默认开关

候选：`技能列表主名.md`、`只给你用默认开关.md`（同波同 wt）。
设计半页：box `xbot-ui-design/skills-followups/` · Obsidian `设计/技能主名与只给你用.md`。

## 主名

- frontmatter **`title` = 人读短名**；**`name` = slug**。
- 列表/详情标题优先级：`title` → 可读化 `name`/文件夹名 → **永不**长 `description`。
- `description` 只做副行/详情。
- `write_skill` 模板/参数写出 `title`（示例「记一下」）。

## 默认开关

| 当轮意图 | 落库默认 |
|---|---|
| 「只给你」「只给 Astr」「别给别人」等可解析 | **只开当前对话 Bot**，其余关 |
| 推断不出 / 没说范围 | **全开**（钉死；不做「未指定」第三态） |

只影响落库那一刻；主人事后改开关不变。不推翻本机共享；不做假物理隔离。

实现：`write_skill` 可选 `scope`/`only_for`（`self` / bot 名 / 省略=全开）；服务端只信显式字段。导入无范围 → 全开。

## 怎样算对（验收人话）

1. 有 `title`：列表第一行短名，不是 description。
2. 无 `title`：主名是可读 slug/文件夹名，不是一整句说明书。
3. `description` 不当标题。
4. `write_skill` 写出短人名（如「记一下」）。
5. 说了「只给你用」再装：当前 Bot 开、其余关。
6. 没说范围：仍全开。
7. 事后改开关仍灵；五条热修文案不回退。

## 底线

错开账号菜单热文件（`app.js|css|index.html` 底栏）；不 push；不改 CLAUDE.md/AGENTS.md/lock。

## 还没做的

独立复审、合入、8788 真实入口与设计师验收、主人日用。
