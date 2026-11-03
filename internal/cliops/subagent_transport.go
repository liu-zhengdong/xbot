package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func serveSubagentList(w http.ResponseWriter, r *http.Request, handlers *SubagentHandlers) {
	var fn func(context.Context, SubagentList) (SubagentReply, error)
	if handlers != nil {
		fn = handlers.List
	}
	serveSubagent(w, r, "list", fn, &SubagentList{})
}

func serveSubagentRead(w http.ResponseWriter, r *http.Request, handlers *SubagentHandlers) {
	var fn func(context.Context, SubagentRead) (SubagentReply, error)
	if handlers != nil {
		fn = handlers.Read
	}
	serveSubagent(w, r, "read", fn, &SubagentRead{})
}

func serveSubagentExpand(w http.ResponseWriter, r *http.Request, handlers *SubagentHandlers) {
	var fn func(context.Context, SubagentExpand) (SubagentReply, error)
	if handlers != nil {
		fn = handlers.Expand
	}
	serveSubagent(w, r, "expand", fn, &SubagentExpand{})
}

// serveSubagent enforces one strict JSON object per action. The original
// methods have no source call ID requirement, same as the transcript reads.
func serveSubagent[I any](w http.ResponseWriter, r *http.Request, action string, fn func(context.Context, I) (SubagentReply, error), input *I) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(SubagentReply{Error: message})
	}
	if fn == nil {
		fail(http.StatusNotFound, "subagent: "+action+" runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "subagent: requires POST")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(http.StatusBadRequest, "subagent: query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, SubagentMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, "subagent: invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, "subagent: request must be one JSON object")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		fail(http.StatusBadRequest, "subagent: invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, "subagent: request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > SubagentMaxRequest {
		fail(http.StatusBadRequest, "subagent: request exceeds 1 MiB after JSON encoding")
		return
	}
	reply, problem := fn(r.Context(), *input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundSubagentReply(reply)
	if displayErr != nil {
		fail(http.StatusInternalServerError, displayErr.Error())
		return
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > SubagentMaxReply {
		fail(http.StatusInternalServerError, "subagent: runtime reply exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// The original methods have no source call ID requirement. These clients send
// only source root and typed input, and never retry the operation.
func CallSubagentList(ctx context.Context, endpoint, root string, input SubagentList) (SubagentReply, error) {
	return callSubagent(ctx, endpoint, root, "list", input)
}

func CallSubagentRead(ctx context.Context, endpoint, root string, input SubagentRead) (SubagentReply, error) {
	return callSubagent(ctx, endpoint, root, "read", input)
}

func CallSubagentExpand(ctx context.Context, endpoint, root string, input SubagentExpand) (SubagentReply, error) {
	return callSubagent(ctx, endpoint, root, "expand", input)
}

func callSubagent(ctx context.Context, endpoint, root, action string, input any) (SubagentReply, error) {
	var reply SubagentReply
	root, err := sourceRoot(endpoint, root, "subagent")
	if err != nil {
		return reply, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(body) > SubagentMaxRequest {
		return reply, errors.New("subagent: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/subagent/"+action, bytes.NewReader(body))
	if err != nil {
		return reply, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return reply, fmt.Errorf("subagent: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, SubagentMaxReply+1))
	if err != nil || len(raw) > SubagentMaxReply {
		return reply, errors.New("subagent: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return SubagentReply{}, errors.New("subagent: invalid runtime response")
	}
	reply, err = boundSubagentReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("subagent: runtime returned HTTP %d", response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
