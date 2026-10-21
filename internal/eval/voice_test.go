package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xbot/internal/tools"
)

const badVoice = "w1 做完了，骨架建好了：backlog.md、README.md、projects/xbot.md 和 designs/ 都在 ~/.dev/，你现有的仓库一个没动。值得留意：w1 全局装了 tasks-axi@0.4.2，以后直接敲 tasks-axi list 就行。不想要的话说一声，我让它卸掉。"
const expectedVoice = "骨架建好了，你现有的仓库一个都没碰。我顺手全局装了 tasks-axi，以后直接用就行，不想要的话说一声我就卸掉。"

func TestVoiceCounterRejectsTechnicalLog(t *testing.T) {
	bad := CountVoice(badVoice, nil)
	if bad.Paths != 5 || bad.Internals != 2 || bad.Code != 0 {
		t.Fatalf("bad=%+v", bad)
	}
	if got := CountVoice(expectedVoice, nil); got != (VoiceStats{}) {
		t.Fatalf("expected=%+v", got)
	}
	c := CountVoice("bg_a sa_22 read_file `~/a.md` ```go\nx\n```", []string{"read_file", "read_file"})
	if c.Paths != 1 || c.Internals != 3 || c.Code != 2 {
		t.Fatalf("counts=%+v", c)
	}
	if len(gradeVoice(&VoiceCheck{}, []VoiceStats{bad})) == 0 {
		t.Fatal("bad example passed")
	}
}
func TestVoiceSuiteUsesFakeAndRecordsActivity(t *testing.T) {
	rep, err := Run(Options{Root: t.TempDir(), Model: "fake/scripted", Suite: "voice", Runs: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Cases {
		r := c.Runs[0]
		if c.Name == "quick-fix" {
			if r.Activity.Steps != 3 || r.Activity.ToolsBeforeFirstWrite == nil || *r.Activity.ToolsBeforeFirstWrite != 1 {
				t.Fatalf("activity=%+v", r.Activity)
			}
		} else if c.Name == "plain-report" {
			if r.Activity.Steps != 1 || r.Activity.ToolsBeforeFirstWrite != nil || len(r.Voice) != 1 {
				t.Fatalf("report=%+v", r)
			}
		}
	}
	md, _ := os.ReadFile(rep.Markdown)
	if !strings.Contains(string(md), "说话") {
		t.Fatal(string(md))
	}
}
func TestActivityDetectsFileChangesNotJustWriteToolNames(t *testing.T) {
	root := t.TempDir()
	a := newActivity(root, &Fake{Steps: Script{{Text: "done"}}})
	a.toolStart("shell", "")
	a.toolEnd("shell", "failed")
	if a.result().ToolsBeforeFirstWrite != nil {
		t.Fatal("failed call counted as write")
	}
	a.toolStart("shell", "")
	if err := os.WriteFile(filepath.Join(root, "out"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	a.toolEnd("shell", "exit 0")
	if got := a.result().ToolsBeforeFirstWrite; got == nil || *got != 1 {
		t.Fatal(got)
	}
}
func TestVoiceCaseReferenceMatchesRequestedReply(t *testing.T) {
	cases, _, err := LoadSuite("", "voice")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Checks.Voice != nil && c.Checks.Voice.Reference != expectedVoice {
			t.Fatal(c.Checks.Voice.Reference)
		}
	}
}

func TestVoiceCountsUnicodeAndWindowsPaths(t *testing.T) {
	if got := CountVoice("`~/项目/记录.md` 与 `C:\\work\\note.txt`", nil); got.Paths != 2 || got.Code != 2 {
		t.Fatal(got)
	}
}

func TestFileOnlyToolsExcludeShellRegardlessOfCatalogOrder(t *testing.T) {
	all := caseTools(t.TempDir(), false)
	for shift := 0; shift < len(all); shift++ {
		list := append(append([]tools.Tool{}, all[shift:]...), all[:shift]...)
		got := fileOnlyTools(list)
		names := map[string]bool{}
		for _, tool := range got {
			names[tool.Name()] = true
		}
		if len(got) != 3 || names["shell"] || !names["read_file"] || !names["write_file"] || !names["edit_file"] {
			t.Fatalf("catalog shift %d gave %v", shift, names)
		}
	}
}
