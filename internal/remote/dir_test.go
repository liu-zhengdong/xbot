package remote

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/person"
)

func TestDirectoryReceiveVisible(t *testing.T) {
	a, root, _, key, _, self := pair(t)
	cfg, err := Load(a)
	mustRemote(t, err)
	now := time.Now().Truncate(time.Second)
	receiver, err := Load(root)
	mustRemote(t, err)
	path := filepath.Join(root, "remote", "dirs", receiver.Devices[0].ID+".json")
	send := func(dir directory, kind string) {
		t.Helper()
		body, _ := json.Marshal(dir)
		env, err := newBoxEnvelope(key, cfg.Devices[0], kind, body, now)
		mustRemote(t, err)
		if kind == "box" { // changing the signed kind still cannot bypass AAD
			env.Kind = "dir"
			env.Sign(key)
		}
		ack, bounces, err := Receive(root, self, []gateway.Envelope{env}, now)
		mustRemote(t, err)
		if len(ack) != 1 || len(bounces) != 0 {
			t.Fatal("directory must be acked without bouncing")
		}
	}
	dir := directory{[]dirBot{{Name: "ada"}}, now.Unix()}
	send(dir, "dir")
	visible, err := VisibleBots(root, []string{"u1"}, now.Add(5*time.Minute))
	mustRemote(t, err)
	if len(visible) != 1 || visible[0].Name != "ada@a" {
		t.Fatalf("directory not visible or unsafe: %+v", visible)
	}
	for _, tc := range []struct {
		owners []string
		at     time.Time
	}{{[]string{"other"}, now}, {[]string{"u1"}, now.Add(7*24*time.Hour + time.Second)}} {
		bots, err := VisibleBots(root, tc.owners, tc.at)
		mustRemote(t, err)
		if len(bots) != 0 {
			t.Fatal("private or stale directory visible")
		}
	}
	for _, bad := range []directory{
		{[]dirBot{{Name: "old"}}, now.Unix() - 1},
		{[]dirBot{{Name: "../bad"}}, now.Unix() + 1},
		{make([]dirBot, 65), now.Unix() + 1},
	} {
		send(bad, "dir")
	}
	send(directory{[]dirBot{{Name: "relabel"}}, now.Unix() + 1}, "box")
	var cache dirCache
	mustRemote(t, readJSON(path, &cache))
	if cache.At != dir.At || len(cache.Bots) != 1 || cache.Bots[0].Name != "ada" || !cache.Received.Equal(now) {
		t.Fatal("invalid or older directory replaced cache")
	}
	send(directory{[]dirBot{{Name: "newer"}}, now.Unix() + 1}, "dir")
	mustRemote(t, readJSON(path, &cache))
	if cache.Bots[0].Name != "newer" {
		t.Fatal("newer directory ignored")
	}
	for p, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(p)
		mustRemote(t, err)
		if info.Mode().Perm() != mode {
			t.Fatal("directory cache permissions")
		}
	}
	mustRemote(t, RemoveDevice(root, "a"))
	send(dir, "dir")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unpaired directory stored or cache not removed")
	}
}

