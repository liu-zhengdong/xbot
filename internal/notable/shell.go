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
	for i := 0; i < len(ws); i++ {
		if !ws[i].op {
			args = append(args, ws[i].text)
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
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return
		}
	}
	base := filepath.Base(args[0])
	// Use the existing policy flags only for actual command positions, not words
	// in echo/printf or a file name that happens to be "rm".
	switch base {
	case "rm", "unlink", "rmdir", "shred":
		if !has(args, "--help", "--version", "-h") {
			deletes, _ := review.ShellFlags(cmd)
			if deletes {
				record(ctx, "delete")
			}
		}
	case "find":
		if has(args, "-delete") {
			deletes, _ := review.ShellFlags(cmd)
			if deletes {
				record(ctx, "delete")
			}
		}
	case "git":
		if len(args) > 1 && (args[1] == "clean" || args[1] == "push") {
			deletes, _ := review.ShellFlags(cmd)
			if deletes {
				record(ctx, "delete")
			}
		}
	case "curl", "wget", "sendmail", "mail", "mailx", "nc", "ncat", "netcat", "scp", "rsync":
		_, sends := review.ShellFlags(cmd)
		if sends {
			record(ctx, "external_message")
		}
	}
	for _, arg := range args {
		if arg == "--dry-run" || arg == "--package-lock-only" || arg == "--help" || arg == "--version" || arg == "-h" || arg == "-n" {
			return
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
		record(ctx, "global_install")
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
