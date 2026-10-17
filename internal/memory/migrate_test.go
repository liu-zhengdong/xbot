package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
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
	writeOwners(t, root, "ada", "u1", "u2")
	if err := saveTier(filepath.Join(root, "bots", "ada", "memory"), TierProfile, []Fact{{
		Key: "rule", Kind: KindWork, Scope: ScopeBot, Tier: TierProfile, BotName: "ada",
		WrittenBy: "ada", Text: "already here", Time: when,
	}}); err != nil {
		t.Fatal(err)
	}
	s := Open(root)
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
	writeOwners(t, root, "ada", "u1")
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

func writeOwners(t *testing.T, root, bot string, ids ...string) {
	t.Helper()
	dir := filepath.Join(root, "bots", bot)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"name": bot, "owners": ids})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
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

func TestMigrateLeavesNonOwnerInBackup(t *testing.T) {
	root := t.TempDir()
	when := time.Unix(100, 0).UTC()
	writeOwners(t, root, "ada", "u1")
	writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{{
		Key: "mine", Kind: KindAttribute, WrittenBy: "ada", Text: "owner-fact", Time: when,
	}})
	writeLegacy(t, root, "ada", "u9", TierProfile, []Fact{{
		Key: "theirs", Kind: KindAttribute, WrittenBy: "ada", Text: "stranger-fact", Time: when,
	}})
	got, err := Open(root).Recall(context.Background(), Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	body := texts(got)
	if !strings.Contains(body, "owner-fact") || strings.Contains(body, "stranger-fact") {
		t.Fatalf("bot memory:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(root, "bots", "ada", "people", "u9", "memory")); !os.IsNotExist(err) {
		t.Fatalf("non-owner dir still live: %v", err)
	}
	bak, err := os.ReadFile(filepath.Join(root, "bots", "ada", "people", "u9", "memory.bak", "profile.md"))
	if err != nil || !strings.Contains(string(bak), "stranger-fact") {
		t.Fatalf("backup %q %v", bak, err)
	}
}

func TestMigrateLogStaysWithinCap(t *testing.T) {
	root := t.TempDir()
	writeOwners(t, root, "ada", "u1")
	when := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	var facts []Fact
	for i := 0; ; i++ {
		facts = append(facts, Fact{
			Key: "k", Kind: KindWork, WrittenBy: "ada",
			Text: strings.Repeat("m", 400) + string(rune('a'+i%26)),
			Time: when.Add(time.Duration(i) * time.Second),
		})
		if len(renderMD(append([]Fact(nil), facts...))) > maxLogBytes {
			break
		}
		if i > 400 {
			t.Fatal("log never filled")
		}
	}
	facts = append(facts, Fact{
		Key: "tail", Kind: KindWork, WrittenBy: "ada", Text: "TAIL-OVERFLOW",
		Time: when.Add(time.Hour),
	})
	writeLegacy(t, root, "ada", "u1", TierLog, facts)
	got, err := Open(root).Recall(context.Background(), Query{Scope: ScopeBot, BotName: "ada", Tier: TierLog})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts(got), "TAIL-OVERFLOW") {
		t.Fatal("overflow fact missing from recall")
	}
	raw, err := os.ReadFile(filepath.Join(root, "bots", "ada", "memory", "log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > logKeepBytes {
		t.Fatalf("log.md is %d bytes, keep budget is %d", len(raw), logKeepBytes)
	}
	if !strings.Contains(string(raw), "TAIL-OVERFLOW") {
		t.Fatal("newest fact was not kept in log.md")
	}
	if _, err := os.Stat(filepath.Join(root, "bots", "ada", "memory", "log.overflow.md")); err != nil {
		t.Fatal(err)
	}
}

func TestM33CorruptLegacyIndexDoesNotBreakOtherBots(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "bots", "broken", "people", "u1", "memory")
	if err := os.MkdirAll(bad, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, ".index.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bots", "good"), 0700); err != nil {
		t.Fatal(err)
	}
	s := Open(root)
	ctx := context.Background()
	err := s.Remember(ctx, Fact{Scope: ScopeBot, BotName: "good", Tier: TierProfile, Text: "hello", WrittenBy: "good", Time: time.Unix(1, 0)})
	if err != nil {
		t.Fatalf("remember for an unrelated bot failed: %v", err)
	}
	if _, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "good", Tier: TierProfile}); err != nil {
		t.Fatalf("recall for an unrelated bot failed: %v", err)
	}
}

