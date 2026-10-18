package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/gateway"
	"xbot/internal/person"
	"xbot/internal/remote"
)

func TestRemoteCLI(t *testing.T) {
	root := t.TempDir()
	checkDefault := func() {
		t.Helper()
		cfg, err := remote.Load(root)
		if err != nil || cfg.Gateway != "" || cfg.GatewayURL() != remote.DefaultGateway {
			t.Fatalf("default config: %+v, %v", cfg, err)
		}
		if got := captureStdout(t, func() error { return cmdRemote(root, []string{"gateway"}) }); got != "https://xbot.babelgo.cn (default)\n" {
			t.Fatal(got)
		}
	}
	checkDefault()
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id := captureStdout(t, func() error { return run([]string{"--root", t.TempDir(), "remote", "id"}) })
	fields := strings.Fields(id)
	if len(fields) != 6 || fields[0] != "id" || fields[2] != "key" || fields[4] != "xkey" {
		t.Fatal("bad identity output")
	}
	for _, args := range [][]string{{"gateway", "https://example.test/"}, {"add", "b", fields[3], "--person", p.ID}} {
		if err := cmdRemote(root, args); err != nil {
			t.Fatal(err)
		}
	}
	if got := captureStdout(t, func() error { return cmdRemote(root, []string{"gateway"}) }); got != "https://example.test\n" {
		t.Fatal(got)
	}
	cfg, err := remote.Load(root)
	if err != nil || cfg.GatewayURL() != "https://example.test" {
		t.Fatalf("override: %+v, %v", cfg, err)
	}
	if err := cmdRemote(root, []string{"gateway", "default"}); err != nil {
		t.Fatal(err)
	}
	checkDefault()
	got := captureStdout(t, func() error { return cmdRemote(root, []string{"list"}) })
	if got != "b\t"+fields[1]+"\t"+p.ID+"\n" {
		t.Fatal("bad device listing")
	}
	if err := cmdRemote(root, []string{"remove", "b"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = remote.Load(root)
	if err != nil || len(cfg.Devices) != 0 {
		t.Fatal("device not removed")
	}
}

func TestRemoteInviteJoinCode(t *testing.T) {
	root := t.TempDir()
	if _, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner"); err != nil {
		t.Fatal(err)
	}
	handler := (&gateway.Server{Dir: t.TempDir(), Token: "join-token"}).Handler()
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: remoteTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	if err := cmdRemote(root, []string{"gateway", "http://gateway"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_GATEWAY_TOKEN", "join-token")
	out := captureStdout(t, func() error { return cmdRemote(root, []string{"invite"}) })
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "xbot-pair:") || !strings.HasPrefix(lines[1], "fingerprint ") {
		t.Fatal("bad invite output")
	}
	key, _, _, err := remote.Identity(root)
	if err == nil {
		err = (&gateway.Client{Base: "http://gateway", Key: key}).Register(context.Background(), "")
	}
	if err != nil {
		t.Fatalf("invite did not register install: %v", err)
	}
	pc, err := remote.ParseCode(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	if pc.Gateway != "http://gateway" || pc.Token != "join-token" || pc.Name == "" || len(pc.Secret) != 43 {
		t.Fatal("bad parsed code")
	}
}

type remoteTransport func(*http.Request) (*http.Response, error)

func (f remoteTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
