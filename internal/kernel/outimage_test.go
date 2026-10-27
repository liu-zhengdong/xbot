package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/provider"
	"xbot/internal/tools"
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

// TestNotifyOwnerImagesLandOnTheReplyLine is the journey: send_message_to_user names a
// workspace picture, the picture is stored in paste/, and the turn's final
// reply line carries its name, so the chat draws it in the bot's bubble. The
// bytes stay out of the transcript.
func TestNotifyOwnerImagesLandOnTheReplyLine(t *testing.T) {
	k, dir, _ := outImageKernel(t,
		toolCall("n1", "send_message_to_user", `{"text":"图好了","images":["chart.png","chart.png"]}`),
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
		if out, err := (sendMessageTool{k}).Run(context.Background(), args); err == nil {
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
		if _, err := (sendMessageTool{k}).Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := json.Marshal(map[string]any{"text": "x", "images": []string{"p4.png"}})
	if _, err := (sendMessageTool{k}).Run(context.Background(), args); err == nil {
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
	if _, err := (sendMessageTool{k}).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(k.turn.outImages) != 1 || k.turn.outImages[0] != id {
		t.Fatalf("queued %v", k.turn.outImages)
	}
}

// TestLateNotifyImagesStayWithTheirTurn is the regression for a detached
// send_message_to_user: the owner interjects, the call goes on as a tool job, the
// original turn writes its reply and a new turn starts. The late call used to
// queue its pictures on whatever turn was live, so the next turn's unrelated
// reply showed them. Now the call is refused, with no note, and says so.
func TestLateNotifyImagesStayWithTheirTurn(t *testing.T) {
	for _, replied := range []bool{true, false} {
		t.Run(map[bool]string{true: "reply-sent", false: "reply-pending"}[replied], func(t *testing.T) {
			k, dir, _ := outImageKernel(t)
			if err := os.WriteFile(filepath.Join(dir, "chart.png"), onePixelPNG(t), 0600); err != nil {
				t.Fatal(err)
			}
			owner := &turn{}
			k.turn = owner
			ctx := context.WithValue(context.Background(), turnKey{}, owner)
			signal := make(chan struct{})
			ctx = tools.WithWaitSignal(ctx, signal)
			started, release := make(chan struct{}), make(chan struct{})
			finished := make(chan tools.CallResult, 1)
			receipt := make(chan tools.CallResult, 1)
			defer func() { _ = k.Close() }()
			args := json.RawMessage(`{"text":"chart ready","images":["chart.png"]}`)
			go func() {
				receipt <- k.runToolCall(ctx, sendMessageTool{k}, args, func(callCtx context.Context) tools.CallResult {
					close(started)
					<-release
					out, err := (sendMessageTool{k}).Run(callCtx, args)
					res := tools.CallResult{Text: out, Err: err}
					finished <- res
					return res
				})
			}()
			<-started
			close(signal)
			if got := <-receipt; got.Job == nil {
				close(release)
				t.Fatalf("tool did not detach: %+v", got)
			}
			if replied {
				if _, err := k.recordTurn(owner, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("原轮先收尾")}); err != nil {
					t.Fatal(err)
				}
			}
			next := &turn{}
			k.turnMu.Lock()
			k.turn = next
			k.turnMu.Unlock()
			close(release)
			result := <-finished
			if _, err := k.recordTurn(next, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("另一件事的回复")}); err != nil {
				t.Fatal(err)
			}
			for _, e := range entriesFor(t, k.Bot) {
				if e.Message.Text() == "另一件事的回复" && len(e.Images) > 0 {
					t.Fatalf("late notify put its pictures on the next turn's reply: %v", e.Images)
				}
			}
			owner.mu.Lock()
			queued := slices.Clone(owner.outImages)
			owner.mu.Unlock()
			if replied {
				if result.Err == nil || !strings.Contains(result.Err.Error(), "already sent") {
					t.Fatalf("late notify after the reply = %q, %v; want a refusal that says so", result.Text, result.Err)
				}
				if notes, _ := notify.Open(k.Bot.OutboxPath()).Unread(); len(notes) != 0 {
					t.Fatalf("refused call still sent a note: %+v", notes)
				}
				return
			}
			if result.Err != nil || len(queued) != 1 {
				t.Fatalf("late notify before the reply: %v, queued on owner %v", result.Err, queued)
			}
		})
	}
}

// TestPastedIDContentsAreChecked: a name in paste/ proves nothing about the
// file, so an id goes through the same type and pixel checks as a path.
func TestPastedIDContentsAreChecked(t *testing.T) {
	for name, data := range map[string][]byte{
		"not-an-image":   []byte("not an image"),
		"over-pixel-cap": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if data == nil {
				data = sizedPNG(t, 2049, 1)
			}
			k, dir, _ := outImageKernel(t)
			k.turn = &turn{}
			id, _ := work.PastedName("image/png")
			if err := work.WritePasted(dir, id, data); err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]any{"text": "这张", "images": []string{id}})
			if out, err := (sendMessageTool{k}).Run(context.Background(), args); err == nil {
				t.Fatalf("accepted: %s", out)
			}
			if len(k.turn.outImages) != 0 {
				t.Fatalf("queued %v", k.turn.outImages)
			}
			if notes, _ := notify.Open(k.Bot.OutboxPath()).Unread(); len(notes) != 0 {
				t.Fatalf("note sent: %+v", notes)
			}
		})
	}
}

