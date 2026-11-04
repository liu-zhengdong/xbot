package pipe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/deviceexec"
	"xbot/internal/deviceidentity"
)

func TestExecutionPipeWritesWithoutTargetBot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, _ := account.NewFileStore(t.TempDir())
	api := account.Local{S: account.New(st, nil, account.Config{})}
	signed, err := api.Register(ctx, "execution@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	source, err := api.BindDevice(ctx, signed.Session, account.DeviceReport{ID: "source", Name: "source"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := api.BindDevice(ctx, signed.Session, account.DeviceReport{ID: "target", Name: "target"})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manager, err := deviceexec.New(root, "target", "target")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	hub := &Hub{Accounts: api}
	server := httptest.NewServer(hub.Handler())
	defer server.Close()
	var owners atomic.Int32
	d := &Device{Hub: server.URL, Session: target.Token, DeviceID: "target", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("execution reached owner mux") }), Authorize: func(*http.Request) error { owners.Add(1); return errors.New("owner must not be used") }}
	d.ValidateExecution = func(ctx context.Context, id ExecutionIdentity) error {
		own, err := api.AuthenticateBinding(ctx, target.Token)
		if err != nil {
			return err
		}
		if id.TargetBindingID != own.ID {
			return errors.New("target changed")
		}
		list, err := api.BindingDevices(ctx, target.Token)
		if err != nil {
			return err
		}
		for _, v := range list {
			if v.ID == id.DeviceID && v.BindingID == id.BindingID {
				return nil
			}
		}
		return errors.New("source revoked")
	}
	d.Execution = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := deviceidentity.Source(r.Context())
		if !ok {
			t.Error("no execution source")
		}
		s := deviceexec.Source{Account: id.AccountKey, DeviceID: id.DeviceID, Bot: id.Bot, ContextID: id.ContextID, Readonly: id.Readonly}
		http.StripPrefix("/api/device-execution", manager.HandlerFor(s)).ServeHTTP(w, r)
	})
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- d.Run(runCtx) }()
	defer func() { stop(); <-done }()
	waitFor(t, func() bool { hub.mu.Lock(); defer hub.mu.Unlock(); return len(hub.standby) == 1 })
	if _, err := NewExecutionTicket(ctx, server.URL, signed.Session, "target", ExecutionIdentity{Bot: "ada", ContextID: "main"}); status(err) != 401 {
		t.Fatalf("UI session bought execution ticket: %v", err)
	}
	if _, err := NewTicket(ctx, server.URL, source.Token, "target"); status(err) != 401 {
		t.Fatalf("binding bought owner ticket: %v", err)
	}
	ticket, err := NewExecutionTicket(ctx, server.URL, source.Token, "target", ExecutionIdentity{Bot: "ada", ContextID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := Dial(ctx, server.URL, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if code, _, err := conn.Get(ctx, "/api/session"); err != nil || code != 403 {
		t.Fatalf("runtime could access owner route: %d %v", code, err)
	}
	marker := filepath.Join(root, "marker.txt")
	call := deviceexec.Call{Ref: "call_marker_001", Kind: "write", Tool: "write_file", Description: "write marker", Args: json.RawMessage(`{"path":` + quoteJSON(marker) + `,"content":"target marker"}`)}
	body, _ := json.Marshal(call)
	code, raw, _, err := conn.Do(ctx, "POST", "/api/device-execution/calls", body, "application/json")
	if err != nil || code != 202 {
		t.Fatalf("submit: %d %v %s", code, err, raw)
	}
	waitFor(t, func() bool { data, _ := os.ReadFile(marker); return string(data) == "target marker" })
	if _, err := os.Stat(filepath.Join(root, "bots")); !os.IsNotExist(err) {
		t.Fatal("target execution created a Bot")
	}
	if owners.Load() != 0 {
		t.Fatal("runtime used owner credential")
	}
	// Already connected sessions must stop accepting operations after source revocation.
	if err := api.DeleteDevice(ctx, signed.Session, "source"); err != nil {
		t.Fatal(err)
	}
	code, _, err = conn.Get(ctx, "/api/device-execution/tasks")
	if err != nil || code != 403 {
		t.Fatalf("revoked connected source retained access: %d %v", code, err)
	}
	if _, err := NewExecutionTicket(ctx, server.URL, source.Token, "target", ExecutionIdentity{Bot: "ada", ContextID: "main"}); status(err) != 401 {
		t.Fatalf("revoked binding bought ticket: %v", err)
	}
}
func quoteJSON(s string) string { raw, _ := json.Marshal(s); return string(raw) }
