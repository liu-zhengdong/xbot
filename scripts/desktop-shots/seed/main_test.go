package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestSeedModes(t *testing.T) {
	cases := []struct {
		mode string
		msgs int
		bots int
	}{
		{"empty", 0, 1},
		{"chat", 2, 1},
		{"rich", 2, 4},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if err := seed(root, tc.mode); err != nil {
				t.Fatal(err)
			}
			env, err := os.ReadFile(filepath.Join(root, "env"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(env), "XBOT_API_KEY=ci-screenshot-not-a-key") {
				t.Fatalf("env missing key: %q", env)
			}
			name, err := bot.Primary(root)
			if err != nil {
				t.Fatalf("no primary: %v", err)
			}
			b, err := bot.Load(root, name)
			if err != nil {
				t.Fatal(err)
			}
			defer bot.CloseStore(b)
			n, err := b.Count()
			if err != nil {
				t.Fatal(err)
			}
			if n != tc.msgs {
				t.Fatalf("primary has %d entries, want %d", n, tc.msgs)
			}
			all, err := bot.List(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != tc.bots {
				t.Fatalf("%d bots, want %d", len(all), tc.bots)
			}
		})
	}
}
