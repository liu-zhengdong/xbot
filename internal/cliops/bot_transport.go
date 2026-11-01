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

func serveBot(w http.ResponseWriter, r *http.Request, handlers *BotHandlers) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(BotCreateReply{Error: message})
	}
	if handlers == nil {
		fail(http.StatusNotFound, "bot: runtime unavailable")
		return
	}
	ctx := optionalSourceCallID(r.Context(), r.Header.Get("X-XBot-Call-ID"))
	var reply any
	var problem error
	switch strings.TrimPrefix(r.URL.Path, "/v1/bot/") {
	case "list":
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "bot: list requires GET")
			return
		}
		if handlers.List == nil {
			fail(http.StatusNotFound, "bot: list unavailable")
			return
		}
		value, err := handlers.List(ctx)
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "info":
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "bot: info requires GET")
			return
		}
		if handlers.Info == nil {
			fail(http.StatusNotFound, "bot: info unavailable")
			return
		}
		input := BotInfoQuery{Bot: r.URL.Query().Get("bot")}
		if raw := r.URL.Query().Get("steps"); raw != "" {
			steps, err := strconv.Atoi(raw)
			if err != nil {
				fail(http.StatusBadRequest, "bot: steps must be an integer")
				return
			}
			input.Steps = steps
		}
		value, err := handlers.Info(ctx, input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "create":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "bot: create requires POST")
			return
		}
		if handlers.Create == nil {
			fail(http.StatusNotFound, "bot: create unavailable")
			return
		}
		var input BotCreate
		if err := decodeBotRequest(w, r, &input); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		value, err := handlers.Create(ctx, input)
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "persona":
		if r.Method != http.MethodPut {
			fail(http.StatusMethodNotAllowed, "persona: set requires PUT")
			return
		}
		if handlers.Persona == nil {
			fail(http.StatusNotFound, "persona: runtime unavailable")
			return
		}
		var input BotPersona
		if err := decodeBotRequest(w, r, &input); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		value, err := handlers.Persona(ctx, input)
		if err != nil {
			value.Error = err.Error()
		}
		// A display failure must still acknowledge a committed write. Neither
		// output form may exceed the existing Shell's 32 KiB budget.
		wire, marshalErr := json.Marshal(value)
		if marshalErr != nil || len(wire)+1 > 32<<10 || len(FormatBotPersona(value)) > 32<<10 {
			err = errors.New("persona: runtime result exceeds 32 KiB Shell output budget")
			value.Error = err.Error()
			wire, _ = json.Marshal(value)
			if len(wire)+1 > 32<<10 || len(FormatBotPersona(value)) > 32<<10 {
				value.Name = ""
			}
		}
		reply, problem = value, err
	case "delete":
		if r.Method != http.MethodDelete {
			fail(http.StatusMethodNotAllowed, "bot: delete requires DELETE")
			return
		}
		if handlers.Delete == nil {
			fail(http.StatusNotFound, "bot: delete unavailable")
			return
		}
		value, err := handlers.Delete(ctx, r.URL.Query().Get("name"))
		if err != nil {
			value.Error = err.Error()
		}
		reply, problem = value, err
	case "pin", "primary":
		action := strings.TrimPrefix(r.URL.Path, "/v1/bot/")
		if r.Method != http.MethodPut {
			fail(http.StatusMethodNotAllowed, "bot: "+action+" requires PUT")
			return
		}
		if len(r.URL.RawQuery) > BotMaxRequest {
			fail(http.StatusBadRequest, "bot: request exceeds 1 MiB")
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query["name"]) != 1 || query.Get("name") == "" {
			fail(http.StatusBadRequest, "bot: name requires one value")
			return
		}
		for key := range query {
			if key != "name" && !(action == "pin" && key == "pinned") {
				fail(http.StatusBadRequest, "bot: unknown query field "+key)
				return
			}
		}
		if action == "pin" {
			if len(query["pinned"]) != 1 || (query.Get("pinned") != "true" && query.Get("pinned") != "false") {
				fail(http.StatusBadRequest, "bot: pinned requires one literal true or false")
				return
			}
			if handlers.Pin == nil {
				fail(http.StatusNotFound, "bot: pin unavailable")
				return
			}
			value, err := handlers.Pin(ctx, BotPin{Name: query.Get("name"), Pinned: query.Get("pinned") == "true"})
			if err != nil {
				value.Error = err.Error()
			}
			reply, problem = value, err
		} else {
			if handlers.Primary == nil {
				fail(http.StatusNotFound, "bot: primary unavailable")
				return
			}
			value, err := handlers.Primary(ctx, query.Get("name"))
			if err != nil {
				value.Error = err.Error()
			}
			reply, problem = value, err
		}
	default:
		fail(http.StatusNotFound, "bot: unknown action")
		return
	}
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > BotMaxReply {
		fail(http.StatusInternalServerError, "bot: runtime response exceeds 64 KiB")
		return
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func decodeBotRequest(w http.ResponseWriter, r *http.Request, input any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, BotMaxRequest))
	if err != nil {
		return errors.New("bot: invalid request or request exceeds 1 MiB")
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("bot: request must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return errors.New("bot: invalid request fields")
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("bot: request must be one JSON object followed by EOF")
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > BotMaxRequest {
		return errors.New("bot: request exceeds 1 MiB after JSON encoding")
	}
	return nil
}

func CallBotList(ctx context.Context, endpoint, root string) (BotListReply, error) {
	var result BotListReply
	err := callBot(ctx, endpoint, root, "", http.MethodGet, "list", nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallBotInfo(ctx context.Context, endpoint, root string, query BotInfoQuery) (BotInfoReply, error) {
	var result BotInfoReply
	values := url.Values{"bot": {query.Bot}, "steps": {strconv.Itoa(query.Steps)}}
	err := callBot(ctx, endpoint, root, "", http.MethodGet, "info?"+values.Encode(), nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallBotCreate(ctx context.Context, endpoint, root string, input BotCreate) (BotCreateReply, error) {
	var result BotCreateReply
	err := callBot(ctx, endpoint, root, "", http.MethodPost, "create", input, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallBotPersona(ctx context.Context, endpoint, root, callID string, input BotPersona) (BotPersonaReply, error) {
	var result BotPersonaReply
	err := callBot(ctx, endpoint, root, callID, http.MethodPut, "persona", input, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallBotDelete(ctx context.Context, endpoint, root, callID, name string) (BotDeleteReply, error) {
	var result BotDeleteReply
	err := callBot(ctx, endpoint, root, callID, http.MethodDelete, "delete?name="+url.QueryEscape(name), nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallBotPin(ctx context.Context, endpoint, root string, input BotPin) (BotPinReply, error) {
	var result BotPinReply
	query := url.Values{"name": {input.Name}, "pinned": {strconv.FormatBool(input.Pinned)}}
	err := callBot(ctx, endpoint, root, "", http.MethodPut, "pin?"+query.Encode(), nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func CallBotPrimary(ctx context.Context, endpoint, root, name string) (BotPrimaryReply, error) {
	var result BotPrimaryReply
	err := callBot(ctx, endpoint, root, "", http.MethodPut, "primary?name="+url.QueryEscape(name), nil, &result)
	if result.Error != "" {
		err = errors.New(result.Error)
	}
	return result, err
}

func callBot(ctx context.Context, endpoint, root, callID, method, action string, input, result any) error {
	root, err := sourceRoot(endpoint, root, "bot")
	if err != nil {
		return err
	}
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
		if len(body) > BotMaxRequest {
			return errors.New("bot: request exceeds 1 MiB after JSON encoding")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"/v1/bot/"+action, bytes.NewReader(body))
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
		return fmt.Errorf("bot: runtime request: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, BotMaxReply+1))
	if err != nil || len(raw) > BotMaxReply {
		return errors.New("bot: invalid runtime response or response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.New("bot: invalid runtime response")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("bot: runtime returned HTTP %d", response.StatusCode)
	}
	return nil
}
