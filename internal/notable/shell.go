package notable

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"xbot/internal/review"
)

type word struct {
	text   string
	op     bool
	quoted bool
}

// shellWords handles only literal simple commands. Expansion, pipelines, lists,
// substitutions and here-documents are intentionally not claimed as facts.
func shellWords(cmd string) ([]word, bool) {
	var words []word
	var buf strings.Builder
	var quote rune
	started := false
	quoted := false
	flush := func() {
		if started {
			words = append(words, word{text: buf.String(), quoted: quoted})
			buf.Reset()
			started = false
			quoted = false
		}
	}
	r := []rune(cmd)
	for i := 0; i < len(r); i++ {
		c := r[i]
		if c == '\n' || c == '\r' || c == '`' || c == '$' {
			return nil, false
		}
		if c == '\\' {
			quoted = true
			if i+1 == len(r) {
				return nil, false
			}
			i++
			buf.WriteRune(r[i])
			started = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				buf.WriteRune(c)
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quoted = true
			quote = c
			started = true
		case strings.ContainsRune(";&|<(){}*?[]", c):
			return nil, false
		case c == '>':
			flush()
			op := ">"
			if i+1 < len(r) && r[i+1] == '>' {
				i++
				op = ">>"
			}
			words = append(words, word{text: op, op: true})
		case unicode.IsSpace(c):
			flush()
		default:
			buf.WriteRune(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return words, len(words) > 0
}

// Shell is called only after a command returned an explicit exit 0. dir and env
// are the actual invocation's inputs, rather than the model's requested cwd.
func Shell(ctx context.Context, cmd, dir string, env []string) {
	if from(ctx).log == nil {
		return
	}
	ws, ok := shellWords(cmd)
	if !ok {
		return
	}
	var args []string
	var literals []word
	for i := 0; i < len(ws); i++ {
		if !ws[i].op {
			args = append(args, ws[i].text)
			literals = append(literals, ws[i])
			continue
		}
		if i+1 >= len(ws) || ws[i+1].op {
			return
		}
		target := ws[i+1].text
		i++
		if strings.HasPrefix(target, "~/") && !ws[i].quoted {
			home := ""
			for _, v := range env {
				if strings.HasPrefix(v, "HOME=") {
					home = strings.TrimPrefix(v, "HOME=")
				}
			}
			if home == "" {
				continue
			}
			target = filepath.Join(home, target[2:])
		}
		if target == "/dev/null" {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		if path, err := filepath.EvalSymlinks(target); err == nil {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				Wrote(ctx, path)
			}
		}
	}
	if len(args) == 0 {
		return
	}
	if args[0] == "sudo" {
		args = args[1:]
		literals = literals[1:]
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return
		}
	}
	base := filepath.Base(args[0])
	// Preview/help options must be checked before deletion or send classification.
	// Redirects above are still real writes, even when the command only previews.
	if preview(args, base) {
		return
	}
	switch base {
	case "rm", "unlink", "rmdir", "shred":
		deletes, _ := review.ShellFlags(cmd)
		if deletes {
			recordDelete(ctx, shellPaths(literals[1:], dir, env), "个文件或目录")
		}
	case "find":
		if findDeletes(literals[1:]) {
			recordDelete(ctx, nil, "")
		}
	case "git":
		if len(args) > 1 && (args[1] == "clean" || args[1] == "push") {
			deletes, _ := review.ShellFlags(cmd)
			if deletes {
				recordDelete(ctx, nil, "")
			}
		}
	case "curl", "wget", "sendmail", "mail", "mailx", "nc", "ncat", "netcat", "scp", "rsync":
		_, sends := review.ShellFlags(cmd)
		if sends {
			record(ctx, "external_message", shellDestinations(args)...)
		}
	}
	installed := false
	switch base {
	case "npm":
		installed = len(args) > 1 && (args[1] == "install" || args[1] == "i") && has(args, "-g", "--global")
	case "brew":
		installed = len(args) > 1 && args[1] == "install"
	case "go":
		installed = len(args) > 1 && args[1] == "install"
	case "pip", "pip3", "python", "python3":
		tail := args[1:]
		if base == "python" || base == "python3" {
			if len(tail) < 2 || tail[0] != "-m" || tail[1] != "pip" {
				return
			}
			tail = tail[2:]
		}
		installed = len(tail) > 0 && tail[0] == "install" && !hasPrefix(tail, "--target", "--prefix", "--root", "-t") && !virtualEnv(args[0], env)
	}
	if installed {
		record(ctx, "global_install", installObjects(args)...)
	}
}
func has(args []string, wants ...string) bool {
	for _, arg := range args {
		for _, want := range wants {
			if arg == want {
				return true
			}
		}
	}
	return false
}
func hasPrefix(args []string, wants ...string) bool {
	for _, arg := range args {
		for _, want := range wants {
			if arg == want || strings.HasPrefix(arg, want+"=") {
				return true
			}
		}
	}
	return false
}
func virtualEnv(executable string, env []string) bool {
	for _, line := range env {
		if (strings.HasPrefix(line, "VIRTUAL_ENV=") || strings.HasPrefix(line, "CONDA_PREFIX=")) && strings.SplitN(line, "=", 2)[1] != "" {
			return true
		}
	}
	if !strings.ContainsRune(executable, filepath.Separator) {
		path, err := exec.LookPath(executable)
		if err != nil {
			return true
		}
		executable = path
	}
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		executable = real
	} else {
		return true
	}
	if filepath.IsAbs(executable) || strings.ContainsRune(executable, filepath.Separator) {
		_, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(executable)), "pyvenv.cfg"))
		return err == nil
	}
	return false
}

