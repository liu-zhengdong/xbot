---
kind: adr
status: Accepted
date: 2026-11-01
---
# 0048 同一 desktop 器官适配官方无障碍 CLI

Mac / Windows 控 App 使用官方 agent-desktop CLI，Linux 保留 xdotool / ImageMagick。不 fork 上游、不新增 Link 或商店项。唯一装配仍为 link.Organs → kernel desktop.Open → Link.Tools，CLI 和模型工具共享 Perform 的机器开关及 idle 行为。

snapshot 返回上游 qualified ref，后续动作按 ref，随后重新 snapshot 核验；不保存第二份 ref 映射，不猜坐标、不重试不确定投递。原 JSON envelope 的错误码和 disposition 保留。Windows type 按上游合同显式 headed，set-value 可语义替换；不从语义失败自动降级物理输入。

权限沿 ADR 0019，不新增确认；观察工具归 Readonly，动作 Normal，均登记 MachineTouching。设备设置只读同一个后端 status，Mac 显示授权与手动引导，Windows 显示完整性限制。上游 CLI 为可单独安装的运行依赖（PATH 或 XBOT_AGENT_DESKTOP），缺失明确报错。
