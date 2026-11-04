package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func serveDevice(w http.ResponseWriter, r *http.Request, fn DeviceHandler) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, err error) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	}
	if fn == nil {
		fail(http.StatusNotFound, errors.New("device: runtime unavailable"))
		return
	}
	var in DeviceRequest
	if status, err := decodePOSTJSON(w, r, "device", &in); err != nil {
		fail(status, err)
		return
	}
	if err := in.Validate(); err != nil {
		fail(http.StatusBadRequest, err)
		return
	}
	raw, problem := fn(optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID")), in)
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	// Preserve a received task even when a later operation reports an error.
	if problem != nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			fields = map[string]json.RawMessage{}
		}
		fields["error"], _ = json.Marshal(problem.Error())
		raw, _ = json.Marshal(fields)
	}
	if !json.Valid(raw) || len(raw)+1 > jsonReplyLimit {
		fail(http.StatusInternalServerError, errors.New("device: invalid runtime reply or reply exceeds 64 KiB"))
		return
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func CallDevice(ctx context.Context, endpoint, root, callID string, in DeviceRequest) (json.RawMessage, error) {
	raw, status, err := postJSON(ctx, endpoint, root, "device", "/v1/device", callID, in)
	if err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, errors.New("device: invalid runtime response")
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &failure)
	if failure.Error != "" {
		return raw, errors.New(failure.Error)
	}
	if status < 200 || status >= 300 {
		return raw, fmt.Errorf("device: runtime returned HTTP %d", status)
	}
	return bytes.TrimSpace(raw), nil
}
