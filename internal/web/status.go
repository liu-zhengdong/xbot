package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/node"
	"xbot/internal/peer"
)

const (
	nodeStatusMaxBytes = 16 << 10
	statusStepsMax     = 20
)

// Vars so a test can shorten the deadline and the refresh window.
var (
	nodeStatusTimeout = 2 * time.Second
	nodeStatusRefresh = 10 * time.Second
)

// nodeStatusCache holds the last status list from one node. A node that fails
// after a good fetch keeps serving its cached list with State offline and
// StaleSeconds set, so /api/bots/status never waits on a dead node after the
// first load.
type nodeStatusCache struct {
	mu       sync.Mutex
	bots     []kernel.BotStatus
	updated  time.Time
	failedAt time.Time
	loaded   bool
	// inflight guards one refresh per node at a time.
	inflight bool
}

// statusSource is the kernel.StatusSource the web server attaches to its
// kernels: every same-owner bot locally, merged with remote nodes.
type statusSource struct{ s *Server }

// botStatuses lists all local bots and merges remote ones. A bot with a live
// kernel here is derived; every other local bot is idle from its transcript
// tail. A local bot another process holds is reported running.
func (s *Server) botStatuses() []kernel.BotStatus {
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	kernels := make(map[string]*kernel.Kernel, len(s.kernels))
	for name, k := range s.kernels {
		kernels[name] = k
	}
	holds := make(map[string]bool, len(s.holds))
	for name := range s.holds {
		holds[name] = true
	}
	s.mu.Unlock()
	now := time.Now()
	out := make([]kernel.BotStatus, 0, len(cfgs))
	for _, cfg := range cfgs {
		if k := kernels[cfg.Name]; k != nil {
			out = append(out, k.Status())
			continue
		}
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil {
			continue
		}
		st := kernel.IdleStatus(b, now)
		if holds[cfg.Name] {
			// Another process holds this bot. It may be running elsewhere.
			st.State = kernel.StateRunning
		}
		out = append(out, st)
	}
	out = append(out, s.remoteStatuses()...)
	sort.Slice(out, func(i, j int) bool { return out[i].Bot < out[j].Bot })
	return out
}

// remoteStatuses returns every registered node's cached bots. The first fetch
// for a node is synchronous under a 2s deadline; after that refreshes are
// asynchronous so a slow or dead node cannot delay a status read.
func (s *Server) remoteStatuses() []kernel.BotStatus {
	nodes, err := node.List(s.Root)
	if err != nil {
		return nil
	}
	var out []kernel.BotStatus
	// At most one never-fetched node blocks per call, under the short
	// deadline. Every other node is fetched in the background, so a poll
	// never pays more than one deadline even when several nodes are down.
	firstPending := true
	for _, n := range nodes {
		c := s.nodeStatus(n)
		bots, ok := c.snapshot()
		// The first attempt for a node blocks once under the short deadline.
		// Every later read is served from cache and refreshed in the
		// background at most once per window, so even a node that is down
		// forever never adds a wait to a poll after the first one.
		if !c.attempted() && firstPending {
			firstPending = false
			bots, ok = c.refresh(n)
		} else {
			c.refreshAsync(n)
		}
		if !ok {
			continue
		}
		last := c.lastUpdate()
		stale := c.stale()
		for i := range bots {
			bots[i].Node = n.Name
			if stale {
				bots[i].State = kernel.StateOffline
				bots[i].StaleSeconds = int(time.Since(last).Seconds())
			}
		}
		out = append(out, bots...)
	}
	return out
}

// nodeStatus returns the cache for one node, creating it on first use.
func (s *Server) nodeStatus(n node.Node) *nodeStatusCache {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodeStatuses == nil {
		s.nodeStatuses = map[string]*nodeStatusCache{}
	}
	if c := s.nodeStatuses[n.Name]; c != nil {
		return c
	}
	c := &nodeStatusCache{}
	s.nodeStatuses[n.Name] = c
	return c
}

// attempted reports whether any fetch has finished for this node, success or
// failure, so the synchronous first call happens at most once.
func (c *nodeStatusCache) attempted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loaded || !c.failedAt.IsZero()
}

