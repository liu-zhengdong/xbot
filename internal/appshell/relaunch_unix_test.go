//go:build unix

package appshell

import (
	"os/exec"
	"strings"
	"testing"
)

// The relaunch must wait for this process to exit. MyGo's own Relaunch starts
// the new process straight away, and the new one then races the old for the
// data root's app.lock and dies with "xbot app is already running".
func TestRelaunchWaitsForThisProcess(t *testing.T) {
	script := waitThenLaunch(4242, "/Applications/XBot.app/Contents/MacOS/XBot", []string{"app", "--port", "8787"})
	if !strings.Contains(script, "kill -0 4242") {
		t.Fatalf("script does not wait for the old pid: %s", script)
	}
	if !strings.HasSuffix(script, "exec '/Applications/XBot.app/Contents/MacOS/XBot' 'app' '--port' '8787'") {
		t.Fatalf("script does not start the binary with its arguments: %s", script)
	}
	// Waiting comes first: starting before the wait would defeat the point.
	if strings.Index(script, "kill -0") > strings.Index(script, "exec ") {
		t.Fatalf("the launch precedes the wait: %s", script)
	}
}

// A path with a quote in it must survive the shell as one argument.
func TestShellQuoteHandlesQuotes(t *testing.T) {
	want := "/tmp/it's here/XBot"
	out, err := exec.Command("/bin/sh", "-c", "printf '%s' "+shellQuote(want)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Fatalf("the shell saw %q, want %q", out, want)
	}
}

// The arguments must be the subcommand and what follows it. os.Args[0] is the
// binary itself, which waitThenLaunch already puts first: passing the whole
// os.Args would run "XBot XBot app" and the new process exits at once.
func TestRelaunchArgsExcludeTheProgramName(t *testing.T) {
	argv := []string{"/Applications/XBot.app/Contents/MacOS/XBot", "app", "--port", "8787"}
	script := waitThenLaunch(1, argv[0], argv[1:])
	want := "exec '/Applications/XBot.app/Contents/MacOS/XBot' 'app' '--port' '8787'"
	if !strings.HasSuffix(script, want) {
		t.Fatalf("script = %s\nwant it to end with %s", script, want)
	}
	if strings.Count(script, "MacOS/XBot") != 1 {
		t.Fatalf("the binary appears more than once: %s", script)
	}
}