func TestMigrateCorruptOwnerStaysAndOtherBotWorks(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "bots", "broken", "people", "u1", "memory")
	writeOwners(t, root, "broken", "u1")
	if err := os.MkdirAll(bad, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, ".index.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bots", "good"), 0700); err != nil {
		t.Fatal(err)
	}
	s := Open(root)
	ctx := context.Background()
	if err := s.Remember(ctx, Fact{Scope: ScopeBot, BotName: "good", Tier: TierProfile, Text: "hello", WrittenBy: "good", Time: time.Unix(1, 0)}); err != nil {
		t.Fatalf("remember for an unrelated bot failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bad, ".index.json")); err != nil {
		t.Fatalf("corrupt source was moved: %v", err)
	}
}

func TestMigrateBadConfigLeavesLegacyInPlace(t *testing.T) {
	when := time.Unix(50, 0).UTC()
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{"missing", func(t *testing.T, dir string) {}},
		{"invalid", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unreadable", func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "config.json"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{{
				Key: "keep", Kind: KindAttribute, WrittenBy: "ada", Text: "keep-me", Time: when,
			}})
			tc.setup(t, filepath.Join(root, "bots", "ada"))
			migrateRoot(root)
			src := filepath.Join(root, "bots", "ada", "people", "u1", "memory", "profile.md")
			if _, err := os.Stat(src); err != nil {
				t.Fatalf("source moved: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "bots", "ada", "people", "u1", "memory.bak")); !os.IsNotExist(err) {
				t.Fatal("legacy folder was set aside without a readable owner list")
			}
			got, err := Open(root).Recall(context.Background(), Query{Scope: ScopeBot, BotName: "ada"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(texts(got), "keep-me") {
				t.Fatal("merged without a readable owner list")
			}
		})
	}
}

func TestMigrateNoOwnersParksAndLogs(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	root := t.TempDir()
	when := time.Unix(60, 0).UTC()
	writeOwners(t, root, "ada")
	writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{{
		Key: "old", Kind: KindAttribute, WrittenBy: "ada", Text: "only-in-backup", Time: when,
	}})
	migrateRoot(root)
	if _, err := os.Stat(filepath.Join(root, "bots", "ada", "people", "u1", "memory")); !os.IsNotExist(err) {
		t.Fatalf("non-owner dir still live: %v", err)
	}
	bak, err := os.ReadFile(filepath.Join(root, "bots", "ada", "people", "u1", "memory.bak", "profile.md"))
	if err != nil || !strings.Contains(string(bak), "only-in-backup") {
		t.Fatalf("backup %q %v", bak, err)
	}
	got, err := Open(root).Recall(context.Background(), Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(texts(got), "only-in-backup") {
		t.Fatal("bot with no owners merged legacy memory")
	}
	folder := filepath.Join("bots", "ada", "people", "u1", "memory")
	if !strings.Contains(buf.String(), folder) {
		t.Fatalf("non-owner folder was not logged: %q", buf.String())
	}
}

func TestMigrateLaterBotIsNotBlocked(t *testing.T) {
	root := t.TempDir()
	when := time.Unix(70, 0).UTC()
	writeOwners(t, root, "ada", "u1")
	writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{{
		Key: "a", Kind: KindAttribute, WrittenBy: "ada", Text: "ada-fact", Time: when,
	}})
	migrateRoot(root)
	writeOwners(t, root, "bea", "u1")
	writeLegacy(t, root, "bea", "u1", TierProfile, []Fact{{
		Key: "b", Kind: KindAttribute, WrittenBy: "bea", Text: "bea-fact", Time: when,
	}})
	migrateRoot(root)
	got, err := Open(root).Recall(context.Background(), Query{Scope: ScopeBot, BotName: "bea"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts(got), "bea-fact") {
		t.Fatalf("later bot was not migrated: %+v", got)
	}
}

func TestMigrateCorruptNonOwnerIsSetAside(t *testing.T) {
	root := t.TempDir()
	when := time.Unix(80, 0).UTC()
	writeOwners(t, root, "ada", "u1")
	writeLegacy(t, root, "ada", "u1", TierProfile, []Fact{{
		Key: "mine", Kind: KindAttribute, WrittenBy: "ada", Text: "owner-fact", Time: when,
	}})
	bad := filepath.Join(root, "bots", "ada", "people", "u9", "memory")
	if err := os.MkdirAll(bad, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, ".index.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	migrateRoot(root)
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("corrupt non-owner dir still live: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bad+".bak", ".index.json")); err != nil {
		t.Fatalf("corrupt non-owner was not set aside: %v", err)
	}
	got, err := Open(root).Recall(context.Background(), Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts(got), "owner-fact") {
		t.Fatalf("owner fact missing: %+v", got)
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