// TestRepeatedRefsAreReadOnce: the caps bind while reading. One path named 24
// times used to be read 24 times and held before deduplication.
func TestRepeatedRefsAreReadOnce(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	k.turn = &turn{}
	pic := onePixelPNG(t)
	data := append(pic, make([]byte, (1<<20)-len(pic))...)
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 24)
	for i := range refs {
		refs[i] = "chart.png"
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	ids, err := k.attachOutImages(context.Background(), refs)
	runtime.ReadMemStats(&after)
	if err != nil || len(ids) != 1 {
		t.Fatalf("%v %v", ids, err)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 8<<20 {
		t.Fatalf("allocated %d bytes for one 1 MiB picture named 24 times", got)
	}
	// Distinct refs past the count cap are refused before anything is read.
	many := []string{"a.png", "b.png", "c.png", "d.png", "e.png"}
	if _, err := k.attachOutImages(context.Background(), many); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("five missing files: %v; want the count refusal first", err)
	}
	// A file that would push the reply past 10 MiB is refused by its size.
	k.turn = &turn{outImages: []string{"x"}, outBytes: work.PastedMaxTotalBytes - 10}
	if _, err := k.attachOutImages(context.Background(), []string{"chart.png"}); !errors.Is(err, errOverBudget) {
		t.Fatalf("over budget: %v", err)
	}
}

// TestQueuedDuplicateFitsBudget covers R4 with a complete, legal ~9.4 MiB PNG.
// A duplicate consumes neither another slot nor any more of the reply budget,
// and must not block the notification or the eventual reply's image.
func TestQueuedDuplicateFitsBudget(t *testing.T) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1536, 1536))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if len(data) <= work.PastedMaxTotalBytes/2 || len(data) > work.PastedMaxBytes {
		t.Fatalf("bad fixture size: %d", len(data))
	}
	id, err := work.PastedNameFor("image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"same-path", "paste-id", "path-aliases", "same-content", "same-content-next-call"} {
		t.Run(kind, func(t *testing.T) {
			k, dir, _ := outImageKernel(t)
			k.turn = &turn{}
			for _, name := range []string{"chart.png", "copy.png"} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			call := func(refs []string) error {
				args, err := json.Marshal(map[string]any{"text": "chart ready", "images": refs})
				if err != nil {
					t.Fatal(err)
				}
				_, err = (sendMessageTool{k}).Run(context.Background(), args)
				return err
			}
			wantNotes := 1
			var err error
			switch kind {
			case "path-aliases":
				err = call([]string{"chart.png", "./chart.png"})
			case "same-content":
				err = call([]string{"chart.png", "copy.png"})
			default:
				if err := call([]string{"chart.png"}); err != nil {
					t.Fatal(err)
				}
				ref := "chart.png"
				if kind == "paste-id" {
					ref = id
				} else if kind == "same-content-next-call" {
					ref = "copy.png"
				}
				err = call([]string{ref})
				wantNotes = 2
			}
			if err != nil {
				t.Fatalf("duplicate of %d bytes refused: %v", len(data), err)
			}
			if !slices.Equal(k.turn.outImages, []string{id}) || k.turn.outBytes != len(data) {
				t.Fatalf("queue = %v, bytes = %d", k.turn.outImages, k.turn.outBytes)
			}
			if notes, err := notify.Open(k.Bot.OutboxPath()).Unread(); err != nil || len(notes) != wantNotes {
				t.Fatalf("notifications = %v, %v; want %d", notes, err, wantNotes)
			}
			if _, err := k.recordTurn(k.turn, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("chart")}); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range entriesFor(t, k.Bot) {
				if e.Message.Text() == "chart" {
					found = true
					if !slices.Equal(e.Images, []string{id}) {
						t.Fatalf("reply images = %v", e.Images)
					}
				}
			}
			if !found || len(k.turn.outImages) != 0 || k.turn.outBytes != 0 {
				t.Fatalf("reply recorded = %v; remaining queue = %v / %d", found, k.turn.outImages, k.turn.outBytes)
			}
		})
	}
}

