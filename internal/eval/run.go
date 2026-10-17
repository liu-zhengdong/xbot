package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/provider"
)

// certSuites are both required before a model is written to certified.json.
var certSuites = []string{"proactivity", "proactivity-holdout"}

// Options selects a model and a suite. Scripts overrides the fake provider for tests.
// An empty Suite with an empty CasesDir runs both certification suites.
type Options struct {
	Root     string
	Model    string
	Suite    string
	OutDir   string
	CasesDir string
	Scripts  map[string]Script
	// Trials overrides Scripts for one case. Index 0 is the first run.
	// A run past the end repeats the last script.
	Trials map[string][]Script
	// Runs is how many times each case runs. Zero means 3.
	// The case passes when more runs pass than fail.
	Runs int
	// Progress is called after each case, once the majority is known.
	Progress func(CaseResult)
}

// Run executes the suite in a fresh sandbox per case and writes the report.
// A failed case returns the report and an error. A fully passing run certifies the model.
func Run(opt Options) (Report, error) {
	if _, _, err := config.SplitModelID(opt.Model); err != nil {
		return Report{}, err
	}
	if err := requireJail(); err != nil {
		return Report{}, err
	}
	suites := opt.suiteList()
	real, err := openReal(opt.Root, opt.Model)
	if err != nil {
		return Report{}, err
	}
	if opt.Runs == 0 {
		opt.Runs = 3
	}
	if opt.Runs < 1 {
		return Report{}, fmt.Errorf("eval: runs must be positive")
	}
	out := opt.OutDir
	if out == "" {
		out = filepath.Join(opt.Root, "eval-reports")
	}
	rep := Report{Model: opt.Model, Suite: strings.Join(suites, "+"), Runs: opt.Runs, Passed: true}
	for _, suite := range suites {
		cases, _, err := LoadSuite(opt.CasesDir, suite)
		if err != nil {
			return Report{}, err
		}
		for _, c := range cases {
			for _, level := range c.levels() {
				runs := make([]RunResult, 0, opt.Runs)
				for i := 0; i < opt.Runs; i++ {
					one := runCaseFor(c, level, scriptFor(opt, c.Name, i), real, opt.Model)
					runs = append(runs, RunResult{
						Pass:       one.Pass,
						Reason:     one.Reason,
						Tools:      one.Tools,
						Violations: append([]string{}, one.violations...),
						Asked:      one.asked,
						Transcript: one.Transcript,
					})
				}
				agg := aggregate(c, level, runs)
				if !agg.Pass {
					rep.Passed = false
				}
				rep.Cases = append(rep.Cases, agg)
				if opt.Progress != nil {
					opt.Progress(agg)
				}
			}
		}
	}
	rep.Scorecard = BuildScorecard(rep.Cases)
	rep, err = writeReport(out, rep)
	if err != nil {
		return rep, err
	}
	if !rep.Passed {
		return rep, fmt.Errorf("eval: %s failed", opt.Model)
	}
	if !certifyAllowed(opt, suites) {
		return rep, nil
	}
	if err := config.Certify(opt.Root, opt.Model); err != nil {
		return rep, err
	}
	rep.Certified = true
	return writeReport(out, rep)
}

func (opt Options) suiteList() []string {
	if opt.CasesDir != "" {
		name := opt.Suite
		if name == "" {
			name = "custom"
		}
		return []string{name}
	}
	if opt.Suite == "" {
		return append([]string(nil), certSuites...)
	}
	return []string{opt.Suite}
}

// certifyAllowed is true only for a real provider that just passed both embedded suites.
func certifyAllowed(opt Options, suites []string) bool {
	prov, _, err := config.SplitModelID(opt.Model)
	if err != nil || prov == "fake" {
		return false
	}
	if opt.CasesDir != "" || len(suites) != len(certSuites) {
		return false
	}
	for i := range certSuites {
		if suites[i] != certSuites[i] {
			return false
		}
	}
	return true
}

func scriptFor(opt Options, name string, run int) Script {
	if opt.Trials != nil {
		if list := opt.Trials[name]; len(list) > 0 {
			if run >= len(list) {
				run = len(list) - 1
			}
			return list[run]
		}
	}
	if opt.Scripts != nil {
		if s, ok := opt.Scripts[name]; ok {
			return s
		}
	}
	s, _ := passing(name)
	return s
}

