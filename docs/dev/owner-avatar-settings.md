# 设置里换我的头像（M）

## 怎样算对
沿用户要求：设置入口上传后，session 给出同一头像 URL，侧栏「我」立即更新；重新打开/刷新能读回；清除后回 owner_name 线稿。存盘只有 data root 的 `owner-avatar`，不存 localStorage 图片。

## 设计与对照回顾
- 复用 Bot 的 `SniffImage`、PNG/JPEG/GIF/WebP、1 MiB 限制、multipart 上传范式。现有 Bot 实际不做压缩，本条不另加压缩管线。
- session 增加 `owner_avatar_url`；GET/POST/DELETE `/api/owner/avatar` 共用现有认证/同源检查。固定文件原子替换，类型从字节判断，不另存 MIME 或状态副本。
- UI 沿用通用 setpane，外观前放「我的头像」：左预览，右「换一张」与有图才显示的「清除」。沿用 setrow/按钮/token 与 Mona/XBot SC 字体，不引入颜色、描边和新动效。
- 参照 taste 的 Qoder profile（去边框、靠面与留白）及 appearance（直接看预览）；与 atlas 统一设置、guard 界面一致，不改 Bot 上传或账号资料。
- 用户已授权直接按本机视觉技能出皮落码；局部自检，不委派代理。

**冒烟用例**：`internal/web/owner_avatar_test.go`、`internal/web/owner_avatar_browser_test.go`

## 还没做的
- 用户日用确认；不移动到 guard，不合 main、不 push。
- 全量门禁有仓库原有失败，详情见 MILESTONES；不在本条扩修。

## 验证入口

实现与接口现状已提炼进 atlas/ui.md 和 API.md。专项 HTTP/Chrome 三遍通过；真实二进制执行 `node internal/web/testdata/owner-avatar.cjs http://127.0.0.1:<隔离端口>`，要求单独数据根，serve 禁止自动升级、不连接真模型。可用 `OWNER_AVATAR_SHOTS` 指定证据目录。截图在 `~/.xbot-build/ui-shots/owner-avatar/`；自检而非独立设计师验收，无新动效或冗余说明文案，不改既有视觉规则。
