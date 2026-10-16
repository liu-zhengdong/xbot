package vault

import (
	"net/http"
	"os"
	"strings"
)

// Backend is a credential store. It is either the local same-user Store or a
// client to the separate-user vault daemon (see daemon.go and client.go).
//
// List never returns a value: callers only ever see names and bound domains.
type Backend interface {
	// List returns every secret sorted by name. Value is always empty.
	List() []Secret
	// Names returns the stored secret names sorted.
	Names() []string
	// Set stores value under name with the given allowed domains.
	Set(name, value string, domains []string) error
	// Remove deletes one secret. A missing name is success.
	Remove(name string) error
	// Transport wraps base so outbound requests are injected at egress.
	Transport(base http.RoundTripper) http.RoundTripper
	// Redact replaces known values with [secret].
	Redact(string) string
	// MaskTruncated hides a value prefix cut off by a size clip.
	MaskTruncated(string) string
}

// OpenBackend opens the credential backend for one bot. When XBOT_VAULT_SOCKET
// is set the daemon client is used and vaultDir/keyPath are ignored; otherwise
// the local same-user Store is the fallback.
func OpenBackend(botName, vaultDir, keyPath string) (Backend, error) {
	if socket := strings.TrimSpace(os.Getenv("XBOT_VAULT_SOCKET")); socket != "" {
		return OpenClient(socket, botName)
	}
	return Open(vaultDir, keyPath)
}

// Transport implements Backend for the local Store: it returns a Proxy that
// injects this vault's secrets into outbound requests just before egress.
func (s *Store) Transport(base http.RoundTripper) http.RoundTripper {
	if s == nil {
		if base != nil {
			return base
		}
		return http.DefaultTransport
	}
	return &Proxy{Base: base, Store: s}
}

// compile-time checks.
var (
	_ Backend = (*Store)(nil)
	_ Backend = (*Client)(nil)
)
