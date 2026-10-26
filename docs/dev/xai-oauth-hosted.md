# xAI：OAuth、可重登、hosted tools（一批）

> **结论先行**：xAI 主路径改为 **OAuth（PKCE + 本机回调）**；失败态可 **取消 / 重新登录**；OAuth 绑定走 **Responses**，默认挂厂商 hosted `web_search`（互斥本地 search），并按能力挂 `x_search` / `code_interpreter`；API key 路径保留但非主 CTA。对照 [atlas/providers](../atlas/providers.md)、[multi-provider](multi-provider.md)、ADR 0005/0013/0023。

## 怎样算对

1. 认证失败 / 进行中：UI 有明确「取消」与「重新登录」；取消后可立刻再开 OAuth；重走能成功或给出人话错误（不卡死、不假成功）。
2. 设置·模型与供应商：xAI 主 CTA 是 OAuth 登录；密钥表单若仍在则为次要（文案/按钮权重低于登录）。
3. Bot 绑 `xai` + grok 模型后：全局网页搜索自动模式下走厂商 hosted `web_search`（本绑定无本地 `web_search`）；可选同时开 `x_search`、`code_interpreter`（Responses tools 数组）。失败不假装成功（ADR 0023）。
4. 令牌进现有 vault `_provider_accounts`；可 refresh；临近过期自动换；刷新失败提示重新登录。
5. `go test` 相关包绿；合入本地 main（不 push）→ Mini 8788 → 手测。

## 产品行为

| 动作 | 结果 |
|---|---|
| 点 xAI「登录」 | 开浏览器 `auth.x.ai` PKCE；本机 `127.0.0.1:56121/callback` 收码（Grok CLI 公共 client 固定回调） |
| 登录成功 | 写入 `oauth: xai` 供应商；protocol=`responses`；base=`https://api.x.ai/v1`；模型名单拉取或种子 grok；vault 存 access/refresh |
| 失败 / 过期 / 拒权 | status=`failed` + 人话错误；UI「取消」「重新登录」 |
| 取消 | 关掉 pending 回调监听，status=`cancelled`/`idle`，可立刻再 Begin |
| 重新登录 | 同 Begin；同账号刷新凭证；模型覆盖字段保留（对齐 ChatGPT/OpenRouter） |
| 密钥 | 未 OAuth 时仍可填 `XAI_API_KEY`（Completions 路径保留）；已 OAuth 时不把密钥当主入口 |

## 技术要点（给编码）

### OAuth

- 复用 `internal/providerauth`：`defaults("xai")` → Authorize `https://auth.x.ai/oauth2/authorize`，Token `https://auth.x.ai/oauth2/token`，API `https://api.x.ai/v1`，Issuer `https://auth.x.ai`，JWKS `https://auth.x.ai/.well-known/jwks.json`。
- **公共 client_id**（与 Grok CLI / 各开源客户端一致，无第三方控制台可注册）：`b1a00492-073a-47ea-816f-4c329264a828`。Authorize 必须带 `plan=generic`，否则 loopback 被拒；`referrer=xbot` 仅归因。
- Scope：`openid profile email offline_access grok-cli:access api:access`。
- 回调：**固定** `http://127.0.0.1:56121/callback`（勿用动态端口）。端口占用 → 人话错误「本机 56121 被占用，请关闭占用进程后重试」。
- Token 交换：`application/x-www-form-urlencoded`（非 JSON）；保存 refresh；**不要**套 ChatGPT 的 `chatgpt.tokens.use.direct` / `resource=api.openai.com` 校验。
- `config.validateOAuth`：增加 `xai` → protocol Responses + base `https://api.x.ai/v1`。
- `Manager.Cancel(kind)` + `POST /api/providers/oauth/{kind}/cancel`（owner）；ChatGPT/OpenRouter 共用取消能力。Begin 在同 kind pending 时可先允许 Cancel 再重开，或 Begin 遇 cancelled 清掉。
- Vault `refreshOAuth`：空 `Resource` 勿强制送空 `resource=`；ChatGPT scope 校验仅当 credential 带 ChatGPT resource/scope 期望时执行（xAI 刷新不得因缺 chatgpt scope 失败）。

### Hosted tools

- OAuth 落盘的 xAI 绑定：`protocol: responses`；grok 模型 `hosted_web_search` 默认 true（`HasHostedSearch` 对 `grok-*` / `xai` Responses 放行，或显式写 true）。
- `internal/provider/responses.go`：在已有 `web_search` 之外，当 Endpoint 标明 xAI hosted 扩展时追加 `{"type":"x_search"}`、`{"type":"code_interpreter"}`（本批范围；Image Generation / Collections Search **不做**）。
- 解析：`web_search_call` 已有；为 `x_search_call` / code 类 server tool 落 `PartHosted`（名称区分），未知类型降级文字，不假装成功。
- 密钥 Completions 路径：本批不强制改 protocol；OAuth 主路径才上 hosted。

### UI（`providers.js`）

- xAI 详情：未登录主按钮「登录 xAI」；进行中显示「取消」+「重新打开」；已登录「重新登录」；失败文案可读 + 取消/重试。
- 密钥表单降为次要（如折叠「用 API 密钥」或次按钮），禁工程词。
- 本机已存密钥、但还没 OAuth 时，详情仍以「登录 xAI」为主（状态显示「密钥已配置」），不先拉模型列表；密钥只在折叠入口里改。
- 浏览器单测场景补 xAI OAuth pending/cancel/fail（对齐现有 chatgpt fixtures）。

### 不做

- 设备码（RFC 8628）本批不做（本机桌面有 loopback 即可）。
- Claude 订阅 OAuth、多账号切换、远程 runner 转交 OAuth。
- 公网薄入口 / 多设备配对（另条暂停中）。
- Image generation / Collections Search hosted。

## 自检

- [ ] `go test ./internal/providerauth ./internal/config ./internal/vault ./internal/provider ./internal/web -count=1` 相关绿
- [ ] 无 Claude 订阅 OAuth 入口
- [ ] 失败不标已登录；取消后可重开
- [ ] atlas/providers 一句回写 OAuth 含 xAI + hosted 范围
