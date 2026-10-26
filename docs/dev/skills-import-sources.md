# 技能多源导入

> **结论先行**：编码已合本地 main（`843d33a` 起，glyph/竞态修至 `ddf35f0`；当前 tip 祖先含之）。本文件只挂到设计师 PASS / 收口，然后删。

候选：Obsidian `候选/技能多源导入.md`（status 实现中）。  
设计真源：`设计/技能导入三源IA.md` · box/Mini `xbot-ui-design/skills-import-sources/`（ia · handoff · hifi shots 01–05）。

## 已落地（勿重做）

1. 「导入」→ 嵌层 A「从哪里导入」四行：Agent → Codex → Claude → 本机文件夹；底栏仅取消。
2. Agent/Codex/Claude → `GET /api/skills/sources/{id}` 列表；多选「导入所选」；空源「这边还没有可导入的技能」+「换个来源」。
3. 本机文件夹走原 File System Access / webkitdirectory 路径。
4. 同名 409 → 「库里已有同名技能…覆盖 / 取消」。
5. 实现路径（勿露 UI）：`~/.agents/skills` · `~/.codex/skills` · `~/.claude/skills`。
6. Glyph：Claude 星芒 `#D97757`；Codex Blossom；Agent bot outline；文件夹 folder；来源行盒 44 / 标 28。
7. 文案禁 Cloud / 路径 / SKILL.md / slug（用户可见面）。

## 怎样算对（设计师逛）

对照 handoff 验收 1–12；非目标勿回归。漂亮细节余量非阻断。

## 底线

不 push；不重开编码 wt 重写已合入面；不与 public-ui / context-batch-c 热文件硬撞。

## 还没做的

设计师 PASS → 候选已完成 → 删本文件（必要时 atlas/guard 一句）。
