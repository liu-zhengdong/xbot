---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0063 links install/remove 沿原 source CLI 意图卡，linksTool 整门退

## 原能力与调用方

基线 `fe6952b5`（links search/load 6a 已合 main `9bdfee25`）；本刀基线 worktree `641ff57c`。6a 之后 `linksTool`（links.go:29-108）只剩 install/remove 两个动作：Run（links.go:77-108）dispatch `install`/`remove` 走原 helper（links.go:91-102），成功后 `Reconcile(ctx)`（links.go:99-101）；Confirm（links.go:45-75）组卡 summary，install 为 `"install <id>\n<summary>\nsecrets: <names>"`（links.go:63-68）或 `connectorCard`（links.go:236-243），remove 为 `"remove <id>"`（links.go:70-71），始终 return false——真正决定要不要卡的是 review 表 `NeedsOwner` 的 links install/remove 例外（review.go:222-228）。

本刀把 install/remove 迁到 runtime CLI 并**整门退 linksTool**。Agent 沿原 source Shell 使用：

```sh
"$XBOT_BIN" links install --self <id|name|store-connector> [--json]
"$XBOT_BIN" links remove --self <id|name|store-connector> [--json]
"$XBOT_BIN" links --help
"$XBOT_BIN" links install --help
"$XBOT_BIN" links remove --help
```

调用方用原 Shell 的 XBOT_BIN、XBOT_ENDPOINT、规范 XBOT_ROOT 和当次 XBOT_CALL_ID；旧 XBOT_BOT 只诊断，显式 root 必须一致。成功返回原 `"installed <id>"`/`"removed <id>"` text（scrubSecrets 过 `secretValues()`）；错误为实际脱敏 `{error:…}`、CLI 非0。无 artifact/Applied/rollback ACK。

## 两个实际结构

**A：离线 Load 与新 Kernel。** 同 6a 落选理由且更重：install/remove 要 owner 卡、卡填 secrets stage、live `Reconcile` 和进程态 grant/deniedBefore/parkable——一次性进程里没有 prepared ctx 发卡（`runtimeApprovalSource` 需要原 live Shell 的 running call），`storeSecrets`/`EnableConnector`/`GrantLink` 写盘后运行中 kernel 的 MCP Manager 与 `k.Links` 也不会感知。离线路径只能写文件再指望下次启动生效，与「卡填完即安装生效」的原语义不等价。落选。

**B：同 K、原 source 的 runtimeApprovalSource owner 卡 + 原 helper。** 与 search/load 同一 `linksRuntime` 接缝但 ctx 层换 `runtimeApprovalSource`（卡要 owner 重绑走原 owner 动态 fork 分支；search/load 用 `runtimeSourceCall` 只要 prepared 资格）。形状：

```go
// cliops: 同 LinksLoad 形状，无 action envelope。
type LinksInstall struct{ ID string `json:"id"` }
type LinksRemove struct{ ID string `json:"id"` }
// LinksHandlers 增 Install/Remove 两个函数字段；同 {text?,error?} reply。

// Kernel handler 伪码（install/remove 同形）：
// release := k.holdOperations(); defer release()
// callCtx, cleanup, err := k.runtimeApprovalSource(httpCtx)  // owner 重绑
// defer cleanup()
// args := 原 Shell ToolArgs(callCtx) map；args["_xbot_links"] = {action,id} canonical blob
// summary := 原 Confirm 同构卡文（"install <id>\n<summary>\nsecrets: <names>" / "remove <id>"）
// k.approveRuntimeIntent(callCtx, approvalArgs, summary, true)   // 原 Shell approver 发卡
// 批准后 text := linksTool{k}.install(id) / .remove(id)          // takeSecrets 同 id 命中
// err==nil && k.Resources!=nil 时 Reconcile(callCtx)             // 同原 Run 收尾
// 返回原 scrubbed text 或实际 error（linksRuntime fail 路径 scrub+bound）
```

