---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0068 restart_xbot 与 http_request 两卡族工具 runtime CLI

## 原能力与调用方

基线 main `b97f93ff`（shell jobs 已合 `41540050`）。本刀把 `restart_xbot` 与 `http_request` 两工具迁 `xbot restart --self` / `xbot http --self` runtime，registry 的 `system` 与 `secrets` 两 kernel 行随退。盘点依据 remaining-inventory §1.1 两行 + §1.3「approval/ExternalSend 卡」速分。

两个原工具核实（与早期背景描述的差异已按源码为准）：

- `restart_xbot`（`internal/kernel/restart_tool.go:12`）：`Restart` class（`review.go:38`）；`Run(_ context.Context, args)` **忽略 ctx**：`{hint?}`，hint trim 后 ≤4000 字节，`k.Restart(hint)`，成功回 `"已保存续跑提示，正在优雅重启 XBot。"`；`k.Restart == nil` 报 `"restart is unavailable in this session"`。**原工具从无 owner 卡**：`reviewCall`（`kernel/review.go:165`）对 `review.Restart` class 直接 `return false`——`review.go:204` `NeedsOwner` 的 `case Restart: return true` 是到死都到不了的死代码；`restart_unattended_test.go` 钉死 timer/webhook 源 + permissive 两态下零卡直跑（unattended 是设计特性，ADR 0019 后 timer/webhook 等 untrusted 事件可无人值守触发重启）。`Confirm`（restart_tool.go:21-29）只产卡摘要 `"restart XBot"`，不决定是否确认。
- `http_request`（`internal/tools/http.go:28`）：`HTTP` class（`review.go:31`）；`Run(ctx, args)` **读 ctx**：`ctx.Err()` 早查 + `http.NewRequestWithContext` 绑取消。`{method?,url,headers?,body?}`，method 空转 GET、url 必需；`h.Client` 兜底 30s timeout client + `h.Store.Transport(nil)`（vault 占位符 egress 替换）；响应 `Status\nbody`：读 `maxHTTPBody+httpReadOverscan+1`→`Store.Redact`→clip 64 KiB→`Store.MaskTruncated`→`[truncated]`；dial/Do 错也经 `Store.Redact`。**条件卡**：`review.NeedsOwner` HTTP 分支 → `httpNeedsOwner(args)`——含 `{{secret:` 占位符或 method 非 GET/HEAD 才发卡；纯 GET/HEAD 无卡直跑。`Confirm`（http.go:73-81）只产 `"<METHOD> <URL>"` 卡摘要。

`k.Restart` 的进程语义（cmd_serve.go:482-495）：`restartFromBot` 经 `requestBotRestart`→`saveRestartContinuation`（枚举 working bots 写 `.restart-continuation.json`）→`restart("Bot requested restart")` 非阻塞塞 `restartCh`（缓冲 1，满则丢，reason 只作日志）→return nil。真正的 `Drain+Relaunch` 在 serve loop `case reason := <-restartCh` 异步发生（cmd_serve.go:860/876→`restartApp`→`ui.Drain(drainCap)`→`appshell.Relaunch`）。所以 runtime handler 的 HTTP response 能在进程退出前正常写完——不存在「发命令后响应中断」；Drain 等 inflight turns 归零，承载本次调用的 turn 完成后 drain 才放行，与原工具语义逐字一致（原 Run 返回后 turn 收尾，drain 放行）。

Agent 沿原 source Shell 使用：

```sh
"$XBOT_BIN" restart --self [--hint TEXT] [--json]
"$XBOT_BIN" http --self <url> [--method M] [--header K=V]… [--body B|--body-file F] [--json]
"$XBOT_BIN" restart --help
"$XBOT_BIN" http --help
```

