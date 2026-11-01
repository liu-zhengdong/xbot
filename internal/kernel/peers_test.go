package kernel

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/remote"
)

func TestListBotsRemoteDirectory(t *testing.T) {
	root := t.TempDir()
	ada, _, err := bot.EnsurePrimary(root, bot.Seed{Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	id := strings.Repeat("b", 32)
	cfg, _ := json.Marshal(remote.Config{Devices: []remote.Device{{ID: id, Name: "b", Person: "u1"}}})
	cache, _ := json.Marshal(map[string]any{"bots": []map[string]string{{"name": "bob", "persona": "helper"}}, "at": now.Unix(), "received": now.Add(-5 * time.Minute)})
	for path, raw := range map[string][]byte{"devices.json": cfg, filepath.Join("dirs", id+".json"): cache} {
		if err := fsutil.WriteAtomic(filepath.Join(root, "remote", path), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	k := &Kernel{Bot: ada, Now: func() time.Time { return now }}
	out, err := k.BotHandlers().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || out.Local == nil || len(*out.Local) != 0 || out.Remote == nil || len(*out.Remote) != 1 {
		t.Fatalf("list %+v", out)
	}
	entry := (*out.Remote)[0]
	if entry.Name != "bob@b" || !entry.Seen.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("remote entry %+v", entry)
	}
}
