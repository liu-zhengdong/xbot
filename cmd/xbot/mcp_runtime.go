package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/mcp"
	"xbot/internal/vault"
)

func mcpOptions(root string) mcp.Options {
	return mcp.Options{IsProviderKey: config.ProviderKey, Redact: func(text string) string { return config.RedactHeld(bot.RedactText(text)) }, ResolveSecrets: func(ctx context.Context, names []string) (map[string]string, error) {
		values := make(map[string]string, len(names))
		if len(names) == 0 {
			return values, nil
		}
		if strings.TrimSpace(os.Getenv("XBOT_VAULT_SOCKET")) != "" {
			return nil, errors.New("mcp: vaultd does not export plaintext credentials")
		}
		backend, err := vault.OpenGlobal(root)
		if err != nil {
			return nil, err
		}
		store, ok := backend.(*vault.Store)
		if !ok {
			return nil, errors.New("mcp: credential backend does not export plaintext")
		}
		vault.Register(store)
		for _, name := range names {
			if config.ProviderKey(name) {
				return nil, errors.New("mcp: provider key cannot be used as a server credential")
			}
			if secret, ok := store.Lookup(name); ok && secret.Value != "" {
				values[name] = secret.Value
			} else if value, ok := os.LookupEnv(name); ok {
				values[name] = value
			}
		}
		return values, nil
	}}
}
