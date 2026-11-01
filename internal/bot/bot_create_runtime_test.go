package bot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCommitErrorStages(t *testing.T) {
	for _, tc := range []struct {
		stage   string
		applied bool
		block   string
		persona string
	}{
		{stage: "default_binding", block: "providers.json.lock"},
		{stage: "config", block: "bots/newbot/config.json.lock"},
		{stage: "state", applied: true, block: "bots/newbot/state.json"},
		{stage: "persona", applied: true, block: "bots/newbot/persona.md"},
		{stage: "persona", applied: true, persona: strings.Repeat("p", (32<<10)+1)},
	} {
		t.Run(tc.stage+tc.block, func(t *testing.T) {
			root := t.TempDir()
			if tc.block != "" {
				if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(tc.block)), 0700); err != nil {
					t.Fatal(err)
				}
			}
			persona := tc.persona
			if persona == "" {
				persona = "agreed duties"
			}
			made, err := Create(root, "newbot", persona, []string{"terminal"})
			var committed *CreateCommitError
			if made != nil || !errors.As(err, &committed) || committed.Stage != tc.stage || committed.Applied != tc.applied {
				t.Fatalf("actual create failure: bot=%v error=%v commit=%+v", made, err, committed)
			}
			if err.Error() != committed.Err.Error() || errors.Unwrap(err) != committed.Err {
				t.Fatalf("changed original error: %v", err)
			}
			_, configErr := os.Stat(filepath.Join(Dir(root, "newbot"), "config.json"))
			if tc.applied && configErr != nil {
				t.Fatalf("applied config missing: %v", configErr)
			}
			if !tc.applied && !errors.Is(configErr, os.ErrNotExist) {
				t.Fatalf("unexpected config write: %v", configErr)
			}
			if tc.persona != "" {
				if _, err := os.Stat(filepath.Join(Dir(root, "newbot"), "state.json")); err != nil {
					t.Fatalf("32KiB rejection happened before state commit: %v", err)
				}
				if _, err := os.Stat(filepath.Join(Dir(root, "newbot"), "persona.md")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("oversize persona written: %v", err)
				}
			}
		})
	}
}

func TestCreateCommitErrorUnwrap(t *testing.T) {
	sentinel := errors.New("underlying write failure")
	var err error = &CreateCommitError{Applied: true, Stage: "state", Err: sentinel}
	if !errors.Is(err, sentinel) || err.Error() != sentinel.Error() {
		t.Fatalf("lost original error semantics: %v", err)
	}
	root := t.TempDir()
	if _, err := Create(root, "existing", "duties", nil); err != nil {
		t.Fatal(err)
	}
	_, err = Create(root, "existing", "other duties", nil)
	var committed *CreateCommitError
	if !errors.Is(err, ErrExists) || errors.As(err, &committed) {
		t.Fatalf("validation must retain its original error: %v", err)
	}
}