// stale reports that a loaded cache's most recent fetch failed.
func (c *nodeStatusCache) stale() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loaded && c.failedAt.After(c.updated)
}

func (c *nodeStatusCache) lastUpdate() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.updated
}

// snapshot copies the cached bots. ok is true once a good fetch has landed.
func (c *nodeStatusCache) snapshot() ([]kernel.BotStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]kernel.BotStatus(nil), c.bots...)
	return out, c.loaded
}

// refresh fetches one node synchronously and caches the result. ok is true when
// there is a cached list to show.
func (c *nodeStatusCache) refresh(n node.Node) ([]kernel.BotStatus, bool) {
	c.mu.Lock()
	if c.inflight {
		out := append([]kernel.BotStatus(nil), c.bots...)
		ok := c.loaded
		c.mu.Unlock()
		return out, ok
	}
	c.inflight = true
	c.mu.Unlock()
	bots := fetchNodeStatus(n)
	return c.store(bots)
}

// refreshAsync refreshes a loaded node in the background at most once per
// refresh window, so a poll never blocks on it.
func (c *nodeStatusCache) refreshAsync(n node.Node) {
	c.mu.Lock()
	last := c.updated
	if last.IsZero() {
		last = c.failedAt
	}
	if c.inflight || time.Since(last) < nodeStatusRefresh {
		c.mu.Unlock()
		return
	}
	c.inflight = true
	c.mu.Unlock()
	go func() {
		bots := fetchNodeStatus(n)
		_, _ = c.store(bots)
	}()
}

// store records one fetch result. A nil bots means the call failed.
func (c *nodeStatusCache) store(bots []kernel.BotStatus) ([]kernel.BotStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight = false
	if bots != nil {
		c.bots = bots
		c.updated = time.Now()
		c.loaded = true
	} else {
		c.failedAt = time.Now()
	}
	out := append([]kernel.BotStatus(nil), c.bots...)
	return out, c.loaded
}

// fetchNodeStatus runs the node status call under a short deadline and returns
// the decoded list, or nil on any error.
func fetchNodeStatus(n node.Node) []kernel.BotStatus {
	ctx, cancel := context.WithTimeout(context.Background(), nodeStatusTimeout)
	defer cancel()
	text, err := node.Call(ctx, n.URL, n.Token, "status", "", false)
	if err != nil || len(text) > nodeStatusMaxBytes {
		return nil
	}
	var bots []kernel.BotStatus
	if err := json.Unmarshal([]byte(text), &bots); err != nil {
		return nil
	}
	return bots
}

// BotStatuses implements kernel.StatusSource.
func (src statusSource) BotStatuses() []kernel.BotStatus { return src.s.botStatuses() }

// BotSteps implements kernel.StatusSource.
func (src statusSource) BotSteps(name string, n int) []kernel.Step {
	return src.s.botSteps(name, n)
}

// botSteps returns up to n steps of a bot's current turn. Remote nodes expose
// status only, never steps.
func (s *Server) botSteps(name string, n int) []kernel.Step {
	if n <= 0 || n > statusStepsMax {
		n = statusStepsMax
	}
	s.mu.Lock()
	k := s.kernels[name]
	s.mu.Unlock()
	if k != nil {
		return k.Steps(n)
	}
	return nil
}

// registerStatusAPI mounts the two read-only status routes.
func (s *Server) registerStatusAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bots/status", s.botStatusAPI)
	mux.HandleFunc("GET /api/bots/{name}/steps", s.botStepsAPI)
}

// botStatusAPI returns {bots:[BotStatus]} for every same-owner bot.
func (s *Server) botStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	bots := s.botStatuses()
	if bots == nil {
		bots = []kernel.BotStatus{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": bots})
}

// botStepsAPI returns {steps:[Step]} for one bot's current turn. n is capped.
func (s *Server) botStepsAPI(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	n := statusStepsMax
	if raw := strings.TrimSpace(r.URL.Query().Get("n")); raw != "" {
		got, err := strconv.Atoi(raw)
		if err != nil || got <= 0 {
			writeErr(w, http.StatusBadRequest, "bad n")
			return
		}
		if got < n {
			n = got
		}
	}
	steps := s.botSteps(b.Name(), n)
	if steps == nil {
		steps = []kernel.Step{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": steps})
}
