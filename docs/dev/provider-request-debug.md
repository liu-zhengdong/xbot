# provider 请求 debug 落盘（dev）

候选：`候选/provider请求debug落盘.md`（实现中）。

## 目标

运行时可动态开/关：开启时，每次发往模型供应商的**原原本本请求**落盘为 JSON（含原始密钥；禁止 redact），形态对齐 rqv；关闭则不写。默认关。

## 验收

开 debug → 聊一句 → 落盘文件 → `rqv` 能打开看构成（与真请求一致，含密钥）。

## 底线

默认关闭；**必须含原始密钥，禁止脱敏**；禁 sync-app；不 push；编码克制。

## 实现切法（M 档）

每次请求检查 `~/.xbot/provider-debug.enabled` 普通文件是否存在，创建即开、删除即关，无需重启或重编译。使用与默认数据目录相同的 home 解析方式；开关按本机用户共享，不随 `--root` 改变。共用小函数在三种协议的 HTTP 发送前写入已有编码字节，不重新编码，不调用 redact，不记 header。文件置于 `~/.xbot/provider-debug/`，时间、协议、独占随机后缀区分；新目录 0700、文件 0600。写入失败只报错日志，不影响模型请求。

## 对照回顾

| 材料 | 结论 |
| --- | --- |
| 本条候选及主人验收 | 文件热开关避免环境变量无法从进程外热更新；rqv 接收原始 messages/tools |
| atlas/providers | 复用三个适配器已编码的 body；不加另一套协议序列化 |
| atlas/security、ADR 0013 | 按主人最新纠正，debug 是显式开启的原文诊断例外；不改 vault、常规日志或 transcript 的脱敏 |
| guard/六条基本旅程 | 默认只读一次开关；不改变回复、工具执行或 transcript |

**冒烟用例**：`~/.xbot-build/provider-request-debug/smoke/smoke.py`（独立代理编写，scripted 供应商 + 真二进制 + rqv）。

## 还没做的

- 主人日用验收、独立复审与合入；未获授权移动 guard。
