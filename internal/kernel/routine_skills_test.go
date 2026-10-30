package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

// Exercise the actual trigger, disk queue and model-request assembly, rather
// than calling the projection helper directly.
func TestRoutineSkillsWake(t *testing.T) {
	b := testBot(t, link.NameTimer)
	k := skillKernel(b)
	tm := &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}
	k.Links = append(k.Links, tm)
	for _, s := range []struct{ id, body string }{{"atlas", "ATLAS_BODY_SENTINEL"}, {"ship", "SHIP_BODY_SENTINEL"}} {
		if err := skill.Write(b.Root, s.id, s.id, s.body); err != nil {
			t.Fatal(err)
		}
	}
	library := filepath.Join(k.SkillsDir, "atlas", "SKILL.md")
	original, err := os.ReadFile(library)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (routineCreate{k}).Run(context.Background(), json.RawMessage(`{"id":"morning","name":"morning","prompt":"inspect","cron":"0 * * * *","skills":["atlas","missing","ship"]}`))
	if err != nil {
		t.Fatal(err)
	}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	fire := func(now time.Time) string {
		t.Helper()
		p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}}}
		k.Provider = p
		if err := tm.Fire(now, in.Post); err != nil {
			t.Fatal(err)
		}
		events := in.Pending()
		if len(events) != 1 {
			t.Fatalf("pending: %v", events)
		}
		if err := k.Handle(context.Background(), events[0]); err != nil {
			t.Fatal(err)
		}
		if err := in.Ack(events[0].ID); err != nil {
			t.Fatal(err)
		}
		if len(p.reqs) != 1 {
			t.Fatalf("requests %d", len(p.reqs))
		}
		if strings.Contains(p.reqs[0].Messages[0].Text(), "ATLAS_BODY_SENTINEL") {
			t.Fatal("body changed stable prefix")
		}
		return messagesText(p.reqs[0].Messages)
	}
	now := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	text := fire(now)
	a, m, s := strings.Index(text, "ATLAS_BODY_SENTINEL"), strings.Index(text, `Routine skill "missing" skipped:`), strings.Index(text, "SHIP_BODY_SENTINEL")
	if a < 0 || m < a || s < m {
		t.Fatalf("ordered bodies / explicit missing reason absent: %s", text)
	}
	if strings.Contains(transcriptText(t, b), "ATLAS_BODY_SENTINEL") {
		t.Fatal("skill body persisted")
	}
	after, err := os.ReadFile(library)
	if err != nil || string(after) != string(original) {
		t.Fatal("wake changed skill library")
	}
	if err := skill.Write(b.Root, "atlas", "atlas", "UPDATED_BODY_SENTINEL"); err != nil {
		t.Fatal(err)
	}
	if err := b.DisableSkill("ship"); err != nil {
		t.Fatal(err)
	}
	text = fire(now.Add(time.Hour))
	if !strings.Contains(text, "UPDATED_BODY_SENTINEL") || strings.Contains(text, "ATLAS_BODY_SENTINEL") || strings.Contains(text, "SHIP_BODY_SENTINEL") || !strings.Contains(text, `Routine skill "ship" skipped: skill: disabled`) {
		t.Fatalf("fresh read / disabled: %s", text)
	}
	if err := os.Remove(library); err != nil {
		t.Fatal(err)
	}
	text = fire(now.Add(2 * time.Hour))
	if !strings.Contains(text, `Routine skill "atlas" skipped:`) || strings.Contains(text, "UPDATED_BODY_SENTINEL") {
		t.Fatalf("deleted skill: %s", text)
	}
	_, err = (routineUpdate{k}).Run(context.Background(), json.RawMessage(`{"id":"morning","skills":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	text = fire(now.Add(3 * time.Hour))
	if strings.Contains(text, "Routine skill") || strings.Contains(text, "BODY_SENTINEL") {
		t.Fatalf("empty routine retained skill projection: %s", text)
	}
}

func TestRoutineSkillsToolUpdates(t *testing.T) {
	b := testBot(t, link.NameTimer)
	k := skillKernel(b)
	k.Links = append(k.Links, &link.Timer{Dir: b.RoutineDir()})
	if _, err := (routineCreate{k}).Run(context.Background(), json.RawMessage(`{"id":"r1","name":"routine","prompt":"inspect","cron":"0 * * * *","skills":["ship","atlas"]}`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ args, want string }{{`{"id":"r1","enabled":false}`, `skills ["ship","atlas"]`}, {`{"id":"r1","skills":["atlas"]}`, `skills ["atlas"]`}, {`{"id":"r1","skills":[]}`, ""}} {
		if _, err := (routineUpdate{k}).Run(context.Background(), json.RawMessage(tc.args)); err != nil {
			t.Fatal(err)
		}
		out, err := (routineList{k}).Run(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want != "" && !strings.Contains(out, tc.want) || tc.want == "" && strings.Contains(out, "skills") {
			t.Fatalf("list %s want %s", out, tc.want)
		}
	}
}

type routinePostingProvider struct {
	scripted
	post func() error
}

func (p *routinePostingProvider) Chat(ctx context.Context, req provider.Request, delta func(string)) (provider.Message, error) {
	if p.i == 0 {
		if err := p.post(); err != nil {
			return provider.Message{}, err
		}
	}
	return p.scripted.Chat(ctx, req, delta)
}

func TestRoutineSkillsAbsorbedWhileBusy(t *testing.T) {
	b := testBot(t, link.NameTimer, link.NameTerminal)
	k := skillKernel(b)
	tm := &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}
	k.Links = append(k.Links, tm)
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = box
	if err := skill.Write(b.Root, "atlas", "Atlas", "BUSY_SKILL_BODY"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	if err := tm.Save(link.Routine{ID: "check", Prompt: "inspect", At: now.Format(time.RFC3339), Skills: []string{"atlas"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	p := &routinePostingProvider{scripted: scripted{resps: []provider.Message{assistantCall("list", "", "routine_list", `{}`), {Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}}}, post: func() error { return tm.Fire(now, box.Post) }}
	k.Provider = p
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "first work", now)); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 || !strings.Contains(messagesText(p.reqs[1].Messages), "BUSY_SKILL_BODY") {
		t.Fatal("absorbed routine skill did not reach next model call")
	}
	if strings.Contains(transcriptText(t, b), "BUSY_SKILL_BODY") {
		t.Fatal("absorbed skill body persisted")
	}
	if len(box.Pending()) != 0 {
		t.Fatal("absorbed event not acknowledged")
	}
	next := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("done")}}}
	k.Provider = next
	nextEvent := link.TerminalEvent("u1", "next", now.Add(time.Minute))
	nextEvent.RoutineSkills = []string{"atlas"} // Other sources cannot opt into timer skill loading.
	if err := k.Handle(context.Background(), nextEvent); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(messagesText(next.reqs[0].Messages), "BUSY_SKILL_BODY") {
		t.Fatal("absorbed body leaked to next turn")
	}
}

func BenchmarkRoutineSkillProjection(b *testing.B) {
	root := b.TempDir()
	bot, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		b.Fatal(err)
	}
	k := skillKernel(bot)
	for _, id := range []string{"atlas", "ship"} {
		if err := skill.Write(root, id, id, strings.Repeat("Skill instruction.\n", 800)); err != nil {
			b.Fatal(err)
		}
	}
	ev := link.Event{Source: link.NameTimer, RoutineSkills: []string{"atlas", "ship"}}
	m := provider.Message{Role: provider.RoleUser, Content: provider.TextContent("scheduled wake")}
	samples := make([]time.Duration, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		k.projectRoutineSkills(m, ev)
		samples[i] = time.Since(start)
	}
	b.StopTimer()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	b.ReportMetric(float64(samples[(len(samples)-1)*95/100].Nanoseconds()), "p95-ns")
}
