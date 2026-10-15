// Package person is global identity. Every bot shares ~/.xbot/people/<id>/.
// A link maps an external account (the terminal user now, a Slack user later)
// onto a Person. Events carry the Person id, not the raw account.
package person

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Account is an external identity on one link.
type Account struct {
	Link       string `json:"link"`
	ExternalID string `json:"external_id"`
}

// Person is one human (or account-holder) across bots.
type Person struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Accounts    []Account `json:"accounts"`
	// Timezone is an IANA name. Quiet hours use it when the bot has none.
	Timezone string `json:"timezone,omitempty"`
}

// Identify finds the person linked to acct, or creates one.
func Identify(root string, acct Account, display string) (Person, error) {
	if acct.Link == "" || acct.ExternalID == "" {
		return Person{}, errors.New("person: account is incomplete")
	}
	if strings.ContainsAny(acct.ExternalID, "/\\\x00") {
		return Person{}, errors.New("person: external id contains a path separator")
	}
	var found Person
	err := fsutil.WithLock(filepath.Join(root, "people", ".lock"), func() error {
		people, err := List(root)
		if err != nil {
			return err
		}
		for _, p := range people {
			for _, a := range p.Accounts {
				if a == acct {
					found = p
					return nil
				}
			}
		}
		id, err := freshID(root, acct)
		if err != nil {
			return err
		}
		found = Person{ID: id, DisplayName: display, Accounts: []Account{acct}}
		return found.save(root)
	})
	if err != nil {
		return Person{}, err
	}
	if found.ID == "" {
		return Person{}, errors.New("person: identify failed")
	}
	return found, nil
}

// List reads every person directory.
func List(root string) ([]Person, error) {
	dir := filepath.Join(root, "people")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Person
	for _, ent := range entries {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		p, err := Load(root, ent.Name())
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("person: skipping %s: no identity.json", ent.Name())
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Load reads one person.
func Load(root, id string) (Person, error) {
	if !idRe.MatchString(id) {
		return Person{}, fmt.Errorf("person: invalid id %q", id)
	}
	b, err := os.ReadFile(filepath.Join(root, "people", id, "identity.json"))
	if err != nil {
		return Person{}, err
	}
	var p Person
	if err := json.Unmarshal(b, &p); err != nil {
		return Person{}, err
	}
	if p.ID == "" {
		p.ID = id
	}
	return p, nil
}

func (p Person) save(root string) error {
	if !idRe.MatchString(p.ID) {
		return fmt.Errorf("person: invalid id %q", p.ID)
	}
	raw, err := fsutil.Marshal(p)
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(filepath.Join(root, "people", p.ID, "identity.json"), raw, 0600); err != nil {
		return err
	}
	if err := appendLog(root, LogEntry{Time: time.Now().UTC(), Device: deviceOf(root), Person: p}); err != nil {
		return err
	}
	notifyChange(root)
	return nil
}

func freshID(root string, acct Account) (string, error) {
	id := idFor(acct)
	if !exists(root, id) {
		return id, nil
	}
	sum := sha256.Sum256([]byte(acct.Link + "\x00" + acct.ExternalID))
	alt := id + "-" + hex.EncodeToString(sum[:2])
	if !idRe.MatchString(alt) || exists(root, alt) {
		return "", fmt.Errorf("person: id %q already exists", id)
	}
	return alt, nil
}

func idFor(acct Account) string {
	if acct.Link == "terminal" && strings.HasPrefix(acct.ExternalID, "uid:") {
		id := "u" + strings.TrimPrefix(acct.ExternalID, "uid:")
		if idRe.MatchString(id) {
			return id
		}
	}
	sum := sha256.Sum256([]byte(acct.Link + "\x00" + acct.ExternalID))
	return "p" + hex.EncodeToString(sum[:8])
}

func exists(root, id string) bool {
	_, err := os.Stat(filepath.Join(root, "people", id, "identity.json"))
	return err == nil
}
