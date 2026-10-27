package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

// shellJobs is the process-lifetime job set for this bot. The first call loads
// records left by a previous serve and marks the ones still running as lost.
func (k *Kernel) shellJobs() *tools.Jobs {
	if k == nil || k.Bot == nil || k.Bot.Root == "" || k.Bot.Name() == "" {
		return nil
	}
	k.shellOnce.Do(func() {
		dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "shell-jobs")
		j := tools.NewJobs(dir)
		j.Redact = func(s string) string {
			return scrubSecrets(k.redactor()(s), k.secretValues())
		}
		j.Forms = k.shellForms
		j.Now = k.now
		j.OnDone = k.onToolDone
		if err := j.Load(); err == nil {
			k.shellMu.Lock()
			k.shell = j
			k.shellMu.Unlock()
		}
	})
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	return k.shell
}

// attachShellJobs points every shell tool on a mounted link at this bot's jobs.
// The pointer is written once per shell, so a running Shell.Run does not race
// with a later modelTools refresh.
func (k *Kernel) attachShellJobs() {
	jobs := k.shellJobs()
	if jobs == nil {
		return
	}
	for _, l := range k.snapshotLinks() {
		if l == nil {
			continue
		}
		for _, t := range l.Tools() {
			sh, ok := t.(*tools.Shell)
			if ok {
				sh.BindJobs(jobs)
			}
		}
	}
}

// attachShell points every shell at this bot's job table and persistent cwd.
func (k *Kernel) attachShell() {
	k.attachShellJobs()
	if k == nil {
		return
	}
	for _, l := range k.snapshotLinks() {
		if l == nil {
			continue
		}
		for _, t := range l.Tools() {
			sh, ok := t.(*tools.Shell)
			if ok {
				sh.BindSession(k.shellStartDir, k.shellAfterDir)
				sh.SetMarkerDir(k.shellMarkerDir())
			}
			k.bindFileCwd(t)
			k.configureWeb(t)
		}
	}
}

// shellBackgroundOn reports that a real shell on these links has a job table.
// An eval jail is also named "tools" but its shell is not *tools.Shell and has
// no jobs, so it must not gain shell_output, shell_status, or shell_cancel.
func shellBackgroundOn(links []link.Link) bool {
	for _, l := range links {
		if l == nil {
			continue
		}
		for _, t := range l.Tools() {
			sh, ok := t.(*tools.Shell)
			if ok && sh.Jobs != nil {
				return true
			}
		}
	}
	return false
}

// bindFileCwd points file tools at the shell's current directory. A sandbox
// stays rooted where it was built. Relative paths resolve at call time;
// absolute paths stay absolute, and freshness records use that absolute path.
func (k *Kernel) bindFileCwd(t tools.Tool) {
	if k == nil || t == nil {
		return
	}
	switch f := t.(type) {
	case *tools.ReadFile:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
		}
	case *tools.WriteFile:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
		}
	case *tools.EditFile:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
		}
	case *tools.ViewImage:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
			f.Allowed = k.allowedRoots
		}
	}
}

// configureWeb fills search, fetch, and vision from the data root. A tool that
// already has a backend or fetcher keeps it, so a test double is not replaced
// by the keyless default. The search key is read here and never logged.
func (k *Kernel) configureWeb(t tools.Tool) {
	if k == nil || t == nil {
		return
	}
	root := ""
	if k.Bot != nil {
		root = k.Bot.Root
	}
	cfg, err := config.LoadWeb(root)
	switch f := t.(type) {
	case *tools.WebSearch:
		if f.Store == nil {
			f.Store = k.Vault
		}
		if f.Now == nil {
			f.Now = k.now
		}
		if err != nil {
			if f.Backend == nil {
				f.Backend = tools.FuncSearch(func(context.Context, string, int) ([]tools.Hit, error) { return nil, err })
			}
			return
		}
		k.holdWebKeys(cfg)
		if f.Backend != nil {
			return
		}
		f.Backend = configuredSearch{root: root}
	case *tools.WebFetch:
		if f.Store == nil {
			f.Store = k.Vault
		}
		if f.Now == nil {
			f.Now = k.now
		}
		if f.Fetcher != nil {
			return
		}
		if err != nil {
			f.Fetcher = failFetch{err}
			return
		}
		ft, ferr := tools.NewFetcher(cfg.Fetch.Backend)
		if ferr != nil {
			f.Fetcher = failFetch{ferr}
			return
		}
		f.Fetcher = ft
	case *tools.ViewImage:
		model, ok := k.vision()
		f.Model = model
		f.Vision = ok
	}
}

