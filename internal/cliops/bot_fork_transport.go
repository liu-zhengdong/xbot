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

func serveBotFork(w http.ResponseWriter, r *http.Request, callback func(context.Context, BotFork) (BotForkReply, error)) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(BotForkReply{Error: message})
	}
	if callback == nil {
		fail(http.StatusNotFound, "bot-fork: runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "bot-fork: requires POST")
		return
	}
	if r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "bot-fork: query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, BotForkMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, "bot-fork: invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, "bot-fork: request must be one JSON object")
		return
	}
	var input BotFork
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(http.StatusBadRequest, "bot-fork: invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, "bot-fork: request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > BotForkMaxRequest {
		fail(http.StatusBadRequest, "bot-fork: request exceeds 1 MiB after JSON encoding")
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	reply, problem := callback(ctx, input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundBotForkReply(reply)
	if displayErr != nil {
		problem = displayErr
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > BotForkMaxReply {
		fail(http.StatusInternalServerError, "bot-fork: runtime receipt exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallBotFork uses only the original source endpoint. The wire call ID is
// optional; the source handler requires an actual prepared execution.
func CallBotFork(ctx context.Context, endpoint, root, callID string, input BotFork) (BotForkReply, error) {
	var reply BotForkReply
	root, err := sourceRoot(endpoint, root, "bot-fork")
	if err != nil {
		return reply, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(input.Role) > BotForkMaxRequest || len(body) > BotForkMaxRequest {
		return reply, errors.New("bot-fork: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/bot/fork", bytes.NewReader(body))
	if err != nil {
		return reply, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	if callID != "" {
		req.Header.Set("X-XBot-Call-ID", callID)
	}
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return reply, fmt.Errorf("bot-fork: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, BotForkMaxReply+1))
	if err != nil || len(raw) > BotForkMaxReply {
		return reply, errors.New("bot-fork: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return BotForkReply{}, errors.New("bot-fork: invalid runtime response")
	}
	reply, err = boundBotForkReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("bot-fork: runtime returned HTTP %d", response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
