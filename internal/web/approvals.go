package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/machine"
	"xbot/internal/provider"
	"xbot/internal/review"
)

// approvalWait is how long a card accepts a decision. The default is a full
// day: the owner may be away, and a card that expires silently loses work.
func (s *Server) approvalWait() time.Duration {
	if s.ApprovalWait > 0 {
		return s.ApprovalWait
	}
	return 24 * time.Hour
}

// pendingApproval is one card waiting for a decision. It outlives the request
// that raised it: the owner may decide after the stream that showed the card
// has closed.
type pendingApproval struct {
	bot      string
	tool     string
	summary  string
	wait     time.Duration
	deadline time.Time
	// done carries the decision to the waiting wake. It is never closed, so a
	// late decision after the wait ended is dropped instead of panicking.
	done chan approvalDecision
	// decided guards against a second decision overwriting the first.
	decided bool
}

// RegisterApproval publishes a card so a later request may decide it. It is
// exported for the serve wiring that drops cards left pending by a restart.
func (s *Server) RegisterApproval(botName, id, tool, summary string) {
	s.mu.Lock()
	if s.approvals == nil {
		s.approvals = map[string]*pendingApproval{}
	}
	s.approvals[botName+"/"+id] = &pendingApproval{
		bot: botName, tool: tool, summary: summary,
		wait: s.approvalWait(), deadline: time.Now().Add(s.approvalWait()),
		done: make(chan approvalDecision, 1),
	}
	s.mu.Unlock()
}

// DropApproval forgets a card without a decision, so a caller can resolve it in
// the transcript itself (a process restart does this on startup).
func (s *Server) DropApproval(botName, id string) {
	s.mu.Lock()
	delete(s.approvals, botName+"/"+id)
	s.mu.Unlock()
}

// webApprove shows one confirmation card in the stream and persists it in the
// transcript, first as pending and then with the resolved state, so the card
// survives the reply ending and a page reload. A timeout or cancelled wake
// with no decision records an expired card, never a pending one.
func (s *Server) webApprove(b *bot.Bot, k *kernel.Kernel, send func(string, any)) func(context.Context, string, string) (bool, error) {
	return func(ctx context.Context, tool, summary string) (bool, error) {
		id := newApprovalID()
		key := b.Name() + "/" + id
		s.mu.Lock()
		if s.approvals == nil {
			s.approvals = map[string]*pendingApproval{}
		}
		card := &pendingApproval{
			bot: b.Name(), tool: tool, summary: summary,
			wait: s.approvalWait(), deadline: time.Now().Add(s.approvalWait()),
			done: make(chan approvalDecision, 1),
		}
		s.approvals[key] = card
		s.mu.Unlock()
		// The card outlives this wake: a decision arriving after the wake ended
		// still lands on the persisted card, and only the next card with the
		// same key may replace it.
		defer func() {
			s.mu.Lock()
			if s.approvals[key] == card {
				delete(s.approvals, key)
			}
			s.mu.Unlock()
		}()
		title, subtitle := review.CardText(tool, agent.ToolArgs(ctx), summary, machine.Local)
		deadline := card.deadline.Format(time.RFC3339)
		appendCard := func(state string) {
			text := bot.ApprovalCardText(id, tool, title, subtitle, summary, state)
			if state == bot.ApprovalPending {
				text = bot.ApprovalCardTextAt(id, tool, title, subtitle, summary, state, deadline)
			}
			_ = b.Append(bot.Entry{Branch: kernel.BranchOf(ctx), Message: provider.Message{Role: provider.RoleUser, Content: text}})
		}
		appendCard(bot.ApprovalPending)
		payload := map[string]any{
			"id": id, "tool": tool, "summary": summary, "title": title, "subtitle": subtitle,
			"deadline": deadline,
		}
		names := approvalSecretNames(tool, summary)
		if len(names) > 0 {
			payload["secrets"] = names
		}
		send("approval", payload)
		timer := time.NewTimer(card.wait)
		defer timer.Stop()
		// A wake that moves to the background cancels ctx while the card is
		// still open. That must not decide anything: the card keeps waiting for
		// the owner, and only its own deadline or a real decision ends it.
		select {
		case d := <-card.done:
			if d.allow && k != nil && len(names) > 0 && len(d.secrets) > 0 {
				k.StageSecrets(linkIDFromSummary(summary), filterSecrets(names, d.secrets))
			}
			state := bot.ApprovalDeclined
			if d.allow {
				state = bot.ApprovalApproved
			}
			appendCard(state)
			return d.allow, nil
		case <-timer.C:
			s.retireExpired(key, card)
			appendCard(bot.NoDecisionText(bot.ApprovalReasonExpired))
			return false, fmt.Errorf("%w: %s", kernel.ErrApprovalExpired, tool)
		case <-ctx.Done():
			// The wake moved to the background with the card still open. Keep
			// waiting for the owner, but never past the card's own deadline:
			// the same timer that would have fired in the foreground keeps
			// running here, so an undecided card still expires instead of
			// blocking this goroutine forever.
			if !s.waitStillOpen(key, card) {
				appendCard(bot.NoDecisionText(bot.ApprovalReasonExpired))
				return false, fmt.Errorf("%w: %s", kernel.ErrApprovalExpired, tool)
			}
			select {
			case d, ok := <-card.done:
				if !ok {
					s.retireExpired(key, card)
					appendCard(bot.NoDecisionText(bot.ApprovalReasonExpired))
					return false, fmt.Errorf("%w: %s", kernel.ErrApprovalExpired, tool)
				}
				return s.applyDecision(b, k, id, summary, names, d, appendCard)
			case <-timer.C:
				s.retireExpired(key, card)
				appendCard(bot.NoDecisionText(bot.ApprovalReasonExpired))
				return false, fmt.Errorf("%w: %s", kernel.ErrApprovalExpired, tool)
			}
		}
	}
}

