package eval

import (
	"context"
	"encoding/json"
	"fmt"

	"xbot/internal/provider"
)

// Step is one scripted model turn. Tool and Text are alternatives.
type Step struct {
	Text string
	Tool string
	Args string
}

// Script is the fake model's steps for one case.
type Script []Step

// Fake is a deterministic provider. It does not call a network.
type Fake struct {
	Steps []Step
	i     int
}

// Chat returns the next scripted message.
func (f *Fake) Chat(_ context.Context, _ provider.Request, onDelta func(string)) (provider.Message, error) {
	if f.i >= len(f.Steps) {
		return provider.Message{}, fmt.Errorf("eval: script ended after %d steps", f.i)
	}
	st := f.Steps[f.i]
	f.i++
	if st.Tool != "" {
		return provider.Message{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{
				ID:   fmt.Sprintf("c%d", f.i),
				Type: "function",
				Function: provider.FunctionCall{
					Name:      st.Tool,
					Arguments: st.Args,
				},
			}},
		}, nil
	}
	if onDelta != nil && st.Text != "" {
		onDelta(st.Text)
	}
	return provider.Message{Role: provider.RoleAssistant, Content: st.Text}, nil
}

func args(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
