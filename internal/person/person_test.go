package person

import (
	"os"
	"testing"
)

func TestIdentifyStable(t *testing.T) {
	root := t.TempDir()
	acct := Account{Link: "terminal", ExternalID: "uid:1000"}
	a, err := Identify(root, acct, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "u1000" {
		t.Fatalf("id %s", a.ID)
	}
	b, err := Identify(root, acct, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID {
		t.Fatalf("not stable: %s vs %s", a.ID, b.ID)
	}
	people, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 {
		t.Fatalf("people %d", len(people))
	}
	if _, err := os.Stat(root + "/people/u1000/identity.json"); err != nil {
		t.Fatal(err)
	}
}

func TestListSkipsMissingIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/people/garbage/memory", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/people/sa_abcdef01/memory", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/people/sa_abcdef01/memory/.lock", []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	acct := Account{Link: "terminal", ExternalID: "uid:1000"}
	if _, err := Identify(root, acct, "ada"); err != nil {
		t.Fatal(err)
	}
	people, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].ID != "u1000" {
		t.Fatalf("%+v", people)
	}
}

func TestIdentifySeparateAccounts(t *testing.T) {
	root := t.TempDir()
	a, err := Identify(root, Account{Link: "terminal", ExternalID: "uid:1"}, "one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Identify(root, Account{Link: "slack", ExternalID: "U123"}, "two")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("different accounts share an id")
	}
	if b.ID[0] != 'p' {
		t.Fatalf("slack id %s", b.ID)
	}
}

func TestIdentifyRejectsPath(t *testing.T) {
	_, err := Identify(t.TempDir(), Account{Link: "terminal", ExternalID: "../x"}, "x")
	if err == nil {
		t.Fatal("expected error")
	}
}
