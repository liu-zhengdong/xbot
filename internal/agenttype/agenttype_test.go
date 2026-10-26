package agenttype

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuiltinTypes(t *testing.T) {
	types, errs := Load(filepath.Join(t.TempDir(), "missing"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	for _, name := range []string{"research", "dev", "general"} {
		ty, ok := Find(types, name)
		if !ok {
			t.Fatalf("no built-in %s", name)
		}
		if ty.When == "" || len(ty.Tools) == 0 || ty.Budget <= 0 {
			t.Fatalf("%s is incomplete: %+v", name, ty)
		}
	}
	research, _ := Find(types, "research")
	for _, tool := range []string{"write_file", "edit_file", "sh"} {
		if research.Allows(tool) {
			t.Fatalf("research allows %s", tool)
		}
	}
	if !research.Allows(ShellReadOnly) {
		t.Fatal("research cannot read with the shell")
	}
}

func TestBuiltinDevAllowsSkills(t *testing.T) {
	dev, ok := Find(Builtin(), "dev")
	if !ok {
		t.Fatal("missing dev type")
	}
	for _, name := range []string{"read_skill", "write_skill"} {
		if !dev.Allows(name) {
			t.Errorf("dev cannot use %s", name)
		}
	}
}

func TestFileOverridesAndAdds(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("research.md", "---\nwhen: look things up, slowly\ntools: web_search\nbudget: 5m\nreport: Answer\n---\nBe thorough.\n")
	write("writer.md", "---\nwhen: draft prose\ntools: read_file, write_file\n---\nWrite plainly.")
	write("bad.md", "---\ntools: read_file\n---\n")
	write("notes.txt", "ignored")
	types, errs := Load(dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "when is empty") {
		t.Fatalf("errs %v", errs)
	}
	r, _ := Find(types, "research")
	if r.Builtin || r.Budget != 5*time.Minute || r.Prompt != "Be thorough." || !r.Allows("web_search") || r.Allows("read_file") {
		t.Fatalf("research not overridden: %+v", r)
	}
	w, ok := Find(types, "writer")
	if !ok || w.Budget != 45*time.Minute || !w.Allows("write_file") {
		t.Fatalf("writer %+v", w)
	}
	if _, ok := Find(types, "dev"); !ok {
		t.Fatal("an override dropped the other built-ins")
	}
}

func TestParseRejects(t *testing.T) {
	for name, body := range map[string]string{
		"nohead":  "when: x\n",
		"open":    "---\nwhen: x\ntools: a\n",
		"badkey":  "---\nwhen: x\ntools: a\ncolor: red\n---\n",
		"budget":  "---\nwhen: x\ntools: a\nbudget: soon\n---\n",
		"notools": "---\nwhen: x\n---\n",
		"Upper":   "---\nwhen: x\ntools: a\n---\n",
	} {
		if _, err := Parse(name, []byte(body)); err == nil {
			t.Fatalf("%s parsed", name)
		}
	}
}

func TestInstructionsNameTheReport(t *testing.T) {
	ty, _ := Find(Builtin(), "dev")
	got := ty.Instructions("You are a subagent.")
	for _, want := range []string{"You are a subagent.", "Read before you edit.", "Changed, Tests, Not done"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
