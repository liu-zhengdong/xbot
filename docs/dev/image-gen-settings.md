# 图片生成设置（一批）

> **结论先行**：设置左栏新增「图片生成」（人设卡后）；全局开/关 + 用哪家（OpenAI / Grok·xAI，均 OAuth）。未登录扁平面跳「模型与供应商」现有登录，**不重画 OAuth**。产物进工作区，**不自动进气泡**。密度对齐网页搜索。设计真源：`xbot-ui-design/image-gen/`（ia / handoff / hifi）。

## 背景

政东要在设置里管「Bot 能不能生图、用哪家 OAuth」，与「Bot出图到聊天 / 气泡挂图」解耦。厂商 hosted 生图通路已合入；本批只做**设置面**与全局开关/供应商选择。

## 怎样算对

1. 左栏有「图片生成」，顺序：…人设卡 → **图片生成** → 设备…
2. **关**：仅开关；无「用哪家」列表与脚注噪音；对话不去画图。
3. **开**：出现「用哪家」；v1 仅 OpenAI、Grok（xAI）；中性选中（`--x-fill-3` / 近黑 radio）；无紫。
4. **已登录**：行右绿点「已登录」；脚注「画好的图会放进工作区，不会直接出现在聊天气泡里。」
5. **未登录**：行下扁平面短文案 + 「去模型与供应商」→ 打开同浮层切到该供应商详情；**禁止**本页 OAuth / 密钥表单。
6. 文案不出现 hosted / image_gen / 路径类工程词；全局一份；**新一轮对话**生效。
7. 供应商详情顶栏**不要**另加图片生成开关（本批不扩详情）。
8. 复用已有 `HasHostedImageGen` / `withImageGen` / `generated/` 落盘；设置面控开关与供应商登录态。

## 文案定稿（与 ia 一致）

| 位置 | 文案 |
|---|---|
| 标题 | 图片生成 |
| 导语 | 决定 Bot 用哪家画图。整台 XBot 共用一份。 |
| 开关 | 允许生成图片 |
| 关·说明 | 对话里不会去画图。 |
| 开·说明 | 需要时 Bot 可以画图。 |
| 分区 | 用哪家 |
| 行 meta | 已登录（绿点）/ 未登录 |
| 未登录说明 | 还没登录 {OpenAI\|Grok} 账号。到「模型与供应商」里点登录，回来就能用——不用在这里再登一遍。 |
| 未登录 CTA | 去模型与供应商 |
| 分区脚注 | 画好的图会放进工作区，不会直接出现在聊天气泡里。 |
| 页脚 | 改完后，新开的对话按新设置来。 |

## 技术要点（给编码）

### 配置

- 对齐 `web_search`：在 `WebFile`（或同级清晰字段）存全局 `image_gen_enabled` + `image_gen_provider`（`openai` / `xai`）；读写进现有 `web.json` 锁路径，勿另起配置文件。
- 默认：关；开时默认已登录的优先，都未登可选 openai 并落未登录引导。
- `GET/PUT /api/image-gen`（命名可对齐仓内风格）：返回 enabled、provider、两家登录态（读现有 OAuth/vault，勿新造登录）。
- 生效：`withImageGen`（或等价）读全局开关——**关则不挂** `image_generation`；**开且当前 Bot 绑定供应商=所选家且已登录**才挂。下一轮对话生效（与搜索同）。不要把图塞气泡。

### UI

- 新 `image-gen.js` + 必要 CSS（尽量复用 `web-search.css` / setrow / tog 类）；`index.html` 左栏与 pane；`app.js` mount/unmount 对齐 WebSearch/PersonaCards。
- 「去模型与供应商」：`openSettings("providers")` 并尽量深链到 openai/xai 详情（看 `XBotProviders.settings` 是否已有参数；没有就最小扩展，勿重画 OAuth）。
- 视觉：对照 `/workspace/xbot-ui-design/image-gen/hifi/` 三态；禁紫；扁平面引导。

### 测试

- config round-trip；API GET/PUT；UI 测侧栏顺序、关无列表、开+已登录/未登录、CTA 跳转。
- 相关 go 测一遍（不要 `-count=3`）：`./internal/config` `./internal/web` `./internal/kernel` 中与 image-gen / settings 相关的包。
- 不改 CLAUDE.md / AGENTS.md / lock / 发版脚本。

## 不做

- 本页内嵌 OAuth / 密钥
- 气泡挂图 / 自动进气泡
- 其它生图供应商、按 Bot 分份、本地 generate
- push

## 验收手测（合入后）

关→不能生图；开且已登录→可选 OpenAI/Grok；开未登录→扁平面进现有供应商登录；生出的图在工作区。

## 还没做的

- 执行官合入后验收定稿三态及两家真实账号的生图工作区结果；本轮仅分支提交、fake/scripted 与本机浏览器验证。
- 设计师对浅深色截图复核及主人日用验收；通过后再归档旅程，本轮不改 guard。
