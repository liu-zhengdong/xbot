package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

// shellJobs is the process-lifetime job set for this bot. The first call loads
// records left by a previous serve and marks the ones still running as lost.
// Once built, the set is returned without reading k.Bot: only renameBot moves
// it, so a caller holding a stale bot name can never point it back.
func (k *Kernel) shellJobs() *tools.Jobs {
	if k != nil && k.Resources != nil {
		k.shellMu.Lock()
		k.shell = k.Resources.Jobs
		k.shellMu.Unlock()
		return k.Resources.Jobs
	}
	if jobs := k.liveShell(); jobs != nil {
		return jobs
	}
	if k == nil || k.Bot == nil || k.Bot.Root == "" || k.Bot.Name() == "" {
		return nil
	}
	dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "shell-jobs")
	k.shellOnce.Do(func() {
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
	return k.liveShell()
}

// toolsSet is the kernel-held set of the six base tools ADR 0069 moved off
// ToolsLink: shells, file read/write/edit, web fetch/search, and view_image.
// The kernel keeps these handles for its whole life: attachShell writes the
// Jobs, session, cwd, IO, and vault bindings onto them, so handing the same
// instances to the registry is what makes a shell call see this bot's state.
// A nil k or a kernel without a bot root still builds: Dir/Root fall back to
// the process working directory, and every bind call tolerates a nil kernel.
type toolsSet struct {
	shells []tools.Tool
	read   *tools.ReadFile
	write  *tools.WriteFile
	edit   *tools.EditFile
	fetch  *tools.WebFetch
	search *tools.WebSearch
	view   *tools.ViewImage
}

// list returns the shells then the file, web, and vision tools.
func (s *toolsSet) list() []tools.Tool {
	out := append([]tools.Tool(nil), s.shells...)
	return append(out, s.read, s.write, s.edit, s.fetch, s.search, s.view)
}

// baseTools builds the kernel's six base tools once and binds this bot's
// state onto them. The set is stable: kernelTools rows return these handles.
func (k *Kernel) baseTools() *toolsSet {
	k.baseOnce.Do(func() {
		dir := ""
		if k != nil {
			dir = k.WorkDir
		}
		shared := ""
		if k != nil {
			shared = k.SharedDir
		}
		s := &toolsSet{
			read:   &tools.ReadFile{Root: dir},
			write:  &tools.WriteFile{Root: dir},
			edit:   &tools.EditFile{Root: dir},
			fetch:  &tools.WebFetch{},
			search: &tools.WebSearch{},
			view:   &tools.ViewImage{Root: dir},
		}
		for _, sh := range tools.NewShells() {
			sh.Dir = dir
			sh.Shared = shared
			s.shells = append(s.shells, sh)
		}
		k.base = s
	})
	return k.base
}

// attachShellJobs points every base shell at this bot's jobs. The pointer is
// written once per shell, so a running Shell.Run does not race with a later
// modelTools refresh.
func (k *Kernel) attachShellJobs() {
	jobs := k.shellJobs()
	if jobs == nil {
		return
	}
	for _, sh := range k.baseTools().shells {
		if s, ok := sh.(*tools.Shell); ok {
			s.BindJobs(jobs)
		}
	}
}

// attachShell points every base shell at this bot's job table and persistent
// cwd, and fills file, web, and vision configuration on the same held set.
func (k *Kernel) attachShell() {
	k.attachShellJobs()
	if k == nil {
		return
	}
	for _, t := range k.baseTools().list() {
		if sh, ok := t.(*tools.Shell); ok {
			sh.BindSession(k.shellStartDir, k.shellAfterDir)
			sh.SetMarkerDir(k.shellMarkerDir())
			k.bindShellRuntime(sh)
		}
		k.bindFileCwd(t)
		k.configureWeb(t)
	}
}

// bindShellRuntime is shared by mounted and scoped (subagent) shells.
func (k *Kernel) bindShellRuntime(sh *tools.Shell) {
	sh.BindContextEnvironment(k.cliCallEnvironment)
	sh.BindStartIO(k.withShellStartIO)
	sh.BindMarkerIO(func(fn func(string) error) error {
		return k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
			dir := filepath.Join(bot.Dir(b.Root, b.Name()), "cwd-markers")
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
			return fn(dir)
		})
	})
	if k.Bot == nil || k.Vault == nil {
		return
	}
	sh.BindLocalSudo(func() (vault.Backend, error) {
		return vault.OpenGlobal(k.Bot.Root)
	})
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

var errNoShellJobs = errors.New("shell: background jobs are not available")

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
