package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInterpreterDiscovery(t *testing.T) {
	for _, tc := range []struct {
		os      string
		present map[string]string
		want    string
	}{
		{"darwin", map[string]string{"zsh": "/bin/zsh", "bash": "/bin/bash"}, "zsh,bash"},
		{"linux", map[string]string{"bash": "/bin/bash", "sh": "/bin/sh"}, "bash,sh"},
		{"windows", map[string]string{"powershell.exe": "PS", "cmd.exe": "CMD", "bash.exe": "WSL"}, "powershell,cmd"},
		{"windows", map[string]string{"pwsh.exe": "PS7", "powershell.exe": "PS5", "cmd.exe": "CMD", "git.exe": filepath.Join("git", "cmd", "git.exe"), filepath.Join("git", "bin", "bash.exe"): "GITBASH"}, "powershell,cmd,git-bash"},
	} {
		t.Run(tc.os+tc.want, func(t *testing.T) {
			lookup := func(name string) (string, error) {
				if p, ok := tc.present[name]; ok {
					return p, nil
				}
				return "", errors.New("missing")
			}
			var names []string
			got := discoverInterpreters(tc.os, lookup, func(string) string { return "" }, func(in interpreter) string {
				if in.name == "bash" {
					return "bash 5.2"
				}
				return ""
			})
			for _, in := range got {
				names = append(names, in.name)
				sh := newInterpreterShell(in)
				if !strings.Contains(sh.Description(), in.name) || !strings.Contains(sh.Description(), in.version) {
					t.Fatal(sh.Description())
				}
				if in.version == "" {
					t.Fatal("missing fallback")
				}
			}
			if strings.Join(names, ",") != tc.want {
				t.Fatalf("got %v want %s", names, tc.want)
			}
		})
	}
}

func TestInterpreterArguments(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags string
	}{{"bash", "--noprofile --norc -c"}, {"zsh", "-f -c"}, {"sh", "-c"}, {"git-bash", "--noprofile --norc -c"}, {"powershell", "-NoLogo -NoProfile -NonInteractive -Command"}, {"cmd", "/d /s /c"}} {
		script := "literal $value; 'quotes' & spaces"
		cmd := (interpreter{name: tc.name, path: "/chosen/executable"}).command(context.Background(), script)
		if cmd.Path != "/chosen/executable" || cmd.Args[len(cmd.Args)-1] != script || strings.Join(cmd.Args[1:len(cmd.Args)-1], " ") != tc.flags {
			t.Fatalf("%s: %v", tc.name, cmd.Args)
		}
	}
}

