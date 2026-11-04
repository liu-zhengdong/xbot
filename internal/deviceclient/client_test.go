package deviceclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/cliops"
	"xbot/internal/deviceexec"
	"xbot/internal/pipe"
)

func TestCallPreservesTaskPaginationAndStoresRawArtifact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := account.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := account.Local{S: account.New(store, nil, account.Config{})}
	signed, err := api.Register(ctx, "client@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	source, err := api.BindDevice(ctx, signed.Session, account.DeviceReport{ID: "source", Name: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := api.BindDevice(ctx, signed.Session, account.DeviceReport{ID: "target", Name: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	raw, _ := json.Marshal(Binding{Key: source.AccountKey, DeviceID: source.DeviceID, BindingID: source.ID, Token: source.Token})
	if err := os.WriteFile(filepath.Join(root, "account-binding.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&pipe.Hub{Accounts: api}).Handler())
	defer server.Close()
	ready := make(chan struct{})
	var once sync.Once
	artifact := []byte{0, 1, 2, 255, 0, 17}
	d := &pipe.Device{Hub: server.URL, Session: target.Token, DeviceID: target.DeviceID, Standby: func(hook string) {
		if hook != "" {
			once.Do(func() { close(ready) })
		}
	}, ValidateExecution: func(context.Context, pipe.ExecutionIdentity) error { return nil }}
	d.Execution = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/device-execution/tasks":
			if r.URL.Query().Get("offset") != "7" || r.URL.Query().Get("limit") != "2" {
				t.Errorf("task pagination lost: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tasks":[],"offset":7,"next_offset":9,"has_more":true}`))
		case "/api/device-execution/tasks/ref/artifacts/file":
			if r.URL.Query().Get("offset") != "11" || r.URL.Query().Get("limit") != "6" {
				t.Errorf("artifact range lost: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(artifact)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- d.Run(runCtx) }()
	defer func() { stop(); <-done }()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("target did not enter standby")
	}
	client := Client{Root: root, Accounts: api, Hub: server.URL}
	identity := deviceexec.Source{Bot: "ada", ContextID: "main"}
	reply, err := client.Call(ctx, identity, cliops.DeviceRequest{Action: "tasks", DeviceID: target.DeviceID, Offset: 7, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		NextOffset int  `json:"next_offset"`
		HasMore    bool `json:"has_more"`
	}
	if json.Unmarshal(reply, &page) != nil || page.NextOffset != 9 || !page.HasMore {
		t.Fatalf("pagination receipt %s", reply)
	}
	reply, err = client.Call(ctx, identity, cliops.DeviceRequest{Action: "artifact", DeviceID: target.DeviceID, Ref: "ref", ArtifactID: "file", Offset: 11, Limit: 6})
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		File       string `json:"file"`
		Bytes      int    `json:"bytes"`
		NextOffset int    `json:"next_offset"`
	}
	if json.Unmarshal(reply, &receipt) != nil || receipt.File == "" || receipt.Bytes != len(artifact) || receipt.NextOffset != 17 {
		t.Fatalf("artifact receipt %s", reply)
	}
	data, err := os.ReadFile(receipt.File)
	if err != nil || string(data) != string(artifact) {
		t.Fatal("artifact bytes changed")
	}
	info, err := os.Stat(receipt.File)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("artifact permissions changed")
	}
}
