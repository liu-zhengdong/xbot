package config

import (
	"context"
	"errors"

	"xbot/internal/fsutil"
	"xbot/internal/vault"
)

func (f WebFile) ImageGenOn() bool { return f.ImageGenEnabled != nil && *f.ImageGenEnabled }

func (f WebFile) ImageGenPolicy() string {
	if f.ImageGenMode == "" {
		return "auto"
	}
	return f.ImageGenMode
}

// ImageGenOAuth maps the settings brand to the existing account identity.
// Provider names and API keys are deliberately not login evidence.
func ImageGenOAuth(id string) string {
	switch id {
	case "openai":
		return "chatgpt"
	case "xai":
		return "xai"
	default:
		return ""
	}
}

func ImageGenLoggedIn(ctx context.Context, root, id string) (bool, error) {
	kind := ImageGenOAuth(id)
	if kind == "" {
		return false, errors.New("请选择图片生成供应商")
	}
	b, err := OAuthVault(root)
	if err != nil {
		return false, err
	}
	info, err := vault.OAuthMetadata(ctx, b, kind)
	return info.Configured, err
}

// SetWebImageGen shares the search read/modify/write lock and preserves all
// other web settings. Repeating a PUT does not rewrite an unchanged file.
func SetWebImageGen(root string, enabled bool, id, mode string) error {
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "local" {
		return errors.New("请选择画图方式")
	}
	if ImageGenOAuth(id) == "" {
		return errors.New("请从列表里选择一家")
	}
	return fsutil.WithLock(WebPath(root)+".lock", func() error {
		f, err := LoadWeb(root)
		if err != nil {
			return err
		}
		if f.ImageGenEnabled != nil && f.ImageGenOn() == enabled && f.ImageGenProvider == id && f.ImageGenPolicy() == mode {
			return nil
		}
		f.ImageGenEnabled, f.ImageGenProvider, f.ImageGenMode = &enabled, id, mode
		return SaveWeb(root, f)
	})
}
