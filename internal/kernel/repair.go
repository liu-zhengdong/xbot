package kernel

import (
	"errors"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

const interrupted = "interrupted before the tool returned"

// Messages copies transcript entries into provider messages.
func Messages(entries []bot.Entry) []provider.Message {
	out := make([]provider.Message, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out
}

// unresolvedTail returns tool calls at the end of the transcript that have no result.
// A gap that is not at the tail is an error: the append-only log cannot insert results earlier.
func unresolvedTail(entries []bot.Entry) ([]provider.ToolCall, error) {
	missing := missingResults(Messages(entries))
	if len(missing) == 0 {
		return nil, nil
	}
	i := len(entries) - 1
	for i >= 0 && (entries[i].Message.Role == provider.RoleTool || entries[i].IsForkMark()) {
		i--
	}
	if i < 0 || entries[i].Message.Role != provider.RoleAssistant {
		return nil, errors.New("transcript: unanswered tool calls are not at the tail")
	}
	have := map[string]bool{}
	for _, tc := range entries[i].Message.ToolCalls {
		have[tc.ID] = true
	}
	for _, tc := range missing {
		if !have[tc.ID] {
			return nil, errors.New("transcript: unanswered tool calls are not at the tail")
		}
	}
	return missing, nil
}

func missingResults(msgs []provider.Message) []provider.ToolCall {
	answered := map[string]bool{}
	var calls []provider.ToolCall
	for _, m := range msgs {
		if m.Role == provider.RoleTool && m.ToolCallID != "" {
			answered[m.ToolCallID] = true
		}
		if m.Role == provider.RoleAssistant {
			calls = append(calls, m.ToolCalls...)
		}
	}
	var missing []provider.ToolCall
	for _, tc := range calls {
		if tc.ID != "" && !answered[tc.ID] {
			missing = append(missing, tc)
		}
	}
	return missing
}

func indexEvent(entries []bot.Entry, eventID string) int {
	for i, e := range entries {
		if e.EventID == eventID && e.Message.Role == provider.RoleUser {
			return i
		}
	}
	return -1
}

// turnDone reports whether eventID already has a final assistant answer.
func turnDone(entries []bot.Entry, idx int) bool {
	if idx < 0 {
		return false
	}
	end := len(entries)
	for i := idx + 1; i < len(entries); i++ {
		if entries[i].Message.Role == provider.RoleUser && entries[i].EventID != "" && entries[i].EventID != entries[idx].EventID {
			end = i
			break
		}
	}
	pending := map[string]bool{}
	sawFinal := false
	for _, e := range entries[idx+1 : end] {
		m := e.Message
		switch m.Role {
		case provider.RoleAssistant:
			if len(m.ToolCalls) == 0 && len(pending) == 0 {
				sawFinal = true
				continue
			}
			sawFinal = false
			for _, tc := range m.ToolCalls {
				pending[tc.ID] = true
			}
		case provider.RoleTool:
			delete(pending, m.ToolCallID)
		}
	}
	return sawFinal && len(pending) == 0
}

func lastAssistant(entries []bot.Entry, idx int) string {
	end := len(entries)
	for i := idx + 1; i < len(entries); i++ {
		if entries[i].Message.Role == provider.RoleUser && entries[i].EventID != "" && entries[i].EventID != entries[idx].EventID {
			end = i
			break
		}
	}
	text := ""
	for _, e := range entries[idx+1 : end] {
		if e.Message.Role == provider.RoleAssistant && e.Message.Content != "" && len(e.Message.ToolCalls) == 0 {
			text = e.Message.Content
		}
	}
	return text
}

func lastReplyTo(entries []bot.Entry, idx int) int {
	end := len(entries)
	for i := idx + 1; i < len(entries); i++ {
		if entries[i].Message.Role == provider.RoleUser && entries[i].EventID != "" && entries[i].EventID != entries[idx].EventID {
			end = i
			break
		}
	}
	replyTo := 0
	for _, e := range entries[idx+1 : end] {
		if e.Message.Role == provider.RoleAssistant && len(e.Message.ToolCalls) == 0 {
			replyTo = e.ReplyTo
		}
	}
	return replyTo
}

func placeholder(tc provider.ToolCall) bot.Entry {
	return bot.Entry{
		Time: time.Now().UTC(),
		Message: provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: tc.ID,
			Name:       tc.Function.Name,
			Content:    interrupted,
		},
	}
}
