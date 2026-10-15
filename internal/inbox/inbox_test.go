package inbox

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xbot/internal/link"
)

func TestPostAckReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	in, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := link.NewEvent(link.NameTerminal, link.TypeMessage, "one", "u1", time.Now())
	b := link.NewEvent(link.NameTerminal, link.TypeMessage, "two", "u1", time.Now())
	a.Untrusted = false
	if err := in.Post(a); err != nil {
		t.Fatal(err)
	}
	if err := in.Post(a); err != nil {
		t.Fatal(err)
	}
	if err := in.Post(b); err != nil {
		t.Fatal(err)
	}
	ev, ok := in.Head()
	if !ok || ev.ID != a.ID || !ev.Untrusted {
		t.Fatalf("head %+v ok=%v", ev, ok)
	}
	if err := in.Ack(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := in.Ack(a.ID); err != nil {
		t.Fatal(err)
	}
	ev, ok = in.Head()
	if !ok || ev.ID != b.ID {
		t.Fatalf("after ack %+v", ev)
	}
	re, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ev, ok = re.Head()
	if !ok || ev.Content != "two" {
		t.Fatalf("reload %+v ok=%v", ev, ok)
	}
	if err := re.Ack(b.ID); err != nil {
		t.Fatal(err)
	}
	re2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := re2.Head(); ok {
		t.Fatal("expected empty inbox")
	}
}

func TestPostRejectsIncomplete(t *testing.T) {
	in, err := Open(filepath.Join(t.TempDir(), "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Post(link.Event{ID: "x", Untrusted: true}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestPostWakes(t *testing.T) {
	in, err := Open(filepath.Join(t.TempDir(), "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = in.Post(link.NewEvent(link.NameTimer, link.TypeTimer, "tick", "u1", time.Now()))
	}()
	select {
	case <-in.Wait():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for post")
	}
	if _, ok := in.Head(); !ok {
		t.Fatal("missing event")
	}
}

func TestConcurrentPost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	in, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev := link.NewEvent(link.NameTerminal, link.TypeMessage, "x", "u1", time.Now())
			if err := in.Post(ev); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	re, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		ev, ok := re.Head()
		if !ok {
			break
		}
		if err := re.Ack(ev.ID); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 20 {
		t.Fatalf("got %d events", n)
	}
}
