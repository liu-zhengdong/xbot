package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"xbot/internal/gateway"
	"xbot/internal/pake"
)

// The joiner (B) side of short-code pairing.

// SetJoinPhase records a join failure found before the gateway was asked
// (the web layer's gateway check), so the modal shows it like the others.
func SetJoinPhase(root string, phase PairingPhase) {
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	s.join, s.client = nil, nil
	s.status.B = phase
}
func CancelJoin(root string) {
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	s.join, s.client = nil, nil
	s.status.B = PairingPhase{Phase: "none"}
}

func JoinShortCode(ctx context.Context, root, code, gatewayURL string) error {
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	s.join, s.client = nil, nil
	s.status.B = PairingPhase{Phase: "none"}
	lookup, secret, err := ParseShortCode(code)
	if err != nil {
		s.status.B = PairingPhase{Phase: "typo"}
		return err
	}
	if err = pairingGateway(root, gatewayURL); err != nil {
		return err
	}
	c, err := shortClient(root)
	if err != nil {
		return err
	}
	s.client = c
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	owner, err := c.ClaimCode(ctx, lookup)
	if err != nil {
		phase := "gateway_unreachable"
		var he *gateway.HTTPError
		if errors.As(err, &he) {
			if he.StatusCode == 404 {
				phase = "wrong_or_used"
			}
			if he.StatusCode == 410 {
				phase = "expired"
			}
		}
		s.status.B = PairingPhase{Phase: phase, Gateway: c.Base}
		return err
	}
	wakeRun(root) // The poll loop may be backing off after a 403 from before the claim.
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	if d, ok := cfg.ByID(owner); ok {
		s.status.B = PairingPhase{Phase: "already", Name: deviceLabel(d)}
		return nil
	}
	return s.startJoin(ctx, root, c, owner, lookup, secret)
}

