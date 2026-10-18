package remote

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
)

// 「Bot 互发消息」 off on A's record of B: Send refuses fast with no gateway
// call and nothing queued; off on B's record of A: A's box to B is acked and
// dropped (no inbox post, no bounce), while the same box with the switch on
// is delivered.
func TestBotMessagingOff(t *testing.T) {
	a, b, ka, _ := gatewayPair(t)
	mustRemote(t, SetGateway(a, "http://gateway"))
	selfB := selfID(t, b)
	cfgB, err := Load(b)
	mustRemote(t, err)
	selfA := cfgB.Devices[0].ID

	var calls atomic.Int32
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("gateway must not be called")
	})}
	t.Cleanup(func() { http.DefaultClient = old })

	// Send side on A.
	cfgA, err := Load(a)
	mustRemote(t, err)
	if len(cfgA.Devices) != 1 || cfgA.Devices[0].NoBotMessaging {
		t.Fatalf("bot_messaging is not on by default: %+v", cfgA.Devices)
	}
	mustRemote(t, SetBotMessaging(a, cfgA.Devices[0].ID, false))
	if err := SetBotMessaging(a, "nope", false); err == nil {
		t.Fatal("unknown id was accepted")
	}
	if views, err := Devices(a); err != nil || len(views) != 1 || views[0].BotMessaging {
		t.Fatalf("view still reports bot messaging on: %+v %v", views, err)
	}
	sender, err := bot.Create(a, "ada", "", nil)
	mustRemote(t, err)
	mustRemote(t, sender.SetOwners([]string{"u1"}))
	queued, err := Send(context.Background(), a, "ada", "bob@b", "hello", "chain", 1, time.Now())
	if err == nil || err.Error() != "remote: bot messages to b are turned off in 设备" || queued {
		t.Fatalf("send with bot messaging off: %v %v", queued, err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("gateway called %d times with bot messaging off", n)
	}
	if q := outboxItems(t, a); len(q) != 0 {
		t.Fatalf("send was queued: %v", q)
	}

	// Queued before the switch went off: the outbox drops it unsent and tells ada.
	env, err := newBoxEnvelope(ka, cfgA.Devices[0], "box", []byte(`{}`), time.Now())
	mustRemote(t, err)
	mustRemote(t, enqueue(a, outboxItem{Env: env, FromBot: "ada", To: "bob@b", Snippet: "hello", Created: time.Now(), Next: time.Now()}))
	mustRemote(t, flushOutbox(context.Background(), a, &gateway.Client{Base: "http://gateway", Key: ka}, time.Now()))
	if q := outboxItems(t, a); len(q) != 0 || calls.Load() != 0 {
		t.Fatalf("queued box still sent or kept: %v calls=%d", q, calls.Load())
	}
	adaBox, err := inbox.Open(sender.InboxPath())
	mustRemote(t, err)
	if len(adaBox.Pending()) != 1 {
		t.Fatalf("ada was not told: %d events", len(adaBox.Pending()))
	}

	// Receive side on B.
	target, err := bot.Load(b, "bob")
	mustRemote(t, err)
	now := time.Now()
	box := func() gateway.Envelope {
		body := []byte(`{"from_bot":"ada","to_bot":"bob","text":"hi","chain":"c","hops":1}`)
		env, err := newBoxEnvelope(ka, cfgA.Devices[0], "box", body, now) // A to B
		mustRemote(t, err)
		return env
	}
	mustRemote(t, SetBotMessaging(b, selfA, false))
	ack, bounces, err := Receive(b, selfB, []gateway.Envelope{box()}, now)
	mustRemote(t, err)
	in, err := inbox.Open(target.InboxPath())
	mustRemote(t, err)
	if len(ack) != 1 || len(bounces) != 0 || len(in.Pending()) != 0 {
		t.Fatalf("box with bot messaging off: ack=%v bounces=%d pending=%d", ack, len(bounces), len(in.Pending()))
	}
	mustRemote(t, SetBotMessaging(b, selfA, true))
	ack, _, err = Receive(b, selfB, []gateway.Envelope{box()}, now)
	mustRemote(t, err)
	mustRemote(t, in.Reload())
	if len(ack) != 1 || len(in.Pending()) != 1 {
		t.Fatalf("box with bot messaging on: ack=%v pending=%d", ack, len(in.Pending()))
	}
}

func selfID(t *testing.T, root string) string {
	t.Helper()
	_, id, _, err := Identity(root)
	mustRemote(t, err)
	return id
}

// outboxItems lists what root has queued; the directory missing means none.
func outboxItems(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "remote", "outbox"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	mustRemote(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
