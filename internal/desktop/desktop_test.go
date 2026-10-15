package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fake struct {
	idle   time.Duration
	idleOK bool
	shot   string
	click  string
	text   string
	dy     int
	key    string
}

func (f *fake) Screenshot(_ context.Context, path string) error {
	f.shot = path
	if err := os.WriteFile(path, []byte("png"), 0644); err != nil {
		return err
	}
	return os.Chmod(path, 0644)
}
func (f *fake) Click(_ context.Context, x, y int) error {
	f.click = strconv.Itoa(x) + " " + strconv.Itoa(y)
	return nil
}
func (f *fake) Type(_ context.Context, text string) error { f.text = text; return nil }
func (f *fake) Scroll(_ context.Context, dy int) error    { f.dy = dy; return nil }
func (f *fake) Key(_ context.Context, key string) error   { f.key = key; return nil }
func (f *fake) Idle(context.Context) (time.Duration, bool) {
	return f.idle, f.idleOK
}

func TestPerformSwitchAndIdle(t *testing.T) {
	root := t.TempDir()
	f := &fake{idle: 10 * time.Second, idleOK: true}
	path, err := Perform(context.Background(), root, f, Action{Name: "screenshot"}, false)
	if err != nil || !strings.HasPrefix(path, filepath.Join(root, "desktop")) {
		t.Fatalf("%q %v", path, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("screenshot mode %v %v", fi, err)
	}
	if _, err := Perform(context.Background(), root, f, Action{Name: "click", X: 3, Y: 4}, true); err != nil || f.click != "3 4" {
		t.Fatalf("%q %v", f.click, err)
	}
	f.idle = time.Second
	if _, err := Perform(context.Background(), root, f, Action{Name: "type", Text: "hi"}, true); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "machines.json"), []byte(`{"machines":{"local":"off"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(context.Background(), root, f, Action{Name: "screenshot"}, true); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "machines.json"), []byte(`{"machines":{"local":"ask"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(context.Background(), root, f, Action{Name: "screenshot"}, false); err == nil || !strings.Contains(err.Error(), "ask") {
		t.Fatal(err)
	}
	if _, err := Perform(context.Background(), root, f, Action{Name: "key", Key: "rm -rf"}, true); err == nil {
		t.Fatal("bad key accepted")
	}
}

func TestToolConfirm(t *testing.T) {
	root := t.TempDir()
	l := Open(root)
	l.Driver = &fake{idle: time.Minute, idleOK: true}
	need, _, err := (shotTool{l}).Confirm(nil)
	if err != nil || need {
		t.Fatalf("shot need=%v %v", need, err)
	}
	need, summary, err := (typeTool{l}).Confirm(json.RawMessage(`{"text":"pay now"}`))
	if err != nil || !need || !strings.Contains(summary, "pay now") || !strings.Contains(summary, "payment") {
		t.Fatalf("need=%v summary=%q %v", need, summary, err)
	}
	need, summary, err = (clickTool{l}).Confirm(json.RawMessage(`{"x":8,"y":9}`))
	if err != nil || !need || !strings.Contains(summary, "desktop click 8 9") {
		t.Fatalf("click summary %q %v", summary, err)
	}
	need, summary, err = (keyTool{l}).Confirm(json.RawMessage(`{"key":"Return"}`))
	if err != nil || !need || !strings.Contains(summary, "Return") {
		t.Fatalf("key summary %q %v", summary, err)
	}
	need, summary, err = (typeTool{l}).Confirm(json.RawMessage(`{"text":"hello"}`))
	if err != nil || !need || !strings.Contains(summary, "hello") || strings.Contains(summary, "payment") {
		t.Fatalf("type summary %q %v", summary, err)
	}
	out, err := (clickTool{l}).Run(context.Background(), json.RawMessage(`{"x":8,"y":9}`))
	if err != nil || !strings.Contains(out, "clicked 8 9") {
		t.Fatalf("%q %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(root, "machines.json"), []byte(`{"machines":{"local":"off"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = (shotTool{l}).Confirm(nil)
	if err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
}

func TestCommandRecordsArgs(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "xdotool")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(log) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := run(ctx, bin, "mousemove", "1", "2", "click", "1"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "mousemove\n1\n2\nclick\n1" {
		t.Fatalf("%q", raw)
	}
	if err := run(ctx, bin+"-missing", "key"); err == nil || !strings.Contains(err.Error(), "desktop:") {
		t.Fatal(err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
