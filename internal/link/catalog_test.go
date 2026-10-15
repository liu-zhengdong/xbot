package link

import (
	"errors"
	"testing"
)

func TestCatalogInstallNames(t *testing.T) {
	seen := map[string]bool{}
	var available int
	for _, e := range Catalog() {
		if seen[e.ID] {
			t.Fatalf("duplicate %s", e.ID)
		}
		seen[e.ID] = true
		if e.Kind != KindNative && e.Kind != KindMCP {
			t.Fatalf("kind %s", e.Kind)
		}
		if e.Secrets == nil || e.Capabilities == nil {
			t.Fatalf("nil slice %s", e.ID)
		}
		if e.Available {
			available++
			name, err := GrantName(e.ID)
			if err != nil || name == "" || name != e.Grant {
				t.Fatalf("grant %s %q %v", e.ID, name, err)
			}
		} else {
			if _, err := GrantName(e.ID); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unavailable %s %v", e.ID, err)
			}
		}
	}
	if !seen["webhook"] || !seen["telegram"] || !seen["mcp"] || !seen["coding-agent"] || available < 1 {
		t.Fatalf("catalog %v available %d", seen, available)
	}
	if _, err := GrantName("missing"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("missing %v", err)
	}
	if _, ok := Lookup("lark"); !ok {
		t.Fatal("lark missing")
	}
}
