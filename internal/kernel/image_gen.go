package kernel

import (
	"context"
	"strings"

	"xbot/internal/config"
	"xbot/internal/provider"
	"xbot/internal/work"
)

// withImageGen mounts vendor-hosted image generation when the bound model has
// it switched on. The registry is read on each model request, like the search
// preference, so a settings change reaches the next turn. Pictures land in
// the bot workspace's generated/ directory; the conversation keeps only the
// relative path, never the bytes, and nothing is attached to a bubble.
func (k *Kernel) withImageGen(ctx context.Context) context.Context {
	if k.Bot == nil {
		return ctx
	}
	cfg := k.Bot.Config()
	name, model := strings.TrimSpace(cfg.Provider), strings.TrimSpace(cfg.Model)
	if name == "" {
		return ctx
	}
	p, spec, err := config.Lookup(k.Bot.Root, name, model)
	if err != nil || !spec.HasHostedImageGen(p.WireProtocol()) {
		return ctx
	}
	dir := work.BotDir(k.Bot.Root, k.Bot.Name())
	imageModel := spec.ImageGenModel
	if p.OAuth == "xai" {
		imageModel = "" // xAI chooses Imagine, including on scripted loopback endpoints.
	}
	return provider.WithImageGen(ctx, provider.ImageGen{
		Model: imageModel,
		Save:  func(data []byte) (string, error) { return work.WriteGenerated(dir, data) },
	})
}
