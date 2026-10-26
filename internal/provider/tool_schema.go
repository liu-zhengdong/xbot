package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

var schemaFunctionName = regexp.MustCompile(`(?i)(?:function|tool)\s+['"]([a-zA-Z0-9_-]{1,64})['"]`)

// ToolSchemaRejection recognizes only explicit tool-definition failures from
// HTTP 400. The optional name is parsed, never copied from arbitrary error
// text into model context. Overflow retains priority over schema recovery.
func ToolSchemaRejection(err error) (name string, rejected bool) {
	var se *statusError
	if !errors.As(err, &se) || se.code != http.StatusBadRequest || IsContextOverflow(err) || len(se.body) > 64<<10 {
		return "", false
	}
	detail := se.body
	if strings.HasPrefix(strings.TrimSpace(detail), "{") {
		var envelope struct {
			Error   json.RawMessage `json:"error"`
			Message string          `json:"message"`
			Code    string          `json:"code"`
			Type    string          `json:"type"`
		}
		if json.Unmarshal([]byte(detail), &envelope) != nil {
			return "", false
		}
		if len(envelope.Error) > 0 {
			var problem struct {
				Message string `json:"message"`
				Code    string `json:"code"`
				Type    string `json:"type"`
			}
			if json.Unmarshal(envelope.Error, &problem) != nil {
				return "", false
			}
			envelope.Message, envelope.Code, envelope.Type = problem.Message, problem.Code, problem.Type
		}
		kind := strings.ToLower(envelope.Code + " " + envelope.Type)
		if strings.Contains(kind, "authentication") || strings.Contains(kind, "permission") || strings.Contains(kind, "invalid_api_key") {
			return "", false
		}
		detail = envelope.Message + " " + envelope.Code
	}
	body := strings.ToLower(detail)
	matched := false
	for _, phrase := range []string{"invalid tool schema", "invalid function schema", "invalid schema for function", "invalid schema for tool", "invalid_function_parameters", "invalid_tool_schema"} {
		if strings.Contains(body, phrase) {
			matched = true
			break
		}
	}
	// Anthropic reports the tool index rather than its function name.
	if strings.Contains(body, "input_schema") && (strings.Contains(body, "invalid") || strings.Contains(body, "must be")) && strings.Contains(body, "tools") {
		matched = true
	}
	if !matched {
		return "", false
	}
	if m := schemaFunctionName.FindStringSubmatch(detail); m != nil {
		return m[1], true
	}
	return "", true
}
