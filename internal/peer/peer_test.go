package peer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/memory"
)

func TestMessageVisibilityHopsAndRate(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	mustPeerBot(t, root, "cara", "u2")

	visible, err := VisibleBots(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Name != "bob" {
		t.Fatalf("visible %+v", visible)
	}
	owned, err := OwnerBots(root, "u2")
	if err != nil || len(owned) != 1 || owned[0].Name != "cara" {
		t.Fatalf("owned %+v %v", owned, err)
	}

	bus := &Bus{Root: root, Limit: 2, Window: time.Minute}
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	if err := bus.Message("ada", "ada", "self", "", 1, now); err == nil {
		t.Fatal("self message was accepted")
	}
	if err := bus.Message("ada", "cara", "cross", "", 1, now); err == nil {
		t.Fatal("cross-owner message was accepted")
	}
	if err := bus.Message("ada", "bob", "zero", "", 0, now); err == nil {
		t.Fatal("hop 0 was accepted")
	}
	if err := bus.Message("ada", "bob", "four", "", 4, now); err == nil {
		t.Fatal("hop 4 was accepted")
	}
	if err := bus.Message("ada", "bob", "one", "ch_test", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := bus.Message("ada", "bob", "two", "ch_test", 1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := bus.Message("ada", "bob", "three", "ch_test", 1, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "rate") {
		t.Fatalf("rate: %v", err)
	}
	evs := readPosts(t, bob.InboxPath())
	if len(evs) != 2 || evs[0].Source != "bot:ada" || evs[0].SenderKind != link.KindBot || evs[0].Hops != 1 || evs[0].Content != "one" {
		t.Fatalf("inbox %+v", evs)
	}
	if len(readPosts(t, ada.InboxPath())) != 0 {
		t.Fatal("sender inbox was written")
	}
}

func TestGroupFanoutSkipsSenderAndKeepsMemoryPrivate(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	if err := memory.Open(root).Remember(context.Background(), memory.Fact{
		Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: "bob", WrittenBy: "bob", Text: "BOB_SECRET_FACT", Time: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	bus := &Bus{Root: root}
	if _, err := bus.CreateGroup("ada", "room", nil); err == nil {
		t.Fatal("one-member group was accepted")
	}
	g, err := bus.CreateGroup("ada", "room", []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := bus.PostGroup(link.KindBot, "ada", g.ID, "from ada", "ch_g", 1, now); err != nil {
		t.Fatal(err)
	}
	if n := readPosts(t, ada.InboxPath()); len(n) != 0 {
		t.Fatalf("sender bot was woken: %+v", n)
	}
	bobPosts := readPosts(t, bob.InboxPath())
	if len(bobPosts) != 1 || !strings.Contains(bobPosts[0].Content, "from ada") || strings.Contains(bobPosts[0].Content, "BOB_SECRET_FACT") {
		t.Fatalf("bob fan-out %+v", bobPosts)
	}
	if bobPosts[0].Source != "group:"+g.ID || bobPosts[0].SenderKind != link.KindBot {
		t.Fatalf("event %+v", bobPosts[0])
	}
	if err := bus.PostGroup(link.KindPerson, "u1", g.ID, "from owner", "", 1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(readPosts(t, ada.InboxPath())) != 1 || len(readPosts(t, bob.InboxPath())) != 2 {
		t.Fatalf("person fan-out ada=%d bob=%d", len(readPosts(t, ada.InboxPath())), len(readPosts(t, bob.InboxPath())))
	}
	lines, err := bus.Transcript(g.ID)
	if err != nil || len(lines) != 2 || lines[1].Text != "from owner" {
		t.Fatalf("transcript %+v %v", lines, err)
	}
	if err := bus.PostGroup(link.KindBot, "ada", g.ID, "too far", "", 0, now); err == nil {
		t.Fatal("group hop 0 was accepted")
	}
}

func mustPeerBot(t *testing.T, root, name, owner string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, "persona", []string{link.NamePeers})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{owner}); err != nil {
		t.Fatal(err)
	}
	return b
}

func readPosts(t *testing.T, path string) []link.Event {
	t.Helper()
	var out []link.Event
	err := fsutil.ReadJSONL(path, func(line []byte) error {
		var rec struct {
			Op    string      `json:"op"`
			Event *link.Event `json:"event"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec.Op == "post" && rec.Event != nil {
			out = append(out, *rec.Event)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