CLI 导入 cliops，`CallLinksInstall`/`CallLinksRemove` → `POST /v1/links/install`、`POST /v1/links/remove`，X-XBot-Call-ID mandatory 同 6a。

**ctx 语义差异写清**：`runtimeSourceCall` 拿原 Shell prepared ctx（冻结 policy 身份 + 同 listener/live/running/未 finished 资格），`runtimeApprovalSource` 在其上 `WithValue(turnKey{}, source.owner)` 重绑 turn——卡在 owner 当前 fork 分支显示，child call 时 owner=nil（无卡可用则 approver 走原语义）。install/remove helper 本身不读 ctx（6a 盘点结论），ctx 只喂 `Reconcile` 与卡路径；HTTP 取消经 `runtimeSourceCall` 的 AfterFunc 链合入。

**secrets-stage 洞及最小解**：`approveRuntimeIntent` 走原 Shell 的 wrapped approver，web 端 `webApprove` 收到 `tool=="sh"` 而非 `"links"`——现 `approvalSecretNames(tool, summary)`（approvals.go:184-189）只认 `tool=="links"`，卡不会带 secrets 字段，`applyDecision`（approvals.go:169-171）不 stage。本刀最小扩展：`approvalSecretNames` 认 links CLI 意图——`tool=="links"` 原分支保，另认 `tool=="sh"` 且 summary 首行 `"install "` 前缀（`linkIDFromSummary`/`secretNamesFromSummary` 原解析不动，卡 summary 与 Confirm 同构是扩展成立的前提）；更稳的判据是 approvalArgs 内 `_xbot_links` intent blob 存在且 action=="install"（deniedBefore/grant 键控同一 blob），按 args 判不以文案判——两者都在 web 侧 webApprove 闭包可见的 `(tool, args, summary)` 内，不扩 `approveRuntimeIntent` 返回签名，不造新 secrets 通道。secrets 值仍只经 `filterSecrets` 白名单→`StageSecrets(id)`→`takeSecrets(e.ID)`/`takeSecrets(c.ID)` 同 id 取出（`link.Lookup`/`storeConnector` 归一后 id 与卡首行一致），模型永不接触值。

## linksTool 整门退：退役面与保留面

**退**（模型侧不再见 links 入口，无双入口）：

- `registry.go:124` group `"links"` 移除 `linksTool{k}`；同组 `kernelManualTool{}` 保（manual 另有消费者，改注册行不删组）。
- review 表 `"links": ExternalSend`（review.go:99）整行删；`NeedsOwner` links install/remove 例外分支（review.go:222-228）与 `linksAction`（review.go:332-338，同时喂 cardtext.go:39-43 的 links 标题分支）退；kernel `reviewCall` 的 permissive links 例外（kernel/review.go:222 `name != "links"`）退——permissive 下不再有保卡工具。links 卡标题「安装 Link/移除 Link」（cardtext.go:39-43）随 linksAction 一起退，CLI 意图卡走 shellCardText 原「运行命令」家族。
- `linksTool` 结构体及 `Confirm`/`Run`（links.go:29-108）删；`Description`/`Parameters` 死代码删；`var _ tools.Tool = linksTool{}`（links.go:776）删。`install`/`installConnector`/`remove`/`removeConnector`/`storeConnector`/`connectorCard` 领域方法**保**——runtime handler 直接调（可顺手改为 Kernel 方法或留 linksTool 匿名字段，实施时取不新造类型的最小形）。
- `notable.go:213` `name != "links"` 豁免与 `todo.go:237` links 豁免随工具退役删（`ClassOf("links")` 落 default 后豁免死代码）。
- `internal/manual/sections/connectors.md:3`「links install/remove 走工具确认卡」改指 `"$XBOT_BIN" links install|remove --self`；cmd/xbot `linksUsage` 末行 "Install and remove stay on the links tool owner card" 改为 CLI 语义。

**保**：

