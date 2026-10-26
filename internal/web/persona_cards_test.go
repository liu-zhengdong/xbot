package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/personacard"
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

// A pick that checked the card before a delete removed it must not bind the
// dead id. The test plays the delete by holding the library lock (as another
// process would) and removing the card while the pick waits.
func TestPickWaitsForDeleteUnderLibraryLock(t *testing.T) {
	root, _, call := skillTestServer(t)
	code, raw := call("POST", "/api/persona-cards", `{"name":"简洁","body":"先给结论。"}`, true)
	if code != 200 {
		t.Fatalf("create %d %s", code, raw)
	}
	var card struct{ ID string }
	_ = json.Unmarshal([]byte(raw), &card)
	lock, err := fsutil.Lock(filepath.Join(personacard.OwnerDir(root, "u1"), ".lock"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		code, _ := call("PUT", "/api/bots/ada/persona-card", `{"id":"`+card.ID+`"}`, true)
		done <- code
	}()
	select {
	case code := <-done:
		lock.Release()
		t.Fatalf("pick answered %d without the library lock", code)
	case <-time.After(200 * time.Millisecond):
	}
	if err := os.RemoveAll(filepath.Join(personacard.OwnerDir(root, "u1"), card.ID)); err != nil {
		t.Fatal(err)
	}
	lock.Release()
	if code := <-done; code != 404 {
		t.Fatalf("pick of a deleted card answered %d", code)
	}
	b, err := bot.Load(root, "ada")
	if err != nil || b.Config().PersonaCard != "" {
		t.Fatalf("bound to a dead card: %q %v", b.Config().PersonaCard, err)
	}
}

// The list carries only the start of each body; ?q= searches the whole card,
// so a word past the first KiB of a long Chinese card is still found.
func TestPersonaCardSearchCoversWholeBody(t *testing.T) {
	_, _, call := skillTestServer(t)
	body := strings.Repeat("中", 600) + "关键词Zebra"
	payload, _ := json.Marshal(map[string]string{"name": "长卡", "body": body})
	if code, raw := call("POST", "/api/persona-cards", string(payload), true); code != 200 {
		t.Fatalf("create %d %s", code, raw)
	}
	if code, raw := call("POST", "/api/persona-cards", `{"name":"短卡","body":"先给结论。"}`, true); code != 200 {
		t.Fatalf("create %d %s", code, raw)
	}
	var out struct {
		Cards []struct{ Name, Search string }
	}
	code, raw := call("GET", "/api/persona-cards?q=zebra", "", true)
	if err := json.Unmarshal([]byte(raw), &out); code != 200 || err != nil || len(out.Cards) != 1 || out.Cards[0].Name != "长卡" {
		t.Fatalf("search %d %s", code, raw)
	}
	if strings.Contains(out.Cards[0].Search, "Zebra") {
		t.Fatal("test needs the word past the list's search text")
	}
	if code, raw = call("GET", "/api/persona-cards?q=短卡", "", true); !strings.Contains(raw, `"name":"短卡"`) || strings.Contains(raw, "长卡") {
		t.Fatalf("name search %d %s", code, raw)
	}
	if code, _ = call("GET", "/api/persona-cards?q="+strings.Repeat("x", 5000), "", true); code != 200 {
		t.Fatalf("long query %d", code)
	}
}

// A library opened for the first time lists the built-in cards from disk: they search, open and pick like any card.
func TestPersonaCardListSeedsAnEmptyLibrary(t *testing.T) {
	root, _, call := skillTestServer(t)
	var out struct {
		Cards []struct{ ID, Name string }
	}
	code, raw := call("GET", "/api/persona-cards", "", true)
	if err := json.Unmarshal([]byte(raw), &out); code != 200 || err != nil || len(out.Cards) != 3 {
		t.Fatalf("list %d %s", code, raw)
	}
	for i, want := range []string{"碎嘴群友", "值班运维", "周末搭子"} {
		if out.Cards[i].Name != want {
			t.Fatalf("card %d %s", i, raw)
		}
	}
	if disk, _ := personacard.List(root, "u1"); len(disk) != 3 {
		t.Fatalf("not on disk: %+v", disk)
	}
	if code, raw = call("GET", "/api/persona-cards/"+out.Cards[0].ID, "", true); code != 200 || !strings.Contains(raw, "这个我接不上") {
		t.Fatalf("open %d %s", code, raw)
	}
	if code, raw = call("GET", "/api/persona-cards?q=群友", "", true); !strings.Contains(raw, `"name":"碎嘴群友"`) || strings.Contains(raw, "值班运维") {
		t.Fatalf("search %d %s", code, raw)
	}
	if code, raw = call("PUT", "/api/bots/ada/persona-card", `{"id":"`+out.Cards[1].ID+`"}`, true); code != 200 || !strings.Contains(raw, `"name":"值班运维"`) {
		t.Fatalf("pick %d %s", code, raw)
	}
	if code, raw = call("GET", "/api/persona-cards", "", true); strings.Count(raw, `"id":`) != 3 {
		t.Fatalf("second list %d %s", code, raw)
	}
}
