package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/notify"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/work"
)

func TestEmptyProfileLeavesPrefix(t *testing.T) {
	b := testBot(t)
	file, err := b.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	composed, err := b.ComposedPersona()
	if err != nil || composed != file {
		t.Fatalf("composed %q file %q", composed, file)
	}
	msgs, _ := Assemble(Input{Persona: composed, Rules: "Stay useful.", Links: []string{link.NameTerminal}})
	if strings.Contains(msgs[0].Text(), "About:") || strings.Contains(msgs[0].Text(), "Proactivity:") {
		t.Fatal(msgs[0].Text())
	}
	desc := "项目笔记"
	if err := b.ApplyProfile(bot.ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	composed, err = b.ComposedPersona()
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ = Assemble(Input{Persona: composed, Rules: "Stay useful.", Links: []string{link.NameTerminal}})
	if !strings.Contains(msgs[0].Text(), "About: 项目笔记") || !strings.HasPrefix(msgs[0].Text(), "### 人设\n这段定义你是谁、口吻和关系分寸；下面是正文，照着演，不要复述本说明。\n"+file) {
		t.Fatal(msgs[0].Text())
	}
}

func TestProfileEditSurfacesAsEnvUpdate(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := t0
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("one")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("two")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return now }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", t0)); err != nil {
		t.Fatal(err)
	}
	desc := "项目笔记"
	if err := b.ApplyProfile(bot.ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(5 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.reqs[1].Messages[0].Text(), "About: 项目笔记") {
		t.Fatal(p.reqs[1].Messages[0].Text())
	}
	if !strings.Contains(p.reqs[1].Messages[0].Text(), "Be brief.") {
		t.Fatal("persona missing")
	}
	updates := envUpdates(t, b)
	last := updates[len(updates)-1]
	if !strings.Contains(last, `"profile":`) || strings.Contains(last, "persona: updated") || strings.Contains(last, "Be brief") || strings.Contains(last, "项目笔记") {
		t.Fatalf("profile diff: %s", last)
	}
}

