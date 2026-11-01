package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"xbot/internal/cliops"
)

func readRoutineCreateInput(command cliops.RoutineCommand, stdin io.Reader) (cliops.RoutineCreate, error) {
	var input cliops.RoutineCreate
	err := readRoutineInput(command, stdin, &input)
	return input, err
}

func readRoutinePatchInput(command cliops.RoutineCommand, stdin io.Reader) (cliops.RoutinePatch, error) {
	var input cliops.RoutinePatch
	err := readRoutineInput(command, stdin, &input)
	return input, err
}

// readRoutineInput decodes external JSON directly into the caller's typed DTO.
// It checks both the source bytes and the final JSON wire size; domain values
// remain unchanged for the routine operation boundary.
func readRoutineInput(command cliops.RoutineCommand, stdin io.Reader, input any) error {
	reader := io.Reader(strings.NewReader(command.Args))
	if command.ArgsFile != "" {
		reader = stdin
		if command.ArgsFile != "-" {
			file, err := os.Open(command.ArgsFile)
			if err != nil {
				return err
			}
			defer file.Close()
			reader = file
		}
	}
	raw, err := io.ReadAll(io.LimitReader(reader, cliops.RoutineMaxRequest+1))
	if err != nil {
		return err
	}
	if len(raw) > cliops.RoutineMaxRequest {
		return errors.New("routine: arguments exceed 1 MiB")
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("routine: arguments must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return fmt.Errorf("routine: invalid arguments: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return errors.New("routine: arguments must be one JSON object followed by EOF")
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(wire) > cliops.RoutineMaxRequest {
		return errors.New("routine: request exceeds 1 MiB after JSON encoding")
	}
	return nil
}
