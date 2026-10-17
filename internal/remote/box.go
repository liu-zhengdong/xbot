package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"xbot/internal/gateway"
)

const boxDomain = "xbot-box-v1"

type boxBody struct {
	Ephemeral  []byte `json:"e"`
	Nonce      []byte `json:"n"`
	Ciphertext []byte `json:"c"`
}

// boxAAD keeps the G4 form for kind box so queued G4 envelopes and G4 peers
// still decrypt; every other kind appends itself, so relabeling fails both ways.
func boxAAD(env gateway.Envelope) []byte {
	aad := boxDomain + "|" + env.ID + "|" + env.From + "|" + env.To
	if env.Kind != "box" {
		aad += "|" + env.Kind
	}
	return []byte(aad)
}

func boxAEAD(priv *ecdh.PrivateKey, pub *ecdh.PublicKey, ephemeral, recipient []byte) (cipher.AEAD, error) {
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, err
	}
	salt := append(append([]byte{}, ephemeral...), recipient...)
	key, err := hkdf.Key(sha256.New, shared, salt, boxDomain, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// newBoxEnvelope sets the authenticated fields before sealing, then signs.
func newBoxEnvelope(priv ed25519.PrivateKey, d Device, kind string, plaintext []byte, now time.Time) (gateway.Envelope, error) {
	var env gateway.Envelope
	if len(plaintext) > gateway.MaxBody {
		return env, errors.New("remote: body exceeds 64 KiB")
	}
	raw, err := base64.StdEncoding.DecodeString(d.XKey)
	if err != nil {
		return env, err
	}
	recipient, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return env, err
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return env, err
	}
	aead, err := boxAEAD(eph, recipient, eph.PublicKey().Bytes(), raw)
	if err != nil {
		return env, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return env, err
	}
	env = gateway.Envelope{V: 1, ID: hex.EncodeToString(id[:]), From: gateway.ID(priv.Public().(ed25519.PublicKey)), To: d.ID, Time: now.UTC(), Kind: kind}
	b := boxBody{Ephemeral: eph.PublicKey().Bytes(), Nonce: make([]byte, aead.NonceSize())}
	if _, err := rand.Read(b.Nonce); err != nil {
		return env, err
	}
	b.Ciphertext = aead.Seal(nil, b.Nonce, plaintext, boxAAD(env))
	env.Body, err = json.Marshal(b)
	if err != nil {
		return env, err
	}
	env.Sign(priv)
	// The relay bounds the entire JSON request, including both base64 layers.
	wire, err := json.Marshal(env)
	if err == nil && len(wire) > gateway.MaxBody {
		err = errors.New("remote: envelope exceeds 64 KiB")
	}
	return env, err
}

func openBox(priv *ecdh.PrivateKey, env gateway.Envelope) ([]byte, error) {
	var b boxBody
	if len(env.Body) > gateway.MaxBody || json.Unmarshal(env.Body, &b) != nil || len(b.Ephemeral) != 32 || len(b.Nonce) != 12 {
		return nil, errors.New("remote: invalid box")
	}
	pub, err := ecdh.X25519().NewPublicKey(b.Ephemeral)
	if err != nil {
		return nil, err
	}
	aead, err := boxAEAD(priv, pub, b.Ephemeral, priv.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, b.Nonce, b.Ciphertext, boxAAD(env))
}
