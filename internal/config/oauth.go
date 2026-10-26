package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"xbot/internal/fsutil"
	"xbot/internal/vault"
)

var oauthVaults struct {
	sync.Mutex
	backends map[string]vault.Backend
}

// Leading '_' is accepted by the vault daemon but cannot be a Bot name.
const oauthNamespace = "_provider_accounts"

// OAuthVault is a namespace in the existing vault backend, shared by provider
// bindings just like providers.json. No tokens are stored in the registry.
func OAuthVault(root string) (vault.Backend, error) {
	key := root + "\x00" + os.Getenv("XBOT_VAULT_SOCKET")
	oauthVaults.Lock()
	defer oauthVaults.Unlock()
	if b := oauthVaults.backends[key]; b != nil {
		return b, nil
	}
	b, err := vault.OpenBackend(oauthNamespace, filepath.Join(root, "vault", oauthNamespace), filepath.Join(root, "keys", "vault", oauthNamespace+".key"))
	if err != nil {
		return nil, err
	}
	if local, ok := b.(*vault.Store); ok {
		vault.Register(local)
	}
	if oauthVaults.backends == nil {
		oauthVaults.backends = map[string]vault.Backend{}
	}
	oauthVaults.backends[key] = b
	return b, nil
}

func validateOAuth(p Provider) error {
	protocol, base := "", ""
	switch p.OAuth {
	case "openrouter":
		protocol, base = ChatCompletions, "https://openrouter.ai/api/v1"
	case "chatgpt":
		protocol, base = Responses, "https://api.openai.com/v1"
	case "xai":
		protocol, base = Responses, "https://api.x.ai/v1"
	default:
		return fmt.Errorf("providers: unsupported OAuth provider %q", p.OAuth)
	}
	if p.APIKeyEnv != "" || p.WireProtocol() != protocol {
		return fmt.Errorf("providers: OAuth %s requires %s and no api_key_env", p.OAuth, protocol)
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return fmt.Errorf("providers: invalid OAuth base URL")
	}
	// Loopback is also used by the existing fake/scripted smoke suite.
	loopback := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if strings.TrimRight(p.BaseURL, "/") != base && !loopback {
		return fmt.Errorf("providers: OAuth %s must use %s", p.OAuth, base)
	}
	return nil
}

// SaveOAuthProvider updates one complete binding entry under the registry lock.
func SaveOAuthProvider(root string, p Provider) error {
	if err := validateProvider(p, true); err != nil {
		return err
	}
	for _, m := range p.Models {
		if err := validateModel(m); err != nil {
			return err
		}
	}
	return fsutil.WithLock(Path(root)+".lock", func() error {
		f, err := Load(root)
		if err != nil {
			return err
		}
		i := indexOf(f.Providers, p.Name)
		if i < 0 {
			f.Providers = append(f.Providers, p)
		} else {
			if f.Providers[i].OAuth != p.OAuth {
				return fmt.Errorf("providers: %s already uses an API key; choose a different provider name", p.Name)
			}
			previous := f.Providers[i]
			if p.OAuth == "openrouter" || p.OAuth == "xai" {
				// OpenRouter supplies one initial model and the xAI seed list is not
				// an account catalog either: keep models already chosen, add new ones.
				models := append([]ModelSpec(nil), previous.Models...)
				for _, m := range p.Models {
					found := false
					for _, old := range models {
						if old.Name == m.Name {
							found = true
							break
						}
					}
					if !found {
						models = append(models, m)
					}
				}
				p.Models = models
			} else {
				for n, m := range p.Models {
					for _, old := range previous.Models {
						if m.Name == old.Name {
							p.Models[n] = old
							break
						}
					}
				}
			}
			f.Providers[i] = p
		}
		return Save(root, f)
	})
}
