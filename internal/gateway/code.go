package gateway

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

const (
	CodeAlphabet    = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
	LookupLen       = 4
	MaxCodeClaims   = 5
	MaxCodeTTL      = 10 * time.Minute
	codeRegisterGap = 2 * time.Second // Per owner; the client retries once after it.
)

// Short-code lookup (G7). The owner registers the 4-character lookup part of
// its 12-character code; the secret part never reaches the gateway (the two
// installs run SPAKE2 over it). Records are kept by SHA-256 of the lookup,
// one per owner, and stay an hour past expiry so a late claim gets 410.
type codeRecord struct {
	Owner    string    `json:"owner"`
	Expires  time.Time `json:"expires"`
	Claims   int       `json:"claims"`
	Dead     bool      `json:"dead"`
	Claimers []string  `json:"claimers,omitempty"`
}

type codeInput struct {
	Lookup string `json:"lookup"`
	TTL    int    `json:"ttl"`
}

func parseCode(body []byte) (codeInput, error) {
	var in codeInput
	if json.Unmarshal(body, &in) != nil || len(in.Lookup) != LookupLen {
		return in, httpError{400, "bad lookup"}
	}
	for _, ch := range in.Lookup {
		if !strings.ContainsRune(CodeAlphabet, ch) {
			return in, httpError{400, "bad lookup"}
		}
	}
	return in, nil
}

func (s *Server) codePath(lookup string) string {
	hash := sha256.Sum256([]byte(lookup))
	return filepath.Join(s.Dir, "codes", hex.EncodeToString(hash[:])+".json")
}

func readCode(path string) (codeRecord, error) {
	var record codeRecord
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	return record, err
}

// Direct loads enforce retention even between periodic sweeps.
func (s *Server) loadCodeLocked(path string) (codeRecord, error) {
	record, err := readCode(path)
	if err == nil && s.now().After(record.Expires.Add(time.Hour)) {
		if err := os.Remove(path); err != nil {
			return record, err
		}
		return record, os.ErrNotExist
	}
	return record, err
}

func writeCode(path string, record codeRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, data, 0600)
}

// codeRecordsLocked also sweeps records more than an hour past expiry.
func (s *Server) codeRecordsLocked() (map[string]codeRecord, error) {
	files, err := s.filesLocked("codes", "", 0)
	if err != nil {
		return nil, err
	}
	records := make(map[string]codeRecord, len(files))
	for _, f := range files {
		record, err := s.loadCodeLocked(f.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		ok, err := exists(filepath.Join(s.Dir, "installs", record.Owner))
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := os.Remove(f.path); err != nil {
				return nil, err
			}
		} else {
			records[f.path] = record
		}
	}
	return records, nil
}

func (s *Server) deleteOwnerCodesLocked(owner string) error {
	records, err := s.codeRecordsLocked()
	if err != nil {
		return err
	}
	for path, record := range records {
		if record.Owner == owner {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(s.Dir, "code-times", owner)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) registerCode(_ *http.Request, body []byte, caller string, _ ed25519.PublicKey) (int, any, error) {
	in, err := parseCode(body)
	if err != nil {
		return 0, nil, err
	}
	if in.TTL < 1 || in.TTL > int(MaxCodeTTL/time.Second) {
		return 0, nil, httpError{400, "bad ttl"}
	}
	// Keep the rate timestamp separately: another owner may reuse an expired lookup.
	ratePath := filepath.Join(s.Dir, "code-times", caller)
	stamp, err := os.Stat(ratePath)
	if err != nil && !os.IsNotExist(err) {
		return 0, nil, err
	}
	if err == nil && s.now().Before(stamp.ModTime().Add(codeRegisterGap)) {
		return 0, nil, httpError{429, "code registration too frequent"}
	}
	records, err := s.codeRecordsLocked()
	if err != nil {
		return 0, nil, err
	}
	path := s.codePath(in.Lookup)
	if old, ok := records[path]; ok && old.Owner != caller && !old.Dead && old.Claims < MaxCodeClaims && s.now().Before(old.Expires) {
		return 0, nil, httpError{409, "lookup taken"}
	}
	if err := fsutil.WriteAtomic(ratePath, nil, 0600); err != nil {
		return 0, nil, err
	}
	if err := os.Chtimes(ratePath, s.now(), s.now()); err != nil {
		return 0, nil, err
	}
	for p, old := range records {
		if old.Owner == caller {
			if err := os.Remove(p); err != nil {
				return 0, nil, err
			}
		}
	}
	err = writeCode(path, codeRecord{Owner: caller, Expires: s.now().Add(time.Duration(in.TTL) * time.Second)})
	return 200, map[string]string{"status": "ok"}, err
}

func (s *Server) cancelCode(_ *http.Request, body []byte, caller string, _ ed25519.PublicKey) (int, any, error) {
	in, err := parseCode(body)
	if err != nil {
		return 0, nil, err
	}
	path := s.codePath(in.Lookup)
	record, err := s.loadCodeLocked(path)
	if os.IsNotExist(err) || (err == nil && record.Owner != caller) {
		return 0, nil, httpError{404, "no such code"}
	}
	if err != nil {
		return 0, nil, err
	}
	record.Dead = true
	return 200, map[string]string{"status": "ok"}, writeCode(path, record)
}

func (s *Server) claimCode(r *http.Request, body []byte, caller string, pub ed25519.PublicKey) (int, any, error) {
	in, err := parseCode(body)
	if err != nil {
		return 0, nil, err
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if s.TrustForwarded {
		if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
			forwarded := values[len(values)-1]
			if last := net.ParseIP(strings.TrimSpace(forwarded[strings.LastIndex(forwarded, ",")+1:])); last != nil {
				ip = last.String()
			}
		}
	}
	stale := func(t time.Time) bool { return !t.After(s.now().Add(-time.Minute)) }
	s.codeTries = slices.DeleteFunc(s.codeTries, stale)
	for host, tries := range s.codeFailures {
		tries = slices.DeleteFunc(tries, stale)
		if len(tries) == 0 {
			delete(s.codeFailures, host)
		} else {
			s.codeFailures[host] = tries
		}
	}
	if len(s.codeTries) >= 120 || len(s.codeFailures[ip]) >= 10 {
		return 0, nil, httpError{429, "too many tries"}
	}
	path := s.codePath(in.Lookup)
	record, err := s.loadCodeLocked(path)
	known := slices.Contains(record.Claimers, caller)
	var failure httpError
	switch {
	case os.IsNotExist(err):
		failure = httpError{404, "no such code"}
	case err != nil:
		return 0, nil, err
	case record.Dead || (!known && record.Claims >= MaxCodeClaims):
		failure = httpError{410, "code used"}
	case !s.now().Before(record.Expires):
		failure = httpError{410, "code expired"}
	}
	// Only counted failures create entries, so the IP map is bounded by 120.
	if failure.status != 0 {
		if s.codeFailures == nil {
			s.codeFailures = make(map[string][]time.Time)
		}
		s.codeFailures[ip] = append(s.codeFailures[ip], s.now())
		s.codeTries = append(s.codeTries, s.now())
		return 0, nil, failure
	}
	if record.Owner == caller {
		return 0, nil, httpError{400, "own code"}
	}
	if err := s.admitPendingLocked(pub, record.Owner); err != nil {
		return 0, nil, err
	}
	if !known {
		record.Claims++
		record.Claimers = append(record.Claimers, caller)
		if err := writeCode(path, record); err != nil {
			return 0, nil, err
		}
	}
	return 200, map[string]string{"install": record.Owner}, nil
}
