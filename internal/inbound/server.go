package inbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/vault"
)

const (
	// MaxBody is the largest webhook body that is stored.
	MaxBody = 32 << 10
	// MaxBurst is how many deliveries one source keeps before further ones are only counted.
	MaxBurst = 32
	// MaxMerged is the cap on a coalesced webhook body.
	MaxMerged = 32 << 10
	// MaxWatches is the per-bot cap on configured globs.
	MaxWatches = 32
	// MaxWatchFiles is how many paths one glob may match.
	MaxWatchFiles = 200
	// MaxSeen is how many delivery ids are remembered across restarts.
	MaxSeen = 1024
	// DefaultWindow is the tumbling coalesce window for one webhook source.
	DefaultWindow = 2 * time.Second
	// DefaultDebounce is how long a file must stay quiet before a watch event.
	DefaultDebounce = 500 * time.Millisecond

	maxWatchPaths = 20
)

var (
	idRe       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	senderRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@-]{0,63}$`)
	deliveryRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@-]{0,127}$`)
	typeRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)
)

// ValidID reports whether a webhook or watch name is safe in a URL and a filename.
func ValidID(name string) bool { return idRe.MatchString(name) }

// Server accepts webhook posts, coalesces a burst per source, and posts one inbox event per window.
// It does not run the model. The chat or serve loop handles the inbox.
type Server struct {
	Root     string
	Window   time.Duration
	Debounce time.Duration
	Now      func() time.Time
	// OpenVault returns the backend that verifies a connector webhook for one
	// bot. Every vault belongs to a single bot, so the hub opens the backend per
	// request instead of sharing one. Nil falls back to the legacy plaintext
	// secrets.json; a daemon-backed vault refuses the request because it does
	// not expose the secret to this process.
	OpenVault func(*bot.Bot) (vault.Backend, error)

	mu      sync.Mutex
	batches map[string]*batch
}

type batch struct {
	bot     string
	hook    string
	first   time.Time
	eventID string
	items   []item
	dropped int
}

type item struct {
	delivery string
	typ      string
	body     string
	sender   string
}

