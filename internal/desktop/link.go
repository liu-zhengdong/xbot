package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"xbot/internal/link"
	"xbot/internal/tools"
)

// Link is the desktop organ: on from boot, its tools behind the tools door.
type Link struct {
	Root   string
	Driver Driver
}

// Open binds desktop actions to this data root.
func Open(root string) *Link { return &Link{Root: root} }

func (l *Link) Name() string { return link.NameDesktop }

func (l *Link) Tools() []tools.Tool {
	if l == nil {
		return nil
	}
	return []tools.Tool{
		shotTool{l}, snapshotTool{l}, setValueTool{l}, clickTool{l}, typeTool{l}, scrollTool{l}, keyTool{l},
	}
}

func (l *Link) driver() Driver {
	if l != nil && l.Driver != nil {
		return l.Driver
	}
	return defaultDriver()
}

type shotTool struct{ l *Link }

func (shotTool) Name() string { return "desktop_screenshot" }
func (shotTool) Description() string {
	return "Save a PNG of the local screen under the data root. Prefer a shell command or an API when that can answer the question. The machine switch can refuse this."
}
func (shotTool) Parameters() any { return obj(nil, nil) }
func (t shotTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "desktop screenshot", nil
}
func (t shotTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	return Perform(ctx, t.l.Root, t.l.driver(), Action{Name: "screenshot"}, true)
}

type clickTool struct{ l *Link }

func (clickTool) Name() string { return "desktop_click" }
func (clickTool) Description() string {
	return "Click a qualified ref from desktop_snapshot on Mac/Windows; x,y are Linux-only. Snapshot again to verify. Never retry uncertain delivery without observing. Prefer shell/API."
}
func (clickTool) Parameters() any {
	required := []string{"x", "y"}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		required = []string{"ref"}
	}
	return obj(map[string]any{
		"ref": map[string]any{"type": "string", "description": "Qualified ref from desktop_snapshot (Mac/Windows)"},
		"x":   map[string]any{"type": "integer", "description": "Pixels from the left (Linux)"},
		"y":   map[string]any{"type": "integer", "description": "Pixels from the top"},
	}, required)
}
func (t clickTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		X   int    `json:"x"`
		Y   int    `json:"y"`
		Ref string `json:"ref"`
	}
	_ = json.Unmarshal(args, &in)
	if in.Ref != "" {
		return false, "desktop click " + in.Ref, nil
	}
	return false, fmt.Sprintf("desktop click %d %d", in.X, in.Y), nil
}
func (t clickTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.l.performArgs(ctx, args, "click")
}

type typeTool struct{ l *Link }

func (typeTool) Name() string { return "desktop_type" }
func (typeTool) Description() string {
	return "Insert text into a desktop_snapshot ref on Mac/Windows (Windows uses physical input); Linux types into the focused window. Snapshot again to verify; do not retry uncertain delivery. Prefer shell/API."
}
func (typeTool) Parameters() any {
	required := []string{"text"}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		required = append(required, "ref")
	}
	return obj(map[string]any{
		"ref":  map[string]any{"type": "string", "description": "Qualified ref (required on Mac/Windows)"},
		"text": map[string]any{"type": "string", "description": "Text to type"},
	}, required)
}
func (t typeTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Text string `json:"text"`
		Ref  string `json:"ref"`
	}
	_ = json.Unmarshal(args, &in)
	summary := "desktop type " + in.Ref + "\n" + in.Text
	if sensitive(in.Text) {
		summary += "\npayment, login, or send"
	}
	return false, summary, nil
}
func (t typeTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.l.performArgs(ctx, args, "type")
}

type scrollTool struct{ l *Link }

func (scrollTool) Name() string { return "desktop_scroll" }
func (scrollTool) Description() string {
	return "Scroll a desktop_snapshot ref on Mac/Windows, or the focused Linux window. Positive dy is up, negative down, at most 20 steps. Snapshot again to verify. Yields when owner activity is detectable."
}
func (scrollTool) Parameters() any {
	required := []string{"dy"}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		required = append(required, "ref")
	}
	return obj(map[string]any{
		"ref": map[string]any{"type": "string", "description": "Qualified ref (required on Mac/Windows)"},
		"dy":  map[string]any{"type": "integer", "description": "Steps, -20..-1 or 1..20. Positive is up."},
		"x":   map[string]any{"type": "integer", "description": "Pixels from the left. Omit to keep the pointer."},
		"y":   map[string]any{"type": "integer", "description": "Pixels from the top. Omit to keep the pointer."},
	}, required)
}
func (t scrollTool) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := scrollArgs(args)
	if err != nil {
		return false, "", err
	}
	return false, scrollSummary(in), nil
}
func (t scrollTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.l.performArgs(ctx, args, "scroll")
}

type keyTool struct{ l *Link }

func (keyTool) Name() string { return "desktop_key" }
func (keyTool) Description() string {
	return "Press a key chord such as Return or ctrl+l in app (required on Mac/Windows). Snapshot afterwards to verify. Prefer shell/API."
}
func (keyTool) Parameters() any {
	required := []string{"key"}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		required = append(required, "app")
	}
	return obj(map[string]any{
		"app": map[string]any{"type": "string", "description": "Target app (required on Mac/Windows)"},
		"key": map[string]any{"type": "string", "description": "Key name"},
	}, required)
}
func (t keyTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(args, &in)
	summary := "desktop key " + in.Key
	if sensitive(in.Key) {
		summary += "\npayment, login, or send"
	}
	return false, summary, nil
}
func (t keyTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.l.performArgs(ctx, args, "key")
}

type scrollIn struct {
	dy    int
	x, y  int
	hasXY bool
}

func scrollArgs(args json.RawMessage) (scrollIn, error) {
	var in struct {
		Dy *int `json:"dy"`
		X  *int `json:"x"`
		Y  *int `json:"y"`
	}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return scrollIn{}, err
		}
	}
	got := scrollIn{}
	if in.Dy != nil {
		got.dy = *in.Dy
	}
	if in.X != nil || in.Y != nil {
		got.hasXY = true
		if in.X != nil {
			got.x = *in.X
		}
		if in.Y != nil {
			got.y = *in.Y
		}
	}
	return got, nil
}

func scrollSummary(in scrollIn) string {
	dir := "up"
	n := in.dy
	if in.dy < 0 {
		dir = "down"
		n = -in.dy
	}
	pos := "pointer"
	if in.hasXY {
		pos = fmt.Sprintf("%d %d", in.x, in.y)
	}
	return fmt.Sprintf("desktop scroll %s %d at %s", dir, n, pos)
}

func obj(props map[string]any, required []string) map[string]any {
	if required == nil {
		required = []string{}
	}
	if props == nil {
		props = map[string]any{}
	}
	return map[string]any{
		"type": "object", "properties": props, "required": required, "additionalProperties": false,
	}
}

func sensitive(text string) bool {
	s := strings.ToLower(text)
	for _, word := range []string{"pay", "password", "login", "付款", "登录", "发消息"} {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}
