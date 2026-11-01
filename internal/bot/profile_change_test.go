package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"xbot/internal/work"
)

func normalizedChange(t *testing.T, change ProfileChange) ProfileChange {
	t.Helper()
	normalized, err := ValidateProfileChange(change)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}

func TestProfileChangeCopiesAndNormalizesBeforeIO(t *testing.T) {
	name, description, zone, seed := " next ", "one\n two", " UTC ", " seed#2 "
	change := normalizedChange(t, ProfileChange{Name: &name, Description: &description, Timezone: &zone, AvatarSeed: &seed})
	name, description, zone, seed = "bad/name", "changed", "bad/timezone", "<bad>"
	if *change.Name != "next" || *change.Description != "one two" || *change.Timezone != "UTC" || *change.AvatarSeed != "seed#2" {
		t.Fatalf("normalization aliases caller: %+v", change)
	}
	if _, err := ValidateProfileChange(ProfileChange{Name: &name}); err == nil {
		t.Fatal("invalid name accepted")
	}
	if _, err := ValidateProfileChange(ProfileChange{Timezone: &zone}); err == nil {
		t.Fatal("invalid zone accepted")
	}
}

func TestProfileChangeRenameMergesFreshConfigOnce(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "old", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPrimary(root, "old"); err != nil {
		t.Fatal(err)
	}
	cfg, err := readProfileConfig(root, "old")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Role, cfg.BackgroundMaxTime, cfg.Model, cfg.PersonaCard = RoleDev, 321, "fresh-model", "fresh-card"
	cfg.DisabledSkills = []string{"keep-skill"}
	cfg.Avatar, cfg.AvatarSeed = AvatarCustom, ""
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(Dir(root, "old"), "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work.BotDir(root, "old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work.BotDir(root, "old"), "keep"), []byte("workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	name, description, zone, seed := "new", "new description", "Asia/Shanghai", "new#2"
	current, result, err := b.ApplyProfileChange(normalizedChange(t, ProfileChange{Name: &name, Description: &description, Timezone: &zone, AvatarSeed: &seed}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Moved || result.Name != "new" || result.PreviousName != "old" || !reflect.DeepEqual(result.AppliedFields, []string{"name", "description", "timezone", "avatar_seed"}) {
		t.Fatalf("receipt: %+v", result)
	}
	fresh := current.Config()
	if fresh.Description != description || fresh.Timezone != zone || fresh.AvatarSeed != seed || fresh.Avatar != AvatarGenerated || fresh.Role != RoleDev || fresh.BackgroundMaxTime != 321 || fresh.Model != "fresh-model" || fresh.PersonaCard != "fresh-card" || !reflect.DeepEqual(fresh.DisabledSkills, []string{"keep-skill"}) || !fresh.Pinned {
		t.Fatalf("fresh merge lost fields: %+v", fresh)
	}
	if primary, err := Primary(root); err != nil || primary != "new" {
		t.Fatalf("primary: %q %v", primary, err)
	}
	if data, err := os.ReadFile(filepath.Join(work.BotDir(root, "new"), "keep")); err != nil || string(data) != "workspace" {
		t.Fatalf("work: %s %v", data, err)
	}
	if _, err := os.Stat(Dir(root, "old")); !os.IsNotExist(err) {
		t.Fatalf("old path: %v", err)
	}
}

func TestProfileChangeConflictLeavesLegacyConfigUntouched(t *testing.T) {
	for _, resource := range []string{"bot", "work", "key"} {
		t.Run(resource, func(t *testing.T) {
			root := t.TempDir()
			b, err := Create(root, "old", "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			cfg, _ := readProfileConfig(root, "old")
			cfg.AvatarSeed = ""
			raw, _ := json.Marshal(cfg)
			configPath := filepath.Join(Dir(root, "old"), "config.json")
			if err := os.WriteFile(configPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			switch resource {
			case "bot":
				if err := os.MkdirAll(Dir(root, "next"), 0700); err != nil {
					t.Fatal(err)
				}
			case "work":
				if err := os.MkdirAll(work.BotDir(root, "next"), 0700); err != nil {
					t.Fatal(err)
				}
			case "key":
				if err := os.MkdirAll(filepath.Dir(vaultKeyPath(root, "next")), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(vaultKeyPath(root, "next"), []byte("occupied"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			next, description := "next", "must not apply"
			change := normalizedChange(t, ProfileChange{Name: &next, Description: &description})
			if err := CheckProfileChange(root, "old", change); !errors.Is(err, ErrExists) {
				t.Fatalf("preflight: %v", err)
			}
			_, result, err := b.ApplyProfileChange(change)
			if !errors.Is(err, ErrExists) || result.Moved || len(result.AppliedFields) != 0 {
				t.Fatalf("receipt: %+v %v", result, err)
			}
			after, err := os.ReadFile(configPath)
			if err != nil || !bytes.Equal(raw, after) {
				t.Fatalf("conflict changed legacy config: %s %v", after, err)
			}
		})
	}
}

func TestProfileChangeLateFailureReportsCommittedFields(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "old", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	// This is an actual IO failure after the merged config is committed.
	if err := os.Mkdir(primaryPath(root), 0700); err != nil {
		t.Fatal(err)
	}
	next, description := "next", "already persisted"
	current, result, err := b.ApplyProfileChange(normalizedChange(t, ProfileChange{Name: &next, Description: &description}))
	if err == nil || result.Moved || result.Name != "old" || !reflect.DeepEqual(result.AppliedFields, []string{"description"}) {
		t.Fatalf("partial result: %+v %v", result, err)
	}
	fresh, readErr := readProfileConfig(root, "old")
	if readErr != nil || fresh.Name != "old" || fresh.Description != description || current.Name() != "old" {
		t.Fatalf("partial persisted: %+v %v", fresh, readErr)
	}
	if got, _ := b.ProfileValues(); got != description {
		t.Fatal("source snapshot lost committed fields")
	}
	if _, err := os.Stat(Dir(root, "next")); !os.IsNotExist(err) {
		t.Fatalf("rollback path: %v", err)
	}
}

func TestProfileChangeSameNameAndCaseOnlyWork(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "MixedCase", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, moved, err := RenameWithResult(root, b.Name(), b.Name()); err != nil || moved {
		t.Fatalf("same-name move: %v %v", moved, err)
	}
	if err := os.MkdirAll(work.BotDir(root, b.Name()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work.BotDir(root, b.Name()), "keep"), []byte("work"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(vaultKeyPath(root, b.Name())), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultKeyPath(root, b.Name()), []byte("key"), 0600); err != nil {
		t.Fatal(err)
	}
	next := "mixedcase"
	current, result, err := b.ApplyProfileChange(normalizedChange(t, ProfileChange{Name: &next}))
	if err != nil || !result.Moved || current.Name() != next {
		t.Fatalf("case move: %+v %v", result, err)
	}
	if data, err := os.ReadFile(filepath.Join(work.BotDir(root, next), "keep")); err != nil || string(data) != "work" {
		t.Fatalf("case work: %s %v", data, err)
	}
	if data, err := os.ReadFile(vaultKeyPath(root, next)); err != nil || string(data) != "key" {
		t.Fatalf("case key: %s %v", data, err)
	}
}

func TestProfileChangeWithoutNameAndDisplaySnapshot(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "old", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := &Bot{Root: root, cfg: b.Config(), profile: newProfileState(b.Config())}
	description := "display update"
	current, result, err := b.ApplyProfileChange(normalizedChange(t, ProfileChange{Description: &description}))
	if err != nil || result.Moved || !reflect.DeepEqual(result.AppliedFields, []string{"description"}) {
		t.Fatalf("ordinary update: %+v %v", result, err)
	}
	target.RefreshProfileFrom(current)
	if got, _ := target.ProfileValues(); got != description || target.Name() != "old" {
		t.Fatal("display snapshot did not refresh")
	}
}
