package memory

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func layoutWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func layoutTree(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func layoutQuery(rel string) Query {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	q := Query{Scope: ScopeBot, BotName: parts[1], Tier: TierProfile}
	if parts[0] == "people" {
		q.Scope, q.PersonID, q.BotName = ScopePerson, parts[1], ""
	}
	return q
}
func TestLayoutMigrationIdempotent(t *testing.T) {
	root := t.TempDir()
	f := Fact{Key: "custom", Text: "kept fact", Kind: KindWork, WrittenBy: "dev", Untrusted: true, Time: time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)}
	dirs := []string{"people/u1000/memory", "bots/dev/memory", "bots/health/memory"}
	for _, rel := range dirs {
		dir := filepath.Join(root, rel)
		legacy := f
		legacy.Untrusted = rel == "bots/health/memory"
		layoutWrite(t, filepath.Join(dir, "profile.md"), []byte(renderMD([]Fact{legacy})))
		if rel != "bots/health/memory" {
			if err := writeIndex(filepath.Join(dir, "profile.index.json"), []Fact{f}); err != nil {
				t.Fatal(err)
			}
			layoutWrite(t, filepath.Join(dir, "log.md"), []byte(renderMD(nil)))
		}
	}
	for _, bot := range []string{"dev", "health", "inbox", "diary"} {
		writeOwners(t, root, bot, "u1000")
	}
	layoutWrite(t, filepath.Join(root, "bots/inbox/memory/log.md"), []byte(renderMD(nil)))
	layoutWrite(t, filepath.Join(root, "bots/dev/people/u1000/memory.bak/profile.md"), []byte(renderMD([]Fact{f})))
	f.Scope, f.PersonID, f.Tier = ScopePerson, "u1000", TierProfile
	if err := noteSync(root, opRemember, f); err != nil {
		t.Fatal(err)
	}
	before := layoutTree(t, root)
	migrateRoot(root)
	for _, rel := range dirs {
		got, err := Open(root).Recall(context.Background(), layoutQuery(rel))
		if err != nil || len(got) != 1 || got[0].Text != f.Text || !got[0].Untrusted {
			t.Fatalf("%s: %+v %v", rel, got, err)
		}
		for _, name := range []string{"memory.md", "memory.index.json", "profile.md.bak", "profile.index.json.bak"} {
			if _, err := os.Stat(filepath.Join(root, rel, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	first := layoutTree(t, root)
	for _, rel := range []string{"bots/dev/people/u1000/memory.bak/profile.md", "people/u1000/memory/sync.jsonl"} {
		if first[rel] != before[rel] {
			t.Fatalf("changed %s", rel)
		}
	}
	migrateRoot(root)
	if !reflect.DeepEqual(first, layoutTree(t, root)) {
		t.Fatal("second migration changed tree")
	}
}
func TestMemoryMDKeepsIndexAndMetadata(t *testing.T) {
	root := t.TempDir()
	s, ctx := Open(root), context.Background()
	f := Fact{Scope: ScopeBot, BotName: "dev", Tier: TierProfile, Key: "custom-key", Untrusted: true, Text: "deploy via ssh"}
	if err := s.Remember(ctx, f); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "dev", Tier: TierProfile})
	if err != nil || len(saved) != 1 {
		t.Fatalf("%+v %v", saved, err)
	}
	path := filepath.Join(root, "bots/dev/memory/memory.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index := "- projects/xbot.md  project notes\n  keep this spacing"
	edited := strings.Replace(string(raw), "（暂无）", index, 1)
	edited = strings.Replace(edited, saved[0].Time.In(time.Local).Format("2006-01-02"), "2026-10-14", 1)
	edited += "- (2026-10-18) hand added line\n- (2026-10-19) deploy via ssh\n"
	layoutWrite(t, path, []byte(edited))
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "dev", Tier: TierProfile})
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	found := 0
	for _, g := range got {
		if reflect.DeepEqual(g, saved[0]) {
			found++
		}
		if g.Text == "hand added line" && g.Kind != KindWork {
			t.Fatalf("wrong default kind: %+v", g)
		}
	}
	if found != 1 || !strings.Contains(texts(got), "hand added line") {
		t.Fatalf("metadata or text lost: %+v", got)
	}
	f.Text, f.Key = "another fact", "another"
	if err := s.Remember(ctx, f); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "## 索引\n"+index+"\n\n## 条目") {
		t.Fatalf("index lost: %s %v", raw, err)
	}
}

