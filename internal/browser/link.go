package browser

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/tools"
)

// Link drives a local Chrome. Schemas stay off the model list until load.
type Link struct {
	Root string
	mu   sync.Mutex
	sess *Session
}

// Open binds the browser to this data root.
func Open(root string) *Link { return &Link{Root: root} }

func (l *Link) Name() string { return link.NameBrowser }

func (l *Link) Tools() []tools.Tool {
	if l == nil {
		return nil
	}
	return []tools.Tool{openTool{l}, snapTool{l}, clickTool{l}, stopTool{l}}
}

// confirm is the browser link's local machine adapter. The kernel review path
// still decides confirmation for the model path; these tools only build the
// card summary, and this adapter keeps the browser package's own behavior for
// its direct callers.
func (l *Link) confirm(summary string) (bool, string, error) {
	mode, err := machine.Get(l.Root, machine.Local)
	if err != nil {
		return false, "", err
	}
	if mode == machine.Off {
		return false, "", errors.New("machine local is off")
	}
	if mode == machine.Ask || summary != "browser snapshot" {
		return true, summary, nil
	}
	return false, summary, nil
}

type openTool struct{ l *Link }

func (openTool) Name() string { return "browser_open" }
func (openTool) Description() string {
	return "Open a URL in a local Chrome with its own profile, over the debugging port. Prefer a shell command or an API when that can do the job. The machine switch can refuse this."
}
func (openTool) Parameters() any {
	return obj(map[string]any{
		"url":      map[string]any{"type": "string", "description": "http or https URL"},
		"headless": map[string]any{"type": "boolean", "description": "Run without a window"},
	}, []string{"url"})
}
func (t openTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(args, &in)
	return t.l.confirm("browser open " + in.URL)
}
func (t openTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		URL      string `json:"url"`
		Headless bool   `json:"headless"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	sess, err := Launch(ctx, t.l.Root, in.URL, in.Headless)
	if err != nil {
		return "", err
	}
	t.l.mu.Lock()
	if t.l.sess != nil {
		t.l.sess.Close()
	}
	t.l.sess = sess
	t.l.mu.Unlock()
	return t.l.snapshot(ctx)
}

type snapTool struct{ l *Link }

func (snapTool) Name() string { return "browser_snapshot" }
func (snapTool) Description() string {
	return "Read the title, URL, and visible text of the Chrome page opened by browser_open."
}
func (snapTool) Parameters() any { return obj(nil, nil) }
func (t snapTool) Confirm(json.RawMessage) (bool, string, error) {
	return t.l.confirm("browser snapshot")
}
func (t snapTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	return t.l.snapshot(ctx)
}

type clickTool struct{ l *Link }

func (clickTool) Name() string { return "browser_click" }
func (clickTool) Description() string {
	return "Click the first element matching a CSS selector in the opened Chrome page. Prefer a shell command or an API."
}
func (clickTool) Parameters() any {
	return obj(map[string]any{
		"selector": map[string]any{"type": "string", "description": "CSS selector"},
	}, []string{"selector"})
}
func (t clickTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Selector string `json:"selector"`
	}
	_ = json.Unmarshal(args, &in)
	return t.l.confirm("browser click " + in.Selector)
}
func (t clickTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	sess, err := t.l.session(ctx)
	if err != nil {
		return "", err
	}
	return sess.Click(ctx, in.Selector)
}

type stopTool struct{ l *Link }

func (stopTool) Name() string { return "browser_stop" }
func (stopTool) Description() string {
	return "Stop the Chrome process started for this data root."
}
func (stopTool) Parameters() any { return obj(nil, nil) }
func (stopTool) Confirm(json.RawMessage) (bool, string, error) {
	return true, "browser stop", nil
}
func (t stopTool) Run(context.Context, json.RawMessage) (string, error) {
	t.l.mu.Lock()
	sess := t.l.sess
	t.l.sess = nil
	t.l.mu.Unlock()
	if sess != nil {
		sess.Release()
	}
	note, err := Stop(t.l.Root)
	if err != nil {
		return "", err
	}
	if note != "" {
		return note, nil
	}
	return "stopped", nil
}

func (l *Link) snapshot(ctx context.Context) (string, error) {
	sess, err := l.session(ctx)
	if err != nil {
		return "", err
	}
	return sess.Snapshot(ctx)
}

func (l *Link) session(ctx context.Context) (*Session, error) {
	l.mu.Lock()
	sess := l.sess
	l.mu.Unlock()
	if sess != nil {
		return sess, nil
	}
	sess, err := AttachSaved(ctx, l.Root)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.sess = sess
	l.mu.Unlock()
	return sess, nil
}

func obj(props map[string]any, required []string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	return map[string]any{
		"type": "object", "properties": props, "required": required, "additionalProperties": false,
	}
}