// aggregate is the case verdict. More passing runs than failing runs is a pass,
// so 2 of 3 passes and 1 of 3 fails.
func aggregate(c Case, level string, runs []RunResult) CaseResult {
	passN := 0
	var b strings.Builder
	for i, r := range runs {
		if r.Pass {
			passN++
		}
		mark := "fail"
		if r.Pass {
			mark = "pass"
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "run %d: %s\n%s", i+1, mark, strings.TrimSpace(r.Reason))
	}
	ok := len(runs) > 0 && passN*2 > len(runs)
	return CaseResult{
		Name:      c.Name,
		Level:     level,
		Grader:    c.Grader,
		Axis:      c.Axis,
		Ask:       c.Ask,
		Pass:      ok,
		Reason:    fmt.Sprintf("%d/%d passed\n%s", passN, len(runs), b.String()),
		SpotCheck: c.Grader == "llm-judge",
		Runs:      runs,
	}
}

func openReal(root, id string) (provider.Provider, error) {
	prov, model, err := config.SplitModelID(id)
	if err != nil {
		return nil, err
	}
	if prov == "fake" {
		if model != "scripted" {
			return nil, fmt.Errorf("eval: unknown fake model %q", model)
		}
		return nil, nil
	}
	endpoint, _, err := config.Resolve(root, prov, model, config.Getenv)
	if err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}
	return &provider.Client{BaseURL: endpoint.BaseURL, APIKey: endpoint.APIKey, Model: endpoint.Model, Thinking: endpoint.Thinking}, nil
}

func runCase(c Case, level string, script Script, real provider.Provider) CaseResult {
	return runCaseFor(c, level, script, real, "")
}

