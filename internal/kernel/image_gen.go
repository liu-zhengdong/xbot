package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/config"
	"xbot/internal/provider"
	"xbot/internal/work"
)

// One turn chooses either the binding's hosted tool or the selected account's
// local proxy. Both use the same Responses parser and workspace save hook.
type imageGenCapability struct {
	gen   provider.ImageGen
	proxy provider.Provider
}

func (k *Kernel) resolveImageGen(ctx context.Context) imageGenCapability {
	var cap imageGenCapability
	if k.Bot == nil {
		return cap
	}
	f, err := config.LoadWeb(k.Bot.Root)
	if err != nil || !f.ImageGenOn() {
		return cap
	}
	kind := config.ImageGenOAuth(f.ImageGenProvider)
	if kind == "" {
		return cap
	}
	cfg := k.Bot.Config()
	p, spec, err := config.Lookup(k.Bot.Root, strings.TrimSpace(cfg.Provider), strings.TrimSpace(cfg.Model))
	hosted := false
	if f.ImageGenPolicy() == "auto" && err == nil && (p.OAuth == "chatgpt" || p.OAuth == "xai") {
		spec.HostedImageGen = f.ImageGenEnabled
		hosted = spec.HasHostedImageGen(p.WireProtocol())
		account := "openai"
		if p.OAuth == "xai" {
			account = "xai"
		}
		logged, loginErr := config.ImageGenLoggedIn(ctx, k.Bot.Root, account)
		hosted = hosted && loginErr == nil && logged
	}
	if !hosted {
		if logged, err := config.ImageGenLoggedIn(ctx, k.Bot.Root, f.ImageGenProvider); err != nil || !logged {
			return cap
		}
		registry, err := config.Load(k.Bot.Root)
		if err != nil {
			return cap
		}
		found := false
		for _, account := range registry.Providers {
			if account.OAuth == kind && account.WireProtocol() == config.Responses && len(account.Models) > 0 {
				p, spec, found = account, account.Models[0], true
				break
			}
		}
		if !found {
			return cap
		}
		ep, _, err := config.Resolve(k.Bot.Root, p.Name, spec.Name, func(string) string { return "" })
		if err != nil {
			return cap
		}
		ep.HostedSearch = false
		cap.proxy, err = provider.New(ep)
		if err != nil {
			return imageGenCapability{}
		}
	}
	dir := work.BotDir(k.Bot.Root, k.Bot.Name())
	imageModel := spec.ImageGenModel
	if p.OAuth == "xai" {
		imageModel = ""
	}
	cap.gen = provider.ImageGen{Model: imageModel, Save: func(data []byte) (string, error) {
		return work.WriteGenerated(dir, data)
	}}
	return cap
}

func (k *Kernel) imageGenCapability() imageGenCapability {
	if t := k.liveTurn(); t != nil && t.imageGen != nil {
		return *t.imageGen
	}
	return k.resolveImageGen(context.Background())
}

// withImageGen clears inherited hosted capability when disabled or proxying.
func (k *Kernel) withImageGen(ctx context.Context) context.Context {
	cap := k.imageGenCapability()
	if cap.proxy != nil {
		return provider.WithImageGen(ctx, provider.ImageGen{})
	}
	return provider.WithImageGen(ctx, cap.gen)
}

type generateImageTool struct{ cap imageGenCapability }

func (generateImageTool) Name() string { return "generate_image" }
func (generateImageTool) Description() string {
	return "生成图片（image generation），使用设置中已登录的图片生成账号。返回工作区 generated/ 的真实保存回执，不自动挂图；交付时将回执路径传给 send_message_to_user 的 images。"
}
func (generateImageTool) Parameters() any {
	return schema(map[string]any{"prompt": stringProp("要画的内容与要求")}, []string{"prompt"})
}
func (generateImageTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t generateImageTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Prompt) == "" || len(a.Prompt) > 32<<10 {
		return "", errors.New("prompt must be nonempty and at most 32 KiB")
	}
	msg, err := t.cap.proxy.Chat(provider.WithImageGen(ctx, t.cap.gen), provider.Request{
		Messages:        []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent(a.Prompt)}},
		ImageGeneration: true,
	}, nil)
	if err != nil {
		return "", err
	}
	var receipts []string
	failed := false
	for _, part := range msg.Content {
		if part.Hosted != nil && part.Hosted.Name == "image_generation" {
			receipts = append(receipts, part.Text)
			failed = failed || part.Hosted.Status != "completed"
		}
	}
	if len(receipts) == 0 {
		return "", errors.New("厂商生图失败: 厂商未返回图片")
	}
	receipt := strings.Join(receipts, "\n")
	if failed {
		return "", errors.New(receipt)
	}
	return receipt, nil
}
