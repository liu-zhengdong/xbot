package tunnel

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePublicURL(t *testing.T) {
	log := `
INF Requesting new quick Tunnel on trycloudflare.com...
https://api.cloudflare.com/client/v4
|  https://demo-box.trycloudflare.com                                    |
`
	got, ok := ParsePublicURL(log)
	if !ok || got != "https://demo-box.trycloudflare.com" {
		t.Fatalf("got %q ok %v", got, ok)
	}
	if _, ok := ParsePublicURL("https://www.cloudflare.com/docs"); ok {
		t.Fatal("docs url accepted")
	}
}

func TestTailscaleListenRewritesLoopback(t *testing.T) {
	bind, public := TailscaleListen("127.0.0.1:8787", "100.1.2.3")
	if bind != "100.1.2.3:8787" || public != "http://100.1.2.3:8787" {
		t.Fatalf("bind %s public %s", bind, public)
	}
	bind, public = TailscaleListen("[::1]:8787", "100.1.2.3")
	if bind != "100.1.2.3:8787" || public != "http://100.1.2.3:8787" {
		t.Fatalf("v6 bind %s public %s", bind, public)
	}
	bind, public = TailscaleListen("0.0.0.0:8787", "100.9.8.7")
	if bind != "0.0.0.0:8787" || public != "http://100.9.8.7:8787" {
		t.Fatalf("wildcard bind %s public %s", bind, public)
	}
}

func TestResolveTailscale(t *testing.T) {
	bin := writeFake(t, "#!/bin/sh\necho 100.4.5.6\n")
	got, err := Resolve(context.Background(), "tailscale", "localhost:8787", bin)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenAddr != "100.4.5.6:8787" || got.PublicURL != "http://100.4.5.6:8787" {
		t.Fatalf("%+v", got)
	}
	if _, err := Resolve(context.Background(), "relay", "127.0.0.1:8787", bin); err == nil {
		t.Fatal("unknown mode accepted")
	}
	plain, err := Resolve(context.Background(), "", "127.0.0.1:9", bin)
	if err != nil || plain.PublicURL != "" || plain.ListenAddr != "127.0.0.1:9" {
		t.Fatalf("%+v %v", plain, err)
	}
}

func TestStartCloudflare(t *testing.T) {
	bin := writeFake(t, "#!/bin/sh\necho https://box.trycloudflare.com\nexec sleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proc, err := StartCloudflare(ctx, "http://127.0.0.1:8787", bin, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	if proc.PublicURL != "https://box.trycloudflare.com" {
		t.Fatal(proc.PublicURL)
	}
}

func TestLocalURL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := LocalURL(ln.Addr())
	if !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatal(got)
	}
}

func writeFake(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
