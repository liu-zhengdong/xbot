// Package vault stores per-bot credentials encrypted at rest and injects them
// into outbound requests without ever showing values to the model.
//
// A vault is one directory. The directory is 0700 and every file in it is 0600.
// Secrets are serialized as JSON and encrypted with AES-GCM; the key is a local
// key file outside the bot workspace (normally <root>/keys/vault/<bot>.key), so
// a bot's own shell cannot read the key next to the ciphertext. Values never
// reach the process environment, the model prompt, or a tool result.
package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"xbot/internal/fsutil"
)

const (
	keyFile     = "key"
	secretsFile = "secrets.enc"
	keyLen      = 32
)

// legacyKeyFile is the old in-vault key name, migration source only.
const legacyKeyFile = keyFile

// Secret is one stored credential. Domains is the allow-list the value may be
// sent to. The value is never exposed to the model.
type Secret struct {
	Name    string   `json:"name"`
	Value   string   `json:"value"`
	Domains []string `json:"domains"`
}

type secretDisk struct {
	Secrets []Secret `json:"secrets"`
}

// Store is one bot's credential vault. It is safe for concurrent use.
type Store struct {
	dir     string
	keyPath string

	mu      sync.Mutex
	key     []byte
	secrets map[string]Secret
}

// Open loads the vault under dir, reading its key from keyPath. A missing
// vault is empty and no file is created until the first Set, so reading a bot
// does not leave state behind.
//
// Migration: when keyPath does not exist but the old key next to the
// ciphertext does, the old key is moved to keyPath under the vault file lock.
// A new key is never minted while an old one exists.
func Open(dir, keyPath string) (*Store, error) {
	s := &Store{dir: dir, keyPath: keyPath, secrets: map[string]Secret{}}
	key, err := readKey(keyPath)
	if err != nil {
		return nil, err
	}
	if key == nil {
		if err := s.migrateKey(); err != nil {
			return nil, err
		}
		if key, err = readKey(keyPath); err != nil {
			return nil, err
		}
	}
	s.key = key
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir is the vault directory.
func (s *Store) Dir() string { return s.dir }

// KeyPath is the vault key file, outside the vault directory.
func (s *Store) KeyPath() string { return s.keyPath }

// migrateKey moves the legacy in-vault key to s.keyPath. It runs under the
// vault file lock so two processes cannot both migrate; the second finds the
// new key already in place and leaves it alone.
func (s *Store) migrateKey() error {
	if !legacyKeyExists(s.dir) {
		return nil // no old key: nothing to migrate, nothing to create yet
	}
	return s.withFileLock(s.migrateKeyLocked)
}

// migrateKeyLocked is migrateKey with the vault file lock already held. The
// caller must not already hold s.mu.
func (s *Store) migrateKeyLocked() error {
	oldPath := filepath.Join(s.dir, legacyKeyFile)
	if _, err := os.Stat(oldPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	// Another process may have finished the migration while we waited.
	if _, err := os.Stat(s.keyPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// readKey validates the old key mode before we move it.
	if _, err := readKey(oldPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.keyPath), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.keyPath), 0700); err != nil {
		return err
	}
	if err := os.Rename(oldPath, s.keyPath); err == nil {
		return os.Chmod(s.keyPath, 0600)
	}
	// Cross-device rename: copy the bytes to the new 0600 file first, then
	// remove the old one. A crash between the two leaves both, and the next
	// open sees the new key and does not touch the old file.
	raw, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	if len(raw) != keyLen {
		return errors.New("vault: bad key file")
	}
	if err := fsutil.WriteAtomic(s.keyPath, raw, 0600); err != nil {
		return err
	}
	return os.Remove(oldPath)
}

// legacyKeyExists reports whether a pre-migration key sits next to the
// ciphertext.
func legacyKeyExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, legacyKeyFile))
	return err == nil
}

func readKey(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("vault: key file mode %v is too open; chmod 600 %s", info.Mode().Perm(), path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != keyLen {
		return nil, fmt.Errorf("vault: bad key file")
	}
	return raw, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(filepath.Join(s.dir, secretsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.key == nil {
		return errors.New("vault: secrets without key")
	}
	plain, err := decrypt(s.key, raw)
	if err != nil {
		return err
	}
	var disk secretDisk
	if err := json.Unmarshal(plain, &disk); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	for _, sec := range disk.Secrets {
		if strings.TrimSpace(sec.Name) == "" {
			continue
		}
		sec.Domains = normalizeDomains(sec.Domains)
		s.secrets[sec.Name] = sec
	}
	return nil
}

// Set stores value under name, replacing any previous binding. value must not
// be empty and at least one domain is required. The write is a locked
// read-modify-write, so a concurrent setter (another process) is kept.
func (s *Store) Set(name, value string, domains []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("vault: empty secret name")
	}
	if value == "" {
		return errors.New("vault: empty secret value")
	}
	ds := normalizeDomains(domains)
	if len(ds) == 0 {
		return errors.New("vault: at least one domain is required")
	}
	return s.withFileLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.reloadLocked(); err != nil {
			return err
		}
		if err := s.ensureKeyLocked(); err != nil {
			return err
		}
		s.secrets[name] = Secret{Name: name, Value: value, Domains: ds}
		return s.saveLocked()
	})
}

