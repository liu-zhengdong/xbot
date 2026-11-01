package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
)

func TestParseProfileAvatarRuntimeArguments(t *testing.T) {
	file, err := parseProfileAvatar([]string{"--self", "--file", "face.png", "--json"})
	if err != nil || !file.Self || !file.JSON || file.Clear || file.File != "face.png" || file.Bot != "" {
		t.Fatalf("%+v %v", file, err)
	}
	clear, err := parseProfileAvatar([]string{"--clear", "--json", "ada"})
	if err != nil || clear.Self || !clear.JSON || !clear.Clear || clear.Bot != "ada" {
		t.Fatalf("%+v %v", clear, err)
	}
	for _, args := range [][]string{{"--self", "ada", "--clear"}, {"--self"}, {"--clear"}, {"--self", "--file", "a", "--clear"}, {"--self", "--clear", "--description", "x"}, {"--self", "--file", "a", "--avatar-seed", "seed"}} {
		if _, err := parseProfileAvatar(args); err == nil {
			t.Fatal("invalid avatar argument combination accepted", args)
		}
	}
}

func commandProfileAvatarPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	state := uint32(41)
	for i := 0; i < len(img.Pix); i += 4 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(state), byte(state>>8), byte(state>>16), 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() <= 64<<10 {
		t.Fatal("fixture must exceed JSON metadata budget")
	}
	return encoded.Bytes()
}

func TestProfileAvatarRuntimeRawAndOfflineCompatibility(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "fixture persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := 0
	profile := func() cliops.Profile {
		avatar, seed := b.AvatarState()
		return cliops.Profile{Name: b.Name(), Avatar: avatar, AvatarSeed: seed, Scope: "runtime"}
	}
	server, err := cliops.Start(root, cliops.Handlers{
		Profile: func(*cliops.Update) (cliops.Profile, error) { mu.Lock(); defer mu.Unlock(); return profile(), nil },
		ProfileAvatar: func(data []byte, clear bool) (cliops.Profile, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			result, err := b.ApplyAvatarChange(data, clear)
			value := profile()
			value.PreviousName = result.PreviousName
			value.AppliedFields = result.AppliedFields
			value.Applied = len(result.AppliedFields) != 0
			return value, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.Endpoint)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	data := commandProfileAvatarPNG(t)
	file := filepath.Join(t.TempDir(), "face.png")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() error { return cmdProfile(root, []string{"avatar", "--self", "--file", file, "--json"}) })
	var value cliops.Profile
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatal(err)
	}
	if value.Name != "ada" || value.Scope != "runtime" || value.Avatar != bot.AvatarCustom || len(value.AppliedFields) != 3 || len(out) > 64<<10 || strings.Contains(out, "iVBOR") {
		t.Fatalf("upload metadata=%+v", value)
	}
	stored, mime, custom, err := b.AvatarBytes()
	if err != nil || !custom || mime != "image/png" || !bytes.Equal(stored, data) {
		t.Fatal("CLI file bytes did not reach the domain unchanged", err)
	}
	out = captureStdout(t, func() error { return cmdProfile(root, []string{"avatar", "--self", "--clear", "--json"}) })
	if err := json.Unmarshal([]byte(out), &value); err != nil || value.Avatar != bot.AvatarGenerated || value.AvatarSeed != "ada" {
		t.Fatalf("clear=%+v error=%v", value, err)
	}
	if err := cmdProfile(root, []string{"avatar", "another", "--file", file}); err == nil || !strings.Contains(err.Error(), "another Bot") {
		t.Fatal("explicit foreign Bot accepted", err)
	}
	if err := cmdProfile(t.TempDir(), []string{"avatar", "--self", "--clear"}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatal("wrong root accepted", err)
	}
	// Without an endpoint the explicit-name path continues to edit offline.
	t.Setenv("XBOT_ENDPOINT", "")
	out = captureStdout(t, func() error { return cmdProfile(root, []string{"avatar", "ada", "--file", file, "--json"}) })
	if err := json.Unmarshal([]byte(out), &value); err != nil || value.Scope != "offline" || value.Avatar != bot.AvatarCustom {
		t.Fatalf("offline=%+v error=%v", value, err)
	}
	out = captureStdout(t, func() error { return cmdProfile(root, []string{"avatar", "ada", "--clear"}) })
	if !strings.Contains(out, "cleared avatar for ada") {
		t.Fatal(out)
	}
	fresh, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if avatar, seed := fresh.AvatarState(); avatar != bot.AvatarGenerated || seed != "ada" {
		t.Fatal("offline clear changed seed", avatar, seed)
	}
	if err := cmdProfile(root, []string{"avatar", "--self", "--clear"}); err == nil {
		t.Fatal("self fell back to offline configuration")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatal("rejected/offline request reached the runtime", calls)
	}
}

func TestProfileAvatarPartialOutputAndFileLimit(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	server, err := cliops.Start(root, cliops.Handlers{ProfileAvatar: func([]byte, bool) (cliops.Profile, error) {
		calls.Add(1)
		return cliops.Profile{Name: "ada", Scope: "runtime", Avatar: bot.AvatarGenerated, Applied: true, AppliedFields: []string{"avatar_file"}}, errors.New("fixture: later MIME write failed")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.Endpoint)
	file := filepath.Join(t.TempDir(), "face.png")
	if err := os.WriteFile(file, commandProfileAvatarPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	var callErr error
	out := captureStdout(t, func() error {
		callErr = cmdProfile(root, []string{"avatar", "--self", "--file", file, "--json"})
		return nil
	})
	var actual cliops.Profile
	if err := json.Unmarshal([]byte(out), &actual); err != nil {
		t.Fatal("partial metadata was not printed", err)
	}
	if callErr == nil || actual.Name != "ada" || actual.Avatar != bot.AvatarGenerated || len(actual.AppliedFields) != 1 || actual.AppliedFields[0] != "avatar_file" || actual.Error == "" {
		t.Fatalf("partial=%+v error=%v", actual, callErr)
	}
	oversized := filepath.Join(t.TempDir(), "oversized.png")
	large, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(cliops.MaxAvatarBody + 1); err != nil {
		_ = large.Close()
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() error {
		callErr = cmdProfile(root, []string{"avatar", "--self", "--file", oversized, "--json"})
		return nil
	})
	if callErr == nil || out != "" || calls.Load() != 1 {
		t.Fatal("oversized file was uploaded or produced a false result", callErr, calls.Load())
	}
}
