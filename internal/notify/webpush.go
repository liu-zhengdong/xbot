package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"xbot/internal/fsutil"
)

const pushRecordSize = 4096

// Message is the payload a closed page's service worker displays.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag,omitempty"`
	Bot   string `json:"bot,omitempty"`
}

type vapidFile struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// PublicKey returns the VAPID public key, creating the key pair on first use.
func PublicKey(root string) (string, error) {
	k, err := loadVAPID(root)
	if err != nil {
		return "", err
	}
	return k.Public, nil
}

// Send posts one Web Push message. A 404 or 410 means the subscription is gone.
func Send(ctx context.Context, root string, sub Subscription, msg Message, client *http.Client) error {
	if err := checkSub(sub); err != nil {
		return err
	}
	msg.Body = trimPush(msg.Body)
	plain, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	keys, err := loadVAPID(root)
	if err != nil {
		return err
	}
	body, err := encryptPush(sub, plain)
	if err != nil {
		return err
	}
	aud, err := pushAud(sub.Endpoint)
	if err != nil {
		return err
	}
	priv, err := ecdsaFromScalar(keys.Private)
	if err != nil {
		return err
	}
	pub, err := b64dec(keys.Public)
	if err != nil {
		return err
	}
	tok, err := vapidToken(priv, pub, aud, time.Now().Add(12*time.Hour).Unix())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400")
	if msg.Tag != "" && len(msg.Tag) <= 32 {
		req.Header.Set("Topic", msg.Tag)
	}
	req.Header.Set("Authorization", "vapid t="+tok+",k="+keys.Public)
	client = publicClient(sub.Endpoint, client)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return &goneError{Code: res.StatusCode}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("notify: push status %d", res.StatusCode)
	}
	return nil
}

type goneError struct{ Code int }

func (e *goneError) Error() string { return fmt.Sprintf("notify: push status %d", e.Code) }

// Gone reports whether the push service dropped this subscription.
func Gone(err error) bool {
	var g *goneError
	return errors.As(err, &g)
}

func vapidPath(root string) string { return filepath.Join(root, "push", "vapid.json") }

func loadVAPID(root string) (vapidFile, error) {
	path := vapidPath(root)
	var out vapidFile
	err := fsutil.WithLock(path+".lock", func() error {
		raw, err := os.ReadFile(path)
		if err == nil {
			return json.Unmarshal(raw, &out)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		ec, err := ecdhFromECDSA(priv)
		if err != nil {
			return err
		}
		out = vapidFile{
			Public:  b64enc(ec.PublicKey().Bytes()),
			Private: b64enc(priv.D.FillBytes(make([]byte, 32))),
		}
		raw, err = fsutil.Marshal(out)
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(path, raw, 0600)
	})
	if err != nil {
		return vapidFile{}, err
	}
	if out.Public == "" || out.Private == "" {
		return vapidFile{}, errors.New("notify: empty vapid key")
	}
	return out, nil
}

func encryptPush(sub Subscription, plain []byte) ([]byte, error) {
	uaPub, err := b64dec(sub.P256dh)
	if err != nil {
		return nil, err
	}
	auth, err := b64dec(sub.Auth)
	if err != nil {
		return nil, err
	}
	curve := ecdh.P256()
	ua, err := curve.NewPublicKey(uaPub)
	if err != nil {
		return nil, err
	}
	as, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	cek, nonce, err := contentKey(auth, shared, uaPub, asPub, salt)
	if err != nil {
		return nil, err
	}
	need := pushRecordSize - 16
	if len(plain)+1 > need {
		return nil, errors.New("notify: push payload too large")
	}
	record := make([]byte, need)
	copy(record, plain)
	record[len(plain)] = 2
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, record, nil)
	out := make([]byte, 0, 16+4+1+len(asPub)+len(sealed))
	out = append(out, salt...)
	var rs [4]byte
	binary.BigEndian.PutUint32(rs[:], pushRecordSize)
	out = append(out, rs[:]...)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	out = append(out, sealed...)
	return out, nil
}

