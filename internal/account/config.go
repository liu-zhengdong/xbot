package account

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Env is the standalone service's configuration, read from the environment so
// the hosted deployment needs only the binary, these variables and DATA_DIR.
//
//	DATA_DIR     where accounts.json lives (required)
//	BASE_URL     public address mail links use, e.g. https://account.example.com (required)
//	ADDR         listen address, default :8790
//	MAIL_MODE    outbox (default): write each mail to MAIL_OUTBOX instead of sending
//	MAIL_OUTBOX  default DATA_DIR/outbox
type Env struct {
	DataDir  string
	BaseURL  string
	Addr     string
	MailMode string
	Outbox   string
}

func EnvFrom(get func(string) string) (Env, error) {
	e := Env{
		DataDir:  strings.TrimSpace(get("DATA_DIR")),
		BaseURL:  strings.TrimSpace(get("BASE_URL")),
		Addr:     strings.TrimSpace(get("ADDR")),
		MailMode: strings.TrimSpace(get("MAIL_MODE")),
		Outbox:   strings.TrimSpace(get("MAIL_OUTBOX")),
	}
	if e.DataDir == "" {
		return e, errors.New("DATA_DIR is required")
	}
	if !strings.HasPrefix(e.BaseURL, "https://") && !strings.HasPrefix(e.BaseURL, "http://") {
		return e, errors.New("BASE_URL must be an http(s) address")
	}
	if e.Addr == "" {
		e.Addr = ":8790"
	}
	if e.MailMode == "" {
		e.MailMode = "outbox"
	}
	if e.MailMode != "outbox" {
		return e, errors.New("MAIL_MODE: only outbox is available yet")
	}
	if e.Outbox == "" {
		e.Outbox = filepath.Join(e.DataDir, "outbox")
	}
	return e, nil
}

// Open builds the service an Env describes.
func Open(e Env) (*Service, error) {
	st, err := NewFileStore(e.DataDir)
	if err != nil {
		return nil, err
	}
	return New(st, OutboxMailer{Dir: e.Outbox}, Config{BaseURL: e.BaseURL}), nil
}

// OpenLocal is the stand-in inside xbot serve: data and outbox under dir, and
// links that point back at whichever address the owner used.
func OpenLocal(dir string) (*Service, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	st, err := NewFileStore(dir)
	if err != nil {
		return nil, err
	}
	return New(st, OutboxMailer{Dir: filepath.Join(dir, "outbox")}, Config{}), nil
}
