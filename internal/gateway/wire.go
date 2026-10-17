// Package gateway implements the signed mailbox relay.
package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

// Limits are the hard bounds of the relay.
const (
	MaxBody       = 64 << 10
	MaxQueue      = 512
	MaxQueueBytes = 8 << 20
	TTL           = 7 * 24 * time.Hour
	MaxPoll       = 32
	MaxPollBytes  = 768 << 10
	AckTTL        = 10 * time.Minute
	MaxWait       = 30 * time.Second
	Skew          = 5 * time.Minute
	MaxInstalls   = 64
	MaxAck        = 64
)

// LoadOrCreateKey serializes reading or creating a seed with a filesystem lock.
func LoadOrCreateKey(path string) (key ed25519.PrivateKey, err error) {
	err = fsutil.WithLock(path+".lock", func() error {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			_, key, err = ed25519.GenerateKey(rand.Reader)
			if err == nil {
				err = fsutil.WriteAtomic(path, []byte(hex.EncodeToString(key.Seed())+"\n"), 0600)
			}
			return err
		}
		if err != nil {
			return err
		}
		seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return errors.New("gateway: invalid key seed")
		}
		key = ed25519.NewKeyFromSeed(seed)
		return nil
	})
	return key, err
}

// ID is the install id: the first 16 bytes of sha256(pub) as lowercase hex.
func ID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:16])
}

// Envelope is one relayed message. Body and Sig are base64 in JSON.
type Envelope struct {
	V    int       `json:"v"`
	ID   string    `json:"id"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Body []byte    `json:"body"`
	Sig  []byte    `json:"sig"`
}

func envelopeBlob(e Envelope) []byte {
	sum := sha256.Sum256(e.Body)
	return []byte(fmt.Sprintf("xbot-env-v1\n%d|%s|%s|%s|%s|%s|%x", e.V, e.ID, e.From, e.To, e.Time.UTC().Format(time.RFC3339Nano), e.Kind, sum))
}

// Sign sets Sig over the envelope fields and body.
func (e *Envelope) Sign(priv ed25519.PrivateKey) {
	e.Sig = ed25519.Sign(priv, envelopeBlob(*e))
}

// Verify checks Sig against pub. A wrong or malformed key never panics.
func (e Envelope) Verify(pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, envelopeBlob(e), e.Sig) {
		return errors.New("gateway: bad envelope signature")
	}
	return nil
}

// NewEnvelope builds and signs v1 envelope with a fresh random id.
func NewEnvelope(priv ed25519.PrivateKey, to, kind string, body []byte, now time.Time) Envelope {
	var b [16]byte
	_, _ = rand.Read(b[:])
	e := Envelope{V: 1, ID: hex.EncodeToString(b[:]), From: ID(priv.Public().(ed25519.PublicKey)), To: to, Time: now.UTC(), Kind: kind, Body: body}
	e.Sign(priv)
	return e
}

// SignRequest authenticates the method, URI, time and body hash.
func SignRequest(req *http.Request, priv ed25519.PrivateKey, body []byte, now time.Time) {
	secs := now.Unix()
	pub := priv.Public().(ed25519.PublicKey)
	req.Header.Set("X-Xbot-Key", base64.RawURLEncoding.EncodeToString(pub))
	req.Header.Set("X-Xbot-Time", strconv.FormatInt(secs, 10))
	req.Header.Set("X-Xbot-Sig", base64.RawURLEncoding.EncodeToString(
		ed25519.Sign(priv, requestBlob(req, secs, body))))
}

// VerifyRequest checks request authentication and clock skew.
func VerifyRequest(r *http.Request, body []byte, now time.Time, skew time.Duration) (ed25519.PublicKey, error) {
	pub, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Xbot-Key"))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("gateway: bad key")
	}
	secs, err := strconv.ParseInt(r.Header.Get("X-Xbot-Time"), 10, 64)
	if err != nil {
		return nil, errors.New("gateway: bad time")
	}
	if d := now.Sub(time.Unix(secs, 0)); d > skew || d < -skew {
		return nil, fmt.Errorf("gateway: bad time, server time %s", now.UTC().Format(time.RFC3339))
	}
	sig, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Xbot-Sig"))
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(pub), requestBlob(r, secs, body), sig) {
		return nil, errors.New("gateway: bad signature")
	}
	return ed25519.PublicKey(pub), nil
}

func requestBlob(r *http.Request, secs int64, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(fmt.Sprintf("xbot-gw-v1\n%s\n%s\n%d\n%x", r.Method, r.URL.RequestURI(), secs, sum))
}
