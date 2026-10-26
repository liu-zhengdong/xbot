// Package account is the product account service: email sign-up, password
// login, magic-link verification, sessions, and the devices an account owns.
// It runs inside xbot serve as a local stand-in, or on its own as the Account
// API (xbot account-api) with the same handler, so the hosted deployment is the
// same binary plus a data directory.
package account

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/fsutil"
)

// State is everything the service keeps. Secrets are stored only as hashes.
type State struct {
	Accounts   map[string]*Account   `json:"accounts"`   // by normalized email
	Challenges map[string]*Challenge `json:"challenges"` // by challenge id
	Sessions   map[string]*Session   `json:"sessions"`   // by sha256(token)
	Devices    map[string]*Device    `json:"devices"`    // by email + "/" + device id
}

// Account is one email account.
type Account struct {
	Email    string    `json:"email"`
	PassHash string    `json:"pass_hash"`
	Verified bool      `json:"verified"`
	Created  time.Time `json:"created"`
	// Failures and FailedAt throttle password guessing.
	Failures int       `json:"failures,omitempty"`
	FailedAt time.Time `json:"failed_at,omitempty"`
}

// Challenge is one pending email proof: a link token and a short code, both
// hashed. The client that started it polls by id, so the id is a secret of
// that client: once the mailbox confirms, the first poll with it collects the
// session.
type Challenge struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// PassHash is the password a sign-up chose; it becomes the account's on confirmation.
	PassHash  string    `json:"pass_hash,omitempty"`
	LinkHash  string    `json:"link_hash"`
	CodeHash  string    `json:"code_hash"`
	Expires   time.Time `json:"expires"`
	SentAt    time.Time `json:"sent_at"`
	Attempts  int       `json:"attempts,omitempty"`
	Done      bool      `json:"done,omitempty"`
	Collected bool      `json:"collected,omitempty"`
	// Silent marks a challenge for an address with no usable account: nothing
	// was sent, but the caller sees the same answer as for a real one.
	Silent bool `json:"silent,omitempty"`
}

// Session is one signed-in client.
type Session struct {
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
}

// Device is one computer that reported itself for an account.
type Device struct {
	ID       string    `json:"id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Bots     []string  `json:"bots"`
	LastSeen time.Time `json:"last_seen"`
}

// Store keeps State. Update runs fn under one lock and saves what it leaves;
// a SQL-backed store can implement the same two calls with a transaction.
type Store interface {
	View(fn func(*State) error) error
	Update(fn func(*State) error) error
}

// FileStore keeps State in one JSON file (mode 0600) under a data directory.
type FileStore struct{ Path string }

// NewFileStore stores under dir/accounts.json.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{Path: filepath.Join(dir, "accounts.json")}, nil
}

func (f *FileStore) View(fn func(*State) error) error {
	return fsutil.WithLock(f.Path+".lock", func() error {
		st, err := f.read()
		if err != nil {
			return err
		}
		return fn(st)
	})
}

func (f *FileStore) Update(fn func(*State) error) error {
	return fsutil.WithLock(f.Path+".lock", func() error {
		st, err := f.read()
		if err != nil {
			return err
		}
		if err := fn(st); err != nil {
			return err
		}
		raw, err := fsutil.Marshal(st)
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(f.Path, raw, 0o600)
	})
}

func (f *FileStore) read() (*State, error) {
	st := &State{}
	raw, err := os.ReadFile(f.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, st); err != nil {
			return nil, err
		}
	}
	if st.Accounts == nil {
		st.Accounts = map[string]*Account{}
	}
	if st.Challenges == nil {
		st.Challenges = map[string]*Challenge{}
	}
	if st.Sessions == nil {
		st.Sessions = map[string]*Session{}
	}
	if st.Devices == nil {
		st.Devices = map[string]*Device{}
	}
	return st, nil
}
