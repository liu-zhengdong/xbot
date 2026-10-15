package bot

import (
	"context"
	"encoding/json"
	"math"
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
	tone, length, emoji, lang := "warm and plain", "short", "none", "中文"
	pro, start, end := "quiet", "22:00", "07:00"
	desc := "项目笔记"
	if err := b.ApplyProfile(ProfileUpdate{
		Description: &desc, Tone: &tone, Length: &length, Emoji: &emoji, Language: &lang,
		Proactivity: &pro, QuietStart: &start, QuietEnd: &end,
	}); err != nil {
		t.Fatal(err)
	}
	text := b.ProfileText()
	for _, want := range []string{
		"About: 项目笔记",
		"Tone: warm and plain",
		"Reply length: short. A few sentences at most.",
		"Emoji: do not use emoji.",
		"Language: reply in 中文.",
		"Proactivity: quiet.",
		"Quiet hours: 22:00–07:00 UTC.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	composed, err = b.ComposedPersona()
	if err != nil || !strings.HasPrefix(composed, file+"\n\n") {
		t.Fatalf("composed %q", composed)
	}
	cleared := ""
	if err := b.ApplyProfile(ProfileUpdate{Tone: &cleared}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.ProfileText(), "Tone:") {
		t.Fatal(b.ProfileText())
	}
}

func TestAllowsNotifyAndQuietWindow(t *testing.T) {
	if AllowsNotify("quiet", "normal") || !AllowsNotify("quiet", "high") {
		t.Fatal("quiet threshold")
	}
	if AllowsNotify("normal", "low") || !AllowsNotify("normal", "normal") || !AllowsNotify("normal", "high") {
		t.Fatal("normal threshold")
	}
	if !AllowsNotify("active", "low") || !AllowsNotify("", "normal") || AllowsNotify("", "low") {
		t.Fatal("active or default threshold")
	}
	day := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	night := time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC)
	early := time.Date(2026, 10, 16, 6, 59, 0, 0, time.UTC)
	morning := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	if InQuiet("22:00", "07:00", day) || !InQuiet("22:00", "07:00", night) || !InQuiet("22:00", "07:00", early) || InQuiet("22:00", "07:00", morning) {
		t.Fatal("midnight window")
	}
	if !InQuiet("09:00", "17:00", day) || InQuiet("09:00", "17:00", night) {
		t.Fatal("day window")
	}
	if InQuiet("", "", night) || InQuiet("09:00", "09:00", day) {
		t.Fatal("disabled window")
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

func TestExportImportOmitsMemoryAndTranscript(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: "SECRET-TRANSCRIPT"}}); err != nil {
		t.Fatal(err)
	}
	if err := memory.Open(root).Remember(context.Background(), memory.Fact{
		Key: "secret", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: "ada", WrittenBy: "ada", Text: "SECRET-MEMORY", Time: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	tone := "warm"
	if err := b.ApplyProfile(ProfileUpdate{Tone: &tone}); err != nil {
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
	if persona != "Keep the notes short." || !strings.Contains(next.ProfileText(), "Tone: warm") {
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
	if _, err := ImportTemplate(root, tpl, nil, ""); err == nil {
		t.Fatal("expected existing name to fail")
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
