//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/peer"
)

func TestCLIChatGroupRuntimeProjectionCurrentRoute(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, current = "ChatProjectionWorker", "ChatProjectionMoved"
	srv.CreateBot(source, "Read complete member chat groups after the original source resource move.")
	var peers []string
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("ProjectionPeer%02d", i) + strings.Repeat("P", 48)
		peers = append(peers, name)
		srv.CreateBot(name, "Actual long-name member for complete group result budgets.")
	}
	at := time.Date(2026, 11, 2, 2, 3, 4, 123000000, time.UTC)
	bus := &peer.Bus{Root: srv.Root, Now: func() time.Time { return at }}
	wantGroups := map[string]cliChatGroup{}
	for i := 0; i < 80; i++ {
		g, err := bus.CreateGroup(source, fmt.Sprintf("Projection%02d", i)+strings.Repeat("G", 34), peers)
		if err != nil {
			t.Fatal(err)
		}
		wantGroups[g.ID] = cliChatGroup{ID: g.ID, Name: g.Name, Members: g.Members, Created: g.Created}
	}
	g, err := bus.CreateGroup(source, "Human-only full history", peers[:1])
	if err != nil {
		t.Fatal(err)
	}
	historyGroup := cliChatGroup{ID: g.ID, Name: g.Name, Members: g.Members, Created: g.Created}
	wantGroups[g.ID] = historyGroup
	var lines []cliChatGroupLine
	var history bytes.Buffer
	quotedBytes := 0
	for i := 0; i < 8; i++ {
		text := fmt.Sprintf("H%d", i) + strings.Repeat("\u0085", 1500) + "END"
		ln := cliChatGroupLine{ID: fmt.Sprintf("gm_projection_%d", i), Time: at.Add(time.Duration(i) * time.Second), Sender: source, Kind: "bot", Text: text}
		lines = append(lines, ln)
		quotedBytes += len(strconv.Quote(text))
		raw, err := json.Marshal(struct {
			ID     string    `json:"id"`
			Time   time.Time `json:"time"`
			Sender string    `json:"sender"`
			Kind   string    `json:"kind"`
			Text   string    `json:"text"`
		}{ln.ID, ln.Time, ln.Sender, ln.Kind, ln.Text})
		if err != nil {
			t.Fatal(err)
		}
		history.Write(raw)
		history.WriteByte('\n')
	}
	// Full new historical data fixture, not a claimed runtime post/fanout.
	historyPath := filepath.Join(srv.Root, "groups", g.ID, "transcript.jsonl")
	f, err := os.OpenFile(historyPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(history.Bytes()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(cliChatGroupHistoryReply{Group: &historyGroup, Messages: &lines, Untrusted: true, Count: len(lines)})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain)+1 > 32<<10 || quotedBytes <= 32<<10 {
		t.Fatal("history must fit complete JSON but exceed actual quoted human32KiB")
	}
	// Same source script and endpoint; all four reads happen after the actual
	// move while the environment's diagnostic source spelling can remain old.
	const cue = "CLI_CHAT_PROJECTION_CURRENT"
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Move the original source resources before complete group reads", "command": `"$XBOT_BIN" profile set --self --name ` + current + ` --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read all member groups with complete typed artifact", "command": `"$XBOT_BIN" chat-group list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Project history when actual human quoting exceeds32KiB", "command": `"$XBOT_BIN" chat-group history ` + g.ID + ` --self --limit 8 --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Consume actual human group projection formatter", "command": `"$XBOT_BIN" chat-group list --self`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Consume actual human history projection formatter", "command": `"$XBOT_BIN" chat-group history ` + g.ID + ` --self --limit 8`}),
	)
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 5 {
		t.Fatal("actual source did not execute rename and four complete group formatters")
	}
	var moved struct {
		Name           string
		PreviousName   string `json:"previous_name"`
		Applied, Moved bool
		Error          string
	}
	cliBotDecode(t, results[0], &moved)
	if moved.Name != current || moved.PreviousName != source || !moved.Applied || !moved.Moved || moved.Error != "" {
		t.Fatal("actual source resource move did not complete")
	}
	for id, value := range wantGroups {
		for i, name := range value.Members {
			if name == source {
				value.Members[i] = current
			}
		}
		wantGroups[id] = value
	}
	historyGroup = wantGroups[g.ID]
	filePattern := regexp.MustCompile(`results/[A-Za-z0-9_.-]+\.json`)
	bytesPattern := regexp.MustCompile(`(?m)bytes:\s*([0-9]+)`)
	for index, result := range results[1:] {
		var output string
		if strings.HasPrefix(strings.SplitN(result, "\n", 2)[0], "background ") {
			fields := strings.Fields(strings.SplitN(result, "\n", 2)[0])
			if len(fields) != 2 {
				t.Fatal("actual background output lacks its original Job id")
			}
			id := fields[1]
			var job struct {
				Status         string
				ExitCode       int `json:"exit_code"`
				Bytes, Dropped int
			}
			WaitFor(t, 20*time.Second, "actual chat-group projection Job", func() (bool, string) {
				raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", current, "shell-jobs", id+".json"))
				if err != nil || json.Unmarshal(raw, &job) != nil {
					return false, "metadata not yet complete"
				}
				return job.Status != "running", job.Status
			})
			if job.Status != "done" || job.ExitCode != 0 || job.Dropped != 0 || job.Bytes <= 0 || job.Bytes > 32<<10 {
				t.Fatal("actual group Job failed or lost complete32KiB output")
			}
			readCue := fmt.Sprintf("CLI_CHAT_PROJECTION_JOB_%d", index)
			model.add(readCue, mcpRuntimeTool("sh", map[string]string{"description": "Read the completed job through the runtime jobs CLI", "command": `"$XBOT_BIN" jobs output ` + id + ` --self`}))
			cliSkillSend(t, srv, current, readCue)
			read := model.results(readCue)
			if len(read) != 1 || len(read[0]) > 32<<10 {
				t.Fatal("native group Job consumer exceeded32KiB")
			}
			parts := strings.SplitN(commandOutput(read[0]), "\n", 4)
			if len(parts) != 4 || parts[0] != "status: done" || parts[1] != "offset: 0" || parts[2] != fmt.Sprintf("next_offset: %d", job.Bytes) {
				t.Fatalf("native group Job consumer lost full status/offset/bytes: %s", read[0])
			}
			output = parts[3]
			if strings.HasPrefix(output, "exit ") {
				if !strings.HasPrefix(output, "exit 0\n") {
					t.Fatal("actual group Job exited nonzero")
				}
				output = commandOutput(output)
			}
		} else {
			if !strings.HasPrefix(result, "exit 0\n") {
				t.Fatal("actual group projection command failed")
			}
			output = commandOutput(result)
		}
		if len(output) == 0 || len(output) > 32<<10 {
			t.Fatal("final exact JSON/human group output exceeded native32KiB")
		}
		isList := index%2 == 0
		var file, access string
		var advertised int64
		if index < 2 {
			if isList {
				var ref cliChatGroupListReply
				if json.Unmarshal([]byte(output), &ref) != nil || ref.Groups != nil || ref.Count != len(wantGroups) || ref.Error != "" {
					t.Fatal("large List did not return complete reference/count without clipped arrays")
				}
				file, access, advertised = ref.File, ref.Access, ref.Bytes
			} else {
				var ref cliChatGroupHistoryReply
				if json.Unmarshal([]byte(output), &ref) != nil || ref.Messages != nil || ref.Count != len(lines) || !ref.Untrusted || ref.Error != "" {
					t.Fatal("human-only overflow history did not project its full typed result")
				}
				file, access, advertised = ref.File, ref.Access, ref.Bytes
			}
		} else {
			files := filePattern.FindAllString(output, -1)
			if len(files) != 1 {
				t.Fatal("actual human group formatter did not identify exactly one full artifact")
			}
			file = files[0]
			match := bytesPattern.FindStringSubmatch(output)
			if len(match) != 2 {
				t.Fatal("actual human group formatter lost full byte count")
			}
			advertised, err = strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, "access: local") {
				t.Fatal("actual human artifact is not marked local")
			}
			access = "local"
		}
		if access != "local" || !strings.HasPrefix(file, "results/") || filepath.IsAbs(file) || filepath.Clean(file) != file || advertised <= 0 || advertised > 20<<20 {
			t.Fatal("group artifact reference violates current local full-result bounds")
		}
		path := filepath.Join(srv.Root, "work", current, filepath.FromSlash(file))
		info, err := os.Stat(path)
		if err != nil || info.Size() != advertised || info.Mode().Perm() != 0600 {
			t.Fatal("current full artifact bytes/mode differ from actual reference")
		}
		raw, err := os.ReadFile(path)
		if err != nil || int64(len(raw)) != advertised || len(raw) == 0 || raw[len(raw)-1] != '\n' {
			t.Fatal("actual full group artifact is missing complete JSON/newline bytes")
		}
		if isList {
			var full cliChatGroupListReply
			if json.Unmarshal(raw, &full) != nil || full.Groups == nil || full.Count != len(wantGroups) || len(*full.Groups) != len(wantGroups) || full.Error != "" || full.File != "" {
				t.Fatal("full List artifact is incomplete or another projection")
			}
			seen := map[string]bool{}
			for _, value := range *full.Groups {
				want, ok := wantGroups[value.ID]
				if !ok || seen[value.ID] || !reflect.DeepEqual(value, want) {
					t.Fatal("full List artifact clipped Group4 fields or retained old membership")
				}
				seen[value.ID] = true
			}
		} else {
			var full cliChatGroupHistoryReply
			if json.Unmarshal(raw, &full) != nil || full.Group == nil || !reflect.DeepEqual(*full.Group, historyGroup) || full.Messages == nil || full.Count != len(lines) || !full.Untrusted || !reflect.DeepEqual(*full.Messages, lines) || full.Error != "" || full.File != "" {
				t.Fatal("full history artifact lost Group4/Line5/untrusted or rewrote old sender data")
			}
			if len(raw) > 32<<10 {
				t.Fatal("human-only fixture no longer isolates actual human formatter overflow")
			}
		}
	}
	for _, path := range []string{filepath.Join(srv.Root, "bots", source), filepath.Join(srv.Root, "work", source)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("old group source directory was recreated after current artifact reads")
		}
	}
}
