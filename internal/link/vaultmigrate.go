package link

import (
	"errors"
	"os"
	"strings"

	"xbot/internal/vault"
)

// CredDomains is the allow-list a connector token may be sent to. It is the
// same origin the connector posts to, so a token copied into another request
// is refused by the vault proxy at egress.
func CredDomains(kind string) []string {
	switch kind {
	case NameTelegram:
		return []string{"api.telegram.org"}
	case NameLark:
		return []string{"open.feishu.cn"}
	default:
		return nil
	}
}

// VaultName is the backend secret name for a connector. It must match
// vault.placeholderText's character class [A-Za-z0-9_.-]+, so it carries a dot,
// never a slash: "telegram.token", "lark.token". Empty means kind is not a
// connector.
func VaultName(kind string) string {
	switch kind {
	case NameTelegram:
		return "telegram.token"
	case NameLark:
		return "lark.token"
	default:
		return ""
	}
}

// KindForSecret maps a secrets.json key back to its connector kind. Empty
// means the name is not a connector credential.
func KindForSecret(name string) string {
	switch strings.TrimSpace(name) {
	case SecretName(NameTelegram):
		return NameTelegram
	case SecretName(NameLark):
		return NameLark
	default:
		return ""
	}
}

// SetConnectorSecret stores a connector token in backend with its bound
// domain, so egress only ever carries it to the connector's own origin.
func SetConnectorSecret(backend vault.Backend, kind, value string) error {
	if backend == nil {
		return errors.New("link: no credential backend")
	}
	name := VaultName(kind)
	if name == "" {
		return errors.New("link: not a connector")
	}
	return backend.Set(name, value, CredDomains(kind))
}

// RemoveConnectorSecret deletes a connector token from backend. A missing
// name is success.
func RemoveConnectorSecret(backend vault.Backend, kind string) error {
	if backend == nil {
		return errors.New("link: no credential backend")
	}
	name := VaultName(kind)
	if name == "" {
		return errors.New("link: not a connector")
	}
	return backend.Remove(name)
}

// Placeholder is the token value a connector passes to Poster once the vault
// holds the credential. backend.Transport replaces it just before egress.
func Placeholder(kind string) string {
	name := VaultName(kind)
	if name == "" {
		return ""
	}
	return "{{secret:" + name + "}}"
}

// MigrateConnectorSecrets moves every connector token from the legacy plaintext
// secrets.json at path into backend, then removes the plaintext. The caller
// passes the backend it already opened; this package never builds one.
//
// It is called on first access and is idempotent: a bot whose values are
// already in the backend, or that has no secrets.json, is left alone.
//
// Only after every Set succeeds is the plaintext removed. Remaining keys stay;
// when no key remains at all the file itself is deleted, so nothing on disk
// hints at a migrated credential. A failure part-way through leaves the file
// untouched so the next call retries.
func MigrateConnectorSecrets(path string, backend vault.Backend) error {
	if backend == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	vals, err := ReadSecrets(path)
	if err != nil {
		return err
	}
	var moved []string
	for _, kind := range []string{NameTelegram, NameLark} {
		legacy := strings.TrimSpace(vals[SecretName(kind)])
		if legacy == "" {
			continue
		}
		if err := backend.Set(VaultName(kind), legacy, CredDomains(kind)); err != nil {
			return err
		}
		moved = append(moved, kind)
	}
	if len(moved) == 0 {
		return nil
	}
	names := make([]string, 0, len(moved))
	for _, kind := range moved {
		names = append(names, SecretName(kind))
	}
	if err := DropSecrets(path, names); err != nil {
		return err
	}
	return pruneEmptySecretFile(path)
}

// pruneEmptySecretFile removes the plaintext file once it holds no values, so a
// migrated bot no longer has a secrets.json at all.
func pruneEmptySecretFile(path string) error {
	vals, err := ReadSecrets(path)
	if err != nil {
		return err
	}
	if len(vals) != 0 {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
