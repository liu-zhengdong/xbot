package bot

import (
	"errors"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestFirstRunSeedsPrimary(t *testing.T) {
	root := t.TempDir()
	b, created, err := EnsurePrimary(root, Seed{Links: []string{"terminal"}, Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if !created || b.Name() != SeedName || !b.Config().Pinned {
		t.Fatalf("created %v bot %+v", created, b.Config())
	}
	if got := b.Config().Owners; len(got) != 1 || got[0] != "u1" {
		t.Fatalf("owners %v", got)
	}
	name, err := Primary(root)
	if err != nil || name != SeedName {
		t.Fatalf("primary %q %v", name, err)
	}
	again, created, err := EnsurePrimary(root, Seed{Owner: "u1"})
	if err != nil || created || again.Name() != SeedName {
		t.Fatalf("second created %v name %s err %v", created, again.Name(), err)
	}
	all, err := List(root)
	if err != nil || len(all) != 1 {
		t.Fatalf("bots %+v %v", all, err)
	}
}

func TestUnaddressedEventGoesToPrimary(t *testing.T) {
	root := t.TempDir()
	if _, _, err := EnsurePrimary(root, Seed{Links: []string{"terminal"}, Owner: "u1"}); err != nil {
		t.Fatal(err)
	}
	other, err := Create(root, "ada", "persona", []string{"terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	ev := link.TerminalEvent("u1", "hello primary", time.Unix(10, 0).UTC())
	if err := PostUnaddressed(root, ev); err != nil {
		t.Fatal(err)
	}
	primary, err := Load(root, SeedName)
	if err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(primary.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := box.Head()
	if !ok || got.Content != "hello primary" {
		t.Fatalf("primary inbox %+v ok %v", got, ok)
	}
	otherBox, err := inbox.Open(other.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := otherBox.Head(); ok {
		t.Fatal("unaddressed event landed on ada")
	}
}

func TestDeletePrimaryTransferAndLastBot(t *testing.T) {
	root := t.TempDir()
	if _, _, err := EnsurePrimary(root, Seed{Links: []string{"terminal"}, Owner: "u1"}); err != nil {
		t.Fatal(err)
	}
	ada, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, SeedName); !errors.Is(err, ErrPrimary) {
		t.Fatalf("delete primary: %v", err)
	}
	if err := SetPrimary(root, "ada"); err != nil {
		t.Fatal(err)
	}
	if name, err := Primary(root); err != nil || name != "ada" {
		t.Fatalf("primary %q %v", name, err)
	}
	if !ada.Config().Pinned {
		reloaded, err := Load(root, "ada")
		if err != nil {
			t.Fatal(err)
		}
		if !reloaded.Config().Pinned {
			t.Fatal("new primary was not pinned")
		}
	}
	if err := Delete(root, SeedName); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, SeedName); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old primary still present: %v", err)
	}
	if err := Delete(root, "ada"); !errors.Is(err, ErrLast) {
		t.Fatalf("delete last: %v", err)
	}
	all, err := List(root)
	if err != nil || len(all) != 1 || all[0].Name != "ada" {
		t.Fatalf("bots %+v %v", all, err)
	}
}

func TestPinOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"ada", "bob", "cleo"} {
		b, err := Create(root, name, "persona", nil)
		if err != nil {
			t.Fatal(err)
		}
		if name != "ada" {
			if err := b.SetPinned(true); err != nil {
				t.Fatal(err)
			}
		}
	}
	all, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	ordered := Order(all)
	var names []string
	for _, cfg := range ordered {
		names = append(names, cfg.Name)
	}
	if strings.Join(names, ",") != "bob,cleo,ada" {
		t.Fatalf("order %s", names)
	}
}
