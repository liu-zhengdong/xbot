package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/peer"
	"xbot/internal/remote"
)

const (
	peerPageDefault = 50
	peerPageMax     = 200
)

// peerRow is one line of a peer thread. From and To keep the full address for
// remote rows; the split bot and device fields spare clients parsing bot@device.
// Device is the label if the paired device has one, else its name.
type peerRow struct {
	ID         string    `json:"id"`
	Seq        int       `json:"seq"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Text       string    `json:"text"`
	TS         time.Time `json:"ts"`
	Kind       string    `json:"kind,omitempty"`
	Status     string    `json:"status,omitempty"`
	FromBot    string    `json:"from_bot"`
	FromDevice string    `json:"from_device,omitempty"`
	ToBot      string    `json:"to_bot"`
	ToDevice   string    `json:"to_device,omitempty"`
}

func (s *Server) peerMessages(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	aName := r.PathValue("name")
	bName := r.PathValue("peer")
	if aName == "" || aName == bName {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	a, err := s.owned(aName)
	if err != nil {
		writeMissing(w, err)
		return
	}
	var msgs []peer.Direct
	labels := peerLabels{a: aName}
	if remoteBot, device, isRemote := strings.Cut(bName, "@"); isRemote {
		var found bool
		if labels.bDevice, found = s.peerDevice(device); !found || !bot.ValidName(remoteBot) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		labels.b, labels.bBot = bName, remoteBot
		msgs, err = peer.RemoteConversation(a, bName)
	} else {
		b, ownErr := s.owned(bName)
		if ownErr != nil {
			writeMissing(w, ownErr)
			return
		}
		labels.b, labels.bBot = bName, bName
		msgs, err = peer.Conversation(a, b)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit, err := peerLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad limit")
		return
	}
	page, err := peerPage(msgs, r.URL.Query().Get("before"), limit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad before")
		return
	}
	rows := make([]peerRow, 0, len(page))
	for _, m := range page {
		rows = append(rows, peerRow{
			ID: m.ID, Seq: m.Seq, From: m.From, To: m.To, Text: m.Text, TS: m.Time.UTC(),
			Kind: m.Kind, Status: m.Status,
			FromBot: labels.name(m.From), FromDevice: labels.device(m.From),
			ToBot: labels.name(m.To), ToDevice: labels.device(m.To),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": rows})
}

// peerLabels splits thread addresses into bot and device. a is the local bot
// (no device); b is the peer address as requested, bBot its bot name, and
// bDevice the paired device's label or name ("" for a local peer).
type peerLabels struct {
	a, b, bBot, bDevice string
}

func (l peerLabels) name(addr string) string {
	if addr == l.b {
		return l.bBot
	}
	return addr
}

func (l peerLabels) device(addr string) string {
	if addr == l.b {
		return l.bDevice
	}
	return ""
}

// peerDevice returns a paired device's label, or its name, and whether the
// device table has that name. Unpaired devices still answer, so their history
// stays readable. A config that fails to load answers false.
func (s *Server) peerDevice(name string) (label string, found bool) {
	cfg, err := remote.Load(s.Root)
	if err != nil || name == "" {
		return "", false
	}
	d, found := cfg.ByName(name)
	if !found {
		return "", false
	}
	if d.Label != "" {
		return d.Label, true
	}
	return d.Name, true
}

func peerLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return peerPageDefault, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > peerPageMax {
		return 0, errors.New("bad limit")
	}
	return n, nil
}

func peerPage(msgs []peer.Direct, before string, limit int) ([]peer.Direct, error) {
	end := len(msgs)
	if strings.TrimSpace(before) != "" {
		idx, err := peerCursor(msgs, before)
		if err != nil {
			return nil, err
		}
		end = idx
	}
	start := max(end-limit, 0)
	// Rows sharing one entry (several message_bot calls) share <to>:<seq>, so a
	// page never splits them: the cursor of the oldest row must cover them all.
	for start > 0 && start < end && msgs[start-1].To == msgs[start].To && msgs[start-1].Seq == msgs[start].Seq {
		start--
	}
	return msgs[start:end], nil
}

// peerCursor finds the exclusive upper bound. before is "<to>:<seq>", or a bare
// seq when exactly one message in the thread carries that number. Remote
// addresses hold no colon, so the first colon always ends `<to>`.
func peerCursor(msgs []peer.Direct, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	to, seqPart, qualified := strings.Cut(raw, ":")
	if !qualified {
		seq, err := strconv.Atoi(raw)
		if err != nil || seq < 1 {
			return 0, errors.New("bad before")
		}
		idx := -1
		for i, m := range msgs {
			if m.Seq != seq {
				continue
			}
			if idx >= 0 {
				return 0, errors.New("bad before")
			}
			idx = i
		}
		if idx < 0 {
			return 0, errors.New("bad before")
		}
		return idx, nil
	}
	seq, err := strconv.Atoi(seqPart)
	if err != nil || seq < 1 || to == "" {
		return 0, errors.New("bad before")
	}
	for i, m := range msgs {
		if m.To == to && m.Seq == seq {
			return i, nil
		}
	}
	return 0, errors.New("bad before")
}
