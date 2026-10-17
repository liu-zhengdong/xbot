package gateway

import (
	"bytes"
	"crypto/ed25519"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWireSignatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k", "install.key")
	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateKey(path)
	if err != nil || !bytes.Equal(key, again) {
		t.Fatalf("key not stable: %v", err)
	}
	pub := key.Public().(ed25519.PublicKey)
	if id := ID(pub); len(id) != 32 {
		t.Fatalf("id %q", id)
	}
	e := NewEnvelope(key, strings.Repeat("a", 32), "msg", []byte("hi"), time.Now())
	if err := e.Verify(pub); err != nil {
		t.Fatal(err)
	}
	e.Body = []byte("hI")
	if e.Verify(pub) == nil {
		t.Fatal("tampered body verified")
	}
	now := time.Now()
	req := httptest.NewRequest("POST", "/v1/send?x=1", nil)
	SignRequest(req, key, []byte("body"), now)
	if got, err := VerifyRequest(req, []byte("body"), now, Skew); err != nil || !bytes.Equal(got, pub) {
		t.Fatalf("verify: %v", err)
	}
	if _, err := VerifyRequest(req, []byte("other"), now, Skew); err == nil {
		t.Fatal("body swap verified")
	}
	if _, err := VerifyRequest(req, []byte("body"), now.Add(10*time.Minute), Skew); err == nil || !strings.Contains(err.Error(), "server time") {
		t.Fatalf("skew: %v", err)
	}
}