// Remove deletes one secret. A missing name is success.
func (s *Store) Remove(name string) error {
	name = strings.TrimSpace(name)
	if _, err := os.Stat(s.keyPath); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(s.dir, legacyKeyFile)); errors.Is(err, os.ErrNotExist) {
			return nil // no vault on disk yet: nothing to remove, nothing to create
		}
	}
	return s.withFileLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.reloadLocked(); err != nil {
			return err
		}
		if _, ok := s.secrets[name]; !ok {
			return nil
		}
		delete(s.secrets, name)
		return s.saveLocked()
	})
}

// withFileLock serializes writers across processes on one vault directory.
// The callback runs with the flock held; s.mu is taken inside, never around it.
func (s *Store) withFileLock(fn func() error) error {
	return fsutil.WithLock(filepath.Join(s.dir, secretsFile+".lock"), fn)
}

// reloadLocked re-reads the vault from disk so a concurrent writer that held
// the file lock before us is not silently overwritten (and its key adopted).
// The caller holds the vault file lock.
func (s *Store) reloadLocked() error {
	key, err := readKey(s.keyPath)
	if err != nil {
		return err
	}
	if key == nil && legacyKeyExists(s.dir) {
		// An older writer left the key next to the ciphertext; migrate it under
		// the lock we already hold instead of minting a second key.
		if err := s.migrateKeyLocked(); err != nil {
			return err
		}
		if key, err = readKey(s.keyPath); err != nil {
			return err
		}
	}
	if key != nil {
		s.key = key
	}
	s.secrets = map[string]Secret{}
	return s.load()
}

// List returns every secret sorted by name. The value is always blank: List is
// the model- and CLI-facing view, and a Backend.List must never leak a value.
// Use Lookup for egress.
func (s *Store) List() []Secret {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		sec.Value = ""
		sec.Domains = append([]string{}, sec.Domains...)
		out = append(out, sec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns the stored secret names sorted.
func (s *Store) Names() []string {
	list := s.List()
	out := make([]string, 0, len(list))
	for _, sec := range list {
		out = append(out, sec.Name)
	}
	return out
}

// Lookup returns one secret. The value is for egress only.
func (s *Store) Lookup(name string) (Secret, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec, ok := s.secrets[strings.TrimSpace(name)]
	return sec, ok
}

// Values returns a copy of the name/value pairs for masking.
func (s *Store) Values() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.secrets))
	for name, sec := range s.secrets {
		out[name] = sec.Value
	}
	return out
}

// Redact replaces every stored value in text with [secret], including the
// URL-escaped, JSON-escaped, and base64 forms a server or transcript may carry.
func (s *Store) Redact(text string) string {
	if text == "" {
		return text
	}
	for _, form := range s.valueForms() {
		text = strings.ReplaceAll(text, form, "[secret]")
	}
	return text
}

// valueForms collects every redactable form of every stored value, longest
// first so a form contained in another never leaves a tail.
func (s *Store) valueForms() []string {
	s.mu.Lock()
	values := make([]string, 0, len(s.secrets))
	for _, sec := range s.secrets {
		if sec.Value != "" {
			values = append(values, sec.Value)
		}
	}
	s.mu.Unlock()
	var forms []string
	for _, value := range values {
		forms = append(forms, redactForms(value)...)
	}
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// minEncodedValue is the shortest value whose encoded forms are matched.
// Shorter values are matched literally only; the base64 of a two-character
// value matches far too much unrelated text.
const minEncodedValue = 6

// minPartialMask is the shortest cut-off prefix masked at a truncation
// boundary; below this a leftover fragment is not worth the false positives.
const minPartialMask = 8

// redactForms returns the encodings under which value can travel: the literal
// text, its JSON string escapes (tool arguments and transcripts are JSON), its
// URL percent-escapes, and its base64 forms (a common way servers echo a
// credential).
func redactForms(value string) []string {
	forms := []string{value}
	if len(value) < minEncodedValue {
		return forms
	}
	add := func(form string) {
		if form == "" || form == value {
			return
		}
		for _, existing := range forms {
			if existing == form {
				return
			}
		}
		forms = append(forms, form)
	}
	add(jsonEscape(value, true))
	add(jsonEscape(value, false))
	add(url.QueryEscape(value))
	add(url.PathEscape(value))
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		add(enc.EncodeToString([]byte(value)))
	}
	return forms
}