// Handler is the webhook HTTP handler. Only POST /webhook/<bot>/<name> is accepted.
// Register adds the webhook route to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /webhook/{bot}/{name}", s.handleWebhook)
	mux.HandleFunc("POST /connector/{kind}/{bot}", s.handleConnector)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	botName := r.PathValue("bot")
	name := r.PathValue("name")
	if !validBot(botName) || !ValidID(name) {
		writeStatus(w, http.StatusNotFound, "not_found")
		return
	}
	b, err := bot.Load(s.Root, botName)
	if err != nil {
		if errors.Is(err, bot.ErrNotFound) {
			writeStatus(w, http.StatusNotFound, "not_found")
			return
		}
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	if !granted(b.Config().Webhooks, name) {
		writeStatus(w, http.StatusNotFound, "not_found")
		return
	}
	h, err := LoadHook(b, name)
	if errors.Is(err, os.ErrNotExist) {
		writeStatus(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	if !authHeaderPresent(h, r) {
		writeStatus(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := readBody(r.Body)
	if err != nil {
		if errors.Is(err, errTooBig) {
			writeStatus(w, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		writeStatus(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !verifyHook(h, body, r.Header.Get("X-XBot-Signature"), r.Header.Get("Authorization")) {
		writeStatus(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	delivery, err := deliveryID(r)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "bad_delivery")
		return
	}
	if delivery != "" && s.seenInBatch(botName, name, delivery) {
		writeStatus(w, http.StatusOK, "duplicate")
		return
	}
	dup, err := rememberDelivery(b, name, delivery)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	if dup {
		writeStatus(w, http.StatusOK, "duplicate")
		return
	}
	status := s.enqueue(botName, name, item{
		delivery: delivery,
		typ:      eventType(r.Header.Get("X-XBot-Event")),
		body:     string(body),
		sender:   eventSender(r.Header.Get("X-XBot-Sender"), name),
	})
	if status == "dropped" {
		writeStatus(w, http.StatusTooManyRequests, "dropped")
		return
	}
	writeStatus(w, http.StatusAccepted, "accepted")
}

func authHeaderPresent(h Hook, r *http.Request) bool {
	switch h.Auth {
	case AuthHMAC:
		return strings.TrimSpace(r.Header.Get("X-XBot-Signature")) != ""
	case AuthBearer:
		return strings.TrimSpace(r.Header.Get("Authorization")) != ""
	default:
		return false
	}
}

func validBot(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

func granted(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

var errTooBig = errors.New("webhook: body too large")

func readBody(rc io.ReadCloser) ([]byte, error) {
	if rc == nil {
		return nil, nil
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, MaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBody {
		return nil, errTooBig
	}
	return body, nil
}

func deliveryID(r *http.Request) (string, error) {
	d := strings.TrimSpace(r.Header.Get("X-XBot-Delivery"))
	if d == "" {
		d = strings.TrimSpace(r.Header.Get("X-Delivery-Id"))
	}
	if d == "" {
		return "", nil
	}
	if !deliveryRe.MatchString(d) {
		return "", errors.New("webhook: bad delivery id")
	}
	return d, nil
}

func eventType(raw string) string {
	raw = strings.TrimSpace(raw)
	if typeRe.MatchString(raw) {
		return raw
	}
	return "webhook"
}

func eventSender(raw, hook string) string {
	raw = strings.TrimSpace(raw)
	if senderRe.MatchString(raw) {
		return raw
	}
	return "webhook:" + hook
}

func (s *Server) seenInBatch(botName, hook, delivery string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.batches[batchKey(botName, hook)]
	if b == nil {
		return false
	}
	for _, it := range b.items {
		if it.delivery == delivery {
			return true
		}
	}
	return false
}

func (s *Server) enqueue(botName, hook string, it item) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batches == nil {
		s.batches = map[string]*batch{}
	}
	key := batchKey(botName, hook)
	b := s.batches[key]
	now := s.now()
	if b == nil {
		id := fmt.Sprintf("wh_%s_%d", hook, now.UnixNano())
		if it.delivery != "" {
			id = "wh_" + hook + "_" + it.delivery
		}
		b = &batch{bot: botName, hook: hook, first: now, eventID: id}
		s.batches[key] = b
	}
	if len(b.items) >= MaxBurst {
		b.dropped++
		return "dropped"
	}
	b.items = append(b.items, it)
	return "accepted"
}

func batchKey(botName, hook string) string { return botName + "\x00" + hook }

// Flush posts batches whose tumbling window has elapsed.
// A burst that is still inside the window stays in memory.
func (s *Server) Flush(now time.Time) error {
	if s == nil {
		return nil
	}
	if now.IsZero() {
		now = s.now()
	}
	window := s.window()
	s.mu.Lock()
	var due []*batch
	for k, b := range s.batches {
		if !now.Before(b.first.Add(window)) {
			due = append(due, b)
			delete(s.batches, k)
		}
	}
	s.mu.Unlock()
	var first error
	for _, b := range due {
		if err := postWebhook(s.Root, b); err != nil {
			s.restore(b)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Server) restore(b *batch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batches == nil {
		s.batches = map[string]*batch{}
	}
	key := batchKey(b.bot, b.hook)
	cur := s.batches[key]
	if cur == nil {
		s.batches[key] = b
		return
	}
	merged := append(append([]item{}, b.items...), cur.items...)
	dropped := b.dropped + cur.dropped
	if len(merged) > MaxBurst {
		dropped += len(merged) - MaxBurst
		merged = merged[:MaxBurst]
	}
	cur.items = merged
	cur.dropped = dropped
	if b.first.Before(cur.first) {
		cur.first = b.first
		cur.eventID = b.eventID
	}
}

func postWebhook(root string, b *batch) error {
	if b == nil || len(b.items) == 0 {
		return nil
	}
	bb, err := bot.Load(root, b.bot)
	if err != nil {
		return err
	}
	box, err := inbox.Open(bb.InboxPath())
	if err != nil {
		return err
	}
	ev := link.NewEvent("webhook:"+b.hook, batchType(b), renderBatch(b), b.items[0].sender, b.first)
	ev.ID = b.eventID
	return box.Post(ev)
}

func batchType(b *batch) string {
	typ := b.items[0].typ
	for _, it := range b.items[1:] {
		if it.typ != typ {
			return "webhook"
		}
	}
	if typ == "" {
		return "webhook"
	}
	return typ
}

func renderBatch(b *batch) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "<webhook name=%q count=\"%d\" dropped=\"%d\">\n", b.hook, len(b.items), b.dropped)
	for _, it := range b.items {
		if buf.Len() >= MaxMerged {
			break
		}
		if it.delivery != "" {
			fmt.Fprintf(&buf, "delivery: %s\n", it.delivery)
		}
		fmt.Fprintf(&buf, "type: %s\n", it.typ)
		fmt.Fprintf(&buf, "<delivery id=%q>\n", it.delivery)
		body := it.body
		remain := MaxMerged - buf.Len() - len("</delivery>\n")
		if remain < 0 {
			remain = 0
		}
		if len(body) > remain {
			body = body[:remain]
		}
		buf.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			buf.WriteByte('\n')
		}
		buf.WriteString("</delivery>\n")
	}
	buf.WriteString("</webhook>\n")
	out := buf.String()
	if len(out) > MaxMerged {
		out = out[:MaxMerged]
	}
	return out
}

func (s *Server) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) window() time.Duration {
	if s != nil && s.Window > 0 {
		return s.Window
	}
	return DefaultWindow
}

func durationOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}
