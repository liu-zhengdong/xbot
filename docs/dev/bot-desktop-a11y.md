# Bot 控机：Mac / Windows 无障碍树

档位 L（器官、工具合同、权限分类与设备设置）。本次用户明确开工、由本 Agent 自行设计验收；不等设计师。现存多份 legacy dev 不在本次冲突面内，不擅自收尾其它特性。

## 怎样算对
沿用户候选：可见 App snapshot 取得 qualified refs → click/type → 再 snapshot 看变化；缺权限明确失败；设备设置显示 Mac 授权状态和引导、Windows 完整性说明。不做手机桌面流、跨人遥控、聊天壳替换。

## 用法与形状
- tools 加载 desktop：desktop_snapshot {app:"TextEdit"}；desktop_click {ref:"@snapshot:e1"}；desktop_type {ref:"@snapshot:e2",text:"hello"}；再次 desktop_snapshot。
- CLI 同合同：xbot desktop snapshot TextEdit；xbot desktop click @ref；xbot desktop type @ref hello。
- 唯一装配：link.Organs → kernel 的 desktop.Open(root) → 原 Link.Tools。Perform 共享机器开关与 idle 门，再由平台 driver 选择官方 CLI 或 Linux 像素驱动。
- CLI 输出最大 1 MiB、超时 15 秒；只解 envelope 的 ok，保留原树、qualified refs、error.code/disposition。不重试、不把投递成功当效果验证。
- Mac 语义动作；Windows type 明确使用 headed（上游 type 必需），不从失败偷偷降级到坐标。另提供 set-value 语义替换。Mac/Win 不支持旧坐标点击，报错；Linux 旧路径保留。
- 设置沿用设备/这台电脑透明行：电脑操作、授权状态、短引导、重新检查。只读 status，不主动弹系统权限。安装缺失显示安装命令；授权由系统手动完成。

## 方案选择
A 扩展原 Action/Perform，native driver 接官方 CLI，原工具增 ref 参数（选用）。共享一条策略缝，ref 生命周期交给上游。
B 新 Accessibility Link 与独立动作层（否决）：重复装配、权限、状态，违反同一器官要求。
接受平台细节不统一换来薄适配；官方 CLI 单独安装，不 fork、不自动下载。

## 对照回顾
| 材料 | 结论 |
|---|---|
| 候选 Bot控机、用户本轮全部 1–5 条 | 同一器官、Mac/Win refs、状态可见、Linux 保留；无新增产品机制 |
| atlas machines / ecosystem | 复用原生器官与 tools 注册，不进商店 |
| ADR 0003 / 0019 | 新观察归 Readonly、动作 Normal；MachineTouching；不新增权限卡；原 machine.Get 与内核严格模式保留 |
| ADR 0021 | 无新后台状态或账本，snapshot 作为普通工具结果 |
| guard 界面 | 局部沿用 tokens 和设备行，不加卡框/审批，仅读状态不抢焦点 |
| LESSONS | 外部合同已实探；status 与动作同一个 CLI；测试在器官、进程、HTTP 和浏览器入口，不以静态文字断言代行为 |

## 验证
相关包 go test -count=1；器官注册与 review 分类；fake CLI 记录真实 argv、跨调用 snapshot→动作→snapshot、错误/超限；真实 CLI 缺权限失败；设备 API 与浏览器交互。只用 chrome-headless-shell。
设置为存量局部添加，继承白纸方向与审美样本 018 的少条目灰阶，无新增动效或样式体系；主 Agent 自检，未独立评审（无独立执行工具）。

## 还没做的
真实 App 正向验收尚未通过：当前 Mini 的无障碍授权为 denied，需系统手动授权后再跑真实 snapshot → ref 动作 → snapshot；当前只有 scripted 外部桌面的真二进制/HTTP 冒烟通过，不能替代真实 App。Windows 缺实机，仅通过平台驱动合同、npm 原生路径解析与 amd64/arm64 交叉构建。实现与本地自动检查已落地，证据见 MILESTONES；不等用户逛设置验收，也不将这些缺口写成已通过。
