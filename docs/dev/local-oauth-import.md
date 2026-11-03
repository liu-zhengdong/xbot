# 订阅凭据本机导入（扫描本机 CLI 登录，直接进 vault）

> 源设计：`Obsidian 设计/订阅凭据本机导入.md`（2026-11-02，政东已确认）。

## 需求

xbot 已支持自己走 OAuth（PKCE + 本机回调，chatgpt / xai / openrouter）。用户本机往往已经登录各家 CLI：`codex`、`grok` 等。订阅凭据在 `~/.codex/auth.json`、`~/.grok/auth.json` 这类文件里。

设置 · 模型与供应商的订阅卡检出本机已有登录时，给「导入」入口。一键把凭据拷进 vault `_provider_accounts`，之后走已有 `{{secret:}}` + `OAuthTransport` 自动 refresh。与「浏览器登录」并列，下游同一套。

## 怎样算对

1. 检出 `~/.codex/auth.json`（ChatGPT 登录态）时，ChatGPT 卡显示「发现本机 Codex 登录 · <邮箱>」+「导入」。点了之后凭据进 vault、provider 绑定生成、卡变已登录，不弹浏览器。
2. `~/.grok/auth.json` 检出时 xAI 卡同理。
3. 导入凭据过期走 `refreshOAuth` 自动换新；refresh 被拒时卡回「登录未完成」，可重登或重新导入。
4. 不导入时 UI 不变；检不出凭据时不显示入口。
5. 本批只做 Codex + Grok；**不回写原文件**；不碰 Keychain。
6. `go test` 相关包绿；合入本地 main。

## 方案

`internal/providerauth/import.go`：凭据源表 `{kind, 文件路径, 解析函数, tokenURL, clientID 规则}`。`Scan(kind)` 读文件解析成 `OAuthCredential` + 展示用 account 名；`Import(kind)` 写 `_provider_accounts` vault + `PrepareOAuthProvider` 落绑定。

- **Codex**：只认 `auth_mode=="chatgpt"` 或 `tokens` 非空。`client_id` 取 `oauth_provenance.client_id`。`TokenURL`/`Resource`/`Subject` 同 providerauth chatgpt。
- **Grok**：遍历 map 值取第一个含 `refresh_token` 的条目。access token 在 `key` 字段，`expires_at` RFC3339，`client_id` = `oidc_client_id`。
- 文件读不出/解析失败 → 不显示导入。
- 新 token 只写 vault，不回写原文件。
- config/providers.json 只写 `oauth:` 引用名，不落明文。

## Web / UI

`internal/web/oauth.go`：`GET /api/providers/oauth/{kind}/local` → `{available, account}`（只读，不含 token）；`POST /api/providers/oauth/{kind}/import`（owner）。订阅卡：`local.available` 且未登录时，主 CTA 旁加次按钮「导入本机登录（<邮箱>）」。

## 安全边界

- 读用户自己 home 下文件；不升权、不读 Keychain。
- 导入是显式点击；探测只回元数据。
- vault 域白名单沿用各家 token 端点，不新增。

## 测试 / 冒烟

- 单测：临时 HOME 放假 `auth.json`（chatgpt 登录、api_key 登录、半写、缺 refresh），断言解析与 Import 端到端。
- 冒烟：设置页点导入 → 卡变已登录（复用 loopback 假授权服务器框架）。
- 真机手测：导入后发消息。

## 还没做的

- Claude 订阅导入（Keychain + ToS 另拍板）。
- ZCode/GLM OAuth、Pi `openai-codex` 去重、多账户。
- 「自动导入」开关。
