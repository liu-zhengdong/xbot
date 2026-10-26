package bot

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestProfileTextAndEmptyPrefix(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := b.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	composed, err := b.ComposedPersona()
	if err != nil || composed != file || b.ProfileText() != "" {
		t.Fatalf("empty profile changed persona %q", composed)
	}
	desc := "项目笔记"
	if err := b.ApplyProfile(ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if text := b.ProfileText(); text != "About: 项目笔记" {
		t.Fatal(text)
	}
	composed, err = b.ComposedPersona()
	if err != nil || composed != file+"\n\nAbout: 项目笔记" {
		t.Fatalf("composed %q", composed)
	}
}

// Speaking style and quiet hours are gone. Old configs still load, nothing of
// them reaches the prompt, and the next save drops the quiet hours.
func TestLegacyProactivityAndQuietHoursIgnored(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "ada", "Be brief.", nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(root, "ada"), "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["proactivity"] = "quiet"
	cfg["quiet_start"] = "22:00"
	cfg["quiet_end"] = "07:00"
	cfg["timezone"] = "UTC"
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config().Proactivity != "quiet" {
		t.Fatalf("stored field %q", loaded.Config().Proactivity)
	}
	if loaded.ProfileText() != "" {
		t.Fatal(loaded.ProfileText())
	}
	if err := loaded.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "quiet_start") || strings.Contains(string(raw), "quiet_end") {
		t.Fatal(string(raw))
	}
}

func TestAvatarColorBlockAndGenerator(t *testing.T) {
	bg, ink, initial := AvatarColors("ada")
	bg2, ink2, initial2 := AvatarColors("ada")
	if bg != bg2 || ink != ink2 || initial != "A" || initial2 != "A" || bg == ink {
		t.Fatalf("colors %s %s %s", bg, ink, initial)
	}
	if s := saturation(bg); s < 0.25 || s > 0.36 {
		t.Fatalf("bg saturation %v %s", s, bg)
	}
	if s := saturation(ink); s < 0.25 || s > 0.36 {
		t.Fatalf("ink saturation %v %s", s, ink)
	}
	if l := lightness(bg); l < 0.72 || l > 0.84 {
		t.Fatalf("bg lightness %v", l)
	}
	if l := lightness(ink); l < 0.22 || l > 0.36 {
		t.Fatalf("ink lightness %v", l)
	}
	b, err := Create(t.TempDir(), "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := b.AvatarBytes(); err != nil || ok {
		t.Fatalf("nil generator ok %v err %v", ok, err)
	}
	calls := 0
	restore := SetGenerateAvatar(func(name, description string) ([]byte, error) {
		calls++
		if name != "ada" {
			t.Fatalf("name %s", name)
		}
		return []byte("generated-png"), nil
	})
	defer restore()
	data, mime, ok, err := b.AvatarBytes()
	if err != nil || !ok || string(data) != "generated-png" || mime != "image/png" || calls != 1 {
		t.Fatalf("generated %q %s ok %v calls %d err %v", data, mime, ok, calls, err)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	if err := b.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	data, mime, ok, err = b.AvatarBytes()
	if err != nil || !ok || string(data) != string(png) || mime != "image/png" || calls != 1 {
		t.Fatalf("upload %q %s calls %d err %v", data, mime, calls, err)
	}
	if err := b.ClearAvatar(); err != nil {
		t.Fatal(err)
	}
	data, _, ok, err = b.AvatarBytes()
	if err != nil || !ok || string(data) != "generated-png" || calls != 2 {
		t.Fatalf("after clear %q ok %v calls %d", data, ok, calls)
	}
	restore()
	if _, _, ok, err = b.AvatarBytes(); err != nil || ok {
		t.Fatalf("generator removed ok %v", ok)
	}
}

func TestAvatarSeedLifecycle(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarGenerated || seed != "ada" {
		t.Fatalf("new bot %q %q", av, seed)
	}
	if err := Rename(root, "ada", "ada-notes"); err != nil {
		t.Fatal(err)
	}
	b, err = Load(root, "ada-notes")
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarGenerated || seed != "ada" {
		t.Fatalf("rename changed seed: %q %q", av, seed)
	}

	for _, bad := range []string{" ", strings.Repeat("周", 81), "<x>", "a\x07b", "a\"b", "a&b", "a\xff"} {
		if ValidAvatarSeed(bad) {
			t.Fatalf("accepted %q", bad)
		}
		if err := b.SetAvatarSeed(bad); err == nil {
			t.Fatalf("stored %q", bad)
		}
	}
	for _, good := range []string{"writer#3", "周报", AvatarGlyph, "5116ed70"} {
		if !ValidAvatarSeed(good) {
			t.Fatalf("rejected %q", good)
		}
	}
	if !ValidAvatarSeed(strings.Repeat("a", 80)) || ValidAvatarSeed(strings.Repeat("a", 81)) {
		t.Fatal("80 rune limit")
	}

	if err := b.SetAvatarSeed("  writer#3 "); err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarGenerated || seed != "writer#3" {
		t.Fatalf("seed %q %q", av, seed)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 7}
	if err := b.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarCustom || seed != "writer#3" {
		t.Fatalf("upload %q %q", av, seed)
	}
	if err := b.SetAvatarSeed("周报"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.AvatarPath()); !os.IsNotExist(err) {
		t.Fatalf("upload kept after new seed: %v", err)
	}
	if _, err := os.Stat(b.avatarMIMEPath()); !os.IsNotExist(err) {
		t.Fatalf("mime kept after new seed: %v", err)
	}
	if av, seed := b.AvatarState(); av != AvatarGenerated || seed != "周报" {
		t.Fatalf("after seed %q %q", av, seed)
	}
}

func TestUploadThenClearKeepsSeed(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatarSeed("writer#3"); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 3}
	if err := b.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarCustom || seed != "writer#3" {
		t.Fatalf("upload %q %q", av, seed)
	}
	if err := b.ClearAvatar(); err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarGenerated || seed != "writer#3" {
		t.Fatalf("cleared %q %q", av, seed)
	}
}