func TestDirectorySenderDecision(t *testing.T) {
	a, root, _, _, key, _ := pair(t)
	other, err := bot.Create(root, "private", "", nil)
	mustRemote(t, err)
	mustRemote(t, other.SetOwners([]string{"other"}))
	// No vault redactor is registered yet at serve startup.
	owned, err := bot.Create(root, "owned", "sk-test-directory-secret-value", nil)
	mustRemote(t, err)
	mustRemote(t, owned.SetOwners([]string{"u1"}))
	bots, err := directoryFor(root, Device{Person: "u1"})
	mustRemote(t, err)
	if len(bots) != 2 || bots[0].Name != "bob" || bots[1].Name != "owned" {
		t.Fatal("directory ownership failed")
	}
	raw, err := json.Marshal(directory{bots, 1_800_000_000})
	mustRemote(t, err)
	if strings.Contains(string(raw), "persona") || strings.Contains(string(raw), "sk-test-directory-secret-value") {
		t.Fatal("directory plaintext contains persona or its secret")
	}
	now := time.Unix(1_800_000_000, 0)
	state, due := directoryDue(bots, dirSent{}, now)
	if !due {
		t.Fatal("first directory not due")
	}
	if _, due := directoryDue(bots, state, now.Add(dirRefresh-time.Second)); due {
		t.Fatal("unchanged directory resent early")
	}
	if _, due := directoryDue(bots, state, now.Add(dirRefresh)); !due {
		t.Fatal("directory refresh not due")
	}
	if _, due := directoryDue(bots[:1], state, now.Add(time.Minute)); !due {
		t.Fatal("changed directory not due")
	}
	calls := 0
	xkey, _, err := XIdentity(a)
	mustRemote(t, err)
	client := &gateway.Client{Base: "http://gateway", Key: key, HTTP: &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var env gateway.Envelope
		mustRemote(t, json.NewDecoder(r.Body).Decode(&env))
		body, err := openBox(xkey, env)
		mustRemote(t, err)
		if env.Kind != "dir" || !strings.Contains(string(body), "owned") {
			t.Fatal("missing encrypted directory")
		}
		if strings.Contains(string(body), "persona") || strings.Contains(string(body), "sk-test-directory-secret-value") {
			t.Fatal("sealed directory plaintext contains persona or its secret")
		}
		w := httptest.NewRecorder()
		if calls == 1 {
			w.WriteHeader(503)
		}
		return w.Result(), nil
	})}}
	mustRemote(t, SetGateway(root, client.Base))
	for _, at := range []time.Time{now, now.Add(time.Minute), now.Add(2 * time.Minute)} {
		mustRemote(t, pushDirectories(context.Background(), root, client, at))
	}
	var sent map[string]dirSent
	mustRemote(t, readJSON(filepath.Join(root, "remote", "dirsent.json"), &sent))
	queued, _ := filepath.Glob(filepath.Join(root, "remote", "outbox", "*.json"))
	if calls != 2 || len(sent) != 1 || len(queued) != 0 {
		t.Fatal("directory retry, dedup or best effort persistence failed")
	}
}

func TestDirectoryIgnoresUnknownFields(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	body, err := json.Marshal(map[string]any{
		"bots": []map[string]string{{"name": "bob", "persona": strings.Repeat("x", 81), "future": "ignored"}},
		"at":   now.Unix(), "future": "ignored",
	})
	mustRemote(t, err)
	mustRemote(t, receiveDirectory(root, "device", body, now))
	raw, err := os.ReadFile(filepath.Join(root, "remote", "dirs", "device.json"))
	mustRemote(t, err)
	var cache dirCache
	mustRemote(t, json.Unmarshal(raw, &cache))
	if len(cache.Bots) != 1 || cache.Bots[0].Name != "bob" || strings.Contains(string(raw), "persona") || strings.Contains(string(raw), "future") {
		t.Fatal("unknown fields rejected or persisted")
	}
}

// Each bot carries its portrait seed, or its name when none was set, plus the
// custom flag. Uploaded images never travel.
func TestDirectorySendsAvatarSeed(t *testing.T) {
	root := t.TempDir()
	seeded, err := bot.Create(root, "ada", "", nil)
	mustRemote(t, err)
	mustRemote(t, seeded.SetOwners([]string{"u1"}))
	mustRemote(t, seeded.SetAvatarSeed("writer#2"))
	old, err := bot.Create(root, "old", "", nil)
	mustRemote(t, err)
	mustRemote(t, old.SetOwners([]string{"u1"}))
	cfg := old.Config()
	cfg.AvatarSeed = "" // what a bot created before seeds looks like
	raw, err := json.Marshal(cfg)
	mustRemote(t, err)
	mustRemote(t, os.WriteFile(filepath.Join(bot.Dir(root, "old"), "config.json"), raw, 0600))
	mustRemote(t, old.SetAvatar([]byte("\x89PNG\r\n\x1a\n"), "image/png"))
	bots, err := directoryFor(root, Device{Person: "u1"})
	mustRemote(t, err)
	want := []dirBot{{Name: "ada", Seed: "writer#2"}, {Name: "old", Seed: "old", Custom: true}}
	if len(bots) != len(want) || bots[0] != want[0] || bots[1] != want[1] {
		t.Fatalf("directory avatars: %+v", bots)
	}
	sent, err := json.Marshal(bots[1])
	mustRemote(t, err)
	if strings.Contains(string(sent), "PNG") || strings.Contains(string(sent), "image") {
		t.Fatal("uploaded image data or type sent in directory")
	}
}

// The directory hash covers the seed, so changing one resends at once.
func TestDirectorySeedChangeIsDue(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	state, due := directoryDue([]dirBot{{Name: "ada", Seed: "ada"}}, dirSent{}, now)
	if !due {
		t.Fatal("first directory not due")
	}
	if _, due := directoryDue([]dirBot{{Name: "ada", Seed: "ada"}}, state, now.Add(time.Minute)); due {
		t.Fatal("unchanged directory resent")
	}
	for _, next := range [][]dirBot{
		{{Name: "ada", Seed: "writer#2"}},
		{{Name: "ada", Seed: "ada", Custom: true}},
	} {
		if _, due := directoryDue(next, state, now.Add(time.Minute)); !due {
			t.Fatalf("avatar change not due: %+v", next)
		}
	}
}