// holdWebKeys copies search and fetch key names into memory and drops them
// from the process environment. A web.json written after startup is covered
// the next time a web tool is configured. DEEPSEEK_API_KEY is held when that
// is the selected search key.
func (k *Kernel) holdWebKeys(cfg config.WebFile) {
	var names []string
	if env, ok := config.SearchKeyEnv(cfg.Search); ok {
		names = append(names, env)
	}
	if env := strings.TrimSpace(cfg.Fetch.APIKeyEnv); env != "" {
		names = append(names, env)
	}
	if len(names) == 0 {
		return
	}
	config.HoldKeys(names)
	tools.AddKeyEnv(names)
}

type failFetch struct{ err error }

func (f failFetch) Fetch(context.Context, string) (tools.Page, error) { return tools.Page{}, f.err }

// vision is the bound model's image capability. An explicit providers.json
// vision flag wins; a kernel with no bot is deepseek-flash, which sees images.
func (k *Kernel) vision() (string, bool) {
	model := ""
	if k != nil && k.Bot != nil {
		cfg := k.Bot.Config()
		model = strings.TrimSpace(cfg.Model)
		prov := strings.TrimSpace(cfg.Provider)
		if k.Bot.Root != "" && prov != "" && model != "" {
			if _, spec, err := config.Lookup(k.Bot.Root, prov, model); err == nil {
				return spec.Name, spec.SeesImages()
			}
		}
	}
	if model == "" {
		model = "deepseek-flash"
	}
	return model, config.SeesImages(model, nil)
}