调用方用原 Shell 的 XBOT_BIN、XBOT_ENDPOINT、规范 XBOT_ROOT 和当次 XBOT_CALL_ID（两动作都 mandatory：restart 无卡也要同 K 资格——`k.Restart` 闭包与 `saveRestartContinuation` 的本实例 root/bot 枚举只在运行实例；http 的 `h.Store`/`k.Vault` 绑本实例 vault 后端）。旧 XBOT_BOT 只诊断，显式 root 必须一致。成功返回原 text（restart 的固定 ACK；http 的 `Status\nbody`）；错误为实际脱敏 `{error:…}`、CLI 非 0。无 artifact/Applied/rollback ACK。

## 两个实际结构

**A：离线进程直调。** restart 落选：`restartFromBot` 闭包、`restartCh`/`restartApp` 的 drain+relaunch 回路是 serve 进程态；离线进程无 serve loop，`saveRestartContinuation` 虽可离线跑（枚举 bot 目录）但重启动作本体不可达——`k.Restart == nil` 时原工具本也报 unavailable，离线进程永远落在 nil 分支，等价性为零。http 落选：占位符替换要 `k.Vault`/`vault.Backend.Transport` 的本实例句柄与域名绑定表（`h.Store` 来自 kernel 装配），离线重造是第二份领域真源；且卡要 owner 重绑走原 owner 动态 fork 分支（`runtimeApprovalSource` 需原 live Shell 的 running call）。两刀均落选。

**B：同 K、原 source 的 runtimeSourceCall/runtimeApprovalSource + 原方法体。** 与 links install/remove 同一 `approveRuntime` 接缝，按工具各自卡语义分两层：

```go
// cliops：同 Jobs/Links 形状，无 action envelope。
type Restart struct{ Hint string `json:"hint,omitempty"` }
type HTTP struct {
    Method  string            `json:"method,omitempty"`
    URL     string            `json:"url"`
    Headers map[string]string `json:"headers,omitempty"`
    Body    string            `json:"body,omitempty"`
}
// Handlers 增 Restart / HTTP 两个函数字段；同 {text?,error?} reply。

// restartRuntime（无卡，runtimeSourceCall prepared ctx 资格）：
// release := k.holdOperations(); defer release()
// prepared, cleanup, err := k.runtimeSourceCall(httpCtx)   // 同 K 资格载体
// defer cleanup()
// // hint trim + 4000 字节原校验逐字
// text, err := restartTool{k}.Run(prepared, approvalArgs)   // 原 Run 忽略 ctx
// 返回原固定 ACK 或实际 error

// httpRuntime（条件卡，runtimeApprovalSource owner 重绑）：
// release := k.holdOperations(); defer release()
// callCtx, cleanup, err := k.runtimeApprovalSource(httpCtx) // owner 重绑
// defer cleanup()
// // canonical 请求体 marshal 成 approvalArgs {method,url,headers,body}
// summary := 原 Confirm 同构 "<METHOD> <URL>"（method 空转 GET）
// need := review.NeedsOwner("http_request", approvalArgs)   // 原条件判定
// k.approveRuntimeIntent(callCtx, approvalArgs, summary, need)
//   // need=false → 函数内 return ctx.Err()（无卡直跑，原 GET/HEAD 分支）
//   // need=true  → 原 Shell wrapped approver 发卡（secret/非读方法分支）
// text, err := (&tools.HTTPRequest{Store: k.Vault}).Run(callCtx, args)
// 返回原 Status\nbody（Store.Redact 全链原样）或实际 error
```

CLI 导入 cliops，`CallRestart`/`CallHTTP` → `POST /v1/restart`、`POST /v1/http`，X-XBot-Call-ID mandatory 同 links。

