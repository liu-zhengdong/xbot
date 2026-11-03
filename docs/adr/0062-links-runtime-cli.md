---
kind: adr
guard: owner
status: Proposed
date: 2026-11-02
---
# 0062 links search/load 沿原 source CLI

## 原能力与调用方

基线 TRUE compact worktree `828d9340`。只迁 linksTool 的 `search` 与 `load` 两个无审批子集（connectors-inventory-20261102 候选 A）；`install`/`remove` 继续走 owner 卡与 secrets-stage 链路，本刀不动。原 `linksTool.search(query)`（links.go:110）遍历 `link.Catalog()`＋`link.Connectors()`＋`mcpSpecs()` 拼 TSV `id\tstate\tsummary\tsecrets:…`，state 三态 listed/available/installed，空集兜底 "no links"，收尾 `scrubSecrets(text, k.secretValues())`。原 `loadContext(ctx,id)`（links.go:312）：`link.ResolveApp`→storeConnector 归一成 `mcp:`→`mcp:` 前缀一律指路错误 `"$XBOT_BIN" mcp tools <server> --self --json`→organ 走 `loadedNames(o.Grant)`→catalog `e.Grant==""||!Bot.Grants` 报 "not installed"→`markLoaded(e.ID)`→`loadedNames` 返回 "loaded id: tool1, …"。

Agent 沿原 source Shell 使用：

```sh
"$XBOT_BIN" links search --self [--query TEXT|--query-file FILE|-] [--json]
"$XBOT_BIN" links load --self <id|name> [--json]
"$XBOT_BIN" links --help
"$XBOT_BIN" links search --help
"$XBOT_BIN" links load --help
```

调用方用原 Shell 的 XBOT_BIN、XBOT_ENDPOINT、规范 XBOT_ROOT 和当次 XBOT_CALL_ID；旧 XBOT_BOT 只诊断，显式 root 必须一致。search 返回原 TSV 文本（scrubbed）或兜底 "no links"；JSON 为 `{text:…}`。load 成功返回原 "loaded id: tools…"，错误为实际脱敏 `{error:…}`、CLI 非0；`mcp:` 与 store-connector 名仍返回指向 `mcp tools` 的指路错误。原 API 不返回行集结构化字段，故 JSON 不新拆 lines/rows；无 artifact/Applied/rollback ACK。

## 两个实际结构

**A：离线 Load 与新 Kernel。** CLI 按诊断 source 名装载 Bot，重建目录遍历与 load 语义：

```go
type OfflineLinksOps struct{ Root, SourceName string }
func (r OfflineLinksOps) Search(query string) (string, error) {
    // Load(source); 重读Bot grants、connectors.json、global+bot mcp.json、vault 名；再拼TSV
    panic("not implemented")
}
func (r OfflineLinksOps) Load(id string) (string, error) {
    // 新K后 markLoaded 只写进这个一次性进程的 k.loaded
    panic("not implemented")
}
```

search 必须重造 `EffectiveLinks`/`mcpSpecs`（Manager 保留的有效 spec，不是文件重读）/`GlobalSecretLookup`/vault 占位这套运行态输入，离线文件与运行中 Manager 快照不等价；load 更根本——`markLoaded` 写的是 kernel 进程内 `k.loaded`（loadMu 下），运行时可见效果是 `toolRegistry`（registry.go:162 对未 loaded 的 lazy link 标 deferred）与 `stableLinkNames`（排除 lazy），离线新 K 的标记随进程消失，`tools` door describe/call 在运行中 kernel 里永远看不到。`toolsForLoaded` 还需要 `snapshotLinks` 的运行中 `k.Links` 装配。落选。

**B：同 K、原 prepared source context、原 search/load helper。** 既有 listener 只增一个 typed handler 组，不新 LinksService/dispatcher/context 表或领域抽取：