// shellForms lists every string the job redactor replaces: vault literals and
// their JSON, URL, and base64 forms, staged secret literals, and held API
// key values of at least 8 bytes. The writer holds a suffix of pending output
// only when it is a prefix of one of these. Vault forms are cached until the
// vault generation changes. Staged secrets and held keys are appended on
// every call, because HoldKeys can happen after the cache is filled.
func (k *Kernel) shellForms() []string {
	if k == nil {
		return nil
	}
	staged := k.stagedSecretValues()
	vGen := vaultFormGen(k)
	rGen := vault.RegisteredFormGen()
	var base []string
	k.shellMu.Lock()
	if k.formsOK && k.formsVGen == vGen && k.formsRGen == rGen {
		base = append([]string(nil), k.forms...)
		k.shellMu.Unlock()
	} else {
		k.shellMu.Unlock()
		base = k.collectVaultForms()
		k.shellMu.Lock()
		if k.formsVGen == vGen && k.formsRGen == rGen && k.formsOK {
			base = append([]string(nil), k.forms...)
		} else {
			k.formsOK = true
			k.forms = append([]string(nil), base...)
			k.formsVGen = vGen
			k.formsRGen = rGen
		}
		k.shellMu.Unlock()
	}
	extra := append([]string(nil), staged...)
	extra = append(extra, config.HeldForms()...)
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	for _, f := range base {
		seen[f] = struct{}{}
	}
	out := append([]string(nil), base...)
	for _, v := range extra {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (k *Kernel) collectVaultForms() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(list []string) {
		for _, f := range list {
			if f == "" {
				continue
			}
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
			out = append(out, f)
		}
	}
	if k != nil && k.Vault != nil {
		if src, ok := k.Vault.(interface{ Forms() []string }); ok && src != nil {
			add(src.Forms())
		}
	}
	add(vault.RegisteredForms())
	return out
}

func (k *Kernel) stagedSecretValues() []string {
	if k == nil {
		return nil
	}
	k.secretMu.Lock()
	defer k.secretMu.Unlock()
	var out []string
	seen := map[string]struct{}{}
	for _, m := range k.secrets {
		for _, value := range m {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func vaultFormGen(k *Kernel) uint64 {
	if k == nil || k.Vault == nil {
		return 0
	}
	type gen interface{ FormGen() uint64 }
	if g, ok := k.Vault.(gen); ok && g != nil {
		return g.FormGen()
	}
	return 0
}

type shellOutputTool struct{ k *Kernel }

func (shellOutputTool) Name() string { return "shell_output" }
func (shellOutputTool) Description() string {
	return "Read a background tool job's output. offset is a byte position in the whole stream, not the retained file; the result names next_offset and says when earlier bytes were dropped. When bytes are still buffered because they might begin a secret, the result includes held_bytes. Pass tail to take the last N lines. Do not poll in a tight loop; wait for its normal completion message, then read."
}
func (shellOutputTool) Parameters() any {
	return schema(map[string]any{
		"id":     stringProp("Background job id"),
		"offset": intProp("Byte offset to read from. Ignored when tail is set."),
		"tail":   intProp("Return the last N lines instead of a byte range."),
	}, []string{"id"})
}
func (shellOutputTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t shellOutputTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID     string `json:"id"`
		Offset int    `json:"offset"`
		Tail   int    `json:"tail"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	jobs := t.k.shellJobs()
	if jobs == nil {
		return "", errNoShellJobs
	}
	return jobs.Output(strings.TrimSpace(in.ID), in.Offset, in.Tail)
}

type shellStatusTool struct{ k *Kernel }

func (shellStatusTool) Name() string { return "shell_status" }
func (shellStatusTool) Description() string {
	return "Report one background tool job, or list them when id is omitted. Status is running, done, failed, cancelled, or lost. lost means this serve process restarted and is not tracking the process anymore. When bytes are still buffered because they might begin a secret, the result includes held_bytes."
}
func (shellStatusTool) Parameters() any {
	return schema(map[string]any{
		"id": stringProp("Background job id. Omit to list jobs."),
	}, []string{})
}
func (shellStatusTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t shellStatusTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", err
		}
	}
	jobs := t.k.shellJobs()
	if jobs == nil {
		return "", errNoShellJobs
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		list := jobs.List()
		if len(list) == 0 {
			return "no background tool jobs", nil
		}
		var b strings.Builder
		for i, job := range list {
			if i >= 32 {
				b.WriteString("...\n")
				break
			}
			fmt.Fprintf(&b, "%s %s %s", job.ID, job.Status, clipRunes(job.Command, 80))
			if job.Cwd != "" {
				fmt.Fprintf(&b, " cwd %s", job.Cwd)
			}
			if job.Held > 0 {
				fmt.Fprintf(&b, " held_bytes: %d", job.Held)
			}
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	job, err := jobs.Status(id)
	if err != nil {
		return "", err
	}
	line := fmt.Sprintf("%s %s", job.ID, job.Status)
	if job.Status == "done" {
		line += fmt.Sprintf(" exit %d", job.ExitCode)
	}
	if job.Cwd != "" {
		line += "\ncwd: " + job.Cwd
	}
	if job.Held > 0 {
		line += fmt.Sprintf("\nheld_bytes: %d", job.Held)
	}
	if job.Output != "" {
		line += "\noutput: " + job.Output
	}
	return line, nil
}

type shellCancelTool struct{ k *Kernel }

func (shellCancelTool) Name() string { return "shell_cancel" }
func (shellCancelTool) Description() string {
	return "Request cancellation of a background tool job. Shell jobs kill their process group; other tools stay running until they return."
}
func (shellCancelTool) Parameters() any {
	return schema(map[string]any{
		"id": stringProp("Background job id"),
	}, []string{"id"})
}
func (shellCancelTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &in)
	if strings.TrimSpace(in.ID) == "" {
		return false, "shell_cancel", nil
	}
	return false, "cancel " + strings.TrimSpace(in.ID), nil
}
func (t shellCancelTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	jobs := t.k.shellJobs()
	if jobs == nil {
		return "", errNoShellJobs
	}
	id := strings.TrimSpace(in.ID)
	out, err := jobs.Cancel(id)
	if errors.Is(err, tools.ErrForeignJob) {
		if remote, ok := t.k.execSnapshot().(*exec.Remote); ok {
			err = remote.CancelToolJob(context.Background(), jobs.Dir, id)
			if err == nil {
				out = "cancellation requested " + id
			}
		}
	}
	return out, err
}

var errNoShellJobs = errShell("shell: background jobs are not available")

type errShell string

func (e errShell) Error() string { return string(e) }
