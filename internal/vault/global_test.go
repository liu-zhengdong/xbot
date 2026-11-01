package vault

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalMergeAndConsumers(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	root := t.TempDir()
	for i, name := range []string{"one", "unrelated-bot-72"} {
		st, err := Open(filepath.Join(root, "bots", name, "vault"), filepath.Join(root, "keys", "vault", name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		key := "REMOTE"
		value := "fixture-shared-token"
		domains := []string{"example.com"}
		if i == 1 {
			key = "LOCAL_SUDO"
			value = "fixture-shared-password"
			domains = nil
		}
		if err := st.Set(key, value, domains); err != nil {
			t.Fatal(err)
		}
	}
	oauth, err := Open(filepath.Join(root, "vault", "_provider_accounts"), filepath.Join(root, "keys", "vault", "_provider_accounts.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := oauth.Set("OAUTH", "fixture-oauth", nil); err != nil {
		t.Fatal(err)
	}
	b, err := OpenGlobal(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Names()) != 2 {
		t.Fatal("legacy entries not merged, or OAuth leaked")
	}
	// A second Open, not a manually shared pointer, reads the same persisted source.
	second, err := OpenGlobal(root)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "https://example.com", nil)
	req.Header.Set("Authorization", "Bearer {{secret:REMOTE}}")
	var header string
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		header = r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: http.NoBody}, nil
	})
	if _, err := second.Transport(base).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if header != "Bearer fixture-shared-token" {
		t.Fatal("global outbound injection missing")
	}
	var out bytes.Buffer
	if err := WriteLocalSudo(GlobalDir(root), GlobalKeyPath(root), &out); err != nil || out.String() != "fixture-shared-password\n" {
		t.Fatal("global askpass source missing")
	}
	for _, name := range []string{"one", "unrelated-bot-72"} {
		if _, err := os.Stat(filepath.Join(root, "bots", name, "vault", "secrets.enc")); !os.IsNotExist(err) {
			t.Fatal("legacy live bucket remains")
		}
	}
	if err := b.Remove("REMOTE"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenGlobal(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Names()) != 1 {
		t.Fatal("migration resurrected deleted credential")
	}
	if _, ok := oauth.Lookup("OAUTH"); !ok {
		t.Fatal("OAuth namespace touched")
	}
}

func TestGlobalMigrationConflictRetainsSources(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	for _, differentDomain := range []bool{false, true} {
		t.Run(map[bool]string{false: "value", true: "binding"}[differentDomain], func(t *testing.T) {
			root := t.TempDir()
			for i, name := range []string{"alpha", "beta"} {
				st, err := Open(filepath.Join(root, "bots", name, "vault"), filepath.Join(root, "keys", "vault", name+".key"))
				if err != nil {
					t.Fatal(err)
				}
				value := "fixture-first"
				domains := []string{"example.com"}
				if i == 1 {
					if differentDomain {
						domains = []string{"other.example"}
					} else {
						value = "fixture-second"
					}
				}
				if err := st.Set("COLLISION", value, domains); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := OpenGlobal(root); err == nil || !strings.Contains(err.Error(), "conflict") || strings.Contains(err.Error(), "fixture-") {
				t.Fatal("expected safe explicit conflict")
			}
			if _, err := os.Stat(filepath.Join(GlobalDir(root), "secrets.enc")); !os.IsNotExist(err) {
				t.Fatal("partial destination was committed")
			}
			for _, name := range []string{"alpha", "beta"} {
				if _, err := os.Stat(filepath.Join(root, "bots", name, "vault", "secrets.enc")); err != nil {
					t.Fatal("source lost on conflict")
				}
			}
		})
	}
}

func TestGlobalDaemonMergesProductOnly(t *testing.T) {
	h := newDaemon(t)
	for _, name := range []string{"red", "green"} {
		st, err := Open(filepath.Join(h.dataDir, name), filepath.Join(h.dataDir, "keys", name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Set("SHARED", "fixture-daemon", []string{"example.com"}); err != nil {
			t.Fatal(err)
		}
	}
	oauth := h.openClient(t, "_provider_accounts")
	if err := oauth.Set("OAUTH", "fixture-oauth", nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_VAULT_SOCKET", h.socket)
	backend, err := OpenGlobal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list := backend.List()
	if len(list) != 1 || list[0].Name != "SHARED" || list[0].Value != "" {
		t.Fatal("daemon product merge missing or leaked")
	}
	if len(oauth.List()) != 1 {
		t.Fatal("daemon OAuth namespace touched")
	}
	for _, name := range []string{"red", "green"} {
		if _, err := os.Stat(filepath.Join(h.dataDir, name, secretsFile)); !os.IsNotExist(err) {
			t.Fatal("daemon legacy bucket remains")
		}
	}
}

func TestGlobalDaemonConflictFailsOpen(t *testing.T) {
	h := newDaemon(t)
	for name, value := range map[string]string{"first": "fixture-first", "second": "fixture-second"} {
		st, err := Open(filepath.Join(h.dataDir, name), filepath.Join(h.dataDir, "keys", name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Set("DUPLICATE", value, nil); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XBOT_VAULT_SOCKET", h.socket)
	if _, err := OpenGlobal(t.TempDir()); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatal("daemon conflict hidden as empty vault")
	}
	for _, name := range []string{"first", "second"} {
		if _, err := os.Stat(filepath.Join(h.dataDir, name, secretsFile)); err != nil {
			t.Fatal("daemon conflict lost source")
		}
	}
}

func TestGlobalMigratesLegacyMCPFile(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	root := t.TempDir()
	// Simulate an interrupted cleanup: the destination already holds this
	// identical local entry; nil and empty domain slices are the same binding.
	dst, err := Open(GlobalDir(root), GlobalKeyPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.Set("CONTEXT7_API_KEY", "fixture-mcp", nil); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "bots", "any-bot", "secrets.json")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"values":{"CONTEXT7_API_KEY":"fixture-mcp","TELEGRAM_BOT_TOKEN":"fixture-tg"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := OpenGlobal(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Names()) != 2 {
		t.Fatal("plaintext product entries missing")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("live plaintext source remains")
	}
}
