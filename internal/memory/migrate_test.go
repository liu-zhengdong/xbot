package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrateBotPersonIntoBot(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	when := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	later := when.Add(time.Hour)
	s := Open(root)
	if err := s.Remember(ctx, Fact{
		Key: "rule", Scope: ScopeBot, Tier: TierProfile, BotName: "ada",
		WrittenBy: "ada", Text: "already here", Time: when,
	}); err != nil {
		t.Fatal(err)
	}
	// Same fact in the legacy dir must not be doubled.
	dup := Fact{
		Key: "rule", Kind: KindWork, Text: "already here", WrittenBy: "ada", Time: when,
	}
	kept := Fact{
		Key: "handshake", Kind: KindAttribute, Text: "secret handshake", WrittenBy: "ada",
		Device: "laptop", Untrusted: true, Time: later,
	}
	logged := Fact{
		Key: "aside", Kind: KindAttribute, Text: "mentioned once", WrittenBy: "ada",
		OwnerRecorded: true, Time: later.Add(time.Minute),
	}
	writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{dup, kept})
	writeLegacy(t, root, "ada", "u1", TierLog, []Fact{logged})
	writeLegacy(t, root, "ada", "u2", TierProfile, []Fact{{
		Key: "from-u2", Kind: KindAttribute, Text: "second person fact", WrittenBy: "ada", Time: later,
	}})

	got, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	byText := map[string]Fact{}
	for _, f := range got {
		if _, ok := byText[f.Text]; ok {
			t.Fatalf("duplicate %q in %+v", f.Text, got)
		}
		byText[f.Text] = f
	}
	for _, text := range []string{"already here", "secret handshake", "mentioned once", "second person fact"} {
		if _, ok := byText[text]; !ok {
			t.Fatalf("missing %q in %+v", text, got)
		}
	}
	h := byText["secret handshake"]
	if !h.Time.Equal(later) || h.WrittenBy != "ada" || h.Device != "laptop" || !h.Untrusted || h.Scope != ScopeBot {
		t.Fatalf("provenance %+v", h)
	}
	if byText["mentioned once"].Tier != TierLog || !byText["mentioned once"].OwnerRecorded {
		t.Fatalf("log provenance %+v", byText["mentioned once"])
	}
	if _, err := os.Stat(filepath.Join(root, "bots", "ada", "people", "u1", "memory")); !os.IsNotExist(err) {
		t.Fatalf("legacy dir still live: %v", err)
	}
	bak, err := os.ReadFile(filepath.Join(root, "bots", "ada", "people", "u1", "memory.bak", "profile.md"))
	if err != nil || !strings.Contains(string(bak), "secret handshake") {
		t.Fatalf("backup %q %v", bak, err)
	}
	again, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(got) {
		t.Fatalf("second load %d facts, first %d", len(again), len(got))
	}
	person, err := s.Recall(ctx, Query{Scope: ScopePerson, PersonID: "u1"})
	if err != nil || len(person) != 0 {
		t.Fatalf("legacy facts landed in person %+v %v", person, err)
	}
}

func TestMigrateOverflowGoesToLog(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	when := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	var have []Fact
	for i := 0; ; i++ {
		next := Fact{
			Key: "p", Kind: KindWork, WrittenBy: "ada",
			Text: strings.Repeat("n", 180) + string(rune('a'+i%26)),
			Time: when.Add(time.Duration(i) * time.Second),
		}
		tiny := Fact{Key: "tiny", Kind: KindAttribute, WrittenBy: "ada", Text: "tiny-fits", Time: when.Add(time.Hour)}
		if len(renderMD(append(append([]Fact{}, have...), tiny))) > ProfileBytes {
			if len(have) == 0 {
				t.Fatal("one fact already over the cap")
			}
			have = have[:len(have)-1]
			break
		}
		have = append(have, next)
		if i > 80 {
			t.Fatal("profile never filled")
		}
	}
	tiny := Fact{Key: "tiny", Kind: KindAttribute, WrittenBy: "ada", Text: "tiny-fits", Time: when.Add(time.Hour)}
	big := Fact{
		Key: "big", Kind: KindAttribute, WrittenBy: "ada",
		Text: "OVERFLOW " + strings.Repeat("Z", 700),
		Time: when.Add(2 * time.Hour),
	}
	if len(renderMD(append(append([]Fact{}, have...), tiny))) > ProfileBytes {
		t.Fatal("tiny fact does not fit")
	}
	if len(renderMD(append(append(append([]Fact{}, have...), tiny), big))) <= ProfileBytes {
		t.Fatal("fixture still has room for the overflow fact")
	}
	dest := filepath.Join(root, "bots", "ada", "memory")
	if err := saveTier(dest, TierProfile, have); err != nil {
		t.Fatal(err)
	}
	writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{tiny, big})

	s := Open(root)
	profile, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada", Tier: TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > ProfileBytes {
		t.Fatalf("profile is %d bytes", len(raw))
	}
	joined := texts(profile)
	if !strings.Contains(joined, "tiny-fits") || strings.Contains(joined, "OVERFLOW") {
		t.Fatalf("profile texts:\n%s", joined)
	}
	logFacts, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada", Tier: TierLog})
	if err != nil {
		t.Fatal(err)
	}
	logText := texts(logFacts)
	if !strings.Contains(logText, "OVERFLOW") || !strings.Contains(logText, "profile cap") {
		t.Fatalf("log texts:\n%s", logText)
	}
	var overflow Fact
	for _, f := range logFacts {
		if strings.HasPrefix(f.Text, "OVERFLOW") {
			overflow = f
		}
	}
	if !overflow.Time.Equal(big.Time) || overflow.WrittenBy != "ada" {
		t.Fatalf("overflow provenance %+v", overflow)
	}
	again, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada", Tier: TierLog})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(logFacts) {
		t.Fatalf("second load %d log facts, first %d", len(again), len(logFacts))
	}
}

func writeLegacy(t *testing.T, root, bot, person string, tier Tier, facts []Fact) {
	t.Helper()
	dir := filepath.Join(root, "bots", bot, "people", person, "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := renderMD(facts)
	path := filepath.Join(dir, string(tier)+".md")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func texts(facts []Fact) string {
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(f.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
