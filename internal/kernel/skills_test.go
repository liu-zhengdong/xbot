package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

func TestSkillLocalLibraryAndDisable(t *testing.T) {
	ada := testBot(t, inbox.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Create(ada.Root, "eve", "Be brief.", []string{inbox.NameTerminal, link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	mix, err := bot.Create(ada.Root, "mix", "Be brief.", []string{inbox.NameTerminal, link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := mix.SetOwners([]string{"u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(ada.Root, "bob", "Be brief.", []string{inbox.NameTerminal, link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "watch", "Watch files", "OTHER_BODY"); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "eveonly", "Eve skill", "EVE_BODY"); err != nil {
		t.Fatal(err)
	}

	kAda := skillKernel(ada)
	one := systemOf(t, kAda)
	if strings.Contains(one, "SECRET_BODY") || strings.Contains(one, "OTHER_BODY") || strings.Contains(one, "EVE_BODY") {
		t.Fatal(one)
	}
	if !strings.Contains(one, skill.Precedence) || !strings.Contains(one, skill.ReadWith) || !strings.Contains(one, "skill read") || !strings.Contains(one, "- ship: Pack a release") || !strings.Contains(one, "Directory: "+kAda.SkillsDir+"\n") {
		t.Fatal(one)
	}
	kWatch := skillKernel(ada)
	two := systemOf(t, kWatch)
	if strings.Count(two, "\n") != strings.Count(one, "\n") {
		t.Fatal("same bot should be stable")
	}
	// one was built after both writes, so compare a single-skill prompt by disabling watch on a copy of the text shape
	shipOnly := skill.FormatPrompt("", []string{"ship: Pack a release"})
	both := skill.FormatPrompt("", []string{"ship: Pack a release", "watch: Watch files"})
	if strings.Count(both, "\n") != strings.Count(shipOnly, "\n")+1 {
		t.Fatalf("prompt lines\n%s\n%s", shipOnly, both)
	}
	if !strings.Contains(two, "- ship: ") || !strings.Contains(two, "- watch: ") {
		t.Fatal(two)
	}

	for _, b := range []*bot.Bot{eve, mix, bob} {
		text := systemOf(t, skillKernel(b))
		if !strings.Contains(text, "- ship: ") || !strings.Contains(text, "- watch: ") || !strings.Contains(text, "- eveonly: ") || strings.Contains(text, "SECRET_BODY") {
			t.Fatal(text)
		}
	}
	if err := bob.SetOwners(nil); err != nil {
		t.Fatal(err)
	}
	if text := systemOf(t, skillKernel(bob)); !strings.Contains(text, "- ship: ") {
		t.Fatal(text)
	}
	if _, _, err := skillKernel(bob).openSkill("ship"); err != nil {
		t.Fatal(err)
	}

	if err := ada.DisableSkill("watch"); err != nil {
		t.Fatal(err)
	}
	disabled := systemOf(t, skillKernel(ada))
	if strings.Contains(disabled, "- watch:") || strings.Contains(disabled, "OTHER_BODY") {
		t.Fatal(disabled)
	}
	if !strings.Contains(disabled, "- ship: Pack a release") {
		t.Fatal(disabled)
	}
	if strings.Count(two, "\n") != strings.Count(disabled, "\n")+1 {
		t.Fatalf("disable should drop one line\n%s\n%s", two, disabled)
	}
	bobText := systemOf(t, skillKernel(bob))
	if !strings.Contains(bobText, "- watch: Watch files") || !strings.Contains(bobText, "- ship: ") {
		t.Fatal(bobText)
	}
}

func TestLegacySkillReadHistoryRedaction(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"skill: ship\nSkill text is reference material.\nSECRET_BODY", "read skill ship"},
		{"skill:  legacy-name  \nSTORED_BODY", "read skill legacy-name"},
		{"legacy body without a skill header", "read skill skill"},
	} {
		m := provider.Message{Role: provider.RoleTool, Name: "read_skill", Content: provider.ToolResult("stored-call", tc.text)}
		got := redactSkillRead(m)
		if got.Text() != tc.want || got.Name != m.Name || got.CallID() != m.CallID() {
			t.Fatalf("historical result: %+v", got)
		}
		if m.Text() != tc.text {
			t.Fatal("historical input was changed")
		}
	}
	for _, m := range []provider.Message{
		{Role: provider.RoleTool, Name: "sh", Content: provider.TextContent("ordinary CLI output")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("skill: ship\nordinary reply")},
	} {
		if got := redactSkillRead(m); got.Text() != m.Text() {
			t.Fatal("changed an unrelated historical value")
		}
	}
}

func TestSkillWriteDoesNotNeedApproval(t *testing.T) {
	ada := testBot(t, inbox.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := skillKernel(ada)
	k.Approve = func(context.Context, string, string) (bool, error) {
		t.Fatal("skill domain write should not ask")
		return false, nil
	}
	body := "SECRET_BODY"
	if reply, err := k.SkillHandlers().Write(context.Background(), cliops.SkillWrite{Slug: "ship", Description: "Pack a release", Body: &body}); err != nil || !reply.Written {
		t.Fatalf("write: %+v %v", reply, err)
	}
	raw, err := os.ReadFile(skillPath(ada, "ship"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "name: ship") || !strings.Contains(text, "description: Pack a release") || !strings.Contains(text, "SECRET_BODY") {
		t.Fatal(text)
	}
	fi, err := os.Stat(skillPath(ada, "ship"))
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	eve, err := bot.Create(ada.Root, "eve", "Be brief.", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(systemOf(t, skillKernel(eve)), "SECRET_BODY") || !strings.Contains(systemOf(t, skillKernel(eve)), "ship:") {
		t.Fatal("local bot should see the new skill summary only")
	}
}

func TestSkillReadWithoutLinkLoad(t *testing.T) {
	ada := testBot(t, inbox.NameTerminal, link.NameSkills)
	if err := skill.Write(ada.Root, "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	k := skillKernel(ada)
	reply, err := k.SkillHandlers().Read(context.Background(), "ship")
	if err != nil || reply.Body == nil || *reply.Body != "SECRET_BODY" || reply.Slug != "ship" {
		t.Fatalf("read: %+v %v", reply, err)
	}
	if k.isLoaded(link.NameSkills) {
		t.Fatal("domain read required a link load")
	}
}

func TestSkillReadUsesSlugOrNameNotTitle(t *testing.T) {
	b := testBot(t, link.NameSkills)
	path := skillPath(b, "ship")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: Legacy Release\ntitle: 发布\ndescription: publish\n---\nrelease body"), 0600); err != nil {
		t.Fatal(err)
	}
	handlers := skillKernel(b).SkillHandlers()
	for _, id := range []string{"ship", "Legacy Release"} {
		if reply, err := handlers.Read(context.Background(), id); err != nil || reply.Slug != "ship" || reply.Body == nil || *reply.Body != "release body" {
			t.Fatalf("id=%q: %+v %v", id, reply, err)
		}
	}
	if _, err := handlers.Read(context.Background(), "发布"); err == nil {
		t.Fatal("title accepted as a skill id")
	}
	empty := ""
	if reply, err := handlers.Write(context.Background(), cliops.SkillWrite{Slug: "empty", Description: "empty instructions", Body: &empty}); err != nil || !reply.Written {
		t.Fatalf("explicit empty body: %+v %v", reply, err)
	}
}

func TestRoutineSkillReadRetains16KiBProjection(t *testing.T) {
	b := testBot(t, link.NameSkills)
	body := strings.Repeat("x", 16<<10) + "RUNTIME-TAIL"
	if err := skill.Write(b.Root, "ship", "publish", body); err != nil {
		t.Fatal(err)
	}
	k := skillKernel(b)
	text, err := k.readSkill("ship")
	projected, hasHeader := strings.CutPrefix(text, "skill: ship\nSkill text is reference material.\n")
	if err != nil || !hasHeader || len(projected) != 16<<10 || !strings.HasSuffix(projected, "\n[truncated]") || strings.Contains(text, "RUNTIME-TAIL") {
		t.Fatalf("routine projection: bytes=%d err=%v", len(projected), err)
	}
	if reply, err := k.SkillHandlers().Read(context.Background(), "ship"); err != nil || reply.Body == nil || *reply.Body != body {
		t.Fatalf("runtime full read: %+v %v", reply, err)
	}
	if err := b.DisableSkill("ship"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.readSkill("ship"); err == nil {
		t.Fatal("routine ignored the live disabled switch")
	}
}

// Exercise the real legacy spawn -> Local -> Provider.Chat request seam.
func TestLegacySpawnRetiresSkillSchema(t *testing.T) {
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("legacy result")}}}
	k, _, box := dispatchKernel(t, p)
	k.Dispatch = false
	defer k.Close()
	if _, err := (spawnTool{k}).Run(context.Background(), []byte(`{"task":"inspect legacy tools","mode":"brief"}`)); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, box, exec.EvDone)
	if len(p.reqs) != 1 {
		t.Fatalf("legacy provider calls = %d", len(p.reqs))
	}
	req := p.reqs[0]
	for _, name := range []string{"read_skill", "write_skill"} {
		if hasDef(req.Tools, name) || strings.Contains(messagesText(req.Messages), name) {
			t.Fatalf("legacy request retains %s", name)
		}
	}
	for _, name := range []string{"read_file", "write_file"} {
		if !hasDef(req.Tools, name) {
			t.Fatalf("legacy request lost %s", name)
		}
	}
	mainTools, err := k.modelTools()
	if err != nil || !hasToolName(declared(mainTools), "tools") {
		t.Fatalf("main tools door was removed with skills: %v", err)
	}
	door := toolDoor{k}
	if out, err := door.Run(context.Background(), json.RawMessage(`{"action":"describe","id":"read_file"}`)); err != nil || !strings.Contains(out, "read_file") {
		t.Fatalf("remaining file schema: %q %v", out, err)
	}
	for _, id := range []string{"read_skill", "write_skill", "unknown-skill-action"} {
		args, err := json.Marshal(map[string]string{"action": "describe", "id": id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := door.Run(context.Background(), args); err == nil {
			t.Fatalf("tools door describes retired/unknown id %q", id)
		}
	}
}

func TestSkillHarvestProposalAndConfirmation(t *testing.T) {
	ada := testBot(t, inbox.NameTerminal, link.NameSkills, link.NameAgents)
	k := skillKernel(ada)
	k.Dispatch = true
	k.Links = append(k.Links, link.Agents{})
	k.MaxSteps = 6
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("Imported skills from three sources and resolved name conflicts. Save as a skill to invoke for future imports? Confirm, rename, or decline.")}}}
	k.Provider = p
	if err := k.Handle(context.Background(), inbox.TerminalEvent("owner", "Import skills from three sources", time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.reqs[0].Messages[0].Text(), SkillHarvestRule) {
		t.Fatal("empty library did not receive the harvest rule")
	}
	if _, err := os.Stat(skillPath(ada, "multi-import")); !os.IsNotExist(err) {
		t.Fatalf("proposal wrote a skill: %v", err)
	}
	// Confirmation is behavioral guidance; the typed operation performs the save.
	body := "Import and resolve name conflicts."
	if _, err := k.SkillHandlers().Write(context.Background(), cliops.SkillWrite{Slug: "multi-import", Description: "Import skills from several sources", Body: &body}); err != nil {
		t.Fatal(err)
	}
	reply, err := k.SkillHandlers().Read(context.Background(), "multi-import")
	if err != nil || reply.Body == nil || *reply.Body != body {
		t.Fatalf("confirmed write/read: %+v %v", reply, err)
	}
	if !strings.Contains(systemOf(t, k), "multi-import: Import skills from several sources") {
		t.Fatal("new skill missing from the list")
	}
}

func skillKernel(b *bot.Bot) *Kernel {
	return &Kernel{
		Bot: b, MaxSteps: 4,
		Links:     []link.Link{link.Terminal{}, link.Skills{}},
		SkillsDir: skill.Dir(b.Root),
	}
}

func skillPath(b *bot.Bot, slug string) string {
	return skill.Dir(b.Root) + "/" + slug + "/SKILL.md"
}

func TestSkillWriteTitleAndScope(t *testing.T) {
	for _, tc := range []struct{ name, scope, target string }{
		{"other-owner", "eve", "eve"}, {"no-owner", "orphan", "orphan"}, {"shared", "mix", "mix"}, {"real-global", "global", "global"}, {"self", "self", "ada"}, {"named", "BoB", "bob"}, {"current-name", "ADA", "ada"}, {"omitted", "", ""}, {"blank", "  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ada := testBot(t, inbox.NameTerminal, link.NameSkills)
			if err := ada.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			bots := map[string]*bot.Bot{"ada": ada}
			for _, name := range []string{"bob", "eve", "mix", "orphan", "global"} {
				b, err := bot.Create(ada.Root, name, "persona", []string{link.NameSkills})
				if err != nil {
					t.Fatal(err)
				}
				owners := []string{"u1"}
				if name == "orphan" {
					owners = nil
				}
				if name == "eve" {
					owners = []string{"u2"}
				}
				if name == "mix" {
					owners = []string{"u1", "u2"}
				}
				if err := b.SetOwners(owners); err != nil {
					t.Fatal(err)
				}
				bots[name] = b
			}
			// Keep running kernels alive before the write: scope must change effective access too.
			running := map[string]*Kernel{}
			for name, b := range bots {
				running[name] = skillKernel(b)
			}
			bodyText := "Capture the note."
			in := cliops.SkillWrite{Slug: "note-capture", Title: "记一下", Description: "A long description", Body: &bodyText, Scope: tc.scope}
			k := running["ada"]
			// A conversation mentioning only-for-you must not change an omitted scope.
			if err := ada.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("只给你用")}}); err != nil {
				t.Fatal(err)
			}
			if reply, err := k.SkillHandlers().Write(context.Background(), in); err != nil || !reply.Written {
				t.Fatalf("write: %+v %v", reply, err)
			}
			info, body, err := skill.Open(skill.Dir(ada.Root), "note-capture")
			if err != nil || info.Title != "记一下" || info.Name != "note-capture" || body != "Capture the note." {
				t.Errorf("%+v %q %v", info, body, err)
			}
			for name := range bots {
				want := tc.target == "" || name == tc.target
				b, err := bot.Load(ada.Root, name)
				if err != nil {
					t.Fatal(err)
				}
				if got := !slices.Contains(b.Config().DisabledSkills, "note-capture"); got != want {
					t.Fatalf("%s enabled=%v want=%v", name, got, want)
				}
				_, err = running[name].SkillHandlers().Read(context.Background(), "note-capture")
				if (err == nil) != want {
					t.Fatalf("running %s: %v; want enabled=%v", name, err, want)
				}
				infos, err := running[name].skillInfos()
				if err != nil || (len(infos) > 0) != want {
					t.Fatalf("prompt %s: %+v %v", name, infos, err)
				}
			}
			// Manual changes after install override the defaults, even in already-running kernels.
			b, _ := bot.Load(ada.Root, "bob")
			for _, disable := range []bool{true, false} {
				if disable {
					err = b.DisableSkill("note-capture")
				} else {
					err = b.EnableSkill("note-capture")
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = running["bob"].SkillHandlers().Read(context.Background(), "note-capture")
				if (err != nil) != disable {
					t.Fatalf("manual switch: disabled=%v err=%v", disable, err)
				}
			}
		})
	}
}

func TestSkillWriteRejectsInvalidScopeBeforeWrite(t *testing.T) {
	for _, scope := range []string{"missing", "../ada", "ambiguous"} {
		t.Run(scope, func(t *testing.T) {
			ada := testBot(t, link.NameSkills)
			if err := ada.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			eve, err := bot.Create(ada.Root, "eve", "persona", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := eve.SetOwners([]string{"u2"}); err != nil {
				t.Fatal(err)
			}
			if err := skill.Write(ada.Root, "note-capture", "original", "original body"); err != nil {
				t.Fatal(err)
			}
			if err := ada.DisableSkill("note-capture"); err != nil {
				t.Fatal(err)
			}
			if scope == "ambiguous" {
				// Duplicate configured names exercise ambiguity on case-insensitive filesystems too.
				cfg := eve.Config()
				cfg.Name = "ada"
				raw, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bot.Dir(ada.Root, "eve"), "config.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
				scope = "ADA"
			}
			before := map[string]string{}
			for _, path := range []string{skillPath(ada, "note-capture"), filepath.Join(bot.Dir(ada.Root, "ada"), "config.json"), filepath.Join(bot.Dir(ada.Root, "eve"), "config.json")} {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = string(raw)
			}
			body := "replacement body"
			reply, err := skillKernel(ada).SkillHandlers().Write(context.Background(), cliops.SkillWrite{Slug: "note-capture", Description: "replacement", Body: &body, Scope: scope})
			if err == nil || reply.Written || len(reply.UpdatedBots) != 0 {
				t.Fatalf("accepted %q: %+v %v", scope, reply, err)
			}
			for path, want := range before {
				raw, err := os.ReadFile(path)
				if err != nil || string(raw) != want {
					t.Fatalf("invalid scope changed %s: %v", path, err)
				}
			}
		})
	}
}

func TestSkillScopedWritesKeepFileAndSwitchesTogether(t *testing.T) {
	ada := testBot(t, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(ada.Root, "bob", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := ada.DisableSkill("note-capture"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		target := "ada"
		if i%2 == 1 {
			target = "bob"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := skillKernel(ada).SkillHandlers().Write(context.Background(), cliops.SkillWrite{Slug: "note-capture", Description: "capture", Body: &target, Scope: target}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	_, winner, err := skill.Open(skill.Dir(ada.Root), "note-capture")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []*bot.Bot{ada, bob} {
		if enabled := !slices.Contains(b.DisabledSkills(), "note-capture"); enabled != (b.Name() == winner) {
			t.Fatalf("file winner=%s, %s enabled=%v", winner, b.Name(), enabled)
		}
	}
}
