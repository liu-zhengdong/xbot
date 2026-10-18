package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/pake"
)

// Two-step short-code pairing (G7). A (owner) shows the code; B (joiner) types
// it, claims the lookup at the gateway and runs SPAKE2 over the secret:
// pake1 B→A share and keys, pake2 A→B share, confirmation and sealed info,
// pake3 B→A confirmation after B's 连接, pake4 A→B sealed allow/deny.

type PairingCollision struct {
	Name string    `json:"name"`
	Seen time.Time `json:"seen,omitzero"` // the old device's last directory
	Bots int       `json:"bots"`
}
type PairingPhase struct {
	Phase     string            `json:"phase"`
	Code      string            `json:"code,omitempty"`
	Expires   time.Time         `json:"expires,omitempty"`
	Name      string            `json:"name,omitempty"`
	OS        string            `json:"os,omitempty"`
	Bots      []string          `json:"bots,omitempty"`
	Gateway   string            `json:"gateway,omitempty"`
	Collision *PairingCollision `json:"collision,omitempty"`
}

// Status is a detached UI snapshot. It never exposes keys or the PAKE state.
type Status struct {
	A PairingPhase `json:"a"`
	B PairingPhase `json:"b"`
}
type shortMessage struct {
	Lookup  string          `json:"lookup,omitempty"`
	PA      []byte          `json:"pa,omitempty"`
	PB      []byte          `json:"pb,omitempty"`
	Key     string          `json:"key,omitempty"`
	XKey    string          `json:"xkey,omitempty"`
	Confirm []byte          `json:"confirm,omitempty"`
	Info    json.RawMessage `json:"info,omitempty"`
}
type shortSession struct {
	peer, key, xkey, lookup string
	state                   *pake.State
	keys                    pake.Keys
	info                    pairInfo
	expires                 time.Time
	pa                      []byte            // A: B's pake1 share, to spot a redelivery
	reply                   *gateway.Envelope // A: signed pake2, resent on redelivery
	result                  *bool             // A: committed decision; never changes
}
type shortOwner struct {
	lookup, secret, person string
	expires                time.Time
	sessions               int
	active                 *shortSession
	client                 *gateway.Client
}
type pairingState struct {
	sync.Mutex
	status Status
	owner  *shortOwner
	join   *shortSession
	client *gateway.Client
	view   atomic.Pointer[Status]
}

var pairings sync.Map

func pairingFor(root string) *pairingState {
	path, _ := filepath.Abs(root)
	s, _ := pairings.LoadOrStore(path, &pairingState{status: Status{A: PairingPhase{Phase: "none"}, B: PairingPhase{Phase: "none"}}})
	return s.(*pairingState)
}

// publish keeps status polling independent of slow gateway operations.
func (s *pairingState) publish() { v := clonePairStatus(s.status); s.view.Store(&v) }
func (s *pairingState) release() { s.publish(); s.Unlock() }
func clonePairStatus(out Status) Status {
	out.A.Bots = append([]string(nil), out.A.Bots...)
	out.B.Bots = append([]string(nil), out.B.Bots...)
	if out.A.Collision != nil {
		c := *out.A.Collision
		out.A.Collision = &c
	}
	return out
}
func PairingStatus(root string) Status {
	if v := pairingFor(root).view.Load(); v != nil {
		return clonePairStatus(*v)
	}
	return Status{A: PairingPhase{Phase: "none"}, B: PairingPhase{Phase: "none"}}
}
func shortClient(root string) (*gateway.Client, error) {
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	if cfg.Gateway == "" {
		return nil, errors.New("remote: set a gateway first")
	}
	key, _, _, err := Identity(root)
	return &gateway.Client{Base: cfg.Gateway, Key: key}, err
}
func sendPairing(ctx context.Context, c *gateway.Client, env gateway.Envelope) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Send(ctx, env)
}

// receiveShort is reached before paired-device authentication; every phase still
// authenticates the sender and binds both identities and xkeys into SPAKE2.
func receiveShort(root, self string, env gateway.Envelope, now time.Time) error {
	if env.To != self || env.V != 1 || len(env.Body) > maxPairBody*4 || env.Time.Before(now.Add(-5*time.Minute)) || env.Time.After(now.Add(5*time.Minute)) {
		return nil
	}
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	var m shortMessage
	if env.Kind != "pake4" && json.Unmarshal(env.Body, &m) != nil {
		return nil
	}
	if env.Kind == "pake1" || env.Kind == "pake2" {
		pub, _ := base64.StdEncoding.DecodeString(m.Key)
		if !isB64(m.Key, 32) || !isB64(m.XKey, 32) || gateway.ID(pub) != env.From || env.Verify(pub) != nil {
			return nil
		}
	} else {
		var active *shortSession
		if env.Kind == "pake3" && s.owner != nil {
			active = s.owner.active
		}
		if env.Kind == "pake4" {
			active = s.join
		}
		if active == nil || active.peer != env.From {
			return nil
		}
		pub, _ := base64.StdEncoding.DecodeString(active.key)
		if env.Verify(pub) != nil {
			return nil
		}
	}
	switch env.Kind {
	case "pake1":
		return s.receivePake1(root, self, env, m, now)
	case "pake2":
		return s.receivePake2(root, env, m, now)
	case "pake3":
		return s.receivePake3(root, env, m, now)
	case "pake4":
		return s.receivePake4(root, env, now)
	}
	return nil
}
func infoPhase(phase string, info pairInfo, expires time.Time) PairingPhase {
	return PairingPhase{Phase: phase, Name: info.Name, OS: info.OS, Bots: info.Bots, Expires: expires}
}
func tickPairing(ctx context.Context, root string, client *gateway.Client, now time.Time) {
	_ = fsutil.WithLock(filepath.Join(root, "remote", "seen.json.lock"), func() error {
		s := pairingFor(root)
		s.Lock()
		defer s.release()
		s.tickJoin(now)
		return s.tickOwner(ctx, root, now)
	})
}
