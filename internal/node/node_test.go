package node

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if _, err := Ping(ctx, srv.URL, "nope"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bad token: %v", err)
	}
	got, err := Ping(ctx, srv.URL, "tok")
	if err != nil || got.ID != "abc" || got.Name != "desk" || !containsCap(got.Caps, "info") || !containsCap(got.Caps, "shell") {
		t.Fatalf("%+v %v", got, err)
	}
	text, err := Call(ctx, srv.URL, "tok", "info", "", false)
	if err != nil || !strings.Contains(text, "id\tabc") || !strings.Contains(text, "os\t") {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := Call(ctx, srv.URL, "tok", "nope", "", false); err == nil || !strings.Contains(err.Error(), "unknown cap") {
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

func TestRunShellSwitch(t *testing.T) {
	root := t.TempDir()
	text, err := RunShell(root, "echo hello-node", false)
	if err != nil || !strings.Contains(text, "hello-node") {
		t.Fatalf("%q %v", text, err)
	}
	if err := setMode(root, "off"); err != nil {
		t.Fatal(err)
	}
	if _, err := RunShell(root, "echo no", true); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
	if err := setMode(root, "ask"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "ran")
	_, err = RunShell(root, "touch "+marker, true)
	if err == nil || !strings.Contains(err.Error(), "xbot node approve") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("ask ran the command")
	}
	id := pendingFrom(t, err)
	fi, statErr := os.Stat(filepath.Join(root, "node-pending", id+".json"))
	if statErr != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("pending mode %v %v", fi, statErr)
	}
	if err := setMode(root, "off"); err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(root, id); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("off approve ran the command")
	}
	if err := setMode(root, "ask"); err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(root, id); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatal(statErr)
	}
}

func TestRemoteApprovedDoesNotRun(t *testing.T) {
	root := t.TempDir()
	if err := setMode(root, "ask"); err != nil {
		t.Fatal(err)
	}
	info, err := Identity(root, "desk")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "marker")
	srv := httptest.NewServer(Handler("tok", info, LocalCall(root, info)))
	defer srv.Close()
	_, err = Call(context.Background(), srv.URL, "tok", "shell", "touch "+marker, true)
	if err == nil || !strings.Contains(err.Error(), "xbot node approve") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("remote approved flag ran the command")
	}
}

func TestListUnauthorized(t *testing.T) {
	root := t.TempDir()
	info := Info{ID: "abc", Name: "desk", Caps: []string{"info"}}
	srv := httptest.NewServer(Handler("tok", info, nil))
	defer srv.Close()
	if err := Add(root, Node{Name: "desk", URL: srv.URL, Token: "bad"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(root, Node{Name: "down", URL: "http://127.0.0.1:1", Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	out, err := (listTool{Open(root)}).Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "desk\tunauthorized") || !strings.Contains(out, "down\toffline") || strings.Contains(out, "bad") {
		t.Fatalf("%q", out)
	}
}

func TestResolveToken(t *testing.T) {
	got, err := ResolveToken("-", "envtok", strings.NewReader("stdin-tok\n"))
	if err != nil || got != "stdin-tok" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = ResolveToken("", "envtok", strings.NewReader(""))
	if err != nil || got != "envtok" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = ResolveToken("flagtok", "envtok", nil)
	if err != nil || got != "flagtok" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ResolveToken("-", "", strings.NewReader("\n")); err == nil {
		t.Fatal("empty stdin token accepted")
	}
}

func TestServeStopsOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, addr, http.NewServeMux())
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func pendingFrom(t *testing.T, err error) string {
	t.Helper()
	const mark = "xbot node approve "
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	i := strings.LastIndex(msg, mark)
	if i < 0 {
		t.Fatal(err)
	}
	return strings.TrimSpace(msg[i+len(mark):])
}

func setMode(root, mode string) error {
	return os.WriteFile(root+"/machines.json", []byte(`{"machines":{"local":"`+mode+`"}}`+"\n"), 0600)
}

func containsCap(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
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
