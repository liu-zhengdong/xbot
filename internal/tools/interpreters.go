package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// interpreter is immutable after discovery. Every Shell instance keeps its own
// session bindings while sharing only these process-wide executable facts.
type interpreter struct{ name, path, version string }

var interpreterOnce sync.Once
var installedInterpreters []interpreter

func NewShells() []*Shell {
	interpreterOnce.Do(func() {
		installedInterpreters = discoverInterpreters(runtime.GOOS, exec.LookPath, os.Getenv, probeVersion)
	})
	out := make([]*Shell, 0, len(installedInterpreters))
	for _, in := range installedInterpreters {
		out = append(out, newInterpreterShell(in))
	}
	return out
}

func newInterpreterShell(in interpreter) *Shell {
	return &Shell{interpreter: in, DefaultTimeout: defaultShellTimeout, MaxTimeout: maxShellTimeout, MaxOutput: maxShellOutput}
}

func discoverInterpreters(goos string, lookup func(string) (string, error), getenv func(string) string, probe func(interpreter) string) []interpreter {
	names := []string{"bash", "sh", "zsh"}
	if goos == "darwin" {
		names = []string{"zsh", "bash", "sh"}
	}
	if goos == "windows" {
		names = []string{"powershell", "cmd", "git-bash"}
	}
	var out []interpreter
	for _, name := range names {
		candidates := []string{name}
		if name == "powershell" {
			candidates = []string{"pwsh.exe", "powershell.exe"}
		}
		if name == "cmd" {
			candidates = []string{"cmd.exe"}
		}
		if name == "git-bash" {
			candidates = nil
			// Never accept an arbitrary bash.exe (it may be WSL). Use Git's layout.
			if git, err := lookup("git.exe"); err == nil {
				root := filepath.Dir(filepath.Dir(git))
				candidates = append(candidates, filepath.Join(root, "bin", "bash.exe"))
			}
			for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
				if root := getenv(key); root != "" {
					if key == "LOCALAPPDATA" {
						root = filepath.Join(root, "Programs")
					}
					candidates = append(candidates, filepath.Join(root, "Git", "bin", "bash.exe"))
				}
			}
		}
		for _, candidate := range candidates {
			path, err := lookup(candidate)
			if err != nil {
				continue
			}
			in := interpreter{name: name, path: path}
			in.version = strings.TrimSpace(probe(in))
			if in.version == "" {
				in.version = "版本未知"
			}
			out = append(out, in)
			break
		}
	}
	return out
}

func (in interpreter) command(ctx context.Context, script string) *exec.Cmd {
	if in.name == "" {
		in.name = "sh"
	}
	path := in.path
	if path == "" {
		path = in.name
	}
	switch in.name {
	case "powershell":
		return exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	case "cmd":
		cmd := exec.CommandContext(ctx, path, "/d", "/s", "/c", script)
		configureInterpreterCmdLine(cmd, script)
		return cmd
	case "bash", "git-bash":
		return exec.CommandContext(ctx, path, "--noprofile", "--norc", "-c", script)
	case "zsh":
		return exec.CommandContext(ctx, path, "-f", "-c", script)
	default:
		return exec.CommandContext(ctx, path, "-c", script)
	}
}

func probeVersion(in interpreter) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch in.name {
	case "powershell":
		cmd = in.command(ctx, "$PSVersionTable.PSVersion.ToString()")
	case "cmd":
		cmd = in.command(ctx, "ver")
	case "sh":
		cmd = in.command(ctx, `printf '%s' "${BASH_VERSION:-${ZSH_VERSION:-}}"`)
	default:
		cmd = exec.CommandContext(ctx, in.path, "--version")
	}
	// Version probes never receive held provider keys and have bounded output.
	cmd.Env = (&Shell{}).commandEnv(false)
	configureCmd(cmd)
	out := &capWriter{max: 512}
	cmd.Stdout = out
	cmd.Stderr = out
	if cmd.Run() != nil {
		return ""
	}
	s, _ := out.String()
	s = strings.TrimSpace(s)
	if line, _, ok := strings.Cut(s, "\n"); ok {
		s = line
	}
	return strings.Join(strings.Fields(s), " ")
}

func (s *Shell) wrapCwd(start, command, marker string) string {
	switch s.Name() {
	case "powershell":
		quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
		return "try {\n" + command + "\n$xbotOK = $?; $xbotExit = $LASTEXITCODE\n} finally { [IO.File]::WriteAllText(" + quote(marker) + ", (Get-Location).Path) }\nif (-not $xbotOK) { if ($xbotExit) { exit $xbotExit }; exit 1 }"
	case "cmd":
		// Paths are supplied through the environment, not interpolated as cmd code.
		return command + "\r\nset \"XBOT_CMD_EXIT=%errorlevel%\"\r\ncd > \"%XBOT_CWD_MARKER%\"\r\nexit /b %XBOT_CMD_EXIT%"
	default:
		if runtime.GOOS == "windows" && s.Name() == "git-bash" {
			return "trap " + shQuote("pwd -W > "+shQuote(filepath.ToSlash(marker))) + " EXIT\n" + command + "\n"
		}
		return wrapShellCwd(start, command, marker)
	}
}

func (s *Shell) interpreterDescription() string {
	in := s.interpreter
	if in.version == "" {
		in.version = "版本未知"
	}
	return fmt.Sprintf("Interpreter: %s (%s). ", s.Name(), in.version)
}
