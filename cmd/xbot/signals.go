package main

import (
	"os"
	"strings"
	"syscall"
)

// stopSignals are the signals that stop a long-running command gracefully.
func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// oneLine flattens newlines and tabs so a value prints on one line.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}
