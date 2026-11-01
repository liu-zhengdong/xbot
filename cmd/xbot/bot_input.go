package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"xbot/internal/cliops"
)

// readBotCreateInput bounds source bytes and encoded request bytes. Persona
// normalization and the existing WritePersona limit belong to the domain.
func readBotCreateInput(command cliops.BotCommand, stdin io.Reader) (cliops.BotCreate, error) {
	input := cliops.BotCreate{Name: command.Name, Persona: command.Persona}
	reader := io.Reader(strings.NewReader(command.Persona))
	if command.PersonaFile != "" {
		reader = stdin
		if command.PersonaFile != "-" {
			file, err := os.Open(command.PersonaFile)
			if err != nil {
				return input, err
			}
			defer file.Close()
			reader = file
		}
	}
	raw, err := io.ReadAll(io.LimitReader(reader, cliops.BotMaxRequest+1))
	if err != nil {
		return input, err
	}
	if len(raw) > cliops.BotMaxRequest {
		return input, errors.New("bot: persona input exceeds 1 MiB")
	}
	input.Persona = string(raw)
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.BotMaxRequest {
		return input, errors.New("bot: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