func TestUpdateProfileNeedsConfirmationPermissionsOn(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NamePersona)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := t0
	call := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c1", Type: "function",
		Function: provider.FunctionCall{Name: "update_profile", Arguments: `{"description":"中文笔记"}`},
	}})}
	// A denied call cannot be retried with the same arguments. The later
	// approval uses a different update.
	call2 := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c2", Type: "function",
		Function: provider.FunctionCall{Name: "update_profile", Arguments: `{"description":"中文笔记","timezone":"UTC"}`},
	}})}
	p := &scripted{resps: []provider.Message{
		call,
		{Role: provider.RoleAssistant, Content: provider.TextContent("denied-done")},
		call2,
		{Role: provider.RoleAssistant, Content: provider.TextContent("saved")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("next")},
	}}
	approved := false
	k := &Kernel{
		Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 4,
		Now: func() time.Time { return now },
		Approve: func(_ context.Context, name, summary string) (bool, error) {
			if name != "update_profile" || !strings.Contains(summary, "中文") {
				t.Fatalf("confirm %s %s", name, summary)
			}
			return approved, nil
		},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "change it", t0)); err != nil {
		t.Fatal(err)
	}
	if b.Config().Description != "" || b.Config().Proactivity != "" {
		t.Fatalf("denied profile applied: %+v", b.Config())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !transcriptHas(entries, "denied by user") {
		t.Fatal("missing denial")
	}
	approved = true
	now = t0.Add(5 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "change it again", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if b.Config().Description != "中文笔记" || b.Config().Timezone != "UTC" || b.Config().Proactivity != "" {
		t.Fatalf("profile %+v", b.Config())
	}
	now = t0.Add(10 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "third", t0.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	prefix := p.reqs[len(p.reqs)-1].Messages[0].Text()
	if !strings.Contains(prefix, "About: 中文笔记") || strings.Contains(prefix, "Proactivity:") {
		t.Fatal(prefix)
	}
	updates := envUpdates(t, b)
	last := strings.Join(updates, "\n")
	if !strings.Contains(last, `"profile":`) || strings.Contains(last, "中文") {
		t.Fatalf("env %s", last)
	}
}

func TestUpdateProfileAvatarConfirmationPermissionsOn(t *testing.T) {
	testUpdateProfileAvatar(t, false)
}

func TestUpdateProfileAvatarDirectPermissive(t *testing.T) {
	testUpdateProfileAvatar(t, true)
}

func testUpdateProfileAvatar(t *testing.T, permissive bool) {
	for _, tc := range []struct {
		name, args, description, renamed string
		allow                            bool
	}{
		{name: "deny", args: `{"description":"笔记助手","avatar_seed":"Ada#2"}`},
		{name: "no approver", args: `{"avatar_seed":"Ada#2"}`, allow: true},
		{name: "avatar only", args: `{"avatar_seed":"Ada#2"}`, allow: true},
		{name: "with description", args: `{"description":"笔记助手","avatar_seed":"Ada#2"}`, description: "笔记助手", allow: true},
		{name: "all fields", args: `{"name":"renamed","description":"笔记助手","timezone":"UTC","avatar_seed":" Ada#2 "}`, description: "笔记助手", renamed: "renamed", allow: true},
	} {
		if (permissive && !tc.allow) || (!permissive && tc.name == "no approver") {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t, link.NameTerminal, link.NamePersona)
			png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 7}
			if err := b.SetAvatar(png, "image/png"); err != nil {
				t.Fatal(err)
			}
			before := profileAvatarFiles(t, b)
			other, err := bot.Create(b.Root, "other", "Other bot.", nil)
			if err != nil {
				t.Fatal(err)
			}
			otherBefore := profileAvatarFiles(t, other)
			call := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
				ID: "avatar", Type: "function", Function: provider.FunctionCall{Name: "update_profile", Arguments: tc.args},
			}})}
			p := &scripted{resps: []provider.Message{
				call, {Role: provider.RoleAssistant, Content: provider.TextContent("done")},
				call, {Role: provider.RoleAssistant, Content: provider.TextContent("done again")},
			}}
			confirms := 0
			k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 4, Permissive: permissive}
			k.Approve = func(_ context.Context, name, summary string) (bool, error) {
				confirms++
				if name != "update_profile" || !strings.Contains(summary, `avatar_seed "Ada#2"`) {
					t.Errorf("confirmation %s %s", name, summary)
				}
				if tc.description != "" && !strings.Contains(summary, `description "笔记助手"`) {
					t.Errorf("description missing from shared card: %s", summary)
				}
				if title, _ := review.CardText(name, json.RawMessage(tc.args), summary, "local"); title != "修改档案" {
					t.Errorf("card title %q", title)
				}
				if confirms == 1 && !reflect.DeepEqual(profileAvatarFiles(t, k.Bot), before) {
					t.Error("profile or upload changed before confirmation")
				}
				return tc.allow, nil
			}
			if tc.name == "no approver" {
				k.Approve = nil
			}
			started := time.Now()
			if err := k.Handle(context.Background(), link.TerminalEvent("u1", "给自己换个头像", started)); err != nil {
				t.Fatal(err)
			}
			t.Logf("scripted avatar turn: %s", time.Since(started))
			if !hasDef(p.reqs[0].Tools, "update_profile") {
				t.Fatal("update_profile missing from provider tool list")
			}
			wantConfirms := 1
			if permissive {
				wantConfirms = 0
			}
			if confirms != wantConfirms {
				t.Fatalf("confirmation count %d, want %d", confirms, wantConfirms)
			}
			if tc.allow {
				loaded, err := bot.Load(b.Root, k.Bot.Name())
				if err != nil {
					t.Fatal(err)
				}
				for _, current := range []*bot.Bot{k.Bot, loaded} {
					if av, seed := current.AvatarState(); av != bot.AvatarGenerated || seed != "Ada#2" {
						t.Fatalf("avatar state %q %q", av, seed)
					}
					if current.Config().Description != tc.description {
						t.Fatalf("description %q", current.Config().Description)
					}
				}
				if tc.renamed != "" && (loaded.Name() != tc.renamed || loaded.Config().Timezone != "UTC") {
					t.Fatalf("combined rename/timezone not applied: %+v", loaded.Config())
				}
				after := profileAvatarFiles(t, loaded)
				if _, ok := after["avatar"]; ok {
					t.Fatal("upload was not removed")
				}
				if _, ok := after["avatar.mime"]; ok {
					t.Fatal("upload MIME was not removed")
				}
				// Repeating the same update preserves stored state.
				if err := k.Handle(context.Background(), link.TerminalEvent("u1", "再用同一头像", time.Now())); err != nil {
					t.Fatal(err)
				}
				if confirms != 2*wantConfirms || !reflect.DeepEqual(profileAvatarFiles(t, k.Bot), after) {
					t.Fatal("repeated update changed stored profile or unexpected confirmation count")
				}
			} else {
				if !reflect.DeepEqual(profileAvatarFiles(t, b), before) {
					t.Fatal("denied update changed profile or upload")
				}
				if av, _ := b.AvatarState(); av != bot.AvatarCustom {
					t.Fatal("denied update changed in-memory avatar")
				}
				entries, err := b.Entries()
				if err != nil || !transcriptHas(entries, "denied by user") {
					t.Fatalf("missing denial: %v", err)
				}
			}
			if !reflect.DeepEqual(profileAvatarFiles(t, other), otherBefore) {
				t.Fatal("another bot was changed")
			}
		})
	}
}

