package pipe

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
)

// The end-to-end layer. After the hub says ready, each side sends a fresh
// X25519 public key as its first binary message. Both keys and the ticket
// secret feed HKDF-SHA256, which gives one AES-256-GCM key per direction;
// nonces are message counters, so a dropped, replayed or reordered frame fails
// to open. The hub relays the public keys but holds no private key, so its
// logs and memory never yield the session key. (The hub does see the ticket
// secret it issued, so an actively malicious hub could still sit in the
// middle; batch 1 trusts the hub not to tamper, only not to read.)

type sealer struct {
	send, recv cipher.AEAD
	sn, rn     uint64
}

func (s *sealer) seal(v any) ([]byte, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := s.send.Seal(nil, counterNonce(s.sn), plain, nil)
	s.sn++
	return out, nil
}

func (s *sealer) open(msg []byte, v any) error {
	plain, err := s.recv.Open(nil, counterNonce(s.rn), msg, nil)
	if err != nil {
		return errors.New("pipe: frame failed to open")
	}
	s.rn++
	return json.Unmarshal(plain, v)
}

func counterNonce(n uint64) []byte {
	return binary.BigEndian.AppendUint64(make([]byte, 4, 12), n)
}

// handshake runs right after ready. entry says which side this is.
func handshake(c *wsConn, secret string, entry bool) (*sealer, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := c.WriteMessage(opBinary, priv.PublicKey().Bytes()); err != nil {
		return nil, err
	}
	op, peerRaw, err := c.ReadMessage()
	if err != nil {
		return nil, err
	}
	if op != opBinary {
		return nil, errors.New("pipe: bad handshake")
	}
	peer, err := ecdh.X25519().NewPublicKey(peerRaw)
	if err != nil {
		return nil, errors.New("pipe: bad handshake")
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, errors.New("pipe: bad handshake")
	}
	ePub, dPub := priv.PublicKey().Bytes(), peerRaw
	if !entry {
		ePub, dPub = dPub, ePub
	}
	salt := sha256.Sum256([]byte("xbot-pipe-v1|" + secret))
	key := func(dir string) (cipher.AEAD, error) {
		info := "xbot-pipe-v1 " + dir + "|" + string(ePub) + string(dPub)
		k, err := hkdf.Key(sha256.New, shared, salt[:], info, 32)
		if err != nil {
			return nil, err
		}
		block, err := aes.NewCipher(k)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	}
	e2d, err := key("entry->device")
	if err != nil {
		return nil, err
	}
	d2e, err := key("device->entry")
	if err != nil {
		return nil, err
	}
	if entry {
		return &sealer{send: e2d, recv: d2e}, nil
	}
	return &sealer{send: d2e, recv: e2d}, nil
}

// frame is the plaintext inside a sealed message.
type frame struct {
	T      string `json:"t"` // req | res
	ID     string `json:"id"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Status int    `json:"status,omitempty"`
	Body   []byte `json:"body,omitempty"`
	CT     string `json:"ct,omitempty"`
}
