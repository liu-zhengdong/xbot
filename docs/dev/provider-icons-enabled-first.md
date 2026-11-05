# 模型与供应商：图标恢复与已启用置顶

Created: 2026-11-04

状态：已合入主分支（合并提交 `ea288326`，实现提交 `1dc6757c` + `7328493b`）。用户看到的「只有 ChatGPT 有图标」是主分支一直没合这条分支，不是单独的运行链路 bug；合并后用无头浏览器打开设置页核对，`/provider-icons/<brand>.svg` 全部返回并渲染，已启用分组置顶正常。Codex 会话为 `01a1a6eb-aa1e-7600-bc4b-cfd255123ce3`。
实施工作区：`/Users/liuzhengdong/repos/xbot-worktrees/provider-icons-enabled-first-20261104`。
实施分支：`feat/provider-icons-enabled-first-20261104`。
运行记录位于原型目录的 `swe-2-events.jsonl`，完成报告写入 `swe-2-result.md`。
变更分级：M，供应商设置页局部功能与显示修复。

## 授权与来源

用户需求：“这页厂商的 icon 怎么都没啦？还有已经激活的我觉得应该放到最上面？设计一下”。
用户确认：“可以的，这个设计稿。交给 swe-2 去干吧”。

长期 UC：`/Users/liuzhengdong/Obsidian笔记/正在干什么/06-XBot/设计/模型与供应商-图标与已启用置顶.md`。
定稿原型：`/Users/liuzhengdong/Documents/deepseek-harness/default-workspace/provider-settings-design/index.html`。
设计交接：同目录 `design.md`。
最终桌面与窄屏截图：同目录 `shots/delivery/`。
最终交互证据：同目录 `shots/delivery-interactions/`。

以最终 HTML 和 delivery 截图为视觉基准。不要使用早期 default、final 或 interactions 目录中的旧稿。

## UC：查看已接入供应商

**参与者：** XBot 用户。
**触发条件：** 用户打开设置中的“模型与供应商”。

**主流程：**
1. 系统在顶部显示已启用接入。
2. 用户通过官方图标和名称识别供应商。
3. 用户通过副行区分订阅与 API 密钥。
4. 用户点击条目进入现有详情。

**成功后置条件：** 用户能先找到现有接入，并识别同品牌的不同接入方式。

**验收标准：**
- 已配置或已登录接入集中置顶，下方目录不重复显示同一接入。
- xAI 订阅与 xAI API 密钥分别保留。
- 官方图标在浅深主题可见，内置素材无需外网请求。
- 搜索覆盖所有分组，支持名称与接入方式。
- 普通条目透明跟页，悬停和选中使用中性浅底，不显示强调外框。
- 悬停不移动图标与文字。
- 返回列表保留搜索词，键盘操作保留合理焦点。
- 没有已启用接入时直接展示添加目录。
- 无匹配时提供清除搜索动作。
- 窄屏全屏展示，保留设置目录、关闭入口和独立滚动。
- 原登录、密钥配置、自定义供应商、模型管理及 Bot 换绑不退化。

**扩展流程：** 登录或保存失败保留原状态与输入；配置成功后更新分组；错误接入保留在顶部，沿用现有状态处理。

## 实施范围与已确认事实

主要入口为 `internal/web/static/providers.js` 和 `providers.css`。读取项目 `AGENTS.md`、`design.md`、相关 Atlas、接口与测试约定后实施。

图标素材仍在 `internal/web/static/provider-icons/`。`glyph()` 对已知品牌创建 img，请求 `/provider-icons/<brand>.svg`。ChatGPT 是例外，使用 CSS mask。服务器路由 `internal/web/server.go` 的 `/provider-icons/{brand}.svg` 与 `internal/web/ui_static.go` 的 `/api/ui/static/{path}` 均在；xbot-account `resource-worker.js` 将 `/provider-icons/*` 映射到 pipe 的 `/api/ui/static/provider-icons/*`，链路完整。两轮均未能复现用户侧图标失效，根因未定位；可能是旧二进制或服务工作者缓存的旧资源，但本轮没有取证确认，不声称已修复，也未添加无证据的 fallback。

