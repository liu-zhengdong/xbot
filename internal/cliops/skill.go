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

const SkillMaxRequest = 1 << 20
const SkillMaxReply = 64 << 10

type SkillWrite struct {
	Slug        string  `json:"slug"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description"`
	Scope       string  `json:"scope,omitempty"`
	Body        *string `json:"body"`
}

type SkillReadReply struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description"`
	Body        *string `json:"body,omitempty"`
	Preview     string  `json:"preview,omitempty"`
	File        string  `json:"file,omitempty"`
	Access      string  `json:"access,omitempty"`
	Bytes       int64   `json:"bytes"`
	Error       string  `json:"error,omitempty"`
}

type SkillWriteReply struct {
	Slug        string   `json:"slug"`
	Written     bool     `json:"written"`
	Scope       string   `json:"scope"`
	Target      string   `json:"target,omitempty"`
	UpdatedBots []string `json:"updated_bots"`
	Error       string   `json:"error,omitempty"`
}

type SkillHandlers struct {
	Read  func(context.Context, string) (SkillReadReply, error)
	Write func(context.Context, SkillWrite) (SkillWriteReply, error)
}

func serveSkill(w http.ResponseWriter, r *http.Request, handlers *SkillHandlers) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		if len(message) > 4096 {
			message = message[:4096]
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}
	if handlers == nil {
		fail(http.StatusNotFound, "skill: runtime unavailable")
		return
	}
	var reply any
	var err error
	switch strings.TrimPrefix(r.URL.Path, "/v1/skill/") {
	case "read":
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "skill: read requires GET")
			return
		}
		if handlers.Read == nil {
			fail(http.StatusNotFound, "skill: read unavailable")
			return
		}
		result, problem := handlers.Read(r.Context(), r.URL.Query().Get("id"))
		if problem != nil {
			result.Error = problem.Error()
		}
		reply, err = result, problem
	case "write":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "skill: write requires POST")
			return
		}
		if handlers.Write == nil {
			fail(http.StatusNotFound, "skill: write unavailable")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, SkillMaxRequest)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var in SkillWrite
		if decodeErr := decoder.Decode(&in); decodeErr != nil {
			fail(http.StatusBadRequest, "skill: invalid request")
			return
		}
		if decodeErr := decoder.Decode(new(any)); !errors.Is(decodeErr, io.EOF) {
			fail(http.StatusBadRequest, "skill: expected one request")
			return
		}
		result, problem := handlers.Write(r.Context(), in)
		if problem != nil {
			result.Error = problem.Error()
		}
		reply, err = result, problem
	default:
		fail(http.StatusNotFound, "skill: unknown action")
		return
	}
	raw, marshalErr := json.Marshal(reply)
	if marshalErr != nil || len(raw)+1 > SkillMaxReply {
		fail(http.StatusInternalServerError, "skill: runtime response too large")
		return
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

// CallSkillRead uses the original source endpoint, whose Resources owns self.
func CallSkillRead(ctx context.Context, endpoint, root, id string) (SkillReadReply, error) {
	var result SkillReadReply
	raw, status, err := callSkill(ctx, endpoint, root, http.MethodGet, "read?id="+url.QueryEscape(id), nil)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("skill: invalid runtime response")
	}
	return result, skillResponseError(status, result.Error)
}

// CallSkillWrite retains the decoded partial receipt when the runtime reports failure.
func CallSkillWrite(ctx context.Context, endpoint, root string, in SkillWrite) (SkillWriteReply, error) {
	var result SkillWriteReply
	body, err := json.Marshal(in)
	if err != nil {
		return result, err
	}
	if len(body) > SkillMaxRequest {
		return result, errors.New("skill: request too large")
	}
	raw, status, err := callSkill(ctx, endpoint, root, http.MethodPost, "write", body)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("skill: invalid runtime response")
	}
	return result, skillResponseError(status, result.Error)
}

func callSkill(ctx context.Context, endpoint, root, method, action string, body []byte) ([]byte, int, error) {
	root, err := sourceRoot(endpoint, root, "skill")
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"/v1/skill/"+action, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("skill: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, SkillMaxReply+1))
	if err != nil || len(raw) > SkillMaxReply {
		return nil, 0, errors.New("skill: invalid runtime response")
	}
	return raw, response.StatusCode, nil
}

func skillResponseError(status int, message string) error {
	if message != "" {
		return errors.New(message)
	}
	if status != http.StatusOK {
		return fmt.Errorf("skill: runtime returned HTTP %d", status)
	}
	return nil
}
