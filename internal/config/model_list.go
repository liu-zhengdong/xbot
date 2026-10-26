package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"xbot/internal/compact"
	"xbot/internal/vault"
)

// ProviderModels asks the configured endpoint for its current catalog. Only
// membership comes from the vendor; capability overrides stay in providers.json.
// Nothing is persisted and neither upstream error bodies nor credentials escape.
func ProviderModels(ctx context.Context, root, name string) ([]ModelInfo, error) {
	p, endpoint, err := catalogEndpoint(root, name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if endpoint.HTTPClient != nil {
		client.Transport = endpoint.HTTPClient.Transport
	}
	models := []ModelInfo{}
	var accountModels []ModelSpec
	if p.OAuth == "chatgpt" {
		backend, err := OAuthVault(root)
		if err != nil {
			return nil, errors.New("请重新登录 ChatGPT")
		}
		accountModels, err = ChatGPTModels(ctx, backend, p.BaseURL)
		if err != nil {
			return nil, err
		}
	}
	seen, cursors := map[string]bool{}, map[string]bool{}
	remaining, cursor := int64(1<<20), ""
	specs := make(map[string]ModelSpec, len(p.Models))
	for _, spec := range p.Models {
		specs[spec.Name] = spec
	}
	for {
		var page catalogPage
		var size int64
		if p.OAuth == "chatgpt" {
			for _, spec := range accountModels {
				page.Data = append(page.Data, catalogItem{ID: spec.Name})
				if _, configured := specs[spec.Name]; !configured {
					specs[spec.Name] = spec
				}
			}
		} else {
			page, size, err = readCatalogPage(ctx, client, p, endpoint.APIKey, cursor, remaining)
			if err != nil {
				return nil, err
			}
		}
		remaining -= size
		for _, item := range page.Data {
			id := strings.TrimSpace(item.ID)
			if id == "" || len(id) > 256 {
				return nil, errors.New("模型编号无效")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			if len(models) >= 2048 {
				return nil, errors.New("模型列表超过 2048 项")
			}
			spec := ModelSpec{Name: id, Context: compact.DefaultWindow}
			if configured, ok := specs[id]; ok {
				spec = configured
			}
			_, registered := registeredSpec(p, id)
			if spec.Context <= 0 {
				spec.Context = compact.DefaultWindow
			}
			models = append(models, ModelInfo{ID: ModelID(name, id), Provider: name, Model: id, Context: spec.Context, Protocol: p.WireProtocol(), Vision: spec.SeesImages(), HostedSearch: spec.HasHostedSearch(p.WireProtocol()), Available: SupportedBinding(p.WireProtocol(), spec) == nil,
				HostedImageGen: spec.HasHostedImageGen(p.WireProtocol()), ImageGenModel: spec.ImageGenModel, Registered: registered})
		}
		if !page.HasMore {
			break
		}
		if p.WireProtocol() != AnthropicMessages || page.LastID == "" || cursors[page.LastID] || len(cursors) >= 16 {
			return nil, errors.New("模型列表分页无效")
		}
		cursor = page.LastID
		cursors[cursor] = true
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })
	raw, err := json.Marshal(models)
	if err != nil || len(raw) > (1<<20)-32 {
		return nil, errors.New("模型能力列表超过 1 MiB")
	}
	return models, nil
}

func registeredSpec(p Provider, name string) (ModelSpec, bool) {
	for _, m := range p.Models {
		if m.Name == name {
			return m, true
		}
	}
	return ModelSpec{}, false
}

func catalogEndpoint(root, name string) (Provider, ModelConfig, error) {
	f, err := Load(root)
	if err != nil {
		return Provider{}, ModelConfig{}, errors.New("无法读取供应商配置")
	}
	p := Provider{Protocol: ChatCompletions}
	var endpoint ModelConfig
	if name == "" {
		m := FromEnv(Getenv)
		p.BaseURL, p.APIKeyEnv = m.BaseURL, "XBOT_API_KEY"
		endpoint, _, err = Resolve(root, "", "", Getenv)
	} else {
		i := indexOf(f.Providers, name)
		if i < 0 {
			return Provider{}, ModelConfig{}, errors.New("供应商尚未配置")
		}
		p = f.Providers[i]
		if len(p.Models) == 0 {
			return Provider{}, ModelConfig{}, errors.New("供应商配置缺少模型")
		}
		endpoint, _, err = Resolve(root, name, p.Models[0].Name, Getenv)
	}
	if err != nil || strings.TrimSpace(endpoint.APIKey) == "" {
		if p.OAuth == "chatgpt" {
			return Provider{}, ModelConfig{}, errors.New("请重新登录 ChatGPT")
		}
		return Provider{}, ModelConfig{}, errors.New("请先配置供应商密钥或登录凭证")
	}
	if checkProviderBase(p.BaseURL) != nil || ValidProtocol(p.WireProtocol()) != nil {
		return Provider{}, ModelConfig{}, errors.New("供应商地址或协议无效")
	}
	return p, endpoint, nil
}

type catalogItem struct {
	ID string `json:"id"`
}

type catalogPage struct {
	Data    []catalogItem `json:"data"`
	HasMore bool          `json:"has_more"`
	LastID  string        `json:"last_id"`
}

func readCatalogPage(ctx context.Context, client *http.Client, p Provider, key, cursor string, remaining int64) (catalogPage, int64, error) {
	u, _ := url.Parse(strings.TrimRight(p.BaseURL, "/") + "/models")
	if p.WireProtocol() == AnthropicMessages {
		q := u.Query()
		q.Set("limit", "1000")
		if cursor != "" {
			q.Set("after_id", cursor)
		}
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return catalogPage{}, 0, errors.New("供应商地址无效")
	}
	if p.WireProtocol() == AnthropicMessages {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		// A provider-rejected refresh is a dead credential, not a network
		// hiccup; the settings page turns it into a re-login prompt.
		if vault.IsRefreshRejected(err) {
			return catalogPage{}, 0, errors.New("登录凭证已失效，请重新登录")
		}
		return catalogPage{}, 0, errors.New("拉取模型失败，请检查网络后重试")
	}
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, remaining+1))
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return catalogPage{}, 0, fmt.Errorf("拉取模型失败（HTTP %d）", res.StatusCode)
	}
	if readErr != nil || int64(len(raw)) > remaining {
		return catalogPage{}, 0, errors.New("模型列表过大或读取失败")
	}
	var page catalogPage
	if json.Unmarshal(raw, &page) != nil || page.Data == nil {
		return catalogPage{}, 0, errors.New("模型列表格式无效")
	}
	return page, int64(len(raw)), nil
}