func TestUpdateProfileAvatarRejectsInvalid(t *testing.T) {
	testUpdateProfileAvatarRejectsInvalid(t, false)
}

func TestUpdateProfileAvatarRejectsInvalidPermissive(t *testing.T) {
	testUpdateProfileAvatarRejectsInvalid(t, true)
}

func testUpdateProfileAvatarRejectsInvalid(t *testing.T, permissive bool) {
	cases := []struct{ name, args, want string }{
		{"number", `{"avatar_seed":3}`, "must be a string"},
		{"boolean", `{"avatar_seed":true}`, "must be a string"},
		{"array", `{"avatar_seed":[]}`, "must be a string"},
		{"object", `{"avatar_seed":{}}`, "must be a string"},
		{"null", `{"avatar_seed":null}`, "invalid avatar_seed"},
		{"cross bot target", `{"avatar_seed":"Ada#2","target":"other"}`, "unknown field"},
		{"cross bot name", `{"avatar_seed":"Ada#2","bot":"other"}`, "unknown field"},
		{"upload", `{"avatar":"custom"}`, "unknown field"},
	}
	for _, seed := range []string{"", " ", "<x>", "a\"b", "a'b", "a&b", "a`b", "a\nb", "a\x00b", strings.Repeat("a", 81), strings.Repeat("周", 81)} {
		raw, err := json.Marshal(map[string]string{"avatar_seed": seed, "description": "must not apply"})
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct{ name, args, want string }{seed, string(raw), "invalid avatar_seed"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t, link.NameTerminal, link.NamePersona)
			if err := b.SetAvatar([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, "image/png"); err != nil {
				t.Fatal(err)
			}
			before := profileAvatarFiles(t, b)
			p := &scripted{resps: []provider.Message{
				{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
					ID: "bad-avatar", Type: "function", Function: provider.FunctionCall{Name: "update_profile", Arguments: tc.args},
				}})},
				{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
			}}
			confirms := 0
			k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 4, Permissive: permissive,
				Approve: func(context.Context, string, string) (bool, error) { confirms++; return true, nil },
			}
			if err := k.Handle(context.Background(), link.TerminalEvent("u1", "change avatar", time.Now())); err != nil {
				t.Fatal(err)
			}
			entries, err := b.Entries()
			if err != nil || !transcriptHas(entries, tc.want) {
				t.Fatalf("missing tool error %q: %v", tc.want, err)
			}
			if _, err := (profileTool{k}).Run(context.Background(), json.RawMessage(tc.args)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run must also validate: %v", err)
			}
			if confirms != 0 || !reflect.DeepEqual(profileAvatarFiles(t, b), before) {
				t.Fatal("invalid update asked for confirmation or changed profile/upload")
			}
		})
	}
}

func TestUpdateProfileAvatarSeedBoundary(t *testing.T) {
	for _, seed := range []string{"XBot#3", " 周报#2 ", strings.Repeat("周", 80)} {
		raw, _ := json.Marshal(map[string]string{"avatar_seed": seed})
		need, summary, err := (reviewedTool{Tool: profileTool{}}).Confirm(raw)
		if err != nil || !need || !strings.Contains(summary, `avatar_seed "`+strings.TrimSpace(seed)+`"`) {
			t.Fatalf("valid seed %q: need=%v summary=%s err=%v", seed, need, summary, err)
		}
	}
}

