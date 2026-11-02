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

func serveTranscriptSearch(w http.ResponseWriter, r *http.Request, handlers *TranscriptHandlers) {
	if handlers == nil || handlers.Search == nil {
		writeTranscriptReadReply(w, http.StatusNotFound, TranscriptReadReply{Error: "transcript: search runtime unavailable"})
		return
	}
	var input TranscriptSearch
	if status, err := decodeTranscriptRequest(w, r, &input); err != nil {
		writeTranscriptReadReply(w, status, TranscriptReadReply{Error: err.Error()})
		return
	}
	reply, err := handlers.Search(r.Context(), input)
	finishTranscriptRead(w, reply, err)
}

func serveTranscriptExpand(w http.ResponseWriter, r *http.Request, handlers *TranscriptHandlers) {
	if handlers == nil || handlers.Expand == nil {
		writeTranscriptReadReply(w, http.StatusNotFound, TranscriptReadReply{Error: "transcript: expand runtime unavailable"})
		return
	}
	var input TranscriptExpand
	if status, err := decodeTranscriptRequest(w, r, &input); err != nil {
		writeTranscriptReadReply(w, status, TranscriptReadReply{Error: err.Error()})
		return
	}
	reply, err := handlers.Expand(r.Context(), input)
	finishTranscriptRead(w, reply, err)
}

func serveTranscriptDetail(w http.ResponseWriter, r *http.Request, handlers *TranscriptHandlers) {
	if handlers == nil || handlers.Detail == nil {
		writeTranscriptReadReply(w, http.StatusNotFound, TranscriptReadReply{Error: "transcript: detail runtime unavailable"})
		return
	}
	var input TranscriptDetail
	if status, err := decodeTranscriptRequest(w, r, &input); err != nil {
		writeTranscriptReadReply(w, status, TranscriptReadReply{Error: err.Error()})
		return
	}
	reply, err := handlers.Detail(r.Context(), input)
	finishTranscriptRead(w, reply, err)
}

func decodeTranscriptRequest(w http.ResponseWriter, r *http.Request, input any) (int, error) {
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, errors.New("transcript: requires POST")
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return http.StatusBadRequest, errors.New("transcript: query fields are not supported")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, TranscriptMaxRequest))
	if err != nil {
		return http.StatusBadRequest, errors.New("transcript: invalid request or request exceeds 1 MiB")
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return http.StatusBadRequest, errors.New("transcript: request must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return http.StatusBadRequest, errors.New("transcript: invalid request fields")
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return http.StatusBadRequest, errors.New("transcript: request must be one JSON object followed by EOF")
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > TranscriptMaxRequest {
		return http.StatusBadRequest, errors.New("transcript: request exceeds 1 MiB after JSON encoding")
	}
	return 0, nil
}

func finishTranscriptRead(w http.ResponseWriter, reply TranscriptReadReply, problem error) {
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundTranscriptReadReply(reply)
	status := http.StatusOK
	if problem != nil || displayErr != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	writeTranscriptReadReply(w, status, reply)
}

func writeTranscriptReadReply(w http.ResponseWriter, status int, reply TranscriptReadReply) {
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > TranscriptMaxReply {
		status = http.StatusInternalServerError
		raw = []byte(`{"error":"transcript: runtime response exceeds 64 KiB"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// The original methods have no source call ID requirement. These clients send
// only source root and typed input, and never retry the operation.
func CallTranscriptSearch(ctx context.Context, endpoint, root string, input TranscriptSearch) (TranscriptReadReply, error) {
	if len(input.Query) > TranscriptMaxRequest {
		return TranscriptReadReply{}, errors.New("transcript: query input exceeds 1 MiB")
	}
	return callTranscriptRead(ctx, endpoint, root, "search", input)
}

func CallTranscriptExpand(ctx context.Context, endpoint, root string, input TranscriptExpand) (TranscriptReadReply, error) {
	return callTranscriptRead(ctx, endpoint, root, "expand", input)
}

func CallTranscriptDetail(ctx context.Context, endpoint, root string, input TranscriptDetail) (TranscriptReadReply, error) {
	return callTranscriptRead(ctx, endpoint, root, "detail", input)
}

func callTranscriptRead(ctx context.Context, endpoint, root, action string, input any) (TranscriptReadReply, error) {
	var reply TranscriptReadReply
	root, err := sourceRoot(endpoint, root, "transcript")
	if err != nil {
		return reply, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(body) > TranscriptMaxRequest {
		return reply, errors.New("transcript: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/transcript/"+action, bytes.NewReader(body))
	if err != nil {
		return reply, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return reply, fmt.Errorf("transcript: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, TranscriptMaxReply+1))
	if err != nil || len(raw) > TranscriptMaxReply {
		return reply, errors.New("transcript: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return TranscriptReadReply{}, errors.New("transcript: invalid runtime response")
	}
	reply, err = boundTranscriptReadReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		err := fmt.Errorf("transcript: runtime returned HTTP %d", response.StatusCode)
		reply.Error = err.Error()
		return reply, err
	}
	return reply, nil
}
