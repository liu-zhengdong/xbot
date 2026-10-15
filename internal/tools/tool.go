// Package tools is the built-in local actions a bot can take.
// They are exposed through the tools Link, not registered on the kernel directly.
package tools

import (
	"context"
	"encoding/json"
)

// Tool is one action the model may call.
// Confirm reports whether the kernel must ask before Run.
type Tool interface {
	Name() string
	Description() string
	Parameters() any
	Confirm(args json.RawMessage) (need bool, summary string, err error)
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