**ctx 语义差异写清**：restart 用 `runtimeSourceCall`（同 compact/links search-load 判据）——原 Run 忽略 ctx，ctx 在这里是「同 listener/live Local/running/未 finished/原 Shell call」资格载体，无卡无 approver 需求；http 用 `runtimeApprovalSource`——在 prepared ctx 上 `WithValue(turnKey{}, source.owner)` 重绑 turn，需要卡的调用把卡显示在 owner 当前 fork 分支，child call 时 owner=nil（无卡可用则 approver 走原 noApprover 语义）。http 的 `Run` 真读 ctx（`ctx.Err()` + `NewRequestWithContext`）：callCtx 取消链经 `runtimeSourceCall` 的 AfterFunc 合入 HTTP 传输取消——请求中若源 Shell call 或 runtime HTTP 取消，原 client.Do 即时中断，与原工具取消语义一致。

**`deniedBefore`/`parkable`/`grant` 键控**：`approveRuntimeIntent` 按 `(name=<原 Shell 解释器名>, canonArgs(approvalArgs))` 键控。http 的 approvalArgs 用 canonical typed 请求体（`{method,url,headers,body}` marshal 后），不同 url/method/secret 引用 canonArgs 不同，A 批准不喂 B——同 links `_xbot_links` blob 的键控语义，无需额外 intent blob（请求体本身即意图）。restart 无卡不消耗此机制。

**`httpNeedsOwner` 的复现**：不复制判定逻辑——`review.NeedsOwner("http_request", approvalArgs)` 直接调原表原函数，`{{secret:` 子串与非 GET/HEAD 两分支原样；`approveRuntimeIntent` 的 `need=false` 短路（runtime_approval.go:108-110 `return ctx.Err()`）等价原工具纯 GET/HEAD 的无卡路径，一条调用覆盖两分支，不需要「先判定再走不同 handler」。

**permissive 顺序核对（与 links 相反，防照抄）**：原链 `reviewCall` 的顺序是 `kernel/review.go:222` `if k.permissive() { return false, summary, nil }` 在 `:233` `review.NeedsOwner` **之前**——即 permissive kernel 下 http 的 secret/写方法调用**本就不发卡直跑**。故 http handler 的正确等价是：`k.permissive()` 为真时跳过 `approveRuntimeIntent`（need 按 false 处理），与原 reviewCall 顺序逐字对齐。**这与 links install/remove 相反**：links 的保卡依据是 `kernel/review.go` 里已被 0063 退役的 links 专用例外（在 permissive 检查之前发卡），所以 links handler 必须始终发卡；http 没有这种例外，照抄「permissive 不短路」会多一张原链没有的卡。restart 原语义 permissive 下同样无卡（`reviewCall` 的 `review.Restart` class 分支在 permissive 检查之前 return false），CLI 路径同样无卡。

**restart 的 `runtimeSourceCall` 而非 `runtimeApprovalSource`**：restart 原链到不了任何卡（class 分支在 permissive 之前 return false），不需要 owner 重绑；用 `runtimeApprovalSource` 反而引入不需要的 turn 重绑语义。mandatory CallID 的资格由 `runtimeSourceCall` 承载：`k.Restart` 闭包、`saveRestartContinuation` 的 `bot.List(root)`/`TryLockHandle`/`agents/` meta 扫描、`restartCh` 投递全绑本实例运行态，离线无等价路径——与 compact/links search-load 的「同 K 资格」判据一致（不是 transcript/jobs 的 IGNORE-ctx 判据：那些原方法与状态都不在本实例才有约束，restart 是动作本体只在本实例）。

## 退役面与保留面

**退**（模型侧不再见两工具，无双入口）：

