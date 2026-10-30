//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise real CLI writes, serve's timer ticker, the persisted inbox, and the
// model request. Only the model is scripted; no internal scheduler or kernel
// helper is called by this test.
func TestRoutineSkillsCLIAndWake(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	cli := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(xbotBinary, append([]string{"--root", srv.Root, "routine"}, args...)...)
		cmd.Env = srv.env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("routine %v: %v\n%s", args, err, out)
		}
		return out
	}
	type routine struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Skills []string `json:"skills"`
	}
	list := func(bot string) []routine {
		t.Helper()
		var out []routine
		raw := cli("list", "--bot", bot)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("list is not JSON: %v\n%s", err, raw)
		}
		return out
	}
	const first = "ROUTINE-ONLY-FIRST-BODY"
	const second = "ROUTINE-ONLY-SECOND-BODY"
	var sample []byte
	var sampleHeading string
	if path := os.Getenv("XBOT_ROUTINE_SKILL_SAMPLE"); path != "" {
		var err error
		sample, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(sample), "\n") {
			if strings.HasPrefix(line, "# ") {
				sampleHeading = line
				break
			}
		}
		if sampleHeading == "" {
			t.Fatal("skill sample must contain a Markdown heading")
		}
		t.Logf("real skill sample: %s (%d bytes)", path, len(sample))
	}
	library := map[string][]byte{}
	for slug, body := range map[string]string{"doc-atlas": first, "release-check": second} {
		raw := []byte(fmt.Sprintf("---\nname: %s\ndescription: Local routine smoke fixture\n---\n\n%s\n", slug, body))
		if slug == "doc-atlas" && sample != nil {
			raw = append(append([]byte(nil), sample...), []byte("\n\n"+first+"\n")...)
		}
		path := filepath.Join(srv.Root, "skills", slug, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		library[path] = raw
	}

	// Give configuration ample time, then advance the existing test clock.
	at := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)
	for _, tc := range []struct {
		bot, cue, skills string
	}{
		{"attached", "ATTACHED", "doc-atlas,release-check"},
		{"plain", "PLAIN", ""},
		{"missing", "MISSING", "missing-skill,release-check"},
	} {
		srv.CreateBot(tc.bot, "Inspect the scheduled task and stay silent when appropriate.")
		if reply := srv.Send(tc.bot, "SMOKE_ROUTINE "+tc.cue+" "+at.Format(time.RFC3339)); reply.Error != "" || reply.Status != 200 {
			t.Fatalf("create %s: %+v", tc.bot, reply)
		}
		routines := list(tc.bot)
		if len(routines) != 1 {
			t.Fatalf("%s routines: %+v", tc.bot, routines)
		}
		id := routines[0].ID
		// Plain exercises clearing a previously attached value through the
		// real parser, rather than merely starting with no skills.
		if tc.bot == "plain" {
			cli("update", id, "--bot", tc.bot, "--skills", "doc-atlas")
		}
		cli("update", id, "--bot", tc.bot, "--skills", tc.skills)
		got := list(tc.bot)
		want := strings.Split(tc.skills, ",")
		if tc.skills == "" {
			want = nil
		}
		if len(got) != 1 || strings.Join(got[0].Skills, ",") != strings.Join(want, ",") {
			t.Fatalf("%s skills: %+v, want %v", tc.bot, got, want)
		}
	}

	srv.Advance(time.Until(at.Add(5 * time.Second)))
	requests := map[string]stubRequest{}
	WaitFor(t, 90*time.Second, "all three routine model requests", func() (bool, string) {
		for _, record := range stub.Requests() {
			var req stubRequest
			if err := json.Unmarshal([]byte(record.Body), &req); err != nil {
				t.Fatalf("model request: %v", err)
			}
			text := lastUser(req)
			for _, cue := range []string{"ATTACHED", "PLAIN", "MISSING"} {
				if strings.Contains(text, "<routine") && strings.Contains(text, "SMOKE_FIRE_"+cue) {
					requests[cue] = req
				}
			}
		}
		return len(requests) == 3, fmt.Sprintf("routine calls seen: %v", requests)
	})
	attached := lastUser(requests["ATTACHED"])
	one, two := strings.Index(attached, first), strings.Index(attached, second)
	if one < 0 || two <= one {
		t.Fatalf("attached skill bodies missing or out of order:\n%s", attached)
	}
	if sampleHeading != "" && !strings.Contains(attached, sampleHeading) {
		t.Fatalf("real skill sample body missing from routine request: %s", sampleHeading)
	}
	if strings.Contains(joined(requests["PLAIN"]), first) || strings.Contains(joined(requests["PLAIN"]), second) {
		t.Fatalf("cleared routine inherited skill bodies:\n%s", joined(requests["PLAIN"]))
	}
	missing := lastUser(requests["MISSING"])
	if !strings.Contains(missing, `Routine skill "missing-skill" skipped:`) || !strings.Contains(missing, second) {
		t.Fatalf("missing skill was not explained or remaining skill was not loaded:\n%s", missing)
	}
	for cue, req := range requests {
		for _, message := range req.Messages {
			if message.Role == "system" && (strings.Contains(message.Content, first) || strings.Contains(message.Content, second)) {
				t.Fatalf("%s skill body entered stable prefix", cue)
			}
		}
	}
	for _, bot := range []string{"attached", "plain", "missing"} {
		for _, row := range srv.Transcript(bot) {
			if strings.Contains(row.Content, first) || strings.Contains(row.Content, second) {
				t.Fatalf("%s persisted a skill body: %s", bot, row.Content)
			}
		}
	}
	for path, before := range library {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("routine changed the skill library: %s (%v)", path, err)
		}
	}
}
