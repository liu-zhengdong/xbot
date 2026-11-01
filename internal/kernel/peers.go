package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func hasMember(members []string, name string) bool {
	for _, m := range members {
		if m == name {
			return true
		}
	}
	return false
}

type askTool struct{ k *Kernel }

func (askTool) Name() string { return "ask_user" }
func (askTool) Description() string {
	return "Ask the owner to pick one option. recommended is required and must exactly match one of options. Stop after this call. Their choice arrives as the next user message; after 30 minutes without a reply the recommended option is chosen automatically."
}
func (askTool) Parameters() any {
	return schema(map[string]any{
		"question":    stringProp("the question"),
		"recommended": stringProp("required: exact text of one of options; used automatically if the owner does not reply"),
		"options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "two to six short choices"},
	}, []string{"question", "options", "recommended"})
}
func (askTool) Confirm(args json.RawMessage) (bool, string, error) {
	q, opts, _, err := parseAsk(args)
	if err != nil {
		return false, "", err
	}
	return false, formatAsk(q, opts), nil
}
func (t askTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	q, opts, recommended, err := parseAsk(args)
	if err != nil {
		return "", err
	}
	if err := t.k.askOwner(ctx, q, opts, recommended); err != nil {
		return "", err
	}
	return "waiting for the owner to choose", nil
}

func parseAsk(args json.RawMessage) (string, []string, string, error) {
	var in struct {
		Question    string   `json:"question"`
		Recommended string   `json:"recommended"`
		Options     []string `json:"options"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", nil, "", err
	}
	q := strings.TrimSpace(in.Question)
	var opts []string
	for _, o := range in.Options {
		o = strings.TrimSpace(o)
		if o != "" {
			opts = append(opts, o)
		}
	}
	if q == "" || len(opts) < 2 || len(opts) > 6 {
		return "", nil, "", errAsk
	}
	recommended := strings.TrimSpace(in.Recommended)
	found := false
	for _, o := range opts {
		if o == recommended {
			found = true
		}
	}
	if !found {
		return "", nil, "", errAsk
	}
	return q, opts, recommended, nil
}

func formatAsk(question string, options []string) string {
	var b strings.Builder
	b.WriteString(question)
	for i, o := range options {
		fmt.Fprintf(&b, "\n%d. %s", i+1, o)
	}
	return b.String()
}

var errAsk = askError{}

type askError struct{}

func (askError) Error() string {
	return "ask_user: need a question, 2 to 6 options, and recommended matching one option"
}