// TestNewOutImageOverBudgetIsNotLoaded guards R3 while fixing R4: a genuinely
// new file that does not fit must not be loaded into a full byte slice, even
// when it follows a duplicate. Rejection leaves the established queue and note
// unchanged and does not store any part of the refused batch.
func TestNewOutImageOverBudgetIsNotLoaded(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	k.turn = &turn{}
	data := append(onePixelPNG(t), make([]byte, 6<<20)...)
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"text":"ready","images":["chart.png"]}`)
	if _, err := (sendMessageTool{k}).Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	queued := slices.Clone(k.turn.outImages)
	other := append(sizedPNG(t, 2, 1), make([]byte, 5<<20)...)
	if err := os.WriteFile(filepath.Join(dir, "other.png"), other, 0600); err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]string{{"other.png"}, {"chart.png", "other.png"}} {
		args, err := json.Marshal(map[string]any{"text": "too much", "images": refs})
		if err != nil {
			t.Fatal(err)
		}
		// Measure the new candidate alone; the repeated candidate is allowed
		// its own bounded validation read, but adds nothing to the queue.
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err = (sendMessageTool{k}).Run(context.Background(), args)
		runtime.ReadMemStats(&after)
		if !errors.Is(err, errOverBudget) {
			t.Fatalf("new picture over budget: %v", err)
		}
		if len(refs) == 1 {
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("over-budget 5 MiB candidate allocated %d bytes", allocated)
			if allocated > 1<<20 {
				t.Fatalf("loaded over-budget picture: allocated %d bytes", allocated)
			}
		}
	}
	if !slices.Equal(k.turn.outImages, queued) || k.turn.outBytes != len(data) {
		t.Fatalf("refusal changed queue: %v / %d", k.turn.outImages, k.turn.outBytes)
	}
	if files, err := os.ReadDir(work.PastedDirFor(dir)); err != nil || len(files) != 1 {
		t.Fatalf("refusal wrote files: %v, %v", files, err)
	}
	if notes, err := notify.Open(k.Bot.OutboxPath()).Unread(); err != nil || len(notes) != 1 {
		t.Fatalf("refusal sent notes: %v, %v", notes, err)
	}
}

// A queued id exempts only its budget, never the existing content checks.
func TestQueuedPastedIDIsStillValidated(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"type":      []byte("not an image"),
		"pixels":    sizedPNG(t, 2049, 1),
		"extension": jpg.Bytes(),
	} {
		t.Run(name, func(t *testing.T) {
			k, dir, _ := outImageKernel(t)
			k.turn = &turn{}
			id, err := work.PastedName("image/png")
			if err != nil {
				t.Fatal(err)
			}
			pic := onePixelPNG(t)
			if err := work.WritePasted(dir, id, pic); err != nil {
				t.Fatal(err)
			}
			args, err := json.Marshal(map[string]any{"text": "ready", "images": []string{id}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (sendMessageTool{k}).Run(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			if err := work.WritePasted(dir, id, data); err != nil {
				t.Fatal(err)
			}
			if _, err := (sendMessageTool{k}).Run(context.Background(), args); err == nil {
				t.Fatal("queued id skipped content validation")
			}
			if !slices.Equal(k.turn.outImages, []string{id}) || k.turn.outBytes != len(pic) {
				t.Fatalf("refusal changed queue: %v / %d", k.turn.outImages, k.turn.outBytes)
			}
			if notes, err := notify.Open(k.Bot.OutboxPath()).Unread(); err != nil || len(notes) != 1 {
				t.Fatalf("refusal sent notes: %v, %v", notes, err)
			}
		})
	}
}

// TestContinuationCanAttachImages reproduces a real Handle/Interject journey:
// the same turn has already replied, then a new tool call attaches to its next reply.
func TestContinuationCanAttachImages(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	defer k.Close()
	if err := k.Bot.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), onePixelPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var firstTurn *turn
	late := link.TerminalEvent("u1", "再把图给我", time.Now())
	k.Provider = providerFunc(func(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		calls++
		switch calls {
		case 1:
			firstTurn = k.liveTurn()
			if !Interject(k.Bot, late, &Sink{Done: make(chan Outcome, 1)}) {
				t.Fatal("interjection was not accepted")
			}
			return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第一句已答")}, nil
		case 2:
			if k.liveTurn() != firstTurn {
				t.Fatal("continuation switched turn identity")
			}
			if !strings.Contains(messagesText(req.Messages), late.Content) {
				t.Fatal("continuation lost new input")
			}
			return toolCall("new-picture", "send_message_to_user", `{"text":"新请求的图","images":["chart.png"]}`), nil
		case 3:
			for _, m := range req.Messages {
				if m.Role == provider.RoleTool && m.Name == "send_message_to_user" {
					t.Logf("fresh continuation tool result: %s", m.Text())
				}
			}
			return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("这是新请求的图")}, nil
		default:
			t.Fatalf("unexpected model call %d", calls)
			return provider.Message{}, nil
		}
	})
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "先答一句", time.Now())); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("model calls=%d", calls)
	}
	id, err := work.PastedNameFor("image/png", onePixelPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entriesFor(t, k.Bot) {
		if e.Message.Text() == "第一句已答" && len(e.Images) != 0 {
			t.Errorf("first reply got continuation pictures: %v", e.Images)
		}
		if e.Message.Text() == "这是新请求的图" {
			found = true
			if !slices.Equal(e.Images, []string{id}) {
				t.Errorf("new call in the continuation failed to attach its image: %v", e.Images)
			}
		}
	}
	if !found {
		t.Fatal("continuation final answer absent")
	}
	notes, err := notify.Open(k.Bot.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	// Owner chat: the bubble is the delivery, so no note.
	if len(notes) != 0 {
		t.Errorf("new call in owner chat delivered %d notes, want 0", len(notes))
	}
}

// A new call in a continuation succeeds while an older detached call on that
// same turn remains stale. Clearing answered would accidentally revive it.
func TestContinuationRejectsOldImageJob(t *testing.T) {
	k, dir, _ := outImageKernel(t)
	owner := &turn{}
	k.turn = owner
	oldPic, newPic := onePixelPNG(t), sizedPNG(t, 2, 1)
	for name, data := range map[string][]byte{"old.png": oldPic, "new.png": newPic} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(context.Background(), turnKey{}, owner)
	signal := make(chan struct{})
	jobCtx := tools.WithWaitSignal(ctx, signal)
	started, release := make(chan struct{}), make(chan struct{})
	finished, receipt := make(chan tools.CallResult, 1), make(chan tools.CallResult, 1)
	released := false
	defer func() {
		if !released {
			close(release)
		}
		_ = k.Close()
	}()
	oldArgs := json.RawMessage(`{"text":"old note","images":["old.png"]}`)
	go func() {
		receipt <- k.runToolCall(jobCtx, sendMessageTool{k}, oldArgs, func(callCtx context.Context) tools.CallResult {
			close(started)
			<-release
			out, err := (sendMessageTool{k}).Run(callCtx, oldArgs)
			result := tools.CallResult{Text: out, Err: err}
			finished <- result
			return result
		})
	}()
	<-started
	close(signal)
	if got := <-receipt; got.Job == nil {
		t.Fatalf("old call did not detach: %+v", got)
	}
	if _, err := k.recordTurn(owner, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("first reply")}); err != nil {
		t.Fatal(err)
	}
	newArgs := json.RawMessage(`{"text":"new note","images":["new.png"]}`)
	fresh := k.runToolCall(ctx, sendMessageTool{k}, newArgs, func(callCtx context.Context) tools.CallResult {
		out, err := (sendMessageTool{k}).Run(callCtx, newArgs)
		return tools.CallResult{Text: out, Err: err}
	})
	if fresh.Err != nil || fresh.Job != nil {
		t.Fatalf("new call on the same turn: %+v", fresh)
	}
	close(release)
	released = true
	if got := <-finished; !errors.Is(got.Err, errReplySent) {
		t.Fatalf("stale old call: %+v", got)
	}
	newID, err := work.PastedNameFor("image/png", newPic)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owner.outImages, []string{newID}) || owner.outBytes != len(newPic) {
		t.Fatalf("old call changed continuation queue: %v / %d", owner.outImages, owner.outBytes)
	}
	files, err := os.ReadDir(work.PastedDirFor(dir))
	if err != nil || len(files) != 1 || files[0].Name() != newID {
		t.Fatalf("stale call wrote a picture: %v, %v", files, err)
	}
	notes, err := notify.Open(k.Bot.OutboxPath()).Unread()
	if err != nil || len(notes) != 1 || notes[0].Text != "new note" {
		t.Fatalf("notifications = %v, %v; want only the new call", notes, err)
	}
	if _, err := k.recordTurn(owner, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("continuation reply")}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entriesFor(t, k.Bot) {
		if e.Message.Text() == "first reply" && len(e.Images) != 0 {
			t.Fatalf("first reply images = %v", e.Images)
		}
		if e.Message.Text() == "continuation reply" {
			found = true
			if !slices.Equal(e.Images, []string{newID}) {
				t.Fatalf("continuation images = %v", e.Images)
			}
		}
	}
	if !found {
		t.Fatal("continuation reply absent")
	}
}

// TestSendInOwnerChatSkipsTheBell: in owner chat the bubble is the delivery,
// so send_message_to_user leaves no note; a wake the owner is not chatting in
// still notifies.
func TestSendInOwnerChatSkipsTheBell(t *testing.T) {
	k, _, _ := outImageKernel(t,
		toolCall("s1", "send_message_to_user", `{"text":"搞定了"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
		toolCall("s2", "send_message_to_user", `{"text":"例程有新情况"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("已告知")},
	)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "做吧", time.Unix(10, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if notes, err := notify.Open(k.Bot.OutboxPath()).Unread(); err != nil || len(notes) != 0 {
		t.Fatalf("owner chat rang the bell: %v, %v", notes, err)
	}
	var said string
	for _, e := range entriesFor(t, k.Bot) {
		if e.Message.Role == provider.RoleTool && e.Message.Name == "send_message_to_user" {
			said = e.Message.Text()
		}
	}
	if said != "sent to the owner's chat" {
		t.Fatalf("result = %q", said)
	}
	if err := k.Handle(context.Background(), link.NewEvent(link.SourceSubagent, exec.EvDone, "查完了", "sa_a", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if notes, err := notify.Open(k.Bot.OutboxPath()).Unread(); err != nil || len(notes) != 1 || notes[0].Text != "例程有新情况" {
		t.Fatalf("wake did not notify: %v, %v", notes, err)
	}
}

// After send_message_to_user reached the chat, the owner has the answer, so
// a closing SILENT does not fail the turn.
func TestSilentAfterChatSendIsAnAnswer(t *testing.T) {
	k, _, _ := outImageKernel(t,
		toolCall("s1", "send_message_to_user", `{"text":"好了"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")},
	)
	if err := k.Bot.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "做吧", time.Unix(10, 0).UTC())); err != nil {
		t.Fatal(err)
	}
}
