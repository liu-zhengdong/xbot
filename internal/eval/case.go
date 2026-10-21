package eval

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"xbot/evals"

	"gopkg.in/yaml.v3"
)

// Case is one proactivity scenario. One file is one case.
type Case struct {
	Name    string   `yaml:"name"`
	Level   string   `yaml:"level"`
	Levels  []string `yaml:"levels"`
	Grader  string   `yaml:"grader"`
	Approve string   `yaml:"approve"`
	// Axis places the case on the scorecard: ability, willingness, or rules.
	Axis string `yaml:"axis"`
	// Ask says whether the model should ask the owner: should or should_not.
	Ask string `yaml:"ask"`
	// Network lets the jailed shell use the host network. Default is off.
	Network    bool   `yaml:"network"`
	Rubric     string `yaml:"rubric"`
	MaxSteps   int    `yaml:"max_steps"`
	Now        string `yaml:"now"`
	QuietStart string `yaml:"quiet_start"`
	QuietEnd   string `yaml:"quiet_end"`
	// Memory mounts the memory tools and a real file store for this case.
	Memory bool `yaml:"memory"`
	// Role is the bot working role, chat or dev. Empty stays chat.
	Role string `yaml:"role"`
	// Dispatch runs the bot as a dispatching foreground (ADR 0016). The
	// dispatch is recorded, not run, and the case has no shell, so it needs
	// no jail.
	Dispatch bool `yaml:"dispatch"`
	// FileOnly offers only sandboxed file tools, with no shell or network.
	FileOnly bool   `yaml:"file_only"`
	Wake     Wake   `yaml:"wake"`
	Setup    Setup  `yaml:"setup"`
	Checks   Checks `yaml:"checks"`
}

// Wake is the input. Kind is user, routine, or resume.
type Wake struct {
	Kind string `yaml:"kind"`
	Text string `yaml:"text"`
}

// Setup is the sandbox before the wake.
type Setup struct {
	MemoryFiles []FileSeed `yaml:"memory_files"` // Paths relative to this bot's memory folder.
	Files       []FileSeed `yaml:"files"`
	Transcript  []MsgSeed  `yaml:"transcript"`
	Symlinks    []Symlink  `yaml:"symlinks"`
	// Sibling files are written next to work, outside the sandbox. Tests use them.
	Sibling []FileSeed `yaml:"-"`
}

// FileSeed is one file under the work directory.
// File is a path relative to the suite directory, used for binary fixtures.
type FileSeed struct {
	Path string `yaml:"path"`
	Text string `yaml:"text"`
	File string `yaml:"file"`
}

// Symlink is created inside the sandbox. Target may point outside it.
type Symlink struct {
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
}

// MsgSeed is one transcript line already on the bot.
type MsgSeed struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

// Checks are outcome checks. The model's own summary is not a verdict.
type Checks struct {
	ToolResultContains  map[string][]string `yaml:"tool_result_contains"`
	Silent              bool                `yaml:"silent"`
	TranscriptNotSilent bool                `yaml:"transcript_not_silent"`
	AssistantContains   []string            `yaml:"assistant_contains"`
	Notes               *int                `yaml:"notes"`
	Held                *int                `yaml:"held"`
	MaxToolCalls        int                 `yaml:"max_tool_calls"`
	MinToolCalls        int                 `yaml:"min_tool_calls"`
	ForbidRepeat        int                 `yaml:"forbid_repeat"`
	ForbidTools         []string            `yaml:"forbid_tools"`
	Veto                []string            `yaml:"veto"`
	PlanPath            string              `yaml:"plan_path"`
	Files               []FileCheck         `yaml:"files"`
	AnyFiles            []FileCheck         `yaml:"any_files"`
	Memory              []MemoryCheck       `yaml:"memory"`
	Voice               *VoiceCheck         `yaml:"voice"`
	Dispatch            *DispatchCheck      `yaml:"dispatch"`
}

// DispatchCheck grades what a dispatching foreground handed out.
type DispatchCheck struct {
	// Type is the subagent type the work should go to.
	Type string `yaml:"type"`
	// TaskMentions must each appear in the task, case-insensitively; "a|b"
	// accepts either.
	TaskMentions []string `yaml:"task_mentions"`
	// None says the request needs no subagent at all.
	None bool `yaml:"none"`
}

// MemoryCheck is one fact that must or must not be stored after the run.
type MemoryCheck struct {
	Scope    string `yaml:"scope"`
	Contains string `yaml:"contains"`
	Absent   string `yaml:"absent"`
}

// FileCheck is one file under the work directory after the run.
type FileCheck struct {
	Path      string `yaml:"path"`
	Exists    *bool  `yaml:"exists"`
	Unchanged bool   `yaml:"unchanged"`
	Untouched bool   `yaml:"untouched"`
	Nonempty  bool   `yaml:"nonempty"`
	Contains  string `yaml:"contains"`
	// JSONFields checks parsed config values by dotted object path, not formatting.
	JSONFields map[string]any `yaml:"json_fields"`
	Equals     string         `yaml:"equals"`
	Font       bool           `yaml:"font"`
	CopyOf     string         `yaml:"copy_of"`
}

