package main

import (
	"bytes"
	"errors"
	"testing"
	"xbot/internal/manual"
)

func TestManualCommandUsesEmbeddedSections(t *testing.T) {
	ids := []string{"index"}
	for _, s := range manual.Sections() {
		ids = append(ids, s.ID)
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			var out bytes.Buffer
			args := []string{id}
			if id == "index" {
				args = nil
			}
			if err := cmdManual(args, &out); err != nil {
				t.Fatal(err)
			}
			want, _ := manual.Read(id)
			if out.String() != want {
				t.Fatalf("CLI differs from shared manual: %s", id)
			}
		})
	}
	var out bytes.Buffer
	if err := cmdManual([]string{"../memory"}, &out); err == nil || out.Len() != 0 {
		t.Fatal("invalid section accepted")
	}
	if err := cmdManual([]string{"memory", "context"}, &out); !errors.Is(err, errUsage) {
		t.Fatal(err)
	}
}