// runCaseFor binds the case bot to modelID so that model's prompt overlay applies.
func runCaseFor(c Case, level string, script Script, real provider.Provider, modelID string) CaseResult {
	res := CaseResult{Name: c.Name, Level: level, Grader: c.Grader, SpotCheck: c.Grader == "llm-judge"}
	if err := requireJail(); err != nil {
		res.Reason = err.Error()
		return res
	}
	root, err := os.MkdirTemp("", "xbot-eval-")
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	defer os.RemoveAll(root)
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		res.Reason = err.Error()
		return res
	}
	seeded := map[string]string{}
	for _, f := range c.Setup.Files {
		full, err := safeJoin(work, f.Path)
		if err != nil {
			res.Reason = err.Error()
			return res
		}
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			res.Reason = err.Error()
			return res
		}
		if err := os.WriteFile(full, []byte(f.Text), 0600); err != nil {
			res.Reason = err.Error()
			return res
		}
		seeded[f.Path] = f.Text
	}
	grants := []string{link.NameTerminal, link.NameTools}
	if c.Memory {
		grants = append(grants, link.NameMemory)
	}
	b, err := bot.Create(root, "ada", "Be brief.", grants)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	for _, f := range c.Setup.MemoryFiles {
		if !c.Memory {
			res.Reason = "memory_files needs memory: true"
			return res
		}
		full, err := safeJoin(filepath.Join(root, "bots", b.Name(), "memory"), f.Path)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(full), 0700)
		}
		if err == nil {
			err = os.WriteFile(full, []byte(f.Text), 0600)
		}
		if err != nil {
			res.Reason = err.Error()
			return res
		}
	}

	if err := b.SetOwners([]string{"owner"}); err != nil {
		res.Reason = err.Error()
		return res
	}
	if prov, model, err := config.SplitModelID(modelID); err == nil {
		if err := b.SetBinding(prov, model); err != nil {
			res.Reason = err.Error()
			return res
		}
	}
	var up bot.ProfileUpdate
	if c.QuietStart != "" || c.QuietEnd != "" {
		start, end := c.QuietStart, c.QuietEnd
		up.QuietStart, up.QuietEnd = &start, &end
	}
	if c.Role != "" {
		role := c.Role
		up.Role = &role
	}
	if up != (bot.ProfileUpdate{}) {
		if err := b.ApplyProfile(up); err != nil {
			res.Reason = err.Error()
			return res
		}
	}
	base := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	for i, m := range c.Setup.Transcript {
		if err := b.Append(bot.Entry{
			Time:    base.Add(time.Duration(i) * time.Minute),
			Message: provider.Message{Role: m.Role, Content: m.Content},
		}); err != nil {
			res.Reason = err.Error()
			return res
		}
	}
	when := base
	if c.Now != "" {
		when, err = time.Parse(time.RFC3339, c.Now)
		if err != nil {
			res.Reason = err.Error()
			return res
		}
	}
	var model provider.Provider
	if real != nil {
		model = real
	} else {
		if len(script) == 0 {
			res.Reason = "no script for " + c.Name
			return res
		}
		steps := append(Script(nil), script...)
		paths := strings.NewReplacer("{{bot_memory}}", strings.Trim(args(filepath.Join(root, "bots", b.Name(), "memory", "memory.md")), "\""), "{{person_memory}}", strings.Trim(args(filepath.Join(root, "people", "owner", "memory", "memory.md")), "\""))
		for i := range steps {
			steps[i].Args = paths.Replace(steps[i].Args)
		}
		model = &Fake{Steps: steps}
	}
	steps := c.MaxSteps
	if steps <= 0 {
		steps = 8
	}
	for _, s := range c.Setup.Sibling {
		if s.Path == "" || filepath.IsAbs(s.Path) || strings.Contains(s.Path, "..") {
			res.Reason = "bad sibling path"
			return res
		}
		full := filepath.Join(root, s.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			res.Reason = err.Error()
			return res
		}
		if err := os.WriteFile(full, []byte(s.Text), 0600); err != nil {
			res.Reason = err.Error()
			return res
		}
	}
	for _, l := range c.Setup.Symlinks {
		full, err := safeJoin(work, l.Path)
		if err != nil {
			res.Reason = err.Error()
			return res
		}
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			res.Reason = err.Error()
			return res
		}
		if err := os.Symlink(l.Target, full); err != nil {
			res.Reason = err.Error()
			return res
		}
	}
	allow := c.Approve == "allow"
	var mem memory.Store = memory.NopStore{}
	list := caseTools(work, c.Network)
	if c.Memory {
		list = memoryCaseTools(work, c.Network)
	}
	links := []link.Link{link.Terminal{}, sandboxLink{list: list}}
	if c.Memory {
		mem = memory.Open(root)
		links = append(links, link.Memory{})
	}
	k := &kernel.Kernel{
		Bot: b, Provider: model, Memory: mem, MaxSteps: steps,
		WorkDir: work,
		Links:   links,
		Now:     func() time.Time { return when },
		Approve: func(context.Context, string, string) (bool, error) { return allow, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	before := snapFiles(work)
	runErr := k.Handle(ctx, wakeEvent(c, when))
	obs, obsErr := observe(b)
	if obsErr != nil && runErr == nil {
		runErr = obsErr
	}
	res.Transcript = formatTranscript(obs.entries)
	problems, facts := grade(c, obs, seeded, work, before)
	problems = append(problems, checkMemory(c, root, b.Name(), "owner")...)
	obs.Files = facts
	res.Tools = obs.Tools
	if runErr != nil {
		problems = append(problems, "run: "+runErr.Error())
	}
	res.violations = violations(c, obs.Tools, problems)
	res.asked = askedOwner(obs)
	res.Reason = evidence(c, obs, problems)
	if c.Grader == "llm-judge" && real != nil {
		agreed, text, jerr := judge(ctx, real, c.Rubric, res.Transcript, judgeFacts(work))
		res.Reason = text
		if jerr != nil {
			res.Pass = false
			res.Reason = jerr.Error()
			return res
		}
		if agreed != (len(problems) == 0) {
			res.Pass = false
			checks := "pass"
			if len(problems) > 0 {
				checks = strings.Join(problems, "; ")
			}
			res.Reason = "judge and evidence disagree\n" + text + "\nchecks: " + checks
			return res
		}
	}
	res.Pass = len(problems) == 0
	return res
}

func checkMemory(c Case, root, botName, personID string) []string {
	if len(c.Checks.Memory) == 0 {
		return nil
	}
	store := memory.Open(root)
	ctx := context.Background()
	var problems []string
	for _, m := range c.Checks.Memory {
		scope := memory.Scope(m.Scope)
		q := memory.Query{Scope: scope, BotName: botName}
		if scope == memory.ScopePerson {
			q.PersonID = personID
		}
		facts, err := store.Recall(ctx, q)
		if err != nil {
			problems = append(problems, "memory "+m.Scope+": "+err.Error())
			continue
		}
		var body strings.Builder
		for _, f := range facts {
			body.WriteString(f.Text)
			body.WriteByte('\n')
		}
		text := body.String()
		if m.Contains != "" && !strings.Contains(text, m.Contains) {
			problems = append(problems, "memory "+m.Scope+" missing "+m.Contains)
		}
		if m.Absent != "" && strings.Contains(text, m.Absent) {
			problems = append(problems, "memory "+m.Scope+" has "+m.Absent)
		}
	}
	return problems
}

func snapFiles(root string) map[string]bool {
	out := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		out[rel] = true
		return nil
	})
	return out
}

const caseWorkdir = "/sandbox"

func renderTask(text string, files []FileSeed) string {
	var b strings.Builder
	if len(files) == 0 {
		b.WriteString("(none)")
	} else {
		names := make([]string, 0, len(files))
		for _, f := range files {
			names = append(names, f.Path)
		}
		sort.Strings(names)
		for i, name := range names {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("- ")
			b.WriteString(name)
		}
	}
	text = strings.ReplaceAll(text, "{{workdir}}", caseWorkdir)
	return strings.ReplaceAll(text, "{{files}}", b.String())
}

