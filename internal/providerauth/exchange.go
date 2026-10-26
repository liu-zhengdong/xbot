package providerauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"xbot/internal/config"
	"xbot/internal/vault"
)

func authHTTP() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (m *Manager) finish(ctx context.Context, a *attempt, code, issued string) error {
	var c vault.OAuthCredential
	var body io.Reader
	contentType := "application/x-www-form-urlencoded"
	if a.kind == "openrouter" {
		raw, _ := json.Marshal(map[string]string{"code": code, "code_verifier": a.verifier, "code_challenge_method": "S256"})
		body = strings.NewReader(string(raw))
		contentType = "application/json"
	} else if a.kind == "xai" {
		// Plain form token exchange; no ChatGPT resource or scope expectations.
		body = strings.NewReader(url.Values{"grant_type": {"authorization_code"}, "client_id": {a.clientID}, "code": {code}, "code_verifier": {a.verifier}, "redirect_uri": {a.callback}}.Encode())
	} else {
		if a.clientID == "dynamic_agent_client" {
			if issued == "" || issued == "dynamic_agent_client" {
				return errors.New("注册未返回客户端编号")
			}
			a.clientID = issued
		} else if issued != "" && issued != a.clientID {
			return errors.New("登录返回了不同的客户端编号")
		}
		body = strings.NewReader(url.Values{"grant_type": {"authorization_code"}, "client_id": {a.clientID}, "code": {code}, "code_verifier": {a.verifier}, "redirect_uri": {a.callback}, "resource": {"https://api.openai.com/v1"}}.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.endpoints.Token, body)
	if err != nil {
		return errors.New("无法创建授权请求")
	}
	req.Header.Set("Content-Type", contentType)
	res, err := authHTTP().Do(req)
	if err != nil {
		return errors.New("授权服务连接失败，请重试")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("授权服务拒绝请求（%d），请重新登录", res.StatusCode)
	}
	var token struct {
		Key     string `json:"key"`
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		ID      string `json:"id_token"`
		Type    string `json:"token_type"`
		Expires int    `json:"expires_in"`
		Scope   string `json:"scope"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&token) != nil {
		return errors.New("授权响应格式错误")
	}
	p := config.Provider{Name: a.kind, OAuth: a.kind, BaseURL: a.endpoints.API, Protocol: config.ChatCompletions, Models: []config.ModelSpec{{Name: "openai/gpt-5.4", Context: 128000}}}
	c.AccessToken = token.Key
	if a.kind == "chatgpt" {
		if token.Access == "" || token.Refresh == "" || token.Expires <= 30 || !strings.EqualFold(token.Type, "Bearer") || !strings.Contains(" "+token.Scope+" ", " chatgpt.tokens.use.direct ") {
			return errors.New("授权缺少 ChatGPT 使用权限或可刷新令牌")
		}
		sub, err := validateID(ctx, a, token.ID)
		if err != nil {
			return err
		}
		if a.subject != "" && sub != a.subject {
			return errors.New("登录账号与当前绑定不同，暂不支持账号切换")
		}
		c = vault.OAuthCredential{AccessToken: token.Access, RefreshToken: token.Refresh, IDToken: token.ID, ExpiresAt: time.Now().Add(time.Duration(token.Expires) * time.Second), TokenURL: a.endpoints.Token, ClientID: a.clientID, Resource: "https://api.openai.com/v1", Subject: sub, Scope: token.Scope}
		p.Protocol = config.Responses
	}
	if a.kind == "xai" {
		if token.Access == "" || token.Refresh == "" || token.Expires <= 30 || !strings.EqualFold(token.Type, "Bearer") {
			return errors.New("xAI 授权缺少访问或刷新凭证，请重试")
		}
		sub, err := xaiSubject(a, token.ID)
		if err != nil {
			return err
		}
		if a.subject != "" && sub != "" && sub != a.subject {
			return errors.New("登录账号与当前绑定不同，暂不支持账号切换")
		}
		c = vault.OAuthCredential{AccessToken: token.Access, RefreshToken: token.Refresh, IDToken: token.ID, ExpiresAt: time.Now().Add(time.Duration(token.Expires) * time.Second), TokenURL: a.endpoints.Token, ClientID: a.clientID, Subject: sub, Scope: token.Scope}
		p.Protocol = config.Responses
		p.Models = xaiSeedModels()
	}
	if c.AccessToken == "" {
		return errors.New("授权响应缺少凭证")
	}
	b, err := config.OAuthVault(m.Root)
	if err != nil {
		return errors.New("无法打开保险箱")
	}
	api, _ := url.Parse(a.endpoints.API)
	tokenURL, _ := url.Parse(a.endpoints.Token)
	if err = vault.SetOAuth(b, a.kind, c, []string{api.Hostname(), tokenURL.Hostname()}); err != nil {
		return errors.New("无法保存授权凭证")
	}
	if a.kind == "chatgpt" {
		p.Models, err = config.ChatGPTModels(ctx, b, a.endpoints.API)
		if err != nil {
			return err
		}
	}
	if err = config.SaveOAuthProvider(m.Root, p); err != nil {
		return errors.New("无法保存供应商绑定，请检查同名配置")
	}
	return nil
}

// xaiSubject does a light check of the xAI identity token: the payload must
// name the xAI issuer and echo the nonce. The token arrived over TLS straight
// from the token endpoint, so no signature verification is needed here.
func xaiSubject(a *attempt, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	bad := errors.New("xAI 身份令牌校验失败，请重试")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 32<<10 {
		return "", bad
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", bad
	}
	var id struct {
		Issuer  string `json:"iss"`
		Subject string `json:"sub"`
		Nonce   string `json:"nonce"`
	}
	if json.Unmarshal(payload, &id) != nil || id.Issuer != a.endpoints.Issuer || (id.Nonce != "" && id.Nonce != a.nonce) {
		return "", bad
	}
	return id.Subject, nil
}

// xaiSeedModels seeds the bound model list with the built-in grok models; the
// live catalog is pulled on demand from the endpoint's /models.
func xaiSeedModels() []config.ModelSpec {
	hosted := true
	models := []config.ModelSpec{}
	if v, ok := config.VendorFor("xai"); ok {
		for _, m := range v.Models {
			m.HostedSearch = &hosted
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		models = []config.ModelSpec{{Name: "grok-4", Context: 256000, HostedSearch: &hosted}}
	}
	return models
}
