package kernel

import (
	"context"
	"strings"

	"xbot/internal/config"
	"xbot/internal/provider"
	"xbot/internal/work"
)

// withImageGen snapshots the global preference and account at turn start.
// Only the selected OAuth Responses binding can generate. Pictures land in
// the bot workspace's generated/ directory; the conversation keeps only the
// relative path, never the bytes, and nothing is attached to a bubble.
func (k *Kernel) withImageGen(ctx context.Context) context.Context {
	// Clear an inherited capability as well: a disabled global setting wins.
	ctx = provider.WithImageGen(ctx, provider.ImageGen{})
	if k.Bot == nil {
		return ctx
	}
	cfg := k.Bot.Config()
	name, model := strings.TrimSpace(cfg.Provider), strings.TrimSpace(cfg.Model)
	if name == "" {
		return ctx
	}
	f, err := config.LoadWeb(k.Bot.Root)
	if err != nil || !f.ImageGenOn() || config.ImageGenOAuth(f.ImageGenProvider) == "" {
		return ctx
	}
	p, spec, err := config.Lookup(k.Bot.Root, name, model)
	if err != nil || p.OAuth != config.ImageGenOAuth(f.ImageGenProvider) {
		return ctx
	}
	// The global setting supplies the capability; no per-model switch is
	// required after login or when the account catalog gains a new model.
	spec.HostedImageGen = f.ImageGenEnabled
	if !spec.HasHostedImageGen(p.WireProtocol()) {
		return ctx
	}
	if loggedIn, err := config.ImageGenLoggedIn(ctx, k.Bot.Root, f.ImageGenProvider); err != nil || !loggedIn {
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