func TestInterpreterRequiresDescriptionBeforeExecution(t *testing.T) {
	sh := NewShell()
	for _, raw := range []string{`{"command":"echo ok"}`, `{"command":"echo ok","description":" "}`, `{"command":"echo ok","description":3}`} {
		if _, err := sh.Run(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, in := range []map[string]string{{"command": "echo ok", "description": strings.Repeat("x", 1025)}, {"command": strings.Repeat("x", maxCommandBytes+1), "description": "Check input limit"}} {
		raw, _ := json.Marshal(in)
		if _, err := sh.Run(context.Background(), raw); err == nil {
			t.Fatal("accepted oversized input")
		}
	}
}

func TestInstalledInterpretersExecuteAndKeepJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX execution fixture")
	}
	for _, sh := range NewShells() {
		t.Run(sh.Name(), func(t *testing.T) {
			if _, err := exec.LookPath(sh.interpreter.path); err != nil {
				t.Fatal(err)
			}
			command := "printf explicit-sh"
			if sh.Name() == "bash" {
				command = `[[ -n "$BASH_VERSION" ]] && printf explicit-bash`
			}
			if sh.Name() == "zsh" {
				command = `[[ -n "$ZSH_VERSION" ]] && printf explicit-zsh`
			}
			args, _ := json.Marshal(map[string]string{"command": command, "description": "Verify selected interpreter"})
			out, err := sh.Run(context.Background(), args)
			if err != nil || !strings.Contains(out, "exit 0") || !strings.Contains(out, "explicit-"+sh.Name()) {
				t.Fatalf("%q %v", out, err)
			}
			jobs := NewJobs(t.TempDir())
			defer jobs.Shutdown()
			sh.BindJobs(jobs)
			done := make(chan Job, 1)
			jobs.OnDone = func(job Job) { done <- job }
			args, _ = json.Marshal(map[string]any{"command": command, "description": "Verify selected interpreter", "run_in_background": true})
			out, err = sh.Run(context.Background(), args)
			if err != nil || !strings.HasPrefix(out, "background ") {
				t.Fatalf("job %q %v", out, err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("job did not complete")
			}
			list := jobs.List()
			if len(list) != 1 || list[0].Tool != sh.Name() || list[0].Description != "Verify selected interpreter" {
				t.Fatalf("jobs: %+v", list)
			}
			stream, err := jobs.Output(list[0].ID, 0, 0)
			if err != nil || !strings.Contains(stream, "explicit-"+sh.Name()) {
				t.Fatalf("stream %q %v", stream, err)
			}
		})
	}
}

func TestShellFactoriesDoNotShareMutableSessions(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, sh := range NewShells() {
				sh.BindSession(func(context.Context) (string, string) { return "private", "" }, nil)
			}
		}()
	}
	wg.Wait()
	for _, sh := range NewShells() {
		start, _, _ := sh.session()
		if start != nil {
			t.Fatal("shared session")
		}
	}
}

func TestVersionProbeFailureIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX probe fixture")
	}
	start := time.Now()
	// A missing executable must return unknown, not remove the interpreter.
	if got := probeVersion(interpreter{name: "bash", path: filepath.Join(t.TempDir(), "missing")}); got != "" {
		t.Fatal(got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("unbounded probe")
	}
}

func TestInterpreterCwdAndExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX cwd fixture")
	}
	for _, sh := range NewShells() {
		t.Run(sh.Name(), func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "marker'with-quote")
			script := sh.wrapCwd(root, "exit 7", marker)
			out, err := runInterpreterCommand(context.Background(), sh.interpreter, script, time.Second, 1024, root, nil)
			if err != nil || !strings.HasPrefix(out, "exit 7") {
				t.Fatalf("exit %q %v", out, err)
			}
			want, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := readCwdFile(marker); got != want {
				t.Fatalf("cwd %q want %q", got, want)
			}
		})
	}
}

func TestShellRawArgumentLimit(t *testing.T) {
	raw := json.RawMessage(`{"command":"echo x","description":"Check limit","unused":"` + strings.Repeat("x", 128<<10) + `"}`)
	if _, err := NewShell().Run(context.Background(), raw); err == nil {
		t.Fatal("unbounded arguments")
	}
}

// This reports the local process path, not model or network latency.
func BenchmarkInterpreterPaths(b *testing.B) {
	NewShells()
	b.Run("cached-catalog", func(b *testing.B) {
		samples := make([]time.Duration, 0, b.N)
		for i := 0; i < b.N; i++ {
			start := time.Now()
			NewShells()
			samples = append(samples, time.Since(start))
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		b.ReportMetric(float64(samples[(len(samples)-1)*95/100].Nanoseconds()), "p95-ns")
	})
	if runtime.GOOS == "windows" {
		return
	}
	for _, sh := range NewShells() {
		b.Run(sh.Name(), func(b *testing.B) {
			samples := make([]time.Duration, 0, b.N)
			args := json.RawMessage(`{"command":"printf ok","description":"Measure local invocation overhead"}`)
			for i := 0; i < b.N; i++ {
				start := time.Now()
				out, err := sh.Run(context.Background(), args)
				if err != nil || !strings.Contains(out, "exit 0") {
					b.Fatal(out, err)
				}
				samples = append(samples, time.Since(start))
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			b.ReportMetric(float64(samples[(len(samples)-1)*95/100].Nanoseconds()), "p95-ns")
		})
	}
}
