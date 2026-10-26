package kernel

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestEnvironmentTracePathsReachModelAndReplay(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("one")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("two")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	for _, input := range []string{"where is your trace?", "again"} {
		if err := k.Handle(context.Background(), link.TerminalEvent("owner", input, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	want, err := b.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if want.Trace != b.TracePath() || want.Bot != bot.Dir(b.Root, b.Name()) || !filepath.IsAbs(want.Trace) {
		t.Fatal("paths do not match storage")
	}
	for _, req := range p.reqs {
		env := replayEnvironment(req.Messages, "")
		if got := environmentGet[bot.TracePaths](env, "paths"); got != want {
			t.Fatalf("model paths = %+v, want %+v", got, want)
		}
		if strings.Contains(req.Messages[0].Text(), want.Trace) {
			t.Fatal("paths added to fixed prefix")
		}
	}
	prev := testPublished(t, k)
	next, err := k.sampleEnvironment(prev)
	if err != nil {
		t.Fatal(err)
	}
	delta, _ := decodeEnvironment(environmentDelta(prev, next, "", ""))
	if _, ok := delta.Set["paths"]; ok {
		t.Fatal("unchanged paths repeated")
	}
	snapshot := environmentDelta(environment{}, next, "compressed-range", "")
	replayed := replayEnvironment([]provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(snapshot)}}, "")
	if got := environmentGet[bot.TracePaths](replayed, "paths"); got != want {
		t.Fatal("snapshot lost paths")
	}
}
