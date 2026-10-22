//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"xbot/internal/peer"
)

// This is test evidence, never a production delivery receipt. Repeated values
// require a human to distinguish a recap from repeating a report's body.
type busyReportEvidence struct {
	Task      string              `json:"task"`
	Value     string              `json:"value"`
	Results   []Row               `json:"results"`
	First     *Row                `json:"first"`
	Mentions  []busyReportMention `json:"mentions"`
	Premature []Row               `json:"premature"`
	Status    string              `json:"status"`
}

type busyReportMention struct {
	Row         Row      `json:"row"`
	Occurrences int      `json:"occurrences"`
	NewResults  []string `json:"new_results"`
	OwnerInputs []Row    `json:"owner_inputs"`
}

// busyReports consumes the same ordered transcript in live smoke and offline
// rescoring. It extracts facts only: a new result in the same reply is context,
// not permission to repeat an old report; absence of a new result is not guilt.
func busyReports(rows []Row, tasks []string) []busyReportEvidence {
	out := make([]busyReportEvidence, len(tasks))
	for i, task := range tasks {
		e := &out[i]
		e.Task, e.Value = task, busyCode(task)
		value := regexp.MustCompile(`\b` + regexp.QuoteMeta(e.Value) + `\b`)
		marker := regexp.MustCompile(`\b` + regexp.QuoteMeta("RESULT_"+task) + `\b`)
		var owners []Row
		for _, row := range rows {
			source, _, event := peer.EventSource(row.Content)
			if row.Role == "user" && !row.Internal {
				if event {
					header, _, _ := strings.Cut(row.Content, "\n")
					terminal := strings.Contains(header, `type="done"`) || strings.Contains(header, `type="failed"`) || strings.Contains(header, `type="fork_end"`)
					if source == "subagent" && terminal && marker.MatchString(row.Content) && value.MatchString(row.Content) {
						e.Results = append(e.Results, row)
					}
				} else {
					owners = append(owners, row)
				}
			}
			if !busyVisibleAnswer(row) {
				continue
			}
			n := len(value.FindAllStringIndex(row.Content, -1))
			if n == 0 {
				continue
			}
			if len(e.Results) == 0 {
				e.Premature = append(e.Premature, row)
				continue
			}
			if e.First == nil {
				first := row
				e.First = &first
			}
			e.Mentions = append(e.Mentions, busyReportMention{Row: row, Occurrences: n, OwnerInputs: owners})
			owners = nil
		}
		e.Status = "PASS"
		if len(e.Results) != 1 || e.First == nil || len(e.Premature) > 0 {
			e.Status = "FAIL"
		} else if len(e.Mentions) > 1 {
			e.Status = "REVIEW"
		}
	}
	for i := range out {
		for j := range out[i].Mentions {
			m := &out[i].Mentions[j]
			for _, other := range out {
				if other.First != nil && other.First.Seq == m.Row.Seq {
					m.NewResults = append(m.NewResults, other.Task)
				}
			}
		}
	}
	return out
}

func busyVisibleAnswer(row Row) bool {
	text := strings.TrimSpace(row.Content)
	return row.Role == "assistant" && !row.Internal && row.Branch == "" && len(row.ToolCalls) == 0 && text != "" && text != "SILENT" && busyNormalText(text)
}

func (e busyReportEvidence) occurrences() int {
	n := 0
	for _, m := range e.Mentions {
		n += m.Occurrences
	}
	return n
}

// TestBusyRescoreSavedTranscripts does no HTTP, model calls or server startup.
// The opt-in paths contain only saved artifacts and the derived evidence JSON.
func TestBusyRescoreSavedTranscripts(t *testing.T) {
	dir := os.Getenv("XBOT_BUSY_RESCORE_DIR")
	if dir == "" {
		t.Skip("offline rescore needs XBOT_BUSY_RESCORE_DIR")
	}
	all := map[string][]busyReportEvidence{}
	for n := 1; n <= 6; n++ {
		scene := fmt.Sprintf("s%d", n)
		raw, err := os.ReadFile(filepath.Join(dir, "artifacts", scene, scene+"-transcript.json"))
		if err != nil {
			t.Fatal(err)
		}
		var rows []Row
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		tasks := []string{"BUSY_" + scene + "_A", "BUSY_" + scene + "_B1"}
		if n == 4 {
			for b := 2; b <= 5; b++ {
				tasks = append(tasks, fmt.Sprintf("BUSY_s4_B%d", b))
			}
		}
		all[scene] = busyReports(rows, tasks)
		for _, e := range all[scene] {
			t.Logf("%s: %s (result events=%d, normal mentions=%d)", e.Task, e.Status, len(e.Results), len(e.Mentions))
			if e.Status == "FAIL" {
				t.Errorf("%s: missing/early report or result event count != 1", e.Task)
			}
		}
	}
	output := os.Getenv("XBOT_BUSY_RESCORE_OUTPUT")
	if output == "" {
		t.Fatal("XBOT_BUSY_RESCORE_OUTPUT is required; original artifacts must not be overwritten")
	}
	raw, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		t.Fatal(err)
	}
	t.Log("Offline factual checks only; REVIEW requires the supervisor's semantic decision, not an automatic PASS.")
}