func TestUpdateProfileAvatarSchema(t *testing.T) {
	raw, err := json.Marshal((profileTool{}).Parameters())
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct{ Type string }
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties) != 4 || schema.Properties["avatar_seed"].Type != "string" {
		t.Fatalf("expected four profile fields including string avatar_seed: %s", raw)
	}
	if review.ClassOf("update_profile") != review.SelfEdit {
		t.Fatal("profile avatar must remain SelfEdit")
	}
}

// Snapshot only the profile and uploaded image, excluding transcript/turn state.
func profileAvatarFiles(t *testing.T, b *bot.Bot) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"config.json", "avatar", "avatar.mime"} {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(b.AvatarPath()), name))
		if errors.Is(err, os.ErrNotExist) && name != "config.json" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	return files
}

// Quiet hours are gone: a note goes out at any hour, and notes an old
// quiet window left in held.json are delivered one by one, once.
func TestNotifyAlwaysDeliversAndFlushesLegacyHeld(t *testing.T) {
	b := testBot(t)
	var desks []string
	now := time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC)
	k := &Kernel{Bot: b, Now: func() time.Time { return now }, turn: &turn{turnSource: link.NameTimer}, Desktop: func(_ context.Context, _, body string) error {
		desks = append(desks, body)
		return nil
	}}
	out, err := (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"late","importance":"low"}`))
	if err != nil || out != "notified the owner" {
		t.Fatalf("night note %s %v", out, err)
	}
	if len(desks) != 1 || desks[0] != "late" {
		t.Fatalf("desks %v", desks)
	}
	if _, err := os.Stat(b.LegacyHeldPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("held.json written: %v", err)
	}

	if err := os.WriteFile(b.LegacyHeldPath(), []byte(`{"notes":["kept one","kept two"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	n, err := FlushLegacyHeld(context.Background(), b, now, k.Desktop)
	if err != nil || n != 2 {
		t.Fatalf("flush %d %v", n, err)
	}
	if n, err := FlushLegacyHeld(context.Background(), b, now, k.Desktop); err != nil || n != 0 {
		t.Fatalf("second flush %d %v", n, err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 || notes[0].Text != "late" || notes[1].Text != "kept one" || notes[2].Text != "kept two" {
		t.Fatalf("%+v", notes)
	}
	if len(desks) != 3 {
		t.Fatalf("desks %v", desks)
	}
}

func TestRenameUpdatesPrimaryWorkAndGroup(t *testing.T) {
	root := t.TempDir()
	ada, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(root, "bob", "Notes.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(root, "ada"); err != nil {
		t.Fatal(err)
	}
	ada, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetPinned(false); err != nil {
		t.Fatal(err)
	}
	dir := work.BotDir(root, "ada")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	bus := &peer.Bus{Root: root}
	g, err := bus.CreateGroupForOwner("u1", "ship", []string{"ada", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameBot(root, "ada", "ada2"); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Load(root, "ada"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("old name: %v", err)
	}
	next, err := bot.Load(root, "ada2")
	if err != nil {
		t.Fatal(err)
	}
	if next.Name() != "ada2" || next.Config().Pinned {
		t.Fatalf("renamed %+v", next.Config())
	}
	// The avatar seed is the old name and must survive the rename.
	if av, seed := next.AvatarState(); av != bot.AvatarGenerated || seed != "ada" {
		t.Fatalf("rename changed avatar %q %q", av, seed)
	}
	primary, err := bot.Primary(root)
	if err != nil || primary != "ada2" {
		t.Fatalf("primary %s %v", primary, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(root, "ada2"), "note.txt"))
	if err != nil || string(raw) != "keep" {
		t.Fatalf("work %q %v", raw, err)
	}
	if _, err := os.Stat(work.BotDir(root, "ada")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old work dir: %v", err)
	}
	got, err := bus.LoadGroup(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Members, ",")
	if joined != "ada2,bob" {
		t.Fatalf("members %s", joined)
	}
}

func transcriptHas(entries []bot.Entry, text string) bool {
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), text) {
			return true
		}
	}
	return false
}

func TestProfileRejectsRemovedStyleFields(t *testing.T) {
	for _, field := range []string{"tone", "length", "emoji", "language", "quiet_start", "quiet_end"} {
		if _, _, _, err := parseProfileArgs(json.RawMessage(`{"` + field + `":"old"}`)); err == nil {
			t.Fatalf("removed field %s accepted", field)
		}
	}
}
