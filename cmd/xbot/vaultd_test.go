package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseUIDs(t *testing.T) {
	got, err := parseUIDs("1000, 1001 ,2")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1000, 1001, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUIDs = %v, want %v", got, want)
	}
	for _, bad := range []string{"", ",", "abc", "-1", "1,x"} {
		if _, err := parseUIDs(bad); err == nil {
			t.Fatalf("parseUIDs(%q) was accepted", bad)
		}
	}
}

func TestCmdVaultdRequiresFlags(t *testing.T) {
	err := cmdVaultd(t.TempDir(), nil)
	if err == nil {
		t.Fatal("vaultd without flags was accepted")
	}
	if !strings.Contains(err.Error(), "usage: xbot vaultd") {
		t.Fatalf("usage error %q", err)
	}
}
