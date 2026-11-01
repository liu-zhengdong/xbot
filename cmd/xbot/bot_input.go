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
	input := cliops.BotCreate{Name: command.Name}
	text, err := readBotPersonaText(command, stdin)
	if err != nil {
		return input, err
	}
	input.Persona = text
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.BotMaxRequest {
		return input, errors.New("bot: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}

func readBotPersonaInput(command cliops.BotCommand, stdin io.Reader) (cliops.BotPersona, error) {
	text, err := readBotPersonaText(command, stdin)
	input := cliops.BotPersona{Text: text}
	if err != nil {
		return input, err
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.BotMaxRequest {
		return input, errors.New("persona: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}

// Both real callers read the complete raw input before their own typed JSON
// budget check. Only the domain writer trims and validates duties.
func readBotPersonaText(command cliops.BotCommand, stdin io.Reader) (string, error) {
	reader := io.Reader(strings.NewReader(command.Persona))
	if command.PersonaFile != "" {
		reader = stdin
		if command.PersonaFile != "-" {
			file, err := os.Open(command.PersonaFile)
			if err != nil {
				return "", err
			}
			defer file.Close()
			reader = file
		}
	}
	raw, err := io.ReadAll(io.LimitReader(reader, cliops.BotMaxRequest+1))
	if err != nil {
		return "", err
	}
	if len(raw) > cliops.BotMaxRequest {
		return "", errors.New("bot: persona input exceeds 1 MiB")
	}
	return string(raw), nil
}
