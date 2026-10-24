package config

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"xbot/internal/compact"
	"xbot/internal/vault"
)

// ChatGPTModels reads the SIWC account catalog for both sign-in and settings.
// Credentials are injected only by the existing vault transport.
func ChatGPTModels(ctx context.Context, backend vault.Backend, base string) ([]ModelSpec, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, errors.New("ChatGPT 地址无效")
	}
	r.Header.Set("Authorization", "Bearer {{secret:chatgpt}}")
	client := &http.Client{Transport: vault.OAuthTransport(backend, "chatgpt"), Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(r)
	if err != nil {
		return nil, errors.New("读取 ChatGPT 模型列表失败，请重新登录")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, errors.New("请重新登录 ChatGPT")
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("ChatGPT 模型列表不可用，请重试")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("ChatGPT 模型列表过大或读取失败")
	}
	var catalog struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Context    int    `json:"context_window"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &catalog) != nil || catalog.Models == nil {
		return nil, errors.New("ChatGPT 模型列表格式无效")
	}
	models, seen := []ModelSpec{}, map[string]bool{}
	for _, m := range catalog.Models {
		if m.Visibility != "list" {
			continue
		}
		id := strings.TrimSpace(m.Slug)
		if id == "" || len(id) > 256 {
			return nil, errors.New("ChatGPT 模型编号无效")
		}
		if seen[id] {
			continue
		}
		if len(models) >= 2048 {
			return nil, errors.New("ChatGPT 模型列表超过 2048 项")
		}
		seen[id] = true
		if m.Context <= 0 {
			m.Context = compact.DefaultWindow
		}
		models = append(models, ModelSpec{Name: id, Context: m.Context})
	}
	if len(models) == 0 {
		return nil, errors.New("账号没有可用模型，请重新登录 ChatGPT")
	}
	return models, nil
}