func jsonEscape(s string, html bool) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(html)
	if err := enc.Encode(&s); err != nil {
		return ""
	}
	e := b.String()
	return e[1 : len(e)-2] // strip the quotes and trailing newline
}

// MaskTruncated hides the leading bytes of a value that a size clip has just
// cut off at the end of already redacted text; the first bytes of a
// credential are still sensitive.
func (s *Store) MaskTruncated(text string) string {
	if text == "" {
		return text
	}
	best := 0
	for _, form := range s.valueForms() {
		top := len(form) - 1 // a proper prefix only; full forms are Redact's job
		if top > len(text) {
			top = len(text)
		}
		for k := top; k >= minPartialMask; k-- {
			if strings.HasSuffix(text, form[:k]) {
				if k > best {
					best = k
				}
				break
			}
		}
	}
	if best == 0 {
		return text
	}
	return text[:len(text)-best] + "[secret]"
}

// ensureKeyLocked creates the key file on first write. The caller holds the
// vault file lock, so two processes cannot each mint a different key.
func (s *Store) ensureKeyLocked() error {
	if len(s.key) == keyLen {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0700); err != nil {
		return err
	}
	keyDir := filepath.Dir(s.keyPath)
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(keyDir, 0700); err != nil {
		return err
	}
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(s.keyPath, key, 0600); err != nil {
		return err
	}
	s.key = key
	return nil
}

// saveLocked persists the current secrets. The caller holds s.mu and the
// vault file lock.
func (s *Store) saveLocked() error {
	list := make([]Secret, 0, len(s.secrets))
	for _, sec := range s.secrets {
		sec.Domains = normalizeDomains(sec.Domains)
		list = append(list, sec)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	plain, err := json.Marshal(secretDisk{Secrets: list})
	if err != nil {
		return err
	}
	sealed, err := encrypt(s.key, plain)
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(filepath.Join(s.dir, secretsFile), sealed, 0600); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0700); err != nil {
		return err
	}
	return nil
}

func normalizeDomains(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, d := range in {
		d = normalizeDomain(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// normalizeDomain lowercases a binding and strips its dots. Only ASCII names
// are accepted: Unicode case folding maps some non-ASCII letters onto ASCII
// ones (for example the Kelvin sign onto "k"), which would let a lookalike
// domain match a binding while dialing a different name.
func normalizeDomain(d string) string {
	d = strings.TrimSpace(d)
	if d == "" || !isASCII(d) {
		return ""
	}
	d = strings.ToLower(d)
	d = strings.TrimPrefix(d, ".")
	d = strings.TrimSuffix(d, ".")
	if d == "" || strings.ContainsAny(d, "/\\@: \t{}") {
		return ""
	}
	return d
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func encrypt(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func decrypt(key, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("vault: ciphertext too short")
	}
	nonce, body := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, errors.New("vault: cannot decrypt")
	}
	return plain, nil
}

var (
	regMu     sync.RWMutex
	regStores []*Store
)

// Register adds a store to the process-wide redactor. It returns a function
// that removes it again. The running process registers every open vault so
// transcript reads and exports mask values.
func Register(s *Store) func() {
	if s == nil {
		return func() {}
	}
	regMu.Lock()
	regStores = append(regStores, s)
	regMu.Unlock()
	return func() {
		regMu.Lock()
		for i, other := range regStores {
			if other == s {
				regStores = append(regStores[:i], regStores[i+1:]...)
				break
			}
		}
		regMu.Unlock()
	}
}

// MaxRegisteredFormLen is the longest redactable form in any vault registered
// for process-wide redaction. It moves when a registered store changes.
func MaxRegisteredFormLen() int {
	regMu.RLock()
	defer regMu.RUnlock()
	best := 0
	for _, s := range regStores {
		if s == nil {
			continue
		}
		if n := s.MaxFormLen(); n > best {
			best = n
		}
	}
	return best
}

// Redact replaces known values across every registered vault. It is installed
// with bot.SetRedactText.
func Redact(text string) string {
	if text == "" {
		return text
	}
	regMu.RLock()
	stores := append([]*Store(nil), regStores...)
	regMu.RUnlock()
	for _, s := range stores {
		text = s.Redact(text)
	}
	return text
}
