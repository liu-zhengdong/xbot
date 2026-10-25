package kernel

import (
	"context"
	"errors"

	"xbot/internal/config"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// searchProvider reads install-wide preferences on each model request. Cached
// kernels and local executors keep their task control and bound transport.
type searchProvider struct {
	provider.Provider
	root string
}

func (p searchProvider) UnwrapProvider() provider.Provider { return p.Provider }
func (p searchProvider) HostedSearchEnabled() bool {
	f, err := config.LoadWeb(p.root)
	return err == nil && f.HostedSearchOn() && provider.HasHostedSearch(p.Provider)
}
func (p searchProvider) Chat(ctx context.Context, req provider.Request, delta func(string)) (provider.Message, error) {
	f, err := config.LoadWeb(p.root)
	if err != nil {
		return provider.Message{}, err
	}
	target := p.Provider
	if !f.HostedSearchOn() {
		target = provider.WithoutHostedSearch(target)
	}
	// Also cover already-running executors with an older tool directory.
	if !f.SearchOn() || provider.HasHostedSearch(target) {
		list := make([]provider.ToolDef, 0, len(req.Tools))
		for _, t := range req.Tools {
			if t.Name != "web_search" {
				list = append(list, t)
			}
		}
		if req.Tools != nil {
			req.Tools = list
		}
	}
	return target.Chat(ctx, req, delta)
}

func (k *Kernel) searchProvider(p provider.Provider) provider.Provider {
	if p == nil {
		return nil
	}
	root := ""
	if k.Bot != nil {
		root = k.Bot.Root
	}
	return WithSearchSettings(root, p)
}

// configuredSearch is installed once; it never mutates a running tool's fields.
type configuredSearch struct{ root string }

func (s configuredSearch) Search(ctx context.Context, query string, n int) ([]tools.Hit, error) {
	f, err := config.LoadWeb(s.root)
	if err != nil {
		return nil, err
	}
	if !f.SearchOn() {
		return nil, errors.New("网页搜索已关闭")
	}
	id := f.SearchProviderID()
	if id != "ddg" && id != "deepseek" {
		return nil, errors.New("这家暂时还不能搜网页，请选其它家")
	}
	backend, key, err := config.SearchConnection(s.root, f)
	if err != nil {
		return nil, err
	}
	if err := config.AllowSearchBase(s.root, backend); err != nil {
		return nil, err
	}

	if id == "deepseek" && key == "" {
		return nil, errors.New("请先在网页搜索设置里试一下 API 密钥")
	}
	b, err := tools.NewSearch(id, backend.BaseURL, key)
	if err != nil {
		return nil, err
	}
	return b.Search(ctx, query, n)
}

// WithSearchSettings applies the install-wide policy to a model consumer
// outside the foreground loop, including built-in coding tasks.
func WithSearchSettings(root string, p provider.Provider) provider.Provider {
	if p == nil {
		return nil
	}
	return searchProvider{Provider: p, root: root}
}
