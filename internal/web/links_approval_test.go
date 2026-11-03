package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/provider"
)

// approvalSecretNames still reads the retired links tool name from the card
// summary, and the runtime install/remove cards arrive under a Shell tool
// name with the typed _xbot_links intent: install asks for the keys, remove
// carries none, and a plain shell card never does.
func TestApprovalSecretNames(t *testing.T) {
	installArgs := `{"description":"install","command":"\"$XBOT_BIN\" links install --self telegram","_xbot_links":{"action":"install","id":"telegram"}}`
	summary := "install telegram\nTelegram\nsecrets: TELEGRAM_BOT_TOKEN"
	cases := []struct {
		name    string
		tool    string
		args    string
		summary string
		want    []string
	}{
		{"retired links tool", "links", `{"action":"install","id":"telegram"}`, summary, []string{"TELEGRAM_BOT_TOKEN"}},
		{"shell install intent", "sh", installArgs, summary, []string{"TELEGRAM_BOT_TOKEN"}},
		{"zsh install intent", "zsh", installArgs, summary, []string{"TELEGRAM_BOT_TOKEN"}},
		{"remove asks nothing", "sh", `{"command":"\"$XBOT_BIN\" links remove --self telegram","_xbot_links":{"action":"remove","id":"telegram"}}`, "remove telegram", nil},
		{"shell without intent", "sh", `{"command":"ls"}`, "run ls", nil},
		{"non-shell card", "read_file", `{"path":"x"}`, "read x", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := approvalSecretNames(c.tool, json.RawMessage(c.args), c.summary)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("names %v", got)
			}
		})
	}
}

// The decision handler stages secrets under the link id parsed from the
// card's first line and only the names the card listed; keys the owner
// typed beyond the card never reach the stage.
func TestLinksApprovalStagingParsers(t *testing.T) {
	if got := linkIDFromSummary("install telegram\nTelegram\nsecrets: TELEGRAM_BOT_TOKEN"); got != "telegram" {
		t.Fatalf("install id %q", got)
	}
	if got := linkIDFromSummary("remove telegram"); got != "" {
		t.Fatalf("remove id %q", got)
	}
	got := filterSecrets([]string{"TELEGRAM_BOT_TOKEN"}, map[string]string{"TELEGRAM_BOT_TOKEN": "v", "OTHER": "drop"})
	if len(got) != 1 || got["TELEGRAM_BOT_TOKEN"] != "v" {
		t.Fatalf("filtered %v", got)
	}
	if names := secretNamesFromSummary("install telegram\nTelegram\nsecrets: TELEGRAM_BOT_TOKEN"); !reflect.DeepEqual(names, []string{"TELEGRAM_BOT_TOKEN"}) {
		t.Fatalf("names %v", names)
	}
	if names := secretNamesFromSummary("remove telegram"); names != nil {
		t.Fatalf("remove names %v", names)
	}
}

type turnProvider struct {
	resps []provider.Message
	i     int
}

func (s *turnProvider) Chat(_ context.Context, _ provider.Request, onDelta func(string)) (provider.Message, error) {
	if s.i >= len(s.resps) {
		return provider.Message{}, errors.New("no more responses")
	}
	m := s.resps[s.i]
	s.i++
	if onDelta != nil && m.Text() != "" {
		onDelta(m.Text())
	}
	return m, nil
}

func decideBody(t *testing.T, base string, cookie *http.Cookie, id, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/api/bots/ada/approvals/"+id, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("decide %d %s", res.StatusCode, raw)
	}
}

var _ = decideBody
