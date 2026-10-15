package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestConnectorSchemasWaitForLoad(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTelegram)
	k := &Kernel{Bot: b, Links: []link.Link{link.NewConnector(bot.Dir(b.Root, b.Name()), b.SecretPath(), link.NameTelegram)}}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if hasToolName(list, "telegram_send") {
		t.Fatal("schema was listed before load")
	}
	k.markLoaded(link.NameTelegram)
	list, err = k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !hasToolName(list, "telegram_send") {
		t.Fatal("schema missing after load")
	}
}

func TestCodingSchemasWaitForLoad(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameCoding)
	k := &Kernel{Bot: b, Links: []link.Link{code.Open(bot.Dir(b.Root, b.Name()), b.InboxPath())}}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if hasToolName(list, "code_start") {
		t.Fatal("schema was listed before load")
	}
	k.markLoaded(link.NameCoding)
	list, err = k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"code_start", "code_steer", "code_queue", "code_interrupt", "code_cancel", "code_show"} {
		if !hasToolName(list, name) {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestConnectorDeniedSendDoesNotPost(t *testing.T) {
	const secret = "token-value-1"
	b := testBot(t, link.NameTerminal, link.NameTelegram)
	if err := link.WriteSecrets(b.SecretPath(), map[string]string{"TELEGRAM_BOT_TOKEN": secret}); err != nil {
		t.Fatal(err)
	}
	posted := 0
	conn := link.NewConnector(bot.Dir(b.Root, b.Name()), b.SecretPath(), link.NameTelegram)
	conn.Post = func(context.Context, string, string, string) error {
		posted++
		return nil
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c0", Type: "function",
			Function: provider.FunctionCall{Name: "links", Arguments: `{"action":"load","id":"telegram"}`},
		}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "telegram_send", Arguments: `{"to":"42","body":"hello"}`},
		}}},
		{Role: provider.RoleAssistant, Content: "kept the draft"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links:   []link.Link{conn},
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "tell them", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if posted != 0 {
		t.Fatal("sent without approval")
	}
	text := transcriptText(t, b)
	if strings.Contains(text, secret) || !strings.Contains(text, "denied by user") {
		t.Fatal(text)
	}
}

func hasToolName(list []tools.Tool, name string) bool {
	for _, tool := range list {
		if tool != nil && tool.Name() == name {
			return true
		}
	}
	return false
}
