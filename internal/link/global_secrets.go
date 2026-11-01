package link

import (
	"errors"
	"xbot/internal/vault"
)

// SetProductSecrets stores both connector tokens and MCP environment keys in
// the product backend; only connectors have an outbound domain binding.
func SetProductSecrets(backend vault.Backend, values map[string]string) error {
	if backend == nil {
		return errors.New("link: no credential backend")
	}
	for name, value := range values {
		if kind := KindForSecret(name); kind != "" {
			if err := SetConnectorSecret(backend, kind, value); err != nil {
				return err
			}
		} else if err := backend.Set(name, value, nil); err != nil {
			return err
		}
	}
	return nil
}

func RemoveProductSecrets(backend vault.Backend, names []string) error {
	if backend == nil {
		return errors.New("link: no credential backend")
	}
	for _, name := range names {
		if kind := KindForSecret(name); kind != "" {
			name = VaultName(kind)
		}
		if err := backend.Remove(name); err != nil {
			return err
		}
	}
	return nil
}

// GlobalSecretLookup supplies local MCP child processes from the same product
// backend as settings and HTTP injection. It never bypasses a configured daemon:
// vaultd deliberately has no plaintext export for local process environments.
func GlobalSecretLookup(root string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		backend, err := vault.OpenGlobal(root)
		if err != nil {
			return "", false
		}
		store, ok := backend.(*vault.Store)
		if !ok {
			return "", false
		}
		sec, ok := store.Lookup(name)
		return sec.Value, ok
	}
}
