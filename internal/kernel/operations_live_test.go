package kernel

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestRuntimeProfileRefreshesOtherLiveHandleBeforeNextRequest(t *testing.T) {
	b := testBot(t)
	old := &Kernel{Bot: b}
	if err := old.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer old.CloseOperations()
	current, err := bot.Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	sampled := make(chan string, 1)
	notified := make(chan string, 1)
	step := 0
	p := providerFunc(func(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		step++
		if step == 1 {
			close(ready)
			select {
			case <-release:
			case <-ctx.Done():
				return provider.Message{}, ctx.Err()
			}
			return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "ack", Type: "function", Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: `{"text":"继续处理。"}`}}})}, nil
		}
		var text strings.Builder
		for _, m := range req.Messages {
			text.WriteString(m.Text())
			text.WriteByte('\n')
		}
		sampled <- text.String()
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}, nil
	})
	live := &Kernel{Bot: current, Provider: p, MaxSteps: 4, OnProfile: func(name string) { notified <- name }}
	defer live.Close()
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { done <- live.Handle(ctx, link.TerminalEvent("u1", "continue", time.Now())) }()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("live round did not reach gate")
	}
	desc, tz, seed := "changed by earlier pass", "Pacific/Honolulu", "live-avatar#3"
	value, err := cliops.Call(ctx, operationEndpoint(old), b.Root, &cliops.Update{Description: &desc, Timezone: &tz, AvatarSeed: &seed})
	if err != nil {
		t.Fatal(err)
	}
	if value.Avatar != bot.AvatarGenerated || value.AvatarSeed != seed {
		t.Fatalf("runtime omitted avatar metadata: %+v", value)
	}
	if mode, got := current.AvatarState(); mode != bot.AvatarGenerated || got != seed {
		t.Fatalf("other live handle missed avatar state: %s %s", mode, got)
	}
	select {
	case name := <-notified:
		if name != b.Name() {
			t.Fatal(name)
		}
	default:
		t.Fatal("live round did not receive profile notice")
	}
	once.Do(func() { close(release) })
	select {
	case text := <-sampled:
		if !strings.Contains(text, tz) || !strings.Contains(text, shortHash("About: "+desc)) {
			t.Fatalf("next request missed live profile environment: %s", text)
		}
	case <-ctx.Done():
		t.Fatal("no next request")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