- `registry.go:124-126` `{"system", …, restartTool{k}}` 整行删（组空随退）；`registry.go:128-130` `{"secrets", …}` 行删——`http_request` 是该组唯一工具，组随退（`list_secrets` 已在 0064 退）。
- `review.go` 表两行删：`"http_request": HTTP`（:57）、`"restart_xbot": Restart`（:62）。`review.Restart` class 常量随唯一消费（`kernel/review.go:165` 分支 + `review.go:204` NeedsOwner 分支）退——`NeedsOwner` 的 `case Delete, Restart` 收窄为 `case Delete`；`kernel/review.go:165-167` Restart class 分支删。`review.HTTP` class 常量随消费退——**注意退役陷阱**：`NeedsOwner` 内部 `switch ClassOf(name)`，删 `"http_request"` 表行后它落 `case Normal: return false`，handler 若调 `NeedsOwner("http_request", args)` 则 need 恒 false、卡静默消失。最小解：`review` 包导出 `HTTPNeedsOwner(args)` 薄包装（`httpNeedsOwner` 改导出或加导出），handler 调它而非 `NeedsOwner`；`NeedsOwner` 的 `case HTTP` 分支随表行删（死代码）。写在这里防止实施只删表行导致卡静默消失。
- `restartTool` 结构体及 Confirm/Run/Description/Parameters/`var _ tools.Tool`（restart_tool.go 全文件）删；`tools.HTTPRequest` 结构体及方法（http.go 全文件）**保**——runtime handler 直调 `Run`（`Store`/`Client` 字段、parse/Confirm 方法由 handler 复用组 summary，`var _ tools.Tool` 断言随退役删）。
- `cardtext.go:21` `name == "http_request"` 分支随工具退役删——runtime 卡走 `shellCardText`「运行命令」家族（tool=sh 语义，与 links CLI 意图卡同形）。
- `restart_unattended_test.go` 整文件随退（钉的是工具注册语义，退役后无消费点）；`registry_test.go:85` `k.Restart` 断言迁 CLI 路径。

**保**：

- `k.Restart` 字段与 `restartFromBot`/`requestBotRestart`/`saveRestartContinuation`/`deliverRestartContinuation`/`.restart-continuation.json` 全链（cmd_serve.go/restart_continuation.go）——runtime handler 调 `k.Restart` 同原 Run。
- `tools.HTTPRequest` 的领域实现：parse/method GET 默认/url 必需、`Store.Transport`/`Store.Redact`/`Store.MaskTruncated`、30s timeout client 兜底、`maxHTTPBody`/`httpReadOverscan`/`[truncated]`——handler 组 args 后原 Run 逐字跑。
- `review.httpNeedsOwner`（改导出）、`secretValues`/`scrubSecrets`/`redactor`、vault 全链。
- `web.Approve`/`webApprove`/`applyDecision`/`StageSecrets` 卡面：http 无 secrets stage 需求（占位符在 egress 替换，卡不收值），无 `approvalSecretNames` 扩展（links 特有）。卡 title 由 shellCardText 产（原链走 `httpCardText`「发起 HTTP 请求/带密钥的 HTTP 请求」——**接受的差异**：CLI 意图卡 tool=sh，标题归「运行命令 在本机上执行」家族，summary chip 仍 `"<METHOD> <URL>"`，与原卡的信息量等价、标题文案略变；不为此保留 httpCardText 消费点——links CLI 卡同样不再走原 links 标题，先例一致）。

## 输入、输出与卡路径细节

`ParseCardsRuntime` 或按 nouns 各自 parser（`ParseRestartRuntime`/`ParseHTTPRuntime`——实施取最小形，links 先例是 noun+subcommand 双层，本刀两个 noun 各自单层）：`restart --self [--hint TEXT]`、`http --self <url> [--method M] [--header K=V]… [--body B|--body-file F]`；`--self` mandatory、`--json` 可选、`--help` 离线退 0、`--hint`/`--method`/`--body`/`--body-file` 值 flag、`--header` 可重复（`K=V` 切分，K 非空，重复 K 拒）、`--body`/`--body-file` 互斥（`-` stdin）；裸 noun 拒、未知 flag/重复 flag 拒、恰一位置 url（http）/零位置参数（restart）。`--hint` 长度校验交原 Run（≤4000 字节）。HTTP 侧 strict 单 object/unknown/null/尾随/URL query 拒、1 MiB 请求/64 KiB 回包（`--body` 上限在 typed 请求预算内，body 本身原工具无独立 cap，64 KiB 回包 clip 原样）。`{text?,error?}` reply，人读/JSONNL 各 ≤32 KiB，CLI 先印事实再非 0、不 retry。

