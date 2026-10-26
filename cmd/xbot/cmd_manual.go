package main

import (
	"io"
	"xbot/internal/manual"
)

func cmdManual(args []string, out io.Writer) error {
	if len(args) > 1 {
		return errUsage
	}
	section := "index"
	if len(args) == 1 {
		section = args[0]
	}
	body, err := manual.Read(section)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, body)
	return err
}
