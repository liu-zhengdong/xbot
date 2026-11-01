package cliops

import (
	"bytes"
	"fmt"
	"time"
)

// FormatBotList includes the final newline and preserves source-provided previews
// and absolute remote timestamps. The domain uses this exact text for projection.
func FormatBotList(reply BotListReply) string {
	var text bytes.Buffer
	fmt.Fprintf(&text, "count: %d\n", reply.Count)
	if reply.Local != nil {
		for _, bot := range *reply.Local {
			fmt.Fprintf(&text, "local\t%s", bot.Name)
			if bot.PersonaPreview != "" {
				fmt.Fprintf(&text, "\t%s", bot.PersonaPreview)
			}
			text.WriteByte('\n')
		}
	}
	if reply.Remote != nil {
		for _, bot := range *reply.Remote {
			fmt.Fprintf(&text, "remote\t%s\tseen %s\n", bot.Name, bot.Seen.Format(time.RFC3339Nano))
		}
	}
	if reply.File != "" {
		fmt.Fprintf(&text, "%s (access: %s, bytes: %d)\n", reply.File, reply.Access, reply.Bytes)
	}
	if reply.Error != "" {
		fmt.Fprintf(&text, "error: %s\n", reply.Error)
	}
	return text.String()
}

// FormatBotInfo renders the complete human representation without clipping.
func FormatBotInfo(reply BotInfoReply) string {
	var text bytes.Buffer
	fmt.Fprintf(&text, "count: %d\n", reply.Count)
	if reply.Bots != nil {
		for _, bot := range *reply.Bots {
			fmt.Fprintf(&text, "%s\t%s", bot.State, bot.Bot)
			if bot.Since != "" {
				fmt.Fprintf(&text, "\tsince %s", bot.Since)
			}
			if bot.Tool != "" {
				fmt.Fprintf(&text, "\ttool %s", bot.Tool)
			}
			if bot.ToolSummary != "" {
				fmt.Fprintf(&text, "\t%s", bot.ToolSummary)
			}
			if bot.Backgrounds != 0 {
				fmt.Fprintf(&text, "\tbackgrounds %d", bot.Backgrounds)
			}
			if bot.Agents != 0 {
				fmt.Fprintf(&text, "\tagents %d", bot.Agents)
			}
			if bot.CompletedUnread != 0 {
				fmt.Fprintf(&text, "\tcompleted_unread %d", bot.CompletedUnread)
			}
			if bot.LastMessage != "" {
				fmt.Fprintf(&text, "\tlast: %s", bot.LastMessage)
			}
			if bot.Todo != "" {
				fmt.Fprintf(&text, "\ttodo: %s", bot.Todo)
			}
			text.WriteByte('\n')
			if use := bot.Tokens; use != nil {
				fmt.Fprintf(&text, "tokens\tprompt %d\tcompletion %d\tcached %d\tturn_completion %d\tsession_prompt %d\tsession_completion %d\tsession_cached %d\twindow %d\testimated %t\n", use.Prompt, use.Completion, use.Cached, use.TurnCompletion, use.SessionPrompt, use.SessionCompletion, use.SessionCached, use.Window, use.Estimated)
			}
		}
	}
	if reply.Steps != nil {
		for _, step := range *reply.Steps {
			fmt.Fprintf(&text, "step\t%s\t%s\t%s", step.Time.Format(time.RFC3339Nano), step.State, step.Tool)
			if step.Summary != "" {
				fmt.Fprintf(&text, "\t%s", step.Summary)
			}
			text.WriteByte('\n')
		}
	}
	if reply.File != "" {
		fmt.Fprintf(&text, "%s (access: %s, bytes: %d)\n", reply.File, reply.Access, reply.Bytes)
	}
	if reply.Error != "" {
		fmt.Fprintf(&text, "error: %s\n", reply.Error)
	}
	return text.String()
}

func FormatBotCreate(reply BotCreateReply) string {
	text := fmt.Sprintf("name: %s\napplied: %t\nstage: %s\n", reply.Name, reply.Applied, reply.Stage)
	if reply.Error != "" {
		text += fmt.Sprintf("error: %s\n", reply.Error)
	}
	return text
}

// FormatBotPin includes the final newline; server budgets and CLI output share it.
func FormatBotPin(reply BotPinReply) string {
	text := fmt.Sprintf("name: %s\npinned: %t\napplied: %t\nstage: %s\n", reply.Name, reply.Pinned, reply.Applied, reply.Stage)
	if reply.Error != "" {
		text += fmt.Sprintf("error: %s\n", reply.Error)
	}
	return text
}

// FormatBotPrimary reports the committed pointer, including partial failures.
func FormatBotPrimary(reply BotPrimaryReply) string {
	text := fmt.Sprintf("name: %s\n", reply.Name)
	if reply.Primary != "" {
		text += fmt.Sprintf("primary: %s\n", reply.Primary)
	}
	text += fmt.Sprintf("applied: %t\nstage: %s\n", reply.Applied, reply.Stage)
	if reply.Error != "" {
		text += fmt.Sprintf("error: %s\n", reply.Error)
	}
	return text
}

// FormatBotPersona includes the final newline for shared server and CLI budgets.
func FormatBotPersona(reply BotPersonaReply) string {
	text := fmt.Sprintf("name: %s\nwritten: %t\n", reply.Name, reply.Written)
	if reply.Effective != "" {
		text += fmt.Sprintf("effective: %s\n", reply.Effective)
	}
	if reply.Error != "" {
		text += fmt.Sprintf("error: %s\n", reply.Error)
	}
	return text
}

func FormatBotDelete(reply BotDeleteReply) string {
	text := fmt.Sprintf("name: %s\nremoved: %t\nstage: %s\n", reply.Name, reply.Removed, reply.Stage)
	if reply.Error != "" {
		text += fmt.Sprintf("error: %s\n", reply.Error)
	}
	return text
}
