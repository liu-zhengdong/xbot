package peer

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestChatGroupRuntimeResultComplete(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	cara := mustPeerBot(t, root, "cara", "u1")
	now := time.Date(2026, 11, 2, 4, 0, 0, 0, time.FixedZone("source", 8*3600))
	bus := &Bus{Root: root, Now: func() time.Time { return now }}
	g, err := bus.CreateGroup("ada", "room", []string{"cara", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bus.PostGroupWithResult(link.KindBot, "ada", g.ID, "  hello\n", "ch_frozen", MaxHops, time.Time{})
	if err != nil || !result.Appended || result.Stage != "complete" || result.GroupID != g.ID || result.MessageID == "" || result.FailedMember != "" || !reflect.DeepEqual(result.PostedMembers, []string{"bob", "cara"}) {
		t.Fatalf("complete result %+v: %v", result, err)
	}
	lines, err := bus.Transcript(g.ID)
	if err != nil || len(lines) != 1 || lines[0].ID != result.MessageID || lines[0].Text != "hello" || lines[0].Kind != link.KindBot || lines[0].Sender != "ada" || !lines[0].Time.Equal(now) || lines[0].Time.Location() != time.UTC {
		t.Fatalf("actual transcript %+v: %v", lines, err)
	}
	if posts := readPosts(t, ada.InboxPath()); len(posts) != 0 {
		t.Fatalf("sender received own post: %+v", posts)
	}
	for _, target := range []*bot.Bot{bob, cara} {
		box, err := inbox.Open(target.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		posts := box.Pending()
		if len(posts) != 1 || posts[0].Source != "group:"+g.ID || posts[0].Sender != "ada" || posts[0].SenderKind != link.KindBot || posts[0].Hops != MaxHops || posts[0].Chain != "ch_frozen" || !posts[0].Time.Equal(now) || !strings.Contains(posts[0].Content, "\nhello\n") {
			t.Fatalf("%s inbox %+v", target.Name(), posts)
		}
	}
}

func TestChatGroupRuntimeResultFirstFailure(t *testing.T) {
	for _, stage := range []string{"recipient_load", "recipient_open"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			ada := mustPeerBot(t, root, "ada", "u1")
			bob := mustPeerBot(t, root, "bob", "u1")
			cara := mustPeerBot(t, root, "cara", "u1")
			dan := mustPeerBot(t, root, "dan", "u1")
			bus := &Bus{Root: root, Limit: 1}
			g, err := bus.CreateGroup("ada", "room", []string{"dan", "cara", "bob"})
			if err != nil {
				t.Fatal(err)
			}
			if stage == "recipient_load" {
				path := filepath.Join(bot.Dir(root, cara.Name()), "config.json")
				if err := os.WriteFile(path, []byte("invalid config\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(cara.InboxPath(), []byte("invalid inbox\n"), 0600); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
			result, err := bus.PostGroupWithResult(link.KindBot, "ada", g.ID, "partial", "ch_partial", 1, now)
			if err == nil || !result.Appended || result.MessageID == "" || result.GroupID != g.ID || result.Stage != stage || result.FailedMember != "cara" || !reflect.DeepEqual(result.PostedMembers, []string{"bob"}) {
				t.Fatalf("partial result %+v: %v", result, err)
			}
			lines, err := bus.Transcript(g.ID)
			if err != nil || len(lines) != 1 || lines[0].ID != result.MessageID || lines[0].Text != "partial" {
				t.Fatalf("committed transcript %+v: %v", lines, err)
			}
			if len(readPosts(t, bob.InboxPath())) != 1 || len(readPosts(t, ada.InboxPath())) != 0 || len(readPosts(t, dan.InboxPath())) != 0 {
				t.Fatal("fanout did not stop immediately after cara failed")
			}
			limited, err := bus.PostGroupWithResult(link.KindBot, "ada", g.ID, "no automatic rate refund", "", 1, now)
			if !errors.Is(err, ErrRateLimit) || limited.Stage != "rate" || limited.Appended || limited.MessageID != "" || limited.PostedMembers == nil || len(limited.PostedMembers) != 0 || limited.FailedMember != "" {
				t.Fatalf("partial post rate was refunded: %+v %v", limited, err)
			}
		})
	}
}

func TestChatGroupRuntimeResultAppendFailure(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	bus := &Bus{Root: root}
	g, err := bus.CreateGroup("ada", "room", []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bus.transcript(g.ID), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := bus.PostGroupWithResult(link.KindBot, "ada", g.ID, "must fail append", "", 1, time.Time{})
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || result.Stage != "transcript" || result.Appended || result.MessageID != "" || result.FailedMember != "" || result.PostedMembers == nil || len(result.PostedMembers) != 0 {
		t.Fatalf("append failure %+v: %v", result, err)
	}
	if len(readPosts(t, ada.InboxPath())) != 0 || len(readPosts(t, bob.InboxPath())) != 0 {
		t.Fatal("append failure reached inboxes")
	}
	// The original rate write precedes append; Appended is not a claim that
	// the entire root remained untouched.
	if _, err := os.Stat(filepath.Join(root, "peer-rate.json")); err != nil {
		t.Fatalf("original rate write missing: %v", err)
	}
	if err := bus.PostGroup(link.KindBot, "ada", g.ID, "legacy append failure", "", 1, time.Time{}); !errors.As(err, &pathErr) {
		t.Fatalf("legacy adapter lost storage cause: %v", err)
	}
}

func TestChatGroupRuntimeResultValidationAndRate(t *testing.T) {
	root := t.TempDir()
	mustPeerBot(t, root, "ada", "u1")
	mustPeerBot(t, root, "bob", "u1")
	bus := &Bus{Root: root}
	g, err := bus.CreateGroup("ada", "room", []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, sender, id, text string
		hops                   int
	}{
		{"unknown", "ada", g.ID, "hello", 1},
		{link.KindBot, "ada", "invalid", "hello", 1},
		{link.KindBot, "ada", g.ID, " \n", 1},
		{link.KindBot, "ada", g.ID, strings.Repeat("x", MaxText+1), 1},
		{link.KindBot, "ada", g.ID, "hello", 0},
		{link.KindBot, "ada", g.ID, "hello", MaxHops + 1},
		{link.KindBot, "outsider", g.ID, "hello", 1},
	} {
		result, err := bus.PostGroupWithResult(tc.kind, tc.sender, tc.id, tc.text, "", tc.hops, time.Time{})
		if err == nil || result.Stage != "validation" || result.GroupID != tc.id || result.Appended || result.MessageID != "" || result.PostedMembers == nil || len(result.PostedMembers) != 0 || result.FailedMember != "" {
			t.Fatalf("validation result %+v: %v", result, err)
		}
		legacy := bus.PostGroup(tc.kind, tc.sender, tc.id, tc.text, "", tc.hops, time.Time{})
		if legacy == nil || legacy.Error() != err.Error() {
			t.Fatalf("legacy validation changed: %v vs %v", legacy, err)
		}
	}
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < RatePerMinute; i++ {
		result, err := bus.PostGroupWithResult(link.KindBot, "ada", g.ID, "within original rate", "", 1, now)
		if err != nil || result.Stage != "complete" {
			t.Fatalf("post %d %+v: %v", i, result, err)
		}
	}
	result, err := bus.PostGroupWithResult(link.KindBot, "ada", g.ID, "over original rate", "", 1, now)
	if !errors.Is(err, ErrRateLimit) || result.Stage != "rate" || result.Appended || result.MessageID != "" || result.PostedMembers == nil || len(result.PostedMembers) != 0 {
		t.Fatalf("rate result %+v: %v", result, err)
	}
	if err := bus.PostGroup(link.KindBot, "ada", g.ID, "legacy rate", "", 1, now); !errors.Is(err, ErrRateLimit) {
		t.Fatalf("legacy adapter lost rate cause: %v", err)
	}
	if lines, err := bus.Transcript(g.ID); err != nil || len(lines) != RatePerMinute {
		t.Fatalf("rejected posts changed transcript: %d %v", len(lines), err)
	}
}

func TestChatGroupRuntimeLegacyPersonAdapter(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	bus := &Bus{Root: root}
	g, err := bus.CreateGroupForOwner("u1", "owner room", []string{"ada", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.PostGroup(link.KindPerson, "u2", g.ID, "foreign owner", "", 1, time.Time{}); err == nil || err.Error() != "peer: group is not yours" {
		t.Fatalf("foreign owner accepted: %v", err)
	}
	if err := bus.PostGroup(link.KindPerson, "u1", g.ID, "  owner message  ", "", 1, time.Time{}); err != nil {
		t.Fatal(err)
	}
	lines, err := bus.Transcript(g.ID)
	if err != nil || len(lines) != 1 || lines[0].Kind != link.KindPerson || lines[0].Sender != "u1" || lines[0].Text != "owner message" {
		t.Fatalf("legacy person transcript %+v: %v", lines, err)
	}
	var chain string
	for _, target := range []*bot.Bot{ada, bob} {
		posts := readPosts(t, target.InboxPath())
		if len(posts) != 1 || posts[0].SenderKind != link.KindPerson || posts[0].Sender != "u1" || posts[0].Hops != 1 || !strings.HasPrefix(posts[0].Chain, "ch_") {
			t.Fatalf("person inbox %+v", posts)
		}
		if chain != "" && posts[0].Chain != chain {
			t.Fatal("person fanout changed chain between members")
		}
		chain = posts[0].Chain
	}
}
