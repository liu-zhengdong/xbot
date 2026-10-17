package main

import (
	"strings"
	"testing"

	"xbot/internal/person"
	"xbot/internal/remote"
)

func TestRemoteCLI(t *testing.T) {
	root := t.TempDir()
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id := captureStdout(t, func() error { return run([]string{"--root", t.TempDir(), "remote", "id"}) })
	fields := strings.Fields(id)
	if len(fields) != 4 || fields[0] != "id" || fields[2] != "key" {
		t.Fatal("bad identity output")
	}
	for _, args := range [][]string{{"gateway", "https://example.test/"}, {"add", "b", fields[3], "--person", p.ID}} {
		if err := cmdRemote(root, args); err != nil {
			t.Fatal(err)
		}
	}
	if got := captureStdout(t, func() error { return cmdRemote(root, []string{"gateway"}) }); got != "https://example.test\n" {
		t.Fatal(got)
	}
	got := captureStdout(t, func() error { return cmdRemote(root, []string{"list"}) })
	if got != "b\t"+fields[1]+"\t"+p.ID+"\n" {
		t.Fatal("bad device listing")
	}
	if err := cmdRemote(root, []string{"remove", "b"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := remote.Load(root)
	if err != nil || len(cfg.Devices) != 0 {
		t.Fatal("device not removed")
	}
}