// preview recognizes short option clusters only for commands which define -n
// as no-execution. For find, -name is an expression, not a preview option.
func preview(args []string, base string) bool {
	for _, a := range args[1:] {
		if a == "--" {
			break
		}
		if has([]string{a}, "--dry-run", "--package-lock-only", "--help", "--version", "-h") {
			return true
		}
		if (base == "git" || base == "rsync") && strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "n") {
			return true
		}
	}
	return false
}
func findDeletes(words []word) bool {
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch w.text {
		case "-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename", "-regex", "-iregex", "-lname", "-ilname":
			i++
			continue
		}
		if w.text == "-delete" && !w.quoted {
			return true
		}
	}
	return false
}
func shellPaths(args []word, dir string, env []string) []string {
	home := ""
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			home = strings.TrimPrefix(e, "HOME=")
		}
	}
	var paths []string
	options := true
	for _, w := range args {
		a := w.text
		if options && a == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(a, "-") {
			continue
		}
		if strings.HasPrefix(a, "~/") && !w.quoted {
			if home == "" {
				continue
			}
			a = filepath.Join(home, a[2:])
		}
		if !filepath.IsAbs(a) {
			a = filepath.Join(dir, a)
		}
		paths = append(paths, displayPath(a, home))
	}
	return paths
}
func shellDestinations(args []string) []string {
	var hosts []string
	base := filepath.Base(args[0])
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if strings.HasPrefix(a, "--url=") {
				hosts = append(hosts, endpoint(strings.TrimPrefix(a, "--url="))...)
				continue
			}
			switch a {
			case "--url":
				if i+1 < len(args) {
					i++
					hosts = append(hosts, endpoint(args[i])...)
				}
			case "-X", "--request", "-d", "--data", "--data-raw", "--data-binary", "--data-ascii", "--data-urlencode", "--json", "-F", "--form", "--form-string", "-H", "--header", "-u", "--user", "-o", "--output", "-e", "--referer", "--proxy", "-x", "--upload-file", "-T", "--config", "-K", "-A", "--user-agent", "--cookie", "-b", "-P", "-p":
				i++
			}
			continue
		}
		if strings.Contains(a, "://") {
			hosts = append(hosts, endpoint(a)...)
		} else if base == "scp" || base == "rsync" {
			if host, _, ok := strings.Cut(a, ":"); ok && !strings.ContainsAny(host, "/\\") {
				if _, h, ok := strings.Cut(host, "@"); ok {
					host = h
				}
				if host != "" {
					hosts = append(hosts, host)
				}
			}
		}
	}
	return hosts
}
func installObjects(args []string) []string {
	tail := args[1:]
	if len(tail) > 2 && tail[0] == "-m" && tail[1] == "pip" {
		tail = tail[2:]
	}
	if len(tail) == 0 {
		return nil
	}
	tail = tail[1:]
	var objects []string
	for i := 0; i < len(tail); i++ {
		a := tail[i]
		if a == "--" {
			objects = append(objects, tail[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			switch a {
			case "-r", "--requirement", "-c", "--constraint":
				return nil
			case "--registry", "--cache", "--prefix", "--userconfig", "--index-url", "--extra-index-url", "-i", "--proxy", "--timeout", "--cert", "--client-cert":
				i++
			case "-g", "--global", "--user", "--force", "--no-audit", "--no-fund", "--ignore-scripts", "--prefer-offline", "--offline", "--quiet", "-q", "-v", "--verbose", "--cask", "--formula":
			default:
				if !strings.Contains(a, "=") {
					return nil
				}
			}
			continue
		}
		// A URL does not reliably name the installed package. Keep the generic
		// installation notice rather than calling its host the software name.
		if strings.Contains(a, "://") {
			return nil
		} else {
			objects = append(objects, a)
		}
	}
	return objects
}
