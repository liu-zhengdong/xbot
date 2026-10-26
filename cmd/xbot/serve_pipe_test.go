package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/pipe"
)

// Exercise the actual serve wiring, not a separately assembled Device: a
// restart restores a persisted session and exposes the same authenticated mux.
func TestRunServeRestoresAccountPipe(t *testing.T) {
	st, err := account.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accServer := httptest.NewServer(account.New(st, account.OutboxMailer{Dir: t.TempDir()}, account.Config{}).Handler())
	defer accServer.Close()
	acc := account.NewRemote(accServer.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	signed, err := acc.Register(ctx, "pipe@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	me, err := acc.Me(ctx, signed.Session)
	if err != nil {
		t.Fatal(err)
	}
	hubServer := httptest.NewServer((&pipe.Hub{Accounts: acc}).Handler())
	defer hubServer.Close()
	t.Setenv("XBOT_ACCOUNT_API", accServer.URL)
	t.Setenv("XBOT_PIPE_HUB", hubServer.URL)
	root := t.TempDir()
	binding, _ := json.Marshal(map[string]string{"key": me.Key, "device_id": "serve-device"})
	for name, body := range map[string][]byte{"account-binding.json": binding, "account-session": []byte(signed.Session)} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	// App mode avoids printing an owner login credential to the test log.
	go func() {
		done <- runServe(ctx, root, serveConfig{HTTPAddr: "127.0.0.1:0", App: true, Interval: time.Hour})
	}()
	stopped := false
	defer func() {
		if !stopped {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("serve did not stop")
			}
		}
	}()
	var ticket pipe.TicketOut
	for ctx.Err() == nil {
		ticket, err = pipe.NewTicket(ctx, hubServer.URL, signed.Session, "serve-device")
		if err == nil {
			break
		}
		select {
		case err := <-done:
			stopped = true
			t.Fatalf("serve exited: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err != nil {
		t.Fatal("serve did not enter standby")
	}
	conn, err := pipe.Dial(ctx, hubServer.URL, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	code, body, err := conn.Get(ctx, "/api/session")
	if err != nil || code != http.StatusOK || !strings.Contains(string(body), `"authed":true`) {
		t.Fatal("restored pipe did not reach owner mux")
	}
	cancel()
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
	if info, err := os.Stat(filepath.Join(root, "account-session")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("normal shutdown must preserve 0600 session for restart")
	}
	requestCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, _, err := conn.Get(requestCtx, "/api/session"); err == nil {
		t.Fatal("serve shutdown retained active pipe")
	}
}
