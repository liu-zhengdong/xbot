package link

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/machine"
	"xbot/internal/permissions"
	"xbot/internal/tools"
)

func TestTimerPrecheckHonorsChangedMachinePolicy(t *testing.T) {
	root := t.TempDir()
	if err := permissions.Set(root, true); err != nil {
		t.Fatal(err)
	}
	// This is the same live gate installed by cmd/xbot, not a cached mode.
	tools.SetMachineGate(func() (string, error) { return machine.Get(root, machine.Local) })
	t.Cleanup(func() { tools.SetMachineGate(nil) })
	for _, mode := range []string{machine.Ask, machine.Off, "invalid-file"} {
		t.Run(mode, func(t *testing.T) {
			if err := machine.Set(root, machine.Local, machine.Allow); err != nil {
				t.Fatal(err)
			}
			tm := &Timer{Dir: t.TempDir()}
			marker := filepath.Join(t.TempDir(), "marker")
			command := "printf marker > '" + strings.ReplaceAll(marker, "'", "'\"'\"'") + "'"
			if err := tm.Save(Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Precheck: command, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
			posted := 0
			emit := func(Event) error { posted++; return nil }
			if err := tm.Fire(now, emit); err != nil {
				t.Fatal(err)
			}
			if posted != 1 {
				t.Fatal("allow did not emit")
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if mode == "invalid-file" {
				if err := os.WriteFile(machine.Path(root), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := machine.Set(root, machine.Local, mode); err != nil {
				t.Fatal(err)
			}
			// Ordinary routines still fire while checks are refused.
			if err := tm.Save(Routine{ID: "plain", Prompt: "p", Cron: "0 * * * *", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := (&Timer{Dir: tm.Dir}).Fire(now.Add(time.Hour), emit); err != nil {
					t.Fatal(err)
				}
			}
			if posted != 2 {
				t.Fatalf("refused check emitted or plain routine blocked: %d", posted)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("refused check ran: %v", err)
			}
			r, err := tm.Load("check")
			if err != nil || r.LastSlot != "20261027T0900" {
				t.Fatalf("refusal not committed: %+v %v", r, err)
			}
		})
	}
}

func TestTimerPrecheck(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        int
	}{
		{"missing", "", 1},
		{"empty", `,"precheck":""`, 1},
		{"whitespace", `,"precheck":"  \n\t"`, 1},
		{"exit_zero", `,"precheck":"true"`, 1},
		{"exit_nonzero", `,"precheck":"false"`, 0},
		{"output_zero", `,"precheck":"printf private-output; printf private-error >&2"`, 1},
		{"output_nonzero", `,"precheck":"printf private-output; exit 9"`, 0},
		{"bad_command", `,"precheck":"xbot_precheck_nonexistent_command"`, 0},
		{"no_stdin", `,"precheck":"read answer"`, 0},
		{"timeout", `,"precheck":"sleep 10"`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm := &Timer{Dir: t.TempDir(), Sender: "owner"}
			// Write the old JSON shape directly to exercise an actually missing field.
			raw := `{"id":"check","name":"Check","prompt":"inspect","cron":"7 * * * *","enabled":true` + tc.field + `}`
			if err := os.WriteFile(filepath.Join(tm.Dir, "check.json"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 27, 8, 7, 0, 0, time.UTC)
			var posted []Event
			emit := func(ev Event) error { posted = append(posted, ev); return nil }
			start := time.Now()
			if err := tm.Fire(now, emit); err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("precheck was not bounded: %v", elapsed)
			}
			r, err := tm.Load("check")
			if err != nil || r.LastSlot != "20261027T0807" {
				t.Fatalf("slot: %+v %v", r, err)
			}
			if len(posted) != tc.want {
				t.Fatalf("posted %d events, want %d", len(posted), tc.want)
			}
			if tc.want == 1 {
				ev := posted[0]
				if ev.ID != "rt_check_20261027T0807" || ev.Source != NameTimer || ev.Type != TypeTimer || ev.Sender != "owner" || !ev.Untrusted || ev.Content != RoutineBody("check", "Check", "inspect") || !ev.Time.Equal(now) {
					t.Fatalf("event changed: %+v", ev)
				}
			}
			// Reopen the timer: persistence, not an in-memory suppression flag.
			reopened := &Timer{Dir: tm.Dir, Sender: tm.Sender}
			if err := reopened.Fire(now.Add(15*time.Second), emit); err != nil {
				t.Fatal(err)
			}
			if len(posted) != tc.want {
				t.Fatal("same slot fired twice")
			}
		})
	}
}

func TestTimerPrecheckStartFailureCommits(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // sh cannot be started.
	tm := &Timer{Dir: t.TempDir()}
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	if err := tm.Save(Routine{ID: "once", Prompt: "p", At: now.Format(time.RFC3339), Precheck: "true", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tm.Fire(now, func(Event) error { t.Fatal("start failure emitted"); return nil }); err != nil {
		t.Fatal(err)
	}
	r, err := tm.Load("once")
	if err != nil || r.Enabled || r.LastSlot != "20261027T0800" {
		t.Fatalf("one-shot not consumed: %+v %v", r, err)
	}
}

func TestTimerPrecheckEmitFailureRetries(t *testing.T) {
	tm := &Timer{Dir: t.TempDir()}
	if err := tm.Save(Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Precheck: "true", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	want := errors.New("inbox failed")
	if err := tm.Fire(now, func(Event) error { return want }); !errors.Is(err, want) {
		t.Fatalf("error %v", err)
	}
	r, err := tm.Load("check")
	if err != nil || r.LastSlot != "" {
		t.Fatalf("failed emit committed: %+v %v", r, err)
	}
	n := 0
	if err := tm.Fire(now, func(Event) error { n++; return nil }); err != nil || n != 1 {
		t.Fatalf("retry %d %v", n, err)
	}
}

func TestTimerPrecheckLengthAndJSON(t *testing.T) {
	tm := &Timer{Dir: t.TempDir()}
	r := Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Enabled: true}
	r.Precheck = strings.Repeat("x", tools.MaxPrecheckBytes+1)
	if err := tm.Save(r); err == nil {
		t.Fatal("oversized check accepted")
	}
	r.Precheck = ""
	raw, err := json.Marshal(r)
	if err != nil || strings.Contains(string(raw), "precheck") {
		t.Fatalf("omitempty: %s %v", raw, err)
	}
	if err := json.Unmarshal([]byte(`{"precheck":false}`), &r); err == nil {
		t.Fatal("boolean precheck accepted")
	}
}

func BenchmarkTimerPrecheck(b *testing.B) {
	for _, command := range []string{"", "true", "false"} {
		name := command
		if name == "" {
			name = "missing"
		}
		b.Run(name, func(b *testing.B) {
			tm := &Timer{Dir: b.TempDir()}
			if err := tm.Save(Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Precheck: command, Enabled: true}); err != nil {
				b.Fatal(err)
			}
			now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := tm.Fire(now.Add(time.Duration(i)*time.Hour), func(Event) error { return nil }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
