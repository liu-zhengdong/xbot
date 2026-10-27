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
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

func TestSkillLocalLibraryAndDisable(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Create(ada.Root, "eve", "Be brief.", []string{link.NameTerminal, link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	mix, err := bot.Create(ada.Root, "mix", "Be brief.", []string{link.NameTerminal, link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := mix.SetOwners([]string{"u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(ada.Root, "bob", "Be brief.", []string{link.NameTerminal, link.NameSkills})
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
	if !strings.Contains(one, skill.Precedence) || !strings.Contains(one, skill.ReadWith) || !strings.Contains(one, "read_skill") || !strings.Contains(one, "- ship: Pack a release") || !strings.Contains(one, "Directory: "+kAda.SkillsDir+"\n") {
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

func TestSkillReadDoesNotPersist(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "read_skill", Arguments: `{"id":"ship"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := skillKernel(ada)
	k.Provider = p
	k.MaxSteps = 4
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "read ship", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 3 {
		t.Fatalf("calls %d", len(p.reqs))
	}
	if strings.Contains(p.reqs[0].Messages[0].Text(), "SECRET_BODY") {
		t.Fatal("body in the system prompt")
	}
	if !strings.Contains(messagesText(p.reqs[2].Messages), "SECRET_BODY") {
		t.Fatal("body missing from the turn that read it")
	}
	if strings.Contains(transcriptText(t, ada), "SECRET_BODY") {
		t.Fatal("body persisted in the transcript")
	}
	if !strings.Contains(transcriptText(t, ada), "read skill ship") {
		t.Fatal(transcriptText(t, ada))
	}
	p2 := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("next")}}}
	k.Provider = p2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", time.Unix(2, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p2.reqs) == 0 || strings.Contains(messagesText(p2.reqs[0].Messages), "SECRET_BODY") {
		t.Fatal("skill body lasted into the next turn")
	}
}

// write_skill is a normal tool: it writes a user skill, not this bot's own
// persona or profile, so the one policy table does not ask.
func TestSkillWriteDoesNotNeedApproval(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	args := `{"slug":"ship","description":"Pack a release","body":"SECRET_BODY"}`
	p := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "write_skill", Arguments: args},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("wrote")},
	}}
	k := skillKernel(ada)
	k.Provider = p
	k.MaxSteps = 4
	k.Approve = func(context.Context, string, string) (bool, error) {
		t.Fatal("write_skill should not ask")
		return false, nil
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "draft ship", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
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

func TestReadSkillWithoutLoad(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "read_skill", Arguments: `{"id":"ship"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := skillKernel(ada)
	k.Provider = p
	k.MaxSteps = 4
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "read ship", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatalf("calls %d", len(p.reqs))
	}
	if !hasDef(p.reqs[0].Tools, "read_skill") {
		t.Fatal("read_skill missing while a skill is listed")
	}
	if hasDef(p.reqs[0].Tools, "write_skill") {
		t.Fatal("write_skill should wait for links load")
	}
	text := messagesText(p.reqs[1].Messages)
	if strings.Contains(text, "unknown tool") || !strings.Contains(text, "SECRET_BODY") {
		t.Fatal(text)
	}
}

func TestDispatchMainLineWritesLoadedSkill(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills, link.NameAgents)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		assistantCall("write", "", "write_skill", `{"slug":"ship","description":"Pack a release","body":"Run the release checks."}`),
		assistantCall("read", "", "read_skill", `{"id":"ship"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("installed")},
	}}
	k := skillKernel(ada)
	k.Provider = p
	k.Dispatch = true
	k.PermissionsOn = func() bool { return true }
	k.Links = append(k.Links, link.Agents{})
	k.MaxSteps = 6
	k.Approve = func(context.Context, string, string) (bool, error) {
		t.Error("writing a skill must use the existing Normal policy")
		return false, nil
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "install ship", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 4 {
		t.Fatalf("model calls = %d, want 4", len(p.reqs))
	}
	if !hasDef(p.reqs[0].Tools, "dispatch") {
		t.Fatal("test did not activate typed dispatch")
	}
	if hasDef(p.reqs[0].Tools, "write_skill") {
		t.Fatal("write_skill must wait for links load")
	}
	for _, name := range []string{"read_skill", "write_skill"} {
		if !hasDef(p.reqs[1].Tools, name) {
			t.Errorf("loaded %s missing from the main line with Dispatch enabled", name)
		}
	}
	info, body, err := skill.Open(skill.Dir(ada.Root), "ship")
	if err != nil || info.Description != "Pack a release" || body != "Run the release checks." {
		t.Fatalf("local library write: info=%+v body=%q err=%v", info, body, err)
	}
	if !strings.Contains(messagesText(p.reqs[3].Messages), "Run the release checks.") {
		t.Fatal("main line could not read back the installed skill")
	}
}

func TestSkillHarvestProposalAndConfirmation(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills, link.NameAgents)
	k := skillKernel(ada)
	k.Dispatch = true
	k.Links = append(k.Links, link.Agents{})
	k.MaxSteps = 6
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("Imported skills from three sources and resolved name conflicts. Save as a skill to invoke for future imports? Confirm, rename, or decline.")}}}
	k.Provider = p
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "Import skills from three sources", time.Unix(1, 0))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.reqs[0].Messages[0].Text(), SkillHarvestRule) {
		t.Fatal("empty library did not receive the harvest rule")
	}
	if _, err := os.Stat(skillPath(ada, "multi-import")); !os.IsNotExist(err) {
		t.Fatalf("proposal wrote a skill: %v", err)
	}
	p = &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		assistantCall("write", "", "write_skill", `{"slug":"multi-import","description":"Import skills from several sources","body":"Import and resolve name conflicts."}`),
		assistantCall("read", "", "read_skill", `{"id":"multi-import"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("Saved.")},
	}}
	k.Provider = p
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "Confirm", time.Unix(2, 0))); err != nil {
		t.Fatal(err)
	}
	if !hasDef(p.reqs[1].Tools, "write_skill") || !strings.Contains(messagesText(p.reqs[3].Messages), "Import and resolve name conflicts.") {
		t.Fatal("loaded skill tools did not write and read the skill")
	}
	if !strings.Contains(systemOf(t, k), "multi-import: Import skills from several sources") {
		t.Fatal("new skill missing from the list")
	}
	if !strings.Contains((writeSkillTool{k}).Description(), "only after the owner confirms") {
		t.Fatal("write_skill description lacks confirmation guidance")
	}
}

func TestSkillDescriptionsNameLocalLibrary(t *testing.T) {
	k := &Kernel{SkillsDir: filepath.Join(t.TempDir(), ".xbot-dev", "skills")}
	for name, text := range map[string]string{
		"read_skill":  (skillTool{k}).Description(),
		"write_skill": (writeSkillTool{k}).Description(),
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(text, k.SkillsDir) || strings.Contains(text, "~/.xbot") {
				t.Errorf("missing runtime library path: %s", text)
			}
			for _, misleading := range []string{"<owner>", "magpie", "approve", "approval"} {
				if strings.Contains(strings.ToLower(text), misleading) {
					t.Errorf("description contradicts local library / Normal policy: %s", text)
				}
			}
		})
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
		{"other-owner", "eve", "eve"}, {"no-owner", "orphan", "orphan"}, {"shared", "mix", "mix"}, {"self", "self", "ada"}, {"named", "BoB", "bob"}, {"current-name", "ADA", "ada"}, {"omitted", "", ""}, {"blank", "  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ada := testBot(t, link.NameTerminal, link.NameSkills)
			if err := ada.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			bots := map[string]*bot.Bot{"ada": ada}
			for _, name := range []string{"bob", "eve", "mix", "orphan"} {
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
			args := map[string]string{"slug": "note-capture", "title": "记一下", "description": "A long description", "body": "Capture the note."}
			if tc.name != "omitted" {
				args["scope"] = tc.scope
			}
			raw, _ := json.Marshal(args)
			p := &scripted{resps: []provider.Message{linksCall("load", "skills"), assistantCall("write", "", "write_skill", string(raw)), {Role: provider.RoleAssistant, Content: provider.TextContent("installed")}}}
			k := running["ada"]
			k.Provider = p
			// Deliberately mentions only-for-you even when scope is omitted: the server must not infer it.
			if err := k.Handle(context.Background(), link.TerminalEvent("u1", "只给你用", time.Unix(1, 0))); err != nil {
				t.Fatal(err)
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
				_, _, err = running[name].openSkill("note-capture")
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
				_, _, err = running["bob"].openSkill("note-capture")
				if (err != nil) != disable {
					t.Fatalf("manual switch: disabled=%v err=%v", disable, err)
				}
			}
		})
	}
}

func TestSkillWriteRejectsInvalidScopeBeforeWrite(t *testing.T) {
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
	for _, scope := range []string{"missing", "../ada"} {
		raw, _ := json.Marshal(map[string]string{"slug": "note-capture", "description": "description", "body": "body", "scope": scope})
		if _, err := (writeSkillTool{k: skillKernel(ada)}).Run(context.Background(), raw); err == nil {
			t.Fatalf("accepted %q", scope)
		}
		if _, err := os.Stat(skillPath(ada, "note-capture")); !os.IsNotExist(err) {
			t.Fatalf("invalid scope wrote file: %v", err)
		}
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
			raw, _ := json.Marshal(map[string]string{"slug": "note-capture", "description": "capture", "body": target, "scope": target})
			if _, err := (writeSkillTool{k: skillKernel(ada)}).Run(context.Background(), raw); err != nil {
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
