package web

import (
	"testing"

	"xbot/internal/person"
	"xbot/internal/remote"
)

func TestAccountDeviceNameDefaultAndSaved(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root}
	if got := s.selfDeviceName(); got != remote.DefaultDeviceName() {
		t.Fatalf("fallback name = %q, want %q", got, remote.DefaultDeviceName())
	}
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	s.owner = p.ID
	if got := s.selfDeviceName(); got != remote.DefaultDeviceName() {
		t.Fatalf("default account name = %q, want %q", got, remote.DefaultDeviceName())
	}
	const saved = "政东的 MacBook Pro.local"
	if err := remote.SetSelfName(root, saved); err != nil {
		t.Fatal(err)
	}
	if got := s.selfDeviceName(); got != saved {
		t.Fatalf("saved account name changed to %q", got)
	}
}
