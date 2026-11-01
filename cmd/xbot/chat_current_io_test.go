package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/mcp"
	"xbot/internal/notify"
	"xbot/internal/proc"
	"xbot/internal/provider"
)

func TestChatCallbacksFollowCurrentResourcesAfterRename(t *testing.T) {
	for _, phase := range []string{"Accept", "Idle", "CatchUp", "After"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := t.TempDir()
			original, err := bot.Create(root, "before", "duty", nil)
			if err != nil {
				t.Fatal(err)
			}
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
			k := &kernel.Kernel{Bot: original, Resources: resources, Inbox: box}
			stdout, err := os.CreateTemp(t.TempDir(), "stdout-")
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()
			stderr, err := os.CreateTemp(t.TempDir(), "stderr-")
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			oldOut, oldErr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = stdout, stderr
			defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
			pr := &printer{}
			cfg, err := chatConfig(k, "owner-fixture", nil, pr)
			if err != nil {
				t.Fatal(err)
			}
			first, err := cfg.Accept("first input\n")
			if err != nil {
				t.Fatal(err)
			}
			if err := box.Ack(first.ID); err != nil {
				t.Fatal(err)
			}
			if err := original.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("reply from another process")}}); err != nil {
				t.Fatal(err)
			}
			if _, err := notify.Open(original.OutboxPath()).Append("note after first input", time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := resources.RenameTo("after"); err != nil {
				t.Fatal(err)
			}
			switch phase {
			case "Accept":
				second, err := cfg.Accept("  second input\r\n")
				if err != nil {
					t.Fatal(err)
				}
				if second.ID == "" || second.Sender != "owner-fixture" || second.Content != "second input" {
					t.Fatalf("changed terminal input: %+v", second)
				}
				current, err := bot.Load(root, "after")
				if err != nil {
					t.Fatal(err)
				}
				currentBox, err := inbox.Open(current.InboxPath())
				if err != nil {
					t.Fatal(err)
				}
				pending := currentBox.Pending()
				if len(pending) != 1 || pending[0].ID != second.ID {
					t.Fatalf("second input did not reach current inbox: %+v", pending)
				}
				if ev, err := cfg.Accept(" \r\n"); err != nil || ev.ID != "" {
					t.Fatal("blank line changed", ev, err)
				}
				if _, err := cfg.Accept("quit\n"); !errors.Is(err, proc.ErrStop) {
					t.Fatal("quit no longer stops", err)
				}
			case "Idle":
				cfg.Idle()
			case "CatchUp":
				if !cfg.CatchUp() {
					t.Error("current transcript was not replayed")
				}
				if cfg.CatchUp() {
					t.Error("catch-up replay duplicated output")
				}
			case "After":
				pr.Delta("streamed reply")
				cfg.After(first, errors.New("handled outcome"))
				if cfg.CatchUp() {
					t.Error("After failed to advance current transcript count")
				}
			}
			if entries, err := os.ReadDir(bot.Dir(root, "before")); !os.IsNotExist(err) {
				var names []string
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				t.Errorf("chat %s revived old directory: %v err=%v", phase, names, err)
			}
			out, err := os.ReadFile(stdout.Name())
			if err != nil {
				t.Fatal(err)
			}
			errout, err := os.ReadFile(stderr.Name())
			if err != nil {
				t.Fatal(err)
			}
			switch phase {
			case "Idle", "After":
				current, err := bot.Load(root, "after")
				if err != nil {
					t.Fatal(err)
				}
				unread, err := notify.Open(current.OutboxPath()).Unread()
				if err != nil {
					t.Fatal(err)
				}
				if len(unread) != 0 || strings.Count(string(errout), "notification: note after first input") != 1 {
					t.Errorf("notification read/display/mark lost: unread=%+v stderr=%q", unread, errout)
				}
				if phase == "Idle" && !strings.HasSuffix(string(errout), "after › ") {
					t.Errorf("prompt identity stale: %q", errout)
				}
				if phase == "After" && (string(out) != "streamed reply\n" || !strings.HasPrefix(string(errout), "xbot: handled outcome\nnotification:")) {
					t.Errorf("After changed output order: stdout=%q stderr=%q", out, errout)
				}
			case "CatchUp":
				if string(out) != "reply from another process\n" {
					t.Errorf("replay lost current output: %q", out)
				}
			}
		})
	}
}