- `StageSecrets`/`takeSecrets`/`secretMu`/`k.secrets`（links.go:643-678、kernel.go:176-177）：生产者仍是 web `applyDecision`，消费者从 linksTool.Run 换成 runtime handler 调的原 helper——同一条 stage 链，不搬不改键。
- `secretValues`/`scrubSecrets`（links.go:680-731）：Shell 输出脱敏（shelljob.go:274-320 `shellForms`）与 reply scrub 仍用。
- `installedSection`/`stableLinkNames`/`markLoaded`/`isLoaded`/`snapshotLinks`/`syncGrantsLocked`/`mountGrantLocked`/`lazyUntilLoad`/`storeSecrets`/`dropSecrets`/`mcpSpecs`/`redactor`/`scrubEntry`：运行态装配与提示块原样。
- `internal/web/links.go` install/remove/dropSecrets web API：独立实现不依赖 linksTool，不动。
- 离线 `cmdLink`（linkcmd.go）/`cmdConnector` 旁路：本就无卡无 Reconcile，不在本刀范围。
- `allowReadOnlyPlatform` links 分支（mcp_readonly_shell.go:53-56）：只放离线 help 与 `search --self`；install/remove --self 是新 subcommand，本 parser 的 Action 判定天然不放行——`parsed.Self && parsed.Action == "search"` 不含 install/remove，research 下拒，与 load 同档（改状态非只读）。无需新分支，但须在验收里钉死。

## 输入、输出与卡路径细节

`ParseLinksRuntime`  noun 层扩为四个 subcommand：search/load/install/remove；裸 `links` 拒、未知 subcommand 拒、三种 help 形式离线退 0 原样。install/remove 的 subcommand 层同 load：恰一个位置参数 id/name、`--self` mandatory、`--json` 可选、无 query 族 flag、拒未知/重复 flag。`linksUsage` 与 main.go 帮助同步。HTTP 侧复用 `serveLinks` 泛型 strict 通道（单 object/unknown 字段/null/尾随/URL query 拒、1 MiB 请求、64 KiB 回包）；`LinksInstall`/`LinksRemove` 同 `{id}` 形状，`boundLinksReply`/`FormatLinks`/双 32KiB 预算不动。

卡路径：handler 先 `holdOperations`，`runtimeApprovalSource` 取 callCtx（XBOT_CALL_ID 缺失/失效报原 `"links: requires XBOT_CALL_ID from an active XBot shell"` 家族——`callLinks` 客户端侧已先拒空 CallID，服务端 `optionalSourceCallID` 为空时 `runtimeSourceCall` 拒 `source approval context unavailable`，两层原样）；从 `agent.ToolArgs(callCtx)` 解原 Shell args map，嵌 canonical blob `args["_xbot_links"] = {"action":"install|remove","id":<归一 id>}`，marshal 得 approvalArgs；summary 用原 Confirm 同构逻辑组（catalog id 走 `"install <e.ID>\n<linkOneLine(summary)>\nsecrets: <names>"`，store-connector 走 `connectorCard`，lookup miss 走 `"install <id>"`/`"remove <id>"`，remove 无 secrets 行）——`link.Lookup`/`storeConnector` 在组卡时已跑一遍，批准后 helper 内重跑同一 lookup，id 归一一致。`approveRuntimeIntent(callCtx, approvalArgs, summary, true)`：`deniedBefore("sh", approvalArgs)`、`parkable`/`grant` 按 `(name="sh", canonArgs(approvalArgs))` 键控——嵌 blob 内 id 不同 canonArgs 不同，A 批准不喂 B，同 6a/0051 先例；approve nil 报 `noApprover`，denied 报 `"denied by user"`，parked 经原 `parkedError` 回包。卡超时 `retireExpired` 自动 allow 落 `ApprovalAutoApproved`，与原工具同语义。

批准后：`install`/`installConnector`/`remove`/`removeConnector` 原 helper 逐字跑（takeSecrets 取卡填值、缺名报 `"links: owner did not fill ..."`、`e.Available && e.Grant` 校验、`storeSecrets`→vault+`SetProductSecrets`、`GrantLink`/`EnableConnector`/`RevokeLink`/`dropSecrets`/`RemoveMCP` 幂等、`removeConnector` 内 `snapshotLinks`），成功后 `Reconcile(callCtx)`。返回 `"installed <id>"`/`"removed <id>"` 经 scrubSecrets+`secretValues()` 脱敏；CLI 先印收到事实再非0，不 retry。

