package main

import (
	"fmt"
	"strings"
	"testing"
)

// The startup line names the 登录码 as the login page does, and keeps
// "login token" for log filters and docs that search for it.
func TestLoginTokenLine(t *testing.T) {
	got := fmt.Sprintf(loginTokenLine, "abc")
	if got != "登录码（login token）: abc\n" {
		t.Errorf("line %q", got)
	}
	if !strings.Contains(got, "login token") {
		t.Error("the line must keep \"login token\"")
	}
}
