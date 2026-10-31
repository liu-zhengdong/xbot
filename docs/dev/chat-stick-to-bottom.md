# 主列 stick-to-bottom

档位：M。用户已授权行为合同；不改视觉、服务端窗口、800 raw 保尾，不 push、不合 main、不重启 8788。

## 怎样算对

沿用用户合同：打开/首次/latest 贴底；用户上翻离开阈值解除 stick，新尾不抢位置；stick 时追加、流式和轮询贴底；prepend 在分页控件完成后保视觉锚；图片/语音和控件异步增高仍贴底，解除后不拉回。历史尾窗与 stick 状态一致，回最新抵达真尾。

## 对照回顾与方案

复用 atlas/ui 的 keyed patch、条件请求与历史分页；保留 adfd8eb8 的控件先 paint 后校锚。guard/界面不变，无视觉增项。仅前端滚动状态，不加上下文或后台机制。统一控制器持有 stick、rAF 调度、ResizeObserver 与锚点；其余调用方不自行计算 near。历史搜索显式离底，latest 显式重置；有缺口时继续显示回最新。

## 冒烟用例

`internal/web/chat_stick_browser_test.go`，复用现有真实 Chrome + scripted HTTP fixture；并跑 transcript/window/live/语音浏览器回归。修复需在旧 app.js 上验证新回归失败。

## 验收状态

专项浏览器行为与窗口/流式/语音回归三遍通过；新测试回退旧 app.js 确实在首次贴底断言失败。最终 prepend 锚点 3.007–3.148px；墙钟 rAF 20 帧样本 p95 22.4–22.5ms，不代表稳定 60fps。全仓与 Web 整包三遍仍有失败，详情只记 MILESTONES。

## 还没做的

- 用户 Air 日用验收与设计师截图复审；不移 guard、不合 main。
- 真实小鲸/跨设备与物理帧率验收（本次仅 scripted fixture）。
