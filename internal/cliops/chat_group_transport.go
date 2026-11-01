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
	"strconv"
	"strings"
)

func serveChatGroup(w http.ResponseWriter, r *http.Request, handlers *ChatGroupHandlers) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(struct {
			Error string `json:"error"`
		}{message})
	}
	if handlers == nil {
		fail(http.StatusNotFound, "chat-group: runtime unavailable")
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/v1/chat-group/")
	method := http.MethodGet
	if action == "create" || action == "post" {
		method = http.MethodPost
	}
	switch action {
	case "list", "history", "create", "post":
	default:
		fail(http.StatusNotFound, "chat-group: unknown action")
		return
	}
	if r.Method != method {
		fail(http.StatusMethodNotAllowed, "chat-group: "+action+" requires "+method)
		return
	}
	if len(r.URL.RawQuery) > ChatGroupMaxRequest {
		fail(http.StatusBadRequest, "chat-group: request exceeds 1 MiB")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		fail(http.StatusBadRequest, "chat-group: invalid query")
		return
	}
	for key, values := range query {
		if action != "history" || (key != "id" && key != "limit" && key != "before_id" && key != "sender" && key != "keyword") {
			fail(http.StatusBadRequest, "chat-group: unknown query field")
			return
		}
		if len(values) != 1 {
			fail(http.StatusBadRequest, "chat-group: query fields require one value")
			return
		}
	}
	var reply any
	var human string
	var problem error
	switch action {
	case "list":
		if handlers.List == nil {
			fail(http.StatusNotFound, "chat-group: list unavailable")
			return
		}
		value, err := handlers.List(r.Context())
		if err != nil {
			value.Error = err.Error()
		}
		reply, human, problem = value, FormatChatGroupList(value), err
	case "history":
		if handlers.History == nil {
			fail(http.StatusNotFound, "chat-group: history unavailable")
			return
		}
		if len(query["id"]) != 1 || query.Get("id") == "" {
			fail(http.StatusBadRequest, "chat-group: id requires one nonempty value")
			return
		}
		input := ChatGroupHistory{ID: query.Get("id"), BeforeID: query.Get("before_id"), Sender: query.Get("sender"), Keyword: query.Get("keyword")}
		if values, ok := query["limit"]; ok {
			n, err := strconv.Atoi(values[0])
			if err != nil {
				fail(http.StatusBadRequest, "chat-group: limit must be an integer")
				return
			}
			input.Limit = n
		}
		wire, err := json.Marshal(input)
		if err != nil || len(wire) > ChatGroupMaxRequest {
			fail(http.StatusBadRequest, "chat-group: request exceeds 1 MiB after JSON encoding")
			return
		}
		value, err := handlers.History(r.Context(), input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, human, problem = value, FormatChatGroupHistory(value), err
	case "create":
		if handlers.Create == nil {
			fail(http.StatusNotFound, "chat-group: create unavailable")
			return
		}
		var input ChatGroupCreate
		if err := decodeChatGroupRequest(w, r, &input); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		value, err := handlers.Create(r.Context(), input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, human, problem = value, FormatChatGroupCreate(value), err
	case "post":
		if handlers.Post == nil {
			fail(http.StatusNotFound, "chat-group: post unavailable")
			return
		}
		var input ChatGroupPost
		if err := decodeChatGroupRequest(w, r, &input); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
		value, err := handlers.Post(ctx, input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, human, problem = value, FormatChatGroupPost(value), err
	}
	// Source handlers project large reads. The transport only bounds their receipt;
	// it never creates artifacts or discards committed-write facts.
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > 32<<10 || len(human) > 32<<10 {
		problem = errors.New("chat-group: runtime result exceeds 32 KiB Shell output budget")
		switch value := reply.(type) {
		case ChatGroupCreateReply:
			value.Error = problem.Error()
			raw, _ = json.Marshal(value)
			if len(raw)+1 > 32<<10 || len(FormatChatGroupCreate(value)) > 32<<10 {
				value.Group = nil
			}
			reply, human = value, FormatChatGroupCreate(value)
		case ChatGroupPostReply:
			value.Error = problem.Error()
			reply, human = value, FormatChatGroupPost(value)
		default:
			fail(http.StatusInternalServerError, problem.Error())
			return
		}
		raw, err = json.Marshal(reply)
	}
	if err != nil || len(raw)+1 > min(ChatGroupMaxReply, 32<<10) || len(human) > 32<<10 {
		fail(http.StatusInternalServerError, "chat-group: runtime receipt exceeds output budget")
		return
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func decodeChatGroupRequest(w http.ResponseWriter, r *http.Request, input any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, ChatGroupMaxRequest))
	if err != nil {
		return errors.New("chat-group: invalid request or request exceeds 1 MiB")
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("chat-group: request must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return errors.New("chat-group: invalid request fields")
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("chat-group: request must be one JSON object followed by EOF")
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > ChatGroupMaxRequest {
		return errors.New("chat-group: request exceeds 1 MiB after JSON encoding")
	}
	return nil
}

func CallChatGroupList(ctx context.Context, endpoint, root string) (ChatGroupListReply, error) {
	var reply ChatGroupListReply
	err := callChatGroup(ctx, endpoint, root, "", http.MethodGet, "list", nil, &reply)
	if reply.Error != "" {
		err = errors.New(reply.Error)
	}
	return reply, err
}

func CallChatGroupHistory(ctx context.Context, endpoint, root string, input ChatGroupHistory) (ChatGroupHistoryReply, error) {
	var reply ChatGroupHistoryReply
	query := url.Values{"id": {input.ID}, "limit": {strconv.Itoa(input.Limit)}, "before_id": {input.BeforeID}, "sender": {input.Sender}, "keyword": {input.Keyword}}
	err := callChatGroup(ctx, endpoint, root, "", http.MethodGet, "history?"+query.Encode(), input, &reply)
	if reply.Error != "" {
		err = errors.New(reply.Error)
	}
	return reply, err
}

func CallChatGroupCreate(ctx context.Context, endpoint, root string, input ChatGroupCreate) (ChatGroupCreateReply, error) {
	var reply ChatGroupCreateReply
	err := callChatGroup(ctx, endpoint, root, "", http.MethodPost, "create", input, &reply)
	if reply.Error != "" {
		err = errors.New(reply.Error)
	}
	return reply, err
}

func CallChatGroupPost(ctx context.Context, endpoint, root, callID string, input ChatGroupPost) (ChatGroupPostReply, error) {
	var reply ChatGroupPostReply
	err := callChatGroup(ctx, endpoint, root, callID, http.MethodPost, "post", input, &reply)
	if reply.Error != "" {
		err = errors.New(reply.Error)
	}
	return reply, err
}

func callChatGroup(ctx context.Context, endpoint, root, callID, method, action string, input, result any) error {
	root, err := sourceRoot(endpoint, root, "chat-group")
	if err != nil {
		return err
	}
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
		if len(body) > ChatGroupMaxRequest {
			return errors.New("chat-group: request exceeds 1 MiB after JSON encoding")
		}
	}
	if len(action) > ChatGroupMaxRequest {
		return errors.New("chat-group: request exceeds 1 MiB after URL encoding")
	}
	if method == http.MethodGet {
		body = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"/v1/chat-group/"+action, bytes.NewReader(body))
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
		return fmt.Errorf("chat-group: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, ChatGroupMaxReply+1))
	if err != nil || len(raw) > ChatGroupMaxReply {
		return errors.New("chat-group: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.New("chat-group: invalid runtime response")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("chat-group: runtime returned HTTP %d", response.StatusCode)
	}
	return nil
}
