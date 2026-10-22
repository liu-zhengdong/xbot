package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestFailureReplyOnlyNamesUnansweredInputs(t *testing.T) {
	owner := func(id, text string) bot.Entry {
		return bot.Entry{EventID: id, Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: text}}
	}
	answer := func(text string) bot.Entry {
		return bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleAssistant, Content: text}}
	}
	quoted := answer("first done")
	quoted.ReplyTo = 1
	for _, tc := range []struct {
		name         string
		middle       []bot.Entry
		firstFailure int
	}{
		{"normal unquoted answer", []bot.Entry{answer("first done")}, 0},
		{"explicit quote", []bot.Entry{quoted}, 0},
		{"empty is not an answer", []bot.Entry{answer("")}, 1},
		{"silent is not an answer", []bot.Entry{answer("SILENT")}, 1},
		{"only tool work", []bot.Entry{{Message: toolCall("read", "read_file", "{}")}, {Message: provider.Message{Role: provider.RoleTool, ToolCallID: "read", Content: "data"}}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t)
			k := &Kernel{Bot: b}
			turn := &turn{fromSeq: 1}
			run := &turnRun{k: k, t: turn, sentThrough: 1}
			if err := b.Append(owner("eventA", "first")); err != nil {
				t.Fatal(err)
			}
			for _, row := range tc.middle {
				msg := row.Message
				msg.ReplyTo = row.ReplyTo
				run.onMessage(msg)
			}
			if run.err != nil {
				t.Fatal(run.err)
			}
			secondSeq, err := b.AppendSeq(owner("eventB", "second"))
			if err != nil {
				t.Fatal(err)
			}
			if err := k.replyToFailedInputs(turn); err != nil {
				t.Fatal(err)
			}
			got, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			failures := map[int]int{}
			for _, row := range got {
				if row.Message.Role == provider.RoleAssistant && strings.Contains(row.Message.Content, "还没处理完") {
					failures[row.ReplyTo]++
				}
			}
			if failures[1] != tc.firstFailure || failures[secondSeq] != 1 {
				t.Fatalf("failure replies=%v, want first=%d second=1", failures, tc.firstFailure)
			}
		})
	}
}

func TestCancelledHandleDoesNotInventFailureReply(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		failures int
	}{
		{"cancelled", context.Canceled, 0},
		{"wrapped cancellation", fmt.Errorf("transport: %w", context.Canceled), 0},
		{"provider failed", errors.New("provider unavailable"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t)
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b, MaxSteps: 2, Provider: providerFunc(func(context.Context, provider.Request, func(string)) (provider.Message, error) {
				return provider.Message{}, tc.err
			})}
			ev := link.TerminalEvent("u1", "pending words", time.Now())
			err := k.Handle(context.Background(), ev)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error=%v, want %v", err, tc.err)
			}
			rows, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			inputs, failures := 0, 0
			for _, row := range rows {
				if row.EventID == ev.ID {
					inputs++
				}
				if row.Message.Role == provider.RoleAssistant && strings.Contains(row.Message.Content, "还没处理完") {
					failures++
				}
			}
			if inputs != 1 || failures != tc.failures {
				t.Fatalf("inputs=%d false failures=%d, want 1/%d", inputs, failures, tc.failures)
			}
		})
	}
}

// B arrives after A's request was sent but before A's completed reply is
// committed. Its later error must not be hidden by A's unquoted reply.
func TestLateInputDoesNotBorrowCompletedRequestsAnswer(t *testing.T) {
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	wanted := errors.New("second request failed")
	late := link.TerminalEvent("u1", "B late input", time.Now())
	calls := 0
	k := &Kernel{Bot: b, MaxSteps: 3}
	k.Provider = providerFunc(func(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		calls++
		if calls == 1 {
			if strings.Contains(messagesText(req.Messages), late.Content) {
				t.Fatal("A request already included B")
			}
			if !Interject(b, late, &Sink{Done: make(chan Outcome, 1)}) {
				t.Fatal("late input not accepted")
			}
			return provider.Message{Role: provider.RoleAssistant, Content: "A completed normally"}, nil
		}
		text := messagesText(req.Messages)
		if !strings.Contains(text, late.Content) || !strings.Contains(text, "A completed normally") {
			t.Fatal("next request lost the conversation")
		}
		return provider.Message{}, wanted
	})
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "A opening input", time.Now())); !errors.Is(err, wanted) {
		t.Fatalf("handle=%v", err)
	}
	rows, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	bseq, aseq, answerSeq, failures := 0, 0, 0, map[int]int{}
	for i, row := range rows {
		if row.EventID == late.ID {
			bseq = i + 1
		}
		if row.Message.Content == "A opening input" {
			aseq = i + 1
		}
		if row.Message.Content == "A completed normally" {
			answerSeq = i + 1
		}
		if strings.Contains(row.Message.Content, "还没处理完") {
			failures[row.ReplyTo]++
		}
	}
	if calls != 2 || answerSeq <= bseq || failures[aseq] != 0 || failures[bseq] != 1 {
		t.Fatalf("calls=%d A=%d B=%d answer=%d failures=%v", calls, aseq, bseq, answerSeq, failures)
	}
}