func (c Case) levels() []string {
	if len(c.Levels) > 0 {
		return append([]string(nil), c.Levels...)
	}
	if c.Level == "" {
		return []string{"normal"}
	}
	return []string{c.Level}
}

func (c Case) validate() error {
	if c.Name == "" {
		return fmt.Errorf("eval: case has no name")
	}
	switch c.Grader {
	case "auto", "llm-judge":
	default:
		return fmt.Errorf("eval: %s: grader %q", c.Name, c.Grader)
	}
	switch c.Approve {
	case "", "allow", "deny":
	default:
		return fmt.Errorf("eval: %s: approve %q", c.Name, c.Approve)
	}
	if c.Grader == "llm-judge" && strings.TrimSpace(c.Rubric) == "" {
		return fmt.Errorf("eval: %s: llm-judge needs a rubric", c.Name)
	}
	switch c.Role {
	case "", "chat", "dev":
	default:
		return fmt.Errorf("eval: %s: role %q", c.Name, c.Role)
	}
	switch c.Axis {
	case "", "ability", "willingness", "rules":
	default:
		return fmt.Errorf("eval: %s: axis %q", c.Name, c.Axis)
	}
	switch c.Ask {
	case "", "should", "should_not":
	default:
		return fmt.Errorf("eval: %s: ask %q", c.Name, c.Ask)
	}
	if len(c.Checks.Memory) > 0 && !c.Memory {
		return fmt.Errorf("eval: %s: memory checks need memory: true", c.Name)
	}
	switch c.Wake.Kind {
	case "user", "routine", "resume", "subagent":
	default:
		return fmt.Errorf("eval: %s: wake %q", c.Name, c.Wake.Kind)
	}
	if !strings.Contains(c.Wake.Text, "{{workdir}}") || !strings.Contains(c.Wake.Text, "{{files}}") {
		return fmt.Errorf("eval: %s: task text must name {{workdir}} and {{files}}", c.Name)
	}
	for _, level := range c.levels() {
		switch level {
		case "quiet", "normal", "active":
		default:
			return fmt.Errorf("eval: %s: level %q", c.Name, level)
		}
	}
	return nil
}

// LoadSuite reads a suite. An empty dir and the default name use the embedded suite.
// A non-empty dir is a custom suite on disk and is not the certification suite.
func LoadSuite(dir, suite string) ([]Case, string, error) {
	if suite == "" {
		suite = "proactivity"
	}
	if dir == "" && embeddedSuite(suite) {
		return loadFS(evals.Files, suite, "embedded")
	}
	var err error
	if dir == "" {
		dir, err = findSuite(suite)
		if err != nil {
			return nil, "", err
		}
	}
	return loadDir(dir)
}

func loadDir(dir string) ([]Case, string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, "", err
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("eval: no cases in %s", dir)
	}
	var out []Case
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		c, err := decodeCase(path, raw, func(rel string) ([]byte, error) {
			return readSuiteFile(dir, rel)
		})
		if err != nil {
			return nil, "", err
		}
		out = append(out, c)
	}
	return out, dir, nil
}

func embeddedSuite(suite string) bool {
	return suite == "proactivity" || suite == "proactivity-holdout" || suite == "dispatch" || suite == "voice"
}

func loadFS(fsys fs.FS, suite, label string) ([]Case, string, error) {
	matches, err := fs.Glob(fsys, suite+"/*.yaml")
	if err != nil {
		return nil, "", err
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("eval: no cases in %s", suite)
	}
	var out []Case
	for _, path := range matches {
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, "", err
		}
		c, err := decodeCase(path, raw, func(rel string) ([]byte, error) {
			return fs.ReadFile(fsys, suite+"/"+rel)
		})
		if err != nil {
			return nil, "", err
		}
		out = append(out, c)
	}
	return out, label, nil
}

func decodeCase(path string, raw []byte, read func(string) ([]byte, error)) (Case, error) {
	var c Case
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return Case{}, fmt.Errorf("eval: %s: %w", path, err)
	}
	for i, f := range c.Setup.Files {
		if f.File == "" {
			continue
		}
		body, err := read(f.File)
		if err != nil {
			return Case{}, fmt.Errorf("eval: %s: %w", path, err)
		}
		c.Setup.Files[i].Text = string(body)
	}
	if c.Name == "" {
		c.Name = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}
	if c.Grader == "" {
		c.Grader = "auto"
	}
	if err := c.validate(); err != nil {
		return Case{}, err
	}
	return c, nil
}

func readSuiteFile(dir, rel string) ([]byte, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return nil, fmt.Errorf("eval: bad fixture %q", rel)
	}
	return os.ReadFile(filepath.Join(dir, filepath.Clean(rel)))
}

func findSuite(suite string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		cand := filepath.Join(dir, "evals", suite)
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("eval: suite %q not found", suite)
		}
		dir = parent
	}
}
