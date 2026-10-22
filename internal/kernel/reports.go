package kernel

import (
	"errors"
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// The transcript is the ledger: a result's event ID and the final reply's
// Reports are committed there. This index only caches its unreported tail;
// refresh reads new rows, not the whole history on every model call.
type reportIndex struct {
	through int
	pending map[string]pendingReport
}

type pendingReport struct {
	seq     int
	message provider.Message
}

// ErrPendingResult tells inbox consumers to retain an unsuccessful result
// delivery. Other poison events keep their existing acknowledge-on-error policy.
var ErrPendingResult = errors.New("result delivery remains pending")

func isResultEvent(ev link.Event) bool {
	if ev.Source != link.SourceSubagent || ev.ID == "" {
		return false
	}
	switch ev.Type {
	case exec.EvDone, exec.EvFailed, exec.EvBlocked, "fork_end":
		return true
	}
	return false
}

func reportDelivered(entries []bot.Entry, id string) bool {
	for _, e := range entries {
		for _, reported := range e.Reports {
			if reported == id {
				return true
			}
		}
	}
	return false
}

func (k *Kernel) pendingReports() (map[string]pendingReport, error) {
	k.reportMu.Lock()
	defer k.reportMu.Unlock()
	n, err := k.Bot.Count()
	if err != nil {
		return nil, err
	}
	i := &k.reports
	if i.pending == nil || n < i.through {
		*i = reportIndex{pending: map[string]pendingReport{}}
	}
	err = k.Bot.Walk(i.through+1, n, func(seq int, e bot.Entry) error {
		if e.Report && e.EventID != "" && e.Branch == "" {
			i.pending[e.EventID] = pendingReport{seq: seq, message: e.Message}
		}
		for _, id := range e.Reports {
			delete(i.pending, id)
		}
		i.through = seq
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]pendingReport, len(i.pending))
	for id, p := range i.pending {
		out[id] = p
	}
	return out, nil
}

const reportReference = "<reported_result>The result has already been delivered in the conversation; this is a reference, not another delivery.</reported_result>"

func reportMarker(id string) string { return "<deliver_result id=" + id + "/>" }

// reportProjection preserves the number and order of messages, so fit's
// transcript sequence mapping remains valid. Payload is present only in the
// generation assigned to deliver that event, never in an ordinary owner reply.
func reportProjection(messages []provider.Message, pending map[string]pendingReport, delivery string) []provider.Message {
	out := append([]provider.Message(nil), messages...)
	for i := range out {
		m := &out[i]
		switch {
		case len(m.Reports) > 0:
			m.Content = reportReference
		case m.ReportID != "":
			if _, ok := pending[m.ReportID]; !ok {
				m.Content = reportReference
			} else {
				m.Content = "<pending_result>A result is waiting for its own delivery turn.</pending_result>"
			}
		case m.Internal && strings.HasPrefix(m.Content, "<subagent_report>"):
			m.Content = "<internal_note>Result delivery is tracked separately.</internal_note>"
		case m.Internal && strings.HasPrefix(m.Content, "<deliver_result id="):
			m.Content = "<internal_note>Result delivery reference.</internal_note>"
		}
	}
	if p, ok := pending[delivery]; ok {
		// Use the newest delivery marker, or the opening result event. A
		// cancelled marker earlier in the same conversation never reactivates.
		for i := len(out) - 1; i >= 0; i-- {
			original := messages[i]
			if (original.Internal && original.Content == reportMarker(delivery)) || original.ReportID == delivery {
				hint, _ := subagentReportHint(link.Event{Source: link.SourceSubagent})
				out[i].Content = p.message.Content + "\n" + strings.Replace(hint.Content, "following result", "above result", 1)
				out[i].ReportID = delivery
				break
			}
		}
	}
	return out
}

func (r *turnRun) reportRequest(req provider.Request) (provider.Request, error) {
	pending, err := r.k.pendingReports()
	if err != nil {
		return req, err
	}
	if _, ok := pending[r.delivery]; !ok {
		r.delivery = ""
	}
	r.deliveryPayload = pending[r.delivery].message.Content
	req.Messages = reportProjection(req.Messages, pending, r.delivery)
	if r.delivery != "" {
		req.Tools = nil
	} // Retelling is a delivery, not new work.
	return req, nil
}

func (r *turnRun) reportReply(req provider.Request, msg provider.Message) (provider.Message, error) {
	if r.delivery == "" || len(msg.ToolCalls) > 0 || strings.TrimSpace(msg.Content) == "" || strings.TrimSpace(msg.Content) == "SILENT" || r.t.isForked() {
		return msg, nil
	}
	for _, m := range req.Messages {
		if m.ReportID == r.delivery && r.deliveryPayload != "" && strings.Contains(m.Content, r.deliveryPayload) && strings.Contains(m.Content, "<subagent_report>") {
			msg.Reports = []string{r.delivery}
			return msg, nil
		}
	}
	return provider.Message{}, errors.New("result delivery payload was removed by context fitting")
}

func (r *turnRun) nextReport() (string, bool) {
	r.delivery = ""
	if r.t.isForked() {
		return "", false
	}
	r.t.inter.mu.Lock()
	queuedOwner := len(r.t.inter.queue) > 0
	r.t.inter.mu.Unlock()
	if queuedOwner {
		return "", false
	}
	pending, err := r.k.pendingReports()
	if err != nil {
		r.fail(err)
		return "", false
	}
	first := 0
	for id, p := range pending {
		if first == 0 || p.seq < first {
			first, r.delivery = p.seq, id
		}
	}
	if r.delivery == "" {
		return "", false
	}
	return reportMarker(r.delivery), true
}

// compactReportHistory applies the same receipts to compression input. Pending
// payloads remain recoverable from the canonical transcript after compression.
func (k *Kernel) compactReportHistory(entries []bot.Entry) ([]provider.Message, error) {
	var pending map[string]pendingReport
	if k.Bot != nil {
		var err error
		pending, err = k.pendingReports()
		if err != nil {
			return nil, fmt.Errorf("result receipts: %w", err)
		}
	}
	messages := make([]provider.Message, len(entries))
	for i, e := range entries {
		messages[i] = e.Message
		messages[i].Internal = e.IsInternal()
		if e.Report {
			messages[i].ReportID = e.EventID
		}
		messages[i].Reports = e.Reports
	}
	return reportProjection(messages, pending, ""), nil
}
