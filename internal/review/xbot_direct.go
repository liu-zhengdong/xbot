package review

import "strings"

// DirectXBotArgs recognizes one literal POSIX invocation of this runtime binary.
// It deliberately does not implement shell expansion or compose commands.
func DirectXBotArgs(command, binary string) ([]string, bool) {
	var words []string
	for i := 0; i < len(command); {
		for i < len(command) && (command[i] == ' ' || command[i] == '\t') {
			i++
		}
		if i == len(command) {
			break
		}
		var word string
		if command[i] == '\'' || command[i] == '"' {
			quote := command[i]
			i++
			start := i
			for i < len(command) && command[i] != quote {
				if command[i] == '\n' || command[i] == '\r' || command[i] == 0 {
					return nil, false
				}
				i++
			}
			if i == len(command) {
				return nil, false
			}
			word = command[start:i]
			i++
			if i < len(command) && command[i] != ' ' && command[i] != '\t' {
				return nil, false
			}
			if quote == '"' && strings.ContainsAny(word, "$`\\") {
				if len(words) != 0 || word != "$XBOT_BIN" {
					return nil, false
				}
				word = binary
			}
		} else {
			start := i
			for i < len(command) && command[i] != ' ' && command[i] != '\t' {
				if command[i] < 32 || strings.ContainsRune("'\"\\$`|&;()< >*?[]{}!~#", rune(command[i])) {
					return nil, false
				}
				i++
			}
			word = command[start:i]
		}
		words = append(words, word)
	}
	if len(words) < 2 || binary == "" || words[0] != binary {
		return nil, false
	}
	return words[1:], true
}
