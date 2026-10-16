package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
)

// forkedRig starts a long turn and interrupts it, so one background is running.
func forkedRig(t *testing.T) (*Server, *bot.Bot, func() *kernel.Kernel, *slowTool, string, func(string) []byte, func(string, string) httpResult, func()) {
	t.Helper()
	holdPoll = 20 * time.Millisecond
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", ForkTurns: true}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	slow := &slowTool{started: make(chan struct{}), release: make(chan struct{})}
	var k *kernel.Kernel
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		k = &kernel.Kernel{Bot: b, Provider: forkModel{}, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 6,
			Links: []link.Link{slowLink{slow}}}
		return k, nil
	}
	ui := httptest.NewServer(muxOf(s))
	cookie := loginCookie(t, ui.URL, tok)
	first := make(chan struct{})
	go func() {
		postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"做个长任务"}`)
		close(first)
	}()
	<-slow.started
	postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：现在几点"}`)
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("no fork")
	}
	get := func(path string) []byte { return getAuth(t, ui.URL+path, cookie) }
	post := func(path, body string) httpResult { return postJSON(t, ui.URL+path, cookie, body) }
	return s, b, func() *kernel.Kernel { return k }, slow, ui.URL, get, post, ui.Close
}

type bgView struct {
	Backgrounds []kernel.BackgroundInfo `json:"backgrounds"`
	Finished    []kernel.BackgroundInfo `json:"finished"`
	Controls    []kernel.Control        `json:"controls"`
	Queue       []kernel.QueueItem      `json:"queue"`
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebBackgroundControlsAndIdleEvents(t *testing.T) {
	_, b, kf, slow, _, get, post, done := forkedRig(t)
	defer done()
	var v bgView
	_ = json.Unmarshal(get("/api/bots/ada/backgrounds"), &v)
	if len(v.Backgrounds) != 1 {
		t.Fatalf("backgrounds %+v", v)
	}
	id := v.Backgrounds[0].ID
	if r := post("/api/bots/ada/agents/"+id+"/steer", `{"text":"顺便写个总结"}`); r.StatusCode != 200 || !strings.Contains(string(r.Body), "pending") {
		t.Fatalf("steer %d %s", r.StatusCode, r.Body)
	}
	if r := post("/api/bots/ada/agents/"+id+"/queue", `{"text":"然后清理"}`); r.StatusCode != 200 {
		t.Fatalf("queue %d %s", r.StatusCode, r.Body)
	}
	_ = json.Unmarshal(get("/api/bots/ada/backgrounds"), &v)
	if len(v.Controls) != 2 || v.Controls[0].State != "pending" || len(v.Queue) != 1 || v.Queue[0].Position != 1 || v.Queue[0].Reason != "queue" {
		t.Fatalf("view %+v", v)
	}
	// A timer while the background holds the lock is handled by the web idle loop.
	k := kf()
	if err := k.Inbox.Post(link.Event{ID: "tm1", Source: link.NameTimer, Type: "timer", Sender: "cron", Content: "插一句：定时提醒", Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "timer handled", func() bool {
		_ = k.Inbox.Reload()
		return len(k.Inbox.Pending()) == 0
	})
	if len(k.Backgrounds()) != 1 {
		t.Fatal("background ended early")
	}
	close(slow.release)
	waitFor(t, "background end", func() bool { return len(k.Backgrounds()) == 0 })
	entries, _ := b.Entries()
	var steer, queue bool
	for _, e := range entries {
		steer = steer || (e.Branch == id && e.Message.Content == "[主人的补充] 顺便写个总结")
		queue = queue || (e.Branch == id && strings.HasPrefix(e.Message.Content, "[排队]"))
	}
	if !steer || !queue {
		t.Fatalf("steer %v queue %v", steer, queue)
	}
}

func TestForgetKernelDropsHoldAndBackgrounds(t *testing.T) {
	s, b, kf, slow, _, _, _, done := forkedRig(t)
	defer done()
	defer close(slow.release)
	k := kf()
	s.forgetKernel("ada")
	l, ok, _ := b.TryLockHandle()
	if !ok {
		t.Fatal("handle lock still held after forgetKernel")
	}
	l.Release()
	s.mu.Lock()
	_, held := s.holds["ada"]
	s.mu.Unlock()
	if held {
		t.Fatal("hold entry kept")
	}
	waitFor(t, "background cancelled", func() bool { return len(k.Backgrounds()) == 0 })
}
