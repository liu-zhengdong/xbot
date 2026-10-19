package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// TestAbsorbedMessageKeepsItsPictures is the regression for the absorbed path:
// a message that arrives while a turn is already running is answered by that
// turn (absorbInbox), not by a fresh Handle. The first cut only recorded
// pictures on the fresh path, so an absorbed message lost them — the model
// never saw the picture and the transcript line never named it.
func TestAbsorbedMessageKeepsItsPictures(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	dir, _, err := work.Open(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	id, err := work.PastedName("image/png")
	if err != nil {
		t.Fatal(err)
	}
	pasteDir, err := work.EnsurePastedDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pasteDir, id), onePixelPNG(t), 0600); err != nil {
		t.Fatal(err)
	}

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "post_picture", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "看到了"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4,
		WorkDir: dir, Links: []link.Link{link.Terminal{}},
	}
	// The tool posts an owner message carrying a picture, the way the web link
	// posts one that arrived while this turn was running.
	k.Links = append(k.Links, pasteLink{k: k, id: id})

	ev := link.TerminalEvent("u1", "先做点事", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatalf("steps %d", len(p.reqs))
	}
	// The absorbed message reached the model with its picture attached.
	var sawImage, sawNote bool
	for _, m := range p.reqs[1].Messages {
		if len(m.Images) > 0 {
			sawImage = true
		}
		if strings.Contains(m.Content, "paste/"+id) {
			sawNote = true
		}
	}
	if !sawImage {
		var dump strings.Builder
		for i, m := range p.reqs[1].Messages {
			fmt.Fprintf(&dump, "%d %s imgs=%d content=%q\n", i, m.Role, len(m.Images), m.Content)
		}
		t.Fatalf("the absorbed message reached the model without its picture:\n%s", dump.String())
	}
	if !sawNote {
		t.Fatal("the absorbed message carries no note naming the picture")
	}
	// And the transcript line names it, so a later turn can open it again.
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var named bool
	for _, e := range entries {
		if e.Message.Role == provider.RoleUser && len(e.Images) == 1 && e.Images[0] == id {
			named = true
		}
	}
	if !named {
		t.Fatalf("no transcript line names the picture: %+v", entries)
	}
}

// pasteLink is a link whose one tool posts an owner message with a picture,
// standing in for the web message that arrives while a turn is running.
type pasteLink struct {
	k  *Kernel
	id string
}

func (pasteLink) Name() string          { return "paste" }
func (l pasteLink) Tools() []tools.Tool { return []tools.Tool{pasteTool{k: l.k, id: l.id}} }

type pasteTool struct {
	k  *Kernel
	id string
}

func (pasteTool) Name() string        { return "post_picture" }
func (pasteTool) Description() string { return "post an owner message with a picture" }
func (pasteTool) Parameters() any     { return map[string]any{"type": "object"} }
func (pasteTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "paste_post", nil
}

func (t pasteTool) Run(context.Context, json.RawMessage) (string, error) {
	ev := link.WebEvent("u1", "", time.Unix(20, 0).UTC())
	ev.Images = []string{t.id}
	if err := t.k.Inbox.Post(ev); err != nil {
		return "", err
	}
	return "posted", nil
}
