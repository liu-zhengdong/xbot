---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0056 Bot 单聊沿原本地 Bus 与加密 Remote 发送进入 CLI

## 问题与调用方

聊天群已TRUE main8caacdca，本刀只迁message_bot{bot,text}的LOCAL与bot@device两分支。0056 glob空后建Proposed；选 `"$XBOT_BIN" peer-message TARGET --self (--text TEXT|--file FILE|-) [--json]` 和离线help，不抢owner message-SSE、externalAgent send或侧栏group命名/步骤。相应用户root设计只读。source真实prepared调用链、目标trim与原Bus/Remote规则必须保持，不能静默只做local或冒ownerKind。具体typed合同与验收在 [母dev](../dev/cli-platform-entry.md#批-peer-message本地与配对设备单聊具体合同pass实施中)。

## 两种实际结构与选择

A：owner HTTP messages/SendOwnerChat、离线Load/newK或单独拼Remote HTTP。这改变Bot/主人身份、原chain/hops及配对/加密/queue规则，淘汰。B：原listener新增一个typed PeerMessage callback，用同currentResources/source和已在生产的runtimeSourceCall取原prepared frozen turnKey；LOCAL原Bus.Message，含@原remote.Send与原outbox。选B，不引remoteSender/policyPerson/receiveScope之类现不存在的生产注入wrapper、ctx/Job表或单聊私账。

## 实际写点与不确定性

原Bus.Message先local可见owner交集/self/hops/text校验、原from→to rate，再Load/Open/Inbox.Post；仅Post返回nil才posted:true/event_id，失败不把已消耗rate或可能原JSONL尾字节说成全root无写。旧error签名如需兼容，薄适配到唯一MessageWithResult writer。

remote.Send按原ByName精确配对设备/unpair/NoBotMessaging/XKey/sourceOwners含d.Person检查，不加remote目录缓存目标先存在的门。trim后1..8KiB/hops≤3、RedactText后再校验；原Identity/X25519密文与签名信封、remote-out独立rate、15s Gateway.Send保持。旧Send(bool,error)薄适配到同一SendWithResult；EnvelopeID只来自实际建成的原env。Gateway=not_attempted/accepted/http_error/unknown：只有原Client.Send nil即HTTP2xx且response读成功才accepted，不是对端Inbox/已读；typed HTTPError只给实际status，其他transport/response错误unknown，不能error推网关没收。

只有原retryable（transport/response、5xx含507、429、408）才enqueue同ENV/ID，256项8MiB锁内atomic0600成功才queued:true/nil；queue失败保已知gateway事实/unknown，不能false说未发或重建env确认。原5s检查/同ID指数退避至10min/10s请求/24h过期、后来NoBotMessaging或永久拒绝时给source普通未送达Inbox通知及移项均沿原，操作错不冒完成。Receive原验签/解密/paired/person-owner/目标/hop-rate/稳定remote_FROMINSTALL_ENVID+seen7d4096去重及拒绝也ACK/不可信Neutralize语义保，不把ACK当已读，不加waker或交付账。

## 验证与边界

先具体合同单次SHAPE，caller固定后独立最窄红；首compiled LOCAL真实原Shell→typed→原Inbox/消费链一次，再同cut完成Remote真实事实/退唯一message_bot。remote包private pairTransport/实际gateway.Handler单位可验真签名加密/response-lost-queuefault-sameID重试，但不能冒称子serve独立。显式合法HTTP loopback Gateway现Config可用：fixture先本机test gateway，再显式SetGateway/真实public pair身份，才devices/serve restart，禁止devices+空gateway让child接默认真网关。合法最窄accepted/queued-sameID/Receive场景尽量独立跑；若某层无法安排，按具体层级NOT RUN且用真实单位证据，不静默localOnly完成或加生产fakeflag/Client依赖框架。短回执整JSONNL与准确human各≤32KiB/HTTP64KiB、raw与escaped请求各1MiB，不造通用artifact/结果系统。

原RemoteConversation的远端outgoing真实消费者依赖native args/result，file/stdin不能事后重读或猜Shell。两最小形状：A塞原Shell ToolResult全文/改raw调用，需要percall聚合或最坏>32K回执且损原始调用，淘汰；B在原source transcript追加一次OriginInternal+RoleUser普通专属tag/JSON事实，选B。仅REMOTE已形成真实source尝试后、原Send/queue已得实际result+err时，recordTurn显式prepared.callTurnKey的实际原execution owner（动态branch），消息Internal:true，完整冻结请求target/text经原scrub，payload附实际Gateway/status/queued/EnvelopeID/error。chain/hops仍取prepared.turnKey frozen，二者不混；nilResources原短IO直接原Bot/Inbox，不另开Inbox。无EventID/Inbox/wake、不伪ToolCall或unpaired ToolResult、不加Entry字段/Job协议或第二DM账。

RemoteConversation只信OriginInternal/RoleUser/EventID空+精确新fact格式，新row由实际transcript seq派生稳定ID；queued优先queued、gateway accepted为sent、unknown而未queued为unknown、其它未确认失败failed，旧native pairedhistory/旧pending默认sent及incoming/bounce路径保。全文不进短ACK，recorded/record_seq只AppendSeq成功后给；记录错误保原Gateway/Queued事实并非0，不回滚/重发。源current route/recordTurn原scrub与动态原branch保持，node1MiB单entry边界失败如实报，不截body补成功/造crash事务。LOCAL原Conversation从双方recipient事件读，无理由再记source事实。fork/其余platform与Event/Link装配继续后刀，root/main、用户dirtydocs、guard、日用/push/release不动。
