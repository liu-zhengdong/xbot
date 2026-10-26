//go:build !windows

package tools

import "os/exec"

func configureInterpreterCmdLine(*exec.Cmd, string) {}
