package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/tools"
)

// Link is the desktop grant. Schemas stay off the model list until load.
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
		shotTool{l}, clickTool{l}, typeTool{l}, scrollTool{l}, keyTool{l},
	}
}

func (l *Link) driver() Driver {
	if l != nil && l.Driver != nil {
		return l.Driver
	}
	return commandDriver{}
}

func (l *Link) confirm(action Action, summary string) (bool, string, error) {
	mode, err := machine.Get(l.Root, machine.Local)
	if err != nil {
		return false, "", err
	}
	if mode == machine.Off {
		return false, "", errOff()
	}
	if NeedsConfirm(mode, action) {
		return true, summary, nil
	}
	return false, summary, nil
}

func errOff() error { return errString("machine local is off") }

type errString string

func (e errString) Error() string { return string(e) }

type shotTool struct{ l *Link }

func (shotTool) Name() string { return "desktop_screenshot" }
func (shotTool) Description() string {
	return "Save a PNG of the local screen under the data root. Prefer a shell command or an API when that can answer the question. The machine switch can refuse this."
}
func (shotTool) Parameters() any { return obj(nil, nil) }
func (t shotTool) Confirm(json.RawMessage) (bool, string, error) {
	return t.l.confirm(Action{Name: "screenshot"}, "desktop screenshot")
}
func (t shotTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	return Perform(ctx, t.l.Root, t.l.driver(), Action{Name: "screenshot"}, true)
}

type clickTool struct{ l *Link }

func (clickTool) Name() string { return "desktop_click" }
func (clickTool) Description() string {
	return "Click the local screen at x, y. Yields when the owner has used the machine in the last few seconds. Stops for payment, login, and sending. Prefer a shell command or an API."
}
func (clickTool) Parameters() any {
	return obj(map[string]any{
		"x": map[string]any{"type": "integer", "description": "Pixels from the left"},
		"y": map[string]any{"type": "integer", "description": "Pixels from the top"},
	}, []string{"x", "y"})
}
func (t clickTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	_ = json.Unmarshal(args, &in)
	return t.l.confirm(Action{Name: "click", X: in.X, Y: in.Y}, fmt.Sprintf("desktop click %d %d", in.X, in.Y))
}
func (t clickTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return Perform(ctx, t.l.Root, t.l.driver(), Action{Name: "click", X: in.X, Y: in.Y}, true)
}

type typeTool struct{ l *Link }

func (typeTool) Name() string { return "desktop_type" }
func (typeTool) Description() string {
	return "Type text into the local focused window. Yields when the owner is active. Payment, login, and send text must be confirmed. Prefer a shell command or an API."
}
func (typeTool) Parameters() any {
	return obj(map[string]any{
		"text": map[string]any{"type": "string", "description": "Text to type"},
	}, []string{"text"})
}
func (t typeTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &in)
	summary := "desktop type\n" + in.Text
	if sensitive(in.Text) {
		summary += "\npayment, login, or send"
	}
	return t.l.confirm(Action{Name: "type", Text: in.Text}, summary)
}
func (t typeTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return Perform(ctx, t.l.Root, t.l.driver(), Action{Name: "type", Text: in.Text}, true)
}

type scrollTool struct{ l *Link }

func (scrollTool) Name() string { return "desktop_scroll" }
func (scrollTool) Description() string {
	return "Scroll the local window. Positive dy scrolls up, negative scrolls down, at most 20 steps. Yields when the owner is active."
}
func (scrollTool) Parameters() any {
	return obj(map[string]any{
		"dy": map[string]any{"type": "integer", "description": "Steps, -20..-1 or 1..20. Positive is up."},
		"x":  map[string]any{"type": "integer", "description": "Pixels from the left. Omit to keep the pointer."},
		"y":  map[string]any{"type": "integer", "description": "Pixels from the top. Omit to keep the pointer."},
	}, []string{"dy"})
}
func (t scrollTool) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := scrollArgs(args)
	if err != nil {
		return false, "", err
	}
	return t.l.confirm(Action{Name: "scroll", X: in.x, Y: in.y, Dy: in.dy}, scrollSummary(in))
}
func (t scrollTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Dy int `json:"dy"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return Perform(ctx, t.l.Root, t.l.driver(), Action{Name: "scroll", Dy: in.Dy}, true)
}

type keyTool struct{ l *Link }

func (keyTool) Name() string { return "desktop_key" }
func (keyTool) Description() string {
	return "Press one key or chord such as Return or ctrl+l. Yields when the owner is active. Prefer a shell command or an API."
}
func (keyTool) Parameters() any {
	return obj(map[string]any{
		"key": map[string]any{"type": "string", "description": "Key name"},
	}, []string{"key"})
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
	return t.l.confirm(Action{Name: "key", Key: in.Key}, summary)
}
func (t keyTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return Perform(ctx, t.l.Root, t.l.driver(), Action{Name: "key", Key: in.Key}, true)
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
