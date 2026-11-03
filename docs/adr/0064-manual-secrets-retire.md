---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0064 kernel_manual 与 list_secrets 退注册：离线等价 CLI 已在，只退 model 工具

## 原能力与调用方

基线 main `1cede475`（links/connectors 家族已合 `9277f5c2`）。本刀是批 5 收口前最窄的一刀：退 `kernel_manual` 与 `list_secrets` 两个 model 工具注册。两者都是 **Readonly、零卡、零 ctx 依赖**，且离线等价 CLI 已在。

**kernel_manual**（`internal/kernel/manual.go:10` `kernelManualTool{}`）：`Name()="kernel_manual"`，`Description` 指引「读版本化机制手册段」，`Parameters` 枚举 `index` + `manual.Sections()` 各 ID，`Confirm` 恒 false，`Run` 直接 `manual.Read(in.Section)` 返回 embed 正文——**不读 ctx、不读 Kernel、不读运行态**。挂在 `registry.go:124` `{"links", always, …kernelManualTool{}}` 组（linksTool 退后该组只剩它）。`manual.Prompt()`（`manual.go:42`）注入 `KernelRules`（`assemble.go:109`），告诉模型「先读 kernel_manual 里相关的章节」。

**list_secrets**（`internal/tools/secrets.go:13` `SecretList{}`）：`Name()="list_secrets"`，`Description`「列 vault 名+域，不出值」，`Parameters` 空 object，`Confirm` 恒 false，`Run` 遍历 `Store.List()` 拼 `name\tdomains` TSV——只读 `k.Vault`（`vault.Backend`），不读 ctx。挂在 `registry.go:129-131` `{"secrets", …on: k.Vault != nil && namedLink(links, link.NameTools)}` 组，与 `http_request` 同组。

## 等价 CLI 已在

```sh
xbot manual [section]          # cmd_manual.go:8-22，离线，main.go:274 在 resolveRoot 前分流
xbot secret list <bot>         # secret.go:90-106，离线读 global vault，需 --root/默认 root
```

`xbot manual` 与 `kernel_manual` 共用同一 `internal/manual` embed 正文：`manual.Read(section)` 是同一份 `manual.go:46-57`，`Sections()` 是同一份目录。`xbot secret list` 与 `SecretList.Run` 都读 `vault.OpenGlobal(root).List()` 拼同名+域 TSV——语义逐字等价，只是 CLI 需要 `<bot>` 参数定位 root（`bot.Load` → `openSecretBackend` → `vault.OpenGlobal`），而 `list_secrets` 直接用注入的 `k.Vault`。

## 与前面各刀的关键区别

本刀**不需要** `runtimeSourceCall`/`prepared ctx`/`runtimeApprovalSource`/`/v1/*` endpoint/`--self`/XBOT_CALL_ID——这是与 6a/6b（compact/links search/load/install/remove）的本质不同：

- compact/links 需要**同一运行中 kernel**的 `prepared ctx`（turnSeq、frozen turn、owner 重绑、secrets stage、Reconcile），所以走 `--self` + `/v1/*` + XBOT_CALL_ID。
- `kernel_manual`/`list_secrets` 是**纯本地读**：`manual.Read` 读 embed FS，`vault.OpenGlobal(root).List()` 读磁盘 vault 文件——任何 `xbot` 进程都能独立做，不需要运行中 kernel 的状态。离线 CLI 就是等价，不需要打回当前实例。
- 因此本刀**不新增** cliops handler、`/v1/*` 端点、parser 分支、`--self` 语义——模型经 `sh` 调 `xbot manual`/`xbot secret list` 即可，与「用 Shell 调任意 CLI」同形。

## 退役面

**退**（model 侧不再见 kernel_manual / list_secrets）：

