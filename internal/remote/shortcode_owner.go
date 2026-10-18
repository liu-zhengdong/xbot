package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/pake"
)

// The owner (A) side of short-code pairing: the code, up to five SPAKE2
// sessions, and 「X 想连到这台电脑」 with 拒绝/允许 (auto-declined after 2 minutes).

func cancelLookup(ctx context.Context, c *gateway.Client, lookup string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = c.CancelCode(ctx, lookup)
}
func (s *pairingState) killCode(ctx context.Context) {
	if s.owner != nil && s.owner.secret != "" {
		s.owner.secret = ""
		cancelLookup(ctx, s.owner.client, s.owner.lookup)
	}
}

func NewShortCode(ctx context.Context, root, personID string) (display string, expires time.Time, err error) {
	personID, err = pairingPerson(root, personID)
	if err != nil {
		return
	}
	c, err := shortClient(root)
	if err != nil {
		return "", expires, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = c.Register(ctx, gatewayToken()); err != nil {
		return
	}
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	s.killCode(ctx)
	s.owner = nil
	s.status.A = PairingPhase{Phase: "none"}
	for range 3 {
		var code string
		code, err = randomShortCode()
		if err != nil {
			return
		}
		expires = time.Now().Add(inviteTTL)
		err = c.RegisterCode(ctx, code[:4], inviteTTL)
		var he *gateway.HTTPError
		if errors.As(err, &he) && he.StatusCode == 429 {
			// 换一个 pressed twice within the gateway's per-owner gap.
			select {
			case <-ctx.Done():
				return "", expires, ctx.Err()
			case <-time.After(2100 * time.Millisecond):
			}
			err = c.RegisterCode(ctx, code[:4], inviteTTL)
		}
		if errors.As(err, &he) && he.StatusCode == 409 {
			continue
		}
		if err != nil {
			return
		}
		display = displayCode(code)
		s.owner = &shortOwner{lookup: code[:4], secret: code[4:11], person: personID, expires: expires, client: c}
		s.status.A = PairingPhase{Phase: "waiting", Code: display, Expires: expires}
		return
	}
	return
}
func CancelShortCode(ctx context.Context, root string) {
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	s.killCode(ctx)
	s.owner = nil
	s.status.A = PairingPhase{Phase: "none"}
}
func (s *pairingState) receivePake1(root, self string, env gateway.Envelope, m shortMessage, now time.Time) error {
	ctx := context.Background()
	o := s.owner
	if o == nil || o.secret == "" || o.lookup != m.Lookup || !now.Before(o.expires) || s.status.A.Phase == "request" {
		return nil
	}
	if a := o.active; a != nil && now.Before(a.expires) {
		// A redelivered pake1 (our pake2 send failed, so it was not acked):
		// resend the same pake2 without a new session or guess.
		if a.peer == env.From && a.reply != nil && bytes.Equal(a.pa, m.PA) && s.status.A.Phase != "request" {
			return sendPairing(ctx, o.client, *a.reply)
		}
		// The same joiner retrying (it gave up waiting) starts a new session,
		// counted like any other; anyone else waits for this one to end.
		if a.peer != env.From || s.status.A.Phase == "request" {
			return nil
		}
	}
	o.sessions++
	defer func() {
		if o.sessions >= gateway.MaxCodeClaims {
			s.killCode(ctx) // The fifth session stays live; the tick shows 已过期 when it ends.
		}
	}()
	if o.sessions > gateway.MaxCodeClaims {
		s.killCode(ctx)
		o.active = nil
		s.status.A.Phase = "expired"
		return nil
	}
	o.active = nil
	_, _, ak, err := Identity(root)
	if err != nil {
		return err
	}
	_, ax, err := XIdentity(root)
	if err != nil {
		return err
	}
	state, pb, err := pake.New(pake.RoleB, []byte(env.From), []byte(self), pake.Password(pairV2, []byte(o.lookup+o.secret)))
	if err != nil {
		return err
	}
	keys, err := state.Finish(m.PA, pairAAD(o.lookup, m.Key, m.XKey, ak, ax))
	if err != nil {
		return nil
	}
	info, err := localPairInfo(root, o.person)
	if err != nil {
		return err
	}
	expires := now.Add(2 * time.Minute)
	info.Deadline = expires.Add(-5 * time.Second).Unix() // margin for relay delay
	reply := gateway.NewEnvelope(o.client.Key, env.From, "pake2", nil, now)
	sealed, err := sealPair(keys, reply, info)
	if err != nil {
		return err
	}
	reply.Body, _ = json.Marshal(shortMessage{PB: pb, Key: ak, XKey: ax, Confirm: keys.MyConfirm, Info: sealed, Echo: paEcho(m.PA)})
	reply.Sign(o.client.Key)
	o.active = &shortSession{peer: env.From, key: m.Key, xkey: m.XKey, keys: keys, expires: expires, pa: bytes.Clone(m.PA), reply: &reply}
	return sendPairing(ctx, o.client, reply)
}
func (s *pairingState) receivePake3(root string, env gateway.Envelope, m shortMessage, now time.Time) error {
	ctx := context.Background()
	o := s.owner
	a := o.active
	if s.status.A.Phase == "request" || !now.Before(a.expires) {
		return nil
	}
	if !a.keys.Verify(m.Confirm) {
		o.active = nil
		return nil
	}
	var info pairInfo
	if openPair(a.keys, env, m.Info, &info) != nil || !validPairInfo(&info) {
		o.active = nil
		return nil
	}
	s.killCode(ctx)
	a.info, a.expires = info, now.Add(2*time.Minute)
	s.status.A = infoPhase("request", info, a.expires)
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	for _, d := range cfg.Devices {
		if deviceLabel(d) == info.Name && d.Key != a.key && d.Person == o.person {
			var cache dirCache
			_ = readJSON(filepath.Join(root, "remote", "dirs", d.ID+".json"), &cache) // Display only.
			s.status.A.Collision = &PairingCollision{Name: info.Name, Seen: cache.Received, Bots: len(cache.Bots)}
			break
		}
	}
	return nil
}
func DecidePairing(ctx context.Context, root string, allow, replace bool) error {
	// Same lock order as Receive: seen -> pairing -> devices.
	return fsutil.WithLock(filepath.Join(root, "remote", "seen.json.lock"), func() error {
		s := pairingFor(root)
		s.Lock()
		defer s.release()
		if s.owner == nil || s.owner.active == nil || s.status.A.Phase != "request" {
			return errors.New("remote: no pairing request")
		}
		return s.decide(ctx, root, allow && time.Now().Before(s.owner.active.expires), replace)
	})
}
func (s *pairingState) decide(ctx context.Context, root string, allow, replace bool) error {
	o, name := s.owner, ""
	a := o.active
	if a.result != nil {
		allow, replace = *a.result, a.replace // Committed: a retry or timeout resends the same answer.
	}
	if allow {
		// Promote B from a pending install to a full gateway member (the
		// gateway answers 200 if B already is one). Nothing is saved unless
		// this succeeds, so a failed admission leaves no half-paired device.
		if err := o.client.AdmitCode(ctx, a.peer); err != nil {
			return err
		}
		// B is a member now, so never turn this into a deny: a failed save
		// is retried by the tick.
		committed := true
		a.result, a.replace = &committed, replace
		d, err := savePairDevice(root, a.info.Name, a.key, a.xkey, o.person, replace)
		if err != nil {
			return err
		}
		name = deviceLabel(d)
	}
	committed := allow
	a.result = &committed
	env := gateway.NewEnvelope(o.client.Key, a.peer, "pake4", nil, time.Now())
	body, err := sealPair(a.keys, env, struct {
		OK bool `json:"ok"`
	}{allow})
	if err != nil {
		return err
	}
	env.Body = body
	env.Sign(o.client.Key)
	// Keep the request for a send retry; saving the same id is idempotent.
	if err = sendPairing(ctx, o.client, env); err != nil {
		return err
	}
	s.status.A = PairingPhase{Phase: "denied"}
	if allow {
		s.status.A = PairingPhase{Phase: "done", Name: name}
		wakeDirectories(root)
	}
	s.owner = nil
	return nil
}
func (s *pairingState) tickOwner(ctx context.Context, root string, now time.Time) error {
	o := s.owner
	if o == nil {
		return nil
	}
	if s.status.A.Phase == "request" {
		a := o.active
		if a.result != nil && !now.Before(a.expires.Add(5*time.Minute)) {
			// The answer never got through within B's late window. Settle locally.
			s.status.A = PairingPhase{Phase: "denied"}
			if *a.result {
				s.status.A = PairingPhase{Phase: "done", Name: a.info.Name}
			}
			s.owner = nil
			return nil
		}
		if a.result != nil || !now.Before(a.expires) {
			return s.decide(ctx, root, false, false) // Resends a committed answer at once.
		}
	} else {
		if !now.Before(o.expires) && o.secret != "" {
			s.killCode(ctx)
			if o.active == nil {
				s.status.A.Phase = "expired"
			}
		}
		if o.active != nil && !now.Before(o.active.expires) {
			o.active = nil
			if o.secret == "" {
				s.status.A.Phase = "expired"
			}
		}
	}
	return nil
}
