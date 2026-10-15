// Package inbox is a bot's inbound queue: an in-process list plus an append-only log.
// Post records an event. Ack records that the kernel finished an attempt.
// A process crash before Ack leaves the event pending, so the next open loads it again.
package inbox

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/link"
)

// Inbox is safe for one consumer and concurrent producers.
type Inbox struct {
	mu      sync.Mutex
	path    string
	pending []link.Event
	seen    map[string]bool
	notify  chan struct{}
}

type record struct {
	Op    string      `json:"op"`
	Event *link.Event `json:"event,omitempty"`
	ID    string      `json:"id,omitempty"`
}

// Open loads pending events from the append-only log.
func Open(path string) (*Inbox, error) {
	n := &Inbox{path: path, notify: make(chan struct{}, 1), seen: map[string]bool{}}
	if err := n.Reload(); err != nil {
		return nil, err
	}
	return n, nil
}

// Reload folds the log again so a post from another process becomes visible.
func (n *Inbox) Reload() error {
	return fsutil.WithLock(n.path+".lock", func() error {
		n.mu.Lock()
		defer n.mu.Unlock()
		return n.rebuild()
	})
}

func (n *Inbox) rebuild() error {
	var order []string
	posted := map[string]link.Event{}
	acked := map[string]bool{}
	err := fsutil.ReadJSONL(n.path, func(line []byte) error {
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		switch rec.Op {
		case "post":
			if rec.Event == nil || rec.Event.ID == "" {
				return errors.New("inbox: post without event")
			}
			ev := *rec.Event
			if _, seen := posted[ev.ID]; !seen {
				posted[ev.ID] = ev
				order = append(order, ev.ID)
			}
		case "ack":
			if rec.ID != "" {
				acked[rec.ID] = true
			}
		default:
			return errors.New("inbox: unknown op " + rec.Op)
		}
		return nil
	})
	if err != nil {
		return err
	}
	n.seen = map[string]bool{}
	n.pending = nil
	for _, id := range order {
		n.seen[id] = true
		if !acked[id] {
			n.pending = append(n.pending, posted[id])
		}
	}
	return nil
}

// Post persists the event and queues it. A repeated id, including one already acked, is ignored.
// The Untrusted bit is stored as given. The kernel decides whether the text is trusted.
func (n *Inbox) Post(ev link.Event) error {
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if err := ev.Validate(); err != nil {
		return err
	}
	return fsutil.WithLock(n.path+".lock", func() error {
		n.mu.Lock()
		defer n.mu.Unlock()
		if err := n.rebuild(); err != nil {
			return err
		}
		if n.seen[ev.ID] {
			return nil
		}
		if err := fsutil.AppendJSONL(n.path, record{Op: "post", Event: &ev}); err != nil {
			return err
		}
		n.seen[ev.ID] = true
		n.pending = append(n.pending, ev)
		n.signal()
		return nil
	})
}

// Pending returns a copy of the queue, oldest first.
func (n *Inbox) Pending() []link.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]link.Event, len(n.pending))
	copy(out, n.pending)
	return out
}

// Head returns the oldest pending event without removing it.
func (n *Inbox) Head() (link.Event, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.pending) == 0 {
		return link.Event{}, false
	}
	return n.pending[0], true
}

// Ack records completion and drops the event from the in-memory queue.
// Ack of an unknown or already-acked id succeeds.
func (n *Inbox) Ack(id string) error {
	if id == "" {
		return errors.New("inbox: ack requires an id")
	}
	return fsutil.WithLock(n.path+".lock", func() error {
		if err := fsutil.AppendJSONL(n.path, record{Op: "ack", ID: id}); err != nil {
			return err
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		filtered := n.pending[:0]
		for _, ev := range n.pending {
			if ev.ID != id {
				filtered = append(filtered, ev)
			}
		}
		n.pending = filtered
		return nil
	})
}

// Wait receives when Post queues an event. The channel has buffer 1.
func (n *Inbox) Wait() <-chan struct{} { return n.notify }

func (n *Inbox) signal() {
	select {
	case n.notify <- struct{}{}:
	default:
	}
}
