package main

import (
	"testing"
	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestMountIncludesEveryOrganWithoutGrants(t *testing.T) {
	b, err := bot.Create(t.TempDir(), "ada", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	mounted, err := mount(b, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, l := range mounted {
		got[l.Name()] = true
	}
	for _, organ := range link.Organs() {
		if !got[organ.Grant] {
			t.Errorf("organ %s missing from CLI mount", organ.ID)
		}
	}
	if got[link.NameBrowser] {
		t.Fatal("browser mounted without a grant")
	}
}
