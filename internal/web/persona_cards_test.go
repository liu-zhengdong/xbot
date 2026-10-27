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

	// Pick it for two bots; the panel shows the card name, and the prompt adds its body after the duty.
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
	if code != 200 || !strings.Contains(raw, `"persona_card":{"id":"`+card.ID+`","name":"简洁","preview":`) || !strings.Contains(raw, `"persona":"persona`) {
		t.Fatalf("panel %d %s", code, raw)
	}
	ada, _ := bot.Load(root, "ada")
	if got, _ := ada.ComposedPersona(); !strings.HasPrefix(got, "persona\n\n### 人设卡 · 简洁\n") || !strings.HasSuffix(got, "<b>只说一句</b>") {
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
		if got, _ := b.ComposedPersona(); !strings.HasPrefix(got, "persona\n\n### 人设卡 · "+personacard.DefaultName+"\n") {
			t.Fatalf("%s persona %q", name, got)
		}
	}
	// The default card is what bots fall back to, so it cannot be deleted.
	def, _ := personacard.Default(root)
	if code, raw = call("DELETE", "/api/persona-cards/"+def.ID+"?force=1", "", true); code != 400 || !strings.Contains(raw, "默认人设卡不能删") {
		t.Fatalf("delete default %d %s", code, raw)
	}
	code, raw = call("GET", "/api/bots/ada", "", true)
	if code != 200 || !strings.Contains(raw, `"persona_card":{"id":"`+def.ID+`","name":"`+personacard.DefaultName+`"`) {
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
	lock, err := fsutil.Lock(filepath.Join(personacard.Dir(root), ".lock"))
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
	if err := os.RemoveAll(filepath.Join(personacard.Dir(root), card.ID)); err != nil {
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
	if err := json.Unmarshal([]byte(raw), &out); code != 200 || err != nil || len(out.Cards) != len(personacard.Seeds) {
		t.Fatalf("list %d %s", code, raw)
	}
	for i, want := range []string{"工程合伙人", "碎嘴群友", "值班运维", "周末搭子"} {
		if out.Cards[i].Name != want {
			t.Fatalf("card %d %s", i, raw)
		}
	}
	if disk, _ := personacard.List(root); len(disk) != len(personacard.Seeds) {
		t.Fatalf("not on disk: %+v", disk)
	}
	if code, raw = call("GET", "/api/persona-cards/"+out.Cards[1].ID, "", true); code != 200 || !strings.Contains(raw, "这个我接不上") {
		t.Fatalf("open %d %s", code, raw)
	}
	if code, raw = call("GET", "/api/persona-cards?q=群友", "", true); !strings.Contains(raw, `"name":"碎嘴群友"`) || strings.Contains(raw, "值班运维") {
		t.Fatalf("search %d %s", code, raw)
	}
	if code, raw = call("PUT", "/api/bots/ada/persona-card", `{"id":"`+out.Cards[2].ID+`"}`, true); code != 200 || !strings.Contains(raw, `"name":"值班运维"`) {
		t.Fatalf("pick %d %s", code, raw)
	}
	if code, raw = call("GET", "/api/persona-cards", "", true); strings.Count(raw, `"id":`) != len(personacard.Seeds) {
		t.Fatalf("second list %d %s", code, raw)
	}
}

// Another process holds the library lock in the middle of seeding (k seeds on disk).
// A list must wait for it, not show an empty or partial library, and must not seed again.
func TestPersonaCardListWaitsForASeedRunInAnotherProcess(t *testing.T) {
	for k := 0; k < len(personacard.Seeds); k++ {
		root, _, call := skillTestServer(t)
		dir := personacard.Dir(root)
		seed := func(i int) {
			id := strings.Repeat(string(rune('a'+i)), 16)
			raw := "name: " + personacard.Seeds[i].Name + "\n\n" + personacard.Seeds[i].Body + "\n"
			if err := fsutil.WriteAtomic(filepath.Join(dir, id, "card.md"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
		}
		got := make(chan string, 1)
		err := fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
			for i := 0; i < k; i++ {
				seed(i)
			}
			go func() { _, raw := call("GET", "/api/persona-cards", "", true); got <- raw }()
			select {
			case raw := <-got:
				t.Fatalf("%d seeds written: listed %s while the run held the lock", k, raw)
			case <-time.After(150 * time.Millisecond):
			}
			for i := k; i < len(personacard.Seeds); i++ {
				seed(i)
			}
			return fsutil.WriteAtomic(filepath.Join(dir, ".seeded"), nil, 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
		if raw := <-got; strings.Count(raw, `"id":`) != len(personacard.Seeds) || !strings.Contains(raw, "周末搭子") {
			t.Fatalf("%d seeds written: %s", k, raw)
		}
	}
}

// A failed pick or delete of a missing card on a new library leaves a lock file behind;
// the first list still gets the seeds.
func TestPersonaCardFailedPickStillSeeds(t *testing.T) {
	_, _, call := skillTestServer(t)
	if code, _ := call("PUT", "/api/bots/ada/persona-card", `{"id":"0123456789abcdef"}`, true); code != 404 {
		t.Fatalf("pick %d", code)
	}
	if code, _ := call("DELETE", "/api/persona-cards/0123456789abcdef", "", true); code != 404 {
		t.Fatalf("delete %d", code)
	}
	if code, raw := call("GET", "/api/persona-cards", "", true); strings.Count(raw, `"id":`) != len(personacard.Seeds) {
		t.Fatalf("list %d %s", code, raw)
	}
}

// A library from before the marker: deleting its last card over the API, then listing
// again (what the page does), must not bring the seeds in. Only the default card stays.
func TestPersonaCardEmptiedLegacyLibraryStaysEmpty(t *testing.T) {
	for _, withLock := range []bool{true, false} {
		t.Run(map[bool]string{true: "with_lock", false: "without_lock"}[withLock], func(t *testing.T) {
			root, _, call := skillTestServer(t)
			dir := personacard.Dir(root)
			if err := fsutil.WriteAtomic(filepath.Join(dir, "0123456789abcdef", "card.md"), []byte("name: 旧卡\n\n旧正文。\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if withLock {
				if err := os.WriteFile(filepath.Join(dir, ".lock"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if code, raw := call("GET", "/api/persona-cards", "", true); code != 200 || strings.Count(raw, `"id":`) != 2 {
				t.Fatalf("list %d %s", code, raw)
			}
			if code, raw := call("DELETE", "/api/persona-cards/0123456789abcdef", "", true); code/100 != 2 {
				t.Fatalf("delete %d %s", code, raw)
			}
			for i := 0; i < 2; i++ {
				if code, raw := call("GET", "/api/persona-cards", "", true); code != 200 || strings.Count(raw, `"id":`) != 1 || !strings.Contains(raw, `"default":true`) {
					t.Fatalf("list after delete %d %s", code, raw)
				}
			}
		})
	}
}

// Listing a marked old library upgrades persisted bodies, not just the preview.
func TestPersonaCardListUpgradesThinSeeds(t *testing.T) {
	root, _, call := skillTestServer(t)
	old := []struct{ name, body string }{
		{"碎嘴群友", `说话像群里那个爱接梗的：短句、口语，别端着。

能玩梗就玩，但别硬凹；接不住就老实说「这个我接不上」。

正经事也能办，先给结论再补一句人话。

别碰：别替主人做决定；别编没发生过的群聊八卦；别用公文腔。`},
		{"值班运维", `你是值班那班：先报状态，再给下一步。

出事先止血，再说道理。日志和命令用短句甩出来。

别吓主人，也别报喜不报忧。`},
		{"周末搭子", `周末搭子气质：轻松、会起哄，约局和闲聊都行。

别太卷，别一开口就任务清单。能一起摸鱼也能一起办正事。`},
	}
	var ids []string
	for _, s := range old {
		c, err := personacard.Create(root, s.name, s.body)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	if code, raw := call("GET", "/api/persona-cards", "", false); code != 401 {
		t.Fatalf("unauthenticated list: %d %s", code, raw)
	}
	for i, id := range ids {
		if c, err := personacard.Get(root, id); err != nil || c.Body != old[i].body {
			t.Fatalf("unauthenticated request changed card %d: %v", i, err)
		}
	}
	if code, raw := call("GET", "/api/persona-cards", "", true); code != 200 {
		t.Fatalf("list: %d %s", code, raw)
	}
	for i, id := range ids {
		code, raw := call("GET", "/api/persona-cards/"+id, "", true)
		var c struct{ ID, Name, Body string }
		if err := json.Unmarshal([]byte(raw), &c); err != nil || code != 200 || c.ID != id || c.Name != old[i].name || c.Body != personacard.Seeds[i+1].Body {
			t.Fatalf("detail %d did not return upgraded body: status %d, decode %v", i, code, err)
		}
		if disk, err := personacard.Get(root, id); err != nil || disk.Body != c.Body {
			t.Fatalf("upgrade %d not persisted: %v", i, err)
		}
	}
}
