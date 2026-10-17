package remote

import (
	"errors"
	"strings"
	"testing"
	"time"

	"xbot/internal/person"
)

func TestAddDevice(t *testing.T) {
	root := t.TempDir()
	_, _, self, err := Identity(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, key, err := Identity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, key string }{{"-bad", key}, {strings.Repeat("x", 65), key}, {"b", "bad"}, {"b", self}, {"b", key}} {
		if _, err := AddDevice(root, tc.name, tc.key, ""); err == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	d, err := AddDevice(root, "b", key, "")
	if err != nil || d.Person != p.ID {
		t.Fatalf("default person: %v", err)
	}
	for _, tc := range []struct{ name, key, person string }{{"b", key, p.ID}, {"c", key, p.ID}, {"c", key, "missing"}} {
		if _, err := AddDevice(root, tc.name, tc.key, tc.person); err == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
	if _, err := person.Identify(root, person.Account{Link: "test", ExternalID: "second"}, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddDevice(root, "c", key, ""); err == nil || !strings.Contains(err.Error(), "--person") {
		t.Fatal("ambiguous default")
	}
}

// Regression: Identity used to take the same file lock LoadOrCreateKey takes,
// which blocks forever on a second open of the lock file.
func TestIdentityDoesNotDeadlock(t *testing.T) {
	root := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, id1, _, err := Identity(root)
		if err == nil {
			var id2 string
			_, id2, _, err = Identity(root)
			if err == nil && id1 != id2 {
				err = errors.New("identity changed")
			}
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Identity deadlocked")
	}
}