```go
// cliops: 两个请求没有 action envelope。
type LinksSearch struct{ Query string `json:"query,omitempty"` }
type LinksLoad struct{ ID string `json:"id"` }
type LinksReply struct {
    Text  string `json:"text,omitempty"`
    Error string `json:"error,omitempty"`
}
type LinksHandlers struct {
    Search func(context.Context, LinksSearch) (LinksReply, error)
    Load   func(context.Context, LinksLoad) (LinksReply, error)
}
// 既有 cliops.Handlers 增一个 Links *LinksHandlers 字段。
// Kernel callback伪码（两个同形）：
// release := k.holdOperations(); defer release()
// prepared, cleanup, err := k.runtimeSourceCall(httpCtx); if err != nil { return actualError }
// defer cleanup()
// search: text := linksTool{k}.search(in.Query)（内部已scrubSecrets+兜底"no links"）
// load:   text, err := linksTool{k}.loadContext(prepared, in.ID)
// 返回原text或实际scrubbed error；不外套currentIO/envMu。
```

CLI 导入 cliops，`CallLinksSearch(ctx,endpoint,root,originalCallID,input)`、`CallLinksLoad(...)` 分别到 `POST /v1/links/search`、`POST /v1/links/load`；call ID 来自原 Native Shell 环境，不提供 caller 自报身份 flag。

**为什么 search/load 也要 mandatory XBOT_CALL_ID**：与 transcript 三读不同（原方法不读 ctx，ADR0060 选择不要 CallID），也与 compact 相同（ADR0061：prepared ctx 是资格的载体）。links 的两个动作都要定位**同一个运行中 kernel**：search 的 `mcpSpecs`/`EffectiveLinks`/`secretValues` 与 load 的 `snapshotLinks`/`markLoaded` 都依赖 live source 状态；走 `runtimeSourceCall` 的 prepared 路径同时免费得到"同一 listener、live local、running、未 finished、原 Shell call"这套资格检查，与 compact 同一判据。缺失/完成/foreign/nonShell/cancel 沿原 helper 拒绝，不假造 ctx；它不是 primary/person 资格或审批卡，不调用 `runtimeApprovalSource`/`approveRuntimeIntent`/approver。HTTP 取消合入 prepared ctx（`runtimeSourceCall` 的 AfterFunc 链），loadContext 将其透传给 `toolsForLoaded`（签名留参、实现不读 ctx），不宣称新增即时取消。

## 原工具的去留与本刀语义

- **linksTool 保留**，继续注册在 registry `{"links", always, …linksTool{k}, kernelManualTool{}}`（registry.go:124），review 表 `"links": ExternalSend`（review.go:99）与 `NeedsOwner` 的 install/remove 例外（review.go:222-228）不动；permissive 下唯一保留卡片的语义（review.go:222-224 注释 "Keep links for connector key input"）原样。`approvalSecretNames` 只认 tool=="links"（approvals.go:184-233），install/remove 的 StageSecrets→takeSecrets→storeSecrets/dropSecrets→GrantLink/RevokeLink→EnableConnector/RemoveMCP→`Reconcile(ctx)` 整链不变。
- **linksTool.Run 的 search/load 分支改成指路错误**，文本指向本 CLI：`links: search/load use the CLI: "$XBOT_BIN" links search|load --self …`。选择"分支指路"而非"保留原行为"或"整个退门"：install/remove 仍需工具存在（Confirm 卡片、review 归类、secrets stage 都以它为锚），不能整门退；若 search/load 在工具里继续返回原结果，模型侧会出现 CLI 与工具两套等价入口，与母路线"模型侧 links 管理入口退役"方向相悖。指路错误保留工具名与 install/remove 卡链路，把本刀范围锁在最小。Confirm 对 search/load 本就返回 ""，无变化。
- search/load 走 CLI 后，模型经 `"$XBOT_BIN" links search/load --self` 获得与原工具逐字节同语的输出——同一 helper 产出，不是重写。

## 输入、输出与原消费者

纯 `ParseLinksRuntime` 与早入口在 root/default/legacy 初始化前分流。help 离线；其余必须 self；`links` 裸命令、无位置参数子命令/别名拒，拒未知/重复 flag，按 arity 消费，query 中的字面 --help/-- 不冒 help。search 的 query 可省略/空（空串列全目录，同原工具），query 与 query-file 至多一、query-file 路径不可空、`-` 读 stdin；load 的位置参数恰好一个 id/name，原 TrimSpace+`id is required` 语义交领域。raw 文本/文件/stdin max+1 与最终 escaped typed 请求各 1MiB，无截断后 trim。HTTP 严格单 object/unknown 字段/非 object/null/尾随内容/URL query 拒绝，整数无；复用 loopback/Host/browser/canonical-root guard，HTTP 回包 64KiB，请求/回包 DTO 仅 `{query?}`、`{id}` 与 `{text?,error?}`。