// A bad seed costs that bot its portrait only; the rest of the directory stays
// and a valid seed is kept.
func TestDirectoryReceiveSeeds(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	body, err := json.Marshal(directory{Bots: []dirBot{
		{Name: "ada", Seed: "writer#2"},
		{Name: "bob", Seed: "<script>", Custom: true},
		{Name: "cid", Seed: strings.Repeat("x", 81)},
	}, At: now.Unix()})
	mustRemote(t, err)
	mustRemote(t, receiveDirectory(root, "device", body, now))
	var cache dirCache
	mustRemote(t, readJSON(filepath.Join(root, "remote", "dirs", "device.json"), &cache))
	want := []dirBot{{Name: "ada", Seed: "writer#2"}, {Name: "bob", Custom: true}, {Name: "cid"}}
	if len(cache.Bots) != len(want) || cache.Bots[0] != want[0] || cache.Bots[1] != want[1] || cache.Bots[2] != want[2] {
		t.Fatalf("received seeds: %+v", cache.Bots)
	}
}

// Devices() lists one avatar per bot, always a map; an old peer's cache entry
// without a seed gives an empty seed, and a bad one is dropped there too.
func TestDevicesBotAvatars(t *testing.T) {
	root := t.TempDir()
	_, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	mustRemote(t, err)
	add := func(name string) Device {
		pub, _, err := ed25519.GenerateKey(nil)
		mustRemote(t, err)
		d, err := AddDevice(root, name, base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
		mustRemote(t, err)
		return d
	}
	somewhere, empty, fresh := add("mbp"), add("imac"), add("mini")
	now := time.Unix(1_800_000_000, 0)
	old, err := json.Marshal(map[string]any{"bots": []map[string]any{{"name": "bob"}, {"name": "eve", "seed": "<script>"}}, "at": now.Unix()})
	mustRemote(t, err)
	good, err := json.Marshal(directory{Bots: []dirBot{{Name: "writer", Seed: "writer#2"}, {Name: "coach", Seed: "coach", Custom: true}}, At: now.Unix()})
	mustRemote(t, err)
	for id, body := range map[string][]byte{somewhere.ID: old, empty.ID: good} {
		path := filepath.Join(root, "remote", "dirs", id+".json")
		mustRemote(t, os.MkdirAll(filepath.Dir(path), 0700))
		mustRemote(t, os.WriteFile(path, body, 0600))
	}
	views, err := Devices(root)
	mustRemote(t, err)
	for _, v := range views {
		if v.BotAvatars == nil {
			t.Fatalf("nil bot_avatars: %+v", v)
		}
	}
	byID := map[string]DeviceView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	if got := byID[somewhere.ID].BotAvatars; len(got) != 2 || got["bob"] != (BotAvatar{}) || got["eve"] != (BotAvatar{}) {
		t.Fatalf("old peer avatars: %+v", got)
	}
	if got := byID[empty.ID].BotAvatars; len(got) != 2 || got["writer"] != (BotAvatar{Seed: "writer#2"}) || got["coach"] != (BotAvatar{Seed: "coach", Custom: true}) {
		t.Fatalf("avatars: %+v", got)
	}
	// A device whose directory never arrived still sends a map, not null.
	raw, err := json.Marshal(byID[fresh.ID])
	mustRemote(t, err)
	if !strings.Contains(string(raw), `"bot_avatars":{}`) || strings.Contains(string(raw), "bot_avatars\":null") {
		t.Fatalf("empty device json: %s", raw)
	}
}

func TestDirectorySeenIsSenderTime(t *testing.T) {
	a, root, _, key, _, self := pair(t)
	cfg, err := Load(a)
	mustRemote(t, err)
	now := time.Now().Truncate(time.Second)
	made := now.Add(-48 * time.Hour)
	body, _ := json.Marshal(directory{[]dirBot{{Name: "ada"}}, made.Unix()})
	env, err := newBoxEnvelope(key, cfg.Devices[0], "dir", body, now)
	mustRemote(t, err)
	_, _, err = Receive(root, self, []gateway.Envelope{env}, now)
	mustRemote(t, err)
	bots, err := VisibleBots(root, []string{"u1"}, now)
	mustRemote(t, err)
	if len(bots) != 1 || !bots[0].Seen.Equal(made) {
		t.Fatalf("seen should be the directory time: %+v", bots)
	}
}
