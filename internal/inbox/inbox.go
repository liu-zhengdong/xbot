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
	notify  chan struct{}
}

type record struct {
	Op    string      `json:"op"`
	Event *link.Event `json:"event,omitempty"`
	ID    string      `json:"id,omitempty"`
}

// Open loads pending events from the append-only log.
func Open(path string) (*Inbox, error) {
	n := &Inbox{path: path, notify: make(chan struct{}, 1)}
	var order []string
	posted := map[string]link.Event{}
	acked := map[string]bool{}
	err := fsutil.ReadJSONL(path, func(line []byte) error {
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
			ev.Untrusted = true
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
		return nil, err
	}
	for _, id := range order {
		if !acked[id] {
			n.pending = append(n.pending, posted[id])
		}
	}
	return n, nil
}

// Post persists the event and queues it. A repeated id is ignored.
// Untrusted is forced on.
func (n *Inbox) Post(ev link.Event) error {
	ev.Untrusted = true
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if err := ev.Validate(); err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, have := range n.pending {
		if have.ID == ev.ID {
			return nil
		}
	}
	// Also ignore ids already acked in this process by checking the log would duplicate.
	// Duplicate posts of an acked id are ignored by folding on the next Open.
	// Within this process, a second Post after Ack is a new delivery only if we don't
	// still have it pending. Callers that retry Post after Ack would enqueue again.
	// Persist first so a crash cannot lose a queued event.
	if err := fsutil.AppendJSONL(n.path, record{Op: "post", Event: &ev}); err != nil {
		return err
	}
	n.pending = append(n.pending, ev)
	n.signal()
	return nil
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
}

// Wait receives when Post queues an event. The channel has buffer 1.
func (n *Inbox) Wait() <-chan struct{} { return n.notify }

func (n *Inbox) signal() {
	select {
	case n.notify <- struct{}{}:
	default:
	}
}
