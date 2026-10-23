package bot

import (
	"errors"
	"testing"
	"time"

	"xbot/internal/provider"
)

func TestCreateLoadAppend(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "Be brief.", []string{"terminal", "tools"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "ada" {
		t.Fatalf("name %s", b.Name())
	}
	if _, err := Create(root, "ada", "x", nil); !errors.Is(err, ErrExists) {
		t.Fatalf("exists: %v", err)
	}
	if _, err := Create(root, "../x", "x", nil); err == nil {
		t.Fatal("expected invalid name")
	}
	loaded, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config().Persona != "Be brief." || !loaded.Grants("tools") {
		t.Fatalf("%+v", loaded.Config())
	}
	if err := loaded.Append(Entry{
		Time:    time.Unix(1, 0).UTC(),
		EventID: "ev1",
		Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hi")},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := loaded.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Message.Text() != "hi" {
		t.Fatalf("%+v", entries)
	}
	raw, err := loaded.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Append(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("yo")}}); err != nil {
		t.Fatal(err)
	}
	raw2, err := loaded.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw2) <= len(raw) || string(raw2[:len(raw)]) != string(raw) {
		t.Fatal("transcript was not append-only")
	}
	listed, err := List(root)
	if err != nil || len(listed) != 1 || listed[0].Name != "ada" {
		t.Fatalf("%v %+v", err, listed)
	}
	if _, err := Load(root, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