// TestLiveCopyMigration runs the migration on a copy of a real data root
// (XBOT_MIGRATE_COPY; the source is never written): every profile text is kept,
// backups exist, and a second run changes nothing.
func TestLiveCopyMigration(t *testing.T) {
	source := os.Getenv("XBOT_MIGRATE_COPY")
	if source == "" {
		t.Skip("XBOT_MIGRATE_COPY is not set")
	}
	root := filepath.Join(t.TempDir(), "root")
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		raw, err := os.ReadFile(path)
		if err == nil {
			layoutWrite(t, filepath.Join(root, rel), raw)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	logs := map[string]int{}
	for rel := range layoutTree(t, root) {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 4 && parts[2] == "memory" && (parts[3] == "log.md" || parts[3] == "log.overflow.md") {
			q := layoutQuery(rel)
			facts, err := loadNamed(filepath.Join(root, filepath.Dir(rel)), strings.TrimSuffix(parts[3], ".md"), q.Scope, TierLog, q.BotName, q.PersonID)
			if err != nil {
				t.Fatal(err)
			}
			logs[filepath.Dir(rel)] += len(facts)
		}
		if len(parts) != 4 || parts[2] != "memory" || parts[3] != "profile.md" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		facts, err := parseMD(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range facts {
			want[filepath.Dir(rel)] = append(want[filepath.Dir(rel)], f.Text)
		}
	}
	for rel := range logs {
		q := layoutQuery(rel)
		facts, err := loadMonths(filepath.Join(root, rel), q.Scope, q.BotName, q.PersonID, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		logs[rel] += len(facts)
	}
	migrateRoot(root)
	for rel, texts := range want {
		got, err := Open(root).Recall(context.Background(), layoutQuery(rel))
		if err != nil {
			t.Fatal(err)
		}
		present := map[string]bool{}
		for _, f := range got {
			present[f.Text] = true
		}
		for _, text := range texts {
			if !present[text] {
				t.Fatalf("%s: lost %q", rel, text)
			}
		}
		if _, err := os.Stat(filepath.Join(root, rel, "profile.md.bak")); err != nil {
			t.Fatal(err)
		}
	}
	for rel, wantCount := range logs {
		q := layoutQuery(rel)
		facts, err := loadTier(filepath.Join(root, rel), TierLog, q.Scope, q.BotName, q.PersonID)
		if err != nil || len(facts) != wantCount {
			t.Fatalf("%s log count: %d want %d (%v)", rel, len(facts), wantCount, err)
		}
	}
	first := layoutTree(t, root)
	migrateRoot(root)
	if !reflect.DeepEqual(first, layoutTree(t, root)) {
		t.Fatal("second migration changed the copy")
	}
}

func TestLayoutResumesPartialConversion(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "bots/dev/memory")
	when := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	old := Fact{Key: "legacy", Text: "legacy fact", Kind: KindWork, Untrusted: true, Time: when}
	layoutWrite(t, filepath.Join(dir, "profile.md"), []byte(renderMD([]Fact{old})))
	if err := writeIndex(filepath.Join(dir, "profile.index.json"), []Fact{old}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTier(dir, TierProfile, ScopeBot, "dev", ""); err == nil {
		t.Fatal("missing memory.md hid legacy profile")
	}
	current := Fact{Text: OwnerProvenance("new fact", "linked"), Scope: ScopeBot, Time: when}
	untrusted := Fact{Text: Provenance("hand added", "remote", "linked"), Scope: ScopeBot, Time: when}
	if err := writeTier(dir, TierProfile, []Fact{old, current, untrusted}, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "memory.index.json")); err != nil {
		t.Fatal(err)
	}
	// One legacy fact was converted before the crash; another exists only in profile.
	extra := old
	extra.Key, extra.Text = "extra", "profile only"
	if err := writeNamed(dir, "profile", []Fact{old, extra}, ProfileBytes, false, false, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	layoutWrite(t, filepath.Join(dir, "memory.md"), []byte(strings.ReplaceAll(string(raw), "\n", "\r\n")))
	migrateLayout(root)
	got, err := loadTier(dir, TierProfile, ScopeBot, "dev", "")
	if err != nil || len(got) != 4 {
		t.Fatalf("%+v %v", got, err)
	}
	present := map[string]bool{}
	for _, f := range got {
		present[f.Text] = true
		if f.Text == untrusted.Text && !f.Untrusted {
			t.Fatalf("provenance lost: %+v", f)
		}
		if f.Text == old.Text && (!f.Untrusted || f.Key != old.Key) {
			t.Fatalf("legacy metadata lost: %+v", f)
		}
		if f.Text == current.Text && (!f.OwnerRecorded || f.Kind != KindWork) {
			t.Fatalf("parsed metadata lost: %+v", f)
		}
	}
	for _, text := range []string{old.Text, extra.Text, current.Text, untrusted.Text} {
		if !present[text] {
			t.Fatalf("missing %q", text)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.md.bak")); err != nil {
		t.Fatal(err)
	}
}
