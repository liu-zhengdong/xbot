package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/mockopenai"
	"xbot/internal/provider"
)

// newApprovalHarness builds a web server whose bots need the owner before every
// shell command, so one mock reply raises exactly one card.
func newApprovalHarness(t *testing.T, wait time.Duration) (*Server, *httptest.Server, *http.Cookie, *bot.Bot) {
	t.Helper()
	root := t.TempDir()
	work := t.TempDir()
	model := httptest.NewServer(mockopenai.NewMux(work))
	t.Cleanup(model.Close)
	client := &provider.Client{BaseURL: model.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: model.Client()}
	s := &Server{Root: root, Owner: "u1", ApprovalWait: wait}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{
			Bot: b, Provider: client, MaxSteps: 4, Budget: compact.DefaultBudget(),
			Links:   []link.Link{link.Web{}, link.NewToolsLinkAt(work, "")},
			WorkDir: work,
			Machine: func() (string, error) { return "ask", nil },
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	t.Cleanup(ui.Close)
	return s, ui, loginCookie(t, ui.URL, tok), b
}

// approvalCardID reads the id out of one approval stream event.
func approvalCardID(t *testing.T, ev sseEvent) string {
	t.Helper()
	var card struct {
		ID   string `json:"id"`
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	if card.ID == "" {
		t.Fatalf("empty card id in %s", ev.data)
	}
	return card.ID
}

func TestApprovalWaitDefaultsTo30Minutes(t *testing.T) {
	if got := (&Server{}).approvalWait(); got != 30*time.Minute {
		t.Fatalf("default wait %v", got)
	}
	if got := (&Server{ApprovalWait: 5 * time.Second}).approvalWait(); got != 5*time.Second {
		t.Fatalf("explicit wait %v", got)
	}
}

func TestWebApprovalTimeoutAutomaticallyResumes(t *testing.T) {
	_, ui, cookie, b := newApprovalHarness(t, 100*time.Millisecond)
	stream := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	id := approvalCardID(t, waitEvent(t, stream, "approval"))
	ran := false
	for ev := range stream {
		if ev.name == "result" && strings.Contains(ev.data, "exit 0") {
			ran = true
		}
		if ev.name == "error" {
			t.Fatal(ev.data)
		}
	}
	if !ran {
		t.Fatal("timeout did not execute original tool")
	}
	if got := cardStates(t, b, id); !reflect.DeepEqual(got, []string{bot.ApprovalPending, bot.ApprovalAutoApproved}) {
		t.Fatalf("states %v", got)
	}
	decisions, err := b.Decisions()
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].State != bot.DecisionAllowed {
		t.Fatalf("decisions %+v", decisions)
	}
	entries, _ := b.Entries()
	found := false
	for _, e := range entries {
		if c, ok := bot.ParseApprovalCard(e.Message.Text()); ok && c.ID == id && strings.Contains(c.Notice, "没人确认，已按允许执行") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing automatic-allow notice")
	}
}

// A disconnected stream must not discard its owner's decision.
func TestWebApprovalSurvivesStreamDisconnect(t *testing.T) {
	s := &Server{Root: t.TempDir(), ApprovalWait: 30 * time.Second}
	b, err := bot.Create(s.Root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The card id arrives from the approve goroutine, so it crosses a channel
	// rather than a shared variable the test would have to poll.
	cardID := make(chan string, 1)
	send := func(name string, payload any) {
		if name != "approval" {
			return
		}
		m, _ := payload.(map[string]any)
		got, _ := m["id"].(string)
		select {
		case cardID <- got:
		default:
		}
	}
	approve := s.webApprove(b, nil, send)
	type outcome struct {
		allowed bool
		err     error
	}
	answered := make(chan outcome, 1)
	go func() {
		allowed, err := approve(ctx, "shell", "mkdir a")
		answered <- outcome{allowed, err}
	}()
	var id string
	select {
	case id = <-cardID:
	case <-time.After(10 * time.Second):
		t.Fatal("no approval card was raised")
	}
	// The stream that showed the card is gone; the owner still answers, and the
	// decision must reach the wake that raised the card.
	s.mu.Lock()
	card := s.approvals["ada/"+id]
	s.mu.Unlock()
	if card == nil {
		t.Fatal("card not registered for a late decision")
	}
	card.done <- approvalDecision{allow: true}
	select {
	case out := <-answered:
		if out.err != nil {
			t.Fatalf("approver returned %v", out.err)
		}
		if !out.allowed {
			t.Fatal("late allow did not reach the wake")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approver did not return after a decision")
	}
}

// TestWebApprovalDecideStatuses pins the status codes: 200 for a decision, 409
// once decided, 410 once the window passed, 404 for an unknown card.
func TestWebApprovalDecideStatuses(t *testing.T) {
	_, ui, cookie, _ := newApprovalHarness(t, 30*time.Second)
	stream := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	id := approvalCardID(t, waitEvent(t, stream, "approval"))
	if code := decideCode(t, ui.URL, cookie, id, true); code != http.StatusOK {
		t.Fatalf("first decision %d, want 200", code)
	}
	if code := decideCode(t, ui.URL, cookie, id, true); code != http.StatusConflict {
		t.Fatalf("second decision %d, want 409", code)
	}
	if code := decideCode(t, ui.URL, cookie, "nosuchcard", true); code != http.StatusNotFound {
		t.Fatalf("unknown card %d, want 404", code)
	}
}

// TestWebApprovalExpiredCardIsGone checks a decision arriving after the window
// is refused with 410 instead of silently waking a finished wake.
func TestWebApprovalExpiredCardIsGone(t *testing.T) {
	_, ui, cookie, _ := newApprovalHarness(t, 200*time.Millisecond)
	stream := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	id := approvalCardID(t, waitEvent(t, stream, "approval"))
	for range stream {
	}
	if code := decideCode(t, ui.URL, cookie, id, true); code != http.StatusGone {
		t.Fatalf("late decision %d, want 410", code)
	}
}

// TestResolveOrphanApprovalsRecordsRestart checks the startup sweep: a card a
// previous process left pending becomes no-decision with the restart reason,
// while a card already decided is untouched.
func TestResolveOrphanApprovalsRecordsRestart(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	orphan := bot.ApprovalCardText("orphan", "shell", "运行命令", "在本机上执行", "mkdir a", bot.ApprovalPending)
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(orphan)}}); err != nil {
		t.Fatal(err)
	}
	answered := []string{
		bot.ApprovalCardText("answered", "shell", "运行命令", "在本机上执行", "mkdir b", bot.ApprovalPending),
		bot.ApprovalCardText("answered", "shell", "运行命令", "在本机上执行", "mkdir b", bot.ApprovalApproved),
	}
	for _, text := range answered {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := ResolveOrphanApprovals(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sweep closed %d cards, want 1", n)
	}
	if got := latestState(t, b, "orphan"); got != bot.NoDecisionText(bot.ApprovalReasonRestart) {
		t.Fatalf("orphan state %q", got)
	}
	if _, ok := bot.NoDecisionReason(latestState(t, b, "orphan")); !ok {
		t.Fatalf("orphan state is not a no-decision state")
	}
	if got := latestState(t, b, "answered"); got != bot.ApprovalApproved {
		t.Fatalf("answered card state %q, want approved", got)
	}
}

// latestState returns the newest persisted state for one card id.
func latestState(t *testing.T, b *bot.Bot, id string) string {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	state := ""
	for _, e := range entries {
		if card, ok := bot.ParseApprovalCard(e.Message.Text()); ok && card.ID == id {
			state = card.State
		}
	}
	return state
}

// decideCode posts one decision to a live server and returns the status code.
func decideCode(t *testing.T, base string, cookie *http.Cookie, id string, allow bool) int {
	t.Helper()
	body := `{"allow":false}`
	if allow {
		body = `{"allow":true}`
	}
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
	if res.StatusCode >= 500 {
		t.Fatalf("decision failed with %d", res.StatusCode)
	}
	return res.StatusCode
}

// TestWebApprovalForegroundToBackgroundKeepsCard checks a wake whose context is
// cancelled, as a foreground-to-background handoff does, keeps its open card:
// the card must not be written off as expired and must still take an answer.
func TestWebApprovalForegroundToBackgroundKeepsCard(t *testing.T) {
	s := &Server{Root: t.TempDir(), ApprovalWait: 30 * time.Second}
	b, err := bot.Create(s.Root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The card id arrives from the approve goroutine, so it crosses a channel
	// rather than a shared variable the test would have to poll.
	cardID := make(chan string, 1)
	send := func(name string, payload any) {
		if name != "approval" {
			return
		}
		m, _ := payload.(map[string]any)
		got, _ := m["id"].(string)
		select {
		case cardID <- got:
		default:
		}
	}
	approve := s.webApprove(b, nil, send)
	type outcome struct {
		allowed bool
		err     error
	}
	answered := make(chan outcome, 1)
	go func() {
		allowed, err := approve(ctx, "shell", "mkdir a")
		answered <- outcome{allowed, err}
	}()
	var id string
	select {
	case id = <-cardID:
	case <-time.After(10 * time.Second):
		t.Fatal("no approval card was raised")
	}
	// Hand the turn to the background.
	cancel()
	time.Sleep(50 * time.Millisecond)
	if states := cardStates(t, b, id); len(states) != 1 || states[0] != bot.ApprovalPending {
		t.Fatalf("card states after the handoff %v, want one pending card", states)
	}
	// The owner answers after the handoff, in this process.
	s.mu.Lock()
	card := s.approvals["ada/"+id]
	s.mu.Unlock()
	if card == nil {
		t.Fatal("card was dropped during the handoff")
	}
	card.done <- approvalDecision{allow: true}
	select {
	case out := <-answered:
		if out.err != nil {
			t.Fatalf("approver returned %v after a decision", out.err)
		}
		if !out.allowed {
			t.Fatal("allow after the handoff was not honoured")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approver did not return after a decision")
	}
	if states := cardStates(t, b, id); strings.Join(states, ",") != bot.ApprovalPending+","+bot.ApprovalApproved {
		t.Fatalf("card states %v, want pending then approved", states)
	}
}

// The background handoff must retain its deadline and default to allow.
func TestWebApprovalBackgroundHandoffExpires(t *testing.T) {
	s := &Server{Root: t.TempDir(), ApprovalWait: 50 * time.Millisecond}
	b, err := bot.Create(s.Root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The card id arrives from the approve goroutine, so it crosses a channel
	// rather than a shared variable the test would have to poll.
	cardID := make(chan string, 1)
	send := func(name string, payload any) {
		if name != "approval" {
			return
		}
		m, _ := payload.(map[string]any)
		got, _ := m["id"].(string)
		select {
		case cardID <- got:
		default:
		}
	}
	approve := s.webApprove(b, nil, send)
	type outcome struct {
		allowed bool
		err     error
	}
	answered := make(chan outcome, 1)
	go func() {
		allowed, err := approve(ctx, "shell", "mkdir a")
		answered <- outcome{allowed, err}
	}()
	var id string
	select {
	case id = <-cardID:
	case <-time.After(10 * time.Second):
		t.Fatal("no approval card was raised")
	}
	// Hand the turn to the background and never answer.
	cancel()
	select {
	case out := <-answered:
		if out.err != nil {
			t.Fatalf("approver returned %v, want nil", out.err)
		}
		if !out.allowed {
			t.Fatal("the default allow was lost")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approver never returned after the handoff; the card waited forever")
	}
	states := cardStates(t, b, id)
	if len(states) != 2 || states[0] != bot.ApprovalPending || states[1] != bot.ApprovalAutoApproved {
		t.Fatalf("card states %v, want pending then %s", states, bot.ApprovalAutoApproved)
	}
}

// cardStates reads the persisted states of one card, oldest first.
func cardStates(t *testing.T, b *bot.Bot, id string) []string {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if card, ok := bot.ParseApprovalCard(e.Message.Text()); ok && card.ID == id {
			out = append(out, card.State)
		}
	}
	return out
}
