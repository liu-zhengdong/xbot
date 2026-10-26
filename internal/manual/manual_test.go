package manual

import (
	"strings"
	"testing"
)

func TestDirectoryAndSections(t *testing.T) {
	sections := Sections()
	if len(sections) != 7 {
		t.Fatalf("sections: %d", len(sections))
	}
	sections[0].ID = "changed"
	if Sections()[0].ID != "memory" {
		t.Fatal("caller mutated directory")
	}
	for _, s := range Sections() {
		body, err := Read(s.ID)
		if err != nil || !strings.HasPrefix(body, "# "+s.Title) || len(body) > 16<<10 {
			t.Fatalf("section %s: %v (%d bytes)", s.ID, err, len(body))
		}
		if !strings.Contains(Index(), s.ID+": "+s.Title) {
			t.Fatal("missing index entry", s)
		}
		if strings.Contains(Prompt(), body) {
			t.Fatal("body injected into prefix", s.ID)
		}
	}
	if len(Prompt()) > 2048 {
		t.Fatalf("manual prefix too large: %d bytes", len(Prompt()))
	}
	for _, bad := range []string{"", "../memory", "sections/memory.md", "/etc/passwd", "MEMORY", "all"} {
		if body, err := Read(bad); err == nil || body != "" {
			t.Fatalf("accepted unknown section %q", bad)
		}
	}
}

func TestMemoryNamesAndLocations(t *testing.T) {
	body, _ := Read("memory")
	for _, fact := range []string{"memory.md", "log/YYYY-MM.md", "<root>/bots/<bot>/memory/", "<root>/people/<id>/memory/", "4 KiB", "64 KiB", "[对话]", "「记忆」", "--root DIR", "persona.md"} {
		if !strings.Contains(body, fact) {
			t.Errorf("missing memory fact: %s", fact)
		}
	}
}

func BenchmarkReadMemory(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Read("memory"); err != nil {
			b.Fatal(err)
		}
	}
}
