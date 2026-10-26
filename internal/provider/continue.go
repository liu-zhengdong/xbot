package provider

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const maxOutputContinuations = 2
const maxContinuedOutput = 1 << 20

// Complete withholds text and hosted progress until the logical completion is
// finished. A terminal stop reason arrives after the text, so even the first
// request must be withheld. Each request still uses the caller's provider
// wrappers (budget fitting, usage, authentication and cancellation).
func Complete(ctx context.Context, p Provider, req Request, delta func(string)) (Message, error) {
	callCtx, cancel := context.WithCancel(WithPartObserver(ctx, nil))
	defer cancel()
	req.Messages = append([]Message(nil), req.Messages...)
	var result Message
	var billed TokenTotals
	size, offset, initialPrompt := 0, 0, 0
	allReported := true
	for round := 0; ; round++ {
		if err := callCtx.Err(); round > 0 && err != nil {
			return Message{}, err
		}
		streamSize := 0
		m, err := p.Chat(callCtx, req, func(s string) {
			streamSize += len(s)
			if streamSize > maxContinuedOutput-size {
				cancel()
			}
		})
		if streamSize > maxContinuedOutput-size {
			return Message{}, errors.New("provider: continued output exceeds 1 MiB")
		}
		if err != nil {
			return Message{}, err
		}
		// A successful final response wins a late preemption, matching the
		// agent loop. A truncated response still checks cancellation before
		// issuing another request.
		raw, err := json.Marshal(m.Content)
		if err != nil {
			return Message{}, errors.New("provider: invalid completion content")
		}
		size += len(raw)
		if size > maxContinuedOutput {
			return Message{}, errors.New("provider: continued output exceeds 1 MiB")
		}
		if m.Truncated() && len(m.Calls()) > 0 {
			// Never execute or replay a possibly incomplete local call.
			return Message{}, errors.New("provider: truncated tool call")
		}
		if round == 0 {
			result = m
			result.Content = nil
			initialPrompt = m.Usage.PromptTokens
			if m.Usage.InitialPromptTokens > 0 {
				initialPrompt = m.Usage.InitialPromptTokens
			}
		}
		for _, part := range m.Content {
			if part.Citation != nil {
				cite := *part.Citation
				cite.Start += offset
				cite.End += offset
				part.Citation = &cite
			}
			result.Content = append(result.Content, part)
		}
		offset += utf8.RuneCountInString(m.Text())
		u := m.Usage.Billable()
		billed.PromptTokens += u.PromptTokens
		billed.CompletionTokens += u.CompletionTokens
		billed.CachedTokens += u.CachedTokens
		allReported = allReported && m.Usage.Reported
		result.Usage, result.StopReason = m.Usage, m.StopReason
		if round > 0 && allReported {
			result.Usage.Billed = &billed
			result.Usage.InitialPromptTokens = initialPrompt
		}
		if !m.Truncated() {
			for _, part := range result.Content {
				if part.Hosted != nil {
					emitPart(ctx, part)
				}
			}
			if delta != nil && result.Text() != "" {
				delta(result.Text())
			}
			return result, nil
		}
		if round == maxOutputContinuations {
			return Message{}, errors.New("provider: output continuation limit reached")
		}
		m.Role = RoleAssistant
		if len(m.Content) > 0 {
			req.Messages = append(req.Messages, m)
		}
		req.Messages = append(req.Messages, Message{Role: RoleUser, Internal: true,
			Content: TextContent("Continue exactly where your previous response was cut off. Output only the remaining text; do not repeat the prefix or explain the continuation.")})
	}
}
