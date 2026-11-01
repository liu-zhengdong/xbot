package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestRoutineInputSources(t *testing.T) {
	raw := `{"id":"morning","name":"","prompt":"  keep spacing  ","cron":" 0 9 * * * ","at":" literal ","precheck":" test -f ready ","skills":[" ship ","ship"]}`
	file := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		command cliops.RoutineCommand
		stdin   io.Reader
	}{
		{"inline", cliops.RoutineCommand{Args: raw}, nil},
		{"file", cliops.RoutineCommand{ArgsFile: file}, nil},
		{"stdin", cliops.RoutineCommand{ArgsFile: "-"}, strings.NewReader(raw)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readRoutineCreateInput(tc.command, tc.stdin)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != "morning" || got.Name != "" || got.Prompt != "  keep spacing  " || got.Cron != " 0 9 * * * " || got.At != " literal " || got.Precheck == nil || *got.Precheck != " test -f ready " || !reflect.DeepEqual(got.Skills, []string{" ship ", "ship"}) {
				t.Fatalf("input changed domain values: %+v", got)
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		_, err := readRoutinePatchInput(cliops.RoutineCommand{ArgsFile: filepath.Join(t.TempDir(), "missing")}, nil)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("stdin read failure", func(t *testing.T) {
		failure := errors.New("stdin failed")
		_, err := readRoutineCreateInput(cliops.RoutineCommand{ArgsFile: "-"}, routineInputErrorReader{failure})
		if !errors.Is(err, failure) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestRoutineInputTypedPointers(t *testing.T) {
	t.Run("create duplicate casefold null resets precheck", func(t *testing.T) {
		got, err := readRoutineCreateInput(cliops.RoutineCommand{Args: `{"prompt":"first","Prompt":"last","precheck":"must not survive","PreCheck":null,"skills":["first"],"SKILLS":[]}`}, nil)
		if err != nil || got.Prompt != "last" || got.Precheck != nil || got.Skills == nil || len(got.Skills) != 0 {
			t.Fatalf("input=%+v error=%v", got, err)
		}
	})
	t.Run("patch omission and null preserve nil", func(t *testing.T) {
		for _, raw := range []string{`{}`, `{"enabled":null,"precheck":null,"skills":null}`} {
			got, err := readRoutinePatchInput(cliops.RoutineCommand{Args: raw}, nil)
			if err != nil || got.Enabled != nil || got.Precheck != nil || got.Skills != nil {
				t.Fatalf("raw=%s input=%+v error=%v", raw, got, err)
			}
		}
	})
	t.Run("patch explicit clears survive final encoding", func(t *testing.T) {
		got, err := readRoutinePatchInput(cliops.RoutineCommand{Args: `{"name":"  ","prompt":" raw ","cron":" raw ","enabled":false,"precheck":"","skills":[]}`}, nil)
		if err != nil || got.Enabled == nil || *got.Enabled || got.Precheck == nil || *got.Precheck != "" || got.Skills == nil || *got.Skills == nil || len(*got.Skills) != 0 || got.Name != "  " || got.Prompt != " raw " || got.Cron != " raw " {
			t.Fatalf("input=%+v error=%v", got, err)
		}
		wire, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip cliops.RoutinePatch
		if err := json.Unmarshal(wire, &roundtrip); err != nil || !reflect.DeepEqual(got, roundtrip) {
			t.Fatalf("wire=%s roundtrip=%+v error=%v", wire, roundtrip, err)
		}
	})
	t.Run("patch duplicate null uses standard typed semantics", func(t *testing.T) {
		got, err := readRoutinePatchInput(cliops.RoutineCommand{Args: `{"name":"first","NAME":null,"enabled":true,"Enabled":null,"precheck":"must not survive","PRECHECK":null,"skills":["first"],"Skills":null}`}, nil)
		if err != nil || got.Name != "first" || got.Enabled != nil || got.Precheck != nil || got.Skills != nil {
			t.Fatalf("input=%+v error=%v", got, err)
		}
	})
}

func TestRoutineInputRejectInvalidObjects(t *testing.T) {
	for _, raw := range []string{
		"", " \n\t", "\u00a0{}", "{}\u00a0", "null", "[]", `"string"`, "true", "1",
		"{", `{"prompt":}`, `{} {}`, `{} null`, `{} garbage`,
		`{"unknown":"field"}`, `{"id":1}`, `{"name":[]}`, `{"prompt":true}`,
		`{"cron":{}}`, `{"precheck":false}`, `{"skills":"ship"}`, `{"skills":[1]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if got, err := readRoutineCreateInput(cliops.RoutineCommand{Args: raw}, nil); err == nil {
				t.Errorf("create accepted %q: %+v", raw, got)
			}
			if got, err := readRoutinePatchInput(cliops.RoutineCommand{Args: raw}, nil); err == nil {
				t.Errorf("patch accepted %q: %+v", raw, got)
			}
		})
	}
	for _, raw := range []string{`{"id":"new"}`, `{"at":"once"}`, `{"zone":"local"}`, `{"last_slot":"slot"}`, `{"enabled":"false"}`} {
		if got, err := readRoutinePatchInput(cliops.RoutineCommand{Args: raw}, nil); err == nil {
			t.Errorf("patch accepted unsupported field/type %q: %+v", raw, got)
		}
	}
	if got, err := readRoutineCreateInput(cliops.RoutineCommand{Args: `{"enabled":false}`}, nil); err == nil {
		t.Errorf("create accepted unsupported enabled: %+v", got)
	}
	if _, err := readRoutinePatchInput(cliops.RoutineCommand{Args: " \n {} \t\n"}, nil); err != nil {
		t.Fatalf("valid object plus whitespace rejected: %v", err)
	}
}

func TestRoutineInputRawLimit(t *testing.T) {
	// This patch has the same raw and final wire length, exactly at the cap.
	raw := `{"prompt":"` + strings.Repeat("p", cliops.RoutineMaxRequest-len(`{"prompt":""}`)) + `"}`
	got, err := readRoutinePatchInput(cliops.RoutineCommand{ArgsFile: "-"}, strings.NewReader(raw))
	if err != nil || len(got.Prompt) != cliops.RoutineMaxRequest-len(`{"prompt":""}`) {
		t.Fatalf("exactly 1 MiB input: prompt bytes=%d error=%v", len(got.Prompt), err)
	}
	over := raw + " "
	file := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(file, []byte(over), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.RoutineCommand{{Args: over}, {ArgsFile: file}, {ArgsFile: "-"}} {
		stdin := strings.NewReader(over + strings.Repeat(" ", 100))
		if _, err := readRoutinePatchInput(command, stdin); err == nil || !strings.Contains(err.Error(), "arguments exceed 1 MiB") {
			t.Errorf("oversized source accepted: args-file=%q error=%v", command.ArgsFile, err)
		}
		if command.ArgsFile == "-" && stdin.Len() != 100 {
			t.Errorf("read beyond max+1: unread bytes=%d", stdin.Len())
		}
	}
	if _, err := readRoutineCreateInput(cliops.RoutineCommand{Args: over}, nil); err == nil {
		t.Fatal("create accepted raw max+1")
	}
}

func TestRoutineInputFinalWireLimit(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		t.Run(action, func(t *testing.T) {
			base, _ := json.Marshal(cliops.RoutineCreate{})
			overhead := len(base)
			if action == "update" {
				base, _ = json.Marshal(cliops.RoutinePatch{Prompt: "x"})
				overhead = len(base) - 1 // only the sample prompt contributes a value byte
			}
			// '<' is six wire bytes (\u003c), though just one raw input byte.
			fit := (cliops.RoutineMaxRequest - overhead) / 6
			for _, tc := range []struct {
				count int
				ok    bool
			}{{fit, true}, {fit + 1, false}} {
				command := cliops.RoutineCommand{Args: `{"prompt":"` + strings.Repeat("<", tc.count) + `"}`}
				if len(command.Args) >= cliops.RoutineMaxRequest {
					t.Fatal("fixture must fit the raw limit")
				}
				var err error
				if action == "create" {
					_, err = readRoutineCreateInput(command, nil)
				} else {
					_, err = readRoutinePatchInput(command, nil)
				}
				if (err == nil) != tc.ok {
					t.Errorf("count=%d want success=%t error=%v", tc.count, tc.ok, err)
				}
				if err != nil && !strings.Contains(err.Error(), "after JSON encoding") {
					t.Errorf("unexpected final-wire error: %v", err)
				}
			}
		})
	}
}

type routineInputErrorReader struct{ err error }

func (r routineInputErrorReader) Read([]byte) (int, error) { return 0, r.err }
