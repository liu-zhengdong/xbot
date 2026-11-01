package main

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"xbot/internal/cliops"
)

func TestProfileRuntimeRenameCombinedAndSelfFollowsEndpoint(t *testing.T) {
	root := t.TempDir()
	var mu sync.Mutex
	state := cliops.Profile{Name: "ada", Scope: "runtime"}
	updates := 0
	server, err := cliops.Start(root, cliops.Handlers{Profile: func(update *cliops.Update) (cliops.Profile, error) {
		mu.Lock()
		defer mu.Unlock()
		if update != nil {
			if update.Name == nil || update.Description == nil || update.Timezone == nil || update.AvatarSeed == nil {
				return state, errors.New("combined fields missing")
			}
			state.PreviousName = state.Name
			state.Name = *update.Name
			state.Description = *update.Description
			state.Timezone = *update.Timezone
			state.AvatarSeed = *update.AvatarSeed
			state.Moved = true
			state.Applied = true
			state.AppliedFields = []string{"name", "description", "timezone", "avatar_seed"}
			updates++
		}
		return state, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.Endpoint)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	out := captureStdout(t, func() error {
		return cmdProfile(root, []string{"set", "--self", "--name", "notes", "--description", "combined description", "--timezone", "UTC", "--avatar-seed", "notes#1", "--json"})
	})
	var result cliops.Profile
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "notes" || result.PreviousName != "ada" || !result.Moved || len(result.AppliedFields) != 4 || result.Description != "combined description" || result.Timezone != "UTC" || result.AvatarSeed != "notes#1" {
		t.Fatalf("%+v", result)
	}
	out = captureStdout(t, func() error { return cmdProfile(root, []string{"show", "--self", "--json"}) })
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Name != "notes" {
		t.Fatalf("show=%s error=%v", out, err)
	}
	if err := cmdProfile(root, []string{"set", "another", "--name", "wrong"}); err == nil || !strings.Contains(err.Error(), "another Bot") {
		t.Fatal("explicit other name accepted", err)
	}
	if err := cmdProfile(t.TempDir(), []string{"set", "--self", "--name", "wrong"}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatal("wrong root accepted", err)
	}
	for _, flag := range []string{"--role", "--background-max-time"} {
		if err := cmdProfile(root, []string{"set", "--self", flag, "1"}); err == nil {
			t.Fatal("runtime-only boundary changed", flag)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if updates != 1 || state.Name != "notes" {
		t.Fatal("rejected request reached mutation", updates, state.Name)
	}
}

func TestProfileRuntimePartialPrintsActualResultBeforeError(t *testing.T) {
	root := t.TempDir()
	server, err := cliops.Start(root, cliops.Handlers{Profile: func(*cliops.Update) (cliops.Profile, error) {
		return cliops.Profile{Name: "notes", PreviousName: "ada", Scope: "runtime", Moved: true, Applied: true, AppliedFields: []string{"name", "description"}}, errors.New("fixture: late I/O failure")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.Endpoint)
	var callErr error
	out := captureStdout(t, func() error {
		callErr = cmdProfile(root, []string{"set", "--self", "--name", "notes", "--json"})
		return nil
	})
	var result cliops.Profile
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal("partial result was not printed", err)
	}
	if callErr == nil || result.Name != "notes" || result.PreviousName != "ada" || !result.Moved || len(result.AppliedFields) != 2 || result.Error == "" {
		t.Fatalf("actual=%+v error=%v", result, callErr)
	}
}
