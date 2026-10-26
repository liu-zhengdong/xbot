package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

const mcpSchemaCooldown = 30 * time.Minute
const maxRejectedMCPSchemas = 4096

// Process-local, bounded negative cache: short-lived kernels for the same Bot
// share it; unrelated Bots/models/schema versions do not. Restart, expiry, a
// changed schema or a different binding permits the original definition again.
// Only hashes and expiry times are retained, never upstream errors or schemas.
var rejectedMCPSchemas = struct {
	sync.Mutex
	entries map[[32]byte]time.Time
}{entries: make(map[[32]byte]time.Time)}

type mcpSchemaAttempt struct {
	k       *Kernel
	retried bool
}

func (a *mcpSchemaAttempt) key(td provider.ToolDef) ([32]byte, bool) {
	if a == nil || a.k == nil || !strings.HasPrefix(td.Name, "mcp_") || len(td.Name) > 64 {
		return [32]byte{}, false
	}
	for _, c := range td.Name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return [32]byte{}, false
		}
	}
	scope := fmt.Sprintf("%p", a.k)
	if a.k.Bot != nil {
		scope = bot.Dir(a.k.Bot.Root, a.k.Bot.Name())
	}
	raw, err := json.Marshal(td.Parameters)
	if err != nil || len(raw) > 1<<20 {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(scope + "\x00" + a.k.boundModelKey() + "\x00" + td.Name + "\x00" + string(raw))), true
}

func (a *mcpSchemaAttempt) filter(req provider.Request) provider.Request {
	if a == nil || a.k == nil {
		return req
	}
	rejectedMCPSchemas.Lock()
	empty := len(rejectedMCPSchemas.entries) == 0
	rejectedMCPSchemas.Unlock()
	if empty {
		return req
	}
	var kept []provider.ToolDef
	var removed []string
	now := a.k.now()
	for i, td := range req.Tools {
		key, ok := a.key(td)
		blocked := false
		if ok {
			rejectedMCPSchemas.Lock()
			until, found := rejectedMCPSchemas.entries[key]
			blocked = found && now.Before(until)
			if found && !blocked {
				delete(rejectedMCPSchemas.entries, key)
			}
			rejectedMCPSchemas.Unlock()
		}
		if blocked {
			if len(removed) == 0 {
				kept = append(make([]provider.ToolDef, 0, len(req.Tools)-1), req.Tools[:i]...)
			}
			removed = append(removed, td.Name)
		} else if len(removed) > 0 {
			kept = append(kept, td)
		}
	}
	if len(removed) == 0 {
		return req
	}
	req.Tools = kept
	// A bounded request-local note, appended after tool results. It does not
	// rewrite history, the fixed prompt, MCP schemas, grants or executable tools.
	names := removed
	if len(names) > 32 {
		names = names[:32]
	}
	note := fmt.Sprintf("[MCP schema fallback: %d tool(s) temporarily unavailable: %s. The provider rejected a tool schema; when it did not identify a tool, MCP tools in that request were suspended together. Continue with the remaining tools or explain the limitation. Original schemas can be tried after 30 minutes, a schema/model change, or restart.]", len(removed), strings.Join(names, ", "))
	req.Messages = append(append([]provider.Message(nil), req.Messages...), provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(note)})
	return req
}

func (a *mcpSchemaAttempt) reject(req provider.Request, err error) bool {
	if a == nil || a.k == nil || a.retried {
		return false
	}
	name, rejected := provider.ToolSchemaRejection(err)
	if !rejected {
		return false
	}
	var keys [][32]byte
	for _, td := range req.Tools {
		if name != "" && td.Name != name {
			continue
		}
		if key, ok := a.key(td); ok {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return false
	}
	now := a.k.now()
	rejectedMCPSchemas.Lock()
	for key, until := range rejectedMCPSchemas.entries {
		if !now.Before(until) {
			delete(rejectedMCPSchemas.entries, key)
		}
	}
	for _, key := range keys {
		if _, exists := rejectedMCPSchemas.entries[key]; exists {
			continue
		}
		if len(rejectedMCPSchemas.entries) >= maxRejectedMCPSchemas {
			// Evict the oldest cooldown, bounding memory even across many Bots.
			var oldest [32]byte
			var expiry time.Time
			for candidate, until := range rejectedMCPSchemas.entries {
				if expiry.IsZero() || until.Before(expiry) {
					oldest, expiry = candidate, until
				}
			}
			delete(rejectedMCPSchemas.entries, oldest)
		}
		rejectedMCPSchemas.entries[key] = now.Add(mcpSchemaCooldown)
	}
	rejectedMCPSchemas.Unlock()
	a.retried = true
	if a.k.Bot != nil {
		_ = a.k.Bot.LogTrace("mcp_schema", "tools_suspended", now)
	}
	return true
}

// chat adds at most one schema retry across all attempts of a logical Chat,
// independent of the existing overflow retry. No retry after streamed output
// or cancellation, and the second error is always returned to the caller.
func (a *mcpSchemaAttempt) chat(ctx context.Context, req provider.Request, delta func(string), send func(context.Context, provider.Request, func(string)) (provider.Message, error)) (provider.Request, provider.Message, error) {
	req = a.filter(req)
	emitted := false
	emit := func(s string) {
		if s != "" {
			emitted = true
		}
		if delta != nil {
			delta(s)
		}
	}
	msg, err := send(ctx, req, emit)
	if err != nil && !emitted && ctx.Err() == nil && a.reject(req, err) {
		req = a.filter(req)
		msg, err = send(ctx, req, delta)
	}
	return req, msg, err
}
