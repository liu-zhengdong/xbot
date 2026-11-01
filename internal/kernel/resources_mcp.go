package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/mcp"
)

func (r *BotResources) MCPHandlers() cliops.MCPHandlers {
	return cliops.MCPHandlers{List: r.MCPList, Add: r.MCPAdd, Remove: r.MCPRemove, Tools: r.MCPTools, Describe: r.MCPDescribe, Call: r.MCPCall}
}

func mcpScope(scope string) (string, error) {
	if scope == "" {
		return "global", nil
	}
	if scope != "global" && scope != "bot" {
		return "", errors.New("mcp: scope must be global or bot")
	}
	return scope, nil
}

func (r *BotResources) MCPList(ctx context.Context, scope string) (cliops.MCPReply, error) {
	scope, err := mcpScope(scope)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	if err := ctx.Err(); err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	r.route.RLock()
	if r.closed || r.ctx.Err() != nil {
		r.route.RUnlock()
		return cliops.MCPReply{}, errResourcesUnavailable
	}
	options := mcp.Options{IsProviderKey: config.ProviderKey}
	var list, local []mcp.Spec
	if scope == "global" {
		list, err = mcp.LoadMCP(r.current.GlobalMCPPath(), options)
		if err == nil {
			local, err = mcp.LoadMCP(r.current.MCPPath(), options)
		}
	} else {
		list, err = mcp.LoadMCP(r.current.MCPPath(), options)
	}
	r.route.RUnlock()
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	shadowed := make(map[string]bool, len(local))
	for _, spec := range local {
		shadowed[spec.Name] = true
	}
	type row struct {
		mcp.Spec
		Source   string     `json:"source"`
		Shadowed bool       `json:"shadowed"`
		Runtime  mcp.Status `json:"runtime"`
	}
	rows := make([]row, 0, len(list))
	for _, spec := range list {
		rows = append(rows, row{Spec: spec, Source: scope, Shadowed: shadowed[spec.Name], Runtime: r.MCPStatus(spec)})
	}
	return r.mcpMetadata(map[string]any{"scope": scope, "servers": rows})
}

func (r *BotResources) MCPAdd(ctx context.Context, scope string, spec mcp.Spec) (cliops.MCPReply, error) {
	scope, err := mcpScope(scope)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	if err := ctx.Err(); err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	r.route.RLock()
	if r.closed || r.ctx.Err() != nil {
		r.route.RUnlock()
		return cliops.MCPReply{}, errResourcesUnavailable
	}
	path := r.current.GlobalMCPPath()
	if scope == "bot" {
		path = r.current.MCPPath()
	}
	err = mcp.AddMCP(path, spec, mcp.Options{IsProviderKey: config.ProviderKey})
	r.route.RUnlock()
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	return r.mcpSaved(ctx, scope, spec.Name)
}

func (r *BotResources) MCPRemove(ctx context.Context, scope, name string) (cliops.MCPReply, error) {
	scope, err := mcpScope(scope)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	if err := ctx.Err(); err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	r.route.RLock()
	if r.closed || r.ctx.Err() != nil {
		r.route.RUnlock()
		return cliops.MCPReply{}, errResourcesUnavailable
	}
	path := r.current.GlobalMCPPath()
	if scope == "bot" {
		path = r.current.MCPPath()
	}
	err = mcp.RemoveMCP(path, name, mcp.Options{IsProviderKey: config.ProviderKey})
	r.route.RUnlock()
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	return r.mcpSaved(ctx, scope, name)
}

func (r *BotResources) mcpSaved(ctx context.Context, scope, name string) (cliops.MCPReply, error) {
	var err error
	if scope == "global" {
		err = r.manager.Reconcile(ctx)
	} else {
		err = r.MCP.Reconcile(ctx)
	}
	reply, encodeErr := r.mcpMetadata(map[string]string{"scope": scope, "name": name, "state": "configured"})
	reply.Saved = true
	if err != nil {
		reply.RuntimeError = r.mcpRedact(err.Error())
	}
	if encodeErr != nil {
		return reply, encodeErr
	}
	return r.boundMCPReply(reply)
}

func (r *BotResources) MCPTools(ctx context.Context, server string) (cliops.MCPReply, error) {
	tools, err := r.MCP.LoadTools(ctx, server)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	return r.mcpMetadata(map[string]any{"server": server, "tools": tools})
}

func (r *BotResources) MCPDescribe(ctx context.Context, server, remote string) (cliops.MCPReply, error) {
	tools, err := r.MCP.LoadTools(ctx, server)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	for _, tool := range tools {
		if tool.Name == remote {
			return r.mcpMetadata(tool)
		}
	}
	return cliops.MCPReply{}, r.mcpFailure(fmt.Errorf("mcp: tool %q not found on %q", remote, server))
}

func (r *BotResources) MCPCall(ctx context.Context, call cliops.MCPCall) (cliops.MCPReply, error) {
	raw, err := r.MCP.CallTool(ctx, call.Server, call.Tool, call.Args, mcp.CallOptions{ReadOnly: call.ReadOnly})
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	return r.projectMCPCall(raw)
}

func (r *BotResources) mcpMetadata(value any) (cliops.MCPReply, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	raw, err = r.redactMCPJSON(raw)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	reply := cliops.MCPReply{Data: raw}
	encoded, err := json.Marshal(reply)
	if err != nil {
		return cliops.MCPReply{}, r.mcpFailure(err)
	}
	if len(encoded)+1 <= cliops.MCPMaxReply {
		return reply, nil
	}
	artifact, err := r.saveMCPResult(raw, "json")
	if err != nil {
		return cliops.MCPReply{ProjectionErrors: []string{"save metadata: " + r.mcpRedact(err.Error())}}, nil
	}
	return cliops.MCPReply{Artifact: artifact}, nil
}
