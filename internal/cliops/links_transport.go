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

func serveLinksSearch(w http.ResponseWriter, r *http.Request, handlers *LinksHandlers) {
	var fn func(context.Context, LinksSearch) (LinksReply, error)
	if handlers != nil {
		fn = handlers.Search
	}
	serveLinks(w, r, "search", fn, &LinksSearch{})
}

func serveLinksLoad(w http.ResponseWriter, r *http.Request, handlers *LinksHandlers) {
	var fn func(context.Context, LinksLoad) (LinksReply, error)
	if handlers != nil {
		fn = handlers.Load
	}
	serveLinks(w, r, "load", fn, &LinksLoad{})
}

func serveLinksInstall(w http.ResponseWriter, r *http.Request, handlers *LinksHandlers) {
	var fn func(context.Context, LinksInstall) (LinksReply, error)
	if handlers != nil {
		fn = handlers.Install
	}
	serveLinks(w, r, "install", fn, &LinksInstall{})
}

func serveLinksRemove(w http.ResponseWriter, r *http.Request, handlers *LinksHandlers) {
	var fn func(context.Context, LinksRemove) (LinksReply, error)
	if handlers != nil {
		fn = handlers.Remove
	}
	serveLinks(w, r, "remove", fn, &LinksRemove{})
}

// serveLinks enforces one strict JSON object per action and binds the original
// Shell call ID into the handler context before the domain reply is bound.
func serveLinks[I any](w http.ResponseWriter, r *http.Request, action string, fn func(context.Context, I) (LinksReply, error), input *I) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(LinksReply{Error: message})
	}
	if fn == nil {
		fail(http.StatusNotFound, "links: "+action+" runtime unavailable")
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "links: requires POST")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(http.StatusBadRequest, "links: query fields are not supported")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, LinksMaxRequest))
	if err != nil {
		fail(http.StatusBadRequest, "links: invalid request or request exceeds 1 MiB")
		return
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		fail(http.StatusBadRequest, "links: request must be one JSON object")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		fail(http.StatusBadRequest, "links: invalid request fields")
		return
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		fail(http.StatusBadRequest, "links: request must be one JSON object followed by EOF")
		return
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > LinksMaxRequest {
		fail(http.StatusBadRequest, "links: request exceeds 1 MiB after JSON encoding")
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	reply, problem := fn(ctx, *input)
	if problem != nil {
		reply.Error = problem.Error()
	}
	reply, displayErr := boundLinksReply(reply)
	if displayErr != nil {
		fail(http.StatusInternalServerError, displayErr.Error())
		return
	}
	raw, err = json.Marshal(reply)
	if err != nil || len(raw)+1 > LinksMaxReply {
		fail(http.StatusInternalServerError, "links: runtime reply exceeds HTTP output budget")
		return
	}
	status := http.StatusOK
	if problem != nil || reply.Error != "" {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallLinksSearch carries the original Shell call ID to the live source
// runtime. CallLinksLoad does the same for the load action; neither retries.
func CallLinksSearch(ctx context.Context, endpoint, root, originalCallID string, input LinksSearch) (LinksReply, error) {
	if len(input.Query) > LinksMaxRequest {
		return LinksReply{}, errors.New("links: query input exceeds 1 MiB")
	}
	return callLinks(ctx, endpoint, root, originalCallID, "search", input)
}

func CallLinksLoad(ctx context.Context, endpoint, root, originalCallID string, input LinksLoad) (LinksReply, error) {
	return callLinks(ctx, endpoint, root, originalCallID, "load", input)
}

func CallLinksInstall(ctx context.Context, endpoint, root, originalCallID string, input LinksInstall) (LinksReply, error) {
	return callLinks(ctx, endpoint, root, originalCallID, "install", input)
}

func CallLinksRemove(ctx context.Context, endpoint, root, originalCallID string, input LinksRemove) (LinksReply, error) {
	return callLinks(ctx, endpoint, root, originalCallID, "remove", input)
}

func callLinks(ctx context.Context, endpoint, root, originalCallID, action string, input any) (LinksReply, error) {
	var reply LinksReply
	root, err := sourceRoot(endpoint, root, "links")
	if err != nil {
		return reply, err
	}
	if originalCallID == "" {
		return reply, errors.New("links: requires XBOT_CALL_ID from an active XBot shell")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return reply, err
	}
	if len(body) > LinksMaxRequest {
		return reply, errors.New("links: request exceeds 1 MiB after JSON encoding")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/links/"+action, bytes.NewReader(body))
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
		return reply, fmt.Errorf("links: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, LinksMaxReply+1))
	if err != nil || len(raw) > LinksMaxReply {
		return reply, errors.New("links: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return LinksReply{}, errors.New("links: invalid runtime response")
	}
	reply, err = boundLinksReply(reply)
	if err != nil {
		return reply, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := fmt.Errorf("links: runtime returned HTTP %d", response.StatusCode)
		reply.Error = problem.Error()
		return reply, problem
	}
	return reply, nil
}