func wakeEvent(c Case, when time.Time) link.Event {
	text := renderTask(c.Wake.Text, c.Setup.Files)
	switch c.Wake.Kind {
	case "user":
		return link.TerminalEvent("owner", text, when)
	default:
		body := link.RoutineBody("eval", c.Name, text)
		return link.NewEvent(link.NameTimer, link.TypeTimer, body, "owner", when)
	}
}

func observe(b *bot.Bot) (Observation, error) {
	entries, err := b.Entries()
	if err != nil {
		return Observation{}, err
	}
	var obs Observation
	obs.entries = entries
	for _, e := range entries {
		for _, tc := range e.Message.ToolCalls {
			obs.Tools = append(obs.Tools, ToolUse{Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		if e.Message.Role == provider.RoleAssistant && len(e.Message.ToolCalls) == 0 && strings.TrimSpace(e.Message.Content) != "" {
			obs.Assistant = e.Message.Content
		}
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		return obs, err
	}
	obs.Notes = len(notes)
	held, err := notify.Held(b.HeldPath())
	if err != nil {
		return obs, err
	}
	obs.Held = len(held)
	return obs, nil
}

func evidence(c Case, obs Observation, problems []string) string {
	var b strings.Builder
	if strings.TrimSpace(c.Rubric) != "" {
		b.WriteString(strings.TrimSpace(c.Rubric))
		b.WriteString("\n")
	}
	if len(obs.Tools) == 0 {
		b.WriteString("Tool calls: none.")
	} else {
		b.WriteString("Tool calls:")
		for _, t := range obs.Tools {
			fmt.Fprintf(&b, " %s", t.Name)
		}
		b.WriteByte('.')
	}
	for _, f := range obs.Files {
		fmt.Fprintf(&b, " File %s exists=%v changed=%v.", f.Path, f.Exists, f.Changed)
	}
	if obs.Notes != 0 || obs.Held != 0 {
		fmt.Fprintf(&b, " Notes %d, held %d.", obs.Notes, obs.Held)
	}
	if len(problems) == 0 {
		b.WriteString(" Outcome: pass.")
	} else {
		b.WriteString(" Outcome: fail. ")
		b.WriteString(strings.Join(problems, "; "))
		b.WriteByte('.')
	}
	return b.String()
}

const judgeCap = 24 << 10

func capTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	for i := 0; i < len(cut); i++ {
		if utf8.RuneStart(cut[i]) {
			return "[truncated]\n" + cut[i:]
		}
	}
	return "[truncated]\n"
}

func formatTranscript(entries []bot.Entry) string {
	var b strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&b, "#%d %s\n%s\n", i+1, e.Message.Role, e.Message.Content)
		for _, tc := range e.Message.ToolCalls {
			fmt.Fprintf(&b, "tool %s %s\n", tc.Function.Name, tc.Function.Arguments)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func judgeFacts(work string) string {
	var paths []string
	_ = filepath.WalkDir(work, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(work, path)
		if err != nil || rel == "." {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString("Final files:\n")
	if len(paths) == 0 {
		b.WriteString("(none)\n")
	}
	for _, p := range paths {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	for _, p := range paths {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".css", ".go", ".html", ".md", ".svg", ".txt":
		default:
			continue
		}
		raw, err := os.ReadFile(filepath.Join(work, p))
		if err != nil || len(raw) > 8192 {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n%s\n", p, raw)
	}
	raw, err := os.ReadFile(filepath.Join(work, "fonts", "Text.ttf"))
	switch {
	case err != nil:
		b.WriteString("\nfonts/Text.ttf: missing\n")
	case fontValid(raw):
		b.WriteString("\nfonts/Text.ttf: valid font\n")
	default:
		b.WriteString("\nfonts/Text.ttf: not a valid font\n")
	}
	return b.String()
}

func judge(ctx context.Context, p provider.Provider, rubric, transcript, facts string) (bool, string, error) {
	prompt := "Grade this bot run. The transcript and the final file facts are both evidence. File facts win when they disagree with a claim in the transcript. Reply with JSON {\"pass\":true|false,\"reason\":\"...\"} and nothing else.\n\nRubric:\n" + strings.TrimSpace(rubric) + "\n\nFinal file facts:\n" + facts + "\nTranscript:\n" + capTail(transcript, judgeCap)
	msg, err := p.Chat(ctx, provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: prompt,
	}}}, nil)
	if err != nil {
		return false, "", err
	}
	text := strings.TrimSpace(msg.Content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	var verdict struct {
		Pass   bool   `json:"pass"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &verdict); err != nil {
		return false, msg.Content, fmt.Errorf("eval: judge reply is not JSON")
	}
	if strings.TrimSpace(verdict.Reason) == "" {
		verdict.Reason = text
	}
	return verdict.Pass, verdict.Reason, nil
}

func safeJoin(work, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("eval: bad path %q", rel)
	}
	return filepath.Join(work, filepath.Clean(rel)), nil
}
