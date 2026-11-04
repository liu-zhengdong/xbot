package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"xbot/internal/mcp"
)

// ServeMCP is shared by guarded source routes and authenticated owner routes.
func ServeMCP(w http.ResponseWriter, r *http.Request, h MCPHandlers, action string) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, err error) {
		text := err.Error()
		if len(text) > 4096 {
			text = text[:4096]
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": text})
	}
	decode := func(value any) error {
		r.Body = http.MaxBytesReader(w, r.Body, MCPMaxRequest)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(value); err != nil {
			return errors.New("mcp: invalid request")
		}
		if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
			return errors.New("mcp: expected one request")
		}
		return nil
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "global"
	}
	if scope != "global" && scope != "bot" {
		fail(400, errors.New("mcp: invalid scope"))
		return
	}
	var result MCPReply
	var err error
	switch action {
	case "servers":
		switch r.Method {
		case http.MethodGet:
			result, err = h.List(r.Context(), scope)
		case http.MethodPost:
			var in struct {
				Scope string   `json:"scope"`
				Spec  mcp.Spec `json:"spec"`
			}
			if err = decode(&in); err != nil {
				break
			}
			if in.Scope != "" {
				scope = in.Scope
			}
			if scope != "global" && scope != "bot" {
				err = errors.New("mcp: invalid scope")
				break
			}
			result, err = h.Add(r.Context(), scope, in.Spec)
		case http.MethodDelete:
			name := r.URL.Query().Get("server")
			if name == "" {
				err = errors.New("mcp: server is required")
				break
			}
			result, err = h.Remove(r.Context(), scope, name)
		default:
			fail(405, errors.New("method not allowed"))
			return
		}
	case "tools", "describe":
		if r.Method != http.MethodGet {
			fail(405, errors.New("method not allowed"))
			return
		}
		server, tool := r.URL.Query().Get("server"), r.URL.Query().Get("tool")
		if server == "" || (action == "describe" && tool == "") {
			fail(400, errors.New("mcp: server/tool is required"))
			return
		}
		if action == "tools" {
			result, err = h.Tools(r.Context(), server)
		} else {
			result, err = h.Describe(r.Context(), server, tool)
		}
	case "call":
		if r.Method != http.MethodPost {
			fail(405, errors.New("method not allowed"))
			return
		}
		var in MCPCall
		if err = decode(&in); err != nil {
			break
		}
		if len(in.Args) == 0 {
			in.Args = json.RawMessage(`{}`)
		}
		if in.Server == "" || in.Tool == "" {
			err = errors.New("mcp: server/tool is required")
			break
		}
		if err = ValidateMCPArgs(in.Args); err != nil {
			break
		}
		result, err = h.Call(r.Context(), in)
	default:
		fail(404, errors.New("not found"))
		return
	}
	if err != nil {
		fail(400, err)
		return
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > MCPMaxReply {
		fail(500, errors.New("mcp: result exceeds reply budget"))
		return
	}
	_, _ = w.Write(raw)
}

func MCPHTTPRequest(ctx context.Context, base, prefix, root string, c MCPCommand, args json.RawMessage) (*http.Request, error) {
	query := url.Values{}
	path, method := "servers", http.MethodGet
	var body any
	switch c.Action {
	case "list":
		query.Set("scope", c.Scope)
	case "add":
		method = http.MethodPost
		body = struct {
			Scope string   `json:"scope"`
			Spec  mcp.Spec `json:"spec"`
		}{c.Scope, mcp.Spec{Name: c.Server, Command: c.Command, Args: c.CommandArgs, URL: c.URL, SecretEnvs: c.SecretEnvs}}
	case "remove":
		method = http.MethodDelete
		query.Set("scope", c.Scope)
		query.Set("server", c.Server)
	case "tools":
		path = "tools"
		query.Set("server", c.Server)
	case "describe":
		path = "describe"
		query.Set("server", c.Server)
		query.Set("tool", c.Tool)
	case "call":
		path = "call"
		method = http.MethodPost
		body = MCPCall{Server: c.Server, Tool: c.Tool, Args: args, ReadOnly: c.ReadOnly}
	default:
		return nil, errors.New("mcp: invalid action")
	}
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if len(encoded) > MCPMaxRequest {
			return nil, errors.New("mcp: request exceeds 1 MiB")
		}
	}
	target := strings.TrimRight(base, "/") + prefix + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if root != "" {
		req.Header.Set("X-XBot-Root", root)
	}
	return req, nil
}

func ReadMCPReply(response *http.Response) (MCPReply, error) {
	defer response.Body.Close()
	var reply MCPReply
	raw, err := io.ReadAll(io.LimitReader(response.Body, MCPMaxReply+1))
	if err != nil {
		return reply, err
	}
	if len(raw) > MCPMaxReply {
		return reply, errors.New("mcp: invalid oversized reply")
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		if failure.Error != "" {
			// 服务端已经带上 "mcp: " 前缀，别再套一层；不然命令行里会看到 mcp: mcp: xxx。
			if strings.HasPrefix(failure.Error, "mcp: ") {
				return reply, errors.New(failure.Error)
			}
			return reply, fmt.Errorf("mcp: %s", failure.Error)
		}
		return reply, fmt.Errorf("mcp: HTTP %d", response.StatusCode)
	}
	if err = json.Unmarshal(raw, &reply); err != nil {
		return reply, errors.New("mcp: invalid reply")
	}
	return reply, nil
}
