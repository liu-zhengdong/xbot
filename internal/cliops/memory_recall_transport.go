package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func serveMemoryRecall(w http.ResponseWriter, r *http.Request, callback func(context.Context, MemoryRecall) (MemoryRecallReply, error)) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(MemoryRecallReply{Error: message})
	}
	if callback == nil {
		fail(http.StatusNotFound, "memory-recall: runtime unavailable")
		return
	}
	var input MemoryRecall
	if status, err := decodePOSTJSON(w, r, "memory-recall", &input); err != nil {
		fail(status, err.Error())
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	reply, problem := callback(ctx, input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundTextResult(reply, "memory-recall: reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > MemoryRecallMaxReply {
		fail(http.StatusInternalServerError, "memory-recall: runtime reply exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || displayErr != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallMemoryRecall forwards the actual source execution ID. The live source
// resolves that ID to the original prepared context; this client never retries.
func CallMemoryRecall(ctx context.Context, endpoint, root, callID string, input MemoryRecall) (MemoryRecallReply, error) {
	var reply MemoryRecallReply
	if callID == "" {
		return reply, errors.New("memory-recall: requires XBOT_CALL_ID from an active XBot shell")
	}
	raw, status, err := postJSON(ctx, endpoint, root, "memory-recall", "/v1/memory/recall", callID, input)
	if err != nil {
		return reply, err
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return MemoryRecallReply{}, errors.New("memory-recall: invalid runtime response")
	}
	reply, err = boundTextResult(reply, "memory-recall: reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		problem := fmt.Errorf("memory-recall: runtime returned HTTP %d", status)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
