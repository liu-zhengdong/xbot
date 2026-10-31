package vault

import (
	"path/filepath"
	"testing"
)

func TestBackendSetDomainsPreservesValue(t *testing.T) {
	local := testStore(t)
	daemon := newDaemon(t)
	for _, tc := range []struct {
		name     string
		backend  Backend
		dir, key string
	}{
		{"local", local, local.Dir(), local.KeyPath()},
		{"daemon", daemon.openClient(t, "bot1"), filepath.Join(daemon.dataDir, "bot1"), filepath.Join(daemon.dataDir, "keys", "bot1.key")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.backend.Set("TOKEN", "fixture-original", []string{"a.com"}); err != nil {
				t.Fatal(err)
			}
			// A different writer has changed the value since this backend was opened.
			writer, err := Open(tc.dir, tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.Set("TOKEN", "fixture-updated", []string{"b.com"}); err != nil {
				t.Fatal(err)
			}
			if err := tc.backend.SetDomains("TOKEN", nil); err != nil {
				t.Fatal(err)
			}
			disk, err := Open(tc.dir, tc.key)
			if err != nil {
				t.Fatal(err)
			}
			sec, ok := disk.Lookup("TOKEN")
			if !ok || sec.Value != "fixture-updated" || len(sec.Domains) != 0 {
				t.Fatal("domain update replaced value or ignored local binding")
			}
			if err := tc.backend.SetDomains("TOKEN", []string{" EXAMPLE.COM ", "example.com"}); err != nil {
				t.Fatal(err)
			}
			list := tc.backend.List()
			if len(list) != 1 || list[0].Value != "" || len(list[0].Domains) != 1 || list[0].Domains[0] != "example.com" {
				t.Fatal("metadata update contract")
			}
			if err := tc.backend.Remove("TOKEN"); err != nil {
				t.Fatal(err)
			}
			if err := tc.backend.SetDomains("TOKEN", nil); err == nil {
				t.Fatal("deleted secret revived")
			}
		})
	}
}
