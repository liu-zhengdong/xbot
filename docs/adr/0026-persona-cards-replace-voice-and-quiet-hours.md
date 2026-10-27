---
kind: adr
guard: owner
status: Accepted
date: 2026-10-26
---
# 0026 人设卡承载口吻；说话方式与免打扰整功能删除

## 背景
Bot 的「怎么说话」分在三处：`persona.md`、编辑资料里的「说话方式」（只剩语言一项，ADR 0020 保留下来的）、免打扰时段。用户改人设要去每个 Bot 的抽屉里改一大块文字，同一种口吻要在几个 Bot 里各抄一份。政东 2026-10-26 定：口吻统一写进用户级的人设卡，Bot 只选一张；「说话方式」和「免打扰」不是藏起来，是整功能删除。

## 决定
1. 用户一份人设卡库（`internal/personacard`），卡是卡名加一整块正文。Bot 配置只存选用的卡 id；选用后固定前缀里的人设就是卡正文，每轮读，改卡即影响所有选用它的 Bot。未选用仍用 `persona.md`。
2. 删除「说话方式」：配置的 `style`、`ProfileUpdate.Language`、PATCH 和 `update_profile` 的 `language`、CLI 的 `--language`、提示里的 `Language:` 行。旧配置的 `style`（含 language）首次加载时并入那条已有的「说话偏好」记忆，再从配置删掉；这一处取代 ADR 0020 的「`language` 保留」。
3. 删除免打扰：配置与 PATCH 的 `quiet_start`/`quiet_end`、`update_profile` 与 CLI 的入口、提示里的 `Quiet hours:` 行、`held.json` 暂存与「Quiet hours batch」合并、手机推送里的免打扰判断和评测里的 `held` 检查。通知在任何时刻都直接送出。旧的 `held.json` 由 serve 下一轮逐条当普通通知送出后删掉，这是唯一留下的迁移代码。
4. 旧模板里的 `style`、`quiet_start`、`quiet_end` 导入时忽略，好让以前导出的模板还能导入。

## 取舍
不把语言保留成隐藏字段：留着就会有人以为它还生效。旧数据只做一次性迁移，不留读进来再忽略的兼容开关（模板除外，因为模板是用户手里的文件，严格解码会让它们导入失败）。环境流对人设仍只报短哈希（ADR 0021），卡正文不进环境。

## 后果
旅程「主动唤醒」去掉「免打扰时段不推送」一条，冒烟用例同步删掉对应子测。