## 接受的代价与验证

选 B，接受：web 侧 `approvalSecretNames` 扩一个 CLI 意图判据（本刀唯一新增协议面）；install/remove 从模型工具改为 Shell CLI（模型多一次 Shell 调用+一次卡，与 persona/bot delete 意图卡先例同形）；`permissive` 下 links 不再例外保卡——卡由 runtime handler 显式 `approveRuntimeIntent` 发出，permissive kernel 下 `checkRuntime*` 先例是 early return（persona_approval.go:17 `if k.permissive() { return nil }`），**install/remove 不照抄这个 early return**：原语义是 permissive 下 install/remove 仍要卡（密钥从卡来，不 stage 就装不成 secrets 条目），故 handler 不走 `k.permissive()` 短路，始终发卡——这是与原工具 NeedsOwner 例外等价的保留，写在合同里防止实施顺手抄错。

最窄验收（盘点 §8 十条）：

1. `links install --self <catalog-id>`：X-XBot-Call-ID → `POST /v1/links/install` → owner 卡（summary `"install <id>\n<summary>\nsecrets: <names>"`、tool=="sh"、secrets 字段名出现在卡 payload）→ 批准+填值 → `StageSecrets`→`takeSecrets`→`storeSecrets`→`GrantLink`→`Reconcile`→`"installed <id>"`。
2. `links install --self <store-connector>`：`connectorCard` summary、`EnableConnector` 写 global mcp.json、`"installed <c.ID>"`。
3. `links remove --self <id>`：`"remove <id>"` 卡 → `RevokeLink`/`dropSecrets`/`"removed <id>"`；store-connector 走 `RemoveMCP` 幂等（ErrMCPMissing 吞）+`dropSecrets`+`snapshotLinks`。
4. 卡填 secrets 经 `filterSecrets` 白名单才 stage；值不出现在 transcript、工具结果、CLI 输出、HTTP 回包（scrubSecrets+secretValues+shellForms 原链）。
5. 拒绝 → `"denied by user"`；超时 → `retireExpired` 自动 allow+`ApprovalAutoApproved`；`deniedBefore`/`parkable`/`grant` 按 `(sh, canonArgs嵌blob)` 键控，A→B 不吃 A。
6. `approvalSecretNames` 扩展后 install 卡带 secrets 字段、`applyDecision` stage 成功；remove/无 secrets 条目卡不带字段。
7. linksTool 退役后 search/load/install/remove 全部走 CLI，无双入口：registry 无 `"links"` 组 tools、`tools` door describe/call 不见 links、review 表无 links 行、NeedsOwner/cardtext/notable/todo 的 links 分支无残留。
8. `connectors.md` 指路保：search/load/install/remove 四个 `--self` 子命令 + `mcp:` 仍指 `mcp tools`。
9. `allowReadOnlyPlatform` links 分支不放 install/remove --self（research 下拒）；离线 help 仍放行。
10. XBOT_CALL_ID 缺失报 `"links: requires XBOT_CALL_ID from an active XBot shell"`；人读/JSONNL ≤32KiB、HTTP ≤64KiB、CLI 先印事实再非0。

完整 caller 合同见 [母路线](../dev/cli-platform-entry.md#批-linksinstallremove-owner卡secrets整门退具体合同pass已实施)。本文保决策边界：不把离线写文件冒「装完即生效」，不把 tool=="sh" 意图卡冒 tool=="links" 原卡链，不把 `_xbot_links` blob 冒新审批协议。两 docs 经 SINGLE ACTUAL CONCRETE SHAPE PASS（reviewer 确认 permissive 不短路写准、验收十点全对，summary secrets 逗号分隔注记不阻塞），本 ADR Accepted，按上述原结构实施；实际入口红与业务证据分别收，不以未知命令冒领域红。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。