// retireExpired records a card whose window passed with no decision, so a late
// answer is told the card expired instead of looking like an unknown id. Note
// the table is only written when a card actually times out: a card created with
// a tiny wait would otherwise be filed as expired while it is still waiting.
func (s *Server) retireExpired(key string, card *pendingApproval) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.expired == nil {
		s.expired = map[string]*pendingApproval{}
	}
	if len(s.expired) > 4096 {
		cutoff := time.Now().Add(-24 * time.Hour)
		for k, c := range s.expired {
			if c.deadline.Before(cutoff) {
				delete(s.expired, k)
			}
		}
	}
	s.expired[key] = card
	if s.approvals[key] == card {
		delete(s.approvals, key)
	}
}

// waitStillOpen reports the card is still the one registered, so a ctx
// cancellation left it waiting rather than ending it.
func (s *Server) waitStillOpen(key string, card *pendingApproval) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.approvals[key] == card && !card.decided
}

// applyDecision records a decision that arrived after the waiting wake ended.
func (s *Server) applyDecision(b *bot.Bot, k *kernel.Kernel, id, summary string, names []string, d approvalDecision, appendCard func(string)) (bool, error) {
	if d.allow && k != nil && len(names) > 0 && len(d.secrets) > 0 {
		k.StageSecrets(linkIDFromSummary(summary), filterSecrets(names, d.secrets))
	}
	state := bot.ApprovalDeclined
	if d.allow {
		state = bot.ApprovalApproved
	}
	appendCard(state)
	return d.allow, nil
}

func approvalSecretNames(tool, summary string) []string {
	if tool != "links" {
		return nil
	}
	return secretNamesFromSummary(summary)
}

func linkIDFromSummary(summary string) string {
	line, _, _ := strings.Cut(summary, "\n")
	line = strings.TrimSpace(line)
	id, ok := strings.CutPrefix(line, "install ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

func secretNamesFromSummary(summary string) []string {
	var names []string
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "secrets:")
		if !ok {
			continue
		}
		for _, part := range strings.Split(rest, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				names = append(names, part)
			}
		}
	}
	return names
}

func filterSecrets(names []string, values map[string]string) map[string]string {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	out := map[string]string{}
	for name, value := range values {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if allowed[name] && value != "" {
			out[name] = value
		}
	}
	return out
}

func newApprovalID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Allow   bool              `json:"allow"`
		Secrets map[string]string `json:"secrets"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	key := b.Name() + "/" + r.PathValue("id")
	s.mu.Lock()
	card, live := s.approvals[key]
	if !live {
		card = s.expired[key]
	}
	switch {
	case card == nil:
		s.mu.Unlock()
		writeErr(w, http.StatusNotFound, "no such approval")
		return
	case card.decided:
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "already decided")
		return
	case !live || time.Now().After(card.deadline):
		// The wait already ended with no decision. The card is kept for a while
		// so a late answer reports 410 instead of looking like an unknown id.
		s.expired[key] = card
		delete(s.approvals, key)
		s.mu.Unlock()
		writeErr(w, http.StatusGone, "approval expired")
		return
	}
	card.decided = true
	// The decision is handed to whoever waits, in this process or in the wake
	// that raised the card. A wake that already gave up leaves it unread; the
	// buffered channel keeps that from blocking this request.
	card.done <- approvalDecision{allow: in.Allow, secrets: in.Secrets}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
