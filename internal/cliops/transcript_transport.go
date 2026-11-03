package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func serveTranscriptSearch(w http.ResponseWriter, r *http.Request, handlers *TranscriptHandlers) {
	if handlers == nil || handlers.Search == nil {
		writeTranscriptReadReply(w, http.StatusNotFound, TranscriptReadReply{Error: "transcript: search runtime unavailable"})
		return
	}
	var input TranscriptSearch
	if status, err := decodePOSTJSON(w, r, "transcript", &input); err != nil {
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
	if status, err := decodePOSTJSON(w, r, "transcript", &input); err != nil {
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
	if status, err := decodePOSTJSON(w, r, "transcript", &input); err != nil {
		writeTranscriptReadReply(w, status, TranscriptReadReply{Error: err.Error()})
		return
	}
	reply, err := handlers.Detail(r.Context(), input)
	finishTranscriptRead(w, reply, err)
}

func finishTranscriptRead(w http.ResponseWriter, reply TranscriptReadReply, problem error) {
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundTextResult(reply, "transcript: reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
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
	raw, status, err := postJSON(ctx, endpoint, root, "transcript", "/v1/transcript/"+action, "", input)
	if err != nil {
		return reply, err
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return TranscriptReadReply{}, errors.New("transcript: invalid runtime response")
	}
	reply, err = boundTextResult(reply, "transcript: reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		err := fmt.Errorf("transcript: runtime returned HTTP %d", status)
		reply.Error = err.Error()
		return reply, err
	}
	return reply, nil
}
