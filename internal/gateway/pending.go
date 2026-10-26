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
	MaxPendingPerOwner = 5 // one per short-code session; one member cannot hold every slot
	PendingTTL         = 15 * time.Minute
)

type pendingInstall struct {
	Owner   string            `json:"owner"`
	Expires time.Time         `json:"expires"`
	Pub     ed25519.PublicKey `json:"pub"`
}

// pendingLocked returns a live pending install. It drops an expired one with
// its mail, and only the file when it cannot be parsed or the install was
// already admitted (a crash between writing installs/ and removing pending/),
// so a stale file never wipes a member's mailbox.
func (s *Server) pendingLocked(id string) (*pendingInstall, error) {
	path := filepath.Join(s.Dir, "pending", id)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p pendingInstall
	member, err := exists(filepath.Join(s.Dir, "installs", id))
	if err != nil {
		return nil, err
	}
	if member || json.Unmarshal(data, &p) != nil || ID(p.Pub) != id {
		return nil, os.Remove(path)
	}
	if !s.now().Before(p.Expires) {
		if err := s.dropMailLocked(id, ""); err != nil {
			return nil, err
		}
		if err := os.RemoveAll(filepath.Join(s.Dir, "acked", id)); err != nil {
			return nil, err
		}
		return nil, os.Remove(path)
	}
	return &p, nil
}

// dropMailLocked deletes queued mail between id and peer, or all of id's mail
// when peer is empty. Queue files are queue/<to>/<from>/<envelope>.json.
func (s *Server) dropMailLocked(id, peer string) error {
	files, err := s.queueLocked("")
	if err != nil {
		return err
	}
	for _, f := range files {
		from := filepath.Base(filepath.Dir(f.path))
		to := filepath.Base(filepath.Dir(filepath.Dir(f.path)))
		if (to == id && (peer == "" || from == peer)) || (from == id && (peer == "" || to == peer)) {
			if err := s.removeLocked(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) sweepPendingLocked() ([]*pendingInstall, error) {
	files, err := s.filesLocked("pending", "", 0)
	if err != nil {
		return nil, err
	}
	var live []*pendingInstall
	for _, f := range files {
		p, err := s.pendingLocked(f.info.Name())
		if err != nil {
			return nil, err
		}
		if p != nil {
			live = append(live, p)
		}
	}
	return live, nil
}

// sweepLocked drops expired pending installs at most once a minute. The next
// sweep is scheduled first, so a failing one cannot fail every request.
func (s *Server) sweepLocked() error {
	if s.now().Before(s.pendingSweep) {
		return nil
	}
	s.pendingSweep = s.now().Add(time.Minute)
	if _, err := s.sweepPendingLocked(); err != nil {
		return err
	}
	_, err := s.codeRecordsLocked() // also drops records past retention
	return err
}

func (s *Server) admitPendingLocked(pub ed25519.PublicKey, owner string) error {
	id := ID(pub)
	if ok, err := exists(filepath.Join(s.Dir, "installs", id)); err != nil || ok {
		return err
	}
	if ok, err := exists(filepath.Join(s.Dir, "installs", owner)); err != nil || !ok || owner == id {
		if err == nil {
			err = httpError{404, "no such owner"}
		}
		return err
	}
	p, err := s.pendingLocked(id)
	if err != nil {
		return err
	}
	if p != nil && p.Owner == owner {
		return nil
	}
	live, err := s.sweepPendingLocked()
	if err != nil {
		return err
	}
	mine := 0
	for _, q := range live {
		if q.Owner == owner {
			mine++
		}
	}
	if mine >= MaxPendingPerOwner || (p == nil && len(live) >= MaxPendingInstalls) {
		return httpError{429, "too many pending installs"}
	}
	expires := s.now().Add(PendingTTL)
	if p != nil {
		// A pending install that claims another owner's code (a retry on
		// another machine) is re-pointed: mail with the old owner goes, and
		// the expiry stays, so hopping between codes never extends it.
		if err := s.dropMailLocked(id, p.Owner); err != nil {
			return err
		}
		expires = p.Expires
	}
	data, err := json.Marshal(pendingInstall{owner, expires, pub})
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
	// Promotion counts against the claimant's daily new-install bucket, like
	// open registration (the Worker does the same).
	ip, claimed := s.claimIP[in.Install]
	if claimed && s.dailyFullLocked(ip) {
		return 0, nil, httpError{429, "too many registrations"}
	}
	if err := s.admitLocked(p.Pub); err != nil {
		return 0, nil, err
	}
	if claimed {
		s.regCount[ip]++
		delete(s.claimIP, in.Install)
	}
	return 200, map[string]string{"status": "ok"}, nil
}

// noteClaimLocked records the claimant's IP for a pending install, dropping
// entries whose pending install is gone so the map stays bounded.
func (s *Server) noteClaimLocked(id, ip string) error {
	if s.claimIP == nil {
		s.claimIP = make(map[string]string)
	}
	if len(s.claimIP) >= 4*MaxPendingInstalls {
		for k := range s.claimIP {
			if ok, err := exists(filepath.Join(s.Dir, "pending", k)); err != nil {
				return err
			} else if !ok {
				delete(s.claimIP, k)
			}
		}
	}
	s.claimIP[id] = ip
	return nil
}
