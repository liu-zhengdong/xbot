package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type routineCallIDKey struct{}

// RoutineCallID returns the source-call correlation header. The value alone
// does not establish an approval context or caller identity.
func RoutineCallID(ctx context.Context) string {
	id, _ := ctx.Value(routineCallIDKey{}).(string)
	return id
}

func serveRoutine(w http.ResponseWriter, r *http.Request, handlers *RoutineHandlers) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(RoutineWriteReply{Error: message})
	}
	if handlers == nil {
		fail(http.StatusNotFound, "routine: runtime unavailable")
		return
	}
	ctx := context.WithValue(r.Context(), routineCallIDKey{}, r.Header.Get("X-XBot-Call-ID"))
	var reply any
	var problem error
	switch strings.TrimPrefix(r.URL.Path, "/v1/routine/") {
	case "list":
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "routine: list requires GET")
			return
		}
		if handlers.List == nil {
			fail(http.StatusNotFound, "routine: list unavailable")
			return
		}
		value, err := handlers.List(ctx)
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "create":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "routine: create requires POST")
			return
		}
		if handlers.Create == nil {
			fail(http.StatusNotFound, "routine: create unavailable")
			return
		}
		var input RoutineCreate
		if err := decodeRoutineRequest(w, r, &input); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		value, err := handlers.Create(ctx, input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "update":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "routine: update requires POST")
			return
		}
		if handlers.Update == nil {
			fail(http.StatusNotFound, "routine: update unavailable")
			return
		}
		var input RoutinePatch
		if err := decodeRoutineRequest(w, r, &input); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		value, err := handlers.Update(ctx, r.URL.Query().Get("id"), input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "delete":
		if r.Method != http.MethodDelete {
			fail(http.StatusMethodNotAllowed, "routine: delete requires DELETE")
			return
		}
		if handlers.Delete == nil {
			fail(http.StatusNotFound, "routine: delete unavailable")
			return
		}
		value, err := handlers.Delete(ctx, r.URL.Query().Get("id"))
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	default:
		fail(http.StatusNotFound, "routine: unknown action")
		return
	}
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > RoutineMaxReply {
		fail(http.StatusInternalServerError, "routine: runtime response exceeds 64 KiB")
		return
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func decodeRoutineRequest(w http.ResponseWriter, r *http.Request, input any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, RoutineMaxRequest))
	if err != nil {
		return errors.New("routine: invalid request or request exceeds 1 MiB")
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("routine: request must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return errors.New("routine: invalid request fields")
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("routine: request must be one JSON object followed by EOF")
	}
	return nil
}

func CallRoutineList(ctx context.Context, endpoint, root string) (RoutineListReply, error) {
	var result RoutineListReply
	err := callRoutine(ctx, endpoint, root, "", http.MethodGet, "list", nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallRoutineCreate(ctx context.Context, endpoint, root, callID string, input RoutineCreate) (RoutineWriteReply, error) {
	var result RoutineWriteReply
	err := callRoutine(ctx, endpoint, root, callID, http.MethodPost, "create", input, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallRoutineUpdate(ctx context.Context, endpoint, root, callID, id string, input RoutinePatch) (RoutineWriteReply, error) {
	var result RoutineWriteReply
	err := callRoutine(ctx, endpoint, root, callID, http.MethodPost, "update?id="+url.QueryEscape(id), input, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallRoutineDelete(ctx context.Context, endpoint, root, callID, id string) (RoutineWriteReply, error) {
	var result RoutineWriteReply
	err := callRoutine(ctx, endpoint, root, callID, http.MethodDelete, "delete?id="+url.QueryEscape(id), nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func callRoutine(ctx context.Context, endpoint, root, callID, method, action string, input, result any) error {
	root, err := sourceRoot(endpoint, root, "routine")
	if err != nil {
		return err
	}
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
		if len(body) > RoutineMaxRequest {
			return errors.New("routine: request exceeds 1 MiB after JSON encoding")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"/v1/routine/"+action, bytes.NewReader(body))
	if err != nil {
		return err
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
		return fmt.Errorf("routine: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, RoutineMaxReply+1))
	if err != nil || len(raw) > RoutineMaxReply {
		return errors.New("routine: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.New("routine: invalid runtime response")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("routine: runtime returned HTTP %d", response.StatusCode)
	}
	return nil
}
