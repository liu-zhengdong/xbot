package bot

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestSkillSwitchesPersistAcrossStaleSaves(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.DisableSkill("capture"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stale.DisabledSkills(), "capture") {
		t.Fatal("running bot missed disable")
	}
	if err := stale.SetOwners([]string{"owner"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(b.DisabledSkills(), "capture") {
		t.Fatal("stale config save reset switch")
	}
	if err := stale.DisableSkill("other"); err != nil {
		t.Fatal(err)
	}
	if err := b.EnableSkill("capture"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reloaded.Config().Owners, []string{"owner"}) || !slices.Equal(reloaded.DisabledSkills(), []string{"other"}) {
		t.Fatal(reloaded.Config())
	}
}

func TestSkillSwitchConcurrentDistinctSlugs(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		loaded, err := Load(b.Root, b.Name())
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := loaded.DisableSkill(fmt.Sprintf("skill-%d", i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	reloaded, err := Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Config().DisabledSkills; len(got) != n {
		t.Fatalf("lost updates: %v", got)
	}
}

func BenchmarkDisabledSkills(b *testing.B) {
	bot, err := Create(b.TempDir(), "ada", "persona", nil)
	if err != nil {
		b.Fatal(err)
	}
	if err := bot.DisableSkill("capture"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(bot.DisabledSkills()) != 1 {
			b.Fatal("missing switch")
		}
	}
}
