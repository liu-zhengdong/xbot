package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/fsutil"
)

// Report is the JSON and Markdown written for one model.
type Report struct {
	Model     string       `json:"model"`
	Suite     string       `json:"suite"`
	Runs      int          `json:"runs,omitempty"`
	Passed    bool         `json:"passed"`
	Certified bool         `json:"certified,omitempty"`
	Cases     []CaseResult `json:"cases"`
	JSON      string       `json:"-"`
	Markdown  string       `json:"-"`
}

// CaseResult is one case at one proactivity level.
// Pass is the majority of Runs. Reason lists every run.
type CaseResult struct {
	Name           string      `json:"name"`
	Level          string      `json:"level"`
	Grader         string      `json:"grader"`
	Pass           bool        `json:"pass"`
	Reason         string      `json:"reason"`
	SpotCheck      bool        `json:"spot_check,omitempty"`
	Runs           []RunResult `json:"runs,omitempty"`
	Tools          []ToolUse   `json:"tools,omitempty"`
	Transcript     string      `json:"-"`
	TranscriptFile string      `json:"transcript,omitempty"`
}

// RunResult is one execution of a case. Pass and Reason are that run alone.
type RunResult struct {
	Pass           bool      `json:"pass"`
	Reason         string    `json:"reason"`
	Tools          []ToolUse `json:"tools,omitempty"`
	Transcript     string    `json:"-"`
	TranscriptFile string    `json:"transcript,omitempty"`
}

func writeReport(dir string, rep Report) (Report, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return rep, err
	}
	safe := strings.ReplaceAll(rep.Model, "/", "__")
	rep.JSON = filepath.Join(dir, safe+".json")
	rep.Markdown = filepath.Join(dir, safe+".md")
	td := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(td, 0700); err != nil {
		return rep, err
	}
	for i, c := range rep.Cases {
		for j, run := range c.Runs {
			name := fmt.Sprintf("%s__%s__%s__%d.txt", safe, c.Name, c.Level, j+1)
			path := filepath.Join(td, name)
			body := run.Transcript
			if strings.TrimSpace(body) == "" {
				body = "(empty)\n"
			}
			if err := fsutil.WriteAtomic(path, []byte(body), 0600); err != nil {
				return rep, err
			}
			rep.Cases[i].Runs[j].TranscriptFile = path
		}
		if len(rep.Cases[i].Runs) > 0 {
			rep.Cases[i].Transcript = rep.Cases[i].Runs[0].Transcript
			rep.Cases[i].TranscriptFile = rep.Cases[i].Runs[0].TranscriptFile
		}
	}
	raw, err := fsutil.Marshal(rep)
	if err != nil {
		return rep, err
	}
	if err := fsutil.WriteAtomic(rep.JSON, raw, 0600); err != nil {
		return rep, err
	}
	if err := fsutil.WriteAtomic(rep.Markdown, []byte(markdown(rep)), 0600); err != nil {
		return rep, err
	}
	return rep, nil
}

func markdown(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Proactivity eval: %s\n\n", rep.Model)
	if rep.Passed {
		b.WriteString("Passed.\n\n")
	} else {
		b.WriteString("Failed.\n\n")
	}
	b.WriteString("| Case | Level | Grader | Result |\n| --- | --- | --- | --- |\n")
	for _, c := range rep.Cases {
		mark := "fail"
		if c.Pass {
			mark = "pass"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Name, c.Level, c.Grader, mark)
	}
	for _, c := range rep.Cases {
		fmt.Fprintf(&b, "\n## %s (%s)\n\n%s\n", c.Name, c.Level, strings.TrimSpace(c.Reason))
		for i, run := range c.Runs {
			if run.TranscriptFile != "" {
				fmt.Fprintf(&b, "\nRun %d transcript: %s\n", i+1, run.TranscriptFile)
			}
		}
		if c.SpotCheck {
			b.WriteString("\nSaved for a human spot check.\n")
		}
	}
	b.WriteByte('\n')
	return b.String()
}