成功人读原 text+newline，JSON `{text:…}`；错误沿原动态 redactor＋`secretValues()` scrub，仅显示 error 可缩；共享准确 `FormatLinks` 与整 JSONNL 各 ≤32KiB，CLI 先输出收到事实再非0，不自动 retry/proxy/redirect。search 的完整目录 TSV 与 load 的短 ACK 都不挂 20MiB 结果 artifact 框架——search 原文已由 1024B prompt cap 之外的全目录构成但仍是单一 text 语义，无 File/bytes 字段。

实际 consumer 须同 cut 迁：`internal/manual/sections/connectors.md` 的 "links search 查目录 / 非 MCP links load 列出工具 id" 改指 `"$XBOT_BIN" links search|load --self`；`link.InstalledRef` 注释与 `installedSection` 产出文本保持原样（它只列 id+summary，不含入口指令）；tools door 的 describe/call 消费 loaded 工具不变；`stableLinkNames`/deferred 逻辑原样。readonly 分类（`allowReadOnlyPlatform` mcp_readonly_shell.go）新增 `links` 分支：`search --self` 只读可放行，`load --self` 改变 kernel 进程内 loaded 状态（deferred 解除=运行时效果）**不属于只读**，只有 `links --help`/`search --help`/`load --help` 离线 help 放行，与 compact 同档。research 只读包装沿原 DirectXBotArgs 分类器，只认当前 canonicalBIN/受控 XBOT_BIN 单 POSIX 直调＋本 parser 的 help/合法 self search，不 Contains 放宽。notable.Tool 的 links 豁免（notable.go:213）与旧 native literal/工具配对历史仍读，无可执行暗 shim/伪 native 配对/新 ENV/Inbox/waker。

## 接受的代价与验证

选 B，接受 search/load 从模型工具改为 Shell CLI 的入口迁移（模型多一次 Shell 调用，与 transcript/compact 同形），换取领域单一真源与"运行中 kernel 状态"不复制。`mcp:` load 的指路错误文本与 `playwright_load_test.go`/`connectors_prompt_test.go:58` 断言保持同语（文本只换指路目标的家族）；`isLoaded("mcp:"+id)` 永不置位语义不变。

最窄验收（同 inventory §9 前两条＋本刀新增）：

1. `links search --self --json` 输出与 `linksTool.search` 相同的 TSV 行集：catalog 4 条＋connectors.json 行＋mcp 已装行，state 三态一致；空目录 "no links"；`secrets:` 名出现而值不出现（scrubSecrets 经 Vault.Names 占位与已 stage 值）。
2. `links load --self <native>`：运行中 kernel 内 `isLoaded(id)` 置位（可用 tools door describe/call 该 link 工具或 registry deferred 解除验证）；organ id 走 `loadedNames(o.Grant)` 原路径；`load mcp:x`/store-connector 名返回指 `mcp tools` 的指路错误、不置 loaded。
3. `links` 工具的 `search`/`load` action 返回指 CLI 的指路错误；`install`/`remove` 卡片链路、secrets stage、permissive 例外照旧。
4. XBOT_CALL_ID 缺失/失效报 "requires XBOT_CALL_ID from an active XBot shell"（compact_runtime.go:73 同款）；HTTP 端 strict object/unknown/null/query 拒绝。
5. 人读/JSONNL 各 ≤32KiB、HTTP ≤64KiB，CLI 先打印收到事实再非0。

复用未改 catalog/state/secrets-hiding 原有效证据与源 resolver/前序已绿族，只 new＋必要 actual affected 最小 once，不整 longrun/真模型/性能矩阵。完整 caller 合同见 [母路线](../dev/cli-platform-entry.md#批-linkssearchload-两无审批子集具体合同)。本文保决策边界，不把离线 TSV 冒运行态等价、把指路错误冒领域失败或把 loaded 进程态冒持久状态。
