// Package pake implements SPAKE2 as specified in RFC 9382, ciphersuite
// SPAKE2-P256-SHA256-HKDF-HMAC.
//
// G7 device pairing uses it over the secret part of a short pairing code.
// The gateway only relays the messages: without the secret it learns nothing
// it can test offline, and an active attacker gets one guess per session.
package pake

import (
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
)

// Role selects the RFC 9382 side: A sends pA = x*P + w*M, B sends pB = y*P + w*N.
type Role int

const (
	RoleA Role = iota
	RoleB
)

var (
	curve = elliptic.P256()
	order = curve.Params().N
	// RFC 9382 §6, compressed SEC1.
	mx, my = mustPoint("02886e2f97ace46e55ba9dd7242579f2993b64e16ef3dcab95afd497333d8fa12f")
	nx, ny = mustPoint("03d8bbd6c639c62937b04d997f38c3770719c629d7014d49a24b4f98baa1292b49")

	ErrBadMessage = errors.New("pake: invalid peer message")
)

func mustPoint(s string) (*big.Int, *big.Int) {
	b, _ := hex.DecodeString(s)
	x, y := elliptic.UnmarshalCompressed(curve, b)
	if x == nil {
		panic("pake: bad constant")
	}
	return x, y
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }

// Password derives w: SHA-512(domain || 0x00 || secret) reduced mod n, 32 bytes big-endian.
func Password(domain string, secret []byte) []byte {
	h := sha512.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(secret)
	w := new(big.Int).SetBytes(h.Sum(nil))
	w.Mod(w, order)
	if w.Sign() == 0 { // probability 2^-256; keep w a valid nonzero scalar
		w.SetInt64(1)
	}
	return w.FillBytes(make([]byte, 32))
}

// State is one side of one exchange; Finish may be called once.
type State struct {
	mu       sync.Mutex
	role     Role
	idA, idB []byte
	w        []byte
	scalar   []byte
	msg      []byte
	used     bool
}

// Keys are the RFC 9382 outputs.
type Keys struct {
	Ke, MyConfirm, PeerConfirm []byte
}

// Verify compares the peer's confirmation in constant time.
func (k Keys) Verify(got []byte) bool { return hmac.Equal(got, k.PeerConfirm) }

// New starts an exchange with a random scalar and returns the message to send.
func New(role Role, idA, idB, w []byte) (*State, []byte, error) {
	for {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, nil, err
		}
		s := new(big.Int).SetBytes(k)
		if s.Sign() > 0 && s.Cmp(order) < 0 {
			return newWithScalar(role, idA, idB, w, k)
		}
	}
}

func newWithScalar(role Role, idA, idB, w, scalar []byte) (*State, []byte, error) {
	wi := new(big.Int).SetBytes(w)
	if len(w) != 32 || wi.Sign() == 0 || wi.Cmp(order) >= 0 {
		return nil, nil, errors.New("pake: bad password scalar")
	}
	bx, by := curve.ScalarBaseMult(scalar)
	px, py := mx, my
	if role == RoleB {
		px, py = nx, ny
	}
	wx, wy := curve.ScalarMult(px, py, w)
	x, y := curve.Add(bx, by, wx, wy)
	if x.Sign() == 0 && y.Sign() == 0 {
		return nil, nil, errors.New("pake: degenerate message")
	}
	msg := elliptic.Marshal(curve, x, y)
	st := &State{role: role, idA: clone(idA), idB: clone(idB), w: clone(w), scalar: clone(scalar), msg: msg}
	return st, clone(msg), nil
}

// Finish consumes the peer message and derives the session keys.
func (s *State) Finish(peer, aad []byte) (Keys, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return Keys{}, errors.New("pake: state already used")
	}
	s.used = true
	if len(peer) != 65 || peer[0] != 4 || hmac.Equal(peer, s.msg) {
		return Keys{}, ErrBadMessage
	}
	qx, qy := elliptic.Unmarshal(curve, peer) // checks the point is on the curve
	if qx == nil {
		return Keys{}, ErrBadMessage
	}
	// Remove the peer's mask: peer - w*(M or N); P-256 has cofactor 1.
	px, py := nx, ny
	if s.role == RoleB {
		px, py = mx, my
	}
	wx, wy := curve.ScalarMult(px, py, s.w)
	wy = new(big.Int).Sub(curve.Params().P, wy)
	ux, uy := curve.Add(qx, qy, wx, wy)
	kx, ky := curve.ScalarMult(ux, uy, s.scalar)
	if kx.Sign() == 0 && ky.Sign() == 0 {
		return Keys{}, ErrBadMessage
	}
	k := elliptic.Marshal(curve, kx, ky)
	pA, pB := s.msg, peer
	if s.role == RoleB {
		pA, pB = peer, s.msg
	}
	tt := transcript(s.idA, s.idB, pA, pB, k, s.w)
	sum := sha256.Sum256(tt)
	ke, ka := sum[:16], sum[16:]
	kc, err := hkdf.Key(sha256.New, ka, nil, "ConfirmationKeys"+string(aad), 32)
	if err != nil {
		return Keys{}, err
	}
	ca, cb := mac(kc[:16], tt), mac(kc[16:], tt)
	if s.role == RoleA {
		return Keys{Ke: ke, MyConfirm: ca, PeerConfirm: cb}, nil
	}
	return Keys{Ke: ke, MyConfirm: cb, PeerConfirm: ca}, nil
}

func transcript(fields ...[]byte) []byte {
	var out []byte
	for _, f := range fields {
		out = binary.LittleEndian.AppendUint64(out, uint64(len(f)))
		out = append(out, f...)
	}
	return out
}

func mac(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}
