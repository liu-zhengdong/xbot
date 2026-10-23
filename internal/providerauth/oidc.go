package providerauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

func validateID(ctx context.Context, a *attempt, token string) (string, error) {
	bad := errors.New("ChatGPT 身份令牌校验失败")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 32<<10 {
		return "", bad
	}
	decode := base64.RawURLEncoding.DecodeString
	header, err := decode(parts[0])
	if err != nil {
		return "", bad
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "RS256" || h.Kid == "" {
		return "", bad
	}
	r, err := http.NewRequestWithContext(ctx, "GET", a.endpoints.JWKS, nil)
	if err != nil {
		return "", bad
	}
	res, err := authHTTP().Do(r)
	if err != nil {
		return "", bad
	}
	defer res.Body.Close()
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&jwks) != nil {
		return "", bad
	}
	sig, err := decode(parts[2])
	if err != nil {
		return "", bad
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	verified := false
	for _, k := range jwks.Keys {
		if k.Kid != h.Kid || k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, e := new(big.Int), new(big.Int)
		nb, err := decode(k.N)
		if err != nil {
			continue
		}
		eb, err := decode(k.E)
		if err != nil {
			continue
		}
		n.SetBytes(nb)
		e.SetBytes(eb)
		if n.BitLen() < 2048 || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
			continue
		}
		if rsa.VerifyPKCS1v15(&rsa.PublicKey{N: n, E: int(e.Int64())}, crypto.SHA256, digest[:], sig) == nil {
			verified = true
			break
		}
	}
	if !verified {
		return "", bad
	}
	payload, err := decode(parts[1])
	if err != nil {
		return "", bad
	}
	var c struct {
		Issuer   string          `json:"iss"`
		Subject  string          `json:"sub"`
		Audience json.RawMessage `json:"aud"`
		Expires  int64           `json:"exp"`
		Nonce    string          `json:"nonce"`
		AZP      string          `json:"azp"`
	}
	if json.Unmarshal(payload, &c) != nil || c.Issuer != a.endpoints.Issuer || c.Subject == "" || c.Expires <= time.Now().Unix() || c.Nonce != a.nonce {
		return "", bad
	}
	var single string
	var audiences []string
	if json.Unmarshal(c.Audience, &single) == nil {
		audiences = []string{single}
	} else if json.Unmarshal(c.Audience, &audiences) != nil {
		return "", bad
	}
	if len(audiences) > 1 && c.AZP != a.clientID {
		return "", bad
	}
	for _, aud := range audiences {
		if aud == a.clientID {
			return c.Subject, nil
		}
	}
	return "", bad
}
