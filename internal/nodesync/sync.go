// Package nodesync exchanges person identity and person memory with nodes
// registered by `xbot node add`. It uses the node call client; it does not
// change the node transport.
package nodesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/person"
)

const (
	// Cap is the node capability that exchanges one sync bundle.
	Cap = "sync"
	// DefaultEvery is how often a running server contacts registered nodes.
	DefaultEvery = 30 * time.Second
	// DefaultDebounce is how long a local write waits before a sync.
	DefaultDebounce = 300 * time.Millisecond
)

// callLimit is the node client's body cap (1 MiB), minus the trailing newline
// the server encoder writes.
const callLimit = 1<<20 - 1

func init() {
	person.OnChange(kick)
	memory.OnChange(kick)
}

// Bundle is the identity log plus the person-memory log.
type Bundle struct {
	People []person.LogEntry `json:"people"`
	Memory []memory.SyncLine `json:"memory"`
}

type runner struct {
	wake chan struct{}
	done chan struct{}
	mu   sync.Mutex
}

// Wait blocks until the sync loop for root has stopped.
// A root that was never started returns immediately.
func Wait(root string) {
	regMu.Lock()
	r := runners[root]
	var done <-chan struct{}
	if r != nil {
		done = r.done
	}
	regMu.Unlock()
	if done == nil {
		return
	}
	<-done
}

var (
	regMu   sync.Mutex
	runners = map[string]*runner{}
)

// Start contacts registered nodes on a period, and soon after a local
// identity or person-memory write. A second Start for the same root is ignored.
// Cancel ctx to stop.
func Start(ctx context.Context, root string, every, debounce time.Duration) {
	if every <= 0 {
		every = DefaultEvery
	}
	if debounce <= 0 {
		debounce = DefaultDebounce
	}
	r := bind(root)
	if r == nil {
		return
	}
	go func() {
		defer func() {
			unbind(root)
			close(r.done)
		}()
		timer := time.NewTimer(every)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			case <-timer.C:
				r.mu.Lock()
				err := once(ctx, root)
				r.mu.Unlock()
				if err != nil && ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "nodesync: %v\n", err)
				}
				timer.Reset(every)
			}
		}
	}()
}

// Once exchanges bundles with every registered node.
// A node that is down or rejects the token is reported, and the others still sync.
func Once(ctx context.Context, root string) error {
	regMu.Lock()
	r := runners[root]
	regMu.Unlock()
	if r == nil {
		return once(ctx, root)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return once(ctx, root)
}

// Accept handles one inbound sync call. command is the peer's bundle.
// The returned text is this root's bundle from before the merge.
func Accept(root, command string) (string, error) {
	local, err := export(root)
	if err != nil {
		return "", err
	}
	raw, err := marshal(local)
	if err != nil {
		return "", err
	}
	if err := fits(raw); err != nil {
		return "", err
	}
	if command != "" {
		if err := fits([]byte(command)); err != nil {
			return "", err
		}
		var remote Bundle
		if err := json.Unmarshal([]byte(command), &remote); err != nil {
			return "", err
		}
		if err := merge(root, "", remote); err != nil {
			return "", err
		}
	}
	return string(raw), nil
}

func once(ctx context.Context, root string) error {
	nodes, err := node.List(root)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	local, err := export(root)
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range nodes {
		raw, err := marshal(local)
		if err != nil {
			return err
		}
		if err := fits(raw); err != nil {
			return err
		}
		peer := ""
		if info, err := node.Ping(ctx, n.URL, n.Token); err == nil {
			peer = info.ID
			if err := node.RememberDevice(root, n.Name, info.ID); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", n.Name, err))
			}
		}
		text, err := node.Call(ctx, n.URL, n.Token, Cap, string(raw), false)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Name, err))
			continue
		}
		var remote Bundle
		if err := json.Unmarshal([]byte(text), &remote); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Name, err))
			continue
		}
		if err := merge(root, peer, remote); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Name, err))
			continue
		}
		local, err = export(root)
		if err != nil {
			return err
		}
	}
	return errors.Join(errs...)
}

func export(root string) (Bundle, error) {
	people, err := person.ExportLog(root)
	if err != nil {
		return Bundle{}, err
	}
	mem, err := memory.ExportSync(root)
	if err != nil {
		return Bundle{}, err
	}
	if people == nil {
		people = []person.LogEntry{}
	}
	if mem == nil {
		mem = []memory.SyncLine{}
	}
	return Bundle{People: people, Memory: mem}, nil
}

func merge(root, peer string, b Bundle) error {
	if err := person.MergeLog(root, b.People); err != nil {
		return err
	}
	return memory.MergeFrom(root, peer, b.Memory)
}

func marshal(b Bundle) ([]byte, error) {
	return json.Marshal(b)
}

func fits(raw []byte) error {
	wrapped, err := json.Marshal(node.Request{Cap: Cap, Command: string(raw)})
	if err != nil {
		return err
	}
	reply, err := json.Marshal(map[string]string{"text": string(raw)})
	if err != nil {
		return err
	}
	if len(wrapped) > callLimit || len(reply) > callLimit {
		return fmt.Errorf("nodesync: bundle is %d bytes and does not fit in one node call (1 MiB limit)", len(raw))
	}
	return nil
}

func bind(root string) *runner {
	regMu.Lock()
	defer regMu.Unlock()
	if _, ok := runners[root]; ok {
		return nil
	}
	r := &runner{wake: make(chan struct{}, 1), done: make(chan struct{})}
	runners[root] = r
	return r
}

func unbind(root string) {
	regMu.Lock()
	delete(runners, root)
	regMu.Unlock()
}

func kick(root string) {
	regMu.Lock()
	r := runners[root]
	regMu.Unlock()
	if r == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
