# 底栏账号菜单定稿落码

2026-10-26 · 政东经设计师拍板 [t415u]

真源：`~/.xbot-build/account-menu/design/`（`handoff.md` §账号菜单 · `shots/07-account-menu.png`）  
多设备整包仍见 `docs/dev/multi-device.md`；**本切片只改底栏账号弹出菜单 UI**。

## 背景

现网 `#account-menu` 太丑：icon 有无不一、分组乱、「账号 / 网关」斜杠双名、关于+dev 抢戏。

## 怎样算对

严格对照 `07-account-menu.png`：

1. **行行左 icon** 齐列（settings / sidebar / monitor|device / person / logout / info）
2. **分组**（发丝分隔，勿大块空白 mgap 当主分隔）：
   - 设置类：设置 · 收起列表
   - 账号设备：设备（右 meta 台数）· 账号（右弱化「网关」，**勿**「账号 / 网关」）
   - 危险：退出登录（danger 字色 + 红/err icon）
   - 页脚 quiet：关于 XBot + 右 `dev`/版本（弱化）
3. 「我的设备」收成「设备」；白纸、禁紫
4. 现有点击行为保留（设置抽屉 / 收起侧栏 / 设备抽屉 / 账号网关 / 退出 / 关于）
5. 假开关不算过：菜单结构真改；台数来自真实已配对/已绑设备数（已有数据源则复用；没有则不硬编假数，可空/隐藏）

## 落点

- `internal/web/static/index.html`（`#account-menu` 结构）
- `internal/web/static/app.css` / `devices.css`（分组、danger、quiet、icon 齐列）
- `internal/web/static/app.js`（补各行 icon、台数 meta、collapse 文案时勿丢 icon）

## 不做

- 完整多设备同步协议 / 侧栏设备组 / 设置·设备大改
- 假设备列表、假台数
- 碰 `feature/agent-todo-tool`、批 B、生图热修

## 验收

相关前端/web 测试绿；encode-report 写清 diff 与对照 shot 的结构点。