- `registry.go:124` `{"links", always, func…kernelManualTool{}}` 整行删——`links` 组空，组随工具一起退（不再有空组）。`registry.go:129-131` secrets 组的 `&tools.SecretList{Store: k.Vault}` 从 tools slice 删，`http_request` 保。
- `manual.go:10-32` `kernelManualTool` 整型删（`Name`/`Description`/`Parameters`/`Confirm`/`Run`）；`manual.go` 文件可删或只留 package 头——`internal/manual` 包本体**保**（`xbot manual` CLI、`KernelRules` 注入、`manual_test` 都在用）。
- `secrets.go:13-49` `SecretList` 整型删（`Name`/`Description`/`Parameters`/`Confirm`/`Run`/`Store` 字段）；`secrets.go` 文件可删或只留 package 头。`http_request` 用的 `tools.HTTPRequest` 不动。
- `review.go` 表两行删：`"kernel_manual": Readonly`（review.go:76）、`"list_secrets": Readonly`（review.go:61）。`ClassOf`/`Known`/`NeedsOwner` 无需改——退后 `ClassOf("kernel_manual")`/`ClassOf("list_secrets")` 落 `Normal` default，但两者不再注册，守门测试的 `Known` 断言要迁。
- `manual.Prompt()`（`manual.go:42`）与 `manual.Index()`（`manual.go:36`）的读取指引改词：不再说「先读 kernel_manual 里相关的章节」/「读取：kernel_manual(section)」，改为指 `xbot manual <section>`——`KernelRules` 仍注入手册目录与「先读手册再答机制问题」的要求，只是入口从 model 工具改为 Shell CLI。`manual.Read` 的错误文本 `"use xbot manual to list sections"`（manual.go:56）已指 CLI，不动。
- `breaker.go:249` `looseCall` 的 `case "read_file", "list_secrets"` 中 `"list_secrets"` 删——该工具不再注册，`looseCall` 的豁免死代码删（`read_file` 保）。`recall`/`recall_history` 等已退工具的豁免本就在同一 case 家族内，不动。
- `docs/atlas/security.md:185` readonly 表行的 `kernel_manual`、`list_secrets` 删；`:178`/`:196` 的 `list_secrets` 提及删。`docs/atlas/kernel.md:212` 「`kernel_manual(section)` 始终挂载且属于 readonly」改为「模型经 `xbot manual` 读同一正文」。`docs/atlas/memory.md:11` 「模型的 `kernel_manual(memory)`」改为「模型经 `xbot manual memory`」。`docs/API.md:363` 的 links 退役段不动（它已记 `kernelManualTool{}` 保——本刀把它也退了，API.md 补一句）。
- `context_batch_c_test.go:33-69` `TestBatchCManualReadBeforeAnswer`：该测试驱动 scripted model 调 `kernel_manual` 读 section——退役后模型不再有此工具。改写为「模型经 `sh` 调 `xbot manual <section>`」：scripted 改发 `batchCall("sh", "sh", {"command":"$XBOT_BIN manual <section>"})`，断言手册正文出现在 tool result 与 prefix 只含 `manual.Prompt()` 不含正文。`review.ClassOf("kernel_manual")` 断言删。`kernelManualTool{}.Run` 的拒绝参数断言（`:64-68`）删——工具本体已删。
- `vault_test.go:28-51` `TestModelToolsIncludeVaultToolsOnlyWithVault`：`hasToolName(list, "list_secrets")` 断言改为「无 vault 时无 `http_request`/`list_secrets`，有 vault 时**只**有 `http_request`」——`list_secrets` 不再注册，改为断言 `!hasToolName(list, "list_secrets")`。
- `internal/kernel/review_test.go:358` `want` 列表的 `"list_secrets"` 删；`:370` retired 列表加 `"kernel_manual"`, `"list_secrets"`（`seen[name] || review.Known(name)` 拒）。
- `internal/review/review_test.go:22` `want` map 的 `"list_secrets": Readonly` 删；`:219` readonly 循环列表的 `"list_secrets"` 删。
- `registry_test.go:130` 注释「kernel_manual still lives in it」改为「links 组已随 kernel_manual 一起退」；如该测试断言 links 组存在或 kernel_manual 注册，改为断言**不**注册。
- `internal/tools/http_test.go:121-162` `TestSecretListShowsNamesAndDomainsOnly`/`TestSecretListDoesNotAsk`：`SecretList` 本体已删，这两个测试随删（`http_request` 的测试不动）。
- `allowReadOnlyPlatform`（`mcp_readonly_shell.go`）**不新增分支**：`xbot manual`/`xbot secret list` 是离线命令不经 `allowReadOnlyPlatform` 判定（该函数只判 `--self` runtime CLI 与 help）。模型在 research（readonly platform）子代理下经 `sh` 调 `xbot manual` 时，shell 工具被 `readOnlyShell` 包装（dispatch.go:160-161,169-183）：`Run` 在解释器前先查 `ShellToolIsReadOnly` 与 `allowReadOnlyPlatform`，`xbot` 不在 `ShellIsReadOnly` 的 POSIX 白名单（review.go:573-576），`allowReadOnlyPlatform`（mcp_readonly_shell.go）也无 manual/secret 分支——所以 `Run` 直接 error「只能运行单纯的读取命令」，**不弹卡、不执行**。research 子代理本无给任意 shell 弹主人卡的路径。这是与原 `kernel_manual` Readonly 豁免不同、且比「弹卡」更强的收紧：**模型在 research 平台下完全无手册读取路径**。接受这个变化，理由：(a) research 定位是「只读不动作」，`xbot manual` 虽是读但仍是新进程 spawn；(b) 与 transcript/memory-recall 等 `--self` CLI 的「research 放行」不同——那些是 runtime 调用打回同一实例，有 CallID 资格链；离线命令无资格链，不享受同等豁免。验收时钉死此边界。

## 保留面

