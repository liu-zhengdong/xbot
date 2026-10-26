package web

import (
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestPersonaCardAPI(t *testing.T) {
	root, _, call := skillTestServer(t)
	if code, _ := call("GET", "/api/persona-cards", "", false); code != 401 {
		t.Fatalf("unauthenticated list %d", code)
	}
	code, raw := call("POST", "/api/persona-cards", `{"name":"简洁","body":"先给结论。"}`, true)
	if code != 200 {
		t.Fatalf("create %d %s", code, raw)
	}
	var card struct{ ID, Name, Body string }
	_ = json.Unmarshal([]byte(raw), &card)
	if card.ID == "" || card.Name != "简洁" {
		t.Fatal(raw)
	}
	code, raw = call("GET", "/api/persona-cards", "", true)
	if code != 200 || !strings.Contains(raw, `"preview":"先给结论。"`) || !strings.Contains(raw, `"search":"先给结论。"`) {
		t.Fatalf("list %d %s", code, raw)
	}
	if code, raw = call("PUT", "/api/persona-cards/"+card.ID, `{"body":"<b>只说一句</b>"}`, true); code != 200 {
		t.Fatalf("update %d %s", code, raw)
	}
	if code, raw = call("PUT", "/api/persona-cards/"+card.ID, `{"body":"  "}`, true); code != 400 || !strings.Contains(raw, "不能为空") {
		t.Fatalf("empty update %d %s", code, raw)
	}

	// Import never overwrites: the same name asks first, then becomes " (2)".
	imp := `{"filename":"x.md","content":"# 简洁\n\n另一份。"}`
	if code, raw = call("POST", "/api/persona-cards/import", imp, true); code != 409 || !strings.Contains(raw, `"conflict":true`) {
		t.Fatalf("same-name import %d %s", code, raw)
	}
	imp = `{"filename":"x.md","content":"# 简洁\n\n另一份。","confirm":true}`
	if code, raw = call("POST", "/api/persona-cards/import", imp, true); code != 200 || !strings.Contains(raw, `"name":"简洁 (2)"`) {
		t.Fatalf("confirmed import %d %s", code, raw)
	}

	// Pick it for two bots; the panel shows the card name, and the prompt uses its body.
	for _, name := range []string{"ada", "bob"} {
		if code, raw = call("PUT", "/api/bots/"+name+"/persona-card", `{"id":"`+card.ID+`"}`, true); code != 200 || !strings.Contains(raw, `"name":"简洁"`) {
			t.Fatalf("pick %s %d %s", name, code, raw)
		}
	}
	if code, _ = call("PUT", "/api/bots/eve/persona-card", `{"id":"`+card.ID+`"}`, true); code != 404 {
		t.Fatalf("someone else's bot %d", code)
	}
	if code, _ = call("PUT", "/api/bots/ada/persona-card", `{"id":"0123456789abcdef"}`, true); code != 404 {
		t.Fatalf("missing card %d", code)
	}
	code, raw = call("GET", "/api/bots/ada", "", true)
	if code != 200 || !strings.Contains(raw, `"persona_card":{"id":"`+card.ID+`","name":"简洁"}`) {
		t.Fatalf("panel %d %s", code, raw)
	}
	ada, _ := bot.Load(root, "ada")
	if got, _ := ada.ComposedPersona(); got != "<b>只说一句</b>" {
		t.Fatalf("persona %q", got)
	}
	if code, raw = call("GET", "/api/persona-cards/"+card.ID, "", true); code != 200 || !strings.Contains(raw, `"bots":2`) {
		t.Fatalf("detail %d %s", code, raw)
	}

	// Deleting a card in use asks first, then unbinds and deletes.
	if code, raw = call("DELETE", "/api/persona-cards/"+card.ID, "", true); code != 409 || !strings.Contains(raw, "2 个 Bot 还在用") {
		t.Fatalf("delete in use %d %s", code, raw)
	}
	if code, raw = call("DELETE", "/api/persona-cards/"+card.ID+"?force=1", "", true); code != 200 || !strings.Contains(raw, `"unbound":2`) {
		t.Fatalf("forced delete %d %s", code, raw)
	}
	for _, name := range []string{"ada", "bob"} {
		b, _ := bot.Load(root, name)
		if b.Config().PersonaCard != "" {
			t.Fatalf("%s still bound", name)
		}
		if got, _ := b.ComposedPersona(); got != "persona" {
			t.Fatalf("%s persona %q", name, got)
		}
	}
	code, raw = call("GET", "/api/bots/ada", "", true)
	if code != 200 || !strings.Contains(raw, `"persona_card":null`) {
		t.Fatalf("panel after delete %d %s", code, raw)
	}
	if code, _ = call("GET", "/api/persona-cards/..%2Fx", "", true); code != 404 {
		t.Fatalf("traversal %d", code)
	}
	if code, _ = call("PUT", "/api/bots/ada/persona-card", `{"id":""}`, true); code != 200 {
		t.Fatalf("unbind %d", code)
	}
}
