package bot

import (
	"fmt"
	"testing"
	"time"
)

// Separate API/CLI handles must merge only explicitly supplied fields. An old
// loaded handle saving another setting must not roll back a live profile edit.
func TestProfileUpdatesMergeAcrossLoadedHandles(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	desc, tz := "runtime description", "Asia/Shanghai"
	if err := a.ApplyProfile(ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if err := b.ApplyProfile(ProfileUpdate{Timezone: &tz}); err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if cfg := fresh.Config(); cfg.Description != desc || cfg.Timezone != tz {
		t.Fatalf("lost profile edit: %+v", cfg)
	}
	if err := a.SetPinned(true); err != nil {
		t.Fatal(err)
	}
	fresh, err = Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if cfg := fresh.Config(); cfg.Description != desc || cfg.Timezone != tz || !cfg.Pinned {
		t.Fatalf("stale save reverted profile: %+v", cfg)
	}
}

func TestProfileConcurrentSaveDoesNotDeadlock(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		<-start
		for i := 0; i < 40; i++ {
			if err := b.SaveConfig(); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	go func() {
		<-start
		for i := 0; i < 40; i++ {
			desc := fmt.Sprintf("profile %d", i)
			if err := b.ApplyProfile(ProfileUpdate{Description: &desc}); err != nil {
				done <- err
				return
			}
			_ = b.ProfileText()
		}
		done <- nil
	}()
	close(start)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("profile/config save lock inversion")
		}
	}
	if desc, _ := b.ProfileValues(); desc != "profile 39" {
		t.Fatal(desc)
	}
}

func TestProfileInvalidUpdateDoesNotPublish(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	desc, bad := "must not publish", "Invalid/Timezone"
	if err := b.ApplyProfile(ProfileUpdate{Description: &desc, Timezone: &bad}); err == nil {
		t.Fatal("invalid timezone succeeded")
	}
	if b.Config().Description != "" || b.ProfileText() != "" {
		t.Fatal("failed update published in memory")
	}
}
