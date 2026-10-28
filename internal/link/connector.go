package link

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

const (
	// TelegramAPI is the default Bot API origin. Tests pass their own origin to PostTelegram.
	TelegramAPI = "https://api.telegram.org"
	// LarkAPI is the default custom-bot origin. Tests pass their own origin to PostLark.
	LarkAPI = "https://open.feishu.cn"

	secretTelegram = "TELEGRAM_BOT_TOKEN"
	secretLark     = "LARK_APP_SECRET"

	draftStatusOpen = "draft"
	draftStatusSent = "sent"

	maxDraftTo   = 128
	maxDraftBody = 4096
)

// SecretName is the secrets.json key for a connector. Empty means it is not a connector.
func SecretName(kind string) string {
	switch kind {
	case NameTelegram:
		return secretTelegram
	case NameLark:
		return secretLark
	default:
		return ""
	}
}

// Poster delivers one approved draft. secret is the connector credential.
type Poster func(ctx context.Context, to, body, secret string) error

// Connector is the Telegram or Lark grant. Its tools register behind the tools door.
// Dir is the bot directory. SecretPath is the legacy plaintext secrets.json.
// Vault holds the credential; it is the only place a token is stored.
type Connector struct {
	Dir        string
	SecretPath string
	Kind       string
	// Vault is the credential backend. When set, the token lives there and
	// every request carries the {{secret:NAME}} placeholder instead of a value.
	Vault vault.Backend
	// Post replaces the default HTTP send. Nil uses TelegramAPI or LarkAPI.
	Post Poster
	// Client replaces the default outbound client. Nil builds one whose
	// Transport is Vault.Transport, so placeholders are filled at egress.
	Client *http.Client
}

// NewConnector returns a connector link for kind telegram or lark.
func NewConnector(dir, secretPath, kind string) *Connector {
	return &Connector{Dir: dir, SecretPath: secretPath, Kind: kind}
}

// UseVault attaches the credential backend and migrates any legacy plaintext
// token into it. It is the first-access hook: callers mount the connector and
// then call UseVault once, so a bot that never touches the connector keeps its
// file until it is needed.
func (c *Connector) UseVault(backend vault.Backend) error {
	if c == nil {
		return nil
	}
	if err := MigrateConnectorSecrets(c.SecretPath, backend); err != nil {
		return err
	}
	c.Vault = backend
	return nil
}

// client returns the HTTP client every outbound connector request uses. The
// transport injects placeholders at egress; a nil vault leaves the default.
func (c *Connector) client() *http.Client {
	if c != nil && c.Client != nil {
		return c.Client
	}
	if c == nil || c.Vault == nil {
		return connectorClient
	}
	base := connectorClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	clone := *connectorClient
	clone.Transport = c.Vault.Transport(base)
	return &clone
}

// token returns the value handed to Poster: the placeholder when a vault holds
// the credential, the legacy file value otherwise. The placeholder is only
// meaningful with a vault-attached client; without one, sending stops here.
func (c *Connector) token() (string, error) {
	if c == nil {
		return "", errors.New("connector: no connector")
	}
	if c.Vault != nil {
		if ph := Placeholder(c.Kind); ph != "" {
			return ph, nil
		}
	}
	return connectorSecret(c.SecretPath, c.Kind)
}

func (c *Connector) Name() string {
	if c == nil {
		return ""
	}
	return c.Kind
}

func (c *Connector) Tools() []tools.Tool {
	if c == nil || SecretName(c.Kind) == "" {
		return nil
	}
	return []tools.Tool{&sendTool{c: c}}
}

type sendTool struct{ c *Connector }

func (t *sendTool) Name() string { return t.c.Kind + "_send" }
func (t *sendTool) Description() string {
	return "Draft a message on the owner's behalf. It is saved and shown to the owner, and it is not sent until the owner confirms. A refusal leaves the draft unsent."
}
func (t *sendTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to":   map[string]any{"type": "string", "description": "chat id the owner is sending to"},
			"body": map[string]any{"type": "string", "description": "message text"},
		},
		"required":             []string{"to", "body"},
		"additionalProperties": false,
	}
}

// Confirm only saves the draft and builds the card summary. The kernel review
// path decides that an external send needs the owner.
func (t *sendTool) Confirm(args json.RawMessage) (bool, string, error) {
	to, body, err := parseSend(args)
	if err != nil {
		return false, "", err
	}
	d, err := saveDraft(t.c.Dir, t.c.Kind, to, body)
	if err != nil {
		return false, "", err
	}
	return false, draftPreview(t.c.Kind, d), nil
}

