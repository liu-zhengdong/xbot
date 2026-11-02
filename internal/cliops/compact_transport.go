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

func serveCompact(w http.ResponseWriter, r *http.Request, fn func(context.Context, Compact) (CompactReply, error)) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(CompactReply{Error: message})
	}
	if fn == nil {
		fail(http.StatusNotFound, "compact: runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "compact: requires POST")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(http.StatusBadRequest, "compact: query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, CompactMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, "compact: invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, "compact: request must be one JSON object")
		return
	}
	var input Compact
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(http.StatusBadRequest, "compact: invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, "compact: request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > CompactMaxRequest {
		fail(http.StatusBadRequest, "compact: request exceeds 1 MiB after JSON encoding")
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	reply, problem := fn(ctx, input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundCompactReply(reply)
	if displayErr != nil {
		fail(http.StatusInternalServerError, displayErr.Error())
		return
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > CompactMaxReply {
		fail(http.StatusInternalServerError, "compact: runtime reply exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallCompact carries the original Shell call ID to the live source runtime.
// It preserves the caller context and never retries or follows redirects.
func CallCompact(ctx context.Context, endpoint, root, originalCallID string, input Compact) (CompactReply, error) {
	var reply CompactReply
	root, err := sourceRoot(endpoint, root, "compact")
	if err != nil {
		return reply, err
	}
	if originalCallID == "" {
		return reply, errors.New("compact: requires XBOT_CALL_ID from an active XBot shell")
	}
	if len(input.Focus) > CompactMaxRequest {
		return reply, errors.New("compact: focus input exceeds 1 MiB")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(body) > CompactMaxRequest {
		return reply, errors.New("compact: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/compact", bytes.NewReader(body))
	if err != nil {
		return reply, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("X-XBot-Call-ID", originalCallID)
	req.Header.Set("Content-Type", "application/json")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return reply, fmt.Errorf("compact: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, CompactMaxReply+1))
	if err != nil || len(raw) > CompactMaxReply {
		return reply, errors.New("compact: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return CompactReply{}, errors.New("compact: invalid runtime response")
	}
	reply, err = boundCompactReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("compact: runtime returned HTTP %d", response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
