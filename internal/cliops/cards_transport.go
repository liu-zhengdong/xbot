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

func serveRestart(w http.ResponseWriter, r *http.Request, fn func(context.Context, RestartCall) (CardReply, error)) {
	serveCards(w, r, "restart", fn, &RestartCall{})
}

func serveHTTP(w http.ResponseWriter, r *http.Request, fn func(context.Context, HTTPCall) (CardReply, error)) {
	serveCards(w, r, "http", fn, &HTTPCall{})
}

// serveCards enforces one strict JSON object per action and binds the original
// Shell call ID into the handler context before the domain reply is bound.
func serveCards[I any](w http.ResponseWriter, r *http.Request, action string, fn func(context.Context, I) (CardReply, error), input *I) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(CardReply{Error: message})
	}
	if fn == nil {
		fail(http.StatusNotFound, action+": runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, action+": requires POST")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(http.StatusBadRequest, action+": query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, CardsMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, action+": invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, action+": request must be one JSON object")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		fail(http.StatusBadRequest, action+": invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, action+": request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > CardsMaxRequest {
		fail(http.StatusBadRequest, action+": request exceeds 1 MiB after JSON encoding")
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	reply, problem := fn(ctx, *input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundCardReply(reply)
	if displayErr != nil {
		fail(http.StatusInternalServerError, displayErr.Error())
		return
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > CardsMaxReply {
		fail(http.StatusInternalServerError, action+": runtime reply exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallRestart and CallHTTP carry the original Shell call ID to the live source
// runtime; neither retries.
func CallRestart(ctx context.Context, endpoint, root, originalCallID string, input RestartCall) (CardReply, error) {
	return callCards(ctx, endpoint, root, originalCallID, "restart", input)
}

func CallHTTP(ctx context.Context, endpoint, root, originalCallID string, input HTTPCall) (CardReply, error) {
	return callCards(ctx, endpoint, root, originalCallID, "http", input)
}

func callCards(ctx context.Context, endpoint, root, originalCallID, action string, input any) (CardReply, error) {
	var reply CardReply
	root, err := sourceRoot(endpoint, root, action)
	if err != nil {
		return reply, err
	}
	if originalCallID == "" {
		return reply, fmt.Errorf("%s: requires XBOT_CALL_ID from an active XBot shell", action)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(body) > CardsMaxRequest {
		return reply, fmt.Errorf("%s: request exceeds 1 MiB after JSON encoding", action)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/"+action, bytes.NewReader(body))
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
		return reply, fmt.Errorf("%s: runtime request: %w", action, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, CardsMaxReply+1))
	if err != nil || len(raw) > CardsMaxReply {
		return reply, fmt.Errorf("%s: invalid runtime response or response exceeds 64 KiB", action)
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return CardReply{}, fmt.Errorf("%s: invalid runtime response", action)
	}
	reply, err = boundCardReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("%s: runtime returned HTTP %d", action, response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
