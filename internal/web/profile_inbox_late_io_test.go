package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

type renameInboxHandleModel struct {
	rename func() error
	calls  int
}

func (m *renameInboxHandleModel) Chat(_ context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	m.calls++
	if m.calls == 1 {
		if err := m.rename(); err != nil {
			return provider.Message{}, err
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}, nil
}

func TestProfileAbsorbedReplyUsesCurrentInboxAndTranscript(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	original := mustWebBot(t, root, "before", "u1")
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := kernel.NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	box, err := inbox.Open(original.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	event := link.WebEvent("u1", "accepted before the wait", time.Now())
	if err := box.Post(event); err != nil {
		t.Fatal(err)
	}
	if err := original.Append(bot.Entry{EventID: event.ID, Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(event.Content)}}); err != nil {
		t.Fatal(err)
	}
	if err := original.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("reply recorded before rename")}}); err != nil {
		t.Fatal(err)
	}
	if err := box.Ack(event.ID); err != nil {
		t.Fatal(err)
	}
	if err := resources.RenameTo("after"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root}
	absorbed, reply, background, failed := s.absorbedReply(original, resources, box, event.ID)
	if !absorbed || failed || background != "" || reply != "reply recorded before rename" {
		t.Fatalf("lost original absorbed reply after wait: %v %q %q %v", absorbed, reply, background, failed)
	}
	if _, err := os.Stat(bot.Dir(root, "before")); !os.IsNotExist(err) {
		t.Fatalf("absorbed read revived old directory: %v", err)
	}
}

func TestProfileInboxAckAfterHandleRename(t *testing.T) {
	for _, mode := range []string{"waiting_message", "cli_event"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := t.TempDir()
			original := mustWebBot(t, root, "before", "u1")
			s := &Server{Root: root, Owner: "u1"}
			token, _, err := s.EnsureToken()
			if err != nil {
				t.Fatal(err)
			}
			manager := mcp.NewManager(ctx, mcp.Options{})
			defer manager.Close()
			s.NewResources = func(b *bot.Bot) (*kernel.BotResources, error) {
				r, err := kernel.NewBotResources(ctx, b, manager)
				if err == nil {
					r.Rename = s.RenameResources
				}
				return r, err
			}
			model := &renameInboxHandleModel{}
			s.NewKernel = func(b *bot.Bot, r *kernel.BotResources) (*kernel.Kernel, error) {
				box, err := inbox.Open(b.InboxPath())
				if err != nil {
					return nil, err
				}
				k := &kernel.Kernel{Bot: b, Resources: r, Inbox: box, Provider: model, Memory: memory.NopStore{}, Permissive: true, MaxSteps: 4, Budget: compact.DefaultBudget(), Links: []link.Link{link.Web{}}}
				model.rename = func() error {
					name := "after"
					receipt, err := k.ApplyProfileChange(bot.ProfileChange{Name: &name})
					if err == nil && (!receipt.Moved || receipt.Name != "after") {
						t.Errorf("rename not applied: %+v", receipt)
					}
					return err
				}
				return k, nil
			}
			defer s.CloseResources()
			defer s.CloseOperations()
			defer kernel.SetWaker(root, nil)
			cached, _, err := s.kernel(original)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "waiting_message" {
				lock, ok, err := original.TryLockHandle()
				if err != nil || !ok {
					t.Fatalf("other-process handle: %v %v", ok, err)
				}
				var wake <-chan struct{}
				if err := cached.Resources.WithCurrentIO(func(_ *bot.Bot, box *inbox.Inbox) error { wake = box.Wait(); return nil }); err != nil {
					t.Fatal(err)
				}
				released := make(chan struct{})
				go func() {
					defer close(released)
					defer lock.Release()
					select {
					case <-wake:
					case <-ctx.Done():
					}
				}()
				t.Cleanup(func() { cancel(); <-released })
			}
			host := httptest.NewServer(muxOf(s))
			defer host.Close()
			path, body := "/api/bots/before/messages", `{"text":"rename during this accepted message"}`
			if mode == "cli_event" {
				path, body = "/api/bots/before/events", `{"sender":"fixture","text":"rename during this accepted event"}`
			}
			request, err := http.NewRequest(http.MethodPost, host.URL+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
			client := host.Client()
			client.Timeout = 5 * time.Second
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), "event: done") || strings.Contains(string(raw), "event: error") {
				t.Fatalf("accepted stream lost outcome: %d %s", response.StatusCode, raw)
			}
			if entries, err := os.ReadDir(bot.Dir(root, "before")); !os.IsNotExist(err) {
				var names []string
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				t.Fatalf("late accepted inbox IO revived old directory: children=%v err=%v", names, err)
			}
			current, err := bot.Load(root, "after")
			if err != nil {
				t.Fatal(err)
			}
			box, err := inbox.Open(current.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			if len(box.Pending()) != 0 {
				t.Fatalf("accepted event was not acked at current path: %+v", box.Pending())
			}
			log, err := os.ReadFile(current.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(log), `"op":"post"`) != 1 || strings.Count(string(log), `"op":"ack"`) != 1 {
				t.Fatalf("event post/ack was lost or duplicated: %s", log)
			}
		})
	}
}
