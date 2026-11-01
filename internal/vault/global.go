package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ProductNamespace is reserved for the machine-wide product vault in vaultd.
// Provider OAuth keeps its independent namespace and is never merged here.
const ProductNamespace = "_product_vault"

func GlobalDir(root string) string { return filepath.Join(root, "vault", ProductNamespace) }
func GlobalKeyPath(root string) string {
	return filepath.Join(root, "keys", "vault", ProductNamespace+".key")
}

// OpenGlobal is the sole product-vault assembly seam. Bot identity is not an input.
func OpenGlobal(root string) (Backend, error) {
	if socket := strings.TrimSpace(os.Getenv("XBOT_VAULT_SOCKET")); socket != "" {
		client, err := OpenClient(socket, ProductNamespace)
		if err != nil {
			return nil, err
		}
		// Initialize/merge at the seam, propagating conflicts. Backend.List's
		// historical no-error signature must not turn a failed migration into an
		// apparently empty product vault.
		if _, err := client.control(context.Background(), http.MethodGet, "/v1/list", nil); err != nil {
			return nil, err
		}
		return client, nil
	}
	return OpenLocalProduct(root)
}

// OpenLocalProduct is the local half of OpenGlobal, also used to move that
// same store to vaultd. Normal product consumers must use OpenGlobal.
func OpenLocalProduct(root string) (*Store, error) {
	return mergeProductVault(GlobalDir(root), GlobalKeyPath(root), filepath.Join(root, "bots"), func(name string) (string, string, string) {
		return filepath.Join(root, "bots", name, "vault"), filepath.Join(root, "keys", "vault", name+".key"), filepath.Join(root, "bots", name, "secrets.json")
	})
}

// mergeProductVault commits the complete merge once, under the destination's
// existing write lock, before removing any source ciphertext/key. A conflicting
// nonempty value OR binding fails without writing the destination or deleting
// sources. Identical entries are idempotent (including after an interrupted cleanup).
func mergeProductVault(dir, key, sources string, paths func(string) (string, string, string)) (*Store, error) {
	dst, err := Open(dir, key)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(sources)
	if errors.Is(err, os.ErrNotExist) {
		return dst, nil
	}
	if err != nil {
		return nil, err
	}
	type source struct {
		dir, key, plain string
		encrypted       bool
	}
	var old []source
	for _, e := range entries {
		// Bot names start with an alphanumeric character. Reserved namespaces
		// (OAuth and this destination) are not Bot buckets.
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		d, k, plain := paths(e.Name())
		encrypted := false
		if _, err := os.Stat(filepath.Join(d, secretsFile)); err == nil {
			encrypted = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		// Legacy MCP/connector plaintext is another old product credential source,
		// not a live fallback. daemon buckets do not have this Bot file layout.
		if plain != "" {
			if _, err := os.Stat(plain); errors.Is(err, os.ErrNotExist) {
				plain = ""
			} else if err != nil {
				return nil, err
			}
		}
		if encrypted || plain != "" {
			old = append(old, source{dir: d, key: k, plain: plain, encrypted: encrypted})
		}
	}
	if len(old) == 0 {
		return dst, nil
	}
	err = dst.withFileLock(func() error {
		dst.mu.Lock()
		defer dst.mu.Unlock()
		if err := dst.reloadLocked(); err != nil {
			return err
		}
		for _, src := range old {
			incoming := map[string]Secret{}
			if src.encrypted {
				st, err := Open(src.dir, src.key)
				if err != nil {
					return err
				}
				incoming = st.secrets
			}
			if src.plain != "" {
				raw, err := os.ReadFile(src.plain)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				var file struct {
					Values map[string]string `json:"values"`
				}
				if len(bytes.TrimSpace(raw)) > 0 {
					dec := json.NewDecoder(bytes.NewReader(raw))
					dec.DisallowUnknownFields()
					if err := dec.Decode(&file); err != nil {
						return errors.New("vault: invalid legacy secrets file")
					}
				}
				for name, value := range file.Values {
					if value == "" {
						continue
					}
					sec := Secret{Name: name, Value: value}
					// These are the pre-vault connector names, translated exactly as the
					// existing connector migration does; MCP environment names stay intact.
					switch name {
					case "TELEGRAM_BOT_TOKEN":
						sec.Name = "telegram.token"
						sec.Domains = []string{"api.telegram.org"}
					case "LARK_APP_SECRET":
						sec.Name = "lark.token"
						sec.Domains = []string{"open.feishu.cn"}
					}
					if prev, ok := incoming[sec.Name]; ok && prev.Value != "" && (prev.Value != sec.Value || !slices.Equal(prev.Domains, sec.Domains)) {
						return fmt.Errorf("vault: migration conflict for %q; source vaults retained", sec.Name)
					}
					incoming[sec.Name] = sec
				}
			}
			for name, sec := range incoming {
				prev, ok := dst.secrets[name]
				if ok && prev.Value != "" && sec.Value != "" && (prev.Value != sec.Value || !slices.Equal(prev.Domains, sec.Domains)) {
					return fmt.Errorf("vault: migration conflict for %q; source vaults retained; resolve the legacy entries before retrying", name)
				}
				if !ok || prev.Value == "" {
					dst.secrets[name] = sec
				}
			}
		}
		if err := dst.ensureKeyLocked(); err != nil {
			return err
		}
		dst.invalidateForms()
		if err := dst.saveLocked(); err != nil {
			return err
		}
		for _, src := range old {
			if src.encrypted {
				if err := os.Remove(filepath.Join(src.dir, secretsFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if err := os.Remove(src.key); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if src.plain != "" {
				if err := os.Remove(src.plain); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			// Only vault-owned files, never a Bot directory or unrelated namespace.
			_ = os.Remove(filepath.Join(src.dir, legacyKeyFile))
			_ = os.Remove(filepath.Join(src.dir, secretsFile+".lock"))
			_ = os.Remove(src.dir)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dst, nil
}
