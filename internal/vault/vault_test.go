package vault

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSetListRemove(t *testing.T) {
	s := testStore(t)
	if err := s.Set("GITHUB_TOKEN", "ghp_value", []string{"a.com", "b.com"}); err != nil {
		t.Fatal(err)
	}
	sec, ok := s.Lookup("GITHUB_TOKEN")
	if !ok || sec.Value != "ghp_value" {
		t.Fatalf("lookup %+v ok=%v", sec, ok)
	}
	if len(sec.Domains) != 2 || sec.Domains[0] != "a.com" || sec.Domains[1] != "b.com" {
		t.Fatalf("domains %v", sec.Domains)
	}
	list := s.List()
	if len(list) != 1 || list[0].Name != "GITHUB_TOKEN" {
		t.Fatalf("list %+v", list)
	}
	if err := s.Remove("GITHUB_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("still there: %+v", got)
	}
	if _, ok := s.Lookup("GITHUB_TOKEN"); ok {
		t.Fatal("lookup after remove")
	}
}

func TestSetValidates(t *testing.T) {
	s := testStore(t)
	if err := s.Set("", "v", []string{"a.com"}); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := s.Set("N", "", []string{"a.com"}); err == nil {
		t.Fatal("empty value accepted")
	}
	if err := s.Set("N", "v", nil); err == nil {
		t.Fatal("empty domains accepted")
	}
	if err := s.Set("N", "v", []string{"a.com", " A.COM ", "a.com"}); err != nil {
		t.Fatal(err)
	}
	sec, _ := s.Lookup("N")
	if len(sec.Domains) != 1 || sec.Domains[0] != "a.com" {
		t.Fatalf("domains not normalized: %v", sec.Domains)
	}
}

func TestOpenDoesNotCreateState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("vault created on open: %v", err)
	}
}

func TestEncryptedAtRestAndPerms(t *testing.T) {
	const value = "SUPER-SECRET-VALUE-123"
	dir := filepath.Join(t.TempDir(), "vault")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("dir mode %v", dirInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %v", entry.Name(), info.Mode().Perm())
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "secrets.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(value)) {
		t.Fatal("plaintext value on disk")
	}
	if bytes.Contains(raw, []byte("TOKEN")) {
		t.Fatal("secret name on disk")
	}
	// Reopen and read the value back through the key.
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := again.Lookup("TOKEN")
	if !ok || sec.Value != value {
		t.Fatalf("reopen %+v ok=%v", sec, ok)
	}
}

func TestRedactAndRegistry(t *testing.T) {
	s := testStore(t)
	if err := s.Set("TOKEN", "tok-value", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	got := s.Redact("Bearer tok-value end")
	if got != "Bearer [secret] end" {
		t.Fatalf("redact %q", got)
	}
	unregister := Register(s)
	if got := Redact("x tok-value y"); got != "x [secret] y" {
		t.Fatalf("global redact %q", got)
	}
	unregister()
	if got := Redact("x tok-value y"); got != "x tok-value y" {
		t.Fatalf("global redact after unregister %q", got)
	}
}

func TestRedactCoversEncodedForms(t *testing.T) {
	const value = `p@ss "w0rd"/&=`
	s := testStore(t)
	if err := s.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	literal := value
	escaped := "p%40ss+%22w0rd%22%2F%26%3D" // url.QueryEscape
	b64 := base64.StdEncoding.EncodeToString([]byte(value))
	jsonForm := `p@ss \"w0rd\"/&=` // json.Marshal of the value, sans quotes
	for _, form := range []string{escaped, b64, jsonForm} {
		if form == literal {
			t.Fatalf("test form %q is the literal value", form)
		}
	}
	text := "a " + literal + " b " + escaped + " c " + b64 + " d " + jsonForm + " e"
	got := s.Redact(text)
	if strings.Contains(got, literal) || strings.Contains(got, escaped) ||
		strings.Contains(got, b64) || strings.Contains(got, jsonForm) {
		t.Fatalf("encoded form leaked: %q", got)
	}
	if n := strings.Count(got, "[secret]"); n != 4 {
		t.Fatalf("want 4 masks, got %d in %q", n, got)
	}
}

func TestMaskTruncatedHidesCutValue(t *testing.T) {
	const value = "0123456789abcdefghij"
	s := testStore(t)
	if err := s.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	cut := "keep " + value[:12] // a proper prefix left at a clip boundary
	got := s.MaskTruncated(cut)
	if strings.Contains(got, value[:12]) {
		t.Fatalf("cut prefix visible: %q", got)
	}
	if !strings.HasSuffix(got, "[secret]") {
		t.Fatalf("tail not masked: %q", got)
	}
	plain := s.MaskTruncated("ends with ordinary words")
	if plain != "ends with ordinary words" {
		t.Fatalf("unrelated text changed: %q", plain)
	}
}

func TestOpenRefusesLooseKeyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key"), make([]byte, keyLen), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "key"), 0644); err != nil { // defeat a strict umask
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "too open") {
		t.Fatalf("loose key accepted: %v", err)
	}
}

func TestSetKeepsConcurrentWriterSecret(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// second opens before first writes, so its snapshot is stale.
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set("A", "value-a", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Set("B", "value-b", []string{"b.com"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B"} {
		if _, ok := reopened.Lookup(name); !ok {
			t.Fatalf("secret %q lost", name)
		}
	}
}
