package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
)

// Key path through the real poll loops: B has no gateway token, so it can only
// reach A as a pending install until A allows, and must still get A's replies.
func TestShortCodeRunLoopsWithoutJoinerToken(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer((&gateway.Server{Dir: dir, Token: "test"}).Handler())
	defer srv.Close()
	t.Setenv("XBOT_GATEWAY_TOKEN", "test")
	a, b := pairingRoot(t), pairingRoot(t)
	for _, v := range []struct{ root, label, bot string }{{a, "书房 iMac", "ada"}, {b, "MacBook", "bob"}} {
		raw, _ := json.Marshal(map[string]string{"name": v.label})
		mustRemote(t, fsutil.WriteAtomic(filepath.Join(v.root, "remote", "device.json"), raw, 0600))
		created, err := bot.Create(v.root, v.bot, "", nil)
		mustRemote(t, err)
		mustRemote(t, created.SetOwners([]string{"u1"}))
	}
	t.Cleanup(func() { pairings.Delete(a); pairings.Delete(b) })
	mustRemote(t, SetGateway(a, srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { Run(ctx, a, "test", nil); done <- struct{}{} }()
	go func() { Run(ctx, b, "", nil); done <- struct{}{} }() // B's serve has no token.
	defer func() { cancel(); <-done; <-done }()

	code, _, err := NewShortCode(ctx, a, "")
	mustRemote(t, err)
	wait := func(root, side, phase string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			st := PairingStatus(root)
			got := st.A.Phase
			if side == "B" {
				got = st.B.Phase
			}
			if got == phase {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s never reached %s: %+v", side, phase, PairingStatus(root))
	}
	// B has no gateway yet: the join sets it and wakes B's idle loop.
	mustRemote(t, JoinShortCode(ctx, b, code, srv.URL))
	wait(b, "B", "confirm")
	mustRemote(t, ConfirmJoin(ctx, b))
	wait(a, "A", "request")
	mustRemote(t, DecidePairing(ctx, a, true, false))
	wait(b, "B", "done")
	_, bid, _, err := Identity(b)
	mustRemote(t, err)
	if _, err := os.Stat(filepath.Join(dir, "installs", bid)); err != nil {
		t.Fatalf("B was not admitted as a gateway member: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending", bid)); !os.IsNotExist(err) {
		t.Fatalf("B is still pending: %v", err)
	}
	cfgA, err := Load(a)
	mustRemote(t, err)
	cfgB, err := Load(b)
	mustRemote(t, err)
	if len(cfgA.Devices) != 1 || len(cfgB.Devices) != 1 || deviceLabel(cfgA.Devices[0]) != "MacBook" || deviceLabel(cfgB.Devices[0]) != "书房 iMac" {
		t.Fatalf("devices: A=%+v B=%+v", cfgA.Devices, cfgB.Devices)
	}
	// Each side pushes its directory right after pairing, not on the next minute tick.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var da, db dirCache
		mustRemote(t, readJSON(filepath.Join(a, "remote", "dirs", cfgA.Devices[0].ID+".json"), &da))
		mustRemote(t, readJSON(filepath.Join(b, "remote", "dirs", cfgB.Devices[0].ID+".json"), &db))
		if !da.Received.IsZero() && !db.Received.IsZero() {
			if len(da.Bots) != 1 || da.Bots[0].Name != "bob" {
				t.Fatalf("A sees B's bots as %+v", da.Bots)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no directory within 10 s of pairing")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cfgA.Devices[0].Person == "" || cfgB.Devices[0].Person == "" {
		t.Fatalf("both sides must bind the new device to their person: A=%+v B=%+v", cfgA.Devices, cfgB.Devices)
	}
}
