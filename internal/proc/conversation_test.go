package proc

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/provider"
)

type normalConversationModel struct {
	calls int
	t     *testing.T
}

func (p *normalConversationModel) Chat(_ context.Context, r provider.Request, d func(string)) (provider.Message, error) {
	p.calls++
	if p.calls == 1 {
		return provider.Message{Role: provider.RoleAssistant, Content: "<｜DSML｜invoke name=\"dispatch\">bad</｜DSML｜invoke>"}, nil
	}
	var all strings.Builder
	for _, m := range r.Messages {
		all.WriteString(m.Content + "\n")
	}
	if !strings.Contains(all.String(), "A的完整结果731") || !strings.Contains(all.String(), "继续问限制") {
		p.t.Fatalf("conversation lost input: %s", all.String())
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "结果731，限制仍然保留。"}, nil
}
func TestFailedResultDoesNotBlockNextOwnerMessage(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "brief", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	in, err := inbox.Open(filepath.Join(bot.Dir(root, "ada"), "inbox-test.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	result := link.NewEvent(link.SourceSubagent, exec.EvDone, "A的完整结果731", "sa_a", time.Now())
	owner := link.TerminalEvent("u1", "继续问限制", time.Now())
	for _, e := range []link.Event{result, owner} {
		if err := in.Post(e); err != nil {
			t.Fatal(err)
		}
	}
	p := &normalConversationModel{t: t}
	k := &kernel.Kernel{Bot: b, Inbox: in, Provider: p, MaxSteps: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var events []link.Event
	var errs []error
	err = Run(ctx, Config{K: k, SkipDeliver: true, After: func(e link.Event, err error) {
		events = append(events, e)
		errs = append(errs, err)
		if len(events) == 2 {
			cancel()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID != result.ID || events[1].ID != owner.ID || errs[0] == nil || errs[1] != nil {
		t.Fatalf("events=%v errors=%v", events, errs)
	}
	if pending := in.Pending(); len(pending) != 0 {
		t.Fatalf("transport stuck: %v", pending)
	}
	rows, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []link.Event{result, owner} {
		n := 0
		for _, row := range rows {
			if row.EventID == ev.ID {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s has %d canonical rows", ev.ID, n)
		}
	}
	for _, row := range rows {
		if row.Message.Role == provider.RoleAssistant && strings.Contains(row.Message.Content, "DSML") {
			t.Fatal("protocol committed as a reply")
		}
	}
}