func (s *pairingState) startJoin(ctx context.Context, root string, c *gateway.Client, owner, lookup, secret string) error {
	_, self, key, err := Identity(root)
	if err != nil {
		return err
	}
	_, xkey, err := XIdentity(root)
	if err != nil {
		return err
	}
	state, pa, err := pake.New(pake.RoleA, []byte(self), []byte(owner), pake.Password(pairV2, []byte(lookup+secret)))
	if err != nil {
		return err
	}
	s.join = &shortSession{peer: owner, lookup: lookup, state: state, expires: time.Now().Add(20 * time.Second), pa: pa}
	s.status.B = PairingPhase{Phase: "checking", Expires: s.join.expires}
	s.publish()
	env := gateway.NewEnvelope(c.Key, owner, "pake1", nil, time.Now())
	env.Body, _ = json.Marshal(shortMessage{Lookup: lookup, PA: pa, Key: key, XKey: xkey})
	env.Sign(c.Key)
	if err = sendPairing(ctx, c, env); err != nil {
		s.join = nil
		s.status.B = PairingPhase{Phase: "gateway_unreachable", Gateway: c.Base}
	}
	return err
}
func (s *pairingState) receivePake2(root string, env gateway.Envelope, m shortMessage, now time.Time) error {
	j := s.join
	if j == nil || j.peer != env.From || s.status.B.Phase != "checking" || !now.Before(j.expires) || !bytes.Equal(m.Echo, paEcho(j.pa)) {
		return nil // An answer to an earlier attempt is not a wrong code.
	}
	_, _, bk, err := Identity(root)
	if err != nil {
		return err
	}
	_, bx, err := XIdentity(root)
	if err != nil {
		return err
	}
	keys, err := j.state.Finish(m.PB, pairAAD(j.lookup, bk, bx, m.Key, m.XKey))
	if err != nil || !keys.Verify(m.Confirm) {
		s.join = nil
		s.status.B = PairingPhase{Phase: "wrong"}
		return nil
	}
	var info pairInfo
	if openPair(keys, env, m.Info, &info) != nil || !validPairInfo(&info) {
		s.join = nil
		s.status.B = PairingPhase{Phase: "wrong"}
		return nil
	}
	// A's deadline and the envelope time are both on A's clock (and signed),
	// so B's window does not depend on the two clocks agreeing.
	left := time.Unix(info.Deadline, 0).Sub(env.Time)
	if info.Deadline == 0 || left <= 0 {
		s.join = nil
		s.status.B = PairingPhase{Phase: "offline"}
		return nil
	}
	j.state = nil
	j.keys, j.key, j.xkey, j.info = keys, m.Key, m.XKey, info
	j.expires = now.Add(min(left, 2*time.Minute))
	s.status.B = infoPhase("confirm", info, j.expires)
	return nil
}
func (s *pairingState) receivePake4(root string, env gateway.Envelope, now time.Time) error {
	// A's allow can arrive after B's window (B asleep, a slow gateway) when A
	// has already saved B, so accept it for 5 more minutes.
	j, late := s.join, false
	if j == nil || j.peer != env.From || !(s.status.B.Phase == "waiting" || s.status.B.Phase == "confirm" && j.sent) {
		j, late = s.late, true
	}
	if j == nil || !now.Before(j.expires.Add(5*time.Minute)) {
		return nil
	}
	late = late || !now.Before(j.expires)
	var result struct {
		OK bool `json:"ok"`
	}
	if openPair(j.keys, env, env.Body, &result) != nil {
		return nil
	}
	if result.OK {
		// Bind A to this install's person, as A binds B, so B's bots can
		// message A's and A's directory shows here (an unbound device is inert).
		personID, err := pairingPerson(root, j.person)
		if err != nil {
			return err
		}
		d, err := savePairDevice(root, j.info.Name, j.key, j.xkey, personID, false)
		if err != nil {
			return err
		}
		s.status.B = PairingPhase{Phase: "done", Name: deviceLabel(d)}
		wakeDirectories(root)
	} else if !late {
		s.status.B = PairingPhase{Phase: "denied"}
	}
	if j == s.late {
		s.late = nil
	} else {
		s.join = nil
	}
	return nil
}

// ConfirmJoin sends 连接 for personID (the signed-in person; "" when this
// install has exactly one), who is also bound to A once it allows.
func ConfirmJoin(ctx context.Context, root, personID string) error {
	s := pairingFor(root)
	s.Lock()
	defer s.release()
	j := s.join
	if j == nil || s.status.B.Phase != "confirm" || !time.Now().Before(j.expires) {
		return errors.New("remote: no live join confirmation")
	}
	info, err := localPairInfo(root, personID)
	if err != nil {
		return err
	}
	env := gateway.NewEnvelope(s.client.Key, j.peer, "pake3", nil, time.Now())
	sealed, err := sealPair(j.keys, env, info)
	if err != nil {
		return err
	}
	env.Body, _ = json.Marshal(shortMessage{Confirm: j.keys.MyConfirm, Info: sealed})
	env.Sign(s.client.Key)
	// Set before sending: if the gateway queued pake3 but the response was
	// lost, A's allow must still be accepted while B shows 连接 again.
	j.person, j.sent = personID, true
	if err = sendPairing(ctx, s.client, env); err != nil {
		return err
	}
	j.expires = time.Now().Add(2*time.Minute + 15*time.Second)
	s.status.B = infoPhase("waiting", j.info, j.expires)
	return nil
}
func (s *pairingState) tickJoin(now time.Time) {
	if j := s.join; j != nil && !now.Before(j.expires) {
		phase := "denied"
		if s.status.B.Phase == "checking" {
			phase = "offline"
		}
		if phase == "denied" {
			s.late = j
		}
		s.join = nil
		s.status.B = PairingPhase{Phase: phase}
	}
	if l := s.late; l != nil && !now.Before(l.expires.Add(5*time.Minute)) {
		s.late = nil
	}
}
