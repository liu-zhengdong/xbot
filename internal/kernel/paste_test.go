package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
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
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "post_picture", Arguments: "{}"},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("看到了")},
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
	var sawImage bool
	for _, m := range p.reqs[1].Messages {
		if len(m.Images) > 0 {
			sawImage = true
		}
	}
	if !sawImage {
		var dump strings.Builder
		for i, m := range p.reqs[1].Messages {
			fmt.Fprintf(&dump, "%d %s imgs=%d content=%q\n", i, m.Role, len(m.Images), m.Text())
		}
		t.Fatalf("the absorbed message reached the model without its picture:\n%s", dump.String())
	}
	// The stored line is the owner's own words: the note naming the picture is
	// added when the line is assembled, so it is not written twice.
	for _, e := range entriesFor(t, b) {
		if e.Message.Role == provider.RoleUser && strings.Contains(e.Message.Text(), "paste/") {
			t.Fatalf("the stored line already carries the note: %q", e.Message.Text())
		}
	}
	// And the transcript line names it, so a later turn can open it again.
	var named bool
	for _, e := range entriesFor(t, b) {
		if e.Message.Role == provider.RoleUser && len(e.Images) == 1 && e.Images[0] == id {
			named = true
		}
	}
	if !named {
		t.Fatalf("no transcript line names the picture: %+v", entriesFor(t, b))
	}
}

func entriesFor(t *testing.T, b *bot.Bot) []bot.Entry {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	return entries
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

// TestAbsorbedPicturesRespectVisionAndTheTurnCap covers the two ways the
// absorbed path could still over-send: a model that cannot see must never get
// the bytes, and the per-turn cap counts the whole turn, not one message, so a
// second absorbed message cannot start a fresh four pictures.
func TestAbsorbedPicturesRespectVisionAndTheTurnCap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		model     string
		wantImage bool
	}{
		{"a model without vision gets no picture", "deepseek-chat", false},
		{"a model with vision gets it", "deepseek-flash", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t, link.NameTerminal, link.NameTools)
			if err := b.SetBinding("p", tc.model); err != nil {
				t.Fatal(err)
			}
			dir, _, err := work.Open(b.Root, b.Name())
			if err != nil {
				t.Fatal(err)
			}
			id, err := work.PastedName("image/png")
			if err != nil {
				t.Fatal(err)
			}
			if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
				t.Fatal(err)
			}
			box, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			p := &scripted{resps: []provider.Message{
				{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
					ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "post_picture", Arguments: "{}"},
				}})},
				{Role: provider.RoleAssistant, Content: provider.TextContent("好")},
			}}
			k := &Kernel{
				Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4,
				WorkDir: dir, Links: []link.Link{link.Terminal{}, pasteLink{k: nil, id: id}},
			}
			k.Links[1] = pasteLink{k: k, id: id}
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
			var got bool
			for _, m := range p.reqs[1].Messages {
				if len(m.Images) > 0 {
					got = true
				}
			}
			if got != tc.wantImage {
				t.Fatalf("picture sent = %v, want %v", got, tc.wantImage)
			}
		})
	}
}

// TestPastedPictureSurvivesSummaryAndExpand covers where the name has to
// travel besides the assembled context: a range that gets summarized, and an
// expand of a raw line. Both read the transcript, which stores the words only,
// so a line about a picture would otherwise lose which file it meant.
func TestPastedPictureSurvivesSummaryAndExpand(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	dir, _, err := work.Open(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	id, err := work.PastedName("image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{
		Time:    time.Unix(10, 0).UTC(),
		Origin:  bot.OriginOwner,
		Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("看这个")},
		Images:  []string{id},
	}); err != nil {
		t.Fatal(err)
	}
	// The manual compact path keeps the newest messages raw, so the transcript
	// needs enough of them for range 1-2 to be summarizable at all.
	for i := 0; i < 10; i++ {
		if err := b.Append(bot.Entry{
			Time:    time.Unix(int64(20+i), 0).UTC(),
			Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("看到啦")},
		}); err != nil {
			t.Fatal(err)
		}
	}
	p := &recordingCompactor{}
	k := &Kernel{Bot: b, Provider: &scripted{}, Memory: memory.NopStore{}, Compactor: p, WorkDir: dir}

	got, err := k.Expand(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "paste/"+id) {
		t.Fatalf("expand lost the picture name:\n%s", got)
	}

	if _, err := k.CompactRange(context.Background(), 1, 2, ""); err != nil {
		t.Fatal(err)
	}
	if p.got == "" || !strings.Contains(p.got, "paste/"+id) {
		t.Fatalf("the summary input lost the picture name:\n%s", p.got)
	}
}

// recordingCompactor records the text it was asked to summarize.
type recordingCompactor struct {
	got string
}

func (c *recordingCompactor) Compact(_ context.Context, prior string, turns []provider.Message) (string, error) {
	var b strings.Builder
	for _, m := range turns {
		b.WriteString(m.Text())
		b.WriteByte('\n')
	}
	c.got = b.String()
	return "summary " + prior, nil
}

