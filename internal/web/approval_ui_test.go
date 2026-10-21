package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
)

// TestApprovalCardCountdownFollowsServer pins the web card to the server's
// wait. The page used to count down APPROVAL_WAIT_SECS = 60 and paint itself
// expired while the server was still accepting a decision.
func TestApprovalCardCountdownFollowsServer(t *testing.T) {
	js := servedAppJS(t, "")
	if bytes.Contains(js, []byte("APPROVAL_WAIT_SECS = 60")) || bytes.Contains(js, []byte("Math.floor(left / 60)")) {
		t.Fatal("approval card still counts down 60 seconds as minutes:seconds")
	}
	if !bytes.Contains(js, []byte("opt.deadline")) || !bytes.Contains(js, []byte("payload.deadline")) || !bytes.Contains(js, []byte("card.deadline")) {
		t.Fatal("approval card does not read the server deadline")
	}
	remain := funcBody(t, js, "function approvalRemainText")
	if !bytes.Contains(remain, []byte("小时")) {
		t.Fatal("long remaining time is not shown in hours")
	}

	const wait = 2 * time.Hour
	_, ui, cookie, _ := newApprovalHarness(t, wait)
	gotJS := servedAppJS(t, ui.URL)
	if !bytes.Equal(gotJS, js) {
		t.Fatal("served /app.js is not the embedded approval card")
	}
	stream := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	ev := waitEvent(t, stream, "approval")
	var card struct {
		ID       string `json:"id"`
		Deadline string `json:"deadline"`
	}
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	if card.ID == "" || card.Deadline == "" {
		t.Fatalf("approval payload missing deadline: %s", ev.data)
	}
	deadline, err := time.Parse(time.RFC3339, card.Deadline)
	if err != nil {
		t.Fatalf("deadline %q: %v", card.Deadline, err)
	}
	skew := time.Until(deadline) - wait
	if skew < -15*time.Second || skew > 2*time.Second {
		t.Fatalf("deadline %s is %s from the %s server wait", card.Deadline, skew, wait)
	}
	if code := decideCode(t, ui.URL, cookie, card.ID, false); code != http.StatusOK {
		t.Fatalf("decision %d", code)
	}
	raw := getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie)
	var payload struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	var pending string
	for _, m := range payload.Messages {
		parsed, ok := bot.ParseApprovalCard(m.Content)
		if ok && parsed.ID == card.ID && parsed.State == bot.ApprovalPending {
			pending = m.Content
		}
	}
	if pending == "" || !strings.Contains(pending, `deadline="`+card.Deadline+`"`) {
		t.Fatalf("persisted card does not carry the server deadline:\n%s", pending)
	}
}

func servedAppJS(t *testing.T, base string) []byte {
	t.Helper()
	if base == "" {
		js, err := staticFS.ReadFile("static/app.js")
		if err != nil {
			t.Fatal(err)
		}
		return js
	}
	res, err := http.Get(base + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /app.js %d %s", res.StatusCode, body)
	}
	return body
}

func funcBody(t *testing.T, js []byte, sig string) []byte {
	t.Helper()
	i := bytes.Index(js, []byte(sig))
	if i < 0 {
		t.Fatalf("missing %s", sig)
	}
	rest := js[i:]
	j := bytes.Index(rest, []byte("\n  function "))
	if j < 0 {
		j = len(rest)
	}
	return rest[:j]
}