// decryptPush reverses encryptPush. uaPriv is the subscriber's scalar.
func decryptPush(body, uaPriv, auth []byte) ([]byte, error) {
	if len(body) < 16+4+1+16 {
		return nil, errors.New("notify: short push body")
	}
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idlen := int(body[20])
	if idlen == 0 || 21+idlen > len(body) {
		return nil, errors.New("notify: bad push key id")
	}
	asPub := body[21 : 21+idlen]
	sealed := body[21+idlen:]
	curve := ecdh.P256()
	priv, err := curve.NewPrivateKey(uaPriv)
	if err != nil {
		return nil, err
	}
	as, err := curve.NewPublicKey(asPub)
	if err != nil {
		return nil, err
	}
	shared, err := priv.ECDH(as)
	if err != nil {
		return nil, err
	}
	cek, nonce, err := contentKey(auth, shared, priv.PublicKey().Bytes(), asPub, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	record, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, err
	}
	if uint32(len(record)+16) != rs {
		return nil, errors.New("notify: push record size")
	}
	end := len(record)
	for end > 0 && record[end-1] == 0 {
		end--
	}
	if end == 0 || record[end-1] != 2 {
		return nil, errors.New("notify: push padding")
	}
	return record[:end-1], nil
}

func contentKey(auth, shared, uaPub, asPub, salt []byte) ([]byte, []byte, error) {
	info := append([]byte("WebPush: info\x00"), uaPub...)
	info = append(info, asPub...)
	ikm := hkdfExpand(hkdfExtract(auth, shared), info, 32)
	prk := hkdfExtract(salt, ikm)
	cek := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	return cek, nonce, nil
}

func hkdfExtract(salt, ikm []byte) []byte {
	mac := hmac.New(sha256.New, salt)
	mac.Write(ikm)
	return mac.Sum(nil)
}

func hkdfExpand(prk, info []byte, n int) []byte {
	var out, prev []byte
	var c byte = 1
	for len(out) < n {
		mac := hmac.New(sha256.New, prk)
		mac.Write(prev)
		mac.Write(info)
		mac.Write([]byte{c})
		prev = mac.Sum(nil)
		out = append(out, prev...)
		c++
	}
	return out[:n]
}

func vapidToken(priv *ecdsa.PrivateKey, pub []byte, aud string, exp int64) (string, error) {
	header := b64enc([]byte(`{"typ":"JWT","alg":"ES256"}`))
	payload, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{Aud: aud, Exp: exp, Sub: "mailto:xbot@localhost"})
	if err != nil {
		return "", err
	}
	input := header + "." + b64enc(payload)
	sig, err := signES256(priv, []byte(input))
	if err != nil {
		return "", err
	}
	return input + "." + b64enc(sig), nil
}

func signES256(priv *ecdsa.PrivateKey, input []byte) ([]byte, error) {
	sum := sha256.Sum256(input)
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return nil, err
	}
	half := new(big.Int).Rsh(elliptic.P256().Params().N, 1)
	if s.Cmp(half) == 1 {
		s = new(big.Int).Sub(elliptic.P256().Params().N, s)
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out, nil
}

func pushAud(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("notify: bad endpoint")
	}
	return u.Scheme + "://" + u.Host, nil
}

func ecdsaFromScalar(b64 string) (*ecdsa.PrivateKey, error) {
	d, err := b64dec(b64)
	if err != nil || len(d) != 32 {
		return nil, errors.New("notify: bad vapid key")
	}
	ec, err := ecdh.P256().NewPrivateKey(d)
	if err != nil {
		return nil, err
	}
	priv := new(ecdsa.PrivateKey)
	priv.PublicKey.Curve = elliptic.P256()
	priv.D = new(big.Int).SetBytes(d)
	priv.X, priv.Y = elliptic.Unmarshal(elliptic.P256(), ec.PublicKey().Bytes())
	if priv.X == nil {
		return nil, errors.New("notify: bad vapid key")
	}
	return priv, nil
}

func ecdhFromECDSA(priv *ecdsa.PrivateKey) (*ecdh.PrivateKey, error) {
	return ecdh.P256().NewPrivateKey(priv.D.FillBytes(make([]byte, 32)))
}

func b64enc(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func b64dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("notify: empty key")
	}
	return base64.RawURLEncoding.DecodeString(s)
}

func trimPush(s string) string {
	if len(s) <= 1800 {
		return s
	}
	s = s[:1800]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s + "…"
}
