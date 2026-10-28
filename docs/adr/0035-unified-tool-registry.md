---
kind: adr
guard: owner
status: Accepted
date: 2026-10-28
---
# 0035 统一工具注册中心与代理门（对齐 Pi 的 mcp 代理）

## 背景

政东 2026-10-28 拍板：Bot 能调的工具要有一张统一的表，渐进披露直接抄 Pi 默认那套（一扇代理门 search → describe → call）。之前 `mountedTools` 靠 `namedLink` 一路 if 拼装；`expand_tool_group` 和 `links load` 都是把成员 schema 中途加进 `tools[]`，每加一次前缀缓存就失效，工具数量也越来越多。产品口径见 `docs/dev/tool-registry-progressive-disclose.md` 和 `docs/dev/native-vs-links.md`。

## 决定

1. **一张注册表**（`internal/kernel/registry.go`）：本轮能调的每个工具登记一次，字段有 id（工具名）、source（`kernel` | `organ` | `link` | `mcp:<服务器>`）、group、expose。内核自己的工具写在声明式表 `kernelTools` 里，每行一个分组加一个开启条件；已挂载的 Link 各自登记自己的工具。没有别的地方再往里追加工具。
2. **`tools[]` 固定、短**：只声明代理门 `tools`、`send_message_to_user`、`todo`（模型开了才有）和 hot 名单（各个壳、`read_file`、`write_file`、`edit_file`、`web_fetch`）。其余工具都是 listed：可以调用，但不声明（`tools.Undeclared`）。
3. **代理门照抄 Pi**：只有一个元工具 `tools`，分三种动作。`search` 返回一份短列表（id、分组、一行摘要）。`describe` 返回完整说明和 JSON schema，放在工具结果里；描述一个分组时列出组内成员，如果这组 MCP 还没启动，顺手把它启动起来。`call` 带 id 和 args，由 agent 层转发（`tools.Forwarder`）：从这一步起，审核、确认卡、拒绝表、后台生命周期看到的都是被调用的那个工具，跟直接按名字调用一模一样。三种动作都**不改 `tools[]`**。
4. **短清单放在可变段**：环境流的字段 `tool_index` 按分组列出 listed 的 id 和还没启动的 MCP；`tools` 字段只列已声明的工具。装上或卸下 Link 只会改 `tool_index`，稳定前缀不动。
5. **原生与 Links 的刀口**：功能是自己实现的，算原生（organ 或 kernel），开机就登记；依赖外部的，算 Link（商店的 grant 或 MCP），装上后进同一张表。电脑操作是 organ（`link.Organs()`），每个 Bot 开机就挂上，不是商店卡，也不是目录里的 grant。浏览器依赖 Chrome，所以是商店卡（`StoreApps`，装之前要探测）。目录里的 MCP `fetch` 标成 `retired`：不在目录里，不在商店里；旧的 `mcp.json` 里还有这一条的，既不挂载，也不算已连接。
6. **不抄的部分**：Pi 的 `deferred` / `directTools:"search"`（激活后把工具加进 `tools[]`）、codemode、用 id 黑名单或命令指纹区分原生和 Link。
7. **`expand_tool_group` 删除**，`bot_profile` 只剩一个注册表分组名。`links load` 保留，用来启动 MCP、列出工具 id，同样不改 `tools[]`。

## 后果

- 每次请求的 `tools[]` 只有 10 个左右，一轮里多次 describe、call 之后按字节比也不变（`TestDoorKeepsToolsBytesStableAndCalls`）。
- 主线的模型请求不再声明 MCP 的 schema，MCP schema 降级只对子代理还有用。
- 子代理没有代理门：它的工具集仍然是全部已加载的工具，都声明出来。懒加载 Link 的工具标成 `deferred`，等子代理自己 `links load` 之后才进它的工具集，和以前一样。
- 模型如果不经代理门、直接按名字调用一个 listed 工具，agent 照样执行（注册表里有）。供应商不认识这个名字，是供应商那边的事；主路径是代理门。
- `quiet` 这一档暂时没有用户，没有实现，等真有需要再加。
