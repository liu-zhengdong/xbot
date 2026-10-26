# 生图体验热修 + 头像当面刷新 + tip 文件（一批）

> **结论先行**：修设计师真机 PASS 后的四条体验尾巴（开关视觉/绑家提示/气泡残片/xAI 登录文案），并同波修 update_profile 侧栏当面刷新与 `bin/xbot.tip` 脏写。图仍只进工作区、不进气泡。不做工作区可发现、简介进厂头、气泡工具名。

## 背景

设计师真机 PASS（c91e7dd / Grok OAuth）。证据 `xbot-ui-design/review/imggen-live/`：`isOn:false` 但「用哪家」在且 API `enabled:true`；气泡残留 `!简笔画小猫坐在窗台上`；工具回执写 next wake。`~/.xbot-build/dev/bin/xbot.tip` 仍脏写 `aab566c`，与 `build-info`/`8788` 不一致。

## 怎样算对

1. **开关**：`enabled` 真则 `.tog.on` + `aria-checked=true` 且「用哪家」可见；关则相反。浏览器测断言 class 与 aria 与 section.hidden。
2. **绑家提示**：全局开但当前 Bot OAuth ≠ 所选生图家时，有人话说明为何不挂工具、去「模型与供应商」改绑定或改「用哪家」。
3. **残片**：助手气泡不再出现 `!简笔画…` 半截图语法（设计仍是图只进工作区 → **剥残片**，不挂气泡图）。
4. **xAI 未登录钮**：从未登录过的主 CTA 写「登录」（对称 ChatGPT）；过期/失败重登可仍「重新登录」。
5. **update_profile**：工具描述与回执禁止 `applies on the next wake` / 「下次唤醒」；落盘后面栏/顶栏头像当面刷新（流式中也要，勿等 3s poll / 下次唤醒）。
6. **tip**：`scripts/xbot-dev` 每次 build 写 `~/.xbot-build/dev/bin/xbot.tip`（短 HEAD）与 `build-info` 同值；重启后 tip == `git rev-parse --short HEAD` == 8788 构建。

## 不做

工作区侧栏找 generated/；简介进厂头；气泡工具名改写；push。

## 测试

相关包 `-count=1`：`./internal/config` `./internal/web` `./internal/kernel`（及 tip/脚本测若有）。浏览器测覆盖开关 class。不改 CLAUDE.md / AGENTS.md / lock / 发版脚本。

## 还没做的

- 执行官复核现场开关记录：基线 c91e7dd 在本机 Chrome 已通过限定图片设置面板的 class、aria、hidden 和计算样式断言，未复现记录里的失同步；全局首个 `.tog` 属于隐藏的手机推送面板，需核对原采样选择器。box 原始证据暂不可读。
- 执行官合入、构建和重启 8788 后核对真实运行版本、build-info 与 xbot.tip；本分支不重启、不推送。
- 设计师复核本批浅深色截图；真实账号端到端与主人日用级别未提升，不催主人手测。

**冒烟用例**：`internal/web/image_gen_browser_test.go`、`internal/web/image_gen_bubble_test.go`、`internal/web/profile_refresh_browser_test.go`、`internal/web/xai_ui_test.go`、`scripts/test_xbot_dev.py`。
