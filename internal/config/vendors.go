package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Vendor is a built-in API-key vendor the settings grid can add with one key.
// Defaults come from the vendors' own OpenAI-compatible endpoints; nothing here
// is a secret. KeyEnv is only the environment variable's name.
type Vendor struct {
	Brand    string
	Title    string
	Protocol string
	BaseURL  string
	KeyEnv   string
	Models   []ModelSpec
	// KeyLogin means the vendor has no plain API-key route yet (Vertex wants a
	// Google Cloud sign-in). It is listed with its models but cannot be added.
	KeyLogin bool
	// Hosts are the hostnames (or ".suffix" domains) that identify the brand.
	Hosts []string
}

// ExistingBrands are the vendors the grid already had; their defaults live
// elsewhere and are not changed by the vendor table.
var existingBrands = []struct {
	Brand, Title string
	Hosts        []string
}{
	{"deepseek", "DeepSeek", []string{"api.deepseek.com"}},
	{"openai", "OpenAI", []string{"api.openai.com"}},
	{"anthropic", "Anthropic", []string{"api.anthropic.com"}},
	{"openrouter", "OpenRouter", []string{"openrouter.ai"}},
}

func specs(context int, names ...string) []ModelSpec {
	out := make([]ModelSpec, 0, len(names))
	for _, n := range names {
		out = append(out, ModelSpec{Name: n, Context: context})
	}
	return out
}

// Vendors is the grid order after the four existing brands.
var Vendors = []Vendor{
	{Brand: "gemini", Title: "Google Gemini", Protocol: ChatCompletions, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", KeyEnv: "GEMINI_API_KEY",
		Models: specs(1048576, "gemini-2.5-pro", "gemini-2.5-flash"), Hosts: []string{"generativelanguage.googleapis.com"}},
	{Brand: "vertex", Title: "Vertex AI", Protocol: ChatCompletions, KeyEnv: "GOOGLE_API_KEY", KeyLogin: true,
		Models: specs(1048576, "gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.6-flash", "gemini-3.5-flash", "gemini-3.5-flash-lite",
			"gemini-3.1-pro-preview", "gemini-3.1-flash-lite", "gemini-3-flash-preview", "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite"),
		Hosts: []string{"aiplatform.googleapis.com", ".aiplatform.googleapis.com"}},
	{Brand: "xai", Title: "xAI", Protocol: ChatCompletions, BaseURL: "https://api.x.ai/v1", KeyEnv: "XAI_API_KEY",
		Models: specs(256000, "grok-4"), Hosts: []string{"api.x.ai"}},
	{Brand: "kimi", Title: "Kimi", Protocol: ChatCompletions, BaseURL: "https://api.moonshot.ai/v1", KeyEnv: "MOONSHOT_API_KEY",
		Models: specs(262144, "kimi-k3"), Hosts: []string{"api.moonshot.ai", "api.moonshot.cn"}},
	{Brand: "kimi-code", Title: "Kimi Code", Protocol: ChatCompletions, BaseURL: "https://api.kimi.ai/coding/v1", KeyEnv: "KIMI_API_KEY",
		Models: specs(262144, "kimi-for-coding"), Hosts: []string{"api.kimi.ai", "api.kimi.com"}},
	{Brand: "zhipu", Title: "Zhipu GLM", Protocol: ChatCompletions, BaseURL: "https://open.bigmodel.cn/api/paas/v4", KeyEnv: "ZHIPU_API_KEY",
		Models: specs(200000, "glm-5.2"), Hosts: []string{"open.bigmodel.cn"}},
	{Brand: "zai", Title: "Z.ai", Protocol: ChatCompletions, BaseURL: "https://api.z.ai/api/paas/v4", KeyEnv: "ZAI_API_KEY",
		Models: specs(200000, "glm-5.2"), Hosts: []string{"api.z.ai"}},
	{Brand: "minimax", Title: "MiniMax", Protocol: ChatCompletions, BaseURL: "https://api.minimax.io/v1", KeyEnv: "MINIMAX_API_KEY",
		Models: specs(204800, "minimax-m3"), Hosts: []string{"api.minimax.io", "api.minimaxi.com"}},
	{Brand: "stepfun", Title: "StepFun", Protocol: ChatCompletions, BaseURL: "https://api.stepfun.ai/step_plan/v1", KeyEnv: "STEPFUN_API_KEY",
		Models: specs(262144, "step-3.5-flash"), Hosts: []string{"api.stepfun.ai", "api.stepfun.com"}},
	{Brand: "mimo", Title: "Xiaomi MiMo", Protocol: ChatCompletions, BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", KeyEnv: "MIMO_API_KEY",
		Models: specs(262144, "mimo-v2.6-pro", "mimo-v2.6-flash"), Hosts: []string{".xiaomimimo.com"}},
	{Brand: "qianfan", Title: "Baidu Qianfan", Protocol: ChatCompletions, BaseURL: "https://qianfan.baidubce.com/v2", KeyEnv: "QIANFAN_API_KEY",
		Models: specs(131072, "deepseek-v4-pro", "deepseek-v4-flash", "glm-5.2"), Hosts: []string{"qianfan.baidubce.com"}},
	{Brand: "tencent", Title: "Tencent Cloud", Protocol: ChatCompletions, BaseURL: "https://tokenhub.tencentmaas.com/v1", KeyEnv: "TENCENT_API_KEY",
		Models: specs(200000, "glm-5.2", "hy3"), Hosts: []string{"tokenhub.tencentmaas.com", "tokenhub-intl.tencentmaas.com"}},
}

