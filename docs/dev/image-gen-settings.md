# 图片生成设置（一批）

> **2026-10-30 合同补丁优先**：本轮按下文「生图对齐搜索」补丁实现三态与代理；前批的绑定必须对齐、不做本地 generate 和厂商生图可勾开关约束已被政东撤销。

> **前批设计记录**：设置左栏新增「图片生成」（人设卡后）；全局开/关 + 用哪家（OpenAI / Grok·xAI，均 OAuth）。未登录扁平面跳「模型与供应商」现有登录，**不重画 OAuth**。产物进工作区，**不自动进气泡**。密度对齐网页搜索。设计真源：`xbot-ui-design/image-gen/`（ia / handoff / hifi）。

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

## 生图对齐搜索：本地代理兜底（2026-10-30，L 档）

政东本轮覆盖原先「绑定必须匹配所选家」及「不做本地 generate」限制。唯一解析入口 `resolveImageGen` 在轮开始固定能力：关时两路都关；开·自动优先已登录 OpenAI/xAI OAuth Responses 绑定的 hosted（无需匹配所选家，也不依赖备用家登录或旧模型开关），没有则代理到全局所选已登录账号；开·总用设置里的家一律代理，不挂绑定 hosted。代理取所选账号目录首个 Responses 模型。

本地工具名 `generate_image(prompt)`，只在真实可用时经 `kernelTools` 注册到 `image_gen` 分组，listed，通过既有 `tools` 门发现和调用，不恢复 `expand_tool_group`。权限表归 Normal，与 hosted 生图同为模型服务能力；调用复用 `config.Resolve` 的 OAuth 传输、Responses 图片解析与 `WriteGenerated`，不收密钥，不自动挂图。原模型级图片选型保留，xAI 不传 OpenAI 选项。

**怎样算对**：沿候选中政东定的四项：hosted 不退化；DeepSeek + OpenAI/xAI OAuth 能得到 generated/ 真回执；统一注册不造空组；保存上限与挂图沿既有路径。补测关闭、未登录、跨家 hosted 优先、下一轮生效、代理失败不假成功。

**对照回顾**：候选本条由用户明确排期开工；atlas 的 Responses/OAuth/generated/ 及聊天挂图均可复用；搜索的 hosted/local 互斥可复用，图片不能绑定搜索开关。ADR 0021/0035 要求环境索引与统一注册，遵守；不新增内核概念或改写现行 ADR。guard 既有聊天/动手旅程及 hosted 冒烟保持不动。冲突面：kernel image_gen、registry 声明行及 review 分类，二批 UI 按政东转达的设计师定稿：设置「怎么画」复用搜索皮，关闭隐藏策略/供应商；详情模型行仅显示有/无厂商生图芯片，移除可勾开关和绑定对齐脚注；不动 AGENTS/CLAUDE。独立设计/编码复审由主管接手，不伪称已审。

**冒烟用例**：`e2e/smoke/image_gen_proxy_test.go`；覆盖真实 serve、HTTP 绑定/设置、Completions tools 代理门、OAuth Responses、文件及 send_message_to_user 挂图，仅厂商 scripted。

## 本轮验证

`TestImageGenResolution` 覆盖三态、跨家 hosted 优先、备用未登录、两家代理和轮快照；`TestImageGenProxyFailures` 覆盖空结果、失败回执、供应商拒权。旧相等门控恢复后 `TestImageGenGlobalGate/different_selection_uses_hosted` 红，修复绿。真实 serve 冒烟及设置/供应商 Chrome 行为三次通过；截图在 `~/.xbot-build/ui-shots/image-gen-local-proxy/`。完整检查与性能实测只记 MILESTONES，未做独立设计/视觉复审，不改 guard。

## 还没做的

- 主管独立复审；本分支禁止合 main/push。真实账号生图与用户日用待验，本轮只用 fake/scripted。
- 设计师对浅深色截图复核及用户日用验收；通过后再归档旅程，本轮不改 guard。
