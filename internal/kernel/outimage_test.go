package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/work"
)

func outImageKernel(t *testing.T, resps ...provider.Message) (*Kernel, string, *scripted) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameTimer)
	dir, _, err := work.Open(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: resps}
	k := &Kernel{
		Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4, WorkDir: dir,
		Links:   []link.Link{link.Terminal{}, &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}},
		Desktop: func(context.Context, string, string) error { return nil },
	}
	return k, dir, p
}

func sizedPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestNotifyOwnerImagesLandOnTheReplyLine is the journey: notify_owner names a
// workspace picture, the picture is stored in paste/, and the turn's final
// reply line carries its name, so the chat draws it in the bot's bubble. The
// bytes stay out of the transcript.
func TestNotifyOwnerImagesLandOnTheReplyLine(t *testing.T) {
	k, dir, _ := outImageKernel(t,
		toolCall("n1", "notify_owner", `{"text":"图好了","images":["chart.png","chart.png"]}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("看图")},
	)
	pic := onePixelPNG(t)
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), pic, 0600); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "画个图", time.Unix(10, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	id, _ := work.PastedNameFor("image/png", pic)
	stored, err := work.ReadPasted(dir, id)
	if err != nil || !bytes.Equal(stored, pic) {
		t.Fatalf("paste/%s not stored: %v", id, err)
	}
	var reply bool
	for _, e := range entriesFor(t, k.Bot) {
		if e.Message.Role == provider.RoleAssistant && len(e.Message.Calls()) == 0 {
			if len(e.Images) != 1 || e.Images[0] != id || e.Message.Text() != "看图" {
				t.Fatalf("reply line = %q images %v, want [%s]", e.Message.Text(), e.Images, id)
			}
			reply = true
		} else if len(e.Images) > 0 {
			t.Fatalf("%s line carries the bot's pictures: %v", e.Message.Role, e.Images)
		}
	}
	if !reply {
		t.Fatal("no reply line")
	}
	// Only paste/ and the source file hold the bytes.
	_ = filepath.WalkDir(k.Bot.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == filepath.Join(dir, "chart.png") || filepath.Base(filepath.Dir(path)) == work.PastedDir {
			return err
		}
		raw, _ := os.ReadFile(path)
		if bytes.Contains(raw, pic) || strings.Contains(string(raw), "iVBOR") {
			t.Fatalf("picture bytes in %s", path)
		}
		return nil
	})
	// A later turn names the picture as one the bot sent, not as the owner's.
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := k.assemble(st, "")
	if err != nil {
		t.Fatal(err)
	}
	var named bool
	for _, m := range msgs {
		if m.Role == provider.RoleAssistant && strings.Contains(m.Text(), "[发给主人的图]") && strings.Contains(m.Text(), id) {
			named = true
		}
		if m.Role == provider.RoleAssistant && strings.Contains(m.Text(), "[主人的图]") {
			t.Fatalf("reply named as the owner's picture: %q", m.Text())
		}
	}
	if !named {
		t.Fatal("assembled reply does not name the sent picture")
	}
}

// TestNotifyOwnerImagesRefusals covers the boundaries: the owner's caps on type,
// size, and count, and paths that leave the workspace. A refused call writes no
// note and queues nothing.
func TestNotifyOwnerImagesRefusals(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, onePixelPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.png"), sizedPNG(t, 2049, 1), 0600); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if err := os.WriteFile(filepath.Join(dir, "p"+string(rune('0'+i))+".png"), sizedPNG(t, i+1, 1), 0600); err != nil {
			t.Fatal(err)
		}
	}
	k.turn = &turn{}
	for _, refs := range [][]string{
		{outside},
		{"../" + filepath.Base(filepath.Dir(outside)) + "/secret.png"},
		{"link.png"},
		{"notes.txt"},
		{"huge.png"},
		{"missing.png"},
		{strings.Repeat("0", 32) + ".png"},
		{"p0.png", "p1.png", "p2.png", "p3.png", "p4.png"},
	} {
		args, _ := json.Marshal(map[string]any{"text": "x", "images": refs})
		if out, err := (notifyTool{k}).Run(context.Background(), args); err == nil {
			t.Fatalf("%v accepted: %s", refs, out)
		}
	}
	if len(k.turn.outImages) != 0 {
		t.Fatalf("refused calls queued %v", k.turn.outImages)
	}
	if entries, _ := os.ReadDir(work.PastedDirFor(dir)); len(entries) != 0 {
		t.Fatalf("refused calls stored %d files", len(entries))
	}
	// The count is the reply's, across calls.
	for _, refs := range [][]string{{"p0.png", "p1.png"}, {"p2.png", "p0.png"}, {"p3.png"}} {
		args, _ := json.Marshal(map[string]any{"text": "x", "images": refs})
		if _, err := (notifyTool{k}).Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := json.Marshal(map[string]any{"text": "x", "images": []string{"p4.png"}})
	if _, err := (notifyTool{k}).Run(context.Background(), args); err == nil {
		t.Fatal("a fifth picture on one reply was accepted")
	}
	if len(k.turn.outImages) != 4 {
		t.Fatalf("queued %v", k.turn.outImages)
	}
}

// TestNotifyOwnerTakesAPastedID attaches a picture the owner pasted earlier by
// its id, without copying it.
func TestNotifyOwnerTakesAPastedID(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	id, _ := work.PastedName("image/png")
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}
	k.turn = &turn{}
	args, _ := json.Marshal(map[string]any{"text": "这张", "images": []string{id}})
	if _, err := (notifyTool{k}).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(k.turn.outImages) != 1 || k.turn.outImages[0] != id {
		t.Fatalf("queued %v", k.turn.outImages)
	}
}
