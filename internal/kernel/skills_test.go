package kernel

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

func TestSkillOwnerIsolationAndDisable(t *testing.T) {
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
	if err := skill.Write(ada.Root, "u1", "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "u1", "watch", "Watch files", "OTHER_BODY"); err != nil {
		t.Fatal(err)
	}
	if err := skill.Write(ada.Root, "u2", "eveonly", "Eve skill", "EVE_BODY"); err != nil {
		t.Fatal(err)
	}

	kAda := skillKernel(ada)
	one := systemOf(t, kAda)
	if strings.Contains(one, "SECRET_BODY") || strings.Contains(one, "OTHER_BODY") || strings.Contains(one, "EVE_BODY") {
		t.Fatal(one)
	}
	if !strings.Contains(one, skill.Precedence) || !strings.Contains(one, skill.ReadWith) || !strings.Contains(one, "read_skill") || !strings.Contains(one, "- ship: Pack a release") {
		t.Fatal(one)
	}
	kWatch := skillKernel(ada)
	two := systemOf(t, kWatch)
	if strings.Count(two, "\n") != strings.Count(one, "\n") {
		t.Fatal("same bot should be stable")
	}
	// one was built after both writes, so compare a single-skill prompt by disabling watch on a copy of the text shape
	shipOnly := skill.FormatPrompt([]string{"ship: Pack a release"})
	both := skill.FormatPrompt([]string{"ship: Pack a release", "watch: Watch files"})
	if strings.Count(both, "\n") != strings.Count(shipOnly, "\n")+1 {
		t.Fatalf("prompt lines\n%s\n%s", shipOnly, both)
	}
	if !strings.Contains(two, "- ship: ") || !strings.Contains(two, "- watch: ") {
		t.Fatal(two)
	}

	eveText := systemOf(t, skillKernel(eve))
	if strings.Contains(eveText, "ship") || strings.Contains(eveText, "SECRET_BODY") || strings.Contains(eveText, "watch") {
		t.Fatal(eveText)
	}
	if !strings.Contains(eveText, "- eveonly: Eve skill") || strings.Contains(eveText, "EVE_BODY") {
		t.Fatal(eveText)
	}
	mixText := systemOf(t, skillKernel(mix))
	if strings.Contains(mixText, "## Skills") || strings.Contains(mixText, "ship") || strings.Contains(mixText, "eveonly") {
		t.Fatal(mixText)
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
	if err := skill.Write(ada.Root, "u1", "ship", "Pack a release", "SECRET_BODY"); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "read_skill", Arguments: `{"id":"ship"}`},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
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
	if strings.Contains(p.reqs[0].Messages[0].Content, "SECRET_BODY") {
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
	p2 := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "next"}}}
	k.Provider = p2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", time.Unix(2, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p2.reqs) == 0 || strings.Contains(messagesText(p2.reqs[0].Messages), "SECRET_BODY") {
		t.Fatal("skill body lasted into the next turn")
	}
}

func TestSkillWriteRequiresApproval(t *testing.T) {
	ada := testBot(t, link.NameTerminal, link.NameSkills)
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	args := `{"slug":"ship","description":"Pack a release","body":"SECRET_BODY"}`
	p := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "write_skill", Arguments: args},
		}}},
		{Role: provider.RoleAssistant, Content: "stopped"},
	}}
	var tool, summary string
	k := skillKernel(ada)
	k.Provider = p
	k.MaxSteps = 4
	k.Approve = func(_ context.Context, name, text string) (bool, error) {
		if name == "write_skill" {
			tool, summary = name, text
			return false, nil
		}
		return true, nil
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "draft ship", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if tool != "write_skill" || !strings.Contains(summary, "write skill ship") || !strings.Contains(summary, "Pack a release") {
		t.Fatalf("card %s %s", tool, summary)
	}
	if _, err := os.Stat(skillPath(ada, "ship")); err == nil {
		t.Fatal("denied write created a file")
	}
	if !strings.Contains(transcriptText(t, ada), "denied by user") {
		t.Fatal(transcriptText(t, ada))
	}

	p2 := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "write_skill", Arguments: args},
		}}},
		{Role: provider.RoleAssistant, Content: "wrote"},
	}}
	k.Provider = p2
	k.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "draft again", time.Unix(2, 0).UTC())); err != nil {
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
	if strings.Contains(systemOf(t, skillKernel(eve)), "SECRET_BODY") || strings.Contains(systemOf(t, skillKernel(eve)), "ship:") {
		t.Fatal("other owner saw the new skill")
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
	return skill.OwnerDir(b.Root, b.Config().Owners[0]) + "/" + slug + "/SKILL.md"
}
