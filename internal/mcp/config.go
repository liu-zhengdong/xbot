package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"xbot/internal/fsutil"
)

const maxSpecs = 32

func (o Options) providerKey(name string) bool {
	if o.IsProviderKey == nil {
		return false
	}
	return o.IsProviderKey(strings.TrimSpace(name))
}

var (
	// ErrMCPExists means this bot already has a server with that name.
	ErrMCPExists = errors.New("mcp: already exists")
	// ErrMCPMissing means the bot has no server with that name.
	ErrMCPMissing = errors.New("mcp: not found")

	mcpNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// Spec is one MCP server for this bot. Command starts a stdio process.
// URL, when set, is a Streamable HTTP endpoint and Command is unused.
// SecretEnvs names variables; values stay out of the file. Provider API keys
// are rejected. Dial, when set, replaces both transports. Tests use it.
// Secret, when set, is asked first for each SecretEnvs value (the Bot's own
// key store); the process environment is the fallback.
type Spec struct {
	Name       string                                                `json:"name"`
	Command    string                                                `json:"command,omitempty"`
	Args       []string                                              `json:"args"`
	URL        string                                                `json:"url,omitempty"`
	SecretEnvs []string                                              `json:"secret_envs"`
	Dial       func(ctx context.Context) (io.ReadWriteCloser, error) `json:"-"`
	Secret     func(name string) (string, bool)                      `json:"-"`
}

// HasSecretValues says each name is declared and has a non-empty value where
// the server will read it at start. A declared name alone is not a key.
func (s Spec) HasSecretValues(names []string) bool {
	if !HasSecretEnvs(s, names) {
		return false
	}
	lookup := s.secretLookup()
	for _, n := range names {
		if v, ok := lookup(n); !ok || strings.TrimSpace(v) == "" {
			return false
		}
	}
	return true
}

// secretLookup reads a SecretEnvs value from Secret, then the process environment.
func (s Spec) secretLookup() func(string) (string, bool) {
	return func(name string) (string, bool) {
		if s.Secret != nil {
			for _, n := range s.SecretEnvs {
				if n != name {
					continue
				}
				if v, ok := s.Secret(name); ok && v != "" {
					return v, true
				}
			}
		}
		return os.LookupEnv(name)
	}
}

// LoadMCP reads servers from path. A missing file is an empty list.
func LoadMCP(path string, options Options) ([]Spec, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return []Spec{}, nil
	} else if err != nil {
		return nil, err
	}
	var out []Spec
	err := fsutil.WithLock(path+".lock", func() error {
		var err error
		out, err = readMCP(path, options)
		return err
	})
	return out, err
}

// LoadMCPAll reads several lists and merges them by name. A later path wins, so a
// bot's own entry overrides the owner-level one of the same name.
func LoadMCPAll(options Options, paths ...string) ([]Spec, error) {
	var out []Spec
	at := map[string]int{}
	for _, p := range paths {
		list, err := LoadMCP(p, options)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			if i, ok := at[s.Name]; ok {
				out[i] = s
				continue
			}
			at[s.Name] = len(out)
			out = append(out, s)
		}
	}
	if out == nil {
		out = []Spec{}
	}
	return out, nil
}

// AddMCP appends one server. A duplicate name returns ErrMCPExists.
func AddMCP(path string, s Spec, options Options) error {
	s = normalizeServer(s)
	if err := ValidateMCP(s, options); err != nil {
		return err
	}
	return fsutil.WithLock(path+".lock", func() error {
		list, err := readMCP(path, options)
		if err != nil {
			return err
		}
		for _, e := range list {
			if e.Name == s.Name {
				return ErrMCPExists
			}
		}
		if len(list) >= maxSpecs {
			return errors.New("mcp: too many servers")
		}
		list = append(list, s)
		return writeMCP(path, list)
	})
}

// ReplaceMCP swaps the entry named s.Name for s when stale says so, under the
// file lock. A missing entry is added.
func ReplaceMCP(path string, s Spec, stale func(Spec) bool, options Options) error {
	s = normalizeServer(s)
	if err := ValidateMCP(s, options); err != nil {
		return err
	}
	return fsutil.WithLock(path+".lock", func() error {
		list, err := readMCP(path, options)
		if err != nil {
			return err
		}
		for i, e := range list {
			if e.Name != s.Name {
				continue
			}
			if !stale(e) {
				return nil
			}
			list[i] = s
			return writeMCP(path, list)
		}
		if len(list) >= maxSpecs {
			return errors.New("mcp: too many servers")
		}
		return writeMCP(path, append(list, s))
	})
}

