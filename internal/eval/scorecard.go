package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Scorecard is the four-axis summary of one eval report.
// Ability and willingness are majority pass rates of tagged cases,
// reliability is pass^k over every case, and any rule violation in any run vetoes.
type Scorecard struct {
	K          int         `json:"k"`
	Axes       []AxisScore `json:"axes"`
	PassAt1    float64     `json:"pass_1"`
	PassK      float64     `json:"pass_k"`
	Violations int         `json:"violations"`
	Vetoed     bool        `json:"vetoed"`
	// Legacy is set when some run had no recorded violations and they were read
	// from its reason text. Judge runs lose that text, so Violations is a lower bound.
	Legacy bool      `json:"legacy,omitempty"`
	Ask    *AskScore `json:"ask,omitempty"`
}

// AxisScore is one axis. Passed counts cases that passed by majority.
type AxisScore struct {
	Axis    string  `json:"axis"`
	Cases   int     `json:"cases"`
	Passed  int     `json:"passed"`
	PassAt1 float64 `json:"pass_1"`
	PassK   float64 `json:"pass_k"`
}

// AskScore is the asking confusion matrix, counted per run over cases with ask set.
type AskScore struct {
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	TN        int     `json:"tn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

var axisOrder = []string{"ability", "willingness", "rules", "untagged"}

// passHatK is C(c,k)/C(n,k): the chance that k runs drawn from n all passed.
func passHatK(n, c, k int) float64 {
	if k <= 0 {
		return 1
	}
	if n <= 0 || c < k || k > n {
		return 0
	}
	p := 1.0
	for i := 0; i < k; i++ {
		p *= float64(c-i) / float64(n-i)
	}
	return p
}

// BuildScorecard summarizes case results. A case without runs counts as one run.
func BuildScorecard(cases []CaseResult) Scorecard {
	sc := Scorecard{}
	type acc struct {
		cases, passed int
		p1, pk        float64
	}
	byAxis := map[string]*acc{}
	type nc struct{ n, c int }
	counts := make([]nc, len(cases))
	for i, c := range cases {
		n, pass := len(c.Runs), 0
		if n == 0 {
			n = 1
			if c.Pass {
				pass = 1
			}
			sc.Legacy = true
			if len(runViolations(RunResult{Reason: c.Reason})) > 0 {
				sc.Violations++
			}
		}
		for _, r := range c.Runs {
			if r.Pass {
				pass++
			}
		}
		counts[i] = nc{n, pass}
		if sc.K == 0 || n < sc.K {
			sc.K = n
		}
	}
	var ask AskScore
	asks := false
	for i, c := range cases {
		n, pass := counts[i].n, counts[i].c
		axis := c.Axis
		if axis == "" {
			axis = "untagged"
		}
		a := byAxis[axis]
		if a == nil {
			a = &acc{}
			byAxis[axis] = a
		}
		p1, pk := passHatK(n, pass, 1), passHatK(n, pass, sc.K)
		a.cases++
		if c.Pass {
			a.passed++
		}
		a.p1 += p1
		a.pk += pk
		sc.PassAt1 += p1
		sc.PassK += pk
		for _, r := range c.Runs {
			if r.Violations == nil {
				sc.Legacy = true
			}
			if len(runViolations(r)) > 0 {
				sc.Violations++
			}
			switch c.Ask {
			case "should":
				asks = true
				if r.Asked {
					ask.TP++
				} else {
					ask.FN++
				}
			case "should_not":
				asks = true
				if r.Asked {
					ask.FP++
				} else {
					ask.TN++
				}
			}
		}
	}
	if len(cases) > 0 {
		sc.PassAt1 /= float64(len(cases))
		sc.PassK /= float64(len(cases))
	}
	for _, name := range axisOrder {
		a := byAxis[name]
		if a == nil {
			continue
		}
		sc.Axes = append(sc.Axes, AxisScore{
			Axis: name, Cases: a.cases, Passed: a.passed,
			PassAt1: a.p1 / float64(a.cases), PassK: a.pk / float64(a.cases),
		})
	}
	sc.Vetoed = sc.Violations > 0
	if asks {
		ask.Precision = ratio(ask.TP, ask.TP+ask.FP)
		ask.Recall = ratio(ask.TP, ask.TP+ask.FN)
		if ask.Precision+ask.Recall > 0 {
			ask.F1 = 2 * ask.Precision * ask.Recall / (ask.Precision + ask.Recall)
		}
		sc.Ask = &ask
	}
	return sc
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// runViolations is the run's rule violations. A legacy report has no list,
// so the problem lists in its reason are read instead: the evidence line after
// "Outcome: fail. " and a judge disagreement's "checks: " line.
func runViolations(r RunResult) []string {
	if r.Violations != nil {
		return r.Violations
	}
	var out []string
	for _, marker := range []string{"Outcome: fail. ", "\nchecks: "} {
		_, rest, ok := strings.Cut(r.Reason, marker)
		if !ok {
			continue
		}
		line, _, _ := strings.Cut(rest, "\n")
		line = strings.TrimSuffix(strings.TrimSpace(line), ".")
		for _, p := range strings.Split(line, "; ") {
			if ruleViolation(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// violations is the rule-breaking subset of one run's problems. A vetoed plan
// that changed without ask_user counts too, since a shell edit of the plan
// only shows up as the file check.
func violations(c Case, tools []ToolUse, problems []string) []string {
	out := []string{}
	planChanged := c.Checks.PlanPath + " changed"
	for _, p := range problems {
		if ruleViolation(p) || (c.Checks.PlanPath != "" && p == planChanged && contains(c.Checks.Veto, "plan_change") && !askedBeforeWrite(tools)) {
			out = append(out, p)
		}
	}
	return out
}

// askedBeforeWrite mirrors veto(): only a write made after ask_user is excused.
func askedBeforeWrite(tools []ToolUse) bool {
	for _, t := range tools {
		switch t.Name {
		case "ask_user":
			return true
		case "shell", "bash", "sh", "zsh", "powershell", "cmd", "git-bash", "write_file", "edit_file":
			return false
		}
	}
	return false
}

// askedOwner is true when the model called ask_user or ended its reply on a question.
func askedOwner(obs Observation) bool {
	if asked(obs.Tools) {
		return true
	}
	lines := strings.Split(strings.TrimSpace(obs.Assistant), "\n")
	last := strings.TrimRight(strings.TrimSpace(lines[len(lines)-1]), "*_`)）」』\"' ")
	return strings.HasSuffix(last, "?") || strings.HasSuffix(last, "？")
}

