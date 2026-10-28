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

// PreparedRun separates admission from execution. An immediate response has no
// Run (for example a resource conflict that was queued without running).
type PreparedRun struct {
	Run  func(context.Context) (string, error)
	Text string
}

// PathTool is a file tool that reports the resolved path it would act on. The
// kernel uses it to decide whether a call is a persona/profile self-edit or
// reaches a protected vault path. The tool only reports the path; it never
// decides whether to confirm.
type PathTool interface {
	ResolvedPath(args json.RawMessage) string
}

// ContextConfirmer is a tool whose confirmation summary depends on the call
// context, for example which working directory the turn is in.
type ContextConfirmer interface {
	ConfirmContext(ctx context.Context, args json.RawMessage) (need bool, summary string, err error)
}

// PathContext is a file tool that resolves a path against the call context.
type PathContext interface {
	ResolvedPathContext(ctx context.Context, args json.RawMessage) string
}

// Undeclared is a tool the agent can run but does not list in the request's
// tools[]. The model reaches it through a Forwarder, so disclosing it never
// changes the declared set (ADR 0035).
type Undeclared interface {
	Undeclared() bool
}

// Forwarder is a proxy tool. When it returns a tool, the call is that tool
// with inner args, and the agent confirms, runs and reports it as if the model
// had called it by name; a nil tool means the proxy itself runs.
type Forwarder interface {
	Forward(ctx context.Context, args json.RawMessage) (Tool, json.RawMessage, error)
}

// ConfirmCall uses ConfirmContext when the tool has one, so a shared tool can
// confirm against the caller's working directory.
func ConfirmCall(ctx context.Context, t Tool, args json.RawMessage) (bool, string, error) {
	if c, ok := t.(ContextConfirmer); ok {
		return c.ConfirmContext(ctx, args)
	}
	return t.Confirm(args)
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if required == nil {
		required = []string{}
	}
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

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
