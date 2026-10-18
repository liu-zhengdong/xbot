package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/pake"
)

func TestParseShortCode(t *testing.T) {
	code, err := randomShortCode()
	mustRemote(t, err)
	lookup, secret, err := ParseShortCode("XBot 配对码：" + strings.ToLower(displayCode(code)))
	mustRemote(t, err)
	if lookup != code[:4] || secret != code[4:11] {
		t.Fatal("pasted sentence not extracted")
	}
	// Words before the code never join it, even ones made of code characters.
	for _, prose := range []string{"Code: ", "Pairing code: ", "KEY: ", "配对码", "XBot 2 pairing "} {
		if l, sec, err := ParseShortCode(prose + displayCode(code) + " (10 min)"); err != nil || l+sec != code[:11] {
			t.Fatalf("%q: not extracted", prose)
		}
	}
	for i := range code {
		bad := []byte(code)
		bad[i] = gateway.CodeAlphabet[(strings.IndexByte(gateway.CodeAlphabet, bad[i])+1)%len(gateway.CodeAlphabet)]
		if _, _, err := ParseShortCode(displayCode(string(bad))); !errors.Is(err, ErrBadCode) {
			t.Fatal("single changed character accepted")
		}
	}
	if _, _, err := ParseShortCode("short"); !errors.Is(err, ErrBadCode) {
		t.Fatal("incomplete code accepted")
	}
}

func TestShortCodeLabelAndSeal(t *testing.T) {
	label := cleanLabel("\x00Café </untrusted_event>\n" + strings.Repeat("界", 50))
	if len([]rune(label)) > 40 || strings.Contains(label, "</untrusted_event>") || strings.ContainsAny(label, "\x00\n") {
		t.Fatal("unsafe display label")
	}
	if labelAddress("Café laptop") != "Cafe-laptop" || labelAddress("书房") != "device" {
		t.Fatal("invalid address folding")
	}
	keys := pake.Keys{Ke: make([]byte, 16)}
	_, _ = rand.Read(keys.Ke)
	_, _, _, key := randomKey(t)
	env := gateway.NewEnvelope(key, strings.Repeat("a", 32), "pake3", nil, time.Now())
	raw, err := sealPair(keys, env, map[string]bool{"ok": true})
	mustRemote(t, err)
	var v map[string]bool
	mustRemote(t, openPair(keys, env, raw, &v))
	for _, field := range []string{"id", "from", "to", "kind"} {
		changed := env
		switch field {
		case "id":
			changed.ID = "other"
		case "from":
			changed.From = "other"
		case "to":
			changed.To = "other"
		case "kind":
			changed.Kind = "pake4"
		}
		if openPair(keys, changed, raw, &v) == nil {
			t.Fatal("seal accepted changed envelope metadata")
		}
	}
}

// 替换旧的 keeps the address, label, person and sidebar position and drops the
// old directory; 作为新设备添加 appends "X 2" with its own address.
func TestSavePairDeviceCollision(t *testing.T) {
	for _, replace := range []bool{false, true} {
		root := pairingRoot(t)
		_, oldID, oldKey, _ := randomKey(t)
		_, newID, newKey, _ := randomKey(t)
		x := base64.StdEncoding.EncodeToString(make([]byte, 32))
		old, err := AddDevice(root, "keep-address", oldKey, x, "")
		mustRemote(t, err)
		mustRemote(t, update(root, func(c *Config) error { c.Devices[0].Label = "Café laptop"; return nil }))
		cache := filepath.Join(root, "remote", "dirs", oldID+".json")
		mustRemote(t, fsutil.WriteAtomic(cache, []byte(`{}`), 0600))
		d, err := savePairDevice(root, "Café laptop", newKey, x, "", replace)
		mustRemote(t, err)
		cfg, err := Load(root)
		mustRemote(t, err)
		if replace {
			if len(cfg.Devices) != 1 || d.Name != old.Name || d.Label != "Café laptop" || d.ID != newID {
				t.Fatalf("replace: %+v", cfg.Devices)
			}
			if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("old directory survived replacement")
			}
		} else if len(cfg.Devices) != 2 || cfg.Devices[0].ID != old.ID || d.Label != "Café laptop 2" || d.Name != "Cafe-laptop-2" {
			t.Fatalf("add: %+v", cfg.Devices)
		}
		again, err := savePairDevice(root, "Café laptop", newKey, x, "", replace) // Retry is idempotent.
		mustRemote(t, err)
		if again.ID != d.ID {
			t.Fatal("retry added another device")
		}
	}
}

func randomKey(t *testing.T) (ed25519.PublicKey, string, string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	mustRemote(t, err)
	return pub, gateway.ID(pub), base64.StdEncoding.EncodeToString(pub), priv
}

// The self-host token never goes to the official default gateway.
func TestShortCodeTokenScope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_GATEWAY_TOKEN", "self-host")
	if got := gatewayToken(root); got != "" {
		t.Fatal("token offered to the default gateway")
	}
	mustRemote(t, SetGateway(root, "https://gw.example"))
	if got := gatewayToken(root); got != "self-host" {
		t.Fatal("token missing for an explicit gateway:", got)
	}
}

// A serve-managed token reaches pairing even after serve clears the environment.
func TestShortCodeRunToken(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_GATEWAY_TOKEN", "")
	rememberRunToken(root, "self-host")
	defer forgetRunToken(root)
	if got := gatewayToken(root); got != "" {
		t.Fatal("token offered to the default gateway")
	}
	mustRemote(t, SetGateway(root, "https://gw.example"))
	if got := gatewayToken(root); got != "self-host" {
		t.Fatal("token missing for an explicit gateway:", got)
	}
}