- `internal/manual` 包本体：`Sections()`/`Read()`/`Prompt()`/`Index()`/`contents` embed FS 全保——`xbot manual` CLI（`cmd_manual.go:8-22`）与 `KernelRules` 注入（`assemble.go:109`）是同一份消费者。只改 `Prompt()`/`Index()` 的指引词（指 `xbot manual` 而非 `kernel_manual`），不改正文产出。
- `internal/vault` 全链：`vault.OpenGlobal`/`Backend.List()`/`OpenLocalProduct` 不动——`xbot secret list` 用的同一份。
- `http_request`（`internal/tools/http.go:28` `HTTPRequest{}`）：同组保，`{{secret:}}` 占位注入链不动。
- `xbot manual`/`xbot secret` CLI 本体不动——已是等价。
- `KernelRules` 的手册指引要求保：模型被问机制仍须先读手册，只是入口从 `kernel_manual(section)` 改为 `xbot manual <section>`。
- `secretValues`/`scrubSecrets`/`redactor`/`scrubEntry` 不动——secrets 值脱敏链与 `list_secrets` 无关（`list_secrets` 本就不出值）。

## 模型侧行为变化

- `kernel_manual`/`list_secrets` 从 tool_index 消失，`tools` door search/describe/call 报 `no tool with id "kernel_manual"`/`"list_secrets"`。模型被问机制时 `KernelRules` 指引它跑 `xbot manual <section>`（Shell）；要列 secrets 名时跑 `xbot secret list <bot>`。
- 模型不再能**免卡**读手册/列 secrets——`kernel_manual`/`list_secrets` 的 Readonly 豁免没了，`xbot manual`/`xbot secret list` 经 `sh` 执行（trusted turn 下 `sh` 无写/发送词不弹卡，与原 Readonly 等效；research 平台下被 `readOnlyShell` 解释器前拒绝、不弹卡不执行，模型在 research 下无手册读取路径）。这是有意的边界收紧：离线命令不该享受与运行中实例 runtime 调用同等的只读豁免。
- `breaker.go` 的 `looseCall` 对 `xbot manual`/`xbot secret list` 无豁免——连续重读同一 section/列表达到 `loopThreshold` 会熔断（原 `list_secrets` 的 loose 豁免随退）。`xbot manual` 的 `DirectXBotArgs` 不在 `looseCall` 白名单内（只有 `memory-recall` 在 `looseShell` 内），与原 `kernel_manual` 的 loop 阈值同档。

## 最窄验收

1. `registry.go` 无 `kernelManualTool`/`SecretList` 注册行；`links` 组空随退，secrets 组只剩 `http_request`。
2. `review.go` 表无 `kernel_manual`/`list_secrets` 行；`Known`/`ClassOf` 落 `Normal` default；`review_test` 的 want/retired 断言迁移。
3. `manual.Prompt()`/`Index()` 指引词指 `xbot manual`；`KernelRules` 注入不变。
4. `context_batch_c_test` 改写为 scripted `sh` 调 `xbot manual`，断言正文到达与 prefix 只含目录。
5. `vault_test`/`review_test`/`registry_test`/`http_test` 断言迁移；`tools` door `describe kernel_manual`/`describe list_secrets` 报 unknown。
6. `breaker.go` `looseCall` 无 `list_secrets` 豁免。
7. `xbot manual [section]` 与 `xbot secret list <bot>` 离线行为不变（已有测试保）。
8. `docs/atlas` 三处（security.md readonly 表/kernel.md/memory.md）同步。

## 接受的代价

- 模型读手册/列 secrets 多一次 Shell 调用（`sh` → `xbot manual`/`xbot secret list`），与已迁各 CLI 同形。
- research/readonly platform 下 `xbot manual`/`xbot secret list` 被 `readOnlyShell` 解释器前拒绝、不弹卡不执行——不再是免卡 Readonly，research 子代理完全无手册读取路径。接受此收紧：离线命令无运行实例资格链，不享受 `--self` runtime 调用的只读豁免。
- `looseCall` 的 `list_secrets` 豁免退——连续重读 secrets 列表会熔断（原豁免是为已退工具留的死代码，本就应删）。

完整 caller 合同见 [母路线](../dev/cli-platform-entry.md#批-manualsecrets-两读类离线等价退注册具体合同)。本文保决策边界：不把离线 CLI 冒运行态等价（本就等价——embed/vault 是磁盘文件不是进程态），不把 Shell 审批冒 Readonly 豁免（research 边界收紧是有意的），不把 `KernelRules` 手册指引冒删除（只改入口词）。SINGLE ACTUAL CONCRETE SHAPE PASS（2026-11-02）：已核退役面/保留面/验收十点逐条对照现码，design 全部成立；唯一措辞修正——research 下 `xbot manual`/`xbot secret list` 经 `sh` 实为被 `readOnlyShell` 解释器前硬拒（dispatch.go:160-161,169-183；`ShellIsReadOnly` 白名单 review.go:573-576 无 `xbot`、`allowReadOnlyPlatform` 无 manual/secret 分支），不弹卡不执行，research 子代理完全无手册读取路径，非「走正常 Shell 审批/要过卡」。trusted turn 下 `NeedsOwner` 对 Shell 只查 delete/send 词（review.go:220-225），`xbot` 不在词表不弹卡、与原 Readonly 等效，原句此半写对。本 ADR 转 Accepted 实施；实际入口红与业务证据分别收，不以 unknown tool 冒领域红。Root4USERdocs/OwnerSSE/guard/main/push 日用不动。