`entries()` 只排除了 ChatGPT OAuth，没有排除 xAI OAuth。xAI 订阅因此重复进入 API 目录。按真实接入身份去重，不能按品牌名合并订阅与密钥。

状态从现有 `key_configured` 推导。不要新存一份启用或置顶配置。组内保留稳定顺序。绿点表示现有登录或密钥配置状态，不宣称连接测试成功。原型的四条启用项是设计数据，真实产品必须使用实际数据，不得硬编码数量。

## 视觉与交互

页面为“已启用”和“添加供应商”两区。已启用项显示官方图标、名称、接入方式与状态点。下方保留未接入的订阅和 API 目录；只有 API 时不重复显示分类标题。保留添加自定义入口。

保留现有设置壳：176px 侧栏、28px 内容内距、18px 内容面圆角。页标题 24px、区块 16px、名称与设置操作 14px、说明 12px。搜索高 36px，双行项目高 64px。双列网格在窄屏折为单列。

搜索位于标题下方并占内容宽度。最终稿没有页首导语。普通条目不铺灰卡底，不加描边。保留官方品牌色的小面积使用。悬停和当前项复用 `--x-fill-2`。

视觉值来自项目 tokens。原型的 HTML、Alpine 和模拟状态不是产品实现架构。复用产品现有 JS、CSS、登录和详情。详情留在现有右侧内容面，不新增遮罩弹窗，不把原型的身份展示页替换成完整产品详情。

首次出场、按下与详情切换沿用项目动效。搜索即时过滤，不加交叉淡化。尊重 prefers-reduced-motion。

## 对照与边界

已核对长期候选中的“供应商唯一实体”“供应商展示名统一”、现有供应商页与卡选中裁定。本次明确改变顶层分类、普通条目底色和搜索宽度，保留接入身份、注册、协议和密钥契约。

不新增后端排序接口、启停开关、手工拖动、批量操作或最近使用排序。不改 OAuth 协议、密钥存储和 Bot 绑定行为。

交接前主工作区已有其他任务的未提交变更：`docs/atlas/ui.md`、`internal/web/aside_browser_test.go`、`internal/web/static/app.css`、`app.js`、`tokens.css`。另有未跟踪的 `.playwright-mcp/` 和 `.review-ff19ac5-acct-fullwidth/`。不要覆盖、回滚或一并提交他人的变更。需要隔离时使用独立 worktree，提交前协调共享文件。

## 验证与交付

按 `docs/TESTING.md` 选择最小有效范围。优先扩展已有供应商浏览器回归，验证行为，不新增检查源码字符串的测试。

1. 在真实入口复现并验证图标修复。
2. 验证已启用置顶、OAuth 去重与订阅/API 身份隔离。
3. 验证搜索、返回保词和配置后的分组更新。
4. 覆盖空态、长名称、浅深主题及代表窄屏。
5. 验证相关既有登录、密钥和自定义供应商流程。
6. 取得实际截图，与最终定稿对照。

测试使用 fake/scripted，不调用真实模型，不打印密钥。相关检查通过一次后停止；新改动或失败才重跑。交付时给出提交、检查结果、截图、根因依据和未覆盖项。

当前系统说明与开发记录按实施时有效的仓库规范分别更新。不要把原型验证当成产品端到端通过。用户本轮未要求发布版本或对外推送。

## 还没做的

- 用户运行链路中的图标失效未复现、未定位；headless Chrome 与新进程取图均成功，不足以证明用户环境无故障。
- 未合入 main；主工作区其他 Agent 的未提交 UI 变更保持原状。
- 未执行全量回归与 Windows/窄屏实机验证。
