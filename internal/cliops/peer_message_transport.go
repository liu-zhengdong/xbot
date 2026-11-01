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

func servePeerMessage(w http.ResponseWriter, r *http.Request, callback func(context.Context, PeerMessage) (PeerMessageReply, error)) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(PeerMessageReply{Error: message})
	}
	if callback == nil {
		fail(http.StatusNotFound, "peer-message: runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "peer-message: requires POST")
		return
	}
	if r.URL.RawQuery != "" {
		fail(http.StatusBadRequest, "peer-message: query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, PeerMessageMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, "peer-message: invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, "peer-message: request must be one JSON object")
		return
	}
	var input PeerMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(http.StatusBadRequest, "peer-message: invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, "peer-message: request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > PeerMessageMaxRequest {
		fail(http.StatusBadRequest, "peer-message: request exceeds 1 MiB after JSON encoding")
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	reply, problem := callback(ctx, input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundPeerMessageReply(reply)
	if displayErr != nil {
		problem = displayErr
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > PeerMessageMaxReply {
		fail(http.StatusInternalServerError, "peer-message: runtime receipt exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallPeerMessage uses only the original source endpoint. A call ID is optional
// on the wire; the live source handler validates the actual prepared execution.
func CallPeerMessage(ctx context.Context, endpoint, root, callID string, input PeerMessage) (PeerMessageReply, error) {
	var reply PeerMessageReply
	root, err := sourceRoot(endpoint, root, "peer-message")
	if err != nil {
		return reply, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(input.Text) > PeerMessageMaxRequest || len(body) > PeerMessageMaxRequest {
		return reply, errors.New("peer-message: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/peer/message", bytes.NewReader(body))
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
		return reply, fmt.Errorf("peer-message: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, PeerMessageMaxReply+1))
	if err != nil || len(raw) > PeerMessageMaxReply {
		return reply, errors.New("peer-message: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return PeerMessageReply{}, errors.New("peer-message: invalid runtime response")
	}
	reply, err = boundPeerMessageReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("peer-message: runtime returned HTTP %d", response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
