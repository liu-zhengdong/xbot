---
kind: adr
guard: owner
status: Accepted
date: 2026-11-02
---
# 0058 经历记录与检索沿原 source CLI

## 范围与调用方

TRUE Fork main06daa397后，仅迁experience_record与experience_search；不混Memory Recall/person资格、Context Compact/History/Expand/Detail、全库新服务或全局DuckDB教训库。0058先glob空后新建，6e54具体合同单次SHAPE通过后Accepted，沿该形状实施。实际协议见 [API](../API.md#经历记录与检索运行时-cli)，精确证据见 [MILESTONES](../MILESTONES.md)，未迁族留 [母路线](../dev/cli-platform-entry.md)。cmd/xbot实际grep无experience旧noun；选独立 `experience record --self (--summary TEXT|--summary-file FILE|-) [--focus TEXT|--focus-file FILE|-] [--from N] [--to N] [--json]`、`experience search --self [--query TEXT|--query-file FILE|-] [--limit N] [--json]` 及离线help。字段值按arity消费literal flags/--，stdin至多一个字段；不改既有有效CLI或ownerSSE。

## 两种实际结构

A直接离线SQL/Markdown写入或Web Store.Page：会跳过操作时range、segmentDigest、source Clock/currentRef、原BM25池/6KiB呈现或误把Web全库字面分页当模型检索，淘汰。B既有listener加ExperienceHandlers{Record,Search}两callback，调用原source k.RecordExperience/SearchExperiences与原currentIO/Clock/Store，选B。不新Kernel/MemorySession/SQLite封装服务、源登记表或metaAction框架；现primitive忽略ctx，不借上刀fork强加MandatoryLiveCallID/preparedTurn.fromSeq/primary或owner-person授权。原root/Host/browser/loopback guard及Shell自身gate仍保。

## 领域与真实消费者

summary原Trim非空≤2KiB；focus只Trim没有独立领域cap，query沿原空白判断及BM25文本，不复制Web512B查询限制。raw文件/STDIN及最终escaped请求各≤1MiB。from/to按原整数域传递，0默认操作时currentCount、最近200行最新RoleUser且非Internal/ForkMark到尾；非新Owner身份判断，空库可0/0，不拿prepared轮次替换，不顺手修原负数边界/Store nullSeq历史语义。

segment原范围前200行、每行160B/总约600B；保user:/assistant:/[tools:names]词法、跳内部/分叉标记/Tool结果，只截提纲不模型生成。Store.Add原ONEINSERT/LastInsertId、首记录建库；缺DB的Recent/Latest/Get/Page只exists不建库，已有DB reader原open可建表/WAL，不承诺只读全root零字节。Add error不证明库/row绝未产生，不stat猜事务、不自动重试/去重/回滚或新Store进度框架。

RecordReply仅原Entry的ID/from/to与nil成功确认，Recorded *bool：原函数nil才true，error为null（未确认、不承诺零副作用），原返回e即使error含ID/range也如实带；不回summary/focus/segment全文，不造MemoryOwnReceipt/假native配对/新RoleUser事实。SearchReply仅Text或File/Access/Bytes及Error：Text必须是原SearchExperiences已presentHistory脱敏且≤6KiB的语义结果，默认limit8、非法limit回原8；空query最新、非空最近2000 BM25，不构造原始Entry rows或Count猜投影。原搜索文本的截断边界保持，不靠artifact补回被6KiB裁掉的records。

实际整JSON含NL与准确human分别≤Shell32KiB、HTTP64KiB。6KiB文本含控制字符/HTML转义可JSON>32KiB，保存的是完整该Text回包JSON，复用work.WriteResult当前source workspace/0600/≤20MiB与既有File/Access/Bytes样式；预检metadata两格式，源短currentIO取当前work路径，不能先取旧name后迟写。无通用artifact框架/抬cap/截records；失败明确非0不冒有全文引用，record ACK不因展示丢真实ID/range/确认。

DB id-created_at-summary-focus-from_seq-to_seq-segment原字段全保；Web memory_panel.Page仍全库literal英文忽略大小写/created_at-id分页，app6358仍按原segment词法投影并留原文。下一自然环境experience count/latestID+200Bsummary、读错保已发布旧值及原diff；不额外Inbox/waker。原detail按rawseq或experience+0based范围读真正完整blob/64KiB锚仍live，历史native字面保持；无专用依赖recordedexperience旧tool文本的重建器。

## 交付与验证

单次具体SHAPE后才冻最窄source-record入口红。首可编译原source record→SQLite各列→Web字段/Segment→下一provider既有env→search latest/BM25→still-live detail原rawseq/经验offset实际完整ToolBody链早交一次，证明无模型/无prepared强绑、非primary/staleBOT当前source与默认操作时bounds。再同cut真实JSON/human双预算artifact/currentRename、record错误未确认/无自动重试、readonly及两native退门，不等首反馈停，不扩大Memory/Context旧族。

边界按原primitive真实输出：summary2KiB/trim、focus大FILE、raw与escaped1MiB、0/explicit范围与旧宽容语义、segment跳Tool结果/原词法、limit非法回8/最近2000与Web全库不同、缺DB搜索不建库/已有DB读原副作用。测试Add错误不要求造LastInsertId-only故障全局钩子；真实FS/SQLite错误足够，不从error假零写。source身份只listener/root，无CallID仍可按原功能，不加审批卡/approver函数；research仅help+search self、literalhelp值不放record。只退两wrapper/Known/schema/index与指导，memory组尚有recall，context四门/Link.Tools空壳不拆。Root4USERdocs/ownerSSE/guard/main/push日用不动，Fork7/Message6/Group5等及原三审批封不重。
