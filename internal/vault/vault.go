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
	// formLen is the cached longest redactable form. formGen bumps on every
	// change so callers can keep their own cache and notice a vault write.
	formLen   int
	formLenOK bool
	formGen   uint64
	// cache is rebuilt after formGen moves.
	cache   formCache
	formsOK bool
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
	defer s.invalidateForms()
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
		s.invalidateForms()
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
		s.invalidateForms()
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
// It is one left-to-right pass: at each position the longest form wins, and
// an inserted [secret] is never scanned again.
func (s *Store) Redact(text string) string {
	if text == "" {
		return text
	}
	c := s.cachedForms()
	if c.repl == nil {
		return text
	}
	return redactWrapped(c.repl.Replace(text), c.wrap)
}

// Forms returns every redactable form of every stored value, longest first,
// equal lengths in byte order.
func (s *Store) Forms() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.valueForms()...)
}

// valueForms is the cached form list. Callers must not modify it.
func (s *Store) valueForms() []string {
	return s.cachedForms().forms
}

// formCache is everything Redact needs, computed once per vault generation.
type formCache struct {
	forms []string          // longest first, so a form inside another never leaves a tail
	repl  *strings.Replacer // one pass over forms
	wrap  []string          // base64-alphabet forms worth looking for across line breaks
}

func (s *Store) cachedForms() formCache {
	s.mu.Lock()
	if s.formsOK {
		c := s.cache
		s.mu.Unlock()
		return c
	}
	g := s.formGen
	values := make([]string, 0, len(s.secrets))
	for _, sec := range s.secrets {
		if sec.Value != "" {
			values = append(values, sec.Value)
		}
	}
	s.mu.Unlock()
	seen := map[string]struct{}{}
	var forms []string
	for _, value := range values {
		for _, form := range redactForms(value) {
			if _, ok := seen[form]; ok {
				continue
			}
			seen[form] = struct{}{}
			forms = append(forms, form)
		}
	}
	sortForms(forms)
	var repl *strings.Replacer
	if len(forms) > 0 {
		pairs := make([]string, 0, 2*len(forms))
		for _, form := range forms {
			pairs = append(pairs, form, "[secret]")
		}
		repl = strings.NewReplacer(pairs...)
	}
	c := formCache{forms: forms, repl: repl}
	for _, form := range forms {
		if len(form) >= minStreamForm && base64Line(form) {
			c.wrap = append(c.wrap, form)
		}
	}
	s.mu.Lock()
	if s.formGen == g {
		s.cache, s.formsOK = c, true
	}
	s.mu.Unlock()
	return c
}

// sortForms orders longest first, then by bytes, so equal-length forms come
// out the same way every time.
func sortForms(forms []string) {
	sort.Slice(forms, func(i, j int) bool {
		if len(forms[i]) != len(forms[j]) {
			return len(forms[i]) > len(forms[j])
		}
		return forms[i] < forms[j]
	})
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
	if c, ok := oauthValue(value); ok {
		forms := []string{value}
		for _, token := range []string{c.AccessToken, c.RefreshToken, c.IDToken} {
			if token != "" {
				forms = append(forms, redactForms(token)...)
			}
		}
		return forms
	}
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
	// Inside a longer base64 stream (Basic auth "user:token", a JSON blob)
	// the value starts at any offset in a 3-byte group, and its last group
	// mixes with what follows. The whole groups in between encode the same
	// for a given alignment, so each alignment's middle is a form too.
	for skip := 0; skip < 3; skip++ {
		whole := (len(value) - skip) / 3 * 3
		if whole <= 0 {
			continue
		}
		mid := []byte(value[skip : skip+whole])
		for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
			if form := enc.EncodeToString(mid); len(form) >= minStreamForm {
				add(form)
			}
		}
	}
	return forms
}

// minStreamForm is the shortest unaligned base64 middle that is matched.
const minStreamForm = 12

// minWrapLine is the shortest line width taken as wrapped base64.
const minWrapLine = 16

