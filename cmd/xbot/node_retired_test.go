package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

const (
	errNodeRemoved = "xbot node 已移除：多台电脑之间请在「设备」里配对（命令行：xbot remote invite / xbot remote join）"
	errSyncRemoved = "xbot sync 已移除：每台电脑各自保留数据，不再同步人和记忆"
)

func TestNodeServeCommandRemoved(t *testing.T) {
	root := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	err = run([]string{"--root", root, "node", "serve", "--addr", addr})
	if err == nil || err.Error() != errNodeRemoved {
		t.Fatalf("node serve: %v", err)
	}
	if c, err := net.Dial("tcp", addr); err == nil {
		c.Close()
		t.Fatalf("node serve still listened on %s", addr)
	}
}

func TestSyncExportCommandRemoved(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out.json")
	err := run([]string{"--root", root, "sync", "export", "--out", out})
	if err == nil || err.Error() != errSyncRemoved {
		t.Fatalf("sync export: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("sync export wrote %s", out)
	}
}

func TestMountSkipsNodeGrant(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "Be brief.", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.GrantLink(link.NameNode); err != nil {
		t.Fatal(err)
	}
	got, err := mount(b, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got {
		if item.Name() == link.NameNode {
			t.Fatal("old node grant still mounted")
		}
	}
}

func TestLegacyNodeFilesReportedAndKept(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{"nodes.json": "nodes", "node-token": "token"}
	for name, body := range want {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "node-pending"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(legacyNodeFiles(root), ","); got != "nodes.json,node-token,node-pending" {
		t.Fatalf("legacyNodeFiles: %q", got)
	}
	for name, body := range want {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(raw) != body {
			t.Fatalf("%s changed: %q %v", name, raw, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(root, "node-pending")); err != nil || !fi.IsDir() {
		t.Fatalf("node-pending changed: %v", err)
	}
	if got := legacyNodeFiles(t.TempDir()); len(got) != 0 {
		t.Fatalf("a clean root reports %v", got)
	}
}