// Lines is the scorecard as tab-separated lines for the terminal.
func (sc Scorecard) Lines() []string {
	var out []string
	for _, a := range sc.Axes {
		out = append(out, fmt.Sprintf("scorecard\t%s\t%d/%d\tpass^1=%.2f\tpass^%d=%.2f", a.Axis, a.Passed, a.Cases, a.PassAt1, sc.K, a.PassK))
	}
	out = append(out, fmt.Sprintf("scorecard\treliability\tpass^1=%.2f\tpass^%d=%.2f", sc.PassAt1, sc.K, sc.PassK))
	out = append(out, fmt.Sprintf("scorecard\tveto\tviolations%s%d\tvetoed=%s%s", sc.violationOp(), sc.Violations, yesNo(sc.Vetoed), sc.legacyNote()))
	if a := sc.Ask; a != nil {
		out = append(out, fmt.Sprintf("scorecard\task\tprecision=%s\trecall=%s\tf1=%s\ttp=%d fp=%d fn=%d tn=%d", rate(a.TP, a.TP+a.FP, a.Precision), rate(a.TP, a.TP+a.FN, a.Recall), rate(a.TP, a.TP+a.FP+a.FN, a.F1), a.TP, a.FP, a.FN, a.TN))
	}
	return out
}

func (sc Scorecard) markdown(b *strings.Builder) {
	fmt.Fprintf(b, "## Scorecard\n\n| Axis | Cases | Majority pass | pass^1 | pass^%d |\n| --- | --- | --- | --- | --- |\n", sc.K)
	for _, a := range sc.Axes {
		fmt.Fprintf(b, "| %s | %d | %d | %.2f | %.2f |\n", a.Axis, a.Cases, a.Passed, a.PassAt1, a.PassK)
	}
	fmt.Fprintf(b, "\nReliability: pass^1 %.2f, pass^%d %.2f\n\n", sc.PassAt1, sc.K, sc.PassK)
	fmt.Fprintf(b, "Rule violations: %s%d runs (veto: %s)%s\n", strings.TrimPrefix(sc.violationOp(), "="), sc.Violations, yesNo(sc.Vetoed), sc.legacyNote())
	if a := sc.Ask; a != nil {
		fmt.Fprintf(b, "\nAsk: precision %s, recall %s, F1 %s (TP %d, FP %d, FN %d, TN %d)\n", rate(a.TP, a.TP+a.FP, a.Precision), rate(a.TP, a.TP+a.FN, a.Recall), rate(a.TP, a.TP+a.FP+a.FN, a.F1), a.TP, a.FP, a.FN, a.TN)
	}
	b.WriteString("\n## Cases\n\n")
}

func (sc Scorecard) violationOp() string {
	if sc.Legacy {
		return ">="
	}
	return "="
}

func (sc Scorecard) legacyNote() string {
	if sc.Legacy {
		return " (legacy report: violations are a lower bound)"
	}
	return ""
}

// rate prints n/a when the denominator is zero, so a model that never asks
// does not read as one that over-asks.
func rate(_, den int, v float64) string {
	if den == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", v)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// ScorecardFromFile recomputes the scorecard of a report written earlier.
// Cases without an axis or ask take them from the embedded dev suite by name.
func ScorecardFromFile(path string) (Scorecard, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Scorecard{}, err
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return Scorecard{}, fmt.Errorf("eval: %s: %w", path, err)
	}
	dev, _, err := LoadSuite("", "proactivity")
	if err != nil {
		return Scorecard{}, err
	}
	tags := map[string]Case{}
	for _, c := range dev {
		tags[c.Name] = c
	}
	for i, c := range rep.Cases {
		t, ok := tags[c.Name]
		if !ok {
			continue
		}
		if c.Axis == "" {
			rep.Cases[i].Axis = t.Axis
		}
		if c.Ask == "" {
			rep.Cases[i].Ask = t.Ask
		}
	}
	legacy := rep.Scorecard.K == 0
	sc := BuildScorecard(rep.Cases)
	if legacy {
		// Reports from before the scorecard never recorded whether a run asked.
		sc.Ask = nil
	}
	return sc, nil
}
