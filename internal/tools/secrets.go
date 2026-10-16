package tools

import (
	"context"
	"encoding/json"
	"strings"

	"xbot/internal/vault"
)

// SecretList reports credential names and their allowed domains. Values are
// never included; the model may use a name in {{secret:NAME}} for http_request.
type SecretList struct {
	Store *vault.Store
}

func (s *SecretList) Name() string { return "list_secrets" }

func (s *SecretList) Description() string {
	return "List stored credential names and the domains each may be sent to. Values are never returned."
}

func (s *SecretList) Parameters() any {
	return objectSchema(map[string]any{}, nil)
}

func (s *SecretList) Confirm(json.RawMessage) (bool, string, error) {
	return false, "list credential names", nil
}

func (s *SecretList) Run(context.Context, json.RawMessage) (string, error) {
	if s.Store == nil {
		return "no credentials", nil
	}
	list := s.Store.List()
	if len(list) == 0 {
		return "no credentials", nil
	}
	var b strings.Builder
	for _, sec := range list {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sec.Name)
		b.WriteByte('\t')
		b.WriteString(strings.Join(sec.Domains, ","))
	}
	return b.String(), nil
}