// RemoveMCP deletes one server by name.
func RemoveMCP(path, name string, options Options) error {
	name = strings.TrimSpace(name)
	if !mcpNameRe.MatchString(name) {
		return ErrMCPMissing
	}
	return fsutil.WithLock(path+".lock", func() error {
		list, err := readMCP(path, options)
		if err != nil {
			return err
		}
		next := make([]Spec, 0, len(list))
		found := false
		for _, e := range list {
			if e.Name == name {
				found = true
				continue
			}
			next = append(next, e)
		}
		if !found {
			return ErrMCPMissing
		}
		return writeMCP(path, next)
	})
}

// ValidateMCP checks a server record before it is stored.
func ValidateMCP(s Spec, options Options) error {
	if !mcpNameRe.MatchString(s.Name) {
		return fmt.Errorf("mcp: invalid name %q", s.Name)
	}
	if err := validateEndpoint(s); err != nil {
		return err
	}
	if len(s.Args) > 64 {
		return errors.New("mcp: too many args")
	}
	for _, a := range s.Args {
		if strings.ContainsAny(a, "\n\r") || strings.ContainsRune(a, 0) {
			return errors.New("mcp: arg contains a newline")
		}
		if len(a) > 4096 {
			return errors.New("mcp: arg too long")
		}
	}
	if len(s.SecretEnvs) > 32 {
		return errors.New("mcp: too many secrets")
	}
	seen := map[string]bool{}
	for _, n := range s.SecretEnvs {
		if !envNameRe.MatchString(n) {
			return fmt.Errorf("mcp: invalid secret %q", n)
		}
		if options.providerKey(n) {
			return fmt.Errorf("mcp: provider key %q is not passed", n)
		}
		if seen[n] {
			return fmt.Errorf("mcp: duplicate secret %q", n)
		}
		seen[n] = true
	}
	return nil
}

func validateEndpoint(s Spec) error {
	hasURL := strings.TrimSpace(s.URL) != ""
	hasCmd := strings.TrimSpace(s.Command) != ""
	if hasURL && hasCmd {
		return errors.New("mcp: pick command or url")
	}
	if hasURL {
		return validateURL(s.URL)
	}
	return validateCommand(s.Command)
}

func validateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("mcp: empty url")
	}
	if len(raw) > 4096 || strings.ContainsAny(raw, "\n\r") || strings.ContainsRune(raw, 0) {
		return errors.New("mcp: invalid url")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("mcp: url must be http or https")
	}
	return nil
}

func validateCommand(command string) error {
	if command == "" {
		return errors.New("mcp: empty command")
	}
	if len(command) > 4096 {
		return errors.New("mcp: command too long")
	}
	if strings.ContainsAny(command, "\n\r") || strings.ContainsRune(command, 0) {
		return errors.New("mcp: command contains a newline")
	}
	return nil
}

func normalizeServer(s Spec) Spec {
	s.Name = strings.TrimSpace(s.Name)
	s.Command = strings.TrimSpace(s.Command)
	s.URL = strings.TrimSpace(s.URL)
	s.Args = nonNil(s.Args)
	envs := make([]string, 0, len(s.SecretEnvs))
	for _, n := range s.SecretEnvs {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		envs = append(envs, n)
	}
	s.SecretEnvs = envs
	s.Dial = nil
	return s
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

type mcpFile struct {
	Servers []Spec `json:"servers"`
}

func readMCP(path string, options Options) ([]Spec, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Spec{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return []Spec{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var file mcpFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	if file.Servers == nil {
		file.Servers = []Spec{}
	}
	if len(file.Servers) > maxSpecs {
		return nil, errors.New("mcp: too many servers")
	}
	seen := map[string]bool{}
	for i, s := range file.Servers {
		s = normalizeServer(s)
		if err := ValidateMCP(s, options); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("mcp: duplicate %s", s.Name)
		}
		seen[s.Name] = true
		file.Servers[i] = s
	}
	return file.Servers, nil
}

func writeMCP(path string, list []Spec) error {
	if list == nil {
		list = []Spec{}
	}
	for i := range list {
		list[i] = normalizeServer(list[i])
	}
	raw, err := fsutil.Marshal(mcpFile{Servers: list})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, 0600)
}

// HasSecretEnvs says the entry names every key in names.
func HasSecretEnvs(s Spec, names []string) bool {
	for _, n := range names {
		found := false
		for _, have := range s.SecretEnvs {
			if have == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
