package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMonthlyMigrationAndResume(t *testing.T) {
	root := t.TempDir()
	bot := filepath.Join(root, "bots/dev/memory")
	person := filepath.Join(root, "people/u1/memory")
	when := time.Date(2026, 8, 2, 12, 34, 56, 0, time.Local)
	f := Fact{Text: "first", WrittenBy: "dev", Scope: ScopeBot, Time: when}
	g := f
	g.Text = "second"
	g.Time = when.AddDate(0, 1, 0)
	for _, item := range []struct {
		dir, base string
		fact      Fact
	}{{bot, "log", f}, {bot, "log.overflow", g}, {person, "log", f}} {
		layoutWrite(t, filepath.Join(item.dir, item.base+".md"), []byte(renderMD([]Fact{item.fact})))
		if err := writeIndex(filepath.Join(item.dir, item.base+".index.json"), []Fact{item.fact}); err != nil {
			t.Fatal(err)
		}
	}
	// A previous process wrote one month but crashed before backing up log.md.
	if err := writeMonths(bot, []Fact{f}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTier(bot, TierLog, ScopeBot, "dev", ""); err == nil {
		t.Fatal("legacy log hidden")
	}
	layoutWrite(t, filepath.Join(bot, "log.md.bak"), []byte("existing backup"))
	if err := Migrate(root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{bot, person} {
		scope, name, id, want := ScopeBot, "dev", "", 2
		if dir == person {
			scope, name, id, want = ScopePerson, "", "u1", 1
		}
		got, err := loadTier(dir, TierLog, scope, name, id)
		if err != nil || len(got) != want {
			t.Fatalf("%s: %+v %v", dir, got, err)
		}
	}
	for _, path := range []string{filepath.Join(bot, "log.md.bak.1"), filepath.Join(bot, "log.overflow.md.bak"), filepath.Join(person, "log.index.json.bak")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	first := layoutTree(t, root)
	// Exercise the actual walker as well as the public once-per-process entrypoint.
	if err := Migrate(root); err != nil {
		t.Fatal(err)
	}
	migrateRoot(root)
	if !reflect.DeepEqual(first, layoutTree(t, root)) {
		t.Fatal("second migration changed files")
	}
}

func TestMonthlyGuardAndRoundTrip(t *testing.T) {
	s := Open(t.TempDir())
	ctx := context.Background()
	dir := filepath.Join(s.Root, "bots/dev/memory")
	now := time.Now().In(time.Local)
	var past []Fact
	for i := 0; i < 90; i++ {
		past = append(past, Fact{Scope: ScopeBot, Text: fmt.Sprintf("%d %s", i, strings.Repeat("x", 800)), Time: now.AddDate(0, -1, 0)})
	}
	if err := writeMonths(dir, past, false); err != nil {
		t.Fatal(err)
	}
	f := Fact{Scope: ScopeBot, BotName: "dev", Text: "current", Time: now}
	if err := s.Remember(ctx, f); err != nil {
		t.Fatal(err)
	}
	for i := range past {
		past[i].Time = now
	}
	if err := writeMonths(dir, past, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(ctx, f); err == nil || !strings.Contains(err.Error(), "整理") {
		t.Fatalf("guard: %v", err)
	}
	p := Fact{Scope: ScopePerson, PersonID: "u1", Tier: TierLog, WrittenBy: "dev", Text: Provenance("remote", "dev", "node"), Untrusted: true, Time: now}
	if err := s.Remember(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopePerson, PersonID: "u1", Tier: TierLog})
	if err != nil || len(got) != 1 || got[0].WrittenBy != "dev" || got[0].Source != "dev" || !got[0].Untrusted || got[0].Text != p.Text {
		t.Fatalf("%+v %v", got, err)
	}
	f.Source, f.Text, f.OwnerRecorded = "episode", OwnerProvenance("summary", ""), true
	other := filepath.Join(s.Root, "bots/other/memory")
	if err := writeMonths(other, []Fact{f}, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, "log", now.Format("2006-01")+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	layoutWrite(t, path, []byte("# heading\r\ninvalid\r\n"+strings.ReplaceAll(string(raw), "\n", "\r\n")))
	got, err = loadTier(other, TierLog, ScopeBot, "other", "")
	if err != nil || len(got) != 1 || got[0].Source != "episode" || !got[0].OwnerRecorded || got[0].Time.Second() != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestMonthlyTailSkipsOlderFiles(t *testing.T) {
	s := Open(t.TempDir())
	dir := filepath.Join(s.Root, "bots/dev/memory")
	var facts []Fact
	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.Local)
	for i := 0; i < 300; i++ {
		facts = append(facts, Fact{Scope: ScopeBot, Text: fmt.Sprintf("entry-%03d", i), Time: start.AddDate(0, i/50, 0).Add(time.Duration(i%50) * time.Minute)})
	}
	if err := writeMonths(dir, facts, false); err != nil {
		t.Fatal(err)
	}
	total, err := s.LogCount(ScopeBot, "dev", "")
	if err != nil || total != 300 {
		t.Fatalf("count %d %v", total, err)
	}
	// A directory with a month filename deterministically fails if opened,
	// including when tests run as root (unlike chmod).
	old := filepath.Join(dir, "log", "2024-12.md")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(context.Background(), Query{Scope: ScopeBot, BotName: "dev", Tier: TierLog})
	if err != nil || len(got) != 200 || got[0].Text != "entry-100" || got[199].Text != "entry-299" {
		t.Fatalf("tail %d %v", len(got), err)
	}
	if _, err := s.Recall(context.Background(), Query{Scope: ScopeBot, BotName: "dev", Tier: TierLog, Text: "entry"}); err == nil {
		t.Fatal("text search skipped old months")
	}
}