// TestAbsorbedNoteIsNotStored is the regression for the second cut of the
// absorbed path: the note naming the pictures was added to reach the model and
// then also stored, so the next assembly appended it a second time and the
// owner's own words were no longer the line's text.
func TestAbsorbedNoteIsNotStored(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	dir, _, err := work.Open(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	id, err := work.PastedName("image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("好")},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 2, WorkDir: dir,
		Links: []link.Link{link.Terminal{}}}
	// The absorbed message the way absorbInbox builds it.
	if err := k.record(provider.Message{Role: provider.RoleUser, ImageNames: []string{id}, Content: provider.TextContent(k.withPastedNote("看这个", []string{id}))}); err != nil {
		t.Fatal(err)
	}
	var stored string
	for _, e := range entriesFor(t, b) {
		if e.Message.Role == provider.RoleUser {
			stored = e.Message.Text()
		}
	}
	if strings.Contains(stored, "paste/") {
		t.Fatalf("the stored line carries the note: %q", stored)
	}
	if strings.TrimSpace(stored) != "看这个" {
		t.Fatalf("the stored line is not the owner's words: %q", stored)
	}
}

// TestAbsorbedPictureOverTheCapStillNamesItsPath covers the last way a picture
// could vanish: the turn's allowance is spent by a tool's pictures, so the
// absorbed one cannot be attached. The model must still get the path, because
// the message really did arrive with a picture.
func TestAbsorbedPictureOverTheCapStillNamesItsPath(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	if err := b.SetBinding("p", "deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	dir, _, err := work.Open(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	id, err := work.PastedName("image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	// The picture message is waiting while the tool spends the allowance.
	shot := link.WebEvent("u1", "", time.Unix(20, 0).UTC())
	shot.Images = []string{id}
	if err := box.Post(shot); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "spend_images", Arguments: "{}"},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("看到了")},
	}}
	k := &Kernel{Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4, WorkDir: dir,
		Links: []link.Link{link.Terminal{}, imageSpendLink{}}}
	ev := link.TerminalEvent("u1", "先看看那四张", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatalf("steps %d", len(p.reqs))
	}
	var sawPath bool
	for _, m := range p.reqs[1].Messages {
		if strings.Contains(m.Text(), "paste/"+id) {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatal("a picture that did not fit left the model without its path")
	}
}

// imageSpendLink is a tool that returns four pictures, so a test can spend the
// turn's allowance before an absorbed message arrives.
type imageSpendLink struct{}

func (imageSpendLink) Name() string        { return "imagery" }
func (imageSpendLink) Tools() []tools.Tool { return []tools.Tool{spendTool{}} }

type spendTool struct{}

func (spendTool) Name() string                                  { return "spend_images" }
func (spendTool) Description() string                           { return "return four pictures" }
func (spendTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (spendTool) Confirm(json.RawMessage) (bool, string, error) { return false, "spend", nil }
func (spendTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	for i := 0; i < 4; i++ {
		tools.AttachImage(ctx, tools.AttachedImage{MIME: "image/png", Data: []byte{byte(i + 1), 2, 3, 4}})
	}
	return "four", nil
}

// TestPastedPathSurvivesACd covers the note a later turn reads: it names an
// absolute path, because view_image resolves a relative one against the bot's
// current shell directory. A bot that cd-ed elsewhere must still be able to
// open a picture from an earlier message.
func TestPastedPathSurvivesACd(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	dir, _, err := work.Open(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	id, err := work.PastedName("image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, WorkDir: dir}
	note := k.pastedNote([]string{id})
	want := filepath.Join(dir, work.PastedDir, id)
	if !strings.Contains(note, want) {
		t.Fatalf("the note does not name an absolute path:\n%s", note)
	}
	if !filepath.IsAbs(want) {
		t.Fatal("the path in the note is not absolute")
	}
	// The file is where the note says, and it is a picture view_image accepts.
	data, err := work.ReadPasted(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if bot.SniffImage(data) != "image/png" {
		t.Fatal("the stored picture is not readable as an image")
	}
}

// TestCrossBotExpandNamesThatBotsPictures is the regression for a shared
// reader: expanding another bot's history must name that bot's workspace, not
// the reader's. Both pictures would otherwise point at the wrong directory.
func TestCrossBotExpandNamesThatBotsPictures(t *testing.T) {
	root := t.TempDir()
	a, err := bot.Create(root, "ada", "Be brief.", []string{link.NameTerminal, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	bb, err := bot.Create(root, "bob", "Be brief.", []string{link.NameTerminal, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bb.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	dir, _, err := work.Open(root, "bob")
	if err != nil {
		t.Fatal(err)
	}
	id, err := work.PastedName("image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := work.WritePasted(dir, id, onePixelPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := bb.Append(bot.Entry{
		Time:    time.Unix(10, 0).UTC(),
		Origin:  bot.OriginOwner,
		Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("看这个")},
		Images:  []string{id},
	}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: a, Memory: memory.NopStore{}}
	got, err := k.ExpandBot("bob", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantBob := filepath.Join(dir, work.PastedDir, id)
	if !strings.Contains(got, wantBob) {
		t.Fatalf("expand named the wrong workspace:\n%s", got)
	}
	wantAda := filepath.Join(work.BotDir(root, "ada"), work.PastedDir, id)
	if strings.Contains(got, wantAda) {
		t.Fatalf("expand named the reader's workspace:\n%s", got)
	}
	// The reader's own view_image cannot open the other bot's workspace, so the
	// note says that rather than offering a path that fails.
	if !strings.Contains(got, "view_image 打不开") {
		t.Fatalf("expand offers a path this bot cannot open:\n%s", got)
	}
}