func TestOldConfigWithoutAvatar(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "ada", "Be brief.", nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(root, "ada"), "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	delete(cfg, "avatar")
	delete(cfg, "avatar_seed")
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := b.AvatarState(); av != AvatarGenerated || seed != "ada" {
		t.Fatalf("old config %q %q", av, seed)
	}
	// Its face came from the old name, so a rename must keep that seed.
	if err := Rename(root, "ada", "ada2"); err != nil {
		t.Fatal(err)
	}
	moved, err := Load(root, "ada2")
	if err != nil {
		t.Fatal(err)
	}
	if _, seed := moved.AvatarState(); seed != "ada" {
		t.Fatalf("rename of an old bot changed its seed to %q", seed)
	}
}

func TestExportImportOmitsMemoryAndTranscript(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("SECRET-TRANSCRIPT")}}); err != nil {
		t.Fatal(err)
	}
	if err := memory.Open(root).Remember(context.Background(), memory.Fact{
		Key: "secret", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: "ada", WrittenBy: "ada", Text: "SECRET-MEMORY", Time: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	desc := "项目笔记"
	if err := b.ApplyProfile(ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if err := b.WritePersona("Keep the notes short."); err != nil {
		t.Fatal(err)
	}
	if err := (&link.Timer{Dir: b.RoutineDir()}).Save(link.Routine{
		ID: "daily", Name: "morning", Prompt: "Check the board.", Cron: "0 * * * *", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 9}
	if err := b.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	tpl, err := ExportTemplate(b)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET-TRANSCRIPT") || strings.Contains(string(raw), "SECRET-MEMORY") {
		t.Fatalf("template leaked %s", raw)
	}
	tpl.Name = "ada2"
	next, err := ImportTemplate(root, tpl, []string{"terminal"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := next.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %d %v", len(entries), err)
	}
	facts, err := memory.Open(root).Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: "ada2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if strings.Contains(f.Text, "SECRET-MEMORY") {
			t.Fatal("memory copied")
		}
	}
	persona, _ := next.ReadPersona()
	if persona != "Keep the notes short." || next.ProfileText() != "About: 项目笔记" {
		t.Fatalf("persona %q profile %q", persona, next.ProfileText())
	}
	list, err := (&link.Timer{Dir: next.RoutineDir()}).List()
	if err != nil || len(list) != 1 || list[0].Prompt != "Check the board." || list[0].Cron != "0 * * * *" {
		t.Fatalf("routines %+v %v", list, err)
	}
	data, _, ok, err := next.AvatarBytes()
	if err != nil || !ok || string(data) != string(png) {
		t.Fatalf("avatar %q ok %v err %v", data, ok, err)
	}
	if av, seed := next.AvatarState(); av != AvatarCustom || seed != "ada" {
		t.Fatalf("custom template %q %q", av, seed)
	}
	if _, err := ImportTemplate(root, tpl, nil, ""); err == nil {
		t.Fatal("expected existing name to fail")
	}

	// A generated template carries the seed across a rename, and a bad seed
	// falls back to the new name.
	if err := b.SetAvatarSeed("writer#3"); err != nil {
		t.Fatal(err)
	}
	tpl, err = ExportTemplate(b)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Avatar != "" || tpl.AvatarSeed != "writer#3" {
		t.Fatalf("generated template %+v", tpl)
	}
	tpl.Name = "writer"
	next, err = ImportTemplate(root, tpl, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := next.AvatarState(); av != AvatarGenerated || seed != "writer#3" {
		t.Fatalf("generated round trip %q %q", av, seed)
	}
	tpl.Name, tpl.AvatarSeed = "notes", "<bad>"
	next, err = ImportTemplate(root, tpl, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if av, seed := next.AvatarState(); av != AvatarGenerated || seed != "notes" {
		t.Fatalf("bad seed %q %q", av, seed)
	}
}

func saturation(hex string) float64 {
	r, g, b := rgb(hex)
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l := (max + min) / 2
	if max == min {
		return 0
	}
	d := max - min
	if l > 0.5 {
		return d / (2 - max - min)
	}
	return d / (max + min)
}

func lightness(hex string) float64 {
	r, g, b := rgb(hex)
	return (math.Max(r, math.Max(g, b)) + math.Min(r, math.Min(g, b))) / 2
}

func rgb(hex string) (float64, float64, float64) {
	hex = strings.TrimPrefix(hex, "#")
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return float64((n>>16)&0xff) / 255, float64((n>>8)&0xff) / 255, float64(n&0xff) / 255
}
