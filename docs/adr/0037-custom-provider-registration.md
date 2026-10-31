---
kind: adr
guard: owner
status: Accepted
date: 2026-10-31
---
# 0037 自定义供应商复用唯一注册表

用户要求设置直接收 Base URL 与 key。自定义项继续登记在 providers.json，复用 config.Add / Resolve、env 文件及 held 密钥路径，不创建独立 store。稳定配置名与可选展示名分离，匹配内置 host 也不合并条目。

config.Add 允许先注册无模型端点，保存后才读取实时目录，模型选用沿原登记与绑定路径。host 推断只作为创建/编辑默认值，保存明确协议，未知 host 为 Completions，手动选择优先；旧 CLI 协议规则保留。

本次用户明确允许任意 HTTP Base URL，设置显示灰字建议 HTTPS，配置校验保留绝对 http(s)、无用户信息/查询/片段的约束，移除仅私网 HTTP 限制。此决定仅适用于模型供应商，不放宽搜索出站与 OAuth 地址规则。