// redactWrapped replaces forms that a base64 tool split into lines of equal
// width (76 for MIME, 64 for PEM, any -w). A block is two or more lines of
// base64 characters, all but the last the same width and the last no wider.
// The replaced range keeps the line breaks it spanned out of the output.
func redactWrapped(text string, forms []string) string {
	if len(forms) == 0 || strings.Count(text, "\n") == 0 {
		return text
	}
	type line struct{ start, end int } // end excludes "\r\n" or "\n"
	var lines []line
	for i := 0; i < len(text); {
		j := strings.IndexByte(text[i:], '\n')
		next := len(text)
		end := len(text)
		if j >= 0 {
			end = i + j
			next = end + 1
		}
		if end > i && text[end-1] == '\r' {
			end--
		}
		lines = append(lines, line{i, end})
		i = next
	}
	var out strings.Builder
	last := 0
	for a := 0; a < len(lines); {
		width := lines[a].end - lines[a].start
		if width < minWrapLine || !base64Line(text[lines[a].start:lines[a].end]) {
			a++
			continue
		}
		b := a + 1
		for b < len(lines) {
			w := lines[b].end - lines[b].start
			if w == 0 || w > width || !base64Line(text[lines[b].start:lines[b].end]) {
				break
			}
			b++
			if w < width {
				break
			}
		}
		if b-a < 2 {
			a++
			continue
		}
		var joined strings.Builder
		for _, ln := range lines[a:b] {
			joined.WriteString(text[ln.start:ln.end])
		}
		at := func(j int) int { // joined offset -> text offset
			return lines[a+j/width].start + j%width
		}
		for _, m := range wrappedMatches(joined.String(), forms) {
			start, end := at(m[0]), at(m[1]-1)+1
			if start < last {
				continue
			}
			if !strings.Contains(text[start:end], "\n") {
				continue // on one line: the single pass already had its chance
			}
			out.WriteString(text[last:start])
			out.WriteString("[secret]")
			last = end
		}
		a = b
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}

// wrappedMatches is the leftmost, then longest, non-overlapping match of any
// form in s. forms are longest first.
func wrappedMatches(s string, forms []string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		hit := 0
		for _, f := range forms {
			if strings.HasPrefix(s[i:], f) {
				hit = len(f)
				break
			}
		}
		if hit == 0 {
			i++
			continue
		}
		out = append(out, [2]int{i, i + hit})
		i += hit
	}
	return out
}

func base64Line(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '-' || c == '_') {
			return false
		}
	}
	return s != ""
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
	n, _ := RegisteredFormState()
	return n
}

// RegisteredForms returns every redactable form across registered vaults,
// longest first. Duplicates are dropped.
func RegisteredForms() []string {
	regMu.RLock()
	stores := append([]*Store(nil), regStores...)
	regMu.RUnlock()
	seen := map[string]struct{}{}
	var forms []string
	for _, s := range stores {
		if s == nil {
			continue
		}
		for _, form := range s.Forms() {
			if form == "" {
				continue
			}
			if _, ok := seen[form]; ok {
				continue
			}
			seen[form] = struct{}{}
			forms = append(forms, form)
		}
	}
	sort.Slice(forms, func(i, j int) bool {
		if len(forms[i]) != len(forms[j]) {
			return len(forms[i]) > len(forms[j])
		}
		return forms[i] < forms[j]
	})
	return forms
}

// RegisteredFormGen changes when a registered vault's secrets change or the
// set of registered stores changes. It does not recompute the forms.
func RegisteredFormGen() uint64 {
	regMu.RLock()
	defer regMu.RUnlock()
	var g uint64 = uint64(len(regStores)) << 32
	for _, s := range regStores {
		if s != nil {
			g += s.FormGen()
		}
	}
	return g
}

// RegisteredFormState is the cached longest form across registered vaults and
// the generation that produced it.
func RegisteredFormState() (int, uint64) {
	regMu.RLock()
	stores := append([]*Store(nil), regStores...)
	regMu.RUnlock()
	best := 0
	var g uint64 = uint64(len(stores)) << 32
	for _, s := range stores {
		if s == nil {
			continue
		}
		n, sg := s.FormLimit()
		g += sg
		if n > best {
			best = n
		}
	}
	return best, g
}

// FormGen changes each time the stored secrets change.
func (s *Store) FormGen() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.formGen
}

// FormLimit is the longest redactable form and the generation it belongs to.
// The length is cached until Set, Remove, or a reload.
func (s *Store) FormLimit() (int, uint64) {
	if s == nil {
		return 0, 0
	}
	for {
		s.mu.Lock()
		if s.formLenOK {
			n, g := s.formLen, s.formGen
			s.mu.Unlock()
			return n, g
		}
		g := s.formGen
		s.mu.Unlock()
		n := longestForm(s)
		s.mu.Lock()
		if s.formGen == g {
			s.formLen = n
			s.formLenOK = true
			s.mu.Unlock()
			return n, g
		}
		s.mu.Unlock()
	}
}

func longestForm(s *Store) int {
	best := 0
	for _, form := range s.valueForms() {
		if len(form) > best {
			best = len(form)
		}
	}
	return best
}

func (s *Store) invalidateForms() {
	s.formLenOK = false
	s.formsOK = false
	s.formGen++
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
