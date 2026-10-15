package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRegistryHidesTokenOnDiskMode(t *testing.T) {
	root := t.TempDir()
	if err := Add(root, Node{Name: "desk", URL: "http://127.0.0.1:9", Token: "sekret"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(Path(root))
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	got, err := Lookup(root, "desk")
	if err != nil || got.Token != "sekret" || got.URL != "http://127.0.0.1:9" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := Add(root, Node{Name: "desk", URL: "http://127.0.0.1:9"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := Add(root, Node{Name: "bad name", URL: "http://127.0.0.1:9"}); err == nil {
		t.Fatal("bad name accepted")
	}
	if err := Add(root, Node{Name: "x", URL: "ftp://127.0.0.1"}); err == nil {
		t.Fatal("ftp accepted")
	}
	if err := Remove(root, "desk"); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(root, "desk"); err == nil {
		t.Fatal("still present")
	}
}

func TestCallInfo(t *testing.T) {
	info := Info{ID: "abc", Name: "desk", Caps: Caps()}
	srv := httptest.NewServer(Handler("tok", info, nil))
	defer srv.Close()
	ctx := context.Background()
	if _, err := Ping(ctx, srv.URL, "nope"); err == nil {
		t.Fatal("bad token accepted")
	}
	got, err := Ping(ctx, srv.URL, "tok")
	if err != nil || got.ID != "abc" || got.Name != "desk" || len(got.Caps) != 1 || got.Caps[0] != "info" {
		t.Fatalf("%+v %v", got, err)
	}
	text, err := Call(ctx, srv.URL, "tok", "info")
	if err != nil || !strings.Contains(text, "id\tabc") || !strings.Contains(text, "os\t") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := Call(ctx, srv.URL, "tok", "shell"); err == nil || !strings.Contains(err.Error(), "unknown cap") {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/call", strings.NewReader(`{"cap":"info","extra":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestLinkCall(t *testing.T) {
	root := t.TempDir()
	info := Info{ID: "abc", Name: "desk", Caps: []string{"info"}}
	srv := httptest.NewServer(Handler("tok", info, nil))
	defer srv.Close()
	if err := Add(root, Node{Name: "desk", URL: srv.URL, Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	l := Open(root)
	out, err := (listTool{l}).Run(context.Background(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "desk\tonline\tdesk") || strings.Contains(out, "tok") {
		t.Fatalf("%q %v", out, err)
	}
	out, err = (callTool{l}).Run(context.Background(), json.RawMessage(`{"name":"desk","cap":"info"}`))
	if err != nil || !strings.Contains(out, "id\tabc") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestEnsureToken(t *testing.T) {
	root := t.TempDir()
	tok, created, err := EnsureToken(root, "")
	if err != nil || !created || tok == "" {
		t.Fatalf("%q %v %v", tok, created, err)
	}
	fi, err := os.Stat(root + "/node-token")
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	again, created, err := EnsureToken(root, "")
	if err != nil || created || again != tok {
		t.Fatalf("again %q %v %v", again, created, err)
	}
	id, err := LoadID(root)
	if err != nil || id == "" {
		t.Fatal(err)
	}
	same, err := LoadID(root)
	if err != nil || same != id {
		t.Fatalf("%s %s", id, same)
	}
}