// VendorFor finds a built-in vendor by brand id.
func VendorFor(brand string) (Vendor, bool) {
	for _, v := range Vendors {
		if v.Brand == brand {
			return v, true
		}
	}
	return Vendor{}, false
}

// BrandForHost names the brand an endpoint host belongs to, or "".
func BrandForHost(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return ""
	}
	match := func(hosts []string) bool {
		for _, h := range hosts {
			if h == host || (strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)) {
				return true
			}
		}
		return false
	}
	for _, b := range existingBrands {
		if match(b.Hosts) {
			return b.Brand
		}
	}
	for _, v := range Vendors {
		if match(v.Hosts) {
			return v.Brand
		}
	}
	return ""
}

// BrandForBase is BrandForHost for a base URL.
func BrandForBase(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return ""
	}
	return BrandForHost(u.Hostname())
}

// ErrNoKeyRoute is returned for a vendor that cannot be added with an API key.
var ErrNoKeyRoute = errors.New("这家供应商暂不支持直接填密钥，需要先登录 Google Cloud")

// AddVendor registers a built-in vendor's defaults in providers.json. It never
// overwrites an entry the user already has under the same name, and it is safe
// to repeat. It writes no secret.
func AddVendor(root, brand string) (Provider, error) {
	v, ok := VendorFor(brand)
	if !ok {
		return Provider{}, fmt.Errorf("providers: unknown vendor %q", brand)
	}
	if v.KeyLogin {
		return Provider{}, ErrNoKeyRoute
	}
	f, err := Load(root)
	if err != nil {
		return Provider{}, err
	}
	if i := indexOf(f.Providers, v.Brand); i >= 0 {
		return f.Providers[i], nil
	}
	for n, m := range v.Models {
		p := Provider{Name: v.Brand, Protocol: v.Protocol, BaseURL: v.BaseURL, APIKeyEnv: v.KeyEnv}
		if n > 0 {
			p = Provider{Name: v.Brand}
		}
		if err := Add(root, p, m); err != nil {
			return Provider{}, err
		}
	}
	f, err = Load(root)
	if err != nil {
		return Provider{}, err
	}
	return f.Providers[indexOf(f.Providers, v.Brand)], nil
}