卡路径（http，need=true 分支）：handler `holdOperations`→`runtimeApprovalSource` 取 callCtx（XBOT_CALL_ID 缺失/失效报 `"http: requires XBOT_CALL_ID from an active XBot shell"` 家族——client 侧先拒空 CallID，服务端 `optionalSourceCallID` 为空时 `runtimeSourceCall` 拒 `"source approval context unavailable"`，两层原样）；marshal canonical `{method,url,headers,body}` 为 approvalArgs；`approveRuntimeIntent(callCtx, approvalArgs, "<METHOD> <URL>", true)`：`deniedBefore`/`parkable`/`grant` 按 `(sh, canonArgs(approvalArgs))` 键控——url/method/body/headers 不同 canonArgs 不同，A→B 不吃 A；approve nil 报 `noApprover`，denied 报 `"denied by user"`，parked 经原 `parkedError` 回包；卡超时 `retireExpired` 自动 allow 落 `ApprovalAutoApproved`，与原工具同语义。secret 引用只在 args 里，卡 summary 原样含 `{{secret:NAME}}` 占位符文本（原链同），值永不进卡/结果。

批准后（或 need=false 直通）：`(&tools.HTTPRequest{Store: k.Vault, Client: <原兜底一致>}).Run(callCtx, args)`——`h.Client` 的兜底链（nil → 30s client + `Store.Transport(nil)`）在 Run 内原样，handler 只需传 `Store`；`ctx` 取消链经 runtimeSourceCall AfterFunc 合入。返回 `Status\nbody` 经 `config.RedactHeld`+`Resources.Redact` 边界补 + error 沿动态 redactor+secretValues 可缩。

restart 路径：`holdOperations`→`runtimeSourceCall`→`k.Restart` nil 查 →`restartTool{k}.Run(prepared, args)`（hint trim/4000 校验/`k.Restart(hint)`）→固定 ACK text。`restartCh` 非阻塞塞入后 return，response 写完；serve loop 异步 `Drain`+`Relaunch`——**response 先行、进程后置**的顺序与原工具「Run 返回→turn 收尾→drain 放行」逐字等价；Drain 超时（drainCap）走原 `in-flight turns did not finish in time` 分支不阻塞 response。

`allowReadOnlyPlatform`（mcp_readonly_shell.go）分支：restart/http 都是写/卡类动作，research 下 `--self` 拒（同 jobs cancel/links install-remove 档）。无 restart/http 分支时 `--help` 一并拒——比「放行」更严但与白名单逐 noun 显式列分支的形状一致；若要对齐 `--help` 放行，需为两 noun 各加 `parsed.Help` 分支，本刀不加。在验收钉死。

## 接受的代价与验证

选 B，接受：http 卡标题从 `httpCardText` 家族落到 `shellCardText`「运行命令」家族（信息量等价，summary chip 保 `"<METHOD> <URL>"`）；http 从模型工具改为 Shell CLI（多一次 Shell 调用 + 条件卡，与 links/persona 先例同形）；`review.httpNeedsOwner` 需导出（本刀唯一新增协议面）——**退役顺序约束**：删 `review.Table` 的 `"http_request"` 行时 `NeedsOwner` 对它落 `Normal` default 恒 false，handler 必须已改调导出的 `HTTPNeedsOwner`（或直接调 `httpNeedsOwner` 若同包），两处在同一刀内原子切换，先验证卡仍按原条件触发再删表行。

最窄验收（十点）：

