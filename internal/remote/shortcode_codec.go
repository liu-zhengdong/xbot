package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"xbot/internal/gateway"
	"xbot/internal/pake"
)

// G7 short codes: 12 characters from gateway.CodeAlphabet, shown XXXX-XXXX-XXXX.
// Characters 1-4 are the lookup the gateway sees, 5-11 the secret the two
// installs run SPAKE2 over (never sent), 12 a check character so B catches a
// typo locally without spending one of A's five sessions.

const pairV2 = "xbot-pair-v2"

var ErrBadCode = errors.New("remote: invalid short code")

// checkChar is Luhn mod N, starting with a doubled rightmost data digit.
func checkChar(data string) byte {
	sum, factor, n := 0, 2, len(gateway.CodeAlphabet)
	for i := len(data) - 1; i >= 0; i-- {
		v := strings.IndexByte(gateway.CodeAlphabet, data[i]) * factor
		sum += v/n + v%n
		factor = 3 - factor
	}
	return gateway.CodeAlphabet[(n-sum%n)%n]
}

// ParseShortCode finds the code in pasted text: twelve code characters made of
// consecutive ASCII words (any other character separates words), so prose
// around it ("Code: 2345-6789-ABC7") is never folded into the code.
func ParseShortCode(s string) (lookup, secret string, err error) {
	if len(s) > codeMax {
		return "", "", ErrBadCode
	}
	words := strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool {
		return !('A' <= r && r <= 'Z' || '0' <= r && r <= '9')
	})
	for i := range words {
		code := ""
		for _, w := range words[i:] {
			if strings.Trim(w, gateway.CodeAlphabet) != "" || len(code)+len(w) > 12 {
				break
			}
			if code += w; len(code) == 12 && checkChar(code[:11]) == code[11] {
				return code[:4], code[4:11], nil
			}
		}
	}
	return "", "", ErrBadCode
}

func randomShortCode() (string, error) {
	b := make([]byte, 11)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(gateway.CodeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = gateway.CodeAlphabet[n.Int64()]
	}
	return string(b) + string(checkChar(string(b))), nil
}
func displayCode(s string) string { return s[:4] + "-" + s[4:8] + "-" + s[8:] }

type pairInfo struct {
	Name string   `json:"name"`
	OS   string   `json:"os"`
	Bots []string `json:"bots"`
	// Deadline (A to B only, unix seconds) is when A drops the session, so B
	// never offers 连接 after A would ignore it.
	Deadline int64 `json:"deadline,omitempty"`
}

// Keys come from SPAKE2 (internal/pake). pairAAD binds both installs' signing
// keys and xkeys and the lookup into the key confirmation, so a relaying
// gateway cannot substitute either side's keys; seals bind the envelope header.
func pairAAD(lookup, bk, bx, ak, ax string) []byte {
	return []byte(pairV2 + "|" + lookup + "|" + bk + "|" + bx + "|" + ak + "|" + ax)
}
func sealAAD(e gateway.Envelope) []byte {
	return []byte(pairV2 + "|" + e.ID + "|" + e.From + "|" + e.To + "|" + e.Kind)
}
func pairAEAD(keys pake.Keys) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, keys.Ke, nil, pairV2+" seal", 32)
	if err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
func sealPair(keys pake.Keys, e gateway.Envelope, v any) (json.RawMessage, error) {
	a, err := pairAEAD(keys)
	if err != nil {
		return nil, err
	}
	n := make([]byte, a.NonceSize())
	if _, err = rand.Read(n); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		N []byte `json:"n"`
		C []byte `json:"c"`
	}{n, a.Seal(nil, n, raw, sealAAD(e))})
}
func openPair(keys pake.Keys, e gateway.Envelope, raw []byte, v any) error {
	var b struct {
		N []byte `json:"n"`
		C []byte `json:"c"`
	}
	if json.Unmarshal(raw, &b) != nil || len(b.N) != 12 {
		return ErrBadCode
	}
	a, err := pairAEAD(keys)
	if err != nil {
		return err
	}
	plain, err := a.Open(nil, b.N, b.C, sealAAD(e))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, v)
}
