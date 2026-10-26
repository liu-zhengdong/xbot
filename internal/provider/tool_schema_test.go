package provider

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestToolSchemaRejection(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		tool string
		want bool
	}{
		{"plain", NewStatusError(400, "invalid tool schema"), "", true},
		{"named", NewStatusError(400, `{"error":{"message":"Invalid schema for function 'mcp_test_bad': oneOf not supported","code":"invalid_function_parameters"}}`), "mcp_test_bad", true},
		{"wrapped", fmt.Errorf("wrapped: %w", NewStatusError(400, "Invalid schema for tool 'mcp_test_bad'")), "mcp_test_bad", true},
		{"messages", NewStatusError(400, `{"error":{"message":"tools.0.input_schema: must be a valid JSON schema"}}`), "", true},
		{"code", NewStatusError(400, `{"error":{"code":"invalid_function_parameters"}}`), "", true},
		{"builtin", NewStatusError(400, "Invalid schema for function 'shell'"), "shell", true},
		{"ordinary400", NewStatusError(400, "invalid request"), "", false},
		{"argument", NewStatusError(400, "invalid tool arguments"), "", false},
		{"outputschema", NewStatusError(400, "invalid response_format JSON schema"), "", false},
		{"overflow", NewStatusError(400, "maximum context length exceeded"), "", false},
		{"mixed", NewStatusError(400, "context window exceeded; invalid tool schema"), "", false},
		{"413", NewStatusError(413, "invalid tool schema"), "", false},
		{"auth401", NewStatusError(401, "invalid tool schema"), "", false},
		{"auth403", NewStatusError(403, "invalid tool schema"), "", false},
		{"rate", NewStatusError(429, "invalid tool schema"), "", false},
		{"server", NewStatusError(500, "invalid tool schema"), "", false},
		{"untyped", errors.New("provider: http 400: invalid tool schema"), "", false},
		{"oversize", NewStatusError(400, "invalid tool schema"+strings.Repeat("x", 64<<10)), "", false},
		{"nil", nil, "", false},
		{"json_quoted_name", NewStatusError(400, `{"error":{"message":"Invalid schema for function \"mcp_test_bad\""}}`), "mcp_test_bad", true},
		{"auth400", NewStatusError(400, `{"error":{"type":"authentication_error","message":"invalid tool schema"}}`), "", false},
		{"reflected_input", NewStatusError(400, `{"error":{"message":"invalid request","input":{"description":"invalid tool schema"}}}`), "", false},
		{"malformed", NewStatusError(400, `{"error": invalid tool schema`), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, ok := ToolSchemaRejection(tc.err)
			if name != tc.tool || ok != tc.want {
				t.Fatalf("(%q,%v), want (%q,%v)", name, ok, tc.tool, tc.want)
			}
		})
	}
}
