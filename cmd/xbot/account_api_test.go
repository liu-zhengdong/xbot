package main

import (
	"testing"

	"xbot/internal/account"
)

func TestAccountAPIDefaultsToHostedForTheApp(t *testing.T) {
	cases := []struct {
		name string
		env  string
		app  bool
		want string
	}{
		{"app, nothing set", "", true, account.HostedBase},
		{"serve, nothing set", "", false, ""},
		{"app, local asked for", "local", true, ""},
		{"serve, local asked for", "local", false, ""},
		{"app, explicit service", "http://127.0.0.1:9000", true, "http://127.0.0.1:9000"},
		{"serve, explicit service", "http://127.0.0.1:9000", false, "http://127.0.0.1:9000"},
		{"app, padded value", "  https://accounts.example  ", true, "https://accounts.example"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XBOT_ACCOUNT_API", c.env)
			if got := accountAPI(c.app); got != c.want {
				t.Fatalf("accountAPI(%v) with XBOT_ACCOUNT_API=%q = %q, want %q", c.app, c.env, got, c.want)
			}
		})
	}
}
