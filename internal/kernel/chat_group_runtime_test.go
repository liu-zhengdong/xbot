package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/peer"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func chatGroupRuntimeBot(t *testing.T, root, name string, owners ...string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, "agreed duties", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners(owners); err != nil {
		t.Fatal(err)
	}
	return b
}

func chatGroupRuntimeGroup(t *testing.T, bus *peer.Bus, source, name string, members ...string) peer.Group {
	t.Helper()
	g, err := bus.CreateGroup(source, name, members)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func chatGroupRuntimeLines(t *testing.T, root, id string, lines []peer.Line) {
	t.Helper()
	for _, line := range lines {
		if err := fsutil.AppendJSONL(filepath.Join(root, "groups", id, "transcript.jsonl"), line); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChatGroupRuntimeListHistoryFieldsAndSelection(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	a := chatGroupRuntimeBot(t, root, "A", "one", "two")
	chatGroupRuntimeBot(t, root, "B", "one", "two")
	outsider := chatGroupRuntimeBot(t, root, "C", "one")
	bus := &peer.Bus{Root: root, Now: func() time.Time { return time.Unix(100, 123).UTC() }}
	z := chatGroupRuntimeGroup(t, bus, "A", "z-room", "B")
	g := chatGroupRuntimeGroup(t, bus, "A", "a-room", "B")
	chatGroupRuntimeGroup(t, bus, "B", "not-a-member", "C")
	h := (&Kernel{Bot: a}).ChatGroupHandlers()
	listed, err := h.List(ctx)
	if err != nil || listed.Groups == nil || listed.Count != 2 || (*listed.Groups)[0].ID != g.ID || (*listed.Groups)[1].ID != z.ID {
		t.Fatalf("member-only sorted list: %+v %v", listed, err)
	}
	wantGroup := cliops.ChatGroup{ID: g.ID, Name: g.Name, Members: g.Members, Created: g.Created}
	if !reflect.DeepEqual((*listed.Groups)[0], wantGroup) {
		t.Fatalf("full group lost: %+v want %+v", (*listed.Groups)[0], wantGroup)
	}
	lines := make([]peer.Line, 60)
	for i := range lines {
		sender, kind, text := "B", link.KindBot, fmt.Sprintf("body %d", i)
		if i%10 == 0 {
			sender, kind, text = "A", link.KindPerson, fmt.Sprintf("RELEASE %d\nid\tfake row", i)
		}
		lines[i] = peer.Line{ID: fmt.Sprintf("gm_%08d", i), Time: time.Unix(int64(i), 123456789).UTC(), Sender: sender, Kind: kind, Text: text}
	}
	chatGroupRuntimeLines(t, root, g.ID, lines)
	for _, tc := range []struct {
		name    string
		query   cliops.ChatGroupHistory
		indices []int
	}{
		{"default20", cliops.ChatGroupHistory{ID: g.ID}, []int{40, 59}},
		{"max50", cliops.ChatGroupHistory{ID: g.ID, Limit: 500}, []int{10, 59}},
		{"negative-default", cliops.ChatGroupHistory{ID: g.ID, Limit: -1}, []int{40, 59}},
		{"before-exclusive", cliops.ChatGroupHistory{ID: g.ID, BeforeID: lines[30].ID, Limit: 2}, []int{28, 29}},
		{"case-insensitive-keyword", cliops.ChatGroupHistory{ID: g.ID, Sender: "A", Keyword: "release", Limit: 3}, []int{30, 50}},
		{"keyword-not-trimmed", cliops.ChatGroupHistory{ID: g.ID, Keyword: " RELEASE "}, nil},
		{"sender-exact", cliops.ChatGroupHistory{ID: g.ID, Sender: "a"}, nil},
		{"before-first", cliops.ChatGroupHistory{ID: g.ID, BeforeID: lines[0].ID}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := h.History(ctx, tc.query)
			if err != nil || reply.Group == nil || !reflect.DeepEqual(*reply.Group, wantGroup) || reply.Messages == nil || !reply.Untrusted || reply.Count != len(*reply.Messages) {
				t.Fatalf("history metadata: %+v %v", reply, err)
			}
			if tc.indices == nil {
				if reply.Count != 0 {
					t.Fatalf("empty query returned %d", reply.Count)
				}
				encoded, _ := json.Marshal(reply)
				if !strings.Contains(string(encoded), `"messages":[]`) {
					t.Fatalf("empty messages not []: %s", encoded)
				}
				return
			}
			messages := *reply.Messages
			if messages[0].ID != lines[tc.indices[0]].ID || messages[len(messages)-1].ID != lines[tc.indices[1]].ID {
				t.Fatalf("selected range: %+v", messages)
			}
			for _, message := range messages {
				var original peer.Line
				for _, line := range lines {
					if line.ID == message.ID {
						original = line
						break
					}
				}
				want := cliops.ChatGroupLine{ID: original.ID, Time: original.Time, Sender: original.Sender, Kind: original.Kind, Text: original.Text}
				if !reflect.DeepEqual(message, want) {
					t.Fatalf("line fields lost: %+v want %+v", message, want)
				}
			}
			if strings.Count(cliops.FormatChatGroupHistory(reply), "\n") != reply.Count+3 {
				t.Fatal("quoted history text forged extra human rows")
			}
		})
	}
	bad, err := h.History(ctx, cliops.ChatGroupHistory{ID: g.ID, BeforeID: "not-present"})
	if err == nil || bad.Group != nil || bad.Messages != nil || !strings.Contains(bad.Error, "no message") {
		t.Fatalf("unknown before: %+v %v", bad, err)
	}
	bad, err = (&Kernel{Bot: outsider}).ChatGroupHandlers().History(ctx, cliops.ChatGroupHistory{ID: g.ID})
	if err == nil || bad.Group != nil || bad.Messages != nil || !strings.Contains(bad.Error, "not a member") {
		t.Fatalf("owner is not membership: %+v %v", bad, err)
	}
	empty := chatGroupRuntimeBot(t, t.TempDir(), "empty")
	list, err := (&Kernel{Bot: empty}).ChatGroupHandlers().List(ctx)
	raw, _ := json.Marshal(list)
	if err != nil || list.Groups == nil || list.Count != 0 || !strings.Contains(string(raw), `"groups":[]`) {
		t.Fatalf("empty groups: %s %v", raw, err)
	}
}

func TestChatGroupRuntimeCreateUsesNativeMemberScopeAndBounds(t *testing.T) {
	root, ctx := t.TempDir(), context.Background()
	a := chatGroupRuntimeBot(t, root, "A", "one", "two", "")
	chatGroupRuntimeBot(t, root, "B", "two", "three")
	chatGroupRuntimeBot(t, root, "C", "one")
	chatGroupRuntimeBot(t, root, "foreign", "three")
	chatGroupRuntimeBot(t, root, "orphan")
	chatGroupRuntimeBot(t, root, "empty-owner", "")
	if err := bot.SetPrimary(root, "C"); err != nil {
		t.Fatal(err)
	}
	// A malformed source Inbox must not affect ordinary reads or Create, with
	// either a direct source or a resource whose original Inbox is unopened.
	if err := os.WriteFile(a.InboxPath(), []byte("invalid inbox JSON\n"), 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: a}
	for _, resources := range []bool{false, true} {
		if resources {
			k.Resources = &BotResources{current: a, ctx: ctx}
		}
		h := k.ChatGroupHandlers()
		created, err := h.Create(ctx, cliops.ChatGroupCreate{Name: "  room  ", Members: []string{"B", "A", "B"}})
		if err != nil || !created.Created || created.Group == nil || created.Group.Name != "room" || !reflect.DeepEqual(created.Group.Members, []string{"A", "B"}) {
			t.Fatalf("auto-self/dedupe/intersection/nonprimary: %+v %v", created, err)
		}
		if _, err := h.List(ctx); err != nil {
			t.Fatal(err)
		}
		if history, err := h.History(ctx, cliops.ChatGroupHistory{ID: created.Group.ID}); err != nil || history.Messages == nil || history.Count != 0 {
			t.Fatalf("ordinary history: %+v %v", history, err)
		}
		if post, err := h.Post(ctx, cliops.ChatGroupPost{ID: created.Group.ID, Text: "requires live source call"}); err == nil || post.Appended || post.PostedMembers == nil || len(post.PostedMembers) != 0 {
			t.Fatalf("only Post requires original ctx: %+v %v", post, err)
		}
	}
	h := k.ChatGroupHandlers()
	for _, members := range [][]string{{"A", "A"}, {"foreign"}, {"orphan"}, {"empty-owner"}, {"B@device"}, {"missing"}} {
		reply, err := h.Create(ctx, cliops.ChatGroupCreate{Name: "room", Members: members})
		if err == nil || reply.Created || reply.Group != nil {
			t.Fatalf("invalid members %q: %+v %v", members, reply, err)
		}
	}
	for _, name := range []string{"", " \t\n", "a/b", "a\\b", "a\nb", strings.Repeat("界", 17)} {
		reply, err := h.Create(ctx, cliops.ChatGroupCreate{Name: name, Members: []string{"B"}})
		if err == nil || reply.Created {
			t.Fatalf("native name bound %q: %+v %v", name, reply, err)
		}
	}
	for i := 1; i <= 7; i++ {
		chatGroupRuntimeBot(t, root, fmt.Sprintf("peer%d", i), "two")
	}
	members := []string{"B"}
	for i := 1; i <= 6; i++ {
		members = append(members, fmt.Sprintf("peer%d", i))
	}
	created, err := h.Create(ctx, cliops.ChatGroupCreate{Name: "  " + strings.Repeat("界", 16) + "  ", Members: members})
	if err != nil || !created.Created || len(created.Group.Members) != 8 || len(created.Group.Name) != 48 {
		t.Fatalf("native 8 members/48-byte trimmed name: %+v %v", created, err)
	}
	members = append(members, "peer7")
	if reply, err := h.Create(ctx, cliops.ChatGroupCreate{Name: "too-many", Members: members}); err == nil || reply.Created {
		t.Fatalf("native member max: %+v %v", reply, err)
	}
}

func TestChatGroupRuntimeArtifactCurrentRenameAndRedaction(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	a := chatGroupRuntimeBot(t, root, "A", "shared")
	chatGroupRuntimeBot(t, root, "B", "shared")
	k := botControlKernel(t, a)
	k.Redact = func(s string) string { return strings.ReplaceAll(s, "SOURCE_SECRET", "[source]") }
	h, resources := k.ChatGroupHandlers(), k.Resources
	g := chatGroupRuntimeGroup(t, &peer.Bus{Root: root}, "A", "SOURCE_SECRET", "B")
	text := strings.Repeat("complete<text>\n", 2800) + "SOURCE_SECRET TAIL_SENTINEL"
	chatGroupRuntimeLines(t, root, g.ID, []peer.Line{{ID: "gm_full", Time: time.Unix(1, 321).UTC(), Sender: "SOURCE_SECRET", Kind: link.KindBot, Text: text}})
	if err := resources.RenameTo("renamed"); err != nil {
		t.Fatal(err)
	}
	if err := peer.RenameMember(root, "A", "renamed"); err != nil {
		t.Fatal(err)
	}
	// Captured handlers retain their resource/redactor even if a later binding
	// replaces the fields on the Kernel that originally published them.
	k.Resources, k.Redact = &BotResources{}, func(string) string { return "wrong-redactor" }
	reply, err := h.History(ctx, cliops.ChatGroupHistory{ID: g.ID})
	if err != nil || reply.Messages != nil || reply.Count != 1 || reply.Group == nil || reply.Access != "local" || reply.File == "" || reply.Group.Name != "[source]" {
		t.Fatalf("artifact/current source: %+v %v", reply, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(root, "renamed"), filepath.FromSlash(reply.File)))
	if err != nil || int64(len(raw)) != reply.Bytes || len(raw) == 0 || raw[len(raw)-1] != '\n' || strings.Contains(string(raw), "SOURCE_SECRET") {
		t.Fatalf("full redacted artifact bytes=%d reply=%+v err=%v", len(raw), reply, err)
	}
	var full cliops.ChatGroupHistoryReply
	if err := json.Unmarshal(raw, &full); err != nil || full.Messages == nil || len(*full.Messages) != 1 || (*full.Messages)[0].Text != strings.ReplaceAll(text, "SOURCE_SECRET", "[source]") || (*full.Messages)[0].Sender != "[source]" {
		t.Fatalf("complete artifact lost tail/fields: %+v %v", full, err)
	}
	listed, err := h.List(ctx)
	if err != nil || listed.Count != 1 || !reflect.DeepEqual((*listed.Groups)[0].Members, []string{"renamed", "B"}) {
		t.Fatalf("current renamed membership: %+v %v", listed, err)
	}
	created, err := h.Create(ctx, cliops.ChatGroupCreate{Name: "after rename", Members: []string{"B"}})
	if err != nil || !created.Created || !reflect.DeepEqual(created.Group.Members, []string{"B", "renamed"}) {
		t.Fatalf("current renamed create: %+v %v", created, err)
	}
	if _, err := os.Stat(bot.Dir(root, "A")); !os.IsNotExist(err) {
		t.Fatalf("old source directory revived: %v", err)
	}
}

func TestChatGroupRuntimeHumanBudgetBothReadCallers(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	a := chatGroupRuntimeBot(t, root, "A", "shared")
	chatGroupRuntimeBot(t, root, "B", "shared")
	g := chatGroupRuntimeGroup(t, &peer.Bus{Root: root}, "A", "room", "B")
	lines := []peer.Line{{ID: "gm_1", Time: time.Unix(1, 0).UTC(), Sender: "B", Kind: link.KindBot, Text: strings.Repeat("\u0085", 4000)}, {ID: "gm_2", Time: time.Unix(2, 0).UTC(), Sender: "B", Kind: link.KindBot, Text: strings.Repeat("\u0085", 4000)}}
	chatGroupRuntimeLines(t, root, g.ID, lines)
	h := (&Kernel{Bot: a}).ChatGroupHandlers()
	reply, err := h.History(ctx, cliops.ChatGroupHistory{ID: g.ID})
	if err != nil || reply.File == "" || reply.Messages != nil || reply.Count != 2 {
		t.Fatalf("human-only history overflow: %+v %v", reply, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(root, "A"), filepath.FromSlash(reply.File)))
	if err != nil {
		t.Fatal(err)
	}
	var history cliops.ChatGroupHistoryReply
	if err := json.Unmarshal(raw, &history); err != nil || len(raw) > tools.DefaultShellOutputBytes || len(cliops.FormatChatGroupHistory(history)) <= tools.DefaultShellOutputBytes || history.Messages == nil || !reflect.DeepEqual((*history.Messages)[1].Text, lines[1].Text) {
		t.Fatalf("fixture/history projection: json=%d human=%d err=%v", len(raw), len(cliops.FormatChatGroupHistory(history)), err)
	}
	// Valid 48-byte group names have a larger quoted human representation.
	groups := []cliops.ChatGroup{}
	for i := 0; ; i++ {
		group := peer.Group{ID: fmt.Sprintf("g_%08x", i), Name: strings.Repeat("\u0085", 24), Members: []string{"A", "B"}, Created: time.Unix(1, 0).UTC()}
		runtimeBotJSON(t, filepath.Join(root, "groups", group.ID, "meta.json"), group)
		groups = append(groups, cliops.ChatGroup{ID: group.ID, Name: group.Name, Members: group.Members, Created: group.Created})
		fixture := cliops.ChatGroupListReply{Groups: &groups, Count: len(groups)}
		if len(cliops.FormatChatGroupList(fixture)) > tools.DefaultShellOutputBytes {
			break
		}
		if i > 1000 {
			t.Fatal("human overflow fixture did not reach budget")
		}
	}
	list, err := h.List(ctx)
	if err != nil || list.Groups != nil || list.File == "" || list.Count != len(groups)+1 {
		t.Fatalf("human-only list overflow: %+v %v", list, err)
	}
	raw, err = os.ReadFile(filepath.Join(work.BotDir(root, "A"), filepath.FromSlash(list.File)))
	if err != nil {
		t.Fatal(err)
	}
	var full cliops.ChatGroupListReply
	if err := json.Unmarshal(raw, &full); err != nil || len(raw) > tools.DefaultShellOutputBytes || full.Groups == nil || full.Count != list.Count || len(cliops.FormatChatGroupList(full)) <= tools.DefaultShellOutputBytes {
		t.Fatalf("full list artifact json=%d human=%d count=%d err=%v", len(raw), len(cliops.FormatChatGroupList(full)), full.Count, err)
	}
	for _, value := range []struct {
		reply any
		human string
	}{{reply, cliops.FormatChatGroupHistory(reply)}, {list, cliops.FormatChatGroupList(list)}} {
		encoded, _ := json.Marshal(value.reply)
		if len(encoded)+1 > tools.DefaultShellOutputBytes || len(value.human) > tools.DefaultShellOutputBytes {
			t.Fatal("artifact reference exceeds either output budget")
		}
	}
}
