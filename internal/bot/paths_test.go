package bot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/provider"
)

func TestTracePathsMatchStorage(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "Ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.LogUsage(Trace{Prompt: 42, Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("path proof")}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.AgentDir("sa_test"), 0700); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(b.AgentDir("sa_test"), "transcript.jsonl")
	if err := os.WriteFile(agentPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(wd, root)
	if err != nil {
		t.Fatal(err)
	}
	b.Root = relative
	paths, err := b.Paths()
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		paths.Bot: Dir(root, "Ada"), paths.Trace: filepath.Join(Dir(root, "Ada"), "trace.jsonl"),
		paths.Transcript: filepath.Join(Dir(root, "Ada"), "transcript"), paths.Agents: filepath.Join(Dir(root, "Ada"), "agents"),
	} {
		if got != want || !filepath.IsAbs(got) {
			t.Fatalf("got %s, want %s", got, want)
		}
		if _, err := os.Stat(got); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.Agents, "sa_test", "transcript.jsonl")); err != nil {
		t.Fatal(err)
	}
}