func (t *sendTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	to, body, err := parseSend(args)
	if err != nil {
		return "", err
	}
	d, err := openDraft(t.c.Dir, t.c.Kind, to, body)
	if err != nil {
		return "", err
	}
	if d.Status == draftStatusSent {
		return "sent " + d.ID, nil
	}
	secret, err := t.c.token()
	if err != nil {
		return "", err
	}
	if err := t.c.send(ctx, to, body, secret); err != nil {
		return "", scrubErr(err, secret)
	}
	if err := markDraftSent(t.c.Dir, d.ID); err != nil {
		return "", scrubErr(err, secret)
	}
	return "sent " + d.ID, nil
}

func (c *Connector) send(ctx context.Context, to, body, secret string) error {
	if c != nil && c.Post != nil {
		return c.Post(ctx, to, body, secret)
	}
	client := c.client()
	switch c.Kind {
	case NameTelegram:
		return postTelegramWith(ctx, client, TelegramAPI, secret, to, body)
	case NameLark:
		return postLarkWith(ctx, client, LarkAPI, secret, to, body)
	default:
		return errors.New("connector: unknown")
	}
}

func parseSend(args json.RawMessage) (string, string, error) {
	var in struct {
		To   string `json:"to"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", "", err
	}
	to := strings.TrimSpace(in.To)
	body := strings.TrimSpace(in.Body)
	if to == "" || body == "" {
		return "", "", errors.New("connector: to and body are required")
	}
	if len(to) > maxDraftTo || len(body) > maxDraftBody {
		return "", "", errors.New("connector: message is too long")
	}
	return to, body, nil
}

func draftPreview(kind string, d Draft) string {
	body := d.Body
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	return fmt.Sprintf("send %s draft %s to %s\n%s", kind, d.ID, d.To, body)
}

// Draft is one outbound message that has not been sent, or has been sent after approval.
type Draft struct {
	ID      string    `json:"id"`
	Link    string    `json:"link"`
	To      string    `json:"to"`
	Body    string    `json:"body"`
	Status  string    `json:"status"`
	Created time.Time `json:"created"`
	Sent    time.Time `json:"sent,omitempty"`
}

func draftDir(dir string) string {
	return filepath.Join(dir, "drafts")
}

func draftPath(dir, id string) string {
	return filepath.Join(draftDir(dir), id+".json")
}

func pointerPath(dir, kind, to, body string) string {
	sum := sha256.Sum256([]byte(kind + "\n" + to + "\n" + body))
	return filepath.Join(draftDir(dir), "open-"+kind+"-"+hex.EncodeToString(sum[:8])+".json")
}

func saveDraft(dir, kind, to, body string) (Draft, error) {
	if dir == "" {
		return Draft{}, errors.New("connector: no directory")
	}
	var out Draft
	err := fsutil.WithLock(filepath.Join(draftDir(dir), ".lock"), func() error {
		if cur, err := readPointer(dir, kind, to, body); err == nil && cur.Status == draftStatusOpen && cur.To == to && cur.Body == body {
			out = cur
			return nil
		}
		id, err := newDraftID()
		if err != nil {
			return err
		}
		out = Draft{
			ID: id, Link: kind, To: to, Body: body,
			Status: draftStatusOpen, Created: time.Now().UTC(),
		}
		if err := writeDraft(dir, out); err != nil {
			return err
		}
		raw, err := fsutil.Marshal(struct {
			ID string `json:"id"`
		}{ID: id})
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(pointerPath(dir, kind, to, body), raw, 0600)
	})
	return out, err
}

func openDraft(dir, kind, to, body string) (Draft, error) {
	if dir == "" {
		return Draft{}, errors.New("connector: no directory")
	}
	var out Draft
	err := fsutil.WithLock(filepath.Join(draftDir(dir), ".lock"), func() error {
		d, err := readPointer(dir, kind, to, body)
		if err != nil {
			return err
		}
		if d.To != to || d.Body != body || d.Link != kind {
			return errors.New("connector: draft does not match")
		}
		out = d
		return nil
	})
	return out, err
}

func markDraftSent(dir, id string) error {
	return fsutil.WithLock(filepath.Join(draftDir(dir), ".lock"), func() error {
		d, err := readDraft(dir, id)
		if err != nil {
			return err
		}
		if d.Status == draftStatusSent {
			return nil
		}
		d.Status = draftStatusSent
		d.Sent = time.Now().UTC()
		return writeDraft(dir, d)
	})
}

func readPointer(dir, kind, to, body string) (Draft, error) {
	raw, err := os.ReadFile(pointerPath(dir, kind, to, body))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Draft{}, errors.New("connector: no draft")
		}
		return Draft{}, err
	}
	var ptr struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &ptr); err != nil {
		return Draft{}, err
	}
	return readDraft(dir, ptr.ID)
}

func readDraft(dir, id string) (Draft, error) {
	if !draftID(id) {
		return Draft{}, errors.New("connector: bad draft id")
	}
	raw, err := os.ReadFile(draftPath(dir, id))
	if err != nil {
		return Draft{}, err
	}
	var d Draft
	if err := json.Unmarshal(raw, &d); err != nil {
		return Draft{}, err
	}
	if d.ID != id {
		return Draft{}, errors.New("connector: broken draft")
	}
	return d, nil
}

func writeDraft(dir string, d Draft) error {
	raw, err := fsutil.Marshal(d)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(draftPath(dir, d.ID), raw, 0600)
}

func newDraftID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "d_" + hex.EncodeToString(buf[:]), nil
}

func draftID(id string) bool {
	if len(id) != 18 || !strings.HasPrefix(id, "d_") {
		return false
	}
	for _, r := range id[2:] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func connectorSecret(path, kind string) (string, error) {
	name := SecretName(kind)
	if path == "" || name == "" {
		return "", errors.New("connector: secret is missing")
	}
	vals, err := ReadSecrets(path)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(vals[name])
	if secret == "" {
		return "", errors.New("connector: secret is missing")
	}
	return secret, nil
}

// PostTelegram calls sendMessage with the default client. token is scrubbed from any error.
func PostTelegram(ctx context.Context, base, token, to, body string) error {
	return postTelegramWith(ctx, connectorClient, base, token, to, body)
}

// PostLark calls a custom-bot webhook with the default client. token is the
// hook secret and is scrubbed from any error.
func PostLark(ctx context.Context, base, token, to, body string) error {
	return postLarkWith(ctx, connectorClient, base, token, to, body)
}

// postTelegramWith calls sendMessage through client. A vault-attached client
// replaces the placeholder in the path just before egress.
func postTelegramWith(ctx context.Context, client *http.Client, base, token, to, body string) error {
	rawURL, err := connectorURL(base, "/bot"+token+"/sendMessage", token)
	if err != nil {
		return err
	}
	return doJSON(ctx, client, rawURL, token, map[string]string{"chat_id": to, "text": body}, true)
}

// postLarkWith calls a custom-bot webhook through client.
func postLarkWith(ctx context.Context, client *http.Client, base, token, to, body string) error {
	rawURL, err := connectorURL(base, "/open-apis/bot/v2/hook/"+token, token)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": body},
	}
	return doJSON(ctx, client, rawURL, token, payload, false)
}

func connectorURL(base, suffix, token string) (string, error) {
	if !safeToken(token) {
		return "", errors.New("connector: bad secret")
	}
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", errors.New("connector: bad endpoint")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/") + suffix
	return u.String(), nil
}

func safeToken(token string) bool {
	if token == "" || len(token) > 256 {
		return false
	}
	if strings.HasPrefix(token, "{{secret:") && strings.HasSuffix(token, "}}") {
		name := token[len("{{secret:") : len(token)-len("}}")]
		return vaultSecretName(name)
	}
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ':' || r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}

// vaultSecretName is the character class vault's placeholder regexp accepts.
func vaultSecretName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}

var connectorClient = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func postJSON(ctx context.Context, rawURL, secret string, payload any, telegram bool) error {
	return doJSON(ctx, connectorClient, rawURL, secret, payload, telegram)
}

func doJSON(ctx context.Context, client *http.Client, rawURL, secret string, payload any, telegram bool) error {
	if client == nil {
		client = connectorClient
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(raw))
	if err != nil {
		return scrubErr(err, secret)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return scrubErr(err, secret)
	}
	defer res.Body.Close()
	slurp, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return scrubErr(err, secret)
	}
	text := scrubText(string(slurp), secret)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("connector: %d %s", res.StatusCode, text)
	}
	if telegram {
		var out struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
		}
		if json.Unmarshal(slurp, &out) == nil && !out.OK {
			return fmt.Errorf("connector: %s", scrubText(out.Description, secret))
		}
		return nil
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(slurp, &out) == nil && out.Code != 0 {
		return fmt.Errorf("connector: %s", scrubText(out.Msg, secret))
	}
	return nil
}

func scrubErr(err error, secret string) error {
	if err == nil {
		return nil
	}
	return errors.New(scrubText(err.Error(), secret))
}

func scrubText(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[secret]")
}
