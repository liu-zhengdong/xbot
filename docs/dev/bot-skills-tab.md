# Bot 技能标签

## 授权与级别

M 档。用户于 2026-11-04 请求技能 Tab 原型，并在收到可点击原型后说“继续”。本轮按确认原型接入正式页面。

UC 来源：用户 Obsidian 的「06-XBot/设计/Bot技能标签原型」。原型任务目录为本机 `.visual-design/bot-skills/`。现行设计标准与界面守护继续有效。

## UC：查看当前 Bot 的技能

**需求意图：** 用户在 Bot 工作区查看这个 Bot 当前有哪些技能，以及每项技能的用途和内容。
**参与者：** 有管理权限的 XBot 用户。
**触发条件：** 用户点击收件箱右侧的“技能”。

**主流程：**
1. 用户打开技能标签。
2. 系统列出当前 Bot 已启用的技能。
3. 用户按名称或用途搜索。
4. 用户打开一项技能。
5. 系统显示只读正文和可展开的 Markdown 原文。
6. 用户返回列表或聊天。

**成功后置条件：** 用户能确认当前 Bot 的技能及其内容，聊天状态仍保留。

**怎样算对：**
- 技能位于收件箱之后，支持既有更多菜单显隐和键盘导航。
- 列表只显示 disabled=false 的真实技能；名称优先 title，其次 name/slug。
- 搜索覆盖名称、用途和 slug，英文忽略大小写；输入法组合期间不筛选。
- 详情来自已有正文接口。正文复用产品 Markdown 渲染；原文用纯文本。
- 列表与详情失败可重试；搜索不被清空。
- 切 Bot 同步清理视图，旧列表或详情回包不能填入新 Bot。
- 手机单页详情可返回；搜索、列表阅读位置和聊天草稿保留。
- 管理技能进入现有全局设置；关闭后重读当前 Bot 的启用状态。
- 返回聊天保留原消息 DOM、草稿、附件、引用和阅读位置。

## 对照回顾

| 依据 | 处理 |
| --- | --- |
| 原始需求和确认原型 | 增加 Bot 侧查看入口，不复制原型示例内容到产品 |
| design.md、atlas/ui.md、guard/界面.md | 复用记录页、双行 64px、灰阶、Lucide 与 token，不改既有规则 |
| atlas/ecosystem.md、API.md | 复用 Bot 技能列表和正文接口；共享库与按 Bot 停用语义保持 |
| 既有技能设置及技能主名、只给你用、导入等候选 | 复用名称投影和管理入口，不增加另一套导入或启停 |
| ADR 0021、0042、0074 | 只读投影，不增加环境账或唤醒；复用工作区生命周期和公共控件 |
| 六条基本旅程、界面守护 | 聊天保持原 DOM 与 SSE；新标签不进入群组和往来页 |
| LESSONS 界面交互 | 新资源接真实 Server；用实际装配页面检查，不以静态源码断言验收 |

旧 bot-self-skill 执行稿中的 owner 目录与模型工具名已被 atlas 取代。本轮不恢复旧机制。

## 调用方与结构

```js
// 工作区拥有选择和导航，模块拥有技能读取、搜索和详情。
botSkillsView = XBotBotSkills.create({
  botName: () => selection.id, api, el, icon, appendMarkdown,
  current: () => state.sel === selection && !state.peer,
  manage: () => openSettings("skills"),
});
body.replaceChildren(botSkillsView.root);
// 用户进入标签或从技能设置返回时刷新。
botSkillsView.refresh();
// Escape 先收详情；切 Bot 释放本页。
botSkillsView.closeDetail();
botSkillsView.dispose();
```

选择独立 `bot-skills.js/css`，沿用子代理视图的工厂模式。模块仅返回 root、refresh、closeDetail、dispose。app.js 保留唯一 Bot 选择和标签状态。技能名称投影复用 skills.js 已有 displayText，不另建名称规则。

备选为把全部逻辑内联进 app.js 的 paintPanel。它不新增资源，但会把独立请求序列、搜索和详情混进通用轮询。现有 app.js 已较大，因此不采用。

不采用复用全局设置 mount 的方案。设置页管理共享库和所有 Bot 开关，与本页只读目标不同。

接受新增两个静态资源，换取技能生命周期和状态就近归属。资源由现有 Server 和 pipe 静态来源提供。只在进入页面或关闭设置时读取，不新增后台轮询。

详情选择与列表加载分别使用序号，current 同时核对发起时的 Bot 名。切 Bot dispose 让旧回包失效；改名后 getter 使用当前 selection.id。详情回包保留标题和按钮节点，只更新正文。技能设置通过 onChanged 在成功保存后通知当前视图，不依赖设置是否仍打开。窗口缩窄与用户切页时，工作区让选中标签滚入可见范围。

## 验证

独立浏览器用例覆盖启用项、名称规则、搜索、正文与原文、错误重试、迟到列表/详情、窄屏返回与聊天状态。复用既有标签保留聊天回归与技能设置回归，相关范围一次通过即停。

真实 serve 使用隔离数据根和 fake 模型。技能从真实库导入，另一 Bot 设置不同停用状态。浏览器走真实资源路由和已有 API。最终截图采用桌面、手机和深色代表状态，保存到 `~/.xbot-build/ui-shots/bot-skills/product/`。

## 还没做的

- 用户日用验收。产品已接入；当前状态与证据见 Atlas 和里程碑。
