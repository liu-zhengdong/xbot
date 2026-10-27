package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"xbot/internal/provider"
)

var sayCalls atomic.Int64

// sayTo is a scripted model telling the owner text: the chat shows only what
// goes through send_message_to_user.
func sayTo(text string) provider.Message {
	args, _ := json.Marshal(map[string]string{"text": text})
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: fmt.Sprintf("say%d", sayCalls.Add(1)), Type: "function", Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: string(args)},
	}})}
}

// spoke ends the turn on the step after sayTo; the owner already has the
// message, so the closing reply text is never shown.
func spoke(req provider.Request) (provider.Message, bool) {
	last := lastConversationInput(req.Messages)
	if last.Role != provider.RoleTool || last.Name != "send_message_to_user" {
		return provider.Message{}, false
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("好。")}, true
}

// saidIn reports whether m carries text, as reply text or as a message sent
// to the owner.
func saidIn(m provider.Message, text string) bool {
	if strings.Contains(m.Text(), text) {
		return true
	}
	for _, c := range m.Calls() {
		if c.Function.Name == "send_message_to_user" && strings.Contains(c.Function.Arguments, text) {
			return true
		}
	}
	return false
}