1. `restart --self`（无 hint）：X-XBot-Call-ID → `POST /v1/restart` → `runtimeSourceCall` prepared → `saveRestartContinuation` 写 `.restart-continuation.json` → `restartCh` 投递 → response 先回固定 ACK `"已保存续跑提示，正在优雅重启 XBot。"` → serve loop Drain+Relaunch（测试环境用注入 `k.Restart` 桩断 hint/写入，不真重启）。
2. `restart --self --hint "…"`：hint 传 `k.Restart` 实参；超 4000 字节报原错；`k.Restart == nil`（无 serve）报原 `"restart is unavailable in this session"`；二次调用报原 `"restart already requested"`（restartRequestMu 原语义）。
3. `http --self <url>`（GET，无 secret）：`HTTPNeedsOwner`=false → `approveRuntimeIntent` need=false 直通无卡 → `Run` 原链 → `Status\nbody`。
4. `http --self <url> --method POST [--body]`：`HTTPNeedsOwner`=true → owner 卡（tool=sh、summary `"POST <url>"`）→ 批准 → 请求发出；`--header`/`--body-file -` stdin 同链。
5. `http --self <url-with-{{secret:NAME}}>`：占位符触发卡（`{{secret:` 子串分支）→ 批准后 `Store.Transport` egress 替换 → 值不出现在结果/卡/回包（`Store.Redact` 全链）。
6. permissive kernel：http secret/写方法**无卡直跑**（NeedsOwner 分支在 permissive 检查之后，原链本就不发卡——与 links 相反，验收钉死防照抄）；restart 无卡同原。
7. 拒绝 → `"denied by user"`；卡超时 → `retireExpired` 自动 allow+`ApprovalAutoApproved`；`deniedBefore`/`parkable`/`grant` 按 `(sh, canonArgs)` 键控，A→B 不吃 A；`noApprover`（无 owner turn，如 child call）报原错。
8. 退役面：registry `system`/`secrets` 行删（secrets 组空随退）、`review.Table` 两行删、`review.Restart`/`review.HTTP` class 常量与 `NeedsOwner` 两分支死代码删（`httpNeedsOwner` 导出保）、`kernel/review.go:165` Restart 分支删、cardtext http 分支删、restartTool/http.go 按消费定去留、`restart_unattended_test` 退；`tools.HTTPRequest` 类型保由 handler 直调；**`NeedsOwner("http_request")` 不再被任何路径调用**（handler 走 `HTTPNeedsOwner`），守门断言迁移。
9. CallID 缺失报 `requires XBOT_CALL_ID from an active XBot shell`；人读/JSONNL ≤32 KiB、HTTP ≤64 KiB、CLI 先印事实再非 0；`allowReadOnlyPlatform` 不放 restart/http `--self`（research 拒）；无 restart/http 分支时 `--help` 一并拒（更严一致，若放行需加分支）。
10. Drain 语义：restart 批准/直通后 response 在 `restartCh` 投递后正常写完（桩测试断言 response body 完整 + restartCh 收到 reason）；进程重启后 `deliverRestartContinuation` 投 restart 事件原样（cmd_serve/restart_continuation 不动）。

完整 caller 合同见 [母路线](../dev/cli-platform-entry.md#批-cardsfamily-restarthttp-两工具-runtime-cli-具体合同)。本文保决策边界：不把「restart 需 owner 卡」的早期背景误写进合同（原工具 unattended 无卡是设计事实）；不把 links 的「permissive 不短路」照抄给 http（links 例外在表外，http 的 NeedsOwner 分支在 permissive 检查之后，原链 permissive 下本就无卡）；不把 `NeedsOwner("http_request")` 当 need 判定源（退役表行后它恒 false，必须调导出的 `HTTPNeedsOwner`）；不把 restart 的 response 想成「发命令后断开」（`restartCh` 非阻塞投递+异步 Drain，response 先行是原语义）。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。

SINGLE PASS：复审 10/10 通过，唯一修订为验收⑨ `--help` 措辞——实无 restart/http 分支时 research 下 `--help` 亦拒（更严一致），本文按实现对齐；若日后要 `--help` 放行需为两 noun 各加分支。
