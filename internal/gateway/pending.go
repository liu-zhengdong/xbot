package gateway

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/fsutil"
)

// A pending install is a joiner admitted without the gateway token for one
// short-code pairing: it may poll and ack its own mailbox and send only to the
// owner whose code it claimed, and the owner may write back. The owner turns
// it into a full member with POST /v1/code/admit after 允许; otherwise it lapses
// after PendingTTL with its mail. Claiming codes (code.go) creates it.
const (
	MaxPendingInstalls = 16
	PendingTTL         = 15 * time.Minute
)

type pendingInstall struct {
	Owner   string            `json:"owner"`
	Expires time.Time         `json:"expires"`
	Pub     ed25519.PublicKey `json:"pub"`
}

func (s *Server) pendingLocked(id string) (*pendingInstall, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "pending", id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p pendingInstall
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if !s.now().Before(p.Expires) {
		return nil, s.dropPendingLocked(id)
	}
	return &p, nil
}

func (s *Server) dropPendingLocked(id string) error {
	files, err := s.queueLocked("")
	if err != nil {
		return err
	}
	for _, f := range files {
		sender := filepath.Dir(f.path)
		if filepath.Base(sender) == id || filepath.Base(filepath.Dir(sender)) == id {
			if err := s.removeLocked(f); err != nil {
				return err
			}
		}
	}
	if err := os.RemoveAll(filepath.Join(s.Dir, "acked", id)); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.Dir, "pending", id))
}

func (s *Server) sweepPendingLocked() ([]queueFile, error) {
	files, err := s.filesLocked("pending", "", 0)
	if err != nil {
		return nil, err
	}
	var live []queueFile
	for _, f := range files {
		p, err := s.pendingLocked(f.info.Name())
		if err != nil {
			return nil, err
		}
		if p != nil {
			live = append(live, f)
		}
	}
	return live, nil
}

// sweepLocked drops expired pending installs at most once a minute.
func (s *Server) sweepLocked() error {
	if s.now().Before(s.pendingSweep) {
		return nil
	}
	if _, err := s.sweepPendingLocked(); err != nil {
		return err
	}
	if _, err := s.codeRecordsLocked(); err != nil { // also drops records past retention
		return err
	}
	s.pendingSweep = s.now().Add(time.Minute)
	return nil
}

func (s *Server) admitPendingLocked(pub ed25519.PublicKey, owner string) error {
	id := ID(pub)
	if ok, err := exists(filepath.Join(s.Dir, "installs", id)); err != nil || ok {
		return err
	}
	p, err := s.pendingLocked(id)
	if err != nil {
		return err
	}
	// A pending install that claims a fresh code (a retry after a typo, or
	// another machine) is re-pointed at that code's owner. It can still only
	// ever talk to one owner.
	if p != nil && p.Owner == owner {
		return nil // Same owner: keep the original expiry.
	}
	if p == nil {
		files, err := s.sweepPendingLocked()
		if err != nil {
			return err
		}
		if len(files) >= MaxPendingInstalls {
			return httpError{429, "too many pending installs"}
		}
	}
	data, err := json.Marshal(pendingInstall{owner, s.now().Add(PendingTTL), pub})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(s.Dir, "pending", id), data, 0600)
}

func (s *Server) admitCode(_ *http.Request, body []byte, caller string, _ ed25519.PublicKey) (int, any, error) {
	var in struct {
		Install string `json:"install"`
	}
	if json.Unmarshal(body, &in) != nil || !validID(in.Install) {
		return 0, nil, httpError{400, "bad install"}
	}
	// Idempotent: an install that is already a member (paired elsewhere, or a
	// retried admit) is fine, so the owner can tell success from a lapsed pending.
	if ok, err := exists(filepath.Join(s.Dir, "installs", in.Install)); err != nil || ok {
		return 200, map[string]string{"status": "ok"}, err
	}
	p, err := s.pendingLocked(in.Install)
	if err != nil {
		return 0, nil, err
	}
	if p == nil || p.Owner != caller {
		return 0, nil, httpError{404, "no such pending install"}
	}
	err = s.admitLocked(p.Pub)
	return 200, map[string]string{"status": "ok"}, err
}
